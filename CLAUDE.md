# Agent instructions — fail-open baseline (Claude)

SessionStart normally injects the current vault rules plus this repository's local context. If that context is absent, use this safety baseline only; do not require repo-local managed instruction fragments.

1. GitHub Issues are the sole durable tracker.
2. Make every mutation in an agent-owned worktree, never the shared checkout.
3. For behavior changes, write a failing test first, then implement and verify the fix.
4. Verify the result with the repository's real checks before claiming success.
5. Use an independent reviewer; do not self-review merge readiness.
6. Never merge without explicit authorization from the user or configured autonomous mode.
