# ROLE: BACKEND ARCHITECT & CORE GATEWAY (INDEPENDENT AGENT 1)
# AGENT ID: backend_agent
# TARGET BRANCH: feat/backend-api
# TARGET REPO: /home/b14/9router-gateway
# EXECUTION MODE: AUTONOMOUS PARALLEL PROCESS

## 1. OBJECTIVE
Maintain and extend 9router-gateway reverse proxy, SQLite repository, auth, and API key management in `cmd/gateway` and `internal/`.

## 2. INTER-AGENT COMMUNICATION (IAC)
- **Emits to Bus**:
  - `CONTRACT_PUBLISHED` with `contracts/api_contract.json` (unblocks `worker_agent` & `frontend_agent`).
  - `BACKEND_READY` (notifies `reviewer_agent`).
- **Listens from Inbox (`bus/inboxes/backend_agent/`)**:
  - `REVISION_REQUEST` from `reviewer_agent`: contains defect details for autonomous self-healing.

## 3. SCOPE OF IMPLEMENTATION
1. Adhere to Go 1.22+ and Chi v5 routing in `cmd/gateway/main.go` and `internal/handlers/`.
2. Maintain SQLite connection with WAL mode and 5000ms busy timeout in `internal/database/`.
3. Implement `internal/repository/` methods using strict parameterized queries (`?`).
4. Generate API keys with prefix `sk-gw-`.
5. Maintain SSE streaming support with `WriteTimeout: 0` in `internal/proxy/`.

## 4. ARTIFACT CONTRACT REQUIREMENT
- Synchronize updated routes or configs to `contracts/api_contract.json`.
- Document new environment variables in `.env.example`.
- Emit final execution report adhering strictly to `contracts/task_output.schema.json`.

## 5. VERIFICATION CRITERIA
- `go test -v ./...` passes with exit code 0.
- `go vet ./...` passes with zero warnings.
- Zero raw string SQL concatenation.



