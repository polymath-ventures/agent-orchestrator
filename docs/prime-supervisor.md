# Prime supervisor

Status: daemon-gated runtime feature

AO can run one optional fleet-level `prime` session. Prime is a daemon-spawned
supervisor that watches across projects and coaches operators or orchestrators,
but it is not a recovery rung and does not dispatch tickets, merge pull
requests, or command worker sessions directly.

## Enabling

Prime is disabled by default. Set these environment variables before starting
the daemon:

```bash
AO_PRIME_PROJECT_ID=ao
AO_PRIME_DISPLAY_NAME="AO Prime"
```

`AO_PRIME_PROJECT_ID` is the registered project whose repository, config, role
override, and prime rules host the prime session. `AO_PRIME_DISPLAY_NAME` is
optional, trimmed, and capped at 20 runes.

Changing `AO_PRIME_PROJECT_ID` does not move an already healthy active prime.
The new project is used for the next fresh spawn or clean replacement.

## Lifecycle

The daemon starts a small supervisor loop only when `AO_PRIME_PROJECT_ID` is
set. After boot reconciliation, the loop ensures that exactly one active prime
exists across the fleet:

- no active prime: spawn one in the configured project;
- active healthy prime: leave it alone;
- active prime stuck `no_signal` or `exited` for at least five minutes: retire
  all active primes and spawn a clean replacement;
- active idle or waiting-input prime: send a bounded wake message so it can
  continue supervising;
- repeated unhealthy replacement: stop after three replacement attempts per
  hour, then create a `prime_restart_capped` notification for the active prime.

Storage owns the singleton invariant with a partial unique index over active
prime sessions, so concurrent daemon loops cannot create two live primes.

Wake nudges require at least one observed activity signal for the current
prime, so the daemon does not auto-answer a session whose hook pipeline has
never checked in. Pausing the host project or the fleet suppresses wake nudges
but does not suppress unhealthy-prime replacement; prime survives pause, while
work-nudging honors pause.

## Project config

Prime uses the same role override shape as workers and orchestrators:

```json
{
	"prime": {
		"agent": "codex",
		"agentConfig": {
			"model": "gpt-5.4"
		},
		"wakeInterval": "15m",
		"wakeBackoff": {
			"enabled": true,
			"base": "15m",
			"max": "1h"
		}
	},
	"primeRules": "Keep fleet-wide state concise.",
	"primeRulesFile": "docs/prime-rules.md"
}
```

`prime.wakeInterval` controls the first eligible wake after idle or
waiting-input activity. `prime.wakeBackoff` controls repeat wake spacing; omit
it for exponential backoff from `wakeInterval` up to one hour, or set
`enabled: false` to keep fixed-interval nudges.

`ao role prompt <project> prime` shows the effective prime prompt, including
project-specific prime rules.
