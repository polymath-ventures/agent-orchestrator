## Context

AO currently assembles launch context in `session_manager` immediately before starting a TUI or Chat controller. The persisted session row keeps durable launch inputs such as kind, project, issue, harness, model, effort, workspace, and prompt metadata, but the exact initial context is not stored as attributed components. The existing role prompt inspector re-runs current project/role assembly, so it is useful for a hypothetical next spawn but cannot prove what an existing session received.

The feature spans prompt assembly, session persistence, HTTP/API schema, CLI output, and optionally frontend display. It also touches a privacy boundary: context may contain operator-provided values and environment-derived configuration, so the inspection surface must be explicit about redaction rather than silently omitting segments.

## Goals / Non-Goals

**Goals:**

- Persist launch-time context metadata for new sessions so inspection does not re-run assembly against changed project settings or files.
- Represent context as ordered segments with structural source IDs, optional absolute file paths, byte counts, contribution state, and redaction/reconstruction flags.
- Expose one session-scoped API and CLI command that can return JSON or a stable human-readable rendering for diffing.
- Make historical sessions inspectable on a best-effort basis when no recorded segment snapshot exists.

**Non-Goals:**

- Editing prompt/context sources from the inspection view.
- Streaming or recomputing context changes after launch.
- Treating the project/role prompt inspector as session-scoped truth.
- Persisting or displaying raw secret values.

## Decisions

1. Persist an immutable initial-context snapshot in session metadata at spawn time. Reconstructing on read from current files was rejected because it cannot satisfy byte-for-byte reproduction after settings or repo files change, and it would make diffing a historical session depend on present state.

2. Model context as segments, not just a single blob plus labels. Each segment records `kind`, `source`, `path`, `content`, `byteCount`, `contributed`, `redacted`, and `reconstructed` fields. Keeping the content on segments makes exact concatenation testable and lets JSON consumers diff by source. A separate detector that tries to split a saved blob later was rejected because attribution would be guessed after the authoritative assembly moment had passed.

3. Record consulted-empty sources as zero-byte non-contributing segments. Omitting them was rejected because silent absence is the operator problem this feature is meant to remove; explicit empty entries show that a source was checked and had no launch contribution.

4. Store already-redacted segment content when a source is secret-bearing. Persisting raw secrets and redacting only at display time was rejected because the database and API are both broader exposure surfaces than the launch adapter needs. Redacted segments remain present with byte counts for the redacted representation and metadata explaining the omission.

5. Return a best-effort reconstructed context for legacy sessions that predate the snapshot. The response marks the context and affected segments as reconstructed so callers do not confuse it with launch-time proof. Returning 404 for existing historical sessions was rejected because operators still need diagnosis help for sessions whose runtime is gone.

6. Return delivered prompt content to the local operator inspection surface. Metadata-only output was rejected because the issue requires concatenating segments to reproduce the assembled context exactly and because the operator needs to identify the exact paragraph to edit; the daemon remains a local operator surface, while secret-bearing runtime environment values remain redacted and non-contributing.

## Risks / Trade-offs

- Snapshot size increases session metadata storage. Mitigation: the captured prompt/system context is already bounded by existing prompt sizes, byte counts make oversized sources visible, and storage avoids a new runtime dependency.
- Redaction source classification can be incomplete. Mitigation: capture the source type at assembly time and default secret-bearing environment/config injections to redacted content with explicit metadata.
- Historical reconstruction may be inaccurate. Mitigation: mark the entire response and individual segments as reconstructed/best-effort when launch-time snapshots are unavailable.
- UI rendering may be deferred. Mitigation: the required API and CLI satisfy headless operation and diffability; frontend can consume the same API without changing the contract.
