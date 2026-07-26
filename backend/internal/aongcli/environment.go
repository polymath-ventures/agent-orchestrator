package aongcli

import (
	"context"
	"strings"
)

// aoUnits are the systemd user units an AO deployment can be made of, in start
// order: the tmux server that owns every agent pane, the daemon, then the web
// supervisor. `ao` already knows about ao.service; the other two are the only
// reason aong talks to systemctl at all.
var aoUnits = []string{"ao-tmux.service", "ao.service", "ao-web.service"}

type environmentKind string

const (
	envSystemd environmentKind = "systemd"
	envPlain   environmentKind = "plain"
)

// environment is what aong detected about how AO is managed on this host. It is
// derived at run time from the service manager rather than from any assumption
// about a particular deployment's file layout.
type environment struct {
	Kind        environmentKind
	LoadedUnits []string
	systemctl   string
}

// detectEnvironment asks the user service manager which AO units it knows
// about. Asking systemd is asking the component that owns the fact; probing for
// unit files on disk would both miss package-installed units and see units that
// were never loaded.
func (c *commandContext) detectEnvironment(ctx context.Context) environment {
	systemctl, err := c.deps.LookPath("systemctl")
	if err != nil {
		return environment{Kind: envPlain}
	}

	var loaded []string
	for _, unit := range aoUnits {
		if c.unitLoadState(ctx, systemctl, unit) == "not-found" {
			continue
		}
		loaded = append(loaded, unit)
	}
	if len(loaded) == 0 {
		return environment{Kind: envPlain}
	}
	return environment{Kind: envSystemd, LoadedUnits: loaded, systemctl: systemctl}
}

// unitLoadState returns the unit's LoadState, or "not-found" when systemd
// cannot report one — an unreadable answer is indistinguishable from an absent
// unit for our purposes, and treating it as present would make `aong start`
// fail on a unit that does not exist.
func (c *commandContext) unitLoadState(ctx context.Context, systemctl, unit string) string {
	out, err := c.deps.RunCommand(ctx, systemctl, "--user", "show", unit, "-P", "LoadState")
	if err != nil {
		return "not-found"
	}
	state := strings.TrimSpace(string(out))
	if state == "" {
		return "not-found"
	}
	return state
}

// unitActiveState returns the unit's ActiveState for reporting, or "unknown".
func (c *commandContext) unitActiveState(ctx context.Context, systemctl, unit string) string {
	out, err := c.deps.RunCommand(ctx, systemctl, "--user", "show", unit, "-P", "ActiveState")
	if err != nil {
		return "unknown"
	}
	state := strings.TrimSpace(string(out))
	if state == "" {
		return "unknown"
	}
	return state
}
