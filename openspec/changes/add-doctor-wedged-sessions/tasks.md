## 1. The check

- [x] 1.1 Failing tests in `backend/internal/cli/doctor_sessions_test.go` driving a stub session listing: an active session past the threshold warns and names the session, its duration, and a next command; a recently transitioned one passes; a session that is idle/waiting_input/blocked/exited never warns; a terminated one never warns; several stuck ones are all named; a session with no activity timestamp is skipped.
- [x] 1.2 Failing tests for degradation: an unreachable daemon reports the signal as unavailable and does not fail; a slow daemon does not stall the report past the probe bound; the check issues only GETs and never contacts `supervise.sock`; it invokes no external command.
- [x] 1.3 Implement `checkWedgedSessions` in `backend/internal/cli/doctor_sessions.go`, reading the existing session listing through `getJSON` under `probeTimeout` and measuring time-in-active-state from `Activity.LastActivityAt` against `deps.Now()`.
- [x] 1.4 Register the check in `runDoctor`'s Core section.

## 2. Verification

- [x] 2.1 Mutation-check the new tests: inverting the terminated filter, inverting the active-state filter, dropping the zero-timestamp skip, widening the threshold, dropping the active-only listing scope, and widening the probe bound must each fail a test.
- [x] 2.2 Run `npm run ci-local` green, then exercise `ao doctor` against a real daemon with a live session and against a stopped daemon, recording both outputs in the PR.
