# AI MANAGER (9ROUTER-GATEWAY) - MULTI-AGENT SPECIFICATION
Target Project: `/home/b14/9router-gateway`

## 1. System Philosophy: Independent Autonomous Agents
1. Target Existing Repository: Operates on `/home/b14/9router-gateway` without breaking existing Go/Docker code.
2. Dynamic Workflow Injection: Hermes reads `tasks/*.md`, `mandates/*.md`, and `contracts/api_contract.json` dynamically per execution. Any workflow edits apply immediately.
3. True Parallel Execution: All agents spawn simultaneously at $T=0$.
4. Asynchronous Coordination: Agents synchronize state via the shared Event & Message Bus (`bus/`).
5. Git Sandboxing: Builder agents operate on dedicated branches (`feat/*`, `infra/*`) in the target workspace.

## 2. Independent Agent Fleet
- **`backend_agent`**: Go 1.22+ Chi gateway, reverse proxy (`/v1/*`), SQLite WAL, SSE streaming tokens, API keys (`sk-gw-`).
  - Branch: `feat/backend-api`
  - Emits: `CONTRACT_PUBLISHED`, `BACKEND_READY`.
- **`worker_agent`**: Background quota worker & 9router Core syncer (`internal/worker/`, `internal/syncer/`).
  - Branch: `feat/worker-engine`
  - Emits: `WORKER_READY`.
- **`frontend_agent`**: Web UI templates (`web/templates/`), static assets (`web/static/`), session auth.
  - Branch: `feat/frontend-ui`
  - Emits: `FRONTEND_READY`.
- **`reviewer_agent`**: Autonomous Auditor (`go test -v ./...`, `go vet ./...`, SQLite parameterized query check `?`).
  - Emits: `CORE_AUDIT_PASSED` or sends direct `REVISION_REQUEST` message.
- **`infra_agent`**: Dockerfile & `docker-compose.yml` (Port 20129 Gateway, 20128 Core).
  - Branch: `infra/docker-deploy`
  - Emits: `INFRA_READY`.
- **`verifier_agent`**: QA Verifier executing `./scripts/verify.sh` against port 20129.
  - Emits: `SYSTEM_VERIFIED`.

## 3. Dynamic Workflow Updates
To change agent workflow for Hermes:
- Edit `tasks/*.md` for task goals and scope.
- Edit `mandates/hard_rules.md` for strict guardrails.
- Edit `contracts/api_contract.json` for API definitions.
Hermes automatically ingests these changes on the next run.



