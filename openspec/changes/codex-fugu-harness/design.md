## Context

`codex-fugu` is a Polymath-internal wrapper around the Codex CLI: same flags, same
subcommands, same hook protocol, same activity semantics, different binary and
different model namespace. It shipped on the old fork
(`~/agent-orchestrator-fscked`, commits `821f9679d`, `ebfd1e28c`, `71832a3d0`,
`708f5cc8d`) and is being ported forward.

The clean fork has moved since. Three deltas matter:

1. **Harness count.** Upstream now ships 23 harnesses, and the conventions for
   adding one are settled — a hand-maintained slice in `registry.Constructors()`,
   a constant plus an `AllHarnesses` entry, and a handful of enumeration lists that
   must stay in sync.
2. **Migration convention.** This fork's `0007_allow_implemented_harnesses.sql`
   carried a header note saying _"New harnesses are added here by extending this
   list, not by chaining a fresh per-harness migration."_ That note only held while
   0007 was the newest migration; with two dozen later migrations it no longer does
   (see the Decisions section), so this change adds a new migration — landing in the
   same place the old fork did (a chained `0023`), for a reason the note did not
   anticipate.
3. **No model machinery at all.** This fork has no `domain/modelprovider.go`, no
   `ClassifyModelProvider`, no `knownModelsForHarness`, no `standardModelFor`, and
   no worker mix. Model is an opaque passthrough string on the agent config. Roughly
   half of what the old fork's fugu support consisted of has nothing here to attach
   to.

## Goals / Non-Goals

**Goals:**

- `codex-fugu` spawnable as a worker from the CLI and the UI on a host that has the
  binary, with working session restore, hooks, activity reporting, and auth status.
- Registered in every enumeration this fork keys on, so GH #3 can accept it as a
  worker-mix bucket harness without further backend work.
- Zero behavior change for the existing `codex` harness.
- Kept small enough to stay rebase-clean against upstream, since this is a permanent
  fork-only carry.

**Non-Goals:**

- Model-provider classification (`ProviderFugu`, `ClassifyModelProvider`) — GH #4.
- A model catalog listing `fugu` / `fugu-ultra` — GH #4.
- Worker-mix wiring and `agent:fugu` routing labels — GH #3, in flight.
- Widening the reviewer-harness vocabulary. Fugu is a worker harness.
- Upstream submission. This is fork-only, permanently.

## Decisions

### Parameterize the Codex adapter; do not fork it

The fugu harness is served by the _same_ `codex.Plugin` type, carrying five optional
string fields (manifest id / name / description, binary name, hook token). Each is
read through an accessor that returns the Codex default when the field is empty, so
`codex.New()` returns a zero-valued struct that is provably still plain Codex.

_Alternative rejected:_ a separate `codexfugu` package embedding or wrapping the
Codex adapter. That is how most harnesses are added here, and it would have matched
the surrounding pattern — but fugu is not a different agent, it is the same agent
behind a different executable name. A second package would duplicate the launch
flags, hook installation, restore logic, and activity derivation, and every future
Codex change would have to be mirrored by hand or silently drift. The empty-string
fallback keeps one implementation and makes the Codex defaults a property of the
type rather than a thing two files have to agree on.

_Cost accepted:_ the Codex adapter gains five fields and five three-line accessors
that only one constructor populates.

### `--no-update` is emitted positionally, at the front

The fugu wrapper parses `--no-update` only as a top-level flag; behind a subcommand
it is rejected. So the flag is inserted immediately after the binary in both command
builders (launch and restore) rather than appended with the other flags. This is
load-bearing and non-obvious, which is why the spec pins argument _position_ and the
tests assert exact argv rather than set membership.

Note this fork has no `exec`-style model/capability probe — the old fork's
`ValidateModel` path does not exist here, and `DoctorLaunchProbes()` is a
package-level function used only for the deep Codex smoke test. It stays
Codex-only; fugu's doctor coverage is the ordinary `--version` check. Do not confuse
this flag with the pre-existing `appendNoUpdateCheckFlag`, which emits the unrelated
Codex config override `-c check_for_update_on_startup=false` and applies to both.

_Alternative rejected:_ setting an env var or a config key to disable the update
check. The wrapper offers no such knob; the flag is the only documented mechanism.

### Auth falls back to the shared Codex login, narrowly

`codex-fugu login status` does not work — fugu has no credential of its own and the
wrapper errors with `--profile only applies …`. The fallback to probing `codex
login status` is gated on **that specific error string**, not on "fugu auth probe
failed", so an unrelated failure (missing binary, crash, timeout) still surfaces as
itself.

The fallback deliberately does **not** infer authorization from a clean exit. A
runtime help dump exits zero and says nothing about login state; treating that as
authorized would report a broken worker as healthy. Unknown stays unknown. The old
fork learned this the hard way and pinned it with a dedicated negative test
(`TestFuguAuthStatusDoesNotTreatRuntimeHelpAsAuthorized`); that test ports across.

_Alternative rejected:_ reading the shared credential file directly. It is faster
and needs no subprocess, but it duplicates Codex's own notion of where credentials
live and would break the moment Codex changes it. Asking the Codex binary keeps one
source of truth.

### Widen the harness allowlist with a new migration, not an in-place edit of 0007

`0007`'s header note says to add new harnesses by extending its list in place. That
is only safe **while 0007 is the newest migration**. It no longer is: `0007` was
born with all 23 harnesses in one commit, no harness has ever been added post-hoc,
and there are now two dozen later migrations. `migrate()` runs `goose.Up`, and goose
tracks applied migrations by version _number_, not content — so any database already
past `0007` (every existing install) never re-runs it. Editing `0007` in place would
admit `codex-fugu` on fresh installs but silently leave every existing install
rejecting it, because its `sessions.harness` CHECK was frozen at migration time.

So the widening is a new migration, `0027_allow_codex_fugu_harness.sql`, whose Up
rewrites the current 23-harness CHECK to add `codex-fugu` (Down reverses). `0007` is
left byte-identical to `main` except for a corrected header note pointing future
additions at a new migration.

This is why `migrate_test.go`'s existing fresh-migration guard was not enough: it
opens a fresh DB and runs every migration, so an in-place `0007` edit looks fine
there. A second test, `TestMigrateAdmitsCodexFuguOnUpgradeFromInitialPlatform`,
migrates only through `0007`, asserts `codex-fugu` is absent, then runs the rest and
requires it to be admitted — the upgrade path a fresh-migration test cannot see.

_Alternative rejected:_ editing `0007` in place, per its header note. Rejected once
the goose-version-tracking behavior was confirmed: it is correct for fresh installs
and silently broken for every existing one — the header note predates the later
migrations and no longer holds. (This came out of independent review; the first
draft did edit `0007` in place. The old fork reached the same new-migration
conclusion, chaining its own.)

### Register the harness on functional surfaces, not marketing ones

`codex-fugu` is added to the CLI `--harness` help, the `spawn.md` skill asset, the
spawn API enum, the frontend `AGENT_OPTIONS` fallback list, and the `AgentProvider`
union — all surfaces an operator or the app actually reads to select and route a
harness.

It is deliberately **not** added to `README.md`'s agent badge row or
`LandingAgentsBar.tsx`. Those are public marketing surfaces for the upstream
product, and `codex-fugu` is a Polymath-internal binary nobody outside the fleet can
install; listing it there would advertise something unobtainable. They are also
high-churn files upstream, so staying off them keeps the permanent fork divergence
smaller. This is a departure from the old fork, which did edit the docs pages.

### Do not build an enforcement mechanism for the `fugu-ultra` manual-only ruling

The originating issue's third acceptance criterion is that `fugu-ultra` is never
selected by mix or intake — explicit spawn only. In this fork that is already true,
vacuously: there is no worker mix, no intake defaulting, and no per-harness model
pin. There is nothing that _could_ select it.

Adding an exclusion list, a deny-set, or a `manualOnly` flag now would mean building
a guard over machinery that does not exist, and shipping a rule in the wrong layer —
the component that owns model selection should own the constraint, and that
component arrives with GH #3 / GH #4. The old fork reached the same conclusion for a
different reason: even there, `fugu-ultra` was excluded by _absence_ (never pinned as
a default, never captured into a config spec, and documented as a policy) rather
than by a code flag; `grep manualOnly` on the old fork finds only comments.

So the ruling is recorded here as a constraint on the future work, and preserved
today by construction. GH #3 and GH #4 must honor it when they build the selecting
machinery.

## Risks / Trade-offs

- **The `--no-update` position is fragile against a fugu wrapper CLI change.** If the
  wrapper ever accepts the flag after the subcommand, or renames it, launches break.
  → Exact-argv tests fail loudly and locally rather than producing a hung session,
  and the doctor probe surfaces wrapper problems by name.
- **Parameterizing the Codex adapter risks regressing plain Codex.** →
  Empty-string fallback means the Codex path is unchanged by construction, and the
  existing Codex adapter tests run unmodified as the regression guard. Any change
  that breaks Codex breaks them.
- **The new migration `0027` uses the same `replace()`-against-exact-text mechanism
  as `0007`, so a byte mismatch would no-op silently.** → Both the fresh-migration
  guard and the new upgrade-path test assert the live constraint actually admits
  `codex-fugu`, so a mismatch fails CI rather than corrupting a database.
- **Auth fallback couples fugu's health to Codex's login.** A Codex logout silently
  makes fugu workers unauthorized. → This is accurate rather than incidental: fugu
  genuinely has no separate credential, so reporting the shared state is the truthful
  answer.
- **Permanent fork-only divergence in a heavily-touched upstream file.** Every
  upstream change to `codex.go` now has a merge surface. → The change is confined to
  additive fields plus accessor indirection, which rebases more cleanly than
  restructured logic would; and consolidating into one adapter keeps the divergence
  to a single file rather than spreading it.
- **Acceptance cannot be fully exercised on a host without the binary.** →
  Behavior is covered by unit tests over command construction and scripted auth
  fakes; the live-spawn gap is stated explicitly in the PR rather than papered over.

## Migration Plan

Additive and reversible. The new `0027` migration's Down reverses its Up
symmetrically, so rolling back narrows the CHECK again. Rollback is only unsafe if a
`codex-fugu` session row already exists, which is the normal constraint-narrowing
caveat and not specific to this change. No data migration, no API break — the spawn
enum only widens.

## Open Questions

None blocking. Two items are settled as deferrals rather than unknowns: the model
catalog entries for `fugu` / `fugu-ultra` (GH #4) and the worker-mix bucket wiring
plus `agent:fugu` routing labels (GH #3).
