## Context

Claude chat is backed by `@agentclientprotocol/claude-agent-acp`. Electron already builds that dependency and a pinned Node distribution from `frontend/acp-runtime/`, while the fork's headless deploy installs only `ao`, `aong`, and the web bundle. The daemon fallback already looks for `acp-runtime` beside the install prefix, so an `ao` copied to `~/.local/bin/ao` naturally resolves `~/.local/acp-runtime`; the packaging flow simply never creates it.

The change must keep environment overrides, avoid bundling provider CLIs, use Node 22 or newer, remain rollback-safe, and avoid introducing network installation at chat-spawn time.

## Goals / Non-Goals

**Goals:**

- Make the supported headless/web deployment install a runtime that the existing daemon fallback resolves.
- Keep the ACP dependency manifest and lockfile authoritative for both Electron and headless packaging.
- Reuse the current packaging implementation and its validated packaged Node 22+ without adding a second builder.
- Make `ao doctor` exercise the same resolver and give actionable remediation.
- Pin the parity concern in `docs/fork.md` and cover the non-bundle layout behavior.

**Non-Goals:**

- Change the Electron runtime layout.
- Bundle Claude Code or any other provider CLI.
- Add chat support to a harness that is not already capability-gated for it.
- Add a background or first-chat package installer.

## Decisions

### Build the runtime during the existing headless deploy

`ops/deploy.sh` will invoke the existing ACP runtime builder unchanged before flipping the release, then move its completed output into the immutable release. That builder already installs from `frontend/acp-runtime/package-lock.json` with optional packages and lifecycle scripts omitted, retains the provider-package defense, verifies the downloaded Node archive checksum, and creates the required Node 22+ layout.

The release stores the result at `<release>/acp-runtime`. A stable `~/.local/acp-runtime` symlink points through `~/.ao/deploy/current/acp-runtime`, so the current `runtimeDirectoryBesideExecutable` fallback finds it for `~/.local/bin/ao`, and rollback follows the existing `current` symlink atomically.

Alternative considered: lazily run npm on first chat use. Rejected because it turns a packaging omission into a spawn-time network mutation, adds progress/state machinery, and can fail after the session request has begun. Installing during the release build prevents the missing state at the layer that owns distribution.

Alternative considered: add another daemon-specific runtime directory fallback. Rejected because the current install-prefix fallback already expresses the correct contract; provisioning the directory it expects is smaller and keeps Electron and headless discovery aligned.

### Share the committed package manifest and lockfile

Both Electron and headless builders continue to consume `frontend/acp-runtime/package.json` and `package-lock.json`. No ACP version constant is added to Go or shell code. Both paths use the same builder and packaged Node distribution, so the deploy path adds no second dependency pin or packaging implementation. The ticket permits a system Node if packaging Node proves disproportionate, but reusing the already-maintained builder is smaller and makes the runtime independent of later host Node changes.

### Expose a read-only runtime check to doctor

The Claude ACP adapter package will expose a small check that calls the same internal resolver used by probe and launch. `ao doctor` will report it under agent harnesses. A missing runtime is a warning with exact remediation because AO and non-Claude chat remain usable; a valid runtime is a pass. Resolver details such as an old Node version remain in the message.

Alternative considered: duplicate file and version checks in the CLI package. Rejected because duplicated resolution rules would drift from the spawn path and could report a false pass.

### Preserve the fork concern with executable anchors

`docs/fork.md` item 1 will name non-Electron ACP chat parity and anchor the resolver, shared runtime package/build script, deploy flow, doctor check, and regression tests so upstream syncs cannot silently restore Electron-only behavior.

## Risks / Trade-offs

- **[Deploy grows a network/package-build step]** → Runtime construction happens before the release flip, uses the committed lockfile, and fails without mutating the active release.
- **[Stable runtime symlink becomes stale]** → It points to the existing `current` release symlink, so deploy and rollback move it together; tests pin this relationship.
- **[Rollback targets a release from before ACP packaging]** → The deploy script removes its now-dangling managed link and emits an explicit warning that Claude chat needs a forward deploy or runtime override; it leaves any operator-managed runtime untouched.
- **[Optional provider package slips into the runtime]** → The shared builder continues to omit optional dependencies and retains its explicit post-build invariant.

## Migration Plan

1. Build the ACP runtime in the new immutable release before changing `current`.
2. Create or refresh the stable install-prefix symlink only after the runtime exists.
3. Flip `current`, install binaries, restart, and run existing deployment verification.
4. Rollback flips `current` back and reconciles the stable runtime link; a pre-feature release produces an explicit chat-unavailable warning rather than a silent dangling link.

## Open Questions

None. The ticket's decision defaults settle system Node use and the existing deploy path provides an install-time provisioning point.
