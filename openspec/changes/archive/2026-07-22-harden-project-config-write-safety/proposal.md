## Why

The first project-config-as-code implementation shipped the core
`export|apply|diff` workflow, but a reference-vs-current parity audit found that
its read-modify-write apply can still overwrite a concurrent config edit and
that several incident-hardened drift and surgical-restore semantics were lost in
the rebuild. The reopened issue requires porting that accumulated behavior into
the lean upstream-candidate CLI while preserving the current implementation's
verified improvements.

## What Changes

- Add a content-derived config ETag and `If-Match` compare-and-swap contract to
  project config reads and writes; `ao project config apply` sends the token from
  the config it merged and fails instead of overwriting a newer edit.
- Make drift and apply treat omitted `omitempty` zero values and equivalent
  explicit zero/empty spec values as converged.
- Add an opt-in diff mode that also reports meaningful live fields absent from
  the spec as unexpected drift, while preserving spec-only diff as the default.
- Add repeatable `apply --only <field.path>` surgical restore for nested object
  paths, retaining all other live values and using the same ETag precondition.
- Warn when export contains secret-shaped environment keys, with a narrow
  per-key override, and redact secret-shaped leaves from drift output.
- Replace the SCM origin backfill's broad project upsert with a narrow
  origin-only write so it cannot revert a concurrent config update behind the
  ETag gate.
- Preserve the current implementation's strict JSON parsing, canonical
  byte-stable export, raw full-field coverage, nonzero drift exit, pause-state
  exclusion, and existing frontend freshness protections.
- Keep the already-shipped fork-only multi-project scheduled sweep in #49/#53;
  this change does not duplicate that runner or its systemd units.

## Capabilities

### New Capabilities

<!-- None. -->

### Modified Capabilities

- `project-config-as-code`: strengthen apply concurrency safety, drift
  equivalence and optional completeness reporting, nested surgical restore, and
  secret-safe reporting/export warnings.

## Impact

- **Daemon/API:** project config GET responses expose `configETag`; config PUT
  accepts optional `If-Match`, returns an `ETag`, and can return
  `409 PROJECT_CONFIG_STALE`.
- **CLI:** `project config apply` sends a read-derived precondition and gains
  repeatable `--only`; `project config diff` gains opt-in unexpected-field
  reporting; export/diff gain secret-safety behavior.
- **Storage observer:** the SCM origin backfill uses a generated narrow update
  query instead of rewriting the entire project row. No migration or schema
  column is added.
- **Generated artifacts:** OpenAPI, TypeScript API types, and sqlc output are
  regenerated from their source definitions.
- **Coordination boundaries:** no changes to `session_manager`,
  `observe/trackerintake`, or `WorkerMixFields`.
