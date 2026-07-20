## Why

AO currently has an Electron supervisor and a dev-only web preview path, but the
preview does not use the real daemon state. Operators running AO on a headless
host need the full supervisor UI in a normal browser over the tailnet, without an
Electron process in the request path.

## What Changes

- Add a production browser surface that serves the built renderer and reverse
  proxies the daemon's existing HTTP, SSE, health, readiness, and terminal mux
  WebSocket routes from the same origin.
- Replace browser-preview mock data with real daemon API data in browser mode.
- Make the renderer distinguish Electron from browser runtime so daemon status,
  API base URL, terminal mux URL, project import, and native-only controls behave
  correctly in both environments.
- Add browser-mode e2e coverage that drives the real renderer in Chromium against
  daemon-shaped HTTP and WebSocket fixtures.
- Add fork-only systemd/Tailscale deployment wiring and documentation for this
  repository's headless host setup.

## Capabilities

### New Capabilities

- `browser-mode`: Full supervisor UI in a plain browser using the same daemon
  contracts as Electron.

### Modified Capabilities

- None.

## Impact

- `ops/`: New dependency-free browser server and fork-only service unit files.
- `frontend/`: Renderer runtime seams, browser build script, CSP/build manifest,
  browser-safe UI behavior, and Playwright coverage.
- Backend daemon API and terminal mux protocol are reused as-is unless validation
  reveals a route mismatch.
