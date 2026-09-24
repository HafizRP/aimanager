# ARCHITECTURAL CONVENTIONS - AI MANAGER (GO 1.22+)
1. Strict typing, decoupled layers (Handler -> Service -> Repository).
2. Use `zerolog` (`github.com/rs/zerolog/log`) for structured logging.
3. Wrap errors with `%w` for error propagation: `fmt.Errorf("context: %w", err)`.
4. Propagate `context.Context` through HTTP requests and repository calls.
5. SQLite connection uses WAL mode and busy timeout (5000ms).
6. Explicit unit test coverage for every added function/handler (`go test -v ./...`).
7. Graceful handling of network timeouts, deadlocks, and null states.

