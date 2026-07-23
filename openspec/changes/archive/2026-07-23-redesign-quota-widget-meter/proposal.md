# Redesign Quota Widget Meter

## Summary

Redesign the supervisor sidebar quota widget so each quota window reads as a
meter row instead of flat mono text. The used percent and progress bar become
the scannable primary signal, while the dated reset timestamp remains present
but visually secondary.

## Scope

- Replace quota window text rows in `QuotaPanel` with accessible meter rows.
- Add severity styling for normal, warning, and critical quota windows.
- Add a quota track design token for dark and light themes.
- Preserve quota data selection, reset formatting, and probe/error/no-source
  behavior.

## Non-Goals

- No daemon, API, storage, hook, or quota data-model changes.
- No changes to low-quota notification thresholds.
- No new colors beyond the existing accent, warning, and danger tokens plus a
  neutral track token.
