## ADDED Requirements

### Requirement: Non-Electron installs provide the Claude ACP runtime

AO's supported non-Electron deployment SHALL install the Claude ACP adapter and a Node.js runtime with major version 22 or newer in the runtime layout resolved by the daemon without requiring operator-created files or environment overrides.

#### Scenario: Headless deployment launches Claude chat

- **WHEN** an operator deploys AO through the supported headless deployment flow and spawns a Claude Code session in chat mode
- **THEN** AO resolves the installed ACP runtime and launches the pinned adapter with the operator's existing Claude Code executable

#### Scenario: TUI session switches to chat

- **WHEN** a Claude Code TUI session created by a supported non-Electron deployment requests a chat interface handoff
- **THEN** the chat-driver preflight resolves the installed ACP runtime instead of returning `CHAT_DRIVER_UNAVAILABLE`

#### Scenario: Explicit runtime overrides remain authoritative

- **WHEN** `AO_CLAUDE_ACP_COMMAND` or `AO_ACP_RUNTIME_DIR` is configured
- **THEN** AO evaluates those overrides in their existing precedence order before the installed-runtime fallback

### Requirement: Electron and non-Electron runtimes share one dependency pin

The Electron and non-Electron packaging paths SHALL install the Claude ACP adapter from the same committed package manifest and lockfile so their dependency versions cannot drift.

#### Scenario: Both package types are built

- **WHEN** Electron and headless artifacts are produced from the same source revision
- **THEN** both install the `@agentclientprotocol/claude-agent-acp` version pinned by `frontend/acp-runtime/package-lock.json`

#### Scenario: Provider CLIs remain external

- **WHEN** either ACP runtime package is built
- **THEN** optional provider CLI packages are omitted and AO uses the user's separately installed Claude Code executable

### Requirement: Doctor diagnoses Claude ACP runtime availability

`ao doctor` SHALL validate the same ACP runtime resolution path used by Claude chat and SHALL report actionable remediation when the runtime or compatible Node.js executable cannot be resolved.

#### Scenario: Runtime is available

- **WHEN** `ao doctor` runs where the installed or explicitly overridden ACP runtime is valid
- **THEN** the report includes a passing Claude ACP runtime check

#### Scenario: Runtime is unavailable

- **WHEN** `ao doctor` cannot resolve a complete Claude ACP runtime
- **THEN** the report names the missing runtime condition and tells the operator to rerun the supported deployment or configure an existing runtime override

#### Scenario: Node version is incompatible

- **WHEN** the resolved runtime uses Node.js older than major version 22
- **THEN** the doctor report includes the required minimum and the detected version
