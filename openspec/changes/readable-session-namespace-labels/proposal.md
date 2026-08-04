## Why

GH #249 made AO session identity non-recycling, but the external namespaces an
operator actually encounters still expose only the project/counter/token key. A
worker's workspace directory, tmux session, and root branch do not tell the
operator which work item the session is doing, even though the daemon already
owns a stable creation-time display name.

This is the readability increment explicitly deferred by #249. It should land
now as a separate change because adding labels to live filesystem, runtime, and
VCS namespaces has different compatibility and migration risk from preventing
identity reuse.

## What Changes

- The daemon computes one immutable, filesystem/tmux/git-safe **namespace
  label** at session creation from the daemon-owned work context and stores it on
  the session record. Display-name changes after creation never recompute it.
- AO-generated worker workspace paths, tmux session names, and root branches use
  one shared external namespace key that combines the readable label with the
  complete non-recycling session identity. Readability decorates identity; it
  never replaces or weakens the collision key.
- Safe tmux namespace keys remain verbatim without an AO-imposed length cutoff.
  Unsupported characters use compatibility canonicalization over the complete
  key rather than silently dropping identity, while persisted handles remain
  authoritative for lookup, attach, restart, and destroy.
- Existing sessions keep their persisted paths, runtime handles, and branches.
  The new naming applies only to newly created resources; restore never moves or
  renames live state.
- Explicit caller-supplied branches remain caller-owned. Project Orc and fleet
  Prime canonical singleton resource behavior remains unchanged unless the
  implementation design proves a compatible improvement.
- The canonical `session-identity` spec and `docs/session-identity.md` are updated
  so all nine surfaces state the final identity/presentation derivation.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `session-identity`: Require one stored immutable namespace label and readable,
  identity-preserving names for newly created worker workspace, tmux, and branch
  surfaces, while preserving existing live resources and explicit branches.

## Impact

- **Session persistence** — likely one immutable namespace-label/key field on the
  session record and a backward-compatible SQLite migration; no existing ID or
  resource metadata is rewritten.
- **Session manager** — creation-time label computation and one external
  namespace-key construction path shared by branch, workspace, and runtime
  launch configuration.
- **Workspace adapter** — managed worker paths consume the shared key rather than
  reconstructing a label independently.
- **tmux runtime adapter** — session canonicalization consumes the shared key,
  preserves long safe keys verbatim, and keeps persisted handles authoritative.
- **Branch generation** — AO-generated worker root branches include the shared
  readable key while retaining the stable session namespace required for
  sibling-PR attribution.
- **No frontend/API behavior change is required** unless exposing the immutable
  label is necessary for debugging; the visible display-name contract remains
  owned by `session-naming`.
- **Upstream posture** — the mechanism remains generic and suitable for upstream
  submission; no Polymath naming is encoded.
