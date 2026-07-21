## Why

A project's stored configuration currently lives only in the daemon's SQLite
store — it can't be inspected as a whole, versioned, reviewed, or restored as
code. The no-black-boxes principle applied to config: an operator should be able
to export a project's config, keep it under version control, and restore it
surgically after a bad change. On the old fork, config-as-code plus a surgical
apply recovered the fleet from bad config twice in a single day (2026-07-20).
This change ports the lean core of that capability — export / apply / diff — and
deliberately leaves out the ops-reconciler daemon that surrounded it there.

## What Changes

- Add `ao project config export <project>` — emit the project's stored config
  (the set of overrides the daemon persists and serves) as canonical JSON to
  stdout (versionable, diffable, restorable).
- Add `ao project config apply <project> <file>` — **surgical apply**: mutate
  only the top-level fields named in the spec file and leave every other field
  untouched. This is what makes recovery safe — applying a partial spec never
  clobbers unnamed fields.
- Add `ao project config diff <project> <file>` — drift report comparing a spec
  file against live config; prints each drifted field and exits nonzero when they
  disagree (zero when in sync), so it can gate CI or a scheduled check.
- All three commands go through the shared CLI daemon-client helpers and the
  existing project config HTTP surface; no new SQLite access from the CLI, no
  new daemon endpoint, and no new persistent daemon behavior. Surgical apply is a
  client-side read-modify-write over the existing config PUT.

Out of scope (deferred, tracked separately): the fork-only convenience layer —
committing per-project JSON under version control plus a scheduled drift check.
That is a few dozen lines with no reconciler daemon and lands as a separate
follow-up commit once the export format has settled.

## Capabilities

### New Capabilities

- `project-config-as-code`: exporting a project's stored config as canonical
  JSON, applying a partial spec surgically (only named fields change), and
  diffing a spec against live config with a drift-signalling exit code.

### Modified Capabilities

<!-- None — no existing capability's requirements change. This builds on the existing project config get/set surfaces without altering their contracts. -->

## Impact

- **CLI** (`backend`): new `ao project config export|apply|diff` subcommands
  under the existing `ao project config` command tree. CLI-only additions that
  call the daemon through shared client helpers.
- **Daemon HTTP API**: reuses the existing project config read/write surface
  (GET the project, PUT its config) with **no new endpoint**. Surgical apply is
  a client-side read-modify-write over the existing full-replace PUT — see
  design.md.
- **No storage migrations**: no new tables or columns; config shape is unchanged.
- **Upstream-candidate**: the three commands are intended for upstream submission
  and stay independent of fork-only wiring.
