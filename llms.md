# AI Manager - LLM Context Document

> This document provides comprehensive, structured context for LLMs and coding agents working with this codebase.

## 1. Project Overview

**AI Manager** (`9router-gateway`) is a Go-based reverse proxy, multi-tenant authentication gateway, FinOps cost analytics tracker, and web administration platform. It is deployed in front of **9router Core** (`127.0.0.1:20128`).

### Primary Purpose
- Route OpenAI-compatible requests (`/v1/chat/completions`, `/v1/models`) to upstream 9router Core
- Enforce granular multi-tenant RBAC (`admin` vs `user`)
- Manage API keys with model-whitelist scoping and sliding window rate limiting (RPM/TPM)
- Intercept token usage from SSE streams and non-streaming responses for real-time quota deduction and WAL audit logging
- Sub-10ms response caching for exact non-streaming queries (15-minute TTL, SHA-256 key)
- FinOps analytics tracking cost savings vs commercial frontier model rates ($5/1M tokens)

### Tech Stack
- **Language**: Go 1.22+ (`go 1.26.0` in `go.mod`)
- **HTTP Router**: Chi v5 (`github.com/go-chi/chi/v5`)
- **Database**: SQLite with WAL mode (`modernc.org/sqlite` pure-Go driver)
- **Templates**: Go standard `html/template` with `embed.FS`
- **Authentication**: Server-side sessions with 64-character hex tokens; bcrypt password hashing (cost 12)
- **Architecture**: Monolithic Go binary, single process, concurrent background daemons

---

## 2. Architecture & Request Flow

```
Clients (Cursor, Claude Code, Cline, Copilot, Python SDKs)
                         │
                         ▼
  AI Manager Gateway (Port 20129 / Public HTTPS)
  ├── 1. Security Headers (CSP, HSTS, X-Frame-Options, CORS)
  ├── 2. Auth Interceptor:
  │      ├── Bearer / x-api-key for /v1/*
  │      └── Session Cookie (gw_session) for Web Dashboard & APIs
  ├── 3. Rate Limiter (Sliding Window RPM & TPM)
  ├── 4. Token Quota Verification (User-level & Key-level)
  ├── 5. Response Cache (SHA-256 Exact Match, 15m TTL)
  ├── 6. Reverse Proxy & SSE Streaming Engine:
  │      ├── Whitelist Filter (User & Key model scopes)
  │      ├── Immediate chunk flushing (http.Flusher)
  │      └── Real-time token usage interceptor
  └── 7. Async SQLite WAL Audit Logger (request_logs)
                         │ (Loopback 127.0.0.1 ONLY)
                         ▼
  9router Core (Port 20128)
  └── Multi-Provider Pooling (Google Gemini, Anthropic, OpenAI, OpenRouter)
```

---

## 3. Key Files & Structure

| File / Directory | Purpose |
|------------------|---------|
| `cmd/gateway/main.go` | Entrypoint, Chi router setup, middleware stack, graceful shutdown |
| `internal/config/config.go` | Configuration loading with `.env` and environment overrides |
| `internal/database/db.go` | SQLite initialization, connection pooling, and schema migrations |
| `internal/repository/repo.go` | SQLite repository implementation for users, keys, logs, sessions, billing |
| `internal/proxy/proxy.go` | Core reverse proxy, auth extraction, quota checks, SSE interceptor |
| `internal/proxy/cache.go` | In-memory response cache with TTL and SHA-256 key hashing |
| `internal/proxy/ratelimit.go` | Sliding window rate limiter for RPM and TPM |
| `internal/handlers/handlers.go` | Web controllers, session auth middleware, crypto helpers |
| `internal/models/models.go` | Core data models (User, APIKey, RequestLog, Session, TokenPackage, Transaction) |
| `internal/syncer/syncer.go` | Background synchronization of API keys to 9router Core SQLite database |
| `internal/worker/provider_keeper.go` | Background daemon periodically reactivating quota-reset upstream nodes |
| `web/` | Embedded HTML templates (`templates/`) and static assets (`static/`) |
| `.env.example` | Configuration file template with all environment variables |
| `docker-compose.yml` | Container and orchestration stack definition |
| `Dockerfile` | Multi-stage Docker container build definition |
| `scripts/verify.sh` | End-to-end integration and verification script |
| `docs/API.md` | API documentation |
| `docs/SETUP.md` | Server deployment and setup guide |
| `docs/ARCHITECTURE.md` | Deep architectural details and invariants |

---

## 4. Database Schema (SQLite WAL)

- `users`: ID, username, name, password_hash, role (`admin` or `user`), token_quota, tokens_used, allowed_models (JSON array or `["*"]`), rate_limit_rpm, rate_limit_tpm, is_active, created_at, updated_at, last_login_at.
- `api_keys`: ID, user_id, key (`sk-gw-...`), name, allowed_models, rate_limit_rpm, is_active, created_at, last_used_at.
- `sessions`: token (64-char hex), user_id, expires_at, created_at.
- `request_logs`: id, user_id, api_key_id, path, method, model, is_stream, prompt_tokens, completion_tokens, total_tokens, status_code, duration_ms, client_ip, error_message, created_at.
- `settings`: key, value, updated_at.
- `login_attempts`: id, ip, created_at.
- `token_packages`: id, name, token_amount, price_idr, is_active, created_at.
- `transactions`: id, user_id, package_id, amount_idr, token_amount, status, payment_type, midtrans_tx_id, snap_token, created_at, updated_at.

---

## 5. API Endpoints

### LLM Client Endpoints (OpenAI-Compatible)
- `POST /v1/chat/completions`: Chat completions with streaming (SSE) and non-streaming support.
- `GET /v1/models`: Returns catalog of models filtered by the user/key allowed models whitelist.

### Public Auth & Health
- `GET /healthz`: Returns `OK` with status 200.
- `GET /readyz`: Pings SQLite DB and returns `READY` with status 200.
- `GET /login`: HTML login view.
- `POST /login`: Session authentication with rate limiting on failed attempts.
- `POST /logout`: Invalidates server-side session.
- `POST /api/webhook/midtrans`: Webhook handler for Midtrans payment notifications.

### Web Dashboard & User APIs (Requires Session Auth)
- `GET /`: Dashboard page with FinOps analytics and usage metrics.
- `GET /api/stats`: Real-time KPI statistics JSON (`total_requests`, `total_tokens`, `cost_saved_usd`).
- `GET /keys`, `POST /keys`, `POST /keys/{id}/toggle`, `POST /keys/{id}/delete`: API key management.
- `GET /logs`, `GET /api/logs`, `GET /api/logs/export`: Request audit logs.
- `GET /models`, `GET /api/models/alias`: Models overview and alias inspection.
- `GET /chat`: Interactive multi-session AI chat playground with SSE streaming.
- `GET /benchmark`, `POST /api/benchmark/run`: Model latency and throughput benchmark.
- `GET /cli-tools`: Ready-to-copy setup instructions for Cursor, Claude Code, Cline, etc.
- `GET /billing`, `POST /api/billing/checkout`: Automated token top-up checkout.

### Admin-Only Endpoints (Requires `admin` Role)
- `GET /users`, `POST /users`, `POST /users/{id}/edit`, `POST /users/{id}/password`, `POST /users/{id}/reset-usage`, `POST /users/{id}/toggle`, `POST /users/{id}/delete`: User management.
- `GET /providers`, `POST /api/providers/*`: Upstream provider management.
- `GET /combos`, `POST /api/combos/*`: Fallback model combos configuration.
- `GET /token-saver`, `POST /api/token-saver/save`: RTK prompt compression & token saving settings.
- `GET /proxy-pools`, `POST /api/proxy-pools/*`: Outbound proxy pool management.
- `POST /settings/upstream`, `GET /api/upstream/test`: 9router Core connection settings.

---

## 6. Critical Operational Rules & Constraints

1. **Localhost Lockdown for 9router Core**:
   - Port `20128` must never be exposed publicly.
   - All external client traffic must pass through port `20129`.
2. **API Key Generation & Prefixes**:
   - API keys are generated with `handlers.GenerateSecureAPIKey("sk-gw-")`.
3. **SSE Streaming Requirements**:
   - `http.Server.WriteTimeout` must remain `0`.
   - Streaming responses must flush chunks immediately via `http.Flusher`.
4. **SQLite Concurrency**:
   - Must use `WAL` mode and busy timeout (5000ms).
   - Use parameterized SQL queries (`?`) for all queries.

---

## 7. Development & Verification Commands

```bash
# Build binary
go build -ldflags="-w -s" -o bin/9router-gateway ./cmd/gateway
# Or with make
make build

# Run application locally
go run ./cmd/gateway

# Run unit tests
go test -v ./...

# Run static analysis
go vet ./...

# Run verification suite (against running server on :20129)
./scripts/verify.sh
```

