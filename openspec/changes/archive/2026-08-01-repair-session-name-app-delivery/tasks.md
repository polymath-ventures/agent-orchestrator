## 1. Spec Repair

- [x] 1.1 Add an OpenSpec delta correcting launch-argument and spawn-readiness requirements.
- [x] 1.2 Archive the delta so canonical `session-naming` matches the implementation.

## 2. Implementation

- [x] 2.1 Add failing tests proving launch-named spawns still issue `/rename`.
- [x] 2.2 Route spawn name delivery through the solicited send guard.
- [x] 2.3 Keep restore/operator rename on unsolicited guard policies.
- [x] 2.4 Reconcile naming comments with the non-load-bearing launch flag contract.

## 3. Verification

- [x] 3.1 Run focused Go tests for session naming, session guard, and Claude adapter comments.
- [x] 3.2 Live-verify Claude Code and Codex app-visible names with an isolated daemon.
- [x] 3.3 Run the full local CI parity gate before pushing.
