## Context

`domain/session_naming.go` owns the naming grammar: `ComposeWorkerDisplayName`
builds `<prefix> #<issue> <slug>` and `ComposeOrchestratorDisplayName` builds
`<prefix> Orc`, both capped at `MaxSessionDisplayNameRunes` (20). The prefix is
the head of every name, and the grammar takes it as given.

Where the prefix comes from is currently unowned. `ProjectConfig.SessionPrefix`
is blank unless typed by hand, and four call sites independently fall back to
`sessionPrefix(id)` — `id[:12]`, or the literal `"ao"` when the id is empty:
`service/project/service.go:1192`, `session_manager/manager.go:1324`,
`adapters/workspace/gitworktree/workspace.go:1117`, and
`service/session/service.go:595`.

`Service.Add` is the single create path for operator-registered projects. It
holds `addMu` across the whole registration and already calls
`store.ListProjects` (via `activeProjectCount`) before persisting, then branches
into a workspace persist and a single-repo persist. `EnsureDefaultScratchProject`
is a second, narrower create path that seeds `Scratch` only when no project
exists at all.

## Goals / Non-Goals

**Goals:**

- A project created without an explicit prefix gets a short, name-derived,
  project-unique prefix persisted on it.
- One deterministic derivation rule, stated once, beside the grammar that
  consumes it.
- Operator override wins everywhere; a blank is the only thing derivation fills.

**Non-Goals:**

- Migrating or renaming existing projects.
- Changing the naming grammar, the display-name cap, or the delivery path.
- Reworking the four legacy `sessionPrefix(id)` fallbacks.
- Any storage change. The derived value goes in the existing config field.

## Decisions

### Derive at creation and persist, rather than resolve on read

The prefix is written into `ProjectConfig.SessionPrefix` at `Add` time. The
alternative — deriving lazily wherever the prefix is resolved — was rejected on
two counts. First, uniqueness is only meaningful against the _other_ projects, so
a lazy rule would need a full project listing at every name composition, on a path
that today touches nothing but the one record. Second, a lazy value is invisible:
the settings form shows the stored field, so an operator would see a blank box
while sessions displayed something else, and editing it would look like setting a
value rather than overriding one.

Persisting also makes the "existing projects are not renamed" requirement fall
out for free: derivation runs on the create path only, so a project that already
exists is never revisited.

### Cap at three characters, initials first

The operator's call, and the name budget supports it. In a 20-rune name,
`<prefix> #151 ` already spends 6 runes on separators and the work item; a
12-character prefix leaves about two runes of title slug, which is why the
current `id[:12]` fallback is unusable rather than merely ugly. Three characters
leaves the slug intact, and the work item number — not the prefix — is what
identifies a session. The prefix only has to say _which project_, and it is
editable the moment an operator dislikes it.

So: multi-word names take one leading character per word up to three
(`Coach Claw` → `cc`); single-word names take the first three characters
(`mirrorborn` → `mir`).

### Collision resolution lengthens from the name, then numbers

Checked against the prefixes already in use, in this order:

1. The initials/leading-characters candidate.
2. Longer candidates drawn from the project's own characters — a still-meaningful
   prefix beats an arbitrary one, so the name is exhausted before falling back.
3. The smallest unused numeric suffix that keeps the whole prefix within three
   characters (`cc` → `cc2`), truncating the base as needed to make room.

Step 3 always terminates: the three-character alphanumeric space is far larger
than any plausible project count, and the search walks it in a fixed order, so
the result is deterministic given the same set of taken prefixes.

### Compare against _resolved_ prefixes, not stored ones

The collision check reads each existing project's **resolved** prefix
(`resolveSessionPrefix`), not just its stored config value. A project with no
stored prefix still displays `id[:12]`, and a short id resolves to a short
displayed prefix — a project whose id is literally `ao` displays `ao`. That is the
exact collision from the report: a new "Agent Orchestrator" would derive `ao` and
land on top of it. Comparing stored values only would miss precisely the case this
change exists to fix.

### Reuse the listing `Add` already performs

`Add` calls `activeProjectCount`, which lists every project, purely to test
`== 0`. The collision check needs that same list, so `Add` lists once and uses the
result for both. This adds no store round-trip.

Because `addMu` is held across the read and the persist, two concurrent `Add`
calls cannot both observe the same free prefix and both claim it. Uniqueness is a
property of the create path's existing serialization, not a new lock or a
post-hoc duplicate check.

### One unusable-name path, and it is not a shared literal

When a name yields no usable characters, the fallback derives from the project id
instead — and when that yields nothing either, from a deterministic hash of the
id. Emitting a fixed literal here would recreate the defect: `ao` on every project
is exactly the state being fixed, and per the operator, a meaningless-but-unique
token beats a meaningful-but-shared one. Creation never fails over a prefix.

### Rule lives in `domain/session_prefix.go`

In the `domain` package beside `session_naming.go`, whose
`ComposeWorkerDisplayName` consumes the prefix — its own file because derivation
and the grammar are separate concerns over the same value. The service supplies the
inputs (project name, id, the set of prefixes in use) and stores the result; it
holds no derivation logic of its own. Derived prefixes are lowercase
alphanumerics, so they satisfy `NameRuneAllowed` and `validateNameComponent` by
construction.

### `EnsureDefaultScratchProject` derives too

It creates a project with a blank prefix, so it takes the same rule (`Scratch` →
`scr`). It seeds only when no project exists, so no collision is possible there,
but routing it through the same function keeps one rule rather than two.

## Risks / Trade-offs

- **Three characters is lossy; unrelated projects can read alike** ("Coach Claw"
  and "Code Cleanup" both want `cc`) → Collision resolution guarantees the stored
  values differ, and the operator can retype the prefix at any time. Uniqueness
  was the stated priority; legibility was not.
- **A derived prefix can collide with a prefix an operator types _later_** →
  Out of scope by design. `UpdateSettings`/`SetConfig` accept the operator's
  choice as authoritative, and refusing an operator's typed prefix because a
  derived one already took it would be the automation being more cautious than the
  operator. Derivation avoids collisions it can see at creation; it does not
  police the operator afterward.
- **Existing projects keep displaying `id[:12]`** → Deliberate. Renaming them
  would change the names already showing in the Claude and Codex session lists,
  which the ticket rules out. The inconsistency is temporary and visible only to
  operators who registered projects before this change.
