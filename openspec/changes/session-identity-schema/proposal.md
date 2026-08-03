## Why

A session has nine name surfaces and they are not one schema. Some are
human-readable, some are machine identity, and five external namespaces —
the filesystem workspace path, the Claude project directory, the tmux runtime
session, the VCS branch, and (via a pure hash) the Claude session UUID — are
all derived from the AO session ID. That ID is `MAX(num)+1` per project over
AO's SQLite, so it **recycles**: rebuild the database and the counter restarts
at 1 while the five external namespaces persist on the host filesystem. A fresh
session then inherits a previous session's external state.

GH #244 / PR #245 is one consequence — a recycled AO ID hashed to a Claude
session UUID whose transcript already existed on disk, and the spawn died in two
seconds. #245 stops that one bleed by minting and persisting a fresh Claude
UUID. But the root cause is broader than one surface: a recyclable counter is
being used as a permanent primary key in five namespaces that never reset, and
#244 is unlikely to be the only symptom.

GH #150 already unified the surface an operator _reads_ (the daemon-computed
display name, e.g. `ao #237`). It left the surface that _identifies_ untouched,
and that is the one with collision consequences.

This change owns the identity schema. It makes the identifying value unique over
the lifetime of the host — not merely among currently-live sessions or within
one database generation — so no external namespace can be keyed on a value a
database rebuild can reuse.

## What Changes

- The daemon mints a **non-recycling** database-generation token once and
  persists it in the daemon settings row. The store's `CreateSession` — the
  single place that already owns per-project `num` allocation — composes it into
  every new identity: `{project}-{num}-{generation}` (and
  `prime-{num}-{generation}` for the projectless Prime). Nothing derives the
  token; it is stored once per database generation.
- Because every external namespace already derives from the session ID, all five
  become non-recycling at once with **no per-surface change**: a rebuilt database
  at `num=1` mints a different generation token, so the workspace path, Claude
  project directory, tmux session, branch, and even the legacy AO-ID-derived Claude UUID
  no longer collide with surviving on-host state. The fix is at the cause, not
  applied five times at the cases.
- The human-readable head of the identity (`{project}-{num}`) is preserved as the identity head, so
  the operator-facing session ID and every surface derived from it still say
  which project and which sequence number a session is — the token is a
  suffix, not a replacement.
- A regression test proves a rebuilt database (counter reset to 1) cannot mint an
  identity, or a workspace path derived from it, that collides with a prior
  generation's `num=1` session.
- The nine surfaces and their identity-vs-presentation classification are written
  down once, in `docs/session-identity.md`, so the schema is documented rather
  than implicit.

## Capabilities

### New Capabilities

- `session-identity`: What a session's identity is, why it is unique over the
  host's lifetime, that it is stored once and derived nowhere, and that every
  external namespace keys on it so a database rebuild cannot leak a prior
  session's external state into a new one.

### Modified Capabilities

None. `session-naming` (GH #150) continues to own the display name as the
_presentation_ surface; this change consumes that separation rather than
redefining it, and the display name computation is untouched.

## Out of scope

- **Re-doing #244 / PR #245.** This change builds on the persisted-Claude-UUID
  seam #245 lands; it does not duplicate or block it.
- **Changing the display-name format** (`ao #237`). This is about which schema
  each surface uses, not about rewording the readable one.
- **Embedding the work-item slug into the tmux session, workspace path, and
  branch** so an operator can read _what a session is working on_ directly from
  those surfaces. That is a readability enhancement layered on top of the
  now-stable identity, and it carries live operator-muscle-memory and
  existing-worktree risk; it is a follow-up under GH #249, recorded in the schema
  doc, not a prerequisite for stopping the collision.
- **Cleaning up stale on-host state from prior fleet generations.** That is
  operator hygiene, per the ticket's non-goals.

## Impact

- **Storage store** — a migration adds the database-generation token to the existing single-row
  daemon settings table; `CreateSession` appends it to the assigned identity for
  project sessions and the projectless Prime. Pre-existing rows keep their
  tokenless IDs.
- **Every ID-derived external namespace** — workspace path, Claude project
  directory (transitively), tmux session, branch, and the legacy Claude UUID
  fallback — inherits non-recycling identity with no code change of its own.
- **Backward compatibility** — sessions created before this change keep their
  identities; restore resolves their existing workspace, branch, and harness
  session unchanged.
- **Upstream** — `upstream/main` carries the identical allocation
  (`NextSessionNum` + `fmt.Sprintf("%s-%d", …)`); the schema problem is
  upstream's, so the mechanism is kept narrow and upstream-submittable per
  `docs/fork.md`.
- **No API/DTO change**, no frontend change: the identity string stays opaque to
  every consumer that already treats it as opaque.
