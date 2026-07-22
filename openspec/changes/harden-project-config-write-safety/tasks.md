## 1. Content ETag and atomic config writes

- [x] 1.1 Add failing domain tests ported from reference
  `backend/internal/domain/projectconfig_etag_test.go` for deterministic content
  tokens, the concrete empty token, quoted/weak/list matching, stale rejection,
  and wildcard matching.
- [x] 1.2 Port and FIT `ProjectConfig.ETag` / `ETagMatches` from reference
  `backend/internal/domain/projectconfig_etag.go:11-64`, hashing the current
  struct directly because this tree has no normalization alias.
- [x] 1.3 Add failing service tests ported from reference
  `backend/internal/service/project/setconfig_concurrency_test.go` for current,
  stale, wildcard, and omitted preconditions plus token changes.
- [x] 1.4 Add `SetConfigInput.IfMatch`, `Project.ConfigETag`, the service
  load-compare-write mutex, and `409 PROJECT_CONFIG_STALE` behavior; populate the
  token on project reads.
- [x] 1.5 Add failing controller/API tests for quoted `If-Match`, stale 409,
  successful `ETag` response, and `configETag` project payloads; wire the header
  and generated-spec source definitions.
- [x] 1.6 Add a failing SCM observer regression proving origin backfill cannot
  restore stale config; add a narrow `SetProjectOriginURL` query/store method,
  regenerate sqlc, and replace the broad backfill upsert.

## 2. Safe surgical apply

- [x] 2.1 Add failing CLI tests proving apply sends the ETag from its GET and
  surfaces a stale 409 without claiming success.
- [x] 2.2 Extend the shared CLI HTTP client with request headers and make apply
  carry the read-derived `If-Match` token.
- [x] 2.3 Add failing pure-helper and CLI tests for repeatable
  `--only <field.path>`, safe path grammar, missing-path rejection, nested parent
  creation, unchanged live values, and no-op detection.
- [x] 2.4 Port and FIT the reference dotted-path read/write/clone helpers and
  implement nested-only apply with a read-derived precondition.
- [x] 2.5 Add failing helper tests for absent-equivalent false, numeric zero,
  empty string, object, and array values in overlay; implement the shared
  `omitempty` equivalence rule without hiding a real nonzero-to-zero clear.

## 3. Drift and secret safety

- [x] 3.1 Add failing diff tests showing absent-equivalent spec zero/empty
  values converge and unexpected live zero/empty values are suppressed.
- [x] 3.2 Add failing CLI tests for opt-in unexpected-field drift while
  preserving the current spec-only default; implement the diff option and
  reporting.
- [x] 3.3 Add failing secret-safety tests for the reference denylist, PAT
  boundaries, exact env-key overrides, nested secret-shaped diff leaves, and
  full `env` redaction.
- [x] 3.4 Port and FIT secret-key detection; warn on lossless export to stderr
  and recursively redact secret-shaped diff values.

## 4. Integration, generated contracts, and verification

- [x] 4.1 Regenerate OpenAPI and TypeScript API artifacts with `npm run api` and
  verify the project schema, `If-Match` parameter, ETag behavior, and 409
  response are represented.
- [x] 4.2 Extend real-router/e2e coverage for export/apply concurrency and
  nested restore without touching the typed CLI config mirror.
- [x] 4.3 Update CLI help and project-config documentation for stale-write
  behavior, `--only`, `--unexpected`, absent-equivalent zeros, export warnings,
  and diff redaction.
- [x] 4.4 Verify every history-mined issue-14 lesson is either demonstrated by
  a test/file reference or explicitly rejected as out of scope in the PR body.
- [x] 4.5 Run OpenSpec validation, Go build/test/race/vet, frontend typecheck and
  build, lint, generated-artifact checks, and the repository's full
  `npx @redwoodjs/agent-ci run --all` gate.
