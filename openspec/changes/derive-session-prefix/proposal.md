## Why

A project's session prefix is the head of every session name AO shows — workers
are `<prefix> #<issue> <slug>` and project orchestrators are `<prefix> Orc`. The
session-naming capability says names are built from "the project's configured
session prefix" but never says where that prefix comes from, and nothing helps an
operator pick one at project creation.

Today the field is blank unless typed by hand, and resolution falls through to the
first 12 characters of the project id — or to the literal `ao` when the id is
empty. The result is that unrelated projects share one indistinguishable prefix:
the operator's `coachclaw` project ran as `ao`, reading as "Agent Orchestrator"
rather than as itself and colliding on sight with any other project that also
landed on `ao`. Once names propagate into the Claude and Codex session lists, the
prefix is the only project cue in one flat cross-project list, so a shared default
is worse than a meaningless-but-unique token would be.

## What Changes

- A project created without an explicit session prefix gets one **derived from the
  project name and persisted at creation**, so the stored value is what the
  operator sees and edits.
- Derivation is **initials-led and capped at 3 characters**: multi-word names
  yield initials (`Coach Claw` → `cc`), single-word names yield the first three
  characters (`mirrorborn` → `mir`). Uniqueness is the goal; the prefix is glanced
  at, while the work-item number carries the identifying detail.
- The derived prefix is **checked against prefixes already in use by other
  projects**. A collision lengthens from the name's own characters first, then
  falls back to the smallest free numeric suffix that still fits in three
  characters (`cc` → `coa` → `cc2`), then a sweep of a fixed alphabet at every
  width the cap allows. The stored prefix is unique while that search still has a
  free value; past that it duplicates rather than failing project creation.
- A name yielding no usable characters derives a deterministic token from the
  project id instead. This path deliberately produces a _distinct_ token rather
  than a shared literal — a shared default is the defect being fixed — and never
  fails project creation.
- An operator-supplied prefix always wins, on both the create path and the
  settings form. Derivation only fills a blank.
- Existing projects are **not** migrated. The legacy resolve-time fallback stays
  exactly as it is, so no project silently renames itself.

## Capabilities

### New Capabilities

<!-- None. This extends an existing capability. -->

### Modified Capabilities

- `session-naming`: adds a requirement stating where a project's session prefix
  comes from when the operator supplies none — derived from the project name at
  creation, capped at three characters, unique against existing projects while
  the derivation's candidate space holds a free value, and persisted. The naming grammar that consumes the prefix is unchanged.

## Impact

- `backend/internal/domain/session_prefix.go` — the single home for the
  derivation rule, beside the grammar that consumes it.
- `backend/internal/service/project/service.go` — the create path fills a blank
  prefix, reading existing projects' prefixes for the collision check.
- No storage change: the derived value is written into the existing
  `sessionPrefix` config field, so there is no migration.
- No change to `resolveSessionPrefix` / `sessionPrefix` fallback behavior in
  `service/project`, `session_manager`, `adapters/workspace/gitworktree`, or
  `service/session`. Those govern projects that already have no stored prefix, and
  repointing them at the new rule would rename existing projects.
- `frontend/src/renderer/components/ProjectSettingsForm.tsx` — unchanged
  behavior; the field now arrives populated for newly created projects. Its `ao`
  placeholder is dropped, having named one project's prefix as though it were
  every project's default.
