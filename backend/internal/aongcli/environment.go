package aongcli

import (
	"context"
	"fmt"
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
//
// A probe that fails is reported, never silently downgraded to "plain": a
// missing user bus or a permission failure on a real systemd host would
// otherwise make the whole deployment look absent, which is the difference
// between "there is nothing to start here" and "I could not ask".
func (c *commandContext) detectEnvironment(ctx context.Context) (environment, error) {
	systemctl := c.findSystemctl()
	if systemctl == "" {
		return environment{Kind: envPlain}, nil
	}

	var loaded []string
	for _, unit := range aoUnits {
		state, err := c.unitProperty(ctx, systemctl, unit, "LoadState")
		if err != nil {
			return environment{}, err
		}
		if state == "not-found" {
			continue
		}
		loaded = append(loaded, unit)
	}
	if len(loaded) == 0 {
		return environment{Kind: envPlain}, nil
	}
	return environment{Kind: envSystemd, LoadedUnits: loaded, systemctl: systemctl}, nil
}

// findSystemctl returns systemctl's path, or "" when there is none. A host
// without systemctl is not an error — it is the `plain` environment — so the
// lookup failure is deliberately not propagated. This is the only probe failure
// that is safe to read as an answer, because "no service manager" is itself the
// answer.
func (c *commandContext) findSystemctl() string {
	path, err := c.deps.LookPath("systemctl")
	if err != nil {
		return ""
	}
	return path
}

// unitProperty reads one systemd unit property. `systemctl show` reports
// LoadState=not-found for a unit that does not exist and still exits 0, so a
// non-zero exit is a real probe failure rather than an absent unit.
func (c *commandContext) unitProperty(ctx context.Context, systemctl, unit, property string) (string, error) {
	out, err := c.deps.RunCommand(ctx, systemctl, "--user", "show", unit, "-P", property)
	if err != nil {
		return "", fmt.Errorf("systemctl --user show %s -P %s: %w%s", unit, property, err, indentedOutput(out))
	}
	return strings.TrimSpace(string(out)), nil
}
