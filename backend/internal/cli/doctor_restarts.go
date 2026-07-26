package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// A read-only diagnostic for one of the fallouts of #147: systemd restart churn
// on the unit that owns the daemon.
//
// It is strictly observational. Every subprocess is a read-only probe run
// through c.deps.CommandOutput, and the check never touches the supervisor
// stream socket: an HTTP request against supervise.sock is what perturbs daemon
// lifecycle in the first place.
//
// The check cannot fail doctor. An absent systemctl, a non-systemd host, an
// unreadable procfs, or any probe error means the signal is unavailable, not
// that the machine is unhealthy.

const (
	// defaultProcRoot is the procfs mount the daemon's cgroup is read from.
	defaultProcRoot = "/proc"

	// daemonRestartChurnThreshold is the NRestarts count above which the unit is
	// churning rather than having been restarted for an ordinary upgrade.
	daemonRestartChurnThreshold = 3
)

// systemdServiceUnitRE matches a systemd service unit name as a WHOLE cgroup
// path component (v1 `1:name=systemd:/system.slice/ao.service` and v2
// `0::/system.slice/ao.service`). Anchored so a directory that merely contains
// the text — say `not-a.service.d` — cannot pose as a unit.
//
// The class covers systemd's own unit-name alphabet, including `:` and the
// backslash of `\xNN` escapes; leaving those out would silently miss a real
// unit and report the check unavailable. A `:` inside the name is safe here
// because the cgroup line is split on its first two colons before this runs,
// so the path — colons and all — survives intact.
var systemdServiceUnitRE = regexp.MustCompile(`^[A-Za-z0-9_.@:\\-]+\.service$`)

// checkDaemonRestarts surfaces systemd restart churn for the unit that actually
// owns the daemon. The unit is derived from the daemon pid's cgroup rather than
// assumed, so a dev unit, a differently named deployment, or a daemon started
// outside systemd all report honestly instead of asking systemd about a unit
// that is not the one running.
func (c *commandContext) checkDaemonRestarts(ctx context.Context, daemonPID int) doctorCheck {
	const name = "daemon-restarts"
	pass := func(msg string) doctorCheck {
		return doctorCheck{Level: doctorPass, Section: doctorSectionCore, Name: name, Message: msg}
	}

	systemctlPath, err := c.deps.LookPath("systemctl")
	if err != nil || systemctlPath == "" {
		return pass("skipped: systemctl not found in PATH, so restart counts are unavailable")
	}
	if daemonPID == 0 {
		return pass("unavailable: the daemon is not running, so its systemd unit cannot be derived")
	}
	unit, userScope, err := c.daemonSystemdUnit(daemonPID)
	if err != nil {
		return pass(fmt.Sprintf("unavailable: could not derive a systemd unit for daemon pid %d (%v)", daemonPID, err))
	}

	// The scope comes from the same cgroup path as the unit name, so the query
	// always reaches the manager that owns the pid. That matters because
	// `systemctl show` answers 0 for a unit it does not know: asking the system
	// manager about a user unit would report a healthy zero instead of the real
	// restart count.
	args := []string{"show"}
	if userScope {
		args = append(args, "--user")
	}
	args = append(args, unit, "-p", "NRestarts", "--value")

	reqCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	out, err := c.deps.CommandOutput(reqCtx, systemctlPath, args...)
	if err != nil {
		return pass(fmt.Sprintf("unavailable: `systemctl %s` failed (%v)", strings.Join(args, " "), err))
	}
	raw := firstOutputLine(out)
	restarts, err := strconv.Atoi(raw)
	if err != nil {
		return pass(fmt.Sprintf("unavailable: %s reported an unparseable NRestarts value (%q)", unit, raw))
	}
	if restarts > daemonRestartChurnThreshold {
		// Single-quote the unit: systemd names may contain `\xNN` escapes, and an
		// unquoted backslash is eaten by the shell the operator pastes this into,
		// so the suggested command would query a unit that does not exist.
		journal := "journalctl -u '" + unit + "'"
		if userScope {
			journal = "journalctl --user -u '" + unit + "'"
		}
		return doctorCheck{
			Level: doctorWarn, Section: doctorSectionCore, Name: name,
			Message: fmt.Sprintf("%s has restarted %d time(s) (more than %d) — the daemon is being restarted repeatedly; check `%s` for self-shutdowns", unit, restarts, daemonRestartChurnThreshold, journal),
		}
	}
	return pass(fmt.Sprintf("%s has restarted %d time(s)", unit, restarts))
}

// daemonSystemdUnit extracts the systemd service unit owning a pid from its
// cgroup membership, plus whether that unit belongs to a per-user manager. The
// deepest `*.service` on the path wins, which is the unit the process actually
// belongs to when service cgroups nest; an enclosing `user@<uid>.service` on the
// same path is systemd's own marker that the owning manager is `systemd --user`.
func (c *commandContext) daemonSystemdUnit(pid int) (unit string, userScope bool, err error) {
	path := filepath.Join(c.deps.ProcRoot, strconv.Itoa(pid), "cgroup")
	data, err := os.ReadFile(path) //nolint:gosec // procfs path built from the daemon pid AO already tracks
	if err != nil {
		return "", false, err
	}
	// Only the line systemd itself owns may name the unit. A cgroup file is
	// `hierarchy-ID:controller-list:path` per line; v2 uses the single `0::`
	// line, and v1 hosts add one line per controller. Scanning every line and
	// letting the last `.service` win lets an unrelated controller hierarchy
	// choose the unit — or silently reset the user/system scope — so the
	// restart count would be read from the wrong unit or the wrong manager.
	for line := range strings.SplitSeq(string(data), "\n") {
		_, rest, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		controllers, cgroupPath, ok := strings.Cut(rest, ":")
		if !ok {
			continue
		}
		// v2: empty controller list. v1: only the systemd-owned hierarchy.
		if controllers != "" && controllers != "name=systemd" {
			continue
		}
		// Match whole path components so a directory that merely contains the
		// text cannot pose as a unit.
		var components []string
		for _, component := range strings.Split(cgroupPath, "/") {
			if systemdServiceUnitRE.MatchString(component) {
				components = append(components, component)
			}
		}
		if len(components) == 0 {
			continue
		}
		// The deepest component is the unit the pid actually belongs to; an
		// enclosing user@<uid>.service is systemd's marker for a user manager.
		unit = components[len(components)-1]
		userScope = false
		for _, enclosing := range components[:len(components)-1] {
			if strings.HasPrefix(enclosing, "user@") {
				userScope = true
			}
		}
	}
	if unit == "" {
		return "", false, fmt.Errorf("no systemd service unit in %s", path)
	}
	return unit, userScope, nil
}
