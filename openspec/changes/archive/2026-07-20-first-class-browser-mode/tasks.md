## 1. Browser Server And Build

- [x] 1.1 Add a dependency-free production web server that serves the built renderer and proxies daemon HTTP, SSE, health, readiness, and mux WebSocket routes through the browser origin.
- [x] 1.2 Add server tests for static fallback, route proxying, trusted host/origin enforcement, and WebSocket upgrade rejection.
- [x] 1.3 Add a production browser build mode with a same-origin content security policy and build manifest for the served renderer.

## 2. Renderer Browser Runtime

- [x] 2.1 Add a runtime seam for detecting the Electron preload bridge without treating browser mode as preview/mock mode.
- [x] 2.2 Make API base URL, event stream, daemon status, and terminal mux URLs resolve to same-origin browser transport when `VITE_NO_ELECTRON=1`.
- [x] 2.3 Remove browser-preview mock data branches from workspace, pull request, review, migration, and terminal UI paths so browser mode uses daemon APIs.
- [x] 2.4 Replace Electron-only update and native directory-picking controls with browser-safe behavior.

## 3. Browser Mode Coverage And Verification

- [x] 3.1 Add browser-mode unit and component coverage for API base selection, health polling, native-only degradation, and host-path project import.
- [x] 3.2 Replace existing browser e2e mock globals with daemon-shaped Playwright API fixtures.
- [x] 3.3 Add browser-mode e2e coverage for real daemon data rendering, desktop updater degradation, and terminal mux WebSocket routing.
- [x] 3.4 Run the focused and full verification gates, including browser build, frontend typecheck, renderer tests, Playwright e2e, and relevant backend checks.

## 4. Fork Deployment

- [x] 4.1 Add fork-only systemd/Tailscale deployment wiring for serving browser mode from the headless host.
- [x] 4.2 Document how to build, run, and expose the browser surface on the tailnet without treating npm as the canonical distribution path.
- [x] 4.3 Stand up the browser surface on this host and verify it can reach the real daemon through the configured same-origin routes.
