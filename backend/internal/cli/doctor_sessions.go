package cli

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// A read-only diagnostic for the question `ao doctor` could not previously
// answer: is a session wedged?
//
// An earlier attempt walked each pane's process tree to find what was holding
// the foreground. It could not work, and the reason is structural rather than
// an implementation flaw (#178): AO launches every pane as
// `sh -c '<agent argv>; exec "${SHELL:-/bin/sh}" -i'`, so the pane pid is a
// wrapping shell; ppid establishes ancestry, not foreground ownership, so a
// leaked `curl` and a healthy long-lived MCP server look identical; and a
// non-interactive `sh -c` enables no job control, so the wrapper, the agent,
// and every grandchild share one process group.
//
// The operator's question was never "what process is running" but "has this
// agent done anything lately", and the daemon records that for certain. This
// check reads that record and nothing else: no `ps`, no `tmux`, no process
// inspection, and therefore no platform-specific behavior to get wrong.

// wedgedSessionSilence is how long a session the daemon believes is ACTIVE may
// record no activity before doctor mentions it.
//
// It is deliberately hours, not minutes. Activity is recorded per agent hook,
// so an agent that is really working refreshes it constantly, while daemon
// downtime silently drops hooks — doctor already suppresses those as
// restart-window noise. A threshold far longer than any restart window keeps
// that gap from producing false warnings. The reported case was an agent stuck
// for eight hours.
const wedgedSessionSilence = 4 * time.Hour

// checkWedgedSessions warns about live sessions that have gone silent.
//
// It cannot fail doctor. An unreachable daemon means the signal is unavailable,
// not that the machine is unhealthy — and the `daemon` check already reports a
// daemon that is down, so failing here would turn one problem into two.
func (c *commandContext) checkWedgedSessions(ctx context.Context) doctorCheck {
	const name = "sessions-idle"
	report := func(level doctorLevel, msg string) doctorCheck {
		return doctorCheck{Level: level, Section: doctorSectionCore, Name: name, Message: msg}
	}

	// Bound this like every other doctor probe. getJSON raises the client
	// timeout to the two-minute command timeout, which is right for a spawn but
	// would let one slow daemon stall the whole report before the `daemon`
	// check even runs.
	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	// Scope the listing to live sessions: a fleet with a long terminated
	// history should not pay to transfer it.
	params := url.Values{}
	params.Set("active", "true")
	var res sessionListResponse
	if err := c.getJSON(probeCtx, apiPath("sessions", params), &res); err != nil {
		return report(doctorPass, "unavailable: could not read the session list ("+err.Error()+")")
	}

	now := c.deps.Now()
	var silent []string
	for _, session := range res.Sessions {
		// The `active=true` query already excludes terminated sessions. This
		// keeps the check's own output correct if that server-side scoping ever
		// changes, which is cheaper than discovering it through a wrong warning.
		if session.IsTerminated {
			continue
		}
		// Only a session the daemon believes is ACTIVE can be wedged. idle
		// means the agent stopped; waiting_input and blocked mean it is paused
		// on the user, and an operator legitimately leaves those overnight.
		// Warning on them would make this check noise an operator learns to
		// ignore — and the state is the daemon's own record, so using it is
		// reading the owner of the fact, not guessing.
		if session.Activity.State != string(domain.ActivityActive) {
			continue
		}
		// Silence is measured from a starting point. A session that has never
		// recorded activity has none, and reading "never" as "infinitely
		// silent" would warn on every session created before its first hook
		// lands.
		if session.Activity.LastActivityAt.IsZero() {
			continue
		}
		quiet := now.Sub(session.Activity.LastActivityAt)
		if quiet < wedgedSessionSilence {
			continue
		}
		silent = append(silent, fmt.Sprintf("%s (active, silent %s)", session.ID, formatUptime(quiet)))
	}

	if len(silent) == 0 {
		return report(doctorPass, fmt.Sprintf("no active session has been silent for over %s", wedgedSessionSilence))
	}
	sort.Strings(silent)
	return report(doctorWarn, fmt.Sprintf(
		"%d session(s) the daemon believes are active have recorded no activity for over %s — they may be wedged: %s; inspect with `ao session get <id>` and end one with `ao session kill <id>`",
		len(silent), wedgedSessionSilence, strings.Join(silent, ", ")))
}
