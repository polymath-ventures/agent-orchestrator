# AO CLI

The `ao` CLI is a thin Go/Cobra client for the local Agent Orchestrator daemon.
It starts, discovers, inspects, and stops the daemon through the loopback HTTP
surface and the `running.json` handshake. It must not open SQLite directly or
call runtime, workspace, tracker, or agent adapters in-process.

When using the CLI directly from a shell, make sure the daemon is running first
with `ao start` or by opening the desktop app. Product commands such as
`ao agent ls` and `ao spawn` call the loopback daemon and will fail with a
"daemon is not running" error if no `running.json` points at a live process. From
a source checkout, build and run the local binary explicitly, for example:

```bash
cd backend
go build -o ./bin/ao ./cmd/ao
./bin/ao agent ls
```

## Current commands

Every product command resolves to a daemon HTTP route. Run `ao <command>
--help` for the authoritative flag shape.

### Daemon control

| Command                       | Purpose                                                                                                                                                                             |
| ----------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `ao start`                    | Start the daemon in the background and wait for `/readyz`.                                                                                                                          |
| `ao stop`                     | Gracefully stop the daemon after verifying daemon identity; uses `systemctl --user stop ao.service` when that unit owns the PID, otherwise token-bearing loopback `POST /shutdown`. |
| `ao status` / `--json`        | Report daemon state from `running.json`, process liveness, `/healthz`, and `/readyz`.                                                                                               |
| `ao doctor` / `--json`        | Check config, data directory, DB-file presence, daemon state, `git`, and (on Darwin/Linux) `tmux`; on Windows conpty is built in.                                                   |
| `ao completion <shell>`       | Generate completions for `bash`, `zsh`, `fish`, or `powershell`.                                                                                                                    |
| `ao version` / `ao --version` | Print build metadata.                                                                                                                                                               |
| `ao daemon`                   | Hidden internal daemon entrypoint used by `ao start`.                                                                                                                               |

### Product commands

| Command                           | Daemon route                                                          |
| --------------------------------- | --------------------------------------------------------------------- |
| `ao project add`                  | `POST /api/v1/projects`                                               |
| `ao project ls`                   | `GET /api/v1/projects`                                                |
| `ao project get <id>`             | `GET /api/v1/projects/{id}`                                           |
| `ao project set-config <id>`      | `PUT /api/v1/projects/{id}/config`                                    |
| `ao project config export <p>`    | `GET /api/v1/projects/{id}`                                           |
| `ao project config apply <p> <f>` | `GET` + `PUT /api/v1/projects/{id}/config`                            |
| `ao project config diff <p> <f>`  | `GET /api/v1/projects/{id}`                                           |
| `ao project rm <id>`              | `DELETE /api/v1/projects/{id}`                                        |
| `ao role prompt <project> <role>` | `GET /api/v1/projects/{id}/roles/{role}/prompt`                       |
| `ao prime settings`               | `GET /api/v1/prime/settings`                                          |
| `ao prime enable` / `disable`     | `GET` + `PUT /api/v1/prime/settings`                                  |
| `ao prime prompt`                 | `GET /api/v1/prime/prompt`                                            |
| `ao pause [project] [--hard]`     | `POST /api/v1/projects/{id}/pause` (or `/fleet/pause` with `--all`)   |
| `ao resume [project]`             | `POST /api/v1/projects/{id}/resume` (or `/fleet/resume` with `--all`) |
| `ao agent ls`                     | `GET /api/v1/agents`                                                  |
| `ao agent ls --refresh`           | `POST /api/v1/agents/refresh`                                         |
| `ao spawn`                        | `POST /api/v1/sessions`                                               |
| `ao session ls`                   | `GET /api/v1/sessions`                                                |
| `ao session get <id>`             | `GET /api/v1/sessions/{id}`                                           |
| `ao session kill <id>`            | `POST /api/v1/sessions/{id}/kill`                                     |
| `ao session restore <id>`         | `POST /api/v1/sessions/{id}/restore`                                  |

`project config apply` sends the `configETag` from its read as `If-Match`, so
concurrent edits fail with `PROJECT_CONFIG_STALE` instead of being overwritten.
Repeat `--only <field.path>` to restore selected nested object paths.
`project config diff --unexpected` additionally reports meaningful live fields
absent from the spec; the default remains partial-spec comparison. Omitted
`omitempty` zero/empty values compare as converged. Export warns on
secret-shaped env keys, while diff redacts env and secret-shaped leaf values.
| `ao session rename <id> <name>` | `PATCH /api/v1/sessions/{id}` |
| `ao session cleanup` | `POST /api/v1/sessions/cleanup` |
| `ao session claim-pr <id> <pr-ref>` | `POST /api/v1/sessions/{id}/pr/claim` |
| `ao orchestrator ls` | `GET /api/v1/orchestrators` |
| `ao send` | `POST /api/v1/sessions/{id}/send` |
| `ao preview [url]` | `POST /api/v1/sessions/{id}/preview` |
| `ao notify slack` | `GET /api/v1/notifications/stream` (+ `GET /api/v1/notifications`) |
| `ao hooks <agent> <event>` | `POST /api/v1/sessions/{id}/activity` (hidden) |

`ao agent ls` prints the daemon-supported agent catalog with local install/auth
readiness. Use `--refresh` to rerun the bounded local probes and `--json` to
print the raw inventory response.

`ao spawn` resolves project context in this order: explicit `--project`,
`AO_PROJECT_ID`, `AO_SESSION_ID` (by fetching the current session from the
daemon), then the current working directory matched against registered project
paths. If `AO_SESSION_ID` is set but the session cannot be fetched, pass
`--project` explicitly.

If `--agent` / `--harness` is omitted, `ao spawn` uses the resolved project's
`worker.agent` config. Before spawning, the CLI refreshes the advisory agent
catalog and fails early when the selected agent is unsupported, not installed,
or unauthorized. It warns-but-continues when auth remains unknown because daemon
spawn remains the authoritative runtime validation point. Use
`--skip-agent-check` to bypass only this CLI-side preflight.

`ao pause`/`ao resume` gate a single project (positional id) or the whole fleet
(`--all`). A soft pause stops new intake and spawns and lets live workers drain
at idle; `--hard` terminates live workers immediately (orchestrators stay alive
in every mode). Pause state also appears in `ao status` (`fleet:` line) and
`ao project ls` / `ao project get`. `ao spawn --force` overrides an active pause
for a single spawn.

`ao project config` treats a project's stored config as versionable JSON.
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

`ao preview` resolves its session from the `AO_SESSION_ID` environment variable
(it is meant to run inside a session), not a flag. With no argument it
autodetects an `index.html` in the session workspace; with a URL argument it
opens that URL verbatim (`file://`, `http`, `https`).

`ao notify slack` mirrors AO's existing notifications into a Slack channel,
one-way. It is a read-only consumer of the daemon's notification API: it
subscribes to `GET /api/v1/notifications/stream` and posts every notification —
the same ones the desktop bell shows — to a Slack incoming webhook. It runs in
the foreground until interrupted, and adds no daemon surface.

```bash
export AO_SLACK_WEBHOOK_URL="https://hooks.slack.com/services/T000/B000/XXXX"
ao notify slack
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

PR and review actions (merge, resolve-comments, review execute/send) are
HTTP-only today and driven by the frontend; there are no `ao pr` / `ao review`
commands yet.

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

`ao notify slack` additionally reads `AO_SLACK_WEBHOOK_URL` (the Slack incoming
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
/tmp/ao start
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
