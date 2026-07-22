## Tasks

- [x] 1. Add OpenSpec requirements for Codex rollout quota windows and per-window surfaces.
- [x] 2. Add `windowName` to quota snapshots, storage, API schema, CLI, and UI labels.
- [x] 3. Parse Codex `token_count.rate_limits` primary/secondary windows from the existing rollout reader.
- [x] 4. Carry accepted quota snapshots through the activity POST and persist them in lifecycle.
- [x] 5. Suppress stale Codex no-signal rows once exact Codex quota snapshots exist.
- [x] 6. Drive low-quota alerts from Codex `used_percent`.
- [x] 7. Verify with targeted backend tests, generated SQL/API artifacts, frontend quota-panel tests, and OpenSpec validation.
