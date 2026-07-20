# Fork Conventions

This repository is the Polymath fork of
[`AgentWrapper/agent-orchestrator`](https://github.com/AgentWrapper/agent-orchestrator).
It replaces the older fork now kept as
[`polymath-ventures/agent-orchestrator-fscked`](https://github.com/polymath-ventures/agent-orchestrator-fscked)
for reference while selected work is ported forward.

The port is tracked from
[`polymath-ventures/agent-orchestrator#1`](https://github.com/polymath-ventures/agent-orchestrator/issues/1).

## Goals

1. Stay rebase-clean on upstream. Upstream moves quickly, so fork changes should
   be easy to sync and reason about.
2. Keep product features upstream-submittable. Structure work so a later
   upstream PR can be assembled mechanically from narrow, conventional commits.

## Porting Rules

1. Branch from `main` for each feature issue.
2. Use conventional commits.
3. Within a feature PR, keep upstreamable changes and fork-only changes in
   separate commits.
4. Label fork-only work `fork-only`.
5. Label upstream-intended work `upstream-candidate`.
6. Reimplement against current upstream code. The old fork is a reference
   implementation, not a cherry-pick source.
7. Clean packages may be copied near-verbatim when they still fit current
   upstream code. Wiring is always rewritten.

Fork-only examples include ops/systemd/Tailscale integration, Codex Fugu
support, and SDLC files. Upstream-candidate work should be narrow enough to
submit directly to upstream after review.

Blacksmith CI runner migration is intentionally out of scope for this bootstrap;
the operator applies that PR directly.

