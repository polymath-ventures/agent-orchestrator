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

### What `LastActivityAt` actually measures, and why that is the right signal

It is not "time since the last signal". Lifecycle deliberately records the
moment the CURRENT state was entered: `sameActivity` ignores the timestamp so a
stream of same-state repeats does not rewrite `UpdatedAt` or fan out a CDC
event. For an active session it therefore measures **how long the agent has
been active without finishing a turn**.

That is a better signal than raw silence, not a worse one. A healthy agent
transitions active -> idle or waiting_input between turns, so it resets
constantly. An agent blocked on a leaked `curl` never leaves active, and the
clock runs. This is the fact the process-tree approach was trying and failing to
infer.

### Only an ACTIVE session can be wedged

The other states are quiet for good reasons. `idle` means the agent finished.
`waiting_input` and `blocked` are sticky by design — they mean the agent is
paused on the _user_, and an operator routinely leaves one of those overnight.
Warning on them would fire every morning on healthy sessions, and a check that
cries wolf daily is worse than no check, because the operator stops reading it.

An earlier draft reported the state alongside the duration rather than filtering
on it, reasoning that filtering would re-introduce a guess. That reasoning was
wrong: the activity state is the daemon's own record, not an inference. Reading
it is asking the component that owns the fact — the same principle that makes
this check right in the first place.

### The probe is bounded like every other doctor read

`getJSON` raises the HTTP client timeout to the two-minute command timeout,
which is correct for a spawn and wrong here: one slow daemon would stall the
whole report before the `daemon` check that explains it even ran. The call is
wrapped in the same `probeTimeout` doctor uses for its other daemon reads.

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
- **A single legitimate turn longer than the threshold warns** → Accepted. The
  message says "may be wedged" and names how to inspect; a four-hour turn with
  no state change is worth an operator's glance either way.
- **The check adds one daemon request to every `ao doctor` run** → It is a
  single GET on loopback against a listing doctor's own report already depends
  on being reachable.

## Open Questions

None blocking.
