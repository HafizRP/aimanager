# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [Unreleased] - Branch `improve-security` (compared against `master`)

> **Diff Summary**: 70 files changed, +2,881 insertions, -862 deletions across core runtime, tests, configs, and documentation.

### 🏗️ Architecture & Project Layout (Standard Go Project Layout)
- **Application Entrypoint**: Relocated root `main.go` to standard `cmd/gateway/main.go`.
- **Web Assets**: Relocated `embeds/` directory to standard `web/` (`web/static/css/`, `web/static/js/`, `web/templates/`, and `web/web.go`).
- **Test Scripts**: Relocated and consolidated test verification script to `scripts/verify.sh`.
- **Container Build**: Updated `Dockerfile` build targets to point to `./cmd/gateway`.
- **Architecture Documentation**: Added `docs/ARCHITECTURE.md` detailing system topology, concurrency model, and boundary invariants.

### 🛡️ Security Hardening & Defenses
- **Thread-Safe Upstream Configuration**: Upstream 9router Core URL, master API key, and SQLite path are now managed safely at runtime via `sync.RWMutex` with persistent storage in the SQLite `settings` table.
- **SSRF Hardening**: Validated candidate URLs in upstream connection diagnostics (`/api/upstream/test`) to enforce strict HTTP/HTTPS protocol prefixes.
- **HTTP Security Headers Stack**:
  - Enforced `X-Content-Type-Options: nosniff`.
  - Enforced `X-Frame-Options: DENY`.
  - Enforced `Referrer-Policy: strict-origin-when-cross-origin`.
  - Enforced `Permissions-Policy: geolocation=(), camera=(), microphone=()`.
  - Enforced strict `Content-Security-Policy` covering inline styles, scripts, Midtrans CDN, and Google Fonts.
  - Automatic permanent redirect (HTTP 301) to HTTPS for non-local public traffic.
  - Enforced `Strict-Transport-Security` (HSTS) with 1-year max-age and subdomains preload.
- **CSRF Protection**: Form token injection and double-submit cookie validation across all state-changing endpoints.
- **Brute Force Protection**: IP-based rate limiting on `/login` (max 5 failed attempts per 15 minutes).

### 🧪 Automated Testing Suite (+1,200 lines of tests, 10 packages)
- `internal/config/config_test.go`: Validates default parameters, environment variable overrides, fallback data directory resolution, and concurrent getter/setter thread-safety.
- `internal/database/db_test.go`: Verifies SQLite WAL mode pragma settings, busy timeout, and schema migrations.
- `internal/handlers/handlers_test.go`: Tests safe redirect sanitization, CSRF token validation, template parsing, and upstream setting updates.
- `internal/handlers/helpers_test.go`: Tests cryptographically secure API key generation (`sk-gw-`), bcrypt password hashing, and user quota percentages.
- `internal/proxy/proxy_test.go`: Tests API key extraction priority, client IP resolution (`CF-Connecting-IP`, `X-Forwarded-For`, `X-Real-IP`, `RemoteAddr`), and completion forwarding.
- `internal/proxy/cache_test.go`: Tests SHA-256 exact-match response caching, TTL expiration, cache key hashing, and cache bypass.
- `internal/proxy/ratelimit_test.go`: Tests sliding-window rate limiting for RPM, TPM, and unlimited configurations.
- `internal/repository/repo_test.go`: Full CRUD coverage for users, keys, request logs, cursor pagination, and background session/attempt cleanup.
- `internal/upstream/core_test.go`: Tests dynamic CLI token derivation across multi-path directory hierarchies.
- `internal/billing/midtrans_test.go`: Tests SHA-512 webhook signature verification.

### 🧹 Code Consolidation & Redundancy Elimination
- **Model Whitelist & Authorization**: Centralized duplicate `parseAllowedModels`, `hasWildcard`, and `isModelAllowed` implementations into `internal/models/models.go` with `User.GetAllowedModels()` and `APIKey.GetAllowedModels()`.
- **Client IP Extraction**: Centralized duplicate IP parsing into exported `proxy.GetClientIP(r)`.
- **File Cleanups**:
  - Removed duplicate `configs/gateway.env.example` in favor of root `.env.example`.
  - Removed redundant root `verify.sh` wrapper in favor of direct `scripts/verify.sh` and `make verify`.
  - Removed unused `internal/alerts/telegram.go` dead code package.
  - Removed empty `init/` and `build/` directories.
  - Cleaned up redundant `.gitignore` rules.
  - Removed phantom documentation references to nonexistent `api/openapi.yaml`, `deployments/`, and `init/systemd/`.

### 🛠️ Developer Tooling & AI Readiness
- Added `Makefile` (`dev`, `build`, `test`, `test-race`, `vet`, `fmt`, `verify`, `clean`).
- Added `.air.toml` for live-reload development.
- Added AI assistant instructions: `CLAUDE.md`, `.cursorrules`, `.github/copilot-instructions.md`, and sub-instructions in `.github/instructions/`.
- Added `llms.txt` standard specification and updated `llms.md`.

---

## [1.2.0] - 2026-09-10

### Added
- **Upstream 9router Core Feature Parity**:
  - **Providers Management**: View, toggle, prioritize, and delete upstream AI provider accounts (Gemini, Claude, OpenAI, Kiro, Codex).
  - **Combos & Fallback Chains**: Multi-provider fallback routing (e.g. `main` combo with automatic failover).
  - **Token Saver Engine**: Configure RTK (Reduced Token Kinetic), Caveman prompt compression, and Provider Thinking mode.
  - **Proxy Pools**: Manage and test HTTP/SOCKS5 proxy pools for upstream traffic egress.
  - **Model Aliases**: Dynamic mapping of client-requested model IDs (e.g. `gpt-4o`, `claude-3-5-sonnet`) to specific upstream models.
- **Interactive Chat Playground & CLI Setup Hub**:
  - Multi-session markdown chat playground with syntax highlighting and model switcher (`/chat`).
  - Automated setup commands for Cursor, Claude Code, Cline, Roo Code, and OpenAI SDK (`/cli-tools`).
- **Speed & Latency Benchmark Suite**: Real-time TTFT (Time-to-First-Token) and throughput benchmarking tool with graphical latency charts (`/benchmark`).
- **Provider Keeper Background Daemon**: Automated hourly verification worker that reactivates quota-reset provider accounts.
- **Audit Log Export**: Export request logs to CSV and JSON formats with active search/filter preservation.

---

## [1.1.0] - 2026-09-10

### Added
- **FinOps Cost & Volume Analytics**:
  - Forex-style multi-timeframe dashboard metrics (1H, 4H, 1D, 1W, 1M, ALL).
  - Adaptive candlestick token volume bucketing and FinOps cost savings calculations (USD & IDR).
  - Live polling request logs table (`/api/logs`) with dynamic status badges.
- **Sliding-Window Rate Limiter**: Configurable Requests-Per-Minute (RPM) and Tokens-Per-Minute (TPM) enforcement per user and per API key.
- **Exact-Match Response Caching**: SHA-256 hashed request body cache with 15-minute TTL to reduce redundant upstream LLM costs.
- **Midtrans Payment Gateway Integration**: Automated token package purchases via Midtrans Snap (QRIS, GoPay, BCA/Mandiri/BNI Virtual Accounts) with cryptographic webhook verification.
- **Cursor-Based Log Pagination**: Base64 opaque cursor pagination for scalable log querying over SQLite WAL.

### Security
- **Server-Side Cryptographic Session Store**: Session authentication stored in SQLite with SHA-256 tokens (replaced insecure cookie storage).
- **Anti-Brute Force Protection**: Sliding-window IP rate limiting on `/login` (max 5 failed attempts per 15 minutes).
- **Security Headers & HTTPS Ingress**: Enforced strict `Content-Security-Policy`, `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`, and automatic 301 HTTPS redirection with HSTS for public domains.
- **CSRF Defense**: Double-submit CSRF token validation on all state-changing POST/DELETE requests.
- **SSRF Hardening**: Strict loopback validation for upstream 9router Core communication (bound strictly to `127.0.0.1`).

---

## [1.0.0] - 2026-09-10

### Added
- **Core Reverse Proxy**:
  - OpenAI-compatible endpoints (`/v1/chat/completions`, `/v1/models`).
  - Real-time Server-Sent Events (SSE) streaming with zero-buffering chunk forwarding.
  - Interceptor counting prompt tokens, completion tokens, and reasoning tokens.
- **Multi-Tenant Authentication & Scoping**:
  - Dual-mode authentication: Server-side web sessions for UI, Bearer API keys (`sk-gw-...`) for proxy.
  - Role-Based Access Control (`admin` and `user`).
  - Granular model whitelisting per user and per API key with wildcard support (`*`).
- **Database Layer**: SQLite initialization with WAL mode (`journal_mode=WAL`), 5000ms busy timeout, and automated schema migrations.
- **Key Syncer**: Background daemon synchronizing gateway API keys into 9router Core SQLite database.
- **Web Dashboard**: Modern dark-mode UI with real-time stats, key generation, user management, and automatic client timezone display.
