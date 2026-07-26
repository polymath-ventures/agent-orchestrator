## Why

AO's lifecycle verbs were designed around a desktop client that owns the daemon,
and this fork is web-first. The result is that the verbs no longer describe what
they do: `ao start` starts no daemon (it downloads and opens the Electron app),
`pause` is a drain that terminates workers as they go idle, `pause --hard` is
really "kill all work" filed as a flag on the non-destructive verb, `stop` stops
the daemon but not the work, and **no single command stops everything** — the
operator runs four in sequence and has to remember the order.

Changing those verbs in `ao` means an argument with upstream, muscle-memory
breakage for existing users, and rebase pain on every sync. Almost all of the
capability already exists and is already exposed on `ao`'s public CLI; what is
missing is composition and honest naming. A separate porcelain binary gets the
correct model in front of operators now, on the real fleet, without touching a
line of upstream lifecycle code — and it is the evidence upstream would need to
adopt the model later.

## What Changes

- Add a second Go binary, `aong`, at `backend/cmd/aong`, built with Cobra like
  `ao`, and installed alongside `ao` by this fork's deploy path
  (`ops/deploy.sh`). The Electron desktop packaging is deliberately left
  unchanged: the desktop app is exactly the `plain` environment where `aong`
  has nothing to manage.
- `aong` couples **only** to `ao`'s public CLI surface (it shells out to the
  `ao` executable) and to `systemctl --user` for units `ao` does not know about
  (`ao-tmux`, `ao-web`). It never imports `backend/internal/...` daemon or CLI
  packages and never re-implements shutdown tokens, run-file handling, or
  systemd MainPID detection.
- Verb set, each a thin composition with help text that states plainly what
  survives:
  - `aong start` — start the local AO services.
  - `aong status` — daemon status, service-unit states, and fleet pause state.
  - `aong drain` — `ao pause --all`; gate new work, let live workers finish,
    terminate each as it reaches idle. The honest name for today's soft pause.
  - `aong resume` — `ao resume --all`; the way back from `drain` and
    `stop-work`.
  - `aong stop-work` — `ao pause --all --hard`; terminate all live work now.
  - `aong stop` — `ao stop`; stop the daemon and say plainly that agent
    sessions keep running.
  - `aong shutdown` — `stop-work`, then `stop`. The verb that does not exist
    today.
- `aong` detects its environment (systemd-managed fleet vs. a plain local
  daemon) rather than assuming this fork's layout, and reports which environment
  it detected so an operator on a different deployment can see why a command
  behaved as it did.
- Deliberately **not** shipping a separate `aong pause` verb: `ao` has no
  "gate new work but leave live workers alone" capability, so a `pause` that is
  distinct from `drain` would require new daemon behavior. Adding it here would
  make `aong` grow logic of its own, which is the one thing this design forbids.
  See design.md.
- No changes to `ao`'s commands, the systemd unit split, or daemon behavior.

## Capabilities

### New Capabilities

- `aong-lifecycle-cli`: A porcelain CLI over `ao` that presents a coherent,
  honest lifecycle verb set for a web-first deployment — start, status, drain,
  resume, stop-work, stop, shutdown — composed entirely from `ao`'s public CLI
  and `systemctl --user`, with environment detection so it runs outside this
  fork's layout.

### Modified Capabilities

<!-- None. No existing spec's requirements change: `ao`'s verbs, the fleet-pause
     semantics, and the daemon lifecycle all keep their current behavior. -->

## Impact

- **New**: `backend/cmd/aong/main.go` and a new `backend/internal/aongcli`
  package (Cobra commands, environment detection, `ao`/`systemctl` invocation
  seam for tests).
- **Deploy**: `ops/deploy.sh` builds `aong` and installs it next to `ao`, and
  rolls both back together. Electron packaging (`frontend/scripts/build-daemon.mjs`,
  Forge `extraResource`) is unchanged.
- **Docs**: `docs/fork.md` gains a short section describing `aong`, the verb
  set, and which environments are verified versus untested.
- **Unchanged**: `backend/internal/cli` (all `ao` commands), `ops/*.service`,
  the daemon, the HTTP API, and the frontend. No API contract change, so no
  `npm run api` regeneration.
