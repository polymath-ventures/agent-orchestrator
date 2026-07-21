# Fork Conventions

This repository is the Polymath fork of
[`AgentWrapper/agent-orchestrator`](https://github.com/AgentWrapper/agent-orchestrator).
It replaces the older fork now kept as
[`polymath-ventures/agent-orchestrator-fscked`](https://github.com/polymath-ventures/agent-orchestrator-fscked)
for reference while selected work is ported forward.

The port is tracked from
[`polymath-ventures/agent-orchestrator#1`](https://github.com/polymath-ventures/agent-orchestrator/issues/1).

## Goals

1. Stay rebase-clean on upstream. Upstream moves quickly, so fork changes should
   be easy to sync and reason about.
2. Keep product features upstream-submittable. Structure work so a later
   upstream PR can be assembled mechanically from narrow, conventional commits.

## Porting Rules

1. Branch from `main` for each feature issue.
2. Use conventional commits.
3. Within a feature PR, keep upstreamable changes and fork-only changes in
   separate commits.
4. Label fork-only work `fork-only`.
5. Label upstream-intended work `upstream-candidate`.
6. Reimplement against current upstream code. The old fork is a reference
   implementation, not a cherry-pick source.
7. Clean packages may be copied near-verbatim when they still fit current
   upstream code. Wiring is always rewritten.

Fork-only examples include ops/systemd/Tailscale integration, Codex Fugu
support, and SDLC files. Upstream-candidate work should be narrow enough to
submit directly to upstream after review.

Blacksmith CI runner migration is intentionally out of scope for this bootstrap;
the operator applies that PR directly.

## Browser Mode On The Tailnet

The fork ships a small browser-mode web server for headless hosts. It serves the
built renderer from `frontend/dist` and proxies the daemon's existing loopback
routes (`/api`, `/healthz`, `/readyz`, and `/mux`) from the same browser origin.
It does not replace the desktop release flow and it does not make npm the
canonical install path.

**Deploys are one command**: `ops/deploy.sh [ref]` builds an immutable release
(backend binary + web bundle), flips `~/.ao/deploy/current`, syncs top-level
systemd units (never `*.service.d/` drop-ins), restarts `ao`/`ao-web`, and
verifies daemon health, the public URL, and the fresh boot log.
`ops/deploy.sh --rollback` re-flips to the previous release. The manual steps
below remain as the reference for what the script does.

Build the renderer from a release or checked-out source tree:

```bash
cd ~/.ao/deploy/current/source
npm --prefix frontend ci
npm --prefix frontend run build:web
```

Start the daemon with the same tailnet origin in its CORS allowlist. Keep the
packaged Electron origin when overriding the list:

```bash
AO_ALLOWED_ORIGINS=app://renderer,https://ao.tailnet-name.ts.net ao start
```

If the daemon allowlist is missing the tailnet origin, proxied daemon responses
will say `Origin is not allowed to access this daemon`. If `AO_WEB_PUBLIC_URL`
does not match the Tailscale Serve origin, the web server rejects earlier with
`ao-web: request host or origin is not allowed by AO_WEB_PUBLIC_URL`.

Run the web server locally:

```bash
AO_WEB_BIND=127.0.0.1 \
AO_WEB_PORT=5173 \
AO_WEB_API_TARGET=http://127.0.0.1:3001 \
AO_WEB_DIST="$PWD/frontend/dist" \
AO_WEB_PUBLIC_URL=https://ao.tailnet-name.ts.net \
node ops/ao-web-server.mjs
```

Install the user service on a host that has a release-style symlink at
`~/.ao/deploy/current/source`:

```bash
mkdir -p ~/.config/systemd/user
cp ops/ao-web.service ~/.config/systemd/user/ao-web.service
systemctl --user daemon-reload
```

Set the required public URL without editing the tracked unit:

```bash
systemctl --user edit ao-web.service
```

Example override:

```ini
[Service]
Environment=AO_WEB_DIST=/home/orchestrator/.ao/deploy/current/source/frontend/dist
Environment=AO_WEB_PUBLIC_URL=https://ao.tailnet-name.ts.net
```

Then start it:

```bash
systemctl --user enable --now ao-web.service
systemctl --user status ao-web.service
```

Expose the loopback web server through Tailscale Serve:

```bash
tailscale serve --bg --https=443 http://127.0.0.1:5173
tailscale serve status
```

The tracked service sets `AO_WEB_REQUIRE_PUBLIC_URL=1`, so it fails fast until
`AO_WEB_PUBLIC_URL` is configured. The web server executable also refuses
non-loopback `AO_WEB_BIND` values. Expose it through Tailscale Serve rather than
binding it to the LAN. Proxied daemon routes require a loopback peer with a
loopback `Host`, or a request `Host` that matches `AO_WEB_PUBLIC_URL`; browser
`Origin` headers must be loopback or the same configured public origin. Static
app routes fall back to `index.html`, so hash-history and refreshes both land in
the renderer.

Security posture: anyone who can reach the Tailscale Serve endpoint can operate
the AO daemon through the browser surface, including sending session input and
killing sessions. That is intentionally narrower than a LAN daemon listener:
`ops/ao-web-server.mjs` stays loopback-bound and Tailscale is the access
boundary. Do not expose the Node server directly on the LAN; if this ever needs
a non-tailnet listener, use the bearer-auth model in
`docs/adr/0001-lan-listener-for-mobile.md` instead.

## Headless Server Standup

AO can run as a headless Linux user service on the Polymath fork. This is
fork-only infrastructure: upstream's default desktop app still owns daemon
startup for ordinary installs.

Install the `ao` binary somewhere the user service can execute it. The checked-in
unit expects:

```bash
~/.local/bin/ao
```

Then install and enable the user units:

```bash
# Prerequisite: tmux must be installed and available on PATH.
mkdir -p ~/.config/systemd/user
cp ops/ao.service \
  ops/ao-tmux.service \
  ops/ao-tmux-claim.service \
  ops/ao-tmux-claim.timer \
  ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now ao-tmux.service ao-tmux-claim.timer ao.service
```

For unattended server operation, enable lingering for the service account:

```bash
loginctl enable-linger "$USER"
```

`ops/ao.service` runs `ao daemon` directly, stores state under `~/.ao`, restarts
persistently, and uses `KillMode=process` so a daemon restart signals only the
daemon process. It depends on `ops/ao-tmux.service` because the daemon must not
be the first process to create the default tmux server. If it were, tmux and
every agent pane would inherit `ao.service`'s cgroup, and a routine
`systemctl --user restart ao.service` could kill the live fleet.

`ops/ao-tmux.service` owns the tmux server first and pins `exit-empty off` so the
server does not disappear when the last session exits. It intentionally has no
`ExecStop`: a tidy `tmux kill-server` is a fleet kill. Its stop-job hardening
protects ordinary systemd stop/restart jobs, not direct process kills.

`ops/ao-tmux-claim.timer` periodically asks systemd to start `ao-tmux.service`.
Most ticks are no-ops. The timer matters after a legacy or crashed tmux server
frees the socket: the next server should be born under `ao-tmux.service`, not
lazily under the AO daemon.

`ao stop` preserves operator semantics under `Restart=always`: when the verified
daemon PID is the `MainPID` of `ao.service`, or `/proc/<pid>/cgroup` shows the
PID belongs to `ao.service`, the CLI calls `systemctl --user stop ao.service`
instead of POSTing `/shutdown` and letting systemd immediately restart it. If
the daemon is not owned by the unit, `ao stop` uses the normal token-bearing
loopback shutdown path.

Manual restart verification:

```bash
systemctl --user status ao.service ao-tmux.service
ao status
ao session ls
tmux list-sessions
systemctl --user restart ao.service
ao status
ao session ls
tmux list-sessions
```

Expected result: the daemon comes back ready, existing tmux sessions are still
listed, and AO reattaches to live sessions instead of marking them dead after the
reaper window. A daemon with zero connected Electron/browser clients should stay
up; the supervisor only auto-stops an app-owned daemon after a client has
connected and later disconnected.

Residual risks: a tmux server crash, `tmux kill-server`, `systemctl --user kill`,
host shutdown, direct PID signals, OOM selection, or user-manager teardown can
still kill panes. The units remove routine deploy/restart fleet kills; they do
not make tmux itself unkillable. On migration hosts whose tmux server was already
spawned by the daemon before these units existed, `KillMode=process` protects
daemon restarts, but full tmux cgroup ownership moves after reboot, server
replacement, or socket release.
