## Context

The canonical `session-identity` capability separates stable identity from
mutable presentation. PR #252 made the identity non-recycling and documented
that worker workspace paths, tmux sessions, and default branches key on it. That
solved collision safety, but those surfaces still expose an opaque
`{project}-{num}-{generation}` string rather than the work an operator is trying
to supervise.

The daemon already resolves one display name before the seed session row is
created. Reusing the _current_ display name later is unsafe, however: display
names can be changed, while paths, runtime handles, and branches must remain
stable. The readable external namespace therefore needs its own immutable,
creation-time value.

This change crosses persistence, session-manager sequencing, workspace, tmux,
and branch generation. Existing live resources cannot be renamed in place.

## Goals / Non-Goals

**Goals:**

- One readable, immutable external namespace key computed once for every newly
  created worker session.
- Workspace, tmux, and AO-generated root branches consume that same stored key.
- The complete non-recycling session identity remains the collision key.
- Existing sessions and explicit caller branches continue resolving unchanged.
- Namespace safe-character handling stays deterministic and testable.

**Non-Goals:**

- Changing the AO session ID format or native harness `agent_session_id`.
- Changing #150's display-name grammar or harness delivery.
- Renaming existing live workspaces, tmux sessions, or branches.
- Making project Orc or fleet Prime singleton resources per-session.

## Decisions

### D1 — Store one immutable external namespace key

Add an immutable session field (working name `namespace_key`) with the logical
form `<readable-label>--<complete-session-id>`. The readable label is a safe
ASCII slug computed from the daemon-owned creation-time display/work context;
the complete non-recycling session ID is retained verbatim as the authoritative
collision component.

The key is persisted once. Adapters receive it; they do not independently
slugify display names or reconstruct identity. This prevents three copies of the
same naming rule from drifting.

_Alternative rejected — use the mutable display name directly._ A later rename
would point restore/relaunch paths at a different external resource. Rejected.

_Alternative rejected — derive a label independently in each adapter._ That
recreates the multi-schema problem #249 removed. Rejected.

### D2 — Compute after identity allocation, before external resources

The session store still owns AO ID allocation. After `CreateSession` returns the
new ID, Session Manager computes the namespace key from the already-resolved
creation-time display name plus that ID, persists it on the seed row, then uses
it for default branch, workspace, and runtime creation.

If key persistence fails, spawn stops before creating external resources. Once a
workspace/runtime exists, the persisted metadata remains authoritative for
restore.

### D3 — Existing sessions use compatibility fallbacks, never migration renames

A backward-compatible migration adds an empty namespace-key field. Existing rows
remain empty and keep their persisted workspace path, runtime handle, and branch.
Restore SHALL use those stored resource facts and SHALL NOT synthesize a new key
that moves live state.

Only a newly created session must have a namespace key. A legacy path that truly
must recreate a missing external resource uses the legacy complete session ID as
the compatibility key and records the degraded behavior; it never mutates the
stored identity.

### D4 — Namespace-specific canonicalization preserves readability and identity

Workspace paths and git branches can consume the complete namespace key after
normal path/ref validation. tmux derives new worker handles through
`NamespaceSessionName`: a safe key is used verbatim with no AO-imposed length
cutoff. A key containing unsupported characters retains a readable head plus a
digest of the complete key. Create derives a handle from the stored key;
restart, lookup, attach, and destroy consume the persisted opaque handle.
Existing key-less rows keep their stored handle authoritative across restart.

### D5 — Explicit and singleton namespaces remain explicit

An explicit caller branch bypasses generated branch naming. Project Orc and
fleet Prime canonical singleton branches/workspaces continue their documented
role semantics rather than gaining a worker work-item label.

## Risks / Trade-offs

- **Long paths and refs** → the creation-time display-name contract already
  bounds the readable input, so do not add a second namespace-label budget.
  Preserve the authoritative identity and verify filesystem, git-ref, and long
  tmux handles at their owning adapters.
- **Spawn crash between seed insert and key persistence** → persist the key before
  any external resource creation; a key-less untouched seed row remains safely
  rollbackable.
- **Restore divergence for legacy rows** → stored workspace/branch/runtime
  metadata wins; key derivation is creation-only, not a restore default.
- **Display label becomes stale when work changes** → intentional. External
  namespace labels describe creation-time provenance; the mutable display name
  remains the live operator-facing title.
- **Schema/API churn** → keep the field internal unless debugging proves API
  exposure necessary; generated DTO changes are not part of the default design.

## Migration Plan

1. Add a nullable/default-empty session namespace-key column without rewriting
   IDs or existing resource metadata.
2. Teach storage round-trip and seed updates for the immutable key.
3. Compute/persist the key before new external resources are created.
4. Wire generated branches, worker workspaces, and tmux runtime names to it.
5. Verify new sessions end-to-end and legacy restores against real git/tmux
   behavior.

Rollback leaves already-created readable external names intact because their
exact paths, handles, and branches are persisted. An older binary continues to
use those metadata facts; it merely stops generating readable keys for later
sessions.

## Open Questions

None required before implementation. The separator and safe-character mapping
are implementation details; the namespace layer does not impose another length
budget on the already-bounded creation-time display label.
