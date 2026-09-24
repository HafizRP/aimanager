# SYSTEM INVARIANTS (ZERO TOLERANCE) - AI MANAGER (9ROUTER-GATEWAY)
1. NEVER commit API keys, secrets, or credential env vars.
2. Modify ONLY files within the designated module scope.
3. 9router Core (`127.0.0.1:20128`) MUST NEVER be exposed publicly.
4. Generated API keys MUST use prefix `sk-gw-` (e.g. `sk-gw-admin-...`).
5. SQLite queries MUST always use parameterized placeholders (`?`). NEVER concatenate raw strings into queries.
6. Existing tests MUST NOT break (`go test -v ./...` and `go vet ./...` must exit 0).
7. Every final response MUST include a valid JSON codeblock adhering to `contracts/task_output.schema.json`.

