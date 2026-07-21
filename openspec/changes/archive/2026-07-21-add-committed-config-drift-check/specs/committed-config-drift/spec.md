## ADDED Requirements

### Requirement: Committed per-project config snapshots at a known path

The system SHALL keep each tracked project's exported config as a
version-controlled snapshot file at a known, stable path (one file per project,
keyed by project identifier). A snapshot's content SHALL be the byte-exact
canonical JSON emitted by `ao project config export <project>`, so that a
snapshot in sync with live config and a fresh export are byte-identical and the
snapshot diffs cleanly under git. The set of snapshot files present at the known
path SHALL be the authoritative set of tracked projects — a project is tracked
if and only if a snapshot file exists for it, with no separate project registry
to keep in agreement.

#### Scenario: A snapshot equals a fresh export of the same project

- **WHEN** a project's committed snapshot is in sync with live config and an
  operator runs `ao project config export <project>`
- **THEN** the export output is byte-for-byte identical to the committed
  snapshot file

#### Scenario: The tracked set is exactly the committed snapshot files

- **WHEN** the drift check enumerates projects to check
- **THEN** it checks exactly the projects that have a snapshot file at the known
  path, and no others

### Requirement: Scheduled drift check surfaces drift without self-healing

The system SHALL provide a drift check that, for every committed project
snapshot, runs `ao project config diff <project> <snapshot>` and aggregates the
results. The check SHALL exit zero when every project's live config matches its
committed snapshot, and SHALL exit nonzero when any project drifts, reusing the
existing `config diff` exit-code contract rather than reimplementing drift
detection. When any project drifts, the check SHALL report which project(s)
drifted and surface the per-project drift detail from `config diff`. The check
SHALL NEVER mutate live config and SHALL NEVER run `config apply` — drift is
surfaced to the operator, never self-healed. The check SHALL run on a schedule
via the repository's existing `ops/` systemd `.service` + `.timer` convention.

#### Scenario: All snapshots in sync exits zero

- **WHEN** the drift check runs and every tracked project's live config matches
  its committed snapshot
- **THEN** the check reports no drift and exits zero

#### Scenario: A drifting project makes the check exit nonzero and names it

- **WHEN** the drift check runs and at least one tracked project's live config
  disagrees with its committed snapshot
- **THEN** the check exits nonzero, names each drifted project, and includes the
  drifted fields reported by `config diff` for those projects

#### Scenario: One project drifting does not suppress checking the others

- **WHEN** the drift check runs across multiple tracked projects and an early
  project drifts
- **THEN** the check still runs `config diff` for every remaining tracked
  project before exiting, so a single drift does not hide additional drift

#### Scenario: The drift check never mutates live config

- **WHEN** the drift check runs, whether or not any project drifts
- **THEN** it invokes only `ao project config diff` (never `apply`) and no
  project's live config is changed

### Requirement: Deliberate snapshot refresh for intended config changes

The system SHALL provide a refresh helper that regenerates a project's committed
snapshot from live config by writing `ao project config export <project>` to the
project's snapshot file at the known path. After a successful refresh, an
immediate drift check for that project SHALL report no drift. The refresh SHALL
be an explicit operator action producing an ordinary git diff of the snapshot
file, so the reviewed baseline only moves when a human commits the refreshed
snapshot.

#### Scenario: Refresh brings a drifted snapshot back in sync

- **WHEN** a project's live config has drifted from its committed snapshot and an
  operator runs the refresh helper for that project
- **THEN** the snapshot file is rewritten to the current canonical export and a
  subsequent `ao project config diff` for that project exits zero

#### Scenario: Refresh of an unchanged project produces no snapshot change

- **WHEN** an operator runs the refresh helper for a project whose live config
  already matches its committed snapshot
- **THEN** the snapshot file content is unchanged (no spurious git diff)
