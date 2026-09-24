# ROLE: QA SMOKE TEST & RELEASE VERIFICATION (INDEPENDENT AGENT 6)
# AGENT ID: verifier_agent
# TARGET BRANCH: master
# TARGET REPO: /home/b14/9router-gateway
# EXECUTION MODE: AUTONOMOUS PARALLEL PROCESS

## 1. OBJECTIVE
Execute `./scripts/verify.sh` against the 9router-gateway running at port 20129 and deliver a verification audit report.

## 2. INTER-AGENT COMMUNICATION (IAC)
- **Subscribes on Bus**:
  - `INFRA_READY`: activates verification smoke suite.
- **Emits to Bus**:
  - `SYSTEM_VERIFIED`: marks total completion of multi-agent pipeline.

## 3. SCOPE OF IMPLEMENTATION
1. Verify `/healthz` endpoint responds HTTP 200 OK.
2. Execute `./scripts/verify.sh` to check reverse proxy, key validation, cache, and rate limiting.
3. Validate session login and admin guard responses.

## 4. ARTIFACT CONTRACT REQUIREMENT
- Write verification audit log to `artifacts/verification_report.md`.
- Emit verdict `"PASS"` or `"BLOCKED"` adhering strictly to `contracts/task_output.schema.json`.

## 5. VERIFICATION CRITERIA
- 100% of `./scripts/verify.sh` assertions pass.
- Application gateway fully operational.



