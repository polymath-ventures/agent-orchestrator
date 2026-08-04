# Session identity schema

AO has two deliberately separate schemas:

1. **Identity** is stable, non-recycling, and safe to use as an external key.
2. **Presentation** is human-readable and may change without moving external
   state.

The relationship is one-directional. Presentation may be computed from the
session's project, role, and work item; identity is never recomputed from a name.
This preserves the daemon-owned display-name contract from [unified session
naming](../openspec/specs/session-naming/spec.md) while ensuring a database
rebuild cannot reuse a key that still exists on the host.

## Current schema

| #   | Surface                       | Class                       | Source / persistence                                                                                                                                        | Lifetime and notes                                                                                                                                                                                                                                                |
| --- | ----------------------------- | --------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | AO session ID                 | Identity                    | Minted once by the SQLite store as `{project}-{num}-{generation}` (or `prime-{num}-{generation}`); persisted as `sessions.id`                               | `{num}` is monotonic within one database. The 64-bit lowercase-hex generation token is minted once in `daemon_settings` for that database generation, so a rebuilt database cannot reuse the complete ID. Pre-existing `{project}-{num}` IDs are never rewritten. |
| 2   | Display name                  | Presentation                | Computed once by `resolveDisplayName` from role, `sessionPrefix`, issue, and issue title; persisted as `sessions.display_name`                              | The daemon owns it and delivers the same value to AO, the harness TUI, and harness app/mobile lists. Rename changes presentation, never identity.                                                                                                                 |
| 3   | `sessionPrefix`               | Presentation input          | Project config; creation derives a short project-unique value, while legacy projects may fall back to the project ID                                        | Feeds display names and canonical Orc labels; not a durable identity by itself.                                                                                                                                                                                   |
| 4   | Claude session UUID           | Native harness identity     | Fresh UUID minted by the Claude adapter at launch and persisted through `agent_session_id`; deterministic UUIDv5 over AO ID is legacy restore fallback only | Independent of the AO ID for new sessions. PR #245 owns this seam.                                                                                                                                                                                                |
| 5   | Workspace path                | External identity namespace | Worker path derives from the complete AO session ID                                                                                                         | A database generation suffix makes the path non-recycling. Project Orc workspaces are canonical singleton paths by design; replacement adopts the one role workspace rather than creating a competing identity.                                                   |
| 6   | Claude project-directory slug | External identity namespace | Claude slugifies the workspace cwd, transitively deriving from #5                                                                                           | Therefore non-recycling for worker sessions without AO knowing Claude's slug algorithm.                                                                                                                                                                           |
| 7   | tmux session                  | External identity namespace | `tmux.SessionName` canonicalizes the complete AO session ID                                                                                                 | Long IDs use the existing readable-prefix + digest canonical form; every create/lookup/attach path calls the same helper.                                                                                                                                         |
| 8   | VCS branch                    | External identity namespace | Worker root branch derives from the complete AO session ID (`ao/<namespace>/<id>/root`)                                                                     | Non-recycling for workers. Project Orc and fleet Prime branches are canonical singleton role branches by design, and replacement reuses them intentionally. Explicit caller-supplied branches remain caller-owned.                                                |
| 9   | `agent_session_id`            | Native harness identity     | Adapter-native identity persisted on the session row                                                                                                        | Used by harnesses for restore. It is not a replacement display name and is never shown as one.                                                                                                                                                                    |

## Constraints

The surface map above says what each surface _is_; this says what they
_require_. A session ID is composed from the project id, so the project id
carries the constraints of every surface below it. tmux is the binding one: `.`
and `:` are tmux's target grammar (`session:window.pane`), and tmux silently
rewrites them to `_` in a session name rather than rejecting them — the original
string is then unaddressable, so AO would lose track of a live session. git
rejects `..` and `:` in a refname but tolerates a lone `.`. Workspace paths,
Claude project slugs, and request paths tolerate all three.

The intersection is `[A-Za-z0-9_-]`. It is enforced in exactly one place —
`domain.ProjectIDPattern` / `domain.IsValidProjectID` — where the id enters the
system: `validateProjectID` calls it at project registration, and the legacy
importer calls it before writing a migrated project. `cli.sessionIDPattern`
re-checks the same class on the composed session id where it is placed in a
request path. Any new surface stricter than this belongs in this section and in
`domain.ProjectIDPattern`, not as a local sanitizer at the point of use — a
surface that quietly rewrites its own copy of the id produces a second identity
AO cannot map back.

## Database generations

`daemon_settings.session_id_generation` is a 16-character lowercase-hex token
(64 bits) minted when the migration initializes a database. 64 bits is sized
against database generations (single digits over a host lifetime), and it keeps
the composed id short enough that a realistic project keeps a verbatim tmux
session name rather than the digest form `SessionName` falls back to past 48
bytes. It is read, not
re-derived, when the store creates a session. A database replacement resets
`num` but also initializes a new generation token, so the new
`{project}-1-{generation}` cannot equal an old `{project}-1-{old-generation}`
that still names a workspace, tmux session, branch, or Claude project directory
on the host.

If the generation token is missing or malformed, creation fails. Falling back to
`{project}-{num}` would silently reintroduce the exact recyclable identity this
schema removes.

## Compatibility

The migration never rewrites `sessions.id`. Legacy sessions keep their current
IDs, foreign keys, workspaces, runtime handles, branches, and native harness
session IDs. Only sessions created after the migration carry a generation
suffix.

## Readability follow-up under GH #249

The identity head remains human-recognizable (`{project}-{num}`), and the display
name remains the authoritative explanation of the work (`ao #249 session…`). A
future per-surface adoption may attach a work-item slug to workspace, tmux, and
branch labels while retaining the opaque generation token as the collision key.
That work is intentionally not claimed here: changing live paths and operator
muscle memory has different migration risk from stopping identity reuse. The
non-negotiable rule for that follow-up is that readability may decorate the
stored identity but never replace or re-derive it.
