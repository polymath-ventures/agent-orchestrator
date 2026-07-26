## Why

Fleet Prime already persists and validates `PrimeSettings.AgentConfig.Permissions`, but operators can only change it through a raw API call. Prime's default unattended mode is useful, but the supported CLI and Settings surfaces need to expose the same permission-mode choice so an operator can deliberately choose prompting or `accept-edits`.

## What Changes

- Add a `--permission` flag to `ao prime enable` and `ao prime set`.
- Add a Prime permission-mode control to global Settings.
- Keep CLI, UI, and API writes pointed at the existing `PrimeSettings.AgentConfig.Permissions` field.
- Preserve the unattended default when no explicit permission is set, and preserve explicit operator choices.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `fleet-prime-settings`: Prime fleet-owned configuration and Prime's API/CLI/Settings control surface now include permission mode.

## Impact

- Backend CLI Prime settings commands and tests.
- Global Settings Prime section and frontend tests.
- OpenSpec `fleet-prime-settings` requirement delta.
