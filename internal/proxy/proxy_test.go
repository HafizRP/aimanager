package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"9router-gateway/internal/models"
)

func TestParseAllowedModels(t *testing.T) {
	// Empty string -> wildcard
	list1 := models.ParseAllowedModels("")
	if len(list1) != 1 || list1[0] != "*" {
		t.Errorf("expected ['*'], got %v", list1)
	}

	// JSON array
	list2 := models.ParseAllowedModels(`["ag/gemini-3.8-flash", "main"]`)
	if len(list2) != 2 || list2[0] != "ag/gemini-3.8-flash" || list2[1] != "main" {
		t.Errorf("unexpected json array parse: %v", list2)
	}

	// Comma separated
	list3 := models.ParseAllowedModels("gpt-4o, claude-3-5-sonnet")
	if len(list3) != 2 || list3[0] != "gpt-4o" || list3[1] != "claude-3-5-sonnet" {
		t.Errorf("unexpected comma-separated parse: %v", list3)
	}
}

func TestHasWildcard(t *testing.T) {
	if !models.HasWildcard([]string{"*"}) {
		t.Error("expected wildcard true for ['*']")
	}
	if !models.HasWildcard([]string{"model-a", "*", "model-b"}) {
		t.Error("expected wildcard true when '*' is in slice")
	}
	if models.HasWildcard([]string{"model-a", "model-b"}) {
		t.Error("expected wildcard false when '*' is not in slice")
	}
}

func TestExtractAPIKey(t *testing.T) {
	// 1. Authorization: Bearer
	req1 := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req1.Header.Set("Authorization", "Bearer sk-gw-test-12345")
	if key := extractAPIKey(req1); key != "sk-gw-test-12345" {
		t.Errorf("expected 'sk-gw-test-12345', got '%s'", key)
	}

	// 2. Authorization: Raw key
	req2 := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req2.Header.Set("Authorization", "sk-gw-raw-key")
	if key := extractAPIKey(req2); key != "sk-gw-raw-key" {
		t.Errorf("expected 'sk-gw-raw-key', got '%s'", key)
	}

	// 3. x-api-key header
	req3 := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req3.Header.Set("x-api-key", "sk-gw-header-key")
	if key := extractAPIKey(req3); key != "sk-gw-header-key" {
		t.Errorf("expected 'sk-gw-header-key', got '%s'", key)
	}

	// 4. Missing
	req4 := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	if key := extractAPIKey(req4); key != "" {
		t.Errorf("expected empty key, got '%s'", key)
	}
}

func TestGetClientIP(t *testing.T) {
	// CF-Connecting-IP priority
	req0 := httptest.NewRequest(http.MethodGet, "/", nil)
	req0.Header.Set("CF-Connecting-IP", "203.0.113.195")
	if ip := GetClientIP(req0); ip != "203.0.113.195" {
		t.Errorf("expected '203.0.113.195', got '%s'", ip)
	}

	// X-Forwarded-For
	req1 := httptest.NewRequest(http.MethodGet, "/", nil)
	req1.Header.Set("X-Forwarded-For", "203.0.113.195, 70.41.3.18")
	if ip := GetClientIP(req1); ip != "203.0.113.195" {
		t.Errorf("expected '203.0.113.195', got '%s'", ip)
	}

	// X-Real-IP
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.Header.Set("X-Real-IP", "198.51.100.1")
	if ip := GetClientIP(req2); ip != "198.51.100.1" {
		t.Errorf("expected '198.51.100.1', got '%s'", ip)
	}

	// IPv6 Host:Port
	req3 := httptest.NewRequest(http.MethodGet, "/", nil)
	req3.RemoteAddr = "[2001:db8::1]:8080"
	if ip := GetClientIP(req3); ip != "2001:db8::1" {
		t.Errorf("expected '2001:db8::1', got '%s'", ip)
	}
}

func TestRateLimiter(t *testing.T) {
	rl := NewRateLimiter()

	// Limit 3 RPM
	id := "test-client-1"
	for i := 0; i < 3; i++ {
		allowed, _ := rl.Allow(id, 3, 0, 0)
		if !allowed {
			t.Fatalf("expected request %d to be allowed", i+1)
		}
	}

	// 4th request should be rejected
	allowed, msg := rl.Allow(id, 3, 0, 0)
	if allowed {
		t.Fatalf("expected 4th request to exceed RPM limit")
	}
	if msg == "" {
		t.Fatalf("expected rate limit error message")
	}

	// TPM limit test
	idTPM := "test-client-tpm"
	allowed, _ = rl.Allow(idTPM, 0, 1000, 600)
	if !allowed {
		t.Fatalf("expected first request with 600 tokens to be allowed")
	}

	allowed, _ = rl.Allow(idTPM, 0, 1000, 500)
	if allowed {
		t.Fatalf("expected second request with 500 tokens (total 1100) to be rejected by TPM limit")
	}
}

func TestResponseCache(t *testing.T) {
	rc := NewResponseCache()

	key := rc.GenerateKey("model-a", "hello world", 0.7)
	if key == "" {
		t.Fatalf("expected non-empty cache key")
	}

	// Miss
	if _, found := rc.Get(key); found {
		t.Fatalf("expected cache miss initially")
	}

	// Set & Hit
	body := []byte(`{"id":"chatcmpl-123","choices":[{"message":{"content":"Hi"}}]}`)
	rc.Set(key, body, 200, "application/json", 15, "model-a", 100*time.Millisecond)

	cached, found := rc.Get(key)
	if !found {
		t.Fatalf("expected cache hit after Set")
	}
	if string(cached.Body) != string(body) {
		t.Fatalf("cached body mismatch")
	}
	if cached.TotalTokens != 15 {
		t.Fatalf("cached tokens mismatch: %d", cached.TotalTokens)
	}

	// Expiration
	time.Sleep(120 * time.Millisecond)
	if _, found := rc.Get(key); found {
		t.Fatalf("expected cache item to be expired")
	}
}
