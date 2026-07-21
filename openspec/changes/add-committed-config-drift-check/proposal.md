## Why

The `project-config-as-code` core (export / apply / diff, #14/#42) makes a
project's config exportable and diffable, but nothing keeps those exports under
version control or notices when live config silently drifts away from the
reviewed, committed state. On the old fork, unnoticed config drift twice broke
the fleet in a single day; the recovery only worked because a committed snapshot
existed to diff and apply against. This change adds the thin fork-only layer that
makes that snapshot durable and the drift check automatic — surfacing drift to
the operator, never self-healing.

## What Changes

- Add a committed, version-controlled snapshot per tracked project: canonical
  JSON produced by `ao project config export <project>`, stored at a known,
  stable path so it diffs cleanly under git.
- Add a scheduled drift check that runs `ao project config diff <project>
  <snapshot>` for every tracked project, reusing the existing diff exit-code
  contract (zero = in sync, nonzero = drift). Drift is **surfaced** (nonzero
  exit + operator-visible report/log); it is never auto-applied.
- Add a refresh helper that regenerates a project's committed snapshot
  (`export > snapshot`) when an intentional config change lands, so the reviewed
  baseline moves deliberately and via a normal diffable commit.
- Wire the scheduled check through the repo's existing `ops/` systemd
  `.service` + `.timer` convention (as used by `ao-tmux-claim`), covered by the
  existing `ops/ao-systemd-units.test.mjs` harness.

Deliberately **out of scope**: no automatic apply/reconcile, no ops-reconciler
daemon, no new daemon endpoint, and no storage change. This layer is built
entirely on the existing #42 CLI surface and is `fork-only` by design — it never
goes upstream.

## Capabilities

### New Capabilities

- `committed-config-drift`: keeping each tracked project's exported config as a
  committed, version-controlled snapshot at a known path, detecting drift
  between that snapshot and live config on a schedule via the existing
  `config diff` exit-code contract, surfacing drift to the operator without
  self-healing, and refreshing the snapshot deliberately when an intended
  config change lands.

### Modified Capabilities

<!-- None. This layer consumes the project-config-as-code CLI surface unchanged;
it does not alter any export/apply/diff requirement. -->

## Impact

- **New ops assets** under `ops/`: a drift-check runner (small, testable mjs in
  keeping with `ao-web-server.mjs`), a systemd `.service` + `.timer` pair, and a
  refresh helper. No Go/daemon code changes.
- **New committed snapshot location**: a known per-project path (e.g.
  `ops/project-config/<project>.json`) tracked in git.
- **Depends on** the merged `project-config-as-code` CLI (`ao project config
  export|diff`, #14/#42); consumes its exit-code contract, adds no new flags to
  it.
- **Tests**: extends `ops/ao-systemd-units.test.mjs` and adds unit coverage for
  the drift-check runner's project enumeration and exit-code aggregation.
- `fork-only` — carries the `fork-only` label; excluded from upstream PRs.
