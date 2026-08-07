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

| #   | Surface                       | Class                       | Source / persistence                                                                                                                                        | Lifetime and notes                                                                                                                                                                                                                                                                                                      |
| --- | ----------------------------- | --------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | AO session ID                 | Identity                    | Minted once by the SQLite store as `{project}-{num}-{generation}` (or `prime-{num}-{generation}`); persisted as `sessions.id`                               | `{num}` is monotonic within one database. The 64-bit lowercase-hex generation token is minted once in `daemon_settings` for that database generation, so a rebuilt database cannot reuse the complete ID. Pre-existing `{project}-{num}` IDs are never rewritten.                                                       |
| 2   | Display name                  | Presentation                | Computed once by `resolveDisplayName` from role, `sessionPrefix`, issue, and issue title; persisted as `sessions.display_name`                              | The daemon owns it and delivers the same value to AO, the harness TUI, and harness app/mobile lists. Rename changes presentation, never identity.                                                                                                                                                                       |
| 3   | `sessionPrefix`               | Presentation input          | Project config; creation derives a short project-unique value, while legacy projects may fall back to the project ID                                        | Feeds display names and canonical Orc labels; not a durable identity by itself.                                                                                                                                                                                                                                         |
| 4   | Claude session UUID           | Native harness identity     | Fresh UUID minted by the Claude adapter at launch and persisted through `agent_session_id`; deterministic UUIDv5 over AO ID is legacy restore fallback only | Independent of the AO ID for new sessions. PR #245 owns this seam.                                                                                                                                                                                                                                                      |
| 5   | Workspace path                | External identity namespace | New worker paths use the persisted namespace key `<label>--<complete-id>`                                                                                   | The complete generation-qualified ID remains present. Legacy rows keep their persisted paths and fall back to the complete ID only if recreation is unavoidable. Project Orc and projectless Prime keep their existing role-specific behavior.                                                                          |
| 6   | Claude project-directory slug | External identity namespace | Claude slugifies the workspace cwd, transitively deriving from #5                                                                                           | Therefore readable and non-recycling for new workers without AO duplicating Claude's slug algorithm.                                                                                                                                                                                                                    |
| 7   | tmux session                  | External identity namespace | New workers derive the runtime handle from the persisted namespace key                                                                                      | Safe namespace keys remain verbatim with no AO-imposed length cutoff. Keys containing unsupported characters retain a readable head plus a digest of the complete key. Create derives the handle; restart, lookup, attach, and destroy preserve the persisted opaque handle. Legacy rows retain their existing handles. |
| 8   | VCS branch                    | External identity namespace | New worker defaults use `ao[/dev]/<namespace-key>/root` for single repos and `ao[/dev]/<namespace-key>` for workspace projects                              | The namespace key preserves sibling-PR attribution. Explicit branches remain caller-owned. Project Orc and fleet Prime retain canonical singleton role branches. Legacy rows retain their persisted branches.                                                                                                           |
| 9   | `agent_session_id`            | Native harness identity     | Adapter-native identity persisted on the session row                                                                                                        | Used by harnesses for restore. It is not a replacement display name and is never shown as one.                                                                                                                                                                                                                          |

## Constraints

The surface map above says what each surface _is_; this says what they
_require_. A session ID is composed from the project id, so the project id
carries the constraints of every surface below it. tmux is the binding one: `.`
and `:` are tmux's target grammar (`session:window.pane`), and tmux silently
rewrites them to `_` in a session name rather than rejecting them — the original
string is then unaddressable, so AO would lose track of a live session. git
rejects `..` and `:` in a refname but tolerates a lone `.`. Workspace paths,
Claude project slugs, and request paths tolerate all three.

The intersection is `[A-Za-z0-9_-]`. It is enforced in one place —
`domain.IsValidProjectID` — at the moment an id enters the system as **new**:
project registration (`validateNewProjectID`) and legacy import both call it, so a
dotted id can never be minted into an unlaunchable session id. Any new surface
stricter than this belongs in this section and in
`domain.IsValidProjectID`, not as a local sanitizer at the point of use — a surface
that quietly rewrites its own copy of the id produces a second identity AO cannot
map back.

Operations on an id that is **already stored** — get, pause, settings/config
updates, and removal — use a looser, traversal-safe check (`validateProjectID` in
the project service) that still rejects `""`, `.`, `..`, `/`, and `\` but tolerates
a lone `.`. That is deliberate: a project registered before this rule tightened
must stay retrievable and, above all, **removable**, which is how the operator
cleans a dotted project up. Tightening the lookup path to the strict rule would
strand exactly the projects the strict rule is meant to help retire.

Consuming an **already-composed session id** follows the same discipline as
consuming a stored project id, and for the same reason. A session id is composed
as `{project}-{num}-{generation}`, so a project registered before the strict rule
tightened yields sessions whose id keeps the dot — a real, non-recycling identity
that must round-trip, not be rejected. The CLI surfaces that place a session id
into a loopback request path (`ao agent-process supervise --session`, the `ao
hooks` activity callbacks) or a per-session data-dir file therefore validate it
with `domain.IsPathSafeSessionID`, the traversal-safe session-id analog of
`validateProjectID`: it allows a dot _within_ the id (so an `example.net`-derived
id passes) while still rejecting the path-unsafe forms `""`, `.`, `..` (including
as a substring), a trailing `.`, and path separators. The earlier strict
`cli.sessionIDPattern` rejected the dot outright and was the #266 root cause —
the supervisor refused the id with "invalid session id" and the agent never
launched. A minted launch id, by contrast, is random hex and never dotted, so it
keeps the strict `launchIDPattern`.

A launch that dies before it becomes a working agent must never masquerade as a
healthy session. The supervisor (`ao agent-process supervise`) reports its
managed process's exit to the daemon, and `reportSupervisedExit` populates the
error only when the child died inside its launch window. So a `process-exited`
report that carries a launch error, for a session that has never recorded an
agent hook signal, is a FAILED LAUNCH: the lifecycle manager terminates it with
that diagnostic (`ApplyActivitySignal`) and reclaims its runtime and workspace
via the wired session terminator, rather than leaving it exited-but-live. The
launch-window error is what distinguishes a failed launch from a session that
did real work and exited later, and from a broken-hook session whose late exit
carries no launch error; the generation fence (`RuntimeLaunchID`) drops stale
reports; and the supervisor's own exit report never stamps `FirstSignalAt` (that
field means the first AGENT hook). The path covers spawn and restore uniformly.
A terminated failed launch is no longer counted by tracker intake's seen-issue
set, so the issue is retried instead of stranded (#266).

## Database generations

`daemon_settings.session_id_generation` is a 16-character lowercase-hex token
(64 bits) minted when the migration initializes a database. 64 bits is sized
against database generations (single digits over a host lifetime) while keeping
the authoritative identity suffix compact. It is read, not re-derived, when the
store creates a session. A database replacement resets
`num` but also initializes a new generation token, so the new
`{project}-1-{generation}` cannot equal an old `{project}-1-{old-generation}`
that still names a workspace, tmux session, branch, or Claude project directory
on the host.

If the generation token is missing or malformed, creation fails. Falling back to
`{project}-{num}` would silently reintroduce the exact recyclable identity this
schema removes.

## Creation-time worker namespace keys

After the store allocates a worker session ID, Session Manager composes one
immutable namespace key from the already-resolved daemon display name and the
complete ID. The readable component is a lowercase ASCII alphanumeric slug:
unsafe character runs become `-` and an empty result falls back to `work`. It
has no separate namespace-specific length budget; the daemon-owned display name
is already bounded by the `session-naming` contract. The stored form is
`<readable-label>--<complete-session-id>`.

The key is persisted before AO creates a branch, workspace, or runtime. Those
surfaces consume the stored fact; adapters never re-slug the mutable display
name. Renaming a session therefore changes only presentation and harness name
delivery. It does not move or rename external resources.

tmux applies no AO-specific length shortening. A key containing only the safe
namespace character set is used verbatim, including its complete readable label
and generation-qualified identity. A key containing unsupported characters is
sanitized to a readable head and suffixed with a SHA-256 digest computed from
the complete key, so compatibility canonicalization cannot discard the
generation token's collision protection.

## Compatibility

Neither migration rewrites `sessions.id` or existing resource metadata. The
namespace-key column defaults to empty for existing rows, so live and persisted
sessions keep their current workspaces, runtime handles, branches, and native
harness session IDs. Only newly created workers receive readable namespace keys;
role singletons and explicit caller branches keep their existing ownership
rules. Restart treats a key-less session's persisted runtime handle as opaque,
accepting it only when it matches either the current safe name or AO's historical
48-byte canonicalization. Changing the canonical name for future sessions
therefore cannot rename a live legacy tmux session or target another session's
handle.

Project Orc remains a canonical singleton at
`<project>/orchestrator/<prefix>-orchestrator` with branch
`ao[/dev]/<prefix>-orchestrator`. Projectless fleet Prime keeps its canonical
repository at `<data-dir>/prime/repo`, its existing per-session managed worktree
at `prime/<complete-id>`, and branch `ao[/dev]/prime`. Neither role consumes a
worker namespace key.
