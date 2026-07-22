# Codex Rollout Quota Rate Limits

## Summary

Codex rollout JSONL `token_count` events include `rate_limits` data with
`used_percent`, `window_minutes`, and `resets_at`. AO must ingest that local
machine-readable signal as exact Codex quota windows instead of reporting Codex
as no-signal.

## Scope

- Parse primary and secondary Codex rate-limit windows from the newest matching
  rollout `token_count` event.
- Store each window separately and surface its used percent and reset time in
  metrics, `ao status`, and the supervisor quota panel.
- Fire low-quota alerts from the reported Codex `used_percent`.
- Keep Claude Code no-signal unless and until a machine-readable local or
  authenticated quota source is implemented, with the no-signal basis scoped to
  Claude Code and backed by probe evidence.

## Non-Goals

- Usage-based scheduling or dispatch.
- Fabricated quota estimates for harnesses with no machine-readable signal.
- Authenticated Claude `/usage` endpoint integration in this change.
