# Prime supervisor

Status: daemon-global runtime feature

AO can run one optional fleet-level `prime` session. Prime is a daemon-spawned
supervisor that watches across projects and coaches operators or orchestrators,
but it is not a recovery rung and does not dispatch tickets, merge pull
requests, or command worker sessions directly.

## Enabling

Prime is disabled by default and is controlled by persisted daemon settings,
not by a host project. Use global Settings in the desktop app or the CLI:

```bash
ao prime enable --agent codex --permission bypass-permissions --name "AO Prime" --wake-interval 15m
ao prime set --permission accept-edits
ao prime settings
ao prime disable
```

The settings are stored under the daemon data directory and include enablement,
display name, harness, model, effort, permission mode, inline rules, rules file,
wake interval, and wake backoff policy. Legacy `AO_PRIME_PROJECT_ID` and
`AO_PRIME_DISPLAY_NAME` environment variables do not enable Prime, choose a
project, or appear in Prime settings responses.

## Lifecycle

The daemon starts the supervisor loop after boot reconciliation. On every tick
it reads persisted Prime settings and reconciles the singleton:

- disabled settings: retire any active Prime and stop ensure, replacement, and
  wake attempts;
- enabled with no active Prime: spawn one in an AO-managed projectless fleet
  workspace;
- active healthy Prime: leave it alone;
- active Prime stuck `no_signal` or `exited` for at least five minutes: retire
  all active Primes and spawn a clean replacement;
- active idle or waiting-input Prime: send a bounded wake message so it can
  continue supervising;
- repeated unhealthy replacement: stop after three replacement attempts per
  hour, then create a `prime_restart_capped` notification.

Storage owns the singleton invariant with a partial unique index over active
Prime sessions, so concurrent daemon loops cannot create two live Primes.

Wake nudges require at least one observed activity signal for the current Prime,
so the daemon does not auto-answer a session whose hook pipeline has never
checked in. Fleet pause suppresses wake nudges but does not suppress
unhealthy-Prime replacement. Project pause no longer affects Prime because
Prime is not project-owned.

## Settings shape

The API shape for `PUT /api/v1/prime/settings` is:

```json
{
	"settings": {
		"enabled": true,
		"displayName": "AO Prime",
		"agent": "codex",
		"agentConfig": {
			"model": "gpt-5.4",
			"effort": "high",
			"permissions": "bypass-permissions"
		},
		"rules": "Keep fleet-wide state concise.",
		"rulesFile": "docs/prime-rules.md",
		"wakeInterval": "15m",
		"wakeBackoff": {
			"enabled": true,
			"base": "15m",
			"max": "1h"
		}
	}
}
```

`wakeInterval` controls the first eligible wake after idle or waiting-input
activity. `wakeBackoff` controls repeat wake spacing; omit it for exponential
backoff from `wakeInterval` up to one hour, or set `enabled: false` to keep
fixed-interval nudges.

`ao prime prompt` and `GET /api/v1/prime/prompt` show the effective fleet Prime
prompt, including global Prime rules.
