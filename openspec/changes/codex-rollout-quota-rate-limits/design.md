# Design

The Codex hook-side usage extractor already locates the rollout for the current
session by matching `session_meta.payload.cwd` against the hook process working
directory. This change reuses that locator and JSONL scan to read the newest
`payload.rate_limits` attached to `token_count` events.

Each usable window supplies `used_percent`, `window_minutes`, and `resets_at`.
AO stores the window as a quota snapshot with `limit=100`,
`used=used_percent`, and `remaining=100-used_percent`. `windowEnd` is
`resets_at`; `windowStart` is `windowEnd - window_minutes`. `windowName` is
`primary` or `secondary`, so both windows can coexist for the same
harness/account/model.

The hook activity POST gains optional quota snapshots beside the existing usage
delta. Lifecycle persists snapshots only after the normal stale runtime-token
gate accepts the signal. The metrics quota collector remains the read-side
fallback for no-signal rows, but suppresses a harness's static no-signal row
when any exact or estimated snapshot exists for that harness.

Claude Code remains `signalQuality: none`. The basis is scoped to Claude Code:
local transcripts expose per-message token usage only, local CLI help does not
expose a quota/status command, and the authenticated usage surface used by the
TUI remains a future integration path rather than a local machine-readable file.
