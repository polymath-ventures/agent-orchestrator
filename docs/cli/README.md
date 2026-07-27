# AO CLI

This fork's operator-facing CLI is `aong`. It passes most commands through to
the co-installed `ao` binary, while overriding lifecycle verbs whose upstream
names are misleading for the web-first deployment. The raw `ao` binary remains
the machine entrypoint for hidden/software-owned paths such as `ao daemon`,
`ao hooks`, and `ao pty-host`.

The underlying `ao` CLI is a thin Go/Cobra client for the local Agent
Orchestrator daemon. It starts, discovers, inspects, and stops the daemon
through the loopback HTTP surface and the `running.json` handshake. It must not
open SQLite directly or call runtime, workspace, tracker, or agent adapters
in-process.

When using the CLI directly from a shell, make sure the daemon is running first
— `aong start` on this fork's systemd deployment, raw `ao daemon` for a
foreground daemon, or by opening the desktop app. Raw `ao start` does NOT start
a daemon: it fetches and opens the desktop app, which then starts one of its
own. Product commands such as `aong agent ls` and `aong spawn` call the loopback daemon and will fail with a
"daemon is not running" error if no `running.json` points at a live process. From
a source checkout, build and run the local binary explicitly, for example:

```bash
cd backend
go build -o ./bin/ao ./cmd/ao
go build -o ./bin/aong ./cmd/aong
./bin/aong agent ls
```

## Current commands

Every product command resolves to a daemon HTTP route. Run `aong <command>
--help` for the authoritative flag shape.

### Daemon control

| Command                      | Purpose                                                                                                                                      |
| ---------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- |
| `aong start`                 | Start loaded AO user services on this fork's systemd deployment.                                                                             |
| `aong stop`                  | Gracefully stop the daemon. Agent sessions keep running unsupervised.                                                                        |
| `aong status`                | Report daemon state and fleet pause state, plus loaded service-unit active states.                                                           |
| `aong doctor` / `--json`     | Run `ao doctor`, then add fork-owned `ao-web.service` / `ao-tmux.service` health.                                                            |
| `aong drain`                 | Gate fleet-wide intake and drain live workers at idle.                                                                                       |
| `aong stop-work`             | Terminate all live work immediately.                                                                                                         |
| `aong pause <project>`       | Preserve AO's project-scoped pause, including `--hard`. Bare `aong pause` is guidance, not a fleet alias.                                    |
| `aong resume [project]`      | Restore fleet-wide intake and spawns, or resume one paused project when a project is named.                                                  |
| `aong shutdown`              | Stop work, then stop the daemon.                                                                                                             |
| `aong completion <shell>`    | Pass through to `ao completion <shell>`; the generated script is for the raw `ao` command.                                                   |
| `aong version` / `--version` | `aong version` passes through to AO build metadata; `aong --version` prints the aong wrapper's own build metadata.                           |
| `ao daemon`                  | Run the daemon in the foreground. Hidden, but it is the entrypoint the desktop app and `ao.service` both launch, and the one to run by hand. |

### Product commands

| Command                                 | Daemon route                                           |
| --------------------------------------- | ------------------------------------------------------ |
| `aong project add`                      | `POST /api/v1/projects`                                |
| `aong project ls`                       | `GET /api/v1/projects`                                 |
| `aong project get <id>`                 | `GET /api/v1/projects/{id}`                            |
| `aong project set-config <id>`          | `PUT /api/v1/projects/{id}/config`                     |
| `aong project config export <p>`        | `GET /api/v1/projects/{id}`                            |
| `aong project config apply <p> <f>`     | `GET` + `PUT /api/v1/projects/{id}/config`             |
| `aong project config diff <p> <f>`      | `GET /api/v1/projects/{id}`                            |
| `aong project rm <id>`                  | `DELETE /api/v1/projects/{id}`                         |
| `aong role prompt <project> <role>`     | `GET /api/v1/projects/{id}/roles/{role}/prompt`        |
| `aong prime settings`                   | `GET /api/v1/prime/settings`                           |
| `aong prime enable` / `set` / `disable` | `GET` + `PUT /api/v1/prime/settings`                   |
| `aong prime prompt`                     | `GET /api/v1/prime/prompt`                             |
| `aong drain` / `aong stop-work`         | `/fleet/pause` with soft or hard semantics             |
| `aong pause <project>`                  | `POST /api/v1/projects/{id}/pause`                     |
| `aong resume [project]`                 | `/fleet/resume` or `POST /api/v1/projects/{id}/resume` |
| `aong agent ls`                         | `GET /api/v1/agents`                                   |
| `aong agent ls --refresh`               | `POST /api/v1/agents/refresh`                          |
| `aong spawn`                            | `POST /api/v1/sessions`                                |
| `aong session ls`                       | `GET /api/v1/sessions`                                 |
| `aong session get <id>`                 | `GET /api/v1/sessions/{id}`                            |
| `aong session kill <id>`                | `POST /api/v1/sessions/{id}/kill`                      |
| `aong session restore <id>`             | `POST /api/v1/sessions/{id}/restore`                   |

`project config apply` sends the `configETag` from its read as `If-Match`, so
concurrent edits fail with `PROJECT_CONFIG_STALE` instead of being overwritten.
Repeat `--only <field.path>` to restore selected nested object paths.
`project config diff --unexpected` additionally reports meaningful live fields
absent from the spec; the default remains partial-spec comparison. Omitted
`omitempty` zero/empty values compare as converged. Export warns on
secret-shaped env keys, while diff redacts env and secret-shaped leaf values.
| `aong session rename <id> <name>` | `PATCH /api/v1/sessions/{id}` |
| `aong session cleanup` | `POST /api/v1/sessions/cleanup` |
| `aong session claim-pr <id> <pr-ref>` | `POST /api/v1/sessions/{id}/pr/claim` |
| `aong orchestrator ls` | `GET /api/v1/orchestrators` |
| `aong send` | `POST /api/v1/sessions/{id}/send` |
| `aong preview [url]` | `POST /api/v1/sessions/{id}/preview` |
| `aong notify slack` | `GET /api/v1/notifications/stream` (+ `GET /api/v1/notifications`) |
| `ao hooks <agent> <event>` | `POST /api/v1/sessions/{id}/activity` (hidden) |

`aong agent ls` prints the daemon-supported agent catalog with local install/auth
readiness. Use `--refresh` to rerun the bounded local probes and `--json` to
print the raw inventory response.

`aong spawn` resolves project context in this order: explicit `--project`,
`AO_PROJECT_ID`, `AO_SESSION_ID` (by fetching the current session from the
daemon), then the current working directory matched against registered project
paths. If `AO_SESSION_ID` is set but the session cannot be fetched, pass
`--project` explicitly.

If `--agent` / `--harness` is omitted, `aong spawn` uses the resolved project's
`worker.agent` config. Before spawning, the CLI refreshes the advisory agent
catalog and fails early when the selected agent is unsupported, not installed,
or unauthorized. It warns-but-continues when auth remains unknown because daemon
spawn remains the authoritative runtime validation point. Use
`--skip-agent-check` to bypass only this CLI-side preflight.

Use `aong drain` to gate new fleet-wide intake and drain live workers at idle,
`aong stop-work` to terminate live work immediately, and `aong resume` to
restore fleet intake and spawns. Bare `aong pause` is intentionally only
guidance so it is not mistaken for a fleet drain, while `aong pause <project>`,
`aong pause <project> --hard`, and `aong resume <project>` preserve AO's
project-scoped pause/resume. Pause state appears in `aong status` (`fleet:`
line) and `aong project ls` / `aong project get`. `aong spawn --force`
overrides an active pause for a single spawn.

`aong project config` treats a project's stored config as versionable JSON.
`export <project>` prints the stored config (the persisted override set the
daemon serves, not defaults-resolved) as canonical JSON (sorted keys, stable
formatting) — two exports of unchanged config are byte-identical, and every field
the daemon serializes is captured, including ones the flag-based `set-config`
mirror does not model. `apply <project> <file>` is **surgical**: it overlays only
the top-level fields named in the spec file onto the live config (via a
read-modify-write against the existing config PUT) and leaves every unnamed field
untouched; a spec equal to live config performs no write. `diff <project> <file>`
compares only the fields named in the spec against live config, prints each
drifted field (spec vs live), and exits nonzero on drift — so it can gate a CI
job or a scheduled drift check. Unknown field names are rejected by the daemon's
strict config decoder rather than re-validated client-side.

> **Secret handling:** an exported config can include `env` values that carry
> credentials. Treat an export as sensitive — review it before committing to
> version control, and prefer restricting file permissions (e.g. redirect to a
> `0600` file) over pasting it into shared locations. `diff` redacts the value
> of the `env` field in its output so drift checks are safe to run in CI logs.

`aong preview` resolves its session from the `AO_SESSION_ID` environment variable
(it is meant to run inside a session), not a flag. With no argument it
autodetects an `index.html` in the session workspace; with a URL argument it
opens that URL verbatim (`file://`, `http`, `https`).

`aong notify slack` mirrors AO's existing notifications into a Slack channel,
one-way. It is a read-only consumer of the daemon's notification API: it
subscribes to `GET /api/v1/notifications/stream` and posts every notification —
the same ones the desktop bell shows — to a Slack incoming webhook. It runs in
the foreground until interrupted, and adds no daemon surface.

```bash
export AO_SLACK_WEBHOOK_URL="https://hooks.slack.com/services/T000/B000/XXXX"
aong notify slack
```

Because the daemon's notification hub is in-process with no replay, the command
pages through all unread notifications each time it (re)connects and delivers
whatever it has not already sent. Delivered notification ids are stored
atomically in `$AO_DATA_DIR/slack-notifier-state.json` (override with
`AO_SLACK_NOTIFIER_STATE`) so restarts do not replay the backlog. On the very
first run, the current unread snapshot is seeded into that ledger without being
posted; only notifications created after initialization are mirrored.

Set `AO_SLACK_MEMBER_ID` (or the conventional `SLACK_MEMBER_ID` fallback) to
mention a Slack member for attention-class notifications (`needs_input`,
`prime_restart_capped`, and `model_unreachable`). Routine notifications remain
unmentioned.

Nothing is ever read _from_ Slack — there is no slash command, interactivity,
reply path, message-resolution edit, or bot-token requirement. Stopping the
process and unsetting the webhook removes the active Slack surface; the optional
delivery ledger can be deleted independently.

`go run .` in `backend/` remains a compatibility wrapper around the daemon.

PR actions are available through `aong pr merge` and
`aong pr resolve-comments`. Review actions are available through `aong review ls`,
`aong review trigger` (also `execute` and `restart`), `aong review cancel` (also
`stop`), and `aong review submit`.

## Configuration

The CLI and daemon share the same environment-driven config:

| Var                         | Default              | Purpose                                                                                                                                                                                                                                 |
| --------------------------- | -------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `AO_PORT`                   | `3001`               | Loopback daemon port.                                                                                                                                                                                                                   |
| `AO_RUN_FILE`               | `~/.ao/running.json` | PID/port/shutdown-token handshake.                                                                                                                                                                                                      |
| `AO_DATA_DIR`               | `~/.ao/data`         | SQLite data directory.                                                                                                                                                                                                                  |
| `AO_REQUEST_TIMEOUT`        | `60s`                | REST request timeout.                                                                                                                                                                                                                   |
| `AO_SHUTDOWN_TIMEOUT`       | `10s`                | Graceful shutdown cap.                                                                                                                                                                                                                  |
| `AO_MOBILE_ADVERTISED_HOST` | _(autopick)_         | Host (IP or DNS name) advertised in the Connect Mobile pairing status/QR, for daemons reached on an address that interface autopick cannot discover (e.g. a tailnet name). Advertise-only; does not change what the LAN listener binds. |
| `AO_KEEP_DAEMON`            | unset (off)          | Keep the desktop app's daemon running after the window closes; stop only via `ao stop`.                                                                                                                                                 |

`aong notify slack` additionally reads `AO_SLACK_WEBHOOK_URL` (the Slack incoming
webhook to post to), falling back to the conventional un-prefixed
`SLACK_WEBHOOK_URL` if the former is unset. A `--webhook-url` flag is accepted,
but an environment variable is preferred: the command is long-lived, so a flag
value would sit visible in the process table. Resolution order is `--webhook-url`
→ `AO_SLACK_WEBHOOK_URL` → `SLACK_WEBHOOK_URL`.

Slack-only configuration also includes `AO_SLACK_MEMBER_ID` (fallback
`SLACK_MEMBER_ID`) and `AO_SLACK_NOTIFIER_STATE` (default
`$AO_DATA_DIR/slack-notifier-state.json`).

The daemon always binds `127.0.0.1`.

## Manual smoke test

```bash
cd backend
go build -o /tmp/ao ./cmd/ao

tmp=$(mktemp -d)
export AO_RUN_FILE="$tmp/running.json"
export AO_DATA_DIR="$tmp/data"
export AO_PORT=3037

/tmp/ao status --json
/tmp/ao doctor
# `ao start` opens the desktop app and starts no daemon; run the daemon itself.
/tmp/ao daemon &
/tmp/ao status --json
/tmp/ao stop
/tmp/ao status --json
rm -rf "$tmp"
```

## Adding new commands

Add a product command only when a daemon HTTP route owns the corresponding
mutation/read; the CLI must call that route rather than reimplementing daemon
behavior. Commands not yet exposed but with backend routes in place include
`ao events ...` (over the CDC/SSE endpoint) and CLI parity for PR/review
actions.

Do not port old in-process TypeScript CLI behavior that mixed command handling
with storage and runtime implementation details.
