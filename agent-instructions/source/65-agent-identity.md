<!--
@sx-managed: agent-identity-contract (nickify refreshes marked copies; remove this line to own the file)
Shared contract for what the client-specific identity bodies must define.
-->

## Agent Identity Contract

Shared skills describe process; each agent identity supplies the concrete
mechanics. The client-specific identity body appended after this module defines
how that client invokes skills, spawns subagents, runs independent review,
monitors review cycles, identifies its GitHub login, and invokes OpenSpec flows.

Subagents are selected by capability tier, not by a hardcoded model in shared
skill text:

1. **Lightweight** — classification, triage, monitoring, and narrow checks.
2. **Standard** — reproduction, implementation, and verification work.
3. **Deep reasoning** — root-cause analysis, architecture, and design-only
   planning.

Use subagents for substantial phases when they can advance work without
duplicating the main thread. Keep the immediate critical path local when waiting
would slow the work down, and delegate bounded sidecar tasks with clear file or
responsibility ownership.

Independent review is required before merge readiness. The primary reviewer
must be independent of the implementer and preferably from a different model
family. A PR-integrated bot can supplement the review, but it is never the only
reviewer. If no independent local reviewer is available and authenticated, stop
at the review gate and report the missing roster instead of self-reviewing.

The review monitor watches cycle history for convergence, persistent findings,
and ping-pong. It can be a lightweight subagent or an explicit inline pass, but
the result must be recorded before a PR is declared merge-ready.

Nested agent CLI launches must scrub parent AO session credentials from the
child environment. Use the explicit form below for reviewer, fixer, diagnostic,
or other peer-agent subprocesses:

```bash
env -u AO_SESSION_ID -u AO_RUNTIME_TOKEN -u AO_RUN_FILE <agent-cli> ...
```

The scrub applies only to the child process. Do not unset those variables in the
parent pane; parent hooks still need them to authenticate activity for the
current session.
