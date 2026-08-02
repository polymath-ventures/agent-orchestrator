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

## Fork Features To Preserve (sync checklist)

The default posture on an upstream sync is **absorb upstream and shape fork
changes into it** — take upstream's structure, UX, and mechanisms, and re-apply
only the named features below. When upstream and the fork both changed the same
area, this list decides what must survive; everything not on it may follow
upstream. Each item names the feature and the concrete thing a sync must keep
working (not a specific UI shape unless stated).

Every item carries **sync anchors** — the files and symbols to re-check after a
merge — and **reference issues/PRs**, so a sync can read the change that
introduced the behavior instead of re-deriving intent from the diff. References
are written `issue → PR` where both exist; a bare number is a PR that closed no
issue. Anchor paths are repository-root-relative, except where written as an
explicit glob or brace set. Anchors say where the behavior lives today; treat
them as the starting point for a search, not as an exhaustive file list.

1. **Web client as a first-class client.** The browser-served renderer
   (`ops/ao-web-server.mjs`, `npm run build:web`, `VITE_NO_ELECTRON`) must stay
   fully servable and usable in a plain browser, on par with Electron. No
   web-path dependence on `window.ao`, Electron preload, or Electron-only daemon
   fields. Sync anchors: `frontend/package.json` defines `build:web` / `dev:web`;
   `frontend/vite.renderer.config.ts` gates the browser build and its dev proxy;
   `frontend/src/renderer/lib/api-client.ts` (`isBrowserMode`,
   `hasTrustedApiBaseUrl`) selects the HTTP transport instead of `window.ao`, and
   `frontend/src/renderer/hooks/useWorkspaceQuery.ts` and
   `frontend/src/renderer/hooks/useShellTerminals.ts` carry the fork's
   trusted-base-URL guards that keep production web mode off preview data;
   `ops/ao-web.service` and `ops/ao-web-server.test.mjs` cover the server itself.
   The `frontend/e2e/*.spec.ts` **browser-mode** suite is the guard — keep it
   green and meaningful, especially `frontend/e2e/browser-mode.spec.ts` and
   `frontend/e2e/mobile-sidebar-toggle.spec.ts`. UX shape may follow upstream;
   browser functionality may not regress.
   Reference issues/PRs: #2 → #18; #46 → #55; #54 → #62; #182 → #191; and #109
   (web project import daemon readiness).
2. **Terminal auto-focus.** A terminal pane takes keyboard focus when it appears
   or is selected, so the user types without clicking (`focusRequest`/`autoFocus`,
   `data-terminal-tab` + `focusActiveTerminalTab`, plus the Ctrl+F6
   exit-focus escape hatch). Sync anchors: `SessionView` issues session-route
   focus requests, `TerminalPane` gates and preserves them until an attachable
   terminal handle exists, `XtermTerminal` focuses xterm's helper textarea, and
   `ShellTerminalsView`/`CenterPane` pass activation requests for shell and
   session-tab selection. Regression coverage lives in
   `frontend/e2e/terminal-focus.spec.ts` and the matching component tests.
   Reference issues/PRs: #131 → #136, #230; #137 → #157 (which also absorbed
   #134 and #135).
3. **Quota & usage tracking.** Per-turn usage telemetry and quota snapshots from
   agent hooks (`usageTelemetryEvent`, `acceptedQuotaSnapshots`, both in
   `backend/internal/lifecycle/manager.go`; the quota probers under
   `backend/internal/observe/quota`), plus model-health and candidate-health,
   which are three separate fork-only packages — `backend/internal/modelhealth`,
   `backend/internal/service/modelhealth`, and
   `backend/internal/candidatehealth`. Sync anchors: hook ingest in
   `backend/internal/cli/hooks.go` and `backend/internal/cli/usage_extract.go`,
   the per-harness parsers such as
   `backend/internal/adapters/agent/claudecode/quota.go`, aggregation and
   alerting in `backend/internal/observe/metrics/{quota,alert,observer}.go`, and
   the sidebar widget `frontend/src/renderer/components/QuotaPanel.tsx`.
   This is mechanism-independent — re-layer it onto whatever signal path upstream
   uses.
   Reference issues/PRs: #8 → #16, #88; #97 → #102; #112 → #113; #116 → #117.
4. **Harness/agent setup & selection.** The agent-selection catalog, the per-role
   model+effort tuples, and the worker-mix UI, plus the fork-only **codex-fugu**
   harness — as both a worker and a **reviewer**, so the Settings "Default
   reviewer agent" picker offers it. These live in shared upstream files, so
   re-apply the fugu reviewer registration on any sync that touches them. Sync
   anchors: `selectableAgentCatalog` in
   `frontend/src/renderer/lib/agent-selection.ts`, called directly by
   `frontend/src/renderer/components/ProjectSettingsForm.tsx` (which also defines
   `HarnessModelRow`) and
   `frontend/src/renderer/components/CreateProjectAgentSheet.tsx`;
   `frontend/src/renderer/components/WorkerMixFields.tsx` renders the mix rows and
   receives its options through `ProjectSettingsForm` rather than calling the
   catalog itself. On the backend the mix is resolved at spawn by
   `resolveSpawnTarget` and `selectMixBucket` in
   `backend/internal/session_manager/manager.go`;
   `backend/internal/observe/trackerintake/observer.go` is the tracker-intake
   caller, and handles `ErrWorkerMixExhausted` / `ErrWorkerMixBucketDown`
   deferral rather than choosing a bucket. The fugu worker plugin is
   `codex.NewFugu()` in `backend/internal/adapters/agent/codex/codex.go` and the
   reviewer is `backend/internal/adapters/reviewer/codex/codex.go` plus its entry
   in `backend/internal/adapters/reviewer/registry.go`;
   `backend/internal/domain/reviewerharness.go` is an upstream file whose
   `ReviewerCodexFugu` constant and its `AllReviewerHarnesses` entry are fork-only
   lines, and `backend/internal/adapters/reviewer/registry_test.go` is the guard
   that keeps the adapter set and the domain set in sync.
   Reference issues/PRs: model management #4 → #34, #64; codex-fugu worker
   harness #12 → #21; fugu reviewer registration #229 → #231; selector
   unification #121 → #124, #125, #132 → #139, and #140 → #141; setup defaults
   #98 → #100 and #106 → #107; per-harness catalog degradation #234 → #235.
5. **Fleet & Prime.** The projectless "AO Fleet" workspace (`FLEET_WORKSPACE_ID`,
   projectless-prime sessions), worker-mix percentages, fleet pause, and the
   daemon-global Prime supervisor. Sync anchors: `FLEET_WORKSPACE_ID` and
   `isFleetWorkspace` in `frontend/src/renderer/types/workspace.ts`, projected
   into the sidebar by `frontend/src/renderer/hooks/useWorkspaceQuery.ts`; Prime
   lives in `backend/internal/service/prime`,
   `backend/internal/daemon/prime_supervisor.go`,
   `backend/internal/daemon/prime_reconciler.go`,
   `backend/internal/daemon/prime_relaunch.go`,
   `backend/internal/domain/prime_settings.go`,
   `backend/internal/httpd/controllers/prime.go`, and
   `backend/internal/cli/prime.go`; the mix itself is
   `backend/internal/domain/workermix.go`; pause/drain is
   `backend/internal/observe/drain/drain.go` with route coverage in
   `backend/internal/httpd/controllers/pause_routes_test.go`.
   Reference issues/PRs: Prime #7 → #45, #87; fleet-scoped Prime #92 → #95;
   Prime settings unification #99 → #103 and #167 → #184; Prime permission mode
   #163 → #189; pause/drain #5 → #33, #66; worker mix #3 → #17, #80.
6. **Persisted per-session model/effort/mix.** The launched-with model, effort,
   and mix-selection are stored on the session row rather than re-derived from
   project config, because `mixCensus` buckets live workers by
   `domain.BucketKey{Harness, Model, Effort}` — substituting `MixBucketModel` for
   the launched model on mix-selected sessions — and project config can change
   under a live session. Persisting only the model is not enough: dropping effort
   or the bucket model silently miscounts the census. Sync anchors: the four
   session fields `Model`, `Effort`, `MixSelected`, and `MixBucketModel` on
   `backend/internal/domain/session.go`; `mixCensus` in
   `backend/internal/session_manager/manager.go`; `BucketKey` in
   `backend/internal/domain/workermix.go`; and the four migrations
   `backend/internal/storage/sqlite/migrations/{0028_add_session_model,0029_add_session_mix_selected,0036_add_session_effort,0038_add_session_mix_bucket_model}.sql`.
   Note the fork's migration numbering has already diverged from upstream's,
   which is why a version collision is a sync STOP condition rather than a
   mechanical renumber. **The scratch-workspace adapter is currently identical to
   upstream's**, so it needs no preservation today. That is a claim about
   `backend/internal/adapters/workspace/scratch/` only — fork code elsewhere
   (spawn resolution, project-config validation, workspace routing, session
   management) does touch scratch projects — the derived `SessionPrefix` on the
   default scratch project is one such delta — so do not read this as "the fork
   never changes scratch". Re-establish it each sync rather than trusting this
   line, from inside the merge worktree so the comparison covers the tree you are
   about to land rather than the pre-merge `origin/main`:
   `git diff --exit-code upstream/main..HEAD -- backend/internal/adapters/workspace/scratch/`.
   Reference issues/PRs: worker mix #3 → #17, #80 — the session `model` and
   `mix_selected` columns landed in #17; session prefix #151 → #179.
7. **Bug fixes.** Any fork divergence that fixes a real bug beats re-absorbing
   the upstream behavior it fixed. The clusters most likely to be silently
   reverted by a blend, because they live in shared upstream files: compensating
   teardown must run on a detached context, not the caller's cancelled one
   (`cleanupContext` in `backend/internal/session_manager/manager.go`) — #156 →
   #158, #170 → #173, #175 → #188, #208 → #214; workspace/worktree teardown
   safety (`backend/internal/adapters/workspace/gitworktree/workspace.go` for the
   destroy guards, and `backend/internal/session_manager/workspace_ownership.go`
   plus migration
   `backend/internal/storage/sqlite/migrations/0049_session_worktrees_repo_path.sql`
   for teardown after repo deregistration) — #164 → #171, #165 → #187,
   #144 → #166; supervisor
   liveness accounting (`backend/internal/daemon/supervisor/supervisor.go`) —
   #147 → #159, #181 → #190; the tmux runtime socket anchored to the AO data dir
   (`backend/internal/adapters/runtime/tmux/tmux.go`) — #160 → #176; and session
   false-death / hook-attribution guards — #15 → #32, #91.
8. **Ops / SDLC infrastructure.** `ops/deploy.sh` + the web server + systemd /
   Tailscale wiring; the Prettier CI the fork keeps (upstream removed it); and
   the agent SDLC files (`CLAUDE.md`, the repo-carried `skills/`, Beads,
   OpenSpec, `agent-instructions/`). Sync anchors: the whole of `ops/` is
   fork-owned — `ops/deploy.sh`, the `ops/*.service` / `ops/*.timer` units, and
   their `ops/*.test.mjs` guards, of which `ops/ao-systemd-units.test.mjs` is the
   one that pins unit invariants; the pre-push gate is `scripts/ci/`; the Prettier
   CI is `.github/workflows/prettier.yml`; and the final-review status contract
   helper is `ops/final-review-status.mjs`. The `aong` porcelain has its own
   section below.
   Reference issues/PRs: headless standup #13 → #19; tmux socket #160 → #176;
   pre-push gate #105 → #108, #219 → #222, #227 → #228; build revision on the
   health probe #196, #200, #201 → #198; agent-ci workdir #169 → #172; and #52
   (the deploy script itself, opened without a tracking issue).

**Explicitly NOT fork-specific — absorb upstream freely** (do not spend a sync
preserving these; they were merged toward upstream and re-preserving them
creates conflicts): runtime-generation fencing and agent-exit detection (the
fork adopted upstream's `runtime_launch_id` / `AgentExitDetector`; the old
`RuntimeToken` generation-fence and `LaunchCommand` liveness sweep are gone);
worker-idle orchestrator nudges (removed, matching upstream); shell-terminal
session-scoping (upstream's model); and the UX shape of the inspector rail, the
reviews panel, the terminal tab strip, and mobile chrome (upstream's — the fork
requires only that these stay functional in the web client, per item 1).

**The landing site is vendored, not forked.** `frontend/src/landing/` is
upstream's marketing/docs app. The fork ships no code of its own there and does
not deploy it, so the tree is kept **byte-identical to upstream** and a sync
takes it wholesale (`git checkout MERGE_HEAD -- frontend/src/landing`). The one
exception is `frontend/src/landing/content/docs/configuration/projects.mdx`,
which carries the fork's model-management documentation; re-apply that file
after taking upstream's tree. It is listed in `.prettierignore` for the same
reason: reformatting a vendored app to this repo's Prettier config previously
conflicted on essentially every landing file on every sync, for no fork benefit.
A sync that finds a conflict under `frontend/src/landing/` outside that one
`.mdx` should suspect the tree has drifted from upstream again, not hand-merge
it. Note the landing app has its own dependency tree, and the renderer vitest
suite collects its script tests — see `scripts/ci/ci-local.sh`.

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
AO_ALLOWED_ORIGINS=app://renderer,https://ao.tailnet-name.ts.net ao daemon
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

The server lives on `$AO_DATA_DIR/run/tmux/default`, not tmux's default
`/tmp/tmux-$UID/default`. A session handle is app state, and a routine operator
`/tmp` sweep — the kind disk pressure forces — would otherwise orphan the whole
live fleet (issue #160). Neither unit hardcodes the resolved path: both derive
`-S` from their own `AO_DATA_DIR`, so only the `/run/tmux/default` suffix — the
layout of `tmux.SocketPath(AO_DATA_DIR)` in the Go adapter — is written out on
the systemd side. `ops/ao-systemd-units.test.mjs` asserts that every `-S` in
both units is derived and that the two units pin the same `AO_DATA_DIR`.

**Overriding `AO_DATA_DIR` means editing both units.** `ao-tmux.service` owns
the socket that `ao.service`'s daemon then connects to, so a drop-in
(`systemctl --user edit …`) applied to only one of them splits the pair: the
daemon uses one socket while the ownership gate probes another, and the gate
protects nothing. Apply the same override to `ao.service` and
`ao-tmux.service` together.

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
AO_TMUX_SOCKET="${AO_DATA_DIR:-$HOME/.ao/data}/run/tmux/default"
systemctl --user status ao.service ao-tmux.service
aong status
aong session ls
tmux -S "$AO_TMUX_SOCKET" list-sessions
systemctl --user restart ao.service
aong status
aong session ls
tmux -S "$AO_TMUX_SOCKET" list-sessions
```

A bare `tmux list-sessions` no longer shows AO sessions; always pass `-S`. The
daemon logs the resolved socket at startup under `tmux runtime socket`.

Expected result: the daemon comes back ready, existing tmux sessions are still
listed, and AO reattaches to live sessions instead of marking them dead after the
reaper window. A daemon with zero connected Electron/browser clients stays up:
the frontend-death watchdog is installed only when `AO_OWNER=app`, so a
persistent or headless daemon has no `supervise.sock` at all and nothing that
connects can arm it. On this fork's units `AO_OWNER` is unset, so the watchdog
is never installed; `journalctl --user -u ao.service` shows
`supervisor: not app-owned` at boot.

### Migrating off the legacy `/tmp` socket

Moving the socket does not migrate a tmux server already running on the old one,
so hosts that ran a pre-#160 daemon carry two servers for a while. Both sides
tolerate that deliberately: the runtime adapter falls back to tmux's default
socket for sessions it cannot find on AO's, and `ao.service`'s ownership gate
accepts a legacy-socket server so the deploy that moves the socket cannot brick
the daemon (`deploy.sh` restarts `ao.service` but cannot restart
`ao-tmux.service`, which sets `RefuseManualStop=yes`).

Sequence on a live host:

1. Deploy as usual. Existing panes keep running on `/tmp/tmux-$UID/default` and
   stay visible to AO through the fallback; new sessions land on
   `$AO_DATA_DIR/run/tmux/default`.
2. Get a server onto AO's socket under `ao-tmux.service` rather than lazily
   under the daemon — a host reboot is the clean way, since the unit refuses a
   manual stop. Until that happens the AO-socket server is forked by the daemon
   rather than owned by `ao-tmux.service`. `ops/deploy.sh` restarts
   `ao.service` unconditionally and that is still the supported deploy path:
   `KillMode=process` limits the stop job to the daemon's own PID, and tmux
   daemonizes away from it, so the restart is not expected to take the panes
   with it. The unit ownership is defence in depth for the cases
   `KillMode=process` does not cover, which is why the reboot is still worth
   doing rather than deferring indefinitely. Confirm with:

   ```bash
   systemctl --user show ao-tmux.service -p MainPID -p ExecMainStatus
   tmux -S "${AO_DATA_DIR:-$HOME/.ao/data}/run/tmux/default" list-sessions
   ```

3. Once no AO session remains on the legacy socket, retire it. `exit-empty` was
   pinned `off` on that server, so it will not exit on its own:

   ```bash
   tmux list-sessions        # must be empty of AO sessions
   tmux kill-server          # default socket only; AO's -S socket is untouched
   ```

4. With the legacy socket file gone, the adapter's fallback short-circuits on a
   single `stat` and issues no extra probes. At that point `socketFor` and
   `legacySocketPath` in
   `backend/internal/adapters/runtime/tmux/tmux.go`, and the bare
   `tmux list-sessions` branch of `ao.service`'s gate, can all be deleted.

Residual risks: a tmux server crash, `tmux kill-server`, `systemctl --user kill`,
host shutdown, direct PID signals, OOM selection, or user-manager teardown can
still kill panes. The units remove routine deploy/restart fleet kills; they do
not make tmux itself unkillable. On migration hosts whose tmux server was already
spawned by the daemon before these units existed, `KillMode=process` protects
daemon restarts, but full tmux cgroup ownership moves after reboot, server
replacement, or socket release.

## Lifecycle: the `aong` porcelain

`ao`'s lifecycle verbs were designed around a desktop client that owns the
daemon, and they no longer describe what they do on a web-first deployment:
`ao start` opens the Electron app rather than starting a daemon, `pause` is a
drain, `pause --hard` is an emergency stop filed as a flag on the
non-destructive verb, and `stop` stops only the daemon.

`aong` (`backend/cmd/aong`, entrypoint `backend/cmd/aong/main.go`) is the fork's
operator-facing surface over `ao`. Reference issues/PRs: #177 → #185 (the
original porcelain), #203 → #204 (widening it to the complete `ao` surface).
It passes every non-overridden `ao` command through by default, so operators do
not need to remember which binary has which verb. The override table is the
interesting part: lifecycle names that are misleading upstream become honest on
the fork, while the rest of `ao` stays available unchanged. It is deliberately
deletable: if upstream adopts the model, the override command definitions lift
into `ao`'s command tree and this binary goes away.

| Verb             | Composes                 | Effect                                                                     |
| ---------------- | ------------------------ | -------------------------------------------------------------------------- |
| `aong start`     | `systemctl --user start` | Starts the AO user units that are installed, in dependency order.          |
| `aong status`    | `ao status`              | Daemon state and fleet pause state, plus each loaded unit's active state.  |
| `aong doctor`    | `ao doctor`              | AO doctor checks, plus loaded `ao-web.service` / `ao-tmux.service` health. |
| `aong drain`     | `ao pause --all`         | Gate new work; live workers finish, then are terminated as they go idle.   |
| `aong stop-work` | `ao pause --all --hard`  | Terminate all live sessions now, including orchestrators and Prime.        |
| `aong resume`    | `ao resume --all`        | Restore intake and spawns.                                                 |
| `aong stop`      | `ao stop`                | Stop the daemon. Agent sessions keep running, unsupervised.                |
| `aong shutdown`  | stop-work, then stop     | The one verb that stops everything.                                        |
| every other verb | same `ao` argv           | Passthrough with stdin/stdout/stderr and exit code preserved.              |

Rules the implementation keeps:

- It couples only to `ao`'s public CLI and to `systemctl --user` for the
  service-unit operations `ao` has no commands for at all — starting a unit and
  reporting unit state, across `ao-tmux.service`, `ao.service`, and
  `ao-web.service`. It never imports
  daemon or `ao` CLI internals, never opens the run file or the shutdown token,
  and never calls the daemon HTTP API. A change that needs new behavior is an
  upstream `ao` proposal, not an `aong` feature.
- It resolves an executable `ao` beside its own binary before falling back to
  `PATH`, so the pair that was built together stays together. `ops/deploy.sh`
  builds and installs both and rolls both back together; a rollback to a release
  predating `aong` says so and leaves the installed binary alone rather than
  deleting a path it did not place there.
- `aong` is distributed by `ops/deploy.sh` only. The Electron desktop packaging
  is deliberately unchanged — the desktop app is exactly the `plain` environment
  where `aong` has no services to manage.
- `aong pause` is deliberately not an alias. `ao` has no capability that gates
  new work while leaving live workers alone — its soft pause _is_ the drain —
  so `aong pause` points operators at `aong drain` and `aong stop-work` instead
  of silently restoring the old misleading name.
- `aong --verbose <verb>` prints the underlying `ao` invocation for passthrough
  and wrapping overrides, or states plainly when an override diverges instead
  of wrapping an equivalent `ao` command. The flag belongs before the verb;
  flags after a passthrough verb are forwarded to `ao`.
- `aong shutdown` stops work before the daemon, and refuses to stop the daemon
  whenever stopping work failed — unconditionally, with no state that licenses
  an exception. Live agents with no supervisor is worse than a shutdown the
  operator can retry, and a shutdown that reports success in that state is worse
  still. Only `ao status`'s `stopped` (no run file at all) proves there is
  nothing to gate; `stale`, `unhealthy`, and `not_ready` are all reported for
  live daemons too. When shutdown refuses, its error names `aong stop`, which
  reconciles a daemon that is already gone without claiming to have stopped
  work.

Environment support:

- **Verified**: systemd user units on this fork's fleet, and the `plain`
  classification on a host with no `systemctl` on `PATH`.
- **Untested**: macOS and Windows, and any deployment where AO's units are
  installed system-wide rather than as user units. `aong start` fails with an
  explicit message on a `plain` host rather than inventing a supervision path of
  its own; it names `ao daemon` and `ao start` instead.
