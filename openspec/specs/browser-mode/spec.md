# browser-mode Specification

## Purpose
TBD - created by archiving change first-class-browser-mode. Update Purpose after archive.
## Requirements
### Requirement: Browser Surface Uses Real Daemon Data
The system SHALL provide a browser runtime that renders the supervisor UI from
the daemon's real project, session, pull request, review, notification, import,
and workspace-file APIs instead of preview-only mock data.

#### Scenario: Browser loads live workspace state
- **WHEN** a user opens the browser UI served by AO
- **THEN** the UI fetches projects and sessions from the daemon API and renders the returned workspace state

#### Scenario: Browser mode has no mock data branch
- **WHEN** the renderer runs with `VITE_NO_ELECTRON=1`
- **THEN** core workspace, pull request, review, and migration data hooks still use daemon API requests

### Requirement: Browser Surface Uses Same-Origin Daemon Transport
The system SHALL expose daemon HTTP, server-sent event, health, readiness, and
terminal mux WebSocket routes to browser clients through the same origin as the
served renderer.

#### Scenario: Browser API requests stay same-origin
- **WHEN** the production browser bundle runs without an explicit API base URL
- **THEN** API and event-stream requests target the browser page origin

#### Scenario: Browser terminal connects through mux proxy
- **WHEN** a user opens a session terminal in browser mode
- **THEN** the terminal connects to `/mux` using `ws://` or `wss://` derived from the browser page origin

#### Scenario: Browser daemon status uses health endpoint
- **WHEN** the browser runtime has no Electron preload bridge
- **THEN** daemon status is read from the same-origin `/healthz` endpoint

### Requirement: Browser Server Restricts Proxy Access
The system SHALL restrict the browser server's daemon proxy to trusted hosts and
origins before forwarding requests to the loopback daemon.

#### Scenario: Trusted tailnet origin can reach daemon API
- **WHEN** a browser request uses the configured `AO_WEB_PUBLIC_URL` origin and host
- **THEN** the web server proxies supported daemon HTTP routes to the loopback daemon

#### Scenario: Untrusted origin is rejected
- **WHEN** a browser request or WebSocket upgrade uses an origin outside loopback and `AO_WEB_PUBLIC_URL`
- **THEN** the web server rejects the request before it reaches the daemon

#### Scenario: Static app routes fall back to the renderer
- **WHEN** a browser requests a non-asset UI route
- **THEN** the web server serves `index.html` so the client-side router can handle the route

### Requirement: Native-Only Features Degrade In Browser Mode
The system SHALL avoid presenting Electron-only actions as usable browser
controls and SHALL provide browser-safe alternatives where the workflow remains
meaningful.

#### Scenario: Desktop updater is not actionable in browser mode
- **WHEN** a user opens global settings in browser mode
- **THEN** desktop update controls are replaced with a non-actionable browser-mode message

#### Scenario: Browser project import accepts host paths
- **WHEN** a browser user creates or imports a project
- **THEN** the UI allows entering a path on the AO host and submits that path through the existing daemon project APIs

