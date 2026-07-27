---
name: using-ao
description: "Catalog of the AO (Agent Orchestrator) `aong` CLI: spawning workers, managing sessions and projects, sending messages, previewing pages, and daemon control. Use when using the aong CLI, spawning workers, or managing AO sessions in an AO workspace."
trigger: "Using the aong CLI in an AO workspace: spawning workers, managing sessions/projects, sending messages, previewing pages."
---

# AO CLI Catalog

`aong` is this fork's operator-facing CLI. It passes most commands through to
the co-installed `ao` binary, while overriding lifecycle verbs whose upstream
names are misleading on the web-first deployment. Every command is
`aong <command> --help` for the authoritative flag list.

On deployments where `aong` is not installed (for example the desktop app or a
source checkout running `ao daemon` directly), use `ao` with the same arguments
for commands that are not fork-specific lifecycle overrides.

| Command | What it does | When to use | Details |
|---|---|---|---|
| `spawn` | Spawn a worker agent in a fresh git worktree | Starting a new task or issue | [commands/spawn.md](commands/spawn.md) |
| `session` | Manage agent sessions (list, kill, rename, restore, etc.) | Inspecting or controlling running/terminated sessions | [commands/session.md](commands/session.md) |
| `project` | Register, inspect, configure, or remove projects | Setting up or managing repos AO knows about | [commands/project.md](commands/project.md) |
| `orchestrator` | List orchestrator sessions | Viewing which sessions are orchestrators | [commands/orchestrator.md](commands/orchestrator.md) |
| `review` | Submit a reviewer result for a worker's PR | Completing a code review loop | [commands/review.md](commands/review.md) |
| `send` | Send a message to a running agent session | Correcting or directing a live agent | [commands/send.md](commands/send.md) |
| `preview` | Open a URL in the desktop browser panel | Demoing a local server or file from inside a session | [commands/preview.md](commands/preview.md) |
| `start` | Start loaded AO user services | Starting a web-first AO deployment | [commands/start.md](commands/start.md) |
| `status` | Show daemon, fleet, and service-unit status | Verifying the deployment is up and healthy | [commands/status.md](commands/status.md) |
| `doctor` | Run health checks plus fork service checks | Diagnosing AO setup problems | [commands/doctor.md](commands/doctor.md) |
| `drain` | Gate new work and drain workers at idle | Pausing intake without interrupting live turns | [commands/drain.md](commands/drain.md) |
| `stop-work` | Terminate all live work immediately | Emergency fleet stop | [commands/stop-work.md](commands/stop-work.md) |
| `resume` | Restore fleet-wide intake and spawns, or resume one project | Returning from drain/stop-work or a project pause | [commands/resume.md](commands/resume.md) |
| `stop` | Stop the AO daemon only | Stopping supervision while sessions keep running | [commands/stop.md](commands/stop.md) |
| `shutdown` | Stop live work, then stop the daemon | Full local shutdown | [commands/shutdown.md](commands/shutdown.md) |
| `pause` | Explain the honest work-control verbs, or pause one project | Recovering from the old misleading fleet verb | [commands/pause.md](commands/pause.md) |
| `import` | Import projects from a legacy AO install | Migrating from the old flat-file store | [commands/import.md](commands/import.md) |
| `version` | Pass through to AO version information (`aong --version` prints aong metadata) | Checking installed version | - |
| `completion` | Pass through to raw AO shell completion scripts | Setting up tab completion for `ao` | - |

## Conventions

- Most read commands accept `--json` for machine-readable output.
- `-p / --project` scopes session subcommand lookups to one project.
- Session and project ids are shown by `aong session ls` and `aong project ls`.
- `--agent` is an alias for `--harness` on `aong spawn`.
- Every command accepts `-h / --help` for the full flag list.

See [references.md](references.md) for natural-language-to-command mappings.
