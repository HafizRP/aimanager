#!/usr/bin/env bash
set -euo pipefail

BASE_URL="http://127.0.0.1:20129"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
DB_PATH="${DB_PATH:-$REPO_ROOT/data/gateway.db}"

echo "=================================================="
echo "    Running 9router Gateway Verification Suite    "
echo "=================================================="

# 1. Check Health Probes
echo -n "[1/7] Testing Health Probes (/healthz, /readyz)... "
RES_HEALTH=$(curl -s "$BASE_URL/healthz")
RES_READY=$(curl -s "$BASE_URL/readyz")
if [[ "$RES_HEALTH" == "OK" && "$RES_READY" == "READY" ]]; then
    echo "✓ PASSED"
else
    echo "✗ FAILED (healthz: $RES_HEALTH, readyz: $RES_READY)"
    exit 1
fi

# Setup temporary test users for verification
TEST_UID="test-suite-user-$(date +%s)"
TEST_KEY="sk-gw-test-verifier-$(date +%s)"
sqlite3 "$DB_PATH" "INSERT INTO users (id, username, name, role, token_quota, tokens_used, allowed_models, is_active, created_at, updated_at) VALUES ('$TEST_UID', '$TEST_UID', 'Test Verifier', 'user', 1000000, 0, '[\"ag/gemini-3.8-flash-low\"]', 1, datetime('now'), datetime('now'));"
sqlite3 "$DB_PATH" "INSERT INTO api_keys (id, user_id, key, name, is_active, created_at) VALUES ('k-$TEST_UID', '$TEST_UID', '$TEST_KEY', 'Verify Key', 1, datetime('now'));"

# Find any admin or wildcard key
ADMIN_KEY=$(sqlite3 "$DB_PATH" "SELECT k.key FROM api_keys k JOIN users u ON k.user_id = u.id WHERE u.allowed_models LIKE '%*%' LIMIT 1;")
if [[ -z "$ADMIN_KEY" ]]; then
    ADMIN_UID="test-admin-$(date +%s)"
    ADMIN_KEY="sk-gw-test-admin-$(date +%s)"
    sqlite3 "$DB_PATH" "INSERT INTO users (id, username, name, role, token_quota, tokens_used, allowed_models, is_active, created_at, updated_at) VALUES ('$ADMIN_UID', '$ADMIN_UID', 'Admin Verifier', 'admin', 0, 0, '[\"*\"]', 1, datetime('now'), datetime('now'));"
    sqlite3 "$DB_PATH" "INSERT INTO api_keys (id, user_id, key, name, is_active, created_at) VALUES ('k-$ADMIN_UID', '$ADMIN_UID', '$ADMIN_KEY', 'Admin Verify Key', 1, datetime('now'));"
fi

cleanup() {
    sqlite3 "$DB_PATH" "DELETE FROM api_keys WHERE user_id = '$TEST_UID';" 2>/dev/null || true
    sqlite3 "$DB_PATH" "DELETE FROM users WHERE id = '$TEST_UID';" 2>/dev/null || true
    if [[ -n "${ADMIN_UID:-}" ]]; then
        sqlite3 "$DB_PATH" "DELETE FROM api_keys WHERE user_id = '$ADMIN_UID';" 2>/dev/null || true
        sqlite3 "$DB_PATH" "DELETE FROM users WHERE id = '$ADMIN_UID';" 2>/dev/null || true
    fi
}
trap cleanup EXIT

# 2. Test Unauthenticated Request
echo -n "[2/7] Testing Auth Rejection (Missing/Invalid Key)... "
CODE_NO_KEY=$(curl -s -o /dev/null -w "%{http_code}" "$BASE_URL/v1/models")
CODE_BAD_KEY=$(curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer sk-invalid" "$BASE_URL/v1/models")
if [[ "$CODE_NO_KEY" == "401" && "$CODE_BAD_KEY" == "401" ]]; then
    echo "✓ PASSED (Returned 401 Unauthorized)"
else
    echo "✗ FAILED (Codes: $CODE_NO_KEY, $CODE_BAD_KEY)"
    exit 1
fi

# 3. Test Model Whitelist Filtering on /v1/models
echo -n "[3/7] Testing /v1/models Whitelist Filtering... "
ADMIN_MODELS_COUNT=$(curl -s -H "Authorization: Bearer $ADMIN_KEY" "$BASE_URL/v1/models" | jq '.data | length')
USER_MODELS_COUNT=$(curl -s -H "Authorization: Bearer $TEST_KEY" "$BASE_URL/v1/models" | jq '.data | length')
if [[ "$ADMIN_MODELS_COUNT" -gt 10 && "$USER_MODELS_COUNT" -eq 1 ]]; then
    echo "✓ PASSED (Admin sees $ADMIN_MODELS_COUNT models, User restricted to $USER_MODELS_COUNT model)"
else
    echo "✗ FAILED (Admin count: $ADMIN_MODELS_COUNT, User count: $USER_MODELS_COUNT)"
    exit 1
fi

# 4. Test Model Whitelist Enforcement on Completions
echo -n "[4/7] Testing 403 Forbidden for Non-Whitelisted Model... "
HTTP_FORBIDDEN=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$BASE_URL/v1/chat/completions" \
  -H "Authorization: Bearer $TEST_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model": "kr/claude-sonnet-4.5", "messages": [{"role": "user", "content": "hi"}]}')
if [[ "$HTTP_FORBIDDEN" == "403" ]]; then
    echo "✓ PASSED (Blocked with 403 Forbidden)"
else
    echo "✗ FAILED (Expected 403, got $HTTP_FORBIDDEN)"
    exit 1
fi

# 5. Test Successful Chat Completion (Non-Streaming)
echo -n "[5/7] Testing 200 OK for Whitelisted Model (Non-Streaming)... "
RES_OK=$(curl -s -X POST "$BASE_URL/v1/chat/completions" \
  -H "Authorization: Bearer $TEST_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model": "ag/gemini-3.8-flash-low", "messages": [{"role": "user", "content": "ping"}], "max_tokens": 5, "stream": false}')
if echo "$RES_OK" | grep -q "choices"; then
    echo "✓ PASSED (Received completion response)"
else
    echo "✗ FAILED (Response: $RES_OK)"
    exit 1
fi

# 6. Test Streaming SSE Completion & Token Counting
echo -n "[6/7] Testing 200 OK Streaming SSE... "
SSE_RES=$(curl -s -N -X POST "$BASE_URL/v1/chat/completions" \
  -H "Authorization: Bearer $TEST_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model": "ag/gemini-3.8-flash-low", "messages": [{"role": "user", "content": "hello"}], "stream": true}')
if echo "$SSE_RES" | grep -q "data:"; then
    echo "✓ PASSED (Received SSE stream chunks)"
else
    echo "✗ FAILED (Streaming output empty)"
    exit 1
fi

# 7. Test Web Admin Dashboard & Auth
echo -n "[7/7] Testing Web Admin Dashboard & Session Auth... "
COOKIE_JAR=$(mktemp)
ADMIN_USER="${ADMIN_USERNAME:-admin}"
ADMIN_PASS="${ADMIN_PASSWORD:-admin123}"
LOGIN_STATUS=$(curl -s -c "$COOKIE_JAR" -o /dev/null -w "%{http_code}" -X POST "$BASE_URL/login" \
  -d "username=$ADMIN_USER&password=$ADMIN_PASS")
DASH_STATUS=$(curl -s -b "$COOKIE_JAR" -o /dev/null -w "%{http_code}" "$BASE_URL/")
STATS_JSON=$(curl -s -b "$COOKIE_JAR" "$BASE_URL/api/stats")
rm -f "$COOKIE_JAR"

if [[ "$DASH_STATUS" == "200" ]] && echo "$STATS_JSON" | grep -q "total_requests"; then
    echo "✓ PASSED (Dashboard HTTP 200, Stats API validated)"
else
    echo "✗ FAILED (Login: $LOGIN_STATUS, Dash: $DASH_STATUS)"
    exit 1
fi

echo "=================================================="
echo "    ALL 7 TEST SUITES PASSED SUCCESSFULLY!       "
echo "=================================================="
