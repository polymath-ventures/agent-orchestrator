## Context

AO's daemon already exposes the contracts needed by the supervisor UI:
`/api/v1/*` for state and actions, `/api/v1/events` for live updates,
`/healthz` and `/readyz` for lifecycle, and `/mux` for terminal WebSocket
traffic. Electron currently supplies native affordances around that renderer
through the preload bridge. The browser surface should reuse the daemon
contracts, not introduce a second data path.

## Goals / Non-Goals

**Goals:**

- Serve the real supervisor UI from a plain browser with no Electron process in
  the request path.
- Keep the browser server small, dependency-free, and upstream-candidate where
  possible.
- Use same-origin HTTP, SSE, and WebSocket access from the browser so tailnet
  users do not need direct access to the daemon's loopback listener.
- Preserve Electron behavior by isolating browser-specific runtime decisions at
  renderer seams.
- Keep fork-only systemd/Tailscale setup separate from upstreamable code.

**Non-Goals:**

- Do not add a second daemon listener or a new auth system; the tailnet and web
  server allowlist are the deployment boundary for this issue.
- Do not change the terminal mux protocol.
- Do not implement a browser replacement for Electron `BrowserView` preview or
  native file browsing beyond browser-safe degradation.

## Decisions

1. Same-origin static/proxy server.
   The production browser entry point is a small Node server that serves
   `frontend/dist` and reverse proxies `/api`, `/healthz`, `/readyz`, and `/mux`
   to the loopback daemon. This avoids exposing the daemon listener directly and
   lets Chromium use normal same-origin fetch, SSE, and WebSocket URLs. The
   smaller alternative of direct `127.0.0.1:3001` browser calls fails for remote
   tailnet users because `127.0.0.1` would be the viewer machine, not the AO
   host.

2. Renderer runtime seams over broad conditionals.
   Browser/Electron differences live behind `hasElectronBridge()`, API base URL
   selection, daemon status polling, and existing bridge stubs. This keeps most
   components on the same data path and prevents a parallel mock-data mode from
   drifting again.

3. Reuse daemon contracts as-is.
   Browser mode consumes the same generated OpenAPI client, event stream, and
   mux protocol as Electron. If a route is missing, the fix belongs in the
   daemon contract, not a browser-only workaround.

4. Native-only features degrade explicitly.
   Desktop updates, native directory picking, notifications, and Electron
   `BrowserView` controls are hidden or replaced with browser-safe behavior.
   Browser project import accepts a path on the AO host and lets the daemon
   validate it; it does not try to browse the viewer's local filesystem.

## Risks / Trade-offs

- Host/Origin bypass or DNS rebinding risk -> validate proxied requests against
  loopback hosts or `AO_WEB_PUBLIC_URL`, strip `Origin` before forwarding, and
  reject untrusted WebSocket upgrades before they reach the daemon.
- Browser feature parity gaps -> explicitly degrade native-only affordances and
  keep real daemon data/terminal behavior as the acceptance baseline.
- Existing browser e2e drift -> replace global mock data with daemon-shaped
  Playwright route fixtures and add a specific browser-mode spec.
- Fork deployment divergence -> keep service units and Tailscale instructions in
  fork-only files/commits separate from upstream-candidate renderer/server work.
