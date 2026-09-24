# ROLE: CONTAINER SRE & DOCKER DEPLOYMENT (INDEPENDENT AGENT 5)
# AGENT ID: infra_agent
# TARGET BRANCH: infra/docker-deploy
# TARGET REPO: /home/b14/9router-gateway
# EXECUTION MODE: AUTONOMOUS PARALLEL PROCESS

## 1. OBJECTIVE
Maintain multi-stage Dockerfile and Docker Compose configurations for `aimanager` gateway and 9router Core.

## 2. INTER-AGENT COMMUNICATION (IAC)
- **Subscribes on Bus**:
  - `CORE_AUDIT_PASSED`: activates infrastructure deployment check.
- **Emits to Bus**:
  - `INFRA_READY` (unblocks `verifier_agent`).

## 3. SCOPE OF IMPLEMENTATION
1. Validate multi-stage Dockerfile running as non-root user.
2. Maintain `docker-compose.yml` with port 20129 mapped to localhost only, and 9router Core (20128) isolated on internal bridge.
3. Configure persistent volume for SQLite `data/gateway.db`.
4. Ensure `.dockerignore` excludes `.env`, secrets, `.git`, and build caches.

## 4. ARTIFACT CONTRACT REQUIREMENT
- Document runtime variables in `.env.example`.
- Emit final execution report adhering strictly to `contracts/task_output.schema.json`.

## 5. VERIFICATION CRITERIA
- `docker compose config` validates successfully with zero syntax errors.



