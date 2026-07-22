## Context

Round 1 implemented project config as code as a first-class Go CLI:
`backend/internal/cli/projectconfigcmd.go` drives a raw-JSON helper in
`projectconfigcode.go`, reads the current project, overlays a partial spec, and
writes the full merged config through `PUT /projects/{id}/config`. The raw JSON
representation is deliberately better than a typed CLI mirror: it preserves
unknown-to-the-CLI fields, distinguishes absent keys from named zero values, and
keeps export canonical and byte-stable.

The remaining safety hole is that apply's GET and PUT are not coupled. A config
edit that lands between them is silently overwritten. The old fork solved this
with a content ETag and `If-Match`, while its config-as-code layer accumulated
additional semantics for `omitempty` convergence, unexpected drift, nested
restore, and secret-safe reporting.

The operator's binding strategy is **PORT → FIT → IMPROVE → FIX**:

- **PORT:** take the old fork's tested semantics and, where the architecture
  matches, its Go source.
- **FIT:** adapt only seams between the old ops script and the current Go CLI,
  and between the old and current config domain types.
- **IMPROVE:** preserve current strict spec parsing, canonical export, raw-field
  coverage, pause exclusion, and frontend freshness behavior.
- **FIX:** add only deltas the reference did not have or that the current
  architecture requires.

### Port manifest

The reference is `/home/orchestrator/agent-orchestrator-fscked`.

| Requirement | Reference source | Current target / fit |
|---|---|---|
| Content ETag | `backend/internal/domain/projectconfig_etag.go:11-64` and `_test.go:12-115` | Add the same primitive under `backend/internal/domain/`; hash current `ProjectConfig` directly because this tree has no `ProjectPrefix` alias or `Normalized()` method. |
| Atomic config write | `backend/internal/service/project/dto.go` (`IfMatch`), `service.go` stale check, `types.go` (`ConfigETag`), `httpd/controllers/projects.go` header wiring | Add `IfMatch`, `ConfigETag`, a config write mutex, stale conflict, request/response headers, and generated API contract. Keep the current `UpsertProject` write path and existing validation. |
| CLI If-Match | `backend/internal/cli/client.go` at commit `8022bf316`; `ops/project-config.mjs:95-102,155-169` | Extend the shared CLI client with per-request headers; make the Go apply command carry the ETag from its existing read. |
| `omitempty` equivalence | `ops/project-config-core.mjs:40-53,138-145` | Port to `projectconfigcode.go`, accounting for Go `json.Number`. |
| Unexpected drift | `ops/project-config-core.mjs:120-155` | Add an opt-in Go diff option; preserve the current spec-only default. |
| Nested `--only` restore | `ops/project-config.mjs:109-153,274-294` | Port safe dotted-path parsing/read/write into the Go raw-JSON helper and expose a repeatable Cobra flag. |
| Secret safety | `ops/project-config-core.mjs:21-34,62-79,338-347` | Port the denylist and narrow env-key override. Warn on lossless export; redact secret-shaped leaves in diff output. |
| Narrow SCM origin write | `f155fabc4` changes to `observe/scm`, `queries/projects.sql`, generated sqlc, and `store/project_store.go` | Replace only the current broad origin-backfill upsert; do not touch tracker intake or session management. |

## Goals / Non-Goals

**Goals:**

- Make read-derived project config writes reject stale bases rather than
  silently clobbering concurrent edits.
- Keep the ETag derived from authoritative config content so no migration,
  version column, or second source of truth is introduced.
- Make hand-authored specs converge with Go `omitempty` serialization.
- Add explicit operator controls for full drift reporting and nested surgical
  restore without changing existing default behavior.
- Prevent drift logs from leaking secret-shaped values and warn before an export
  containing secret-shaped env keys is committed.
- Remove the known broad metadata upsert that could bypass the write-safety
  property by restoring stale config.

**Non-Goals:**

- Replacing the full-replace PUT with PATCH. PATCH cannot distinguish omitted
  from explicitly-cleared value fields and conflicts with the existing
  omit-means-remove recovery contract.
- Porting the old fork's full project-config plane (`Spawnable`, daemon standard
  defaults, model/reviewer changes, session-prefix identity work).
- Changing `ao project set-config` flag-update semantics in this PR; that is a
  separate command surface from config-as-code apply.
- Duplicating the fork-only multi-project runner and timer already shipped in
  #49/#53.
- Touching `session_manager`, `observe/trackerintake`, or `WorkerMixFields`.
- Adding a force-apply flag. The wildcard remains an API primitive for deliberate
  whole-object writers, but config-as-code apply uses the token it just read.

## Decisions

### D1 — Content-derived ETag, no schema version

`ProjectConfig.ETag()` returns a stable SHA-256 hash of the stored config JSON,
or the concrete token `empty` for an unset config. Go marshals struct fields in
declaration order and sorts map keys, so equal configs produce equal tokens.
`ETagMatches` accepts strong or weak quoted tokens, comma-separated alternatives,
and `*`.

The target has no reference-era `ProjectPrefix`/`SessionPrefix` normalization
alias. Hashing the current struct directly is therefore the faithful FIT; adding
an alias or a no-op normalization layer would be code without a target-owned
property.

_Alternative rejected:_ a database version column. It duplicates config state,
requires a migration, creates migration-number coordination with concurrent
work, and gives no stronger guarantee than hashing the authoritative blob.

### D2 — Compare-and-swap at the service write chokepoint

`SetConfigInput` carries an out-of-band `IfMatch` token. `Service.SetConfig`
serializes load → compare → validate → write with a dedicated mutex. If the
caller provided a token that does not match the currently loaded config, the
service returns `409 PROJECT_CONFIG_STALE` with the current token and performs no
write. Callers that omit the header retain backwards-compatible behavior;
successful writes return the new token as both `project.configETag` and a quoted
`ETag` response header.

The CLI apply path always sends the token from the exact GET whose config it
merged. It does not send `*`: a surgical restore is safe only if its preserved
fields still describe the current config.

_Alternative rejected:_ controller-only comparison. Two requests could pass the
check and then interleave in the service, and non-HTTP callers would bypass the
property.

### D3 — Narrow unrelated writes that can otherwise restore stale config

SCM origin backfill currently reads a full project row and writes it back with
`UpsertProject`, including the row's old config. A narrow generated
`SetProjectOriginURL` query updates only the owned field. This is included in
the same PR because an ETag gate is not a real invariant while another routine
can overwrite config outside it.

_Alternative rejected:_ leave the observer bug for a follow-up. That would ship
a detector at one write path while preserving a known bypass of the same
property.

### D4 — `omitempty` zero values converge with absence

Comparison treats absent as equivalent to the zero values Go omits:
`false`, numeric zero, `""`, empty objects, and empty arrays. The rule applies
only when one side is absent; a nonzero live value still differs from a zero
spec value, allowing apply to clear it. Overlay uses the same rule so an
already-converged zero spec does not trigger a pointless PUT.

_Alternative rejected:_ canonicalize by inserting every zero field. That would
require a duplicated schema in the CLI and would turn future config fields into
drift until the mirror was updated.

### D5 — Unexpected drift is opt-in

The current `diff` contract intentionally ignores live fields absent from a
partial spec. A new `--unexpected` flag adds those fields to the report. This
preserves partial-spec and surgical-apply compatibility while giving committed
full snapshots a stricter check. Unexpected zero/empty values are suppressed by
the same absent-equivalence rule.

_Alternative rejected:_ make strict full drift the default. Existing partial
specs would immediately become red and the behavior would contradict the
round-1 canonical spec.

### D6 — `--only` restores safe dotted object paths

`apply --only <field.path>` is repeatable. Each path must match
`[A-Za-z0-9_-]+(\.[A-Za-z0-9_-]+)*`, must not contain `__proto__`,
`prototype`, or `constructor`, and must exist in the spec. The helper clones the
live config, writes only selected spec values into it, compares paths for the
change report, and sends the read-derived ETag.

The existing no-flag apply remains top-level surgical overlay, preserving its
contract and tests.

_Alternative rejected:_ JSON Pointer or a general patch language. Both enlarge
the operator surface and introduce escaping/array semantics not required by the
incident-born use case.

### D7 — Export remains lossless; warnings and reports are secret-safe

Export must remain a restorable source, so it does not redact or delete data.
Before writing stdout it inspects `env` keys with the reference denylist and
prints a warning to stderr for secret-shaped names. A comma-separated
`AO_PROJECT_CONFIG_ALLOW_ENV_KEYS` escape hatch exempts exact known-safe keys.

Diff rendering redacts an entire `env` value and recursively redacts values
whose leaf key is secret-shaped, while still naming the field/path that drifted.
The fork-only refresh helper may retain its broader non-empty-env warning.

_Alternative rejected:_ refuse every export with any env. Env is also a valid
non-secret transport, and a hard refusal would make the lossless diagnostic and
recovery command unusable.

### D8 — History-mined edge-case rulings

1. **Adopt:** content ETag + CAS; do not introduce PATCH.
2. **Defer:** the separate `set-config --field` blind-replace surface needs its
   own read-merge-write fix; config-as-code apply already owns its merge.
3. **Not applicable:** role-scoped spawnability validation belongs to the wider
   config plane, not export/apply/diff.
4. **Not applicable:** harness-specific unattended permission defaults belong to
   spawn validation/defaulting.
5. **Do not touch:** session-prefix identity/replacement behavior is in the
   coordination-reserved session paths.
6. **Defer to #49:** banned-model checks concern committed snapshot content, and
   no snapshots are currently committed.
7. **Adopt:** order-insensitive comparison already exists; add only
   absent-equals-omitted-zero.
8. **Adopt:** denylist + redaction + narrow per-key override, not a safe-key
   allowlist.
9. **Preserve:** frontend build-tree freshness and `Cache-Control: no-store`
   already exist; CAS protects the config write owned here.
10. **Preserve/defer:** `WithDefaults` remains a read overlay; daemon write-side
    standard defaults are outside stored-override config-as-code semantics.

## Risks / Trade-offs

- **[Optional precondition permits legacy writers]** → Keep optional
  `If-Match` for compatibility, but make config-as-code apply always send it and
  expose the contract for other clients to adopt.
- **[Process-local mutex is not cross-daemon locking]** → AO has one daemon
  process owning the loopback API and SQLite store. The service lock closes
  concurrent writes within that authority; the ETag catches stale external
  clients.
- **[Secret detection is heuristic]** → Use a denylist and exact-key override,
  always warn rather than claiming export is safe, and keep actual values out of
  diff output.
- **[Nested restore creates missing parent objects]** → Require the selected
  path in the spec and limit traversal to object keys; the daemon's strict typed
  decoder remains the authoritative shape validator.
- **[Generated artifacts enlarge the diff]** → Regenerate with `npm run api` and
  `npm run sqlc`; never hand-edit generated files.

## Migration Plan

The API change is additive: GET gains `configETag`; PUT accepts an optional
header and adds a possible 409 response. Existing clients remain valid.
There is no database migration. Deploy daemon and CLI together so apply can read
and send the token. Rollback is a code revert; persisted data is unchanged.

## Open Questions

None. The ticket's latest strategy and gap manifest settle the behavior. The
implementation will take the smallest repo-compatible choices recorded above
and document any necessary seam-only deviations in the PR.
