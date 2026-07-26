## 1. The check

- [ ] 1.1 Failing tests in `backend/internal/cli/doctor_sessions_test.go` driving a stub session listing: a live session silent past the threshold warns and names the session and its silence; a recently active one passes; a terminated one never warns; several silent ones are all named; a session with no activity timestamp is skipped.
- [ ] 1.2 Failing tests for degradation: an unreachable daemon reports the signal as unavailable and does not fail; the check issues only GETs and never contacts `supervise.sock`; it invokes no external command.
- [ ] 1.3 Implement `checkWedgedSessions` in `backend/internal/cli/doctor_sessions.go`, reading the existing session listing through `getJSON` and computing silence from `Activity.LastActivityAt` against `deps.Now()`.
- [ ] 1.4 Register the check in `runDoctor`'s Core section.

## 2. Verification

- [ ] 2.1 Mutation-check the new tests: inverting the terminated filter, dropping the zero-timestamp skip, and widening the threshold must each fail a test.
- [ ] 2.2 Run `npm run ci-local` green, then exercise `ao doctor` against a real daemon with a live session and against a stopped daemon, recording both outputs in the PR.
