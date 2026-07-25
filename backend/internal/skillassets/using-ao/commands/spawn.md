# ao spawn

Spawn a worker agent session in a registered project. The session runs the chosen agent in a fresh git worktree. Register the project first with `ao project add`.

## Syntax

```
ao spawn [flags]
```

## Flags

| Flag | Meaning | Default / Required |
|---|---|---|
| `--branch string` | Branch for the session worktree | `ao/<session-id>/root` |
| `--claim-pr string` | Immediately claim an existing PR for the spawned session | - |
| `--harness string` | Agent harness to use (see list below) | Project `worker.agent`; required if the project has none |
| `--issue string` | Issue id to associate with the session | - |
| `--model string` | Model pin for this one session | Project/role/harness model config |
| `--name string` | Override the sidebar name for a session with no work item (max 20 characters) | Daemon-computed |
| `--no-takeover` | Refuse if another active session owns the claimed PR (requires `--claim-pr`) | - |
| `--project string` | Project id to spawn the session in | Required |
| `--prompt string` | Initial prompt for the agent | - |

`--agent` is an alias for `--harness`.

Do not pass a session name. The daemon computes it from the session's role and
work item and delivers that same name to the agent's own session list, so a
supplied name overrides the one the operator sees everywhere else. Reach for
`--name` only for a session with no work item to name it after.

Available harnesses: `claude-code`, `codex`, `codex-fugu`, `aider`, `opencode`, `grok`, `droid`, `amp`, `agy`, `crush`, `cursor`, `qwen`, `copilot`, `goose`, `auggie`, `continue`, `devin`, `cline`, `kimi`, `kiro`, `kilocode`, `vibe`, `pi`, `autohand`.

## Examples

```bash
# Spawn a worker for issue 142 in the agent-orchestrator project
ao spawn --project agent-orchestrator --issue 142 --prompt "Fix the session leak described in issue 142. Branch off upstream/main."
```

```bash
# Spawn a worker and immediately claim an open PR
ao spawn --project agent-orchestrator --claim-pr 88 --harness claude-code
```

```bash
# Spawn one Codex worker on a specific model without changing project defaults
ao spawn --project agent-orchestrator --issue 142 --harness codex --model gpt-5-codex
```
