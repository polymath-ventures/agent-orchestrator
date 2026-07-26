## Context

Four layers can be stopped independently in AO: the view (browser tab or
Electron window), the daemon, the fleet scheduling state, and the agent
sessions. That layering is correct and load-bearing — `ao-tmux.service` owning
the panes separately is precisely why agents survive a daemon restart. The
interface over it is what is wrong: `ao start` opens the desktop app rather than
starting a daemon, `pause` drains, `pause --hard` kills, `stop` stops only the
daemon, and there is no verb that stops everything.

Everything needed to fix this is already on `ao`'s public CLI: `ao pause --all
[--hard]`, `ao resume --all`, `ao stop` (which already detects `ao.service` and
delegates to `systemctl`), and `ao status` (which already reports fleet pause
state). The gap is composition and naming, not capability. The only pieces `ao`
genuinely does not know about are the sibling units `ao-tmux.service` and
`ao-web.service`.

This fork's fleet runs those three user units; upstream's users run the desktop
app. `aong` has to be honest on both, so environment detection is a requirement
rather than a nicety.

## Goals / Non-Goals

**Goals:**

- A verb set where every verb's name matches its effect, and where a single verb
  stops everything.
- Zero upstream changes: no edits to `ao` commands, the daemon, or the unit
  split.
- Deletable by construction. If upstream adopts the model, the Cobra command
  definitions lift into `ao`'s command tree nearly unchanged and this binary
  goes away.
- Runnable outside this fork's layout, and honest about what it cannot do there.

**Non-Goals:**

- Changing the systemd unit split. It is load-bearing.
- Remote daemon shutdown from the web UI (settled separately; would require
  amending ADR 0001).
- Reimplementing run-file handling, shutdown tokens, systemd MainPID detection,
  or anything else `ao stop` already does.
- A `--json` output mode. Add it when something needs to script `aong`; `ao`
  already has `--json` for the facts that matter.

## Decisions

### Shell out to `ao` rather than import `internal/`

`aong` invokes the `ao` executable as a subprocess. The alternative — importing
`backend/internal/cli` and calling command constructors directly — would be
faster and typed, but it welds the porcelain to internal package boundaries that
upstream rebases move, and it duplicates security-relevant logic (the run-file
shutdown token, `ao.service` MainPID detection) into a second call path that can
drift. Shelling out means `aong` inherits every fix to `ao stop` for free and
cannot diverge from it.

The cost is that `aong` parses nothing and re-prints `ao`'s output. That is
acceptable and is in fact the point: `ao` stays the source of truth for the
facts.

### Resolve `ao` sibling-first, then `PATH`

`aong` and `ao` ship together. Resolving `PATH` first would let a stale `ao`
from an old install service a new `aong`, silently pairing mismatched binaries.
Looking beside `os.Executable()` first makes "the pair that shipped together
stays together" true by construction rather than by operator discipline.

### Detect the environment from loaded units, not from the fork's file layout

The classification is: `systemd` when `systemctl` resolves AND at least one AO
user unit reports a `LoadState` other than `not-found`; `plain` otherwise.
Alternatives rejected:

- Checking for `~/.config/systemd/user/ao.service` on disk — misses units
  installed system-wide or via a package, and sees units that exist but were
  never loaded.
- Assuming systemd on Linux — wrong for a laptop running the desktop app.

Asking the user manager is asking the component that owns the fact.

### `aong start` fails in a `plain` environment instead of supervising

In a `plain` environment there is no service manager to ask, and the honest
options are "run the desktop app" (which is `ao start`) or "run `ao daemon` in
the foreground". Spawning and supervising a background daemon here would be new
logic of `aong`'s own, which is the one thing this design forbids — and a CLI
that half-supervises a daemon is how AO got the incoherent lifecycle in the
first place. Failing with an accurate message is better than a `start` that
silently does nothing, which is the exact defect this change exists to remove.

### No `aong pause`

The original sketch had both `aong pause` ("no kills") and `aong drain`
("today's soft pause, honestly named"). Those are the same command: `ao pause
--all` _is_ the drain. `ao` has no capability that gates new work while leaving
live workers alone — the fleet-pause spec defines soft pause as gate-plus-drain.
Shipping a `pause` verb would therefore mean either aliasing `drain` (which
re-introduces the dishonest name this change removes) or building new daemon
behavior in the porcelain (forbidden, and it belongs in `ao` if it is wanted).
So `aong` has `drain`, `stop-work`, and `resume`, and no `pause`.

If a true non-draining pause is wanted later, it is a daemon capability and an
upstream proposal, not an `aong` feature.

### `aong shutdown` checks daemon reachability before stopping work

`stop-work` against a daemon that is not running fails, which would make
`shutdown` fail on the very common "already mostly down" case. Reading
`ao status --json`'s `state` first and skipping the stop-work step when the
daemon is not `ready` keeps the verb idempotent. This is one field read from a
command `ao` already exposes, not a second source of truth.

Ordering is strict: if stop-work fails, the daemon is not stopped. Leaving live
work with no supervisor is worse than a failed shutdown the operator can retry.

### Testing seam

The Cobra commands take a small `deps` struct holding `LookPath`, a
`RunCommand(ctx, name, args...)` that returns combined output plus exit status,
`Executable()`, and the output writers — the same shape `backend/internal/cli`
already uses for `Deps`. Every requirement in the spec is then testable without
a daemon, a systemd user manager, or an `ao` binary, and the integration-level
truth (that the composed commands are the right ones) is asserted on the
recorded argv.

## Risks / Trade-offs

- **A second binary is a second thing to install, document, and keep in sync**
  → Ship it in the same release artifact as `ao`, resolve `ao` sibling-first so
  a split install is detected rather than silently tolerated, and keep the
  command bodies thin enough that upstream adoption deletes the binary rather
  than forking it.
- **Shelling out loses typed errors and structured output** → Accepted
  deliberately; `ao`'s stdout/stderr and exit code are surfaced verbatim so no
  information is lost, only re-typed.
- **Environment detection can only be verified where it can be run** → Document
  which environments are verified (systemd user units on this fork's fleet;
  `plain` via the no-`systemctl` path) and which are untested, rather than
  claiming portability that was not exercised.
- **`aong` could accrete logic over time and become a second lifecycle
  implementation** → The spec forbids it directly: `aong` may not implement
  behavior `ao` provides. A change that needs new behavior is an upstream `ao`
  proposal.
- **`ao-tmux.service` sets `RefuseManualStop=yes`** → `aong stop` and
  `aong shutdown` deliberately never touch it; only `aong start` names it. Agent
  panes are meant to survive both.

## Open Questions

None blocking. The verb set, language, and binary location are settled in the
issue; the `pause`/`drain` collapse and the `plain`-environment `start` failure
are resolved above.
