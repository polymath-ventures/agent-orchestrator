## 1. Context Capture Model

- [x] 1.1 Add domain types for session initial context documents and source segments.
- [x] 1.2 Capture exact launch-time prompt and system-prompt segments, including zero-byte consulted sources, before TUI and Chat session launch.
- [x] 1.3 Persist the snapshot durably with the session record and mark legacy reconstruction separately.

## 2. Backend Read Surface

- [x] 2.1 Add a session-manager/service read method for initial context inspection.
- [x] 2.2 Add `GET /api/v1/sessions/{id}/context` with generated OpenAPI and TypeScript schema updates.
- [x] 2.3 Cover exact concatenation, source provenance, empty-source entries, redaction metadata, and legacy reconstruction with backend tests.

## 3. CLI Surface

- [x] 3.1 Add `ao session context <id>` human-readable output.
- [x] 3.2 Add `ao session context <id> --json` output and usage-error handling.
- [x] 3.3 Cover CLI formatting and JSON behavior with focused tests.

## 4. Verification

- [x] 4.1 Validate OpenSpec artifacts.
- [x] 4.2 Run focused backend and CLI tests for the new surface.
- [x] 4.3 Run the repo pre-push gate before pushing.
