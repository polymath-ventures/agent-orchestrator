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

Build the renderer from a release or checked-out source tree:

```bash
cd ~/.ao/deploy/current/source
npm --prefix frontend install --allow-git=all --allow-remote=all
npm --prefix frontend run build:web
```

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
