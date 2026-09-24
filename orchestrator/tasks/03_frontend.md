# ROLE: WEB UI & DASHBOARD TEMPLATES (INDEPENDENT AGENT 3)
# AGENT ID: frontend_agent
# TARGET BRANCH: feat/frontend-ui
# TARGET REPO: /home/b14/9router-gateway
# EXECUTION MODE: AUTONOMOUS PARALLEL PROCESS

## 1. OBJECTIVE
Implement and polish Go HTML templates in `web/templates/` and static assets in `web/static/` for user dashboard and admin backoffice.

## 2. INTER-AGENT COMMUNICATION (IAC)
- **Subscribes on Bus**:
  - `CONTRACT_PUBLISHED`: wakes up frontend agent to consume `contracts/api_contract.json`.
- **Emits to Bus**:
  - `FRONTEND_READY` (notifies `reviewer_agent`).
- **Listens from Inbox (`bus/inboxes/frontend_agent/`)**:
  - `REVISION_REQUEST` from `reviewer_agent`: receives UI/styling/contract defect feedback.

## 3. SCOPE OF IMPLEMENTATION
1. Work within embedded Go template structure (`web/templates/`).
2. Integrate responsive dashboard cards, key generation forms, and token usage tables.
3. Validate session auth and admin guards (`h.RequireAuth`, `h.RequireAdmin`).
4. Prevent XSS by relying on html/template auto-escaping.

## 4. ARTIFACT CONTRACT REQUIREMENT
- Document any updated frontend asset requirements.
- Emit final execution report adhering strictly to `contracts/task_output.schema.json`.

## 5. VERIFICATION CRITERIA
- Zero broken CSS or static asset links.
- Template rendering tests pass without parse errors.



