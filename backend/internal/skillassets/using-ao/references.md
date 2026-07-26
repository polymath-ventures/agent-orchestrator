# Quick Reference

Natural-language-to-command mappings for common AO tasks.

| You want to... | Command |
|---|---|
| Show me this webpage / open this page | `aong preview "<url>"` |
| Spawn a worker on issue N | `aong spawn --project <p> --issue N --prompt "..."` (the daemon names it) |
| Message a running agent | `aong send --session <id> --message "..."` |
| Kill a session | `aong session kill <id>` |
| List sessions | `aong session ls` |
| Register a repo as a project | `aong project add --path <abs-path> --name <name>` |
| List projects | `aong project ls` |
| Rename a session | `aong session rename <id> "<name>"` |
| Restore a killed session | `aong session restore <id>` |
| Clean up terminated sessions | `aong session cleanup` |
| See a session's details | `aong session get <id>` |
| Start loaded AO user services | `aong start` |
| Check the daemon is up | `aong status` |
| Run health checks | `aong doctor` |
| Clear the preview panel | `aong preview clear` |
| List orchestrator sessions | `aong orchestrator ls` |
| Claim an existing PR for a session | `aong session claim-pr <id> <pr-ref>` |
| Submit a code review verdict | `aong review submit <session-id> --run <run-id> --verdict approved` |
| Configure a project's default branch or model | `aong project set-config <id> --default-branch <branch> --model <model>` |
| Configure per-harness or reviewer model pins | `aong project set-config <id> --config-json '{"agentConfig":{"modelByHarness":{"codex":{"model":"gpt-5-codex"}}}}'` |
| Import projects from a legacy AO install | `aong import --dry-run` (preview), then `aong import -y` |
