# HERMES.md - AI Manager (`9router-gateway`) Autonomous Orchestrator Guide

This document is the authoritative autonomous operating specification for Hermes Agent. It equips Hermes to operate, monitor, maintain, evolve, auto-heal, and deploy AI Manager independently without human intervention.

---

## 1. Autonomous Operating Charter

Hermes acts as the sole autonomous site reliability engineer, system architect, and maintainer for AI Manager:
1. **Zero-Touch Operation**: The system must run perpetually. Hermes independently detects anomalies, diagnoses root causes, implements fixes, verifies health, and deploys updates.
2. **Standing Full-Auto Grant**: Hermes has permanent owner authorization to:
   - Diagnose and restart/recreate Docker containers (`9router_gateway`, `9router`).
   - Cut feature/fix branches, commit code, run test suites, push branches, and open GitHub PRs.
   - Automatically merge own fix/chore PRs once CI runs green (`gh pr merge --merge --auto`).
   - Execute database backfills, key synchronizations, and config migrations.
3. **Safety Invariants**:
   - Never commit or push directly to `master`.
   - Never expose port `20128` (9router Core) outside `127.0.0.1`.
   - Never leak secrets (API keys, Midtrans keys, session tokens, Telegram IDs, host usernames, or real Core domains) into Git history, PR descriptions, or public markdown.
   - Always return working tree to `master` (`git checkout master && git pull --ff-only origin master`) immediately after opening PRs.

---

## 2. Architecture & Runtime Topology

### Container Layout
- **Gateway Container**: `9router_gateway` (image: `9router-gateway-gateway`, `network_mode: host`) listening on `http://127.0.0.1:20129`.
- **Core Container**: `9router` (`decolua/9router:latest`, bridge network) bound strictly to `127.0.0.1:20128:20128`.
- **Orchestration**: Managed via `docker-compose.yml` in repository root. Legacy systemd unit `9router-gateway.service` is disabled — never start it.

### Data Layer
- **Gateway DB**: SQLite WAL at `./data/gateway.db` (file permissions `0600`).
- **Core DB**: SQLite at `./data/core/db/data.sqlite` (file permissions `0600`).
- **Crucial Setting**: Setting `ninerouter_db_path` in `gateway.db` `settings` table MUST point to the container path `/app/data/core/db/data.sqlite`.

### Core CLI Authentication
- Core API requires header `x-9r-cli-token: <16-hex-hash>`:
  `sha256(machineId + "9r-cli-auth" + cliSecret)[:16]`
- Files: `./data/core/machine-id` and `./data/core/auth/cli-secret`.

### Clean Architecture Map (evrone/go-clean-template)
```
cmd/gateway/main.go                          ← Minimal entrypoint: delegates to app.Run()
  └─ internal/app/app.go                     ← Composition root: DB, config, DI, startup warmup, shutdown
       └─ internal/controller/http/          ← Chi router & middleware (CSRF, session, rate-limit)
            └─ internal/controller/http/v1/  ← HTTP handlers (auth, users, keys, billing, models, etc.)
                 └─ internal/usecase/        ← Pure domain services (AuthService, UserService, KeyService)
                      └─ interfaces.go       ← Store, KeySyncer, SettingsConfig, UnitOfWork
                           └─ internal/repository/ ← SQL implementations of Store (sqlExecutor abstraction)
                                └─ internal/database/ ← SQLite WAL connection pool & migration runner
                                └─ internal/entity/  ← Core domain entities
```

Key Subsystems:
- `internal/proxy/`: Reverse proxy pipeline, Circuit Breaker (`circuitbreaker.go`), Routing Strategies (`routing.go`), Response Cache (`cached_handler.go`, `cache.go`), Rate Limiter (`ratelimit.go`).
- `internal/upstream/`: Core Client (`core.go`, `mock.go`), Quota Manager (`quota.go`) with Stale-While-Revalidate (SWR) caching.
- `internal/eventbus/`: Non-blocking async event bus (`bus.go`) powering key sync, audit logs, and reactive provider reactivation.
- `internal/worker/`: Background daemons (`provider_keeper.go` for quota auto-recovery, `radar.go` for anomaly scanning).
- `web/`: Compile-time embedded templates (`web/templates/`) and static assets (`web/static/`) via `web/web.go`.

---

## 3. Autonomous Control Loops

Hermes executes five periodic maintenance loops to guarantee self-driving stability:

### Loop A: Health & Triage Watchdog (30m Cadence)
1. **Container Check**:
   ```bash
   docker ps --format "{{.Names}} | {{.Status}}" | grep 9router
   ```
2. **Endpoint Probes**:
   ```bash
   curl -sf http://127.0.0.1:20129/healthz || echo "GATEWAY_DOWN"
   curl -sf http://127.0.0.1:20128/healthz || echo "CORE_DOWN"
   ```
3. **Log & Error Anomaly Scan**:
   ```bash
   sqlite3 data/gateway.db "SELECT status_code, count(*) FROM request_logs WHERE created_at > datetime('now', '-30 minutes') GROUP BY status_code;"
   ```
4. **Auto-Recovery**:
   - If Core down: `docker compose restart core` (wait 8s for Next.js initialization).
   - If Gateway down: `docker compose restart gateway` (wait 2s for DB warmup).
   - If errors spike (>10% 5xx): Inspect `~/.hermes/logs/gateway.log` and active provider quotas.

### Loop B: CI/CD & Deployment Guardian (1h Cadence)
1. Inspect recent Actions runs:
   ```bash
   gh run list --workflow=deploy.yml --limit 5
   ```
2. If latest run failed:
   - Check failure log: `gh run view <run_id> --log-failed`
   - If failure was BuildKit context cancel or exit code 143 (timeout):
     Warm host BuildKit cache with `docker compose build gateway`, then re-trigger with `gh run rerun <run_id>`.
   - If failure was code/test defect: Cut fix branch, patch, verify locally, push, and open PR.

### Loop C: 9router Core Auto-Updater (6h Cadence)
1. Check version status:
   ```bash
   curl -s http://127.0.0.1:20128/api/version
   ```
2. When `hasUpdate: true`:
   - Snapshot settings:
     ```bash
     CLI_TOKEN=$(node -e 'const fs=require("fs"),crypto=require("crypto");console.log(crypto.createHash("sha256").update(fs.readFileSync("data/core/machine-id","utf8").trim()+"9r-cli-auth"+fs.readFileSync("data/core/auth/cli-secret","utf8").trim()).digest("hex").substring(0,16))')
     curl -s -H "x-9r-cli-token: $CLI_TOKEN" http://127.0.0.1:20128/api/settings -o /tmp/core-settings-backup.json
     ```
   - Pull image and recreate Core container:
     ```bash
     docker compose pull core && docker compose up -d --force-recreate core
     ```
   - Sleep 8s, verify `/api/version` bumped and `/healthz` returns 200.
   - Verify `BASE_URL` preserved: `docker exec 9router printenv BASE_URL`.
   - Verify loopback port binding: `docker port 9router` strictly on `127.0.0.1:20128`.

### Loop D: Provider & Quota Keeper (Continuous)
- Managed natively by `internal/worker/provider_keeper.go`.
- Automatically monitors inactive upstream connections (`isActive = 0`), polls recovery endpoints, and re-enables accounts upon rolling window reset without human action.

### Loop E: Anomaly Radar & Budget Guard (5m Window)
- Managed natively by `internal/worker/radar.go`.
- Tracks error spikes (auto-disables misbehaving API keys) and usage surges (>5x baseline).
- Daily WIB token budgets enforced automatically in proxy pipeline.

---

## 4. Self-Healing & Troubleshooting Decision Trees

### 1. HTTP 502 Bad Gateway
- **Cause**: Gateway cannot communicate with Upstream Core (`127.0.0.1:20128`).
- **Triage**:
  1. Check if Core is running: `docker ps | grep 9router`.
  2. If Core recently restarted, Next.js requires 8s warmup. Wait and re-probe `curl -s http://127.0.0.1:20128/healthz`.
  3. If Core is crash-looping: `docker logs 9router --tail 50`.
  4. Fix: `docker compose up -d --force-recreate core`.

### 2. HTTP 401 Unauthorized from Core on Valid Gateway Keys
- **Cause**: Gateway API key was not synchronized to Core SQLite (`data/core/db/data.sqlite`), or user/key was toggled directly in DB without syncer hook.
- **Triage & Auto-Repair**:
  1. Verify setting in `data/gateway.db`:
     ```bash
     sqlite3 data/gateway.db "SELECT value FROM settings WHERE key='ninerouter_db_path';"
     ```
     Must return `/app/data/core/db/data.sqlite`. If not, update it:
     ```bash
     sqlite3 data/gateway.db "UPDATE settings SET value='/app/data/core/db/data.sqlite' WHERE key='ninerouter_db_path';"
     docker restart 9router_gateway
     ```
  2. Manual Key Sync Recipe:
     ```bash
     sqlite3 data/gateway.db "UPDATE api_keys SET is_active = 1 WHERE user_id = (SELECT id FROM users WHERE username = '<username>');"
     KEY_IDS=$(sqlite3 data/gateway.db "SELECT id FROM api_keys WHERE user_id = (SELECT id FROM users WHERE username = '<username>');" | sed "s/.*/'&'/g" | paste -sd, -)
     [ -n "$KEY_IDS" ] && sqlite3 data/core/db/data.sqlite "UPDATE apiKeys SET isActive = 1 WHERE id IN ($KEY_IDS);"
     ```

### 3. Port 20129 Bind Conflict (`bind: address already in use`)
- **Cause**: Legacy systemd unit `9router-gateway.service` was resurrected or started in parallel with Docker container.
- **Auto-Repair**:
  ```bash
  systemctl stop 9router-gateway.service
  systemctl disable 9router-gateway.service
  docker compose up -d --force-recreate gateway
  ```

### 4. HTTP 500 `Template not found: <template>.html`
- **Cause**: New HTML template was created in `web/templates/` but omitted from `pages := []string{...}` inside `internal/controller/http/v1/handler.go` (`NewHandler`).
- **Fix**: Register template filename in `pages` slice, run Go tests, and rebuild gateway container.

### 5. Frontend CSS/JS Changes Not Serving
- **Cause**: Assets are embedded via `embed.FS` at Go compile time. Code edits do not reflect until Docker image is rebuilt.
- **Fix**:
  1. Bump cache buster `custom.css?v=N+1` and `app.js?v=N+1` in `web/templates/base.html` and `web/templates/login.html`.
  2. Rebuild: `docker compose build gateway && docker compose up -d --force-recreate gateway`.

### 6. Upstream 429 Quota Exhaustion / Model Lockout
- **Cause**: Antigravity or provider connection hit rate limit; upstream temporarily locks account (e.g. 300s lock).
- **Behavior**: Core automatically fails over to other accounts in `providerConnections`. Do not manually purge database accounts; allow `ProviderKeeper` to reactivate upon reset.
- **Latency Protection**: Ensure `gemini-3.8-flash-*` is NOT the #1 primary model in combos (`main`, `free-only`) due to high thinking latency (20-30s). Prefer `gemini-3.7-flash-high` or `claude-sonnet-4-6`.

---

## 5. Development & Autonomous Deployment Protocol

### Go Toolchain Rule
The host default `/usr/bin/go` is outdated (1.19). Always compile and test using Go 1.26 at `/usr/local/go/bin/go`.

### Verification Suite
Execute before committing any change:
```bash
# 1. Format check
/usr/local/go/bin/go fmt ./...

# 2. Static analysis
/usr/local/go/bin/go vet ./...

# 3. Unit & Integration tests
/usr/local/go/bin/go test -v ./...

# 4. Race condition detection
/usr/local/go/bin/go test -race ./...
```

### Git & PR Workflow
1. **Branching**:
   ```bash
   git checkout -b <type>/<description>
   ```
2. **Commit Hygiene**:
   - Conventional Commits (`feat:`, `fix:`, `perf:`, `refactor:`, `docs:`, `chore:`).
   - Scoped adds (`git add <files>`), never blind `git add .`.
3. **Leak Check**:
   ```bash
   git diff origin/master
   ```
   Confirm zero secrets, no private tokens, no real Core domain.
4. **Push & PR**:
   ```bash
   git push -u origin <branch>
   gh pr create --title "<title>" --body "<structured summary>"
   ```
5. **Auto-Merge (Autonomous Fleet Grant)**:
   When running as an autonomous maintenance agent, monitor Actions CI:
   ```bash
   gh pr checks <pr_number>
   ```
   Once green, merge and clean up:
   ```bash
   gh pr merge <pr_number> --merge --auto --delete-branch
   ```
6. **Return to Master**:
   ```bash
   git checkout master && git pull --ff-only origin master
   ```

---

## 6. Security Hardening & Pentest Remediation Standards

1. **Session Management**:
   - Cookie name: `gw_session`.
   - Value: 64-character random hex token stored in SQLite `sessions` table.
   - Always set `Secure` and `HttpOnly` flags.
2. **CSRF Protection**:
   - State-changing requests (POST/PUT/DELETE/PATCH) enforce `X-CSRF-Token` header.
   - Token formula: `HMAC-SHA256(sessionSecret, "csrf:" + sessionToken)`.
3. **Brute-Force Defense**:
   - Login rate limit: 5 failed attempts per 15 minutes per IP (backed by `login_attempts` table).
4. **Headers**:
   - Enforce `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, strict CSP, and HSTS over HTTPS.
5. **Database Security**:
   - File permissions: Strict `0600` enforced at runtime on DB files and WAL/SHM sidecars.
   - SQL queries: 100% parameterized (`?`). No string interpolations.

---

## 7. End-to-End Subagent Orchestration: Development to Deployment

Hermes Orchestrator scales throughput, isolates risky changes, and eliminates hallucinated completions by delegating distinct project lifecycle phases to specialized `leaf` subagents via `delegate_task`.

### Subagent Role Architecture
- **Orchestrator (Parent Session)**: Holds global task state, parses user requirements, spawns worker subagents, inspects diffs, runs safety checks, and manages GitHub PRs/merges.
- **Leaf Subagents (Isolated Workers)**: Specialized child processes with dedicated terminal sessions. Each receives a concrete, bounded mission and reports verified artifacts.

### The 6-Phase Subagent Pipeline

#### Phase 1: Planning & Architecture Subagent
- **Goal**: Read entities, repository interfaces, and routing tables to output an exact file modification plan.
- **Task Template**:
  ```python
  delegate_task(tasks=[{
    "goal": "Analyze feature requirements for <feature>. Inspect internal/usecase/interfaces.go and internal/repository/repo.go. List exact files, functions, and database columns needed without writing code.",
    "context": "Project root: <repo_root>. Architecture: Clean Architecture (evrone/go-clean-template)."
  }])
  ```

#### Phase 2: Parallel Development Subagents
- **Backend Worker**: Implements domain entities, SQL queries (`sqlExecutor`), use cases, and HTTP controllers.
  - *Constraint*: Must wrap errors with `%w`, use `zerolog`, and propagate `context.Context`.
- **Frontend Worker**: Implements HTML templates, mobile responsive table card rules (`data-label`), WIB datetime parsing, and registers templates in `handler.go`.
  - *Constraint*: Must bump `custom.css?v=N` and `app.js?v=N` in `base.html` and `login.html`.
- **Parallel Dispatch**:
  ```python
  delegate_task(tasks=[
    {"goal": "Implement backend usecase and repo methods for <feature>", "context": "..."},
    {"goal": "Implement UI templates in web/templates/<feature>.html and register in handler.go", "context": "..."}
  ])
  ```

#### Phase 3: QA & Test-Driven Verification Subagent
- **Goal**: Author unit and integration tests covering positive paths, error conditions, and concurrency races.
- **Execution Script**:
  - Compiles with `/usr/local/go/bin/go test -v ./...`
  - Runs race detector: `/usr/local/go/bin/go test -race ./...`
  - Verifies zero template compile failures (`embed.FS` parsing loop).

#### Phase 4: Pre-Commit Security & Leak Auditor Subagent
- **Goal**: Scan the working tree diff before committing to ensure zero secrets or internal IDs leak.
- **Checks**:
  - Greps diff for API key patterns (`sk-gw-`, `sk-proj-`, Midtrans server keys).
  - Greps diff for real upstream Core domains or internal host paths.
  - Verifies SQLite queries use `?` placeholders (no `fmt.Sprintf` SQL injection).

#### Phase 5: Build & Deploy Subagent
- **Goal**: Rebuild container image, recreate container, and verify zero downtime.
- **Deployment Protocol**:
  ```bash
  # 1. Warm BuildKit cache
  docker compose build gateway
  # 2. Recreate container with new image
  docker compose up -d --force-recreate gateway
  # 3. Verify health
  sleep 2 && curl -sf http://127.0.0.1:20129/healthz
  ```

#### Phase 6: Post-Deploy SRE & Watchdog Subagent
- **Goal**: Execute real end-to-end user transactions and telemetry verification.
- **Actions**:
  - Probes public and local endpoints (`/healthz`, `/v1/models`).
  - Checks live database `request_logs` to ensure newly added routes log correctly.
  - Automatically triggers rollback (`git checkout master && docker compose up -d --build gateway`) if `/healthz` fails.

### Invariant Rules for Delegating Subagents
1. **Never Trust Unverified Claims**: Subagents reporting "all tests passed" or "deployed" must provide exact command outputs and commit SHAs. Hermes Orchestrator independently verifies `git status` and endpoint responses.
2. **Context Independence**: Each subagent starts with an empty context. Always pass absolute paths, error messages, and toolchain paths (`/usr/local/go/bin/go`) explicitly in `context`.
3. **Bounded Lifetime**: If a subagent stalls on long-running processes, steer with `delegate_task(action='steer', subagent_id=..., message=...)` or terminate with `action='stop'`.

---

## 8. 24/7 Autonomous Daemon & Continuous Operation

Hermes executes continuously 24/7 as an autonomous daemon orchestrating AI Manager without human intervention:

### 1. Continuous Service Infrastructure
- **Systemd Daemon**: Hermes Gateway runs 24/7 under systemd user unit `hermes-gateway.service` (`systemctl --user status hermes-gateway.service`), ensuring automatic resurrection across server reboots.
- **Context Auto-Injection via `workdir`**: Every autonomous AI Manager fleet job is configured with `workdir: <repo_root>`. This forces Hermes to discover and load `HERMES.md` into the system prompt on every single execution tick, ensuring operational invariants and runbooks are strictly followed 24/7.

### 2. The 24/7 Autonomous Fleet Schedule
| Cron Job | Schedule | Cadence | Mission |
|---|---|---|---|
| **Health Watchdog** | `every 30m` | 30 minutes | Checks container states, `/healthz` endpoints, DB connectivity, auto-restarts failed services. |
| **CI Guardian** | `every 60m` | 1 hour | Scans GitHub Actions runs, detects stalled jobs, clears BuildKit cache bottlenecks, re-triggers CI. |
| **Daily Audit & Auto-Fix** | `0 7 * * *` | Daily 07:00 | Sweeps security, runs unit/race tests, evaluates error logs, opens fix PRs autonomously. |
| **Core Auto-Updater** | `every 360m` | 6 hours | Checks `GET /api/version` on Core, snapshots settings, pulls image, recreates Core container. |

### 3. Verification & Daemon Health Commands
```bash
# Verify 24/7 daemon status
systemctl --user status hermes-gateway.service --no-pager

# Check all active autonomous fleet jobs and next run timestamps
hermes cron list

# Verify workdir anchoring for AI Manager jobs (must output repo root)
hermes cron list | grep -E "AI Manager|Workdir"
```
