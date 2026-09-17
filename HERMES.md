# HERMES.md - AI Manager (`9router-gateway`) Orchestrator Guide

Guidelines, operational invariants, and architectural context for Hermes Agent acting as autonomous orchestrator and coding agent in AI Manager.

## 1. Orchestrator Role & Responsibilities

Hermes operates as the primary autonomous orchestrator for AI Manager:
- **Task Orchestration**: Plan, decompose, delegate subtasks (`delegate_task`), and coordinate multi-agent execution.
- **Autonomous Fleet Operations**: Supervise and execute automated cron routines (Health Watchdog, CI Guardian, Daily Audit & Auto-Fix, Core Auto-Updater).
- **Incident Recovery & Health**: Auto-diagnose gateway/core issues, verify logs (`sqlite3 data/gateway.db`), and restore services safely.
- **PR Lifecycle Management**: Create feature branches, run build/verification suites, open PRs, and switch back to `master`.

## 2. Strict Invariants & Security Guardrails

- **PR-Only Rule**: Never commit directly to `master`. All changes must go through a feature branch (`git checkout -b <branch>`), push to origin, and open a GitHub PR via `gh pr create`.
- **Core Port Hardening**: 9router Core (`127.0.0.1:20128`) MUST strictly bind to localhost loopback. Never expose port 20128 publicly or across Tailscale.
- **Secret & Domain Privacy**:
  - Never commit API keys (`sk-gw-*`, `sk-*`, Midtrans keys, session secrets).
  - Never leak the upstream 9router Core real domain in commits, PR titles/bodies, documentation, or chat. Use placeholder `https://core.example.com` or `127.0.0.1:20128`.
  - Never include private Telegram IDs, chat IDs, topic IDs, or server credentials in repo documentation.
  - Check diffs before PR creation: `git diff origin/master`.
- **Database Permissions**: `gateway.db*` and `core/db/data.sqlite*` must maintain `0600` permissions.
- **Branch Hygiene**: Switch back to `master` (`git checkout master && git pull --ff-only origin master`) immediately after opening PRs so follow-up work starts from clean `master`.

## 3. Toolchain & Verification Commands

Use Go 1.22+ (`go` or `/usr/local/go/bin/go` depending on host environment):

```bash
# Build binary
go build -v ./...

# Run unit tests
go test -v ./...

# Run race detector tests
go test -race ./...

# Static analysis & formatting
go vet ./...
go fmt ./...

# Rebuild gateway container (after code or embedded template/asset changes)
docker compose build gateway
docker compose up -d --force-recreate gateway

# Health check
curl -s http://127.0.0.1:20129/healthz
```

## 4. Architecture & Code Layout

AI Manager implements Clean Architecture (aligned with `evrone/go-clean-template`):

```
cmd/gateway/main.go                      ← Thin entrypoint, delegates to app.Run()
  └─ internal/app/app.go                 ← Composition root: config, DB, DI, graceful shutdown
       └─ internal/controller/http/      ← Chi router & middleware (package http)
            └─ internal/controller/http/v1/  ← Thin HTTP controllers (package v1)
                 └─ internal/usecase/    ← Pure business logic (Auth, User, Key, Billing, etc.)
                      └─ interfaces.go   ← Store, KeySyncer, SettingsConfig interfaces
                           └─ internal/repository/ ← SQLite implementations of Store (sqlExecutor)
                                └─ internal/database/ ← SQLite WAL connection pool
                                └─ internal/entity/  ← Domain entities
```

Key Infrastructure Modules:
- `internal/proxy/`: Reverse proxy, Circuit Breaker (`circuitbreaker.go`), Model Router & Failovers (`routing.go`), Response Cache (`cached_handler.go`, `cache.go`), Rate Limiter (`ratelimit.go`).
- `internal/upstream/`: 9router Core Client (`core.go`, `mock.go`) and Quota Manager (`quota.go`) with SWR caching.
- `internal/eventbus/`: Async pub-sub event bus (`bus.go`, `subscribers.go`) for key sync, audit logs, and reactive provider reactivation.
- `internal/worker/`: Background workers — `provider_keeper.go` (auto-reactivates accounts on quota reset) and `radar.go` (anomaly radar).
- `web/`: Embedded templates (`web/templates/`) and static files (`web/static/`) via `web/web.go` (`embed.FS`).

## 5. Autonomous Fleet & Cron Orchestration

Fleet cron jobs run autonomously through the gateway (`model: free-only`, `provider: custom` at `http://127.0.0.1:20129/v1`):
1. **Health Watchdog**: Every 30m. Checks container status, `/healthz` endpoints, and database connection.
2. **CI Guardian**: Every 1h. Inspects GitHub Actions runs (`gh run list`), retries stuck runs, detects failure signatures.
3. **Daily Audit & Auto-Fix**: Daily. Executes security and performance sweeps, checks log anomalies, opens fix PRs if needed.
4. **9router Core Auto-Updater**: Every 6h. Checks `GET /api/version` on Core, snapshots settings, pulls image, recreates Core container.

**Standing Grant & Alerting**:
- Fleet jobs deliver alerts to the designated Telegram topic configured in server environment (`TG_CHAT_ID`, `TG_THREAD_ID`). Keep reports clean, direct, without system metadata or job IDs.
- Autonomous fleet jobs have standing permission to diagnose, fix, recreate containers, open PRs, and merge their own fix PRs *only* after CI is green.

## 6. Frontend & UI Conventions

- **Embedded Assets**: Templates and static files are baked into the binary. Any update requires rebuilding the gateway image (`docker compose up -d --build gateway`).
- **Cache Busting**: Bump `custom.css?v=N` and `app.js?v=N` in `web/templates/base.html` and `login.html` upon CSS/JS updates.
- **Template Registration**: Add every new `.html` template to `pages := []string{...}` in `internal/controller/http/v1/handler.go` (`NewHandler`).
- **Datetime Formatting**: Format all timestamps in WIB (`Asia/Jakarta`) using `formatDate` in templates or `formatWIB` in client JS. Never display raw ISO strings.
- **Responsive Tables**: Tables automatically stack into cards on mobile (≤767.98px). Ensure every `<th>` has visible or `.visually-hidden` header text so mobile cards receive proper `data-label` attributes.

## 7. Troubleshooting & Recovery Runbook

- **Upstream 401 Unauthorized**: API key missing or inactive in Core SQLite. Check `data/core/db/data.sqlite` `apiKeys` table and sync with `gateway.db`.
- **502 Bad Gateway**: Core container `9router` is restarting or down. Wait 8s for Next.js boot, check `docker logs 9router --tail 50`.
- **Port Conflict (20129)**: Legacy systemd unit `9router-gateway.service` was accidentally started. Stop and disable it: `systemctl stop 9router-gateway && systemctl disable 9router-gateway`.
- **Audit & Recovery**: Run host audit script to verify service integrity and auto-repair common state issues.
