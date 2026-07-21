## 1. Canonical-JSON config helper (TDD)

- [x] 1.1 Write failing tests for a client-side config-doc helper: decode config JSON with `UseNumber()`, re-marshal to canonical form (sorted keys, indented, trailing newline); assert two canonicalizations of the same input are byte-identical and that integer fields (e.g. `maxLiveWorkers`) survive the round-trip unchanged.
- [x] 1.2 Write failing tests for the surgical overlay: given a live config map and a spec map naming a subset of top-level keys, the result changes exactly the named keys and preserves all others; and a "which keys changed" report lists exactly the named keys whose value differs from live.
- [x] 1.3 Write failing tests for the diff computation: named keys that match produce no drift; disagreeing named keys are reported with spec vs live values; keys not named in the spec are ignored.
- [x] 1.4 Implement the helper (canonicalize, overlay, changed-keys, diff) in the CLI package to pass 1.1–1.3.

## 2. `ao project config export` (TDD)

- [x] 2.1 Write a failing CLI test (stub-server pattern from `project_test.go`): `export <project>` issues `GET /api/v1/projects/{id}`, prints the config as canonical JSON, and exits zero; a missing project argument exits 2 (usageError) with no daemon call; a daemon 404 surfaces the error and exits nonzero.
- [x] 2.2 Add the `config` sub-command group under `newProjectCommand` (`cli/project.go`) and implement `export`, capturing the project's `config` as `json.RawMessage` (not the typed mirror) so no fields are dropped.
- [x] 2.3 Add a test asserting export output is byte-stable across two runs against unchanged stub config.

## 3. `ao project config apply` (TDD)

- [ ] 3.1 Write a failing CLI test: `apply <project> <file>` with a spec equal to the exported config performs the read-modify-write and reports zero changed fields; a two-field spec results in a `PUT /api/v1/projects/{id}/config` whose body equals live-config-plus-those-two-fields, and reports exactly those two as changed.
- [ ] 3.2 Write a failing test: missing/unreadable/invalid-JSON spec exits 2 and issues no PUT; a spec naming an unknown config key is rejected client-side with a clear error before any PUT.
- [ ] 3.3 Implement `apply`: read spec file, `GET` current config, overlay named top-level keys, validate named keys against the live key set, `PUT` the merged object, print the changed-keys report.

## 4. `ao project config diff` (TDD)

- [ ] 4.1 Write a failing CLI test: `diff <project> <file>` exits zero and prints no drift when named fields match; exits nonzero and names each drifted field (spec vs live) when they disagree; ignores fields not named in the spec; never issues a PUT.
- [ ] 4.2 Implement `diff` using the helper from group 1 and the read path from group 2.

## 5. Integration, docs, and gates

- [ ] 5.1 Add/extend a real-router round-trip test (mirror `dto_drift_e2e_test.go`) covering export→apply round-trip byte-stability against the actual controller + fake project manager, guarding against CLI-mirror/domain-DTO drift.
- [ ] 5.2 Update CLI help text and any `docs/` CLI reference to document `export|apply|diff`, the surgical (top-level-key) apply semantics, and the diff exit-code contract.
- [ ] 5.3 Run the full local gate: `cd backend && go build ./... && go test ./... && go vet ./...`, plus `npm run lint` and format check at repo root; fix any drift/parity test failures introduced by the new subcommands.
