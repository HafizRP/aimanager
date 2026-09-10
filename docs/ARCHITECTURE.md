# System Architecture & Invariants

This document details the architectural design, security boundaries, and operational invariants of **AI Manager (`9router-gateway`)**.

---

## 1. High-Level Topology

AI Manager acts as an enterprise-grade gateway, reverse proxy, and multi-tenant billing/rate-limiting shield positioned directly in front of **9router Core**.

```
Clients (Cursor, Claude Code, Cline, Copilot, Python SDKs)
                         │
                         │ HTTPS / Bearer Token / Session Cookie
                         ▼
┌─────────────────────────────────────────────────────────────┐
│                 AI MANAGER GATEWAY (Port 20129)              │
│                                                             │
│  [ Chi v5 HTTP Middleware Pipeline ]                         │
│  ├── RequestID, RealIP, Structured Logger (zerolog), Recoverer │
│  ├── Security Headers (HSTS, CSP, X-Frame-Options: DENY)     │
│  └── CORS Preflight Handler                                 │
│                                                             │
│  [ Traffic Classification ]                                 │
│  ├── /v1/*       -> GatewayProxy (OpenAI LLM API)           │
│  ├── /api/*      -> Admin / User Data APIs                  │
│  ├── /static/*   -> Embedded Static Assets (CSS/JS)         │
│  └── /*          -> Go HTML Templates Web Dashboard         │
│                                                             │
│  [ Gateway Core Engines ]                                   │
│  ├── Exact-Match Cache (SHA-256 Key, 15m TTL)               │
│  ├── Sliding-Window Rate Limiter (RPM & TPM)                │
│  ├── Multi-Tenant Model Whitelist Enforcement               │
│  ├── SSE Stream Token Interceptor & Usage Counter           │
│  └── Background Daemons (Key Syncer & Quota Auto-Reactivator)│
│                                                             │
│  [ Persistence: SQLite WAL Mode (data/gateway.db) ]         │
│  └── Users, API Keys, Sessions, Audit Request Logs, Billing │
└──────────────────────────────┬──────────────────────────────┘
                               │
                               │ Loopback HTTP (127.0.0.1:20128)
                               ▼
┌─────────────────────────────────────────────────────────────┐
│                    9ROUTER CORE (Port 20128)                │
│                                                             │
│  • Upstream Provider Pooling (Gemini, Claude, OpenAI)       │
│  • Failover Chains & Model Combos                           │
│  • RTK Prompt Compression & Provider Thinking Controls      │
└──────────────────────────────┬──────────────────────────────┘
                               │ Outbound HTTPS
                               ▼
            Upstream Providers (Google, Anthropic, etc.)
```

---

## 2. Invariants & Security Boundaries

1. **Strict Loopback Binding for 9router Core**:
   - 9router Core (port `20128`) must only listen on `127.0.0.1`.
   - Never expose port 20128 directly to external interfaces or map it to `0.0.0.0:20128` in Docker.
   - All client traffic must traverse AI Manager on port `20129`.

2. **Zero Write Timeout for SSE Streaming**:
   - `http.Server.WriteTimeout` in `cmd/gateway/main.go` must remain `0`. Setting a non-zero write timeout terminates long-running LLM completions prematurely.
   - Streaming SSE responses must be flushed immediately to clients using `http.Flusher`.

3. **Cryptographic Key & Session Tokens**:
   - API keys are created via `handlers.GenerateSecureAPIKey("sk-gw-")` using `crypto/rand`.
   - Sessions are 64-character random hex strings stored server-side in the `sessions` table.
   - Passwords are encrypted using `golang.org/x/crypto/bcrypt` at cost 12.

4. **SQLite Concurrency with WAL Mode**:
   - Connection string pragma: `_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)`.
   - Write-Ahead Logging allows concurrent readers alongside active writers without database locking errors.
   - All SQL queries must use parameterized placeholders (`?`).

---

## 3. Reverse Proxy & SSE Interception Flow

When a client sends a request to `/v1/chat/completions`:

1. **Authentication**: Extract bearer token from `Authorization: Bearer <key>` or `x-api-key`. Verify against `api_keys` and verify associated `users` record is active.
2. **Rate Limiting**: Check sliding window RPM and TPM for both the key and the user.
3. **Quota Check**: For non-admin accounts, verify `user.TokensUsed < user.TokenQuota`.
4. **Cache Check**: If `stream == false`, compute SHA-256 hash of `model + messages + temperature`. If present in cache, return immediately with `X-Cache: HIT`.
5. **Whitelist Verification**: Check requested model against `user.AllowedModels` and `key.AllowedModels`. If disallowed, reject with `403 Forbidden`.
6. **Forwarding & Interception**:
   - If non-streaming: Forward request to upstream 9router Core, read response, extract token usage from `usage` field, deduct from quota, cache response, and record in `request_logs`.
   - If streaming (`stream: true`): Forward request with SSE headers (`text/event-stream`), iterate line-by-line using `bufio.Reader`, immediately flush each chunk to client, inspect `data: {"usage": ...}` for final token count (or compute heuristic count), deduct tokens, and write audit log.

---

## 4. Key Synchronization (`internal/syncer/`)

AI Manager provides seamless integration between its own multi-tenant database (`gateway.db`) and 9router Core's internal database (`data.sqlite`). The background syncer ensures that any API keys created or toggled in AI Manager are reflected in 9router Core's key store without requiring service restarts.

---

## 5. Provider Keeper Daemon (`internal/worker/`)

The provider keeper periodically scans upstream providers. When an account exhausted its daily quota and the quota reset timestamp passes, the daemon automatically pings the provider to verify viability and reactivates it in 9router Core.
