---
description: "Use when writing or editing Go backend code in this repository. Enforces idiomatic Go 1.22+, Chi router patterns, zerolog logging, SQLite WAL queries, and reverse proxy streaming rules."
applyTo: "**/*.go"
---

# Go Backend Guidelines

## 1. Code Style & Conventions
- Adhere strictly to standard `gofmt` and `go vet`.
- Structured logging: Always use zerolog (`github.com/rs/zerolog/log`, e.g. `log.Info()...`, `log.Error().Err(err)...`). Never use bare `fmt.Println` or `log.Println` for application logging.
- Error handling: Always wrap errors with context using `fmt.Errorf("operation failed: %w", err)`.
- Context propagation: Always accept and pass `ctx context.Context` to repository calls and outbound HTTP calls (`http.NewRequestWithContext`).

## 2. Database & Repository Patterns
- Database engine: `modernc.org/sqlite` (pure Go).
- Concurrency: SQLite MUST operate in WAL mode with busy timeout:
  `_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)`.
- Parameterized queries: ALWAYS use `?` placeholders. NEVER concatenate user input or format strings into SQL queries.
- Add schema changes via `migrate(db *sql.DB)` in `internal/database/db.go`.

## 3. Reverse Proxy & Streaming SSE
- Server timeout: Never set `WriteTimeout` on `http.Server` in `cmd/gateway/main.go` because streaming SSE completions are indefinite.
- Immediate flushing: For streaming SSE responses, flush chunks immediately using `http.Flusher`.
- Token calculation: Extract `usage` from the final SSE chunk or JSON response. Fall back to heuristic character-based estimation (`len/4`) if upstream omitted usage.

## 4. Authentication & Security
- Passwords: Hash with `bcrypt` (cost 12) via `handlers.HashPassword`.
- API keys: Generate cryptographically secure keys with prefix `sk-gw-` via `handlers.GenerateSecureAPIKey("sk-gw-")`.
- Session tokens: 64-character random hex strings stored server-side.
- Authorization: Enforce role checks (`RequireAuth`, `RequireAdmin`). Standard users can only view and manage their own resources.
