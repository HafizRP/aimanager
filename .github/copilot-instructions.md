# AI Manager (`9router-gateway`) - Copilot Instructions

Guidelines and architectural standards for AI assistants and developers working with this codebase.

## 1. System Architecture & Component Boundaries

AI Manager is a high-performance reverse proxy, multi-tenant authentication gateway, FinOps cost tracker, and web administration dashboard deployed in front of **9router Core** (`127.0.0.1:20128`).

```
Clients (Cursor, Claude, Cline, Copilot, Python, SDKs)
                     │
                     ▼
  AI Manager Gateway (Port 20129 / Public HTTPS)
  ├── Auth & RBAC (Server-side Session / Bearer API Key)
  ├── Rate Limiter (Sliding Window RPM & TPM)
  ├── Response Cache (Exact-match SHA-256, 15m TTL)
  ├── Reverse Proxy & SSE Streaming Engine
  │     ├── Scoped Models Whitelisting
  │     ├── Token Usage Interceptor (Prompt + Completion + Reasoning)
  │     └── Async SQLite WAL Audit Logger
  └── Background Daemons
        ├── Key Syncer (SQLite to 9router Core)
        └── Provider Keeper (Auto-reactivate quota-reset upstream nodes)
                     │ (Loopback 127.0.0.1 ONLY)
                     ▼
  9router Core (Port 20128)
  └── Multi-Provider Pooling (Google Gemini, Anthropic, OpenAI, OpenRouter)
```

### Key Directories (Standard Go Project Layout)
- `cmd/gateway/main.go`: Application entrypoint, Chi router setup, middleware stack, graceful shutdown.
- `internal/config/`: Environment variable loader with `.env` file support and defaults.
- `internal/database/`: SQLite initialization with WAL pragma and schema migrations.
- `internal/repository/`: Data access layer for users, API keys, request logs, sessions, billing.
- `internal/proxy/`: Reverse proxy handler, SSE stream interceptor, exact-match cache, rate limiter.
- `internal/handlers/`: HTTP handlers for web UI pages, admin APIs, and Midtrans billing webhooks.
- `internal/models/`: Struct definitions for User, APIKey, RequestLog, Session, TokenPackage, etc.
- `internal/syncer/`: Background synchronization of API keys to 9router Core SQLite database.
- `internal/worker/`: Background provider keeper to auto-reactivate quota-reset upstream providers.
- `web/`: Embedded static assets (`web/static/`) and Go HTML templates (`web/templates/`) per standard layout.
- `.env.example`: Configuration template with all environment variable definitions.
- `docker-compose.yml`: Container and orchestration deployment stack.
- `scripts/`: Verification and operational automation scripts (`scripts/verify.sh`).
- `docs/`: In-depth API reference (`docs/API.md`) and deployment guide (`docs/SETUP.md`).

---

## 2. Critical Security & Architectural Constraints

1. **Localhost Lockdown for 9router Core**:
   - Port `20128` (9router Core) MUST NEVER be exposed to public networks or bound to `0.0.0.0`. It MUST bind strictly to `127.0.0.1`.
   - All external client traffic MUST pass through AI Manager on port `20129`.
2. **API Key Generation & Prefixes**:
   - API keys are generated using cryptographically secure random bytes via `GenerateSecureAPIKey(prefix)`.
   - Standard prefix is `sk-gw-` (e.g., `sk-gw-admin-...`, `sk-gw-user-...`).
3. **SSE Streaming Requirements**:
   - In `cmd/gateway/main.go`, `http.Server.WriteTimeout` MUST be set to `0` because streaming SSE LLM responses are long-lived and cannot have a fixed timeout.
   - Streaming responses must flush chunks immediately using `http.Flusher`.
4. **SQLite WAL Mode & Concurrency**:
   - SQLite MUST use `_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)`.
   - Use parameterized SQL queries (`?`) everywhere. NEVER concatenate user input into SQL strings.
5. **Role Hierarchy**:
   - `admin`: Full access to user management, upstream providers, combos, proxy pools, and raw logs.
   - `user`: Restricted to personal keys, personal logs, personal chat playground, and model whitelist.

---

## 3. Code Style & Go Conventions

- **Go Version**: Go 1.22+ (configured as `go 1.26.0` in `go.mod`).
- **Standard Formatting**: Always adhere to standard `gofmt` and `go vet` clean rules.
- **Structured Logging**: Use `github.com/rs/zerolog/log` for high-performance structured logging. Never use `log.Println` or bare `fmt.Print` for production logs.
- **Error Handling**:
  - Always wrap errors with context using `fmt.Errorf("...: %w", err)`.
  - Handle errors explicitly; do not ignore returned errors unless explicitly marked `_ = ...` with a sound reason.
- **Context Propagation**: Always pass `r.Context()` to repository methods and outbound HTTP requests.

---

## 4. Build, Test, and Verification Commands

```bash
# Build binary
go build -ldflags="-w -s" -o bin/9router-gateway ./cmd/gateway

# Run all unit tests
go test -v ./...

# Run static analysis
go vet ./...

# Format all code
go fmt ./...

# Run verification suite (against running instance on :20129)
./scripts/verify.sh
```

---

## 5. Working with HTML Templates and Static Assets

- Templates are embedded via Go standard `embed.FS` in `web/web.go`.
- Pages inherit from `web/templates/base.html`.
- Dynamic data is passed as a `map[string]interface{}` to `h.render(w, r, "page.html", "base.html", data)`.
- Static CSS/JS files live in `web/static/` and are served under `/static/*`.
