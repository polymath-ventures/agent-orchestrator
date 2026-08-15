# aong spawn

Spawn a worker agent session in a registered project. The session runs the chosen agent in a fresh git worktree. Register the project first with `aong project add`.

## Syntax

```
aong spawn [flags]
```

## Flags

| Flag | Meaning | Default / Required |
|---|---|---|
| `--branch string` | Branch for the session worktree | `ao/<namespace-key>/root` (single repo) or `ao/<namespace-key>` (workspace project) |
| `--claim-pr string` | Immediately claim an existing PR for the spawned session | - |
| `--harness string` | Agent harness to use (see list below) | Project `worker.agent`; required if the project has none |
| `--issue string` | Issue to associate with the session — a bare number (`142`), `owner/repo#142`, or an issue URL | - |
| `--model string` | Model pin for this one session | Project/role/harness model config |
| `--name string` | Override the sidebar name for a session with no work item (max 20 characters) | Daemon-computed |
| `--no-takeover` | Refuse if another active session owns the claimed PR (requires `--claim-pr`) | - |
| `--project string` | Project id to spawn the session in | Required |
| `--prompt string` | Initial prompt for the agent | - |

`--agent` is an alias for `--harness`.

A bare `--issue 142` is resolved against the project's tracker repository and
stored as `github:owner/repo#142`, the same form tracker intake writes, so
intake sees the issue as covered and does not spawn a second worker on it.

AO computes the immutable namespace key once from the creation-time work label
and the complete session identity. Passing `--branch` opts out of that generated
branch name; AO uses the supplied branch unchanged.

Do not pass a session name. The daemon computes it from the session's role and
work item and delivers that same name to the agent's own session list, so a
supplied name overrides the one the operator sees everywhere else. Reach for
`--name` only for a session with no work item to name it after.

Available harnesses: `claude-code`, `codex`, `codex-fugu`, `aider`, `opencode`, `grok`, `droid`, `amp`, `agy`, `crush`, `cursor`, `qwen`, `copilot`, `goose`, `auggie`, `continue`, `devin`, `cline`, `kimi`, `kiro`, `kilocode`, `vibe`, `pi`, `autohand`.

## Examples

```bash
# Spawn a worker for issue 142 in the agent-orchestrator project
aong spawn --project agent-orchestrator --issue 142 --prompt "Fix the session leak described in issue 142. Branch off upstream/main."
```

```bash
# Spawn a worker and immediately claim an open PR
aong spawn --project agent-orchestrator --claim-pr 88 --harness claude-code
```

```bash
# Spawn one Codex worker on a specific model without changing project defaults
aong spawn --project agent-orchestrator --issue 142 --harness codex --model gpt-5-codex
```
