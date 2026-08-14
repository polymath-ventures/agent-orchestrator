## Why

Claude Code chat currently depends on an ACP runtime that is only packaged with Electron, so CLI, headless, and browser deployments fail at spawn or interface-switch time. AO needs to provision and discover the same pinned runtime for every supported installation shape and diagnose missing prerequisites before a user starts a chat session.

## What Changes

- Package the Claude ACP dependency for non-Electron installs while preserving the existing Electron bundle layout and environment overrides.
- Resolve a compatible Node.js runtime for the packaged ACP entrypoint without bundling provider CLIs.
- Add an `ao doctor` check that reports ACP runtime availability and actionable remediation.
- Document ACP runtime parity as a first-class browser concern with upstream sync anchors.
- Add a behavioral guard for runtime resolution in a non-bundle executable layout.

## Capabilities

### New Capabilities

- `claude-acp-runtime`: Provisioning, discovery, validation, diagnostics, and non-Electron use of the pinned Claude ACP runtime.

### Modified Capabilities

None.

## Impact

- Backend Claude ACP chat-driver runtime resolution and doctor diagnostics.
- Non-Electron install/deploy packaging under `ops/` and the Electron ACP packaging input under `frontend/acp-runtime/`.
- Distribution/build metadata used to keep the ACP dependency version authoritative in one place.
- Fork sync documentation and backend/ops regression tests.
