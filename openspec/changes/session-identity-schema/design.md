## Context

A session has nine name surfaces (see the table in GH #249). GH #150 unified the
one an operator _reads_ — the daemon-computed display name (`ao #237`). This
change owns the one that _identifies_.

Five surfaces derive from the AO session ID (#1): the filesystem workspace path,
the Claude project directory (transitively, via the path), the tmux runtime
session, the VCS branch, and — before GH #244 / PR #245 — the Claude session
UUID. PR #245 already moved the Claude UUID off the recyclable ID by minting and
persisting a fresh UUID per spawn, so surface #4 is handled and this change is
stacked on it. The remaining recycling exposure is entirely the four ID-derived
external namespaces:

- workspace path: `filepath.Join(managedRoot, projectID, sessionID)`
  (`gitworktree/workspace.go` `managedPath`).
- tmux session name: **is** the session ID (`tmux.go` `SessionName`).
- branch: `ao/<namespace>/<sessionID>/root` (`manager.go` `defaultSessionBranch`).
- Claude project directory: Claude slugifies the workspace cwd → transitively
  the session ID.

The AO session ID is `MAX(num)+1` per project over AO's SQLite
(`session_store.go` `NextSessionNum`). `num` recycles: rebuild the database and
the counter restarts at 1, while the four external namespaces persist on the
host. A fresh `agent-orchestrator-1` then inherits a previous
`agent-orchestrator-1`'s workspace, branch, tmux session, and Claude project
directory. That is the root cause behind #244, and its other symptoms would
present as the same confusing action-at-a-distance.

`upstream/main` carries the identical allocator, so the mechanism is kept
narrow, Polymath-free, and upstream-submittable per `docs/fork.md`.

## Goals / Non-Goals

**Goals:**

- One documented identity schema: for each of the nine surfaces, whether it is
  identity or presentation and what it derives from (`docs/session-identity.md`).
- A session identity that is unique over the lifetime of the host — not merely
  among live sessions, nor only within one database generation.
- No external namespace keyed on a value a database rebuild can reuse.
- The display-name ownership and delivery from #150 preserved unchanged.
- Existing live sessions keep resolving to their current identities; no migration
  orphans in-flight work, workspaces, or branches.
- A regression test proving a rebuilt database cannot mint a colliding identity.

**Non-Goals:**

- Re-doing #244 / PR #245; this builds on it.
- Changing the display-name format (`ao #237`).
- Embedding the work-item slug into the tmux session, workspace path, and branch
  so an operator can read _what a session is working on_ from those surfaces.
  That readability adoption is a follow-up under #249 (see Open Questions), kept
  separate because it carries live operator-muscle-memory and existing-worktree
  risk that the collision fix does not.
- Cleaning up stale on-host state from prior fleet generations (operator
  hygiene, per the ticket's non-goals).

## Decisions

### D1 — Identity stays opaque and stable; presentation is a separate, derived surface

The central open decision the ticket flags is whether the human-readable form
_becomes_ the identity, or identity stays opaque with readable names attached to
every surface. **We keep identity opaque and stable and treat the readable form
as presentation**, because the two properties an identity must have — stability
and uniqueness — are exactly the two the readable form lacks: a work-item
slug/title is mutable (rename, upstream title edits) and non-unique (two sessions
on one issue, retries after a crashed spawn). #150 deliberately split the display
name out; making the readable string the primary key would re-entangle them.

The relationship is therefore **one-directional**: presentation is derived from
identity plus the work item; identity is never derived from presentation. The
schema doc states this rule so a later author adding readability to a surface
cannot "simplify" by dropping the stable key to gain a readable one.

_Alternative rejected — readable form as identity._ Smaller conceptually (one
value), but it fails uniqueness and stability and would reopen #150's collisions
on every rename. Rejected.

### D2 — Non-recycling via a per-database-generation token composed into the ID

Identity becomes `{project}-{num}-{gen}` (and `prime-{num}-{gen}` for the
projectless Prime), where `{gen}` is a **per-database-generation token** minted
once when the database is created and persisted in the single-row
`daemon_settings` table. Within a generation, `num` remains monotonic and unique
per project (the existing `UNIQUE(project_id, num)` invariant is untouched);
across a rebuild, `{gen}` differs, so a restarted counter mints
`{project}-1-{newgen}` which cannot collide with a surviving `{project}-1-{oldgen}`
(or a pre-token `{project}-1`) on the host.

A per-database generation token gives inter-generation separation while `num`
gives exact intra-generation uniqueness. The token is 64 bits (16 lowercase-hex
characters): the colliding population is database generations — single digits
over a host lifetime, not sessions — so 64 bits is already astronomically
sufficient, and it deliberately keeps the composed id short enough that a
realistic project keeps a verbatim tmux session name (see D4). It is
preferred over a per-session suffix because the entropy is minted and stored in
one place rather than repeated for every session: It keeps each fact in one place — the generation
token is stored once, not re-derived per session — matching the Operating
Principles ("a property the code keeps true by construction over a detector").

The token is minted by the migration itself: `lower(hex(randomblob(8)))` run
once against the `daemon_settings` row. A rebuilt database re-runs the migration
against a fresh file and mints a new token; the same database keeps its token
stable forever. The store reads it once and composes it into every new ID at the
single creation site (`CreateSession`), so the four ID-derived external
namespaces become non-recycling with **no per-surface code change**. The fix is
at the cause — the value everything already derives from — not applied four times
at the cases.

_Alternative rejected — seed `num` from surviving host state._ Making `num`
itself survive a rebuild forces the SQLite store to read the filesystem, tmux,
and git branches — cross-layer coupling into the layer that owns the data, and
far less upstream-palatable. The ticket also flags counter-seeding as
insufficient on its own. Rejected.

_Alternative rejected — per-session random suffix._ Only birthday-bounded, adds
per-session noise, and "guesses at uniqueness" where the generation token is
stored once at the database-generation seam. Rejected.

### D3 — New format applies to new sessions only; existing IDs are never rewritten

The session ID is the primary key and is foreign-keyed across tables, and it
already names live worktrees, branches, and tmux sessions on the host. A
migration that rewrote existing IDs would _create_ the orphaning the ticket
forbids. So the migration only adds the generation column and mints the token;
pre-existing rows keep their `{project}-{num}` IDs and resolve unchanged, and the
token is composed only into IDs minted after the migration.

### D4 — Token charset safe across every namespace

`{gen}` is lowercase hex (`[0-9a-f]`), which passes the filesystem
`validatePathComponent` (no separators), the tmux `sessionIDPattern`
(`[a-zA-Z0-9_-]`), the git ref grammar, and Claude's cwd slugification. Length is 32 hex chars (16 random bytes). Long IDs may cross tmux's 48-byte
raw-name cap; tmux's existing `SessionName` canonicalizer then preserves a
readable prefix and appends an 8-hex digest. Every create/lookup/attach path
already calls that same helper, so the canonical runtime identity remains
consistent.

## Risks / Trade-offs

- **tmux 48-char cap / sanitization divergence** → `SessionName` returns the raw
  id only when it matches `sessionIDPattern` and `len<=48`, else it sanitizes to
  a different string and attach/lookup could mismatch. Mitigation: keep every lookup on the existing `SessionName` canonicalizer and
  add a boundary test that a long token-bearing id canonicalizes deterministically
  to the same tmux name for create and attach.
- **Operator muscle-memory churn** → new session IDs gain a 16-char suffix, so
  `ao send --session <id>` requires copying it from `ao session ls` rather than
  typing from memory. The display name (#150) remains the primary readable cue,
  and operators already copy IDs from listings. Documented in the schema doc.
- **#150 regression** → `resolveDisplayName` is independent of the ID format
  (it reads `sessionPrefix + issueID + title`, never `num` from the ID).
  Mitigation: a pinning test asserts the display name is unchanged for a
  token-bearing id.
- **#245 orthogonality** → the generation token must not touch
  `AllocateAgentSessionID`/`MetadataKeyAgentSessionID`. It does not; the Claude
  UUID stays independently minted and persisted, and its legacy derived fallback
  (now over a non-recycling id) is only reached for pre-existing rows.

## Migration Plan

1. Add migration `0059_add_session_id_generation.sql`: `ALTER TABLE
daemon_settings ADD COLUMN session_id_generation TEXT NOT NULL DEFAULT ''`,
   then `UPDATE daemon_settings SET session_id_generation =
lower(hex(randomblob(8))) WHERE id = 1`. Down drops the column.
2. `npm run sqlc` regenerates the query accessor for the new column.
3. `CreateSession` (and the projectless-Prime path) read the token and compose
   `{project}-{num}-{gen}`.
4. Rollback is the migration's Down; because existing IDs were never rewritten,
   rolling back leaves every session resolvable.

## Open Questions

- **Per-surface readability adoption (follow-up under #249).** Putting the
  work-item slug _onto_ the tmux session, workspace path, and branch while the
  opaque token stays the collision key (e.g. `ao/<ns>/{slug}-{gen}/root`) would
  satisfy the ticket's operator-readability criterion for those surfaces. It is
  deferred here per the ticket's own decision default ("land the schema document
  and the non-recycling identity first, and treat per-surface adoption as
  follow-ups"), and is recorded as not-done in `docs/session-identity.md` rather
  than claimed.
