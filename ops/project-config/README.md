# Committed project config snapshots (fork-only)

This directory holds one committed, version-controlled snapshot of each tracked
project's daemon config, plus the scheduled drift check that compares those
snapshots against live config. It is the thin **fork-only** convenience layer on
top of the `ao project config export|apply|diff` core (issues #14 / #42). See
`openspec/changes/add-committed-config-drift-check` for the full spec.

## What lives here

- `<project>.json` — the canonical JSON emitted by
  `ao project config export <project>`, one file per tracked project.
- The set of `<project>.json` files here **is** the list of tracked projects.
  There is no separate registry: a project is drift-checked if and only if it
  has a snapshot file here. To stop tracking a project, delete its file.

Non-snapshot files (like this README) are ignored by the drift check, which only
looks at `*.json` files whose name is a valid project id.

## ⚠️ Secrets: never commit credentials

An exported config can include an `env` block that carries credentials, and
committing a snapshot puts it in git history **durably** — deleting the file
later does not remove it from history. `ao project config diff` redacts `env`
values in its _output_, but that does not protect a committed snapshot file.

Rules:

- Do **not** commit a snapshot whose `env` (or any field) carries a real
  secret. Keep credentials out of project config, or scrub the snapshot before
  committing it.
- The refresh helper prints a warning when the export it writes carries a
  non-empty `env` block — treat that warning as a prompt to review the file
  before `git add`.
- Review every snapshot diff before committing, exactly as you would any change
  that could leak a secret.

## The scheduled drift check

`ops/config-drift-check.mjs` runs `ao project config diff <project> <snapshot>`
for every snapshot and aggregates the result:

- **exit 0** — every project's live config matches its committed snapshot.
- **exit 1** — at least one project genuinely drifted; each drifted project and
  its drifted fields are printed.
- **exit 2** — no drift, but the check itself hit a setup/infra error for at
  least one project (for example the daemon is down or `ao` is missing).

Drift is **surfaced to the operator, never self-healed** — the check only ever
runs `diff`, never `apply`. Acting on drift (investigate, then either fix live
config or refresh the snapshot) is a deliberate human step.

Run it by hand at any time:

```bash
node ops/config-drift-check.mjs            # check all snapshots
```

`AO_BIN` selects the `ao` binary (default `ao` on `PATH`); the service unit sets
it to `%h/.local/bin/ao`.

## Scheduling on the ops host

The check runs on a schedule via `ops/ao-config-drift.service` (a `Type=oneshot`
unit) driven by `ops/ao-config-drift.timer`, mirroring the `ao-tmux-claim` pair.
Install the units alongside the other AO user units (see
`docs/fork.md` → _Headless Server Standup_), then enable the timer:

```bash
mkdir -p ~/.config/systemd/user
cp ops/ao-config-drift.service \
  ops/ao-config-drift.timer \
  ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now ao-config-drift.timer
```

The service's `ExecStart` runs the runner from the deployed source tree
(`%h/.ao/deploy/current/source/ops/config-drift-check.mjs`), so the committed
snapshots it checks are the ones under that deployment. A nonzero exit is visible
through `systemctl --user status ao-config-drift.service` and the journal.

## Bootstrapping and refreshing snapshots

Create or refresh a project's snapshot from live config (requires the daemon to
be running and the project to exist):

```bash
node ops/config-drift-check.mjs --refresh <project>
```

Refresh writes `ao project config export <project>` to `<project>.json`
atomically. It only rewrites the file when the export actually differs (no
spurious diff) and never touches live config. Then **review and commit** the
changed snapshot (mind the secrets warning above):

```bash
git add ops/project-config/<project>.json
git commit -m "ops: refresh <project> config snapshot"
```

The reviewed baseline only moves when a human commits a refreshed snapshot — so
an intentional config change lands as an ordinary, reviewable diff, and anything
the drift check flags that you did _not_ intend is real drift to investigate.

> **Initial bootstrap:** the first snapshot for each project is created with the
> `--refresh` command above, run once against the live daemon on the ops host and
> committed. It is intentionally not committed here from CI or a dev machine,
> since it must reflect the real deployed config.
