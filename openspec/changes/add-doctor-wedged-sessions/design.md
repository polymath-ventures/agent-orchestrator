## Context

#178 exists because an earlier implementation was built and then removed. The
removed check walked a pane's process tree to find what was holding the
foreground, and it drew the same review finding in four consecutive cycles
because the problem is structural, not an implementation flaw: AO launches every
pane as `sh -c '<agent argv>; exec "${SHELL:-/bin/sh}" -i'`, so `#{pane_pid}` is
a wrapping shell; ppid establishes ancestry rather than foreground ownership, so
a leaked `curl` and a healthy long-lived MCP server present identically; and a
non-interactive `sh -c` enables no job control, so the wrapper, the agent, and
every grandchild share one process group and `tpgid` cannot separate them.

The design constraint that follows is therefore hard: this check must answer the
operator's question without inspecting processes at all.

## Goals / Non-Goals

**Goals:**

- Surface a wedged session from a fact AO already owns.
- Be read-only, cheap, and incapable of failing `ao doctor`.
- Work identically on macOS and Linux because it invokes nothing.

**Non-Goals:**

- Naming an offending pid. That is precisely what could not be determined, and
  the operator does not need it to act.
- Killing or remediating. This is a diagnostic.
- A configurable threshold. Add one when an operator asks; a constant is one
  fewer thing to get wrong, and `daemon-restarts` already sets that precedent.

## Decisions

### Ask the daemon, not the OS

The daemon records `domain.Activity.LastActivityAt` for every session and
already exposes it on the session listing — the same listing the removed check
was fetching before it went off to run `ps`. Scraping external state to infer
something the owning component knows for certain is the defect the removed check
embodied; reading the record directly is the whole fix.

A concrete consequence worth stating: an agent running a legitimate long-lived
background server keeps recording activity, so it never warns. The
process-tree approach could not tell that case from the wedged one at all.

### Warn on silence, do not judge the state

The check reports how long a live session has been silent and what activity
state the daemon holds for it, but it does not gate on that state. An idle
session silent for eight hours and a working session silent for eight hours are
both worth an operator's eye, and they read differently in the output. Filtering
on state here would re-introduce a guess — exactly the failure mode of the
removed check — where reporting the two facts side by side lets the operator
decide.

### A session with no activity timestamp is skipped, not warned about

Silence is measured from a starting point. A session that has never recorded
activity has no such point, and treating "never" as "infinitely silent" would
warn on every freshly created session before its first hook lands. Skipping is
the honest reading.

### Unreachable daemon is unavailable, not unhealthy

`daemon-restarts` already sets this pattern: a probe that cannot be answered
reports the signal as unavailable and passes. Failing `ao doctor` because the
daemon is down would be redundant — the `daemon` check already says so — and
would turn one problem into two.

### Accuracy depends on daemon uptime, and that is acknowledged rather than defended

Daemon downtime silently drops activity hooks; `ao doctor` already filters those
misses as restart-window noise. So a long daemon outage can make a healthy
session look silent. Two things make this acceptable rather than a reason to add
machinery: the threshold is hours, far longer than a restart window, and #181's
persistent-daemon work removes the routine source of those outages. Building a
correction for it here would be a second handler for a cause being fixed
elsewhere.

## Risks / Trade-offs

- **A fixed threshold suits some fleets and not others** → It is a named
  constant with its reasoning recorded, changeable in one place. Make it
  configurable when a real deployment needs a different value, not before.
- **A false positive after a long daemon outage** → Accepted and documented
  above; the threshold is much longer than a restart window, and the message
  reports observed silence rather than asserting the session is broken.
- **The check adds one daemon request to every `ao doctor` run** → It is a
  single GET on loopback against a listing doctor's own report already depends
  on being reachable.

## Open Questions

None blocking.
