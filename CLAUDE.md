# CLAUDE.md - AI Manager (`9router-gateway`)

Context and guidelines for Claude Code CLI when working on this repository.

## Build and Test Commands

```bash
# Build the binary
go build -ldflags="-w -s" -o bin/9router-gateway ./cmd/gateway

# Run unit tests
go test -v ./...

# Run tests with race detector
go test -race ./...

# Run static analysis
go vet ./...

# Format code
go fmt ./...

# Run application locally
go run ./cmd/gateway

# Run application with hot reload / live development
make dev

# Run verification suite (requires running gateway at :20129)
./scripts/verify.sh
```

## Architecture & Codebase Map

AI Manager is an enterprise reverse proxy and management gateway in front of **9router Core** (`127.0.0.1:20128`).

- `cmd/gateway/main.go`: Entry point, Chi router middleware stack, route declarations, graceful shutdown.
- `internal/config/`: Configuration loading with `.env` file parsing and environment variable overrides.
- `internal/database/`: SQLite initialization with WAL mode, busy timeout (5000ms), and automated schema migrations.
- `internal/repository/`: Data layer implementing the `Repository` interface for users, keys, request logs, and server sessions.
- `internal/proxy/`: Reverse proxy handler (`GatewayProxy`), exact-match cache (SHA-256, 15m TTL), sliding window rate limiter, and SSE streaming token interceptor.
- `internal/handlers/`: Web UI and admin handlers, session auth (`RequireAuth`, `RequireAdmin`), password hashing with bcrypt, key generation.
- `internal/models/`: Structs for User, APIKey, RequestLog, Session, TokenPackage, etc.
- `internal/syncer/`: Synchronizes API keys to 9router Core SQLite database.
- `internal/worker/`: Background worker for periodic quota checks and provider reactivation.
- `web/`: Embedded Go templates (`web/templates/`) and static assets (`web/static/`).
- `.env.example`: Configuration template with all environment variable definitions.
- `docker-compose.yml`: Docker Compose stack definition.
- `scripts/`: Verification suite and automation scripts (`scripts/verify.sh`).
- `docs/`: `docs/API.md`, `docs/SETUP.md`, `docs/ARCHITECTURE.md`.

## Key Patterns and Conventions

1. **Go Idioms**:
   - Go 1.22+ style.
   - Use `zerolog` (`github.com/rs/zerolog/log`) for structured logging.
   - Wrap errors with `%w` for error propagation: `fmt.Errorf("context: %w", err)`.
   - Propagate `context.Context` through HTTP requests and repository calls.

2. **Security & Guardrails**:
   - 9router Core (`127.0.0.1:20128`) MUST NEVER be exposed publicly.
   - API keys use prefix `sk-gw-` (e.g. `sk-gw-admin-...`, `sk-gw-user-...`).
   - SQLite queries MUST always use parameterized placeholders (`?`). Never format raw strings into SQL queries.
   - Streaming SSE requests require `WriteTimeout: 0` on `http.Server`.

3. **HTTP Routing**:
   - Router is `github.com/go-chi/chi/v5`.
   - Reverse proxy routes are `/v1` and `/v1/*`.
   - Web UI routes require `h.RequireAuth`.
   - Admin-only management routes require `h.RequireAdmin`.
