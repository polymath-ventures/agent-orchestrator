## Context

`project-config-as-code` (#14/#42) shipped `ao project config export|apply|diff`:
export emits canonical JSON, diff compares a spec file to live config and exits
nonzero on drift. What is missing is the durable, automatic layer around it — a
committed baseline and a scheduled comparison. The old fork solved this with
`ops/project-config.mjs` plus a drift timer; that grew heavy (a reconciler-ish
surface). This change ports only the essential shape and keeps it small.

The repo already has the two building blocks this needs:

- An `ops/` home for fork-only operational assets, including testable Node
  helpers (`ao-web-server.mjs`) and systemd unit pairs (`ao-tmux.service` +
  `ao-tmux-claim.timer`), all covered by `ops/ao-systemd-units.test.mjs`.
- The `config diff` exit-code contract (zero = in sync, nonzero = drift).

## Goals / Non-Goals

**Goals:**

- A committed, per-project canonical-JSON snapshot at a known path under git.
- A scheduled check that flags drift between each snapshot and live config, using
  the existing `config diff` contract, and surfaces it to the operator.
- A deliberate refresh path so the reviewed baseline moves only via a normal
  committed diff.
- Stay tiny (a few dozen lines of ops glue), reuse existing patterns, add no
  Go/daemon code.

**Non-Goals:**

- No automatic apply/reconcile, no self-healing, no ops-reconciler daemon.
- No new daemon endpoint, no storage change, no changes to the export/apply/diff
  CLI.
- No alerting integrations — the nonzero service exit + journal is the surface;
  richer alerting is the operator's to wire on top.
- Nothing upstream-bound (`fork-only`).

## Decisions

**D1 — Snapshot location: `ops/project-config/<project>.json`, one file per
project.** Keeps fork-only operational state under the existing `ops/` home,
gives each project an independent git diff, and makes the tracked set explicit
and reviewable. _Alternative:_ a single combined file for all projects — rejected
because it couples unrelated projects into one diff and one merge surface.

**D2 — The committed snapshot files ARE the tracked-project registry.** A project
is checked iff `ops/project-config/<project>.json` exists; there is no second
list of "projects to track." _Alternative:_ a separate manifest enumerating
tracked projects — rejected per Merit (two sources that must agree will
eventually disagree); the files on disk are the single source of truth.

**D3 — Drift detection delegates entirely to `ao project config diff`; the runner
never re-implements diffing.** The runner shells out per snapshot and aggregates
exit codes. _Alternative:_ export live config in the runner and JSON-diff it
against the snapshot — rejected because it forks the drift contract into a second
place; keeping one drift definition (the CLI's) is the whole point of reuse.

**D4 — The runner is a small, unit-testable Node script
(`ops/config-drift-check.mjs`), not inline shell in `ExecStart`.** The repo
already tests ops Node helpers, TDD needs the enumeration + aggregation logic to
be exercised directly, and a script keeps the systemd unit trivial. _Alternative:_
a POSIX shell loop in the unit's `ExecStart` — rejected because it is not
unit-testable and hides the aggregation logic in a unit file.

**D5 — One script, two modes: default = check all snapshots; `--refresh
<project>` = rewrite that project's snapshot from a fresh export.** Fewer assets
than a separate refresh script, and both modes share project/path resolution.
The refresh mode only writes the snapshot file; committing it stays a manual,
reviewable step. _Alternative:_ a separate `refresh.mjs` — rejected to hold the
asset count down.

**D6 — Scheduling via a systemd `.service` (`Type=oneshot`) + `.timer` pair under
`ops/`, mirroring `ao-tmux-claim`.** The oneshot runs `config-drift-check.mjs`;
its nonzero exit on drift is visible through `systemctl --user status` and the
journal. The `.timer` uses a conservative cadence (`OnBootSec` warmup +
`OnUnitInactiveSec` interval) that an operator can tune. Covered by
`ao-systemd-units.test.mjs`.

**D7 — `ao` binary resolution is configurable, defaulting to `ao` on PATH
(`AO_BIN` override), set explicitly in the service unit.** Keeps the script host-
agnostic and testable (tests inject a stub `ao`).

## Risks / Trade-offs

- **A nonzero `config diff` exit can mean genuine drift _or_ an infra failure
  (daemon down, `ao` missing).** → The check treats any per-project nonzero exit
  as "needs operator attention" and includes the command's output in the report,
  rather than building error-class heuristics the operator does not need. Both
  cases genuinely warrant a look; conflating them is acceptable and lean. A
  usage/setup error (exit 2) is reported distinctly so a broken invocation is not
  mistaken for drift.
- **The scheduled check requires the daemon to be up** (diff fetches live config
  over HTTP). → On the ops host `ao.service` runs continuously; a daemon-down run
  simply surfaces as attention-worthy nonzero, which is the correct signal.
- **One project drifting must not mask others.** → The runner checks every
  snapshot before exiting and aggregates, rather than failing fast on the first
  drift (covered by a scenario).
- **Snapshot staleness after an intentional config change** looks like drift until
  refreshed. → That is by design: the refresh helper + a reviewed commit is the
  intended, auditable way to move the baseline.

## Migration Plan

- Purely additive: new files under `ops/`, no existing behavior changed, no
  rollback of data. Deploy = commit the assets and enable the timer on the ops
  host (`systemctl --user enable --now ao-config-drift.timer`). Rollback =
  disable the timer; snapshots are inert JSON with no runtime effect.
- Initial snapshots are created by running the refresh mode once per project and
  committing the results.

## Open Questions

- Exact timer cadence (hourly vs a few times daily) — pick a conservative default
  in the unit; operator-tunable, not a blocker.
- Whether to order the timer `After=ao.service` / add a `Requires=` — default to
  no hard dependency (a daemon-down run surfaces as attention-worthy nonzero,
  which is acceptable); revisit only if false pages become noisy.
