# ROLE: BACKGROUND WORKER & SYNCER (INDEPENDENT AGENT 2)
# AGENT ID: worker_agent
# TARGET BRANCH: feat/worker-engine
# TARGET REPO: /home/b14/9router-gateway
# EXECUTION MODE: AUTONOMOUS PARALLEL PROCESS

## 1. OBJECTIVE
Implement and maintain background quota checks, provider reactivation, and 9router Core key syncing in `internal/worker/` and `internal/syncer/`.

## 2. INTER-AGENT COMMUNICATION (IAC)
- **Subscribes on Bus**:
  - `CONTRACT_PUBLISHED`: wakes up worker agent to ingest `contracts/api_contract.json`.
- **Emits to Bus**:
  - `WORKER_READY` (notifies `reviewer_agent`).
- **Listens from Inbox (`bus/inboxes/worker_agent/`)**:
  - `REVISION_REQUEST` from `reviewer_agent`: receives feedback for defect remediation.

## 3. SCOPE OF IMPLEMENTATION
1. Implement background loops with ticker contexts and graceful shutdown.
2. Synchronize active keys to 9router Core SQLite database via `internal/syncer/`.
3. Handle quota limit calculations without database lock contention.
4. Log all worker operations with `zerolog`.

## 4. ARTIFACT CONTRACT REQUIREMENT
- Document worker schedule or sync settings in `.env.example`.
- Emit final execution report adhering strictly to `contracts/task_output.schema.json`.

## 5. VERIFICATION CRITERIA
- Worker and syncer unit tests pass with exit code 0 (`go test -v ./internal/worker/... ./internal/syncer/...`).
- Safe concurrent SQLite access under WAL mode.



