## Context

A project's effective config is `domain.ProjectConfig`
(`backend/internal/domain/projectconfig.go:20`), persisted in the daemon's
SQLite store. The CLI already reads it (`ao project get <id>` → `GET
/api/v1/projects/{id}`, config is one field of the project detail) and writes it
(`ao project set-config <id>` → `PUT /api/v1/projects/{id}/config`, handled by
`Service.SetConfig` at `service/project/service.go:558`, which does
`row.Config = in.Config` — a **full replace**). There is no GET-config or
PATCH/merge route. The CLI carries a hand-maintained struct mirror of the config
(`projectConfig` at `cli/project.go:105`) that is currently a **subset** of the
domain type — it omits `Reviewers`, `WorkerMix`, and `MaxLiveWorkers`.

This change adds `ao project config export|apply|diff` on top of that existing
surface, with no daemon or storage changes.

## Goals / Non-Goals

**Goals:**
- Lossless export of a project's full effective config as canonical, byte-stable
  JSON — independent of the CLI's (incomplete) typed mirror.
- Surgical apply: a spec naming a subset of fields changes exactly those fields
  and nothing else.
- Diff with a drift-signalling exit code, ignoring fields the spec does not name.
- Stay upstream-submittable: pure CLI addition through the shared daemon-client
  helpers; no new endpoint unless forced.

**Non-Goals:**
- The fork-only convenience layer (committed per-project JSON + scheduled drift
  check) — deferred to a separate follow-up commit.
- A daemon-side merge/PATCH endpoint or any storage migration.
- Deep (recursive) field merge — surgical granularity is top-level config keys
  (see Decisions).

## Decisions

### D1 — Operate on config as raw canonical JSON, not the typed CLI mirror

Export, apply, and diff all treat config as JSON (`map[string]any` decoded with
`json.Decoder.UseNumber()`), **not** the `cli/project.go` `projectConfig`
struct. Rationale:

- **Lossless.** The CLI mirror omits `Reviewers`/`WorkerMix`/`MaxLiveWorkers`;
  decoding config through it would silently drop those on export. Capturing the
  daemon's `config` field as `json.RawMessage` and canonicalizing it preserves
  every field the daemon emits, regardless of mirror drift.
- **Correct surgical semantics.** A typed struct can't distinguish "field
  absent" from "field present at zero value" without pervasive pointers;
  `map[string]any` makes "named in the spec" == "key present in the parsed spec
  object," which is exactly the surgical contract.
- **`UseNumber()`** keeps integers (e.g. `maxLiveWorkers`) exact through the
  decode/re-encode round-trip, preserving byte-stability.

*Alternative rejected:* widen the CLI mirror to the full domain type and go
typed. Larger change, reintroduces the absent-vs-zero ambiguity for surgical
apply, and keeps a second copy of the config shape in sync forever (violates
"keep each fact in one place"). The raw-JSON path needs no mirror at all.

### D1a — Command signatures name the project explicitly

`export <project>`, `apply <project> <file>`, `diff <project> <file>`. The
ticket sketches `apply <file>` / `diff <file>`, but apply and diff act on a
specific project's live config and must know which project. The existing
`ao project get <id>` / `set-config <id>` commands take the project explicitly,
so these mirror that convention and keep the exported/spec file **pure config**
with no project identity embedded (which also keeps the export byte-stable and
reusable across projects). Deviation from the ticket's literal shape is noted in
the PR.

### D2 — Canonical form = sorted keys, indented, trailing newline

Export decodes to `map[string]any` and re-marshals with sorted keys (Go sorts
map keys on marshal at every level) and stable indentation. This gives a
tool-independent canonical form so two exports of unchanged config are
byte-identical and diffs under version control are minimal, without depending on
the daemon's struct field-declaration order.

### D3 — Surgical apply = client-side read-modify-write at top-level-key granularity

No merge endpoint exists, so `apply` does: (1) `GET /projects/{id}` and extract
the current `config` as a map; (2) overlay **only the top-level keys present in
the spec file** onto that map (a named key's value fully replaces the live
value); (3) `PUT /projects/{id}/config` with the full merged object. Fields not
named in the spec are carried through untouched from the live config. This
reuses the existing full-replace PUT — the replace is safe because we always
send live-config-plus-overlay, never a partial object.

Granularity is **top-level config keys** (e.g. naming `env` replaces the whole
`env` map). The ticket's contract is "only the fields named in the spec"; the
top-level key is the natural unit of "a field," and deep-merge would add real
complexity for a semantics the ticket doesn't ask for. Recorded as a decision so
apply/diff agree on the same unit.

*Alternative rejected:* add a daemon PATCH/merge endpoint. More surface, an API
contract change, and a spec/parity update — unjustified when a client-side
overlay against the existing PUT is a few lines and keeps the change
upstream-lean (Merit rule).

### D4 — Reporting and exit codes

- `apply` reports the set of top-level keys it changed (comparing overlaid value
  to prior live value); reports zero changes when the spec equals live config.
- `diff` prints each drifted named field with spec value and live value; exits
  zero on no drift, nonzero on drift.
- Bad input (missing project, unreadable/invalid JSON spec) exits `2` as a
  `usageError` per repo convention; daemon/runtime failures exit `1`. `apply`
  and `diff` never mutate on a validation failure.

### D5 — Unknown keys fail loud

The write path already uses `decodeJSONStrict` with `DisallowUnknownFields()`,
so a spec naming a key that isn't a real config field yields a 400 on `apply`.
`apply`/`diff` additionally validate spec top-level keys against the key set of
the freshly exported live config and emit a clear client-side error before any
PUT, so a typo'd field name is reported as such rather than surfacing only as a
raw daemon 400.

## Risks / Trade-offs

- **Read-modify-write race** → a config change between the `GET` and the `PUT`
  of `apply` could be overwritten. Mitigation: this matches the existing
  `set-config` semantics (also last-writer-wins full replace); config edits are
  a low-frequency operator action. Not introducing optimistic concurrency here
  keeps the change lean; noted as a known limitation, not a regression.
- **Top-level-only surgical granularity** → replacing a whole `env` map when the
  operator meant to change one entry. Mitigation: documented in `--help` and the
  spec; the export→edit-one-key→apply workflow makes the replaced unit visible
  in the diff.
- **Canonical form divergence from daemon serialization** → the sorted-key
  export won't byte-match the daemon's struct-order JSON, but that is internal;
  the external contract is export↔export and export↔apply stability, both of
  which the canonical form guarantees.

## Migration Plan

Pure additive CLI change. No storage migration, no API contract change, no
rollback steps beyond reverting the commit. If a future need for atomic apply
arises, a daemon-side merge endpoint can replace the client-side overlay without
changing the CLI contract.

## Open Questions

- None blocking. The fork-only committed-JSON + scheduled-drift-check layer is
  intentionally deferred (separate follow-up), not an open question for this
  change.
