package cli

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/runtime/tmux"
)

// Two read-only diagnostics for the fallout of #147: a leaked long-lived
// process squatting in a pane AO owns, and systemd restart churn on the unit
// that owns the daemon.
//
// Both are strictly observational. Every subprocess is a read-only probe run
// through c.deps.CommandOutput, and neither check ever touches the supervisor
// stream socket: an HTTP request against supervise.sock is what perturbs daemon
// lifecycle in the first place, so session inventory comes from the daemon's
// ordinary loopback HTTP API instead.
//
// Neither check can fail doctor. An absent tmux, an absent systemctl, a
// non-systemd host, an unreadable procfs, or any probe error means the signal
// is unavailable, not that the machine is unhealthy.

const (
	// defaultProcRoot is the procfs mount the daemon's cgroup is read from.
	defaultProcRoot = "/proc"

	// paneProcessStaleAfter is how long a non-agent foreground process may sit
	// in a pane AO owns before doctor calls it stale. The reported leak was an
	// 8-hour curl; an hour is well clear of the short-lived helpers (git, rg,
	// npm) an agent legitimately runs in the foreground.
	paneProcessStaleAfter = time.Hour

	// daemonRestartChurnThreshold is the NRestarts count above which the unit is
	// churning rather than having been restarted for an ordinary upgrade.
	daemonRestartChurnThreshold = 3
)

// systemdServiceUnitRE matches a systemd service unit name inside a cgroup path
// (v1 `1:name=systemd:/system.slice/ao.service` and v2 `0::/system.slice/ao.service`).
var systemdServiceUnitRE = regexp.MustCompile(`[A-Za-z0-9_.@-]+\.service`)

// checkAgentProcesses warns about long-lived processes in AO's own panes that
// are not the agent AO launched there — the class the reporter hit, an 8-hour
// curl left behind in a session pane.
//
// The population is derived from AO's data, not a maintained list: the daemon
// says which sessions exist, tmux.SessionName says what their tmux sessions are
// called, and doctorHarnesses says which binary each session's harness runs.
// Panes of tmux sessions AO does not own are ignored outright.
func (c *commandContext) checkAgentProcesses(ctx context.Context) doctorCheck {
	const name = "agent-processes"
	pass := func(msg string) doctorCheck {
		return doctorCheck{Level: doctorPass, Section: doctorSectionAgents, Name: name, Message: msg}
	}

	tmuxPath, err := c.deps.LookPath("tmux")
	if err != nil || tmuxPath == "" {
		return pass("skipped: tmux not found in PATH, so AO's panes cannot be inventoried")
	}
	psPath, err := c.deps.LookPath("ps")
	if err != nil || psPath == "" {
		return pass("skipped: ps not found in PATH, so pane process age is unavailable")
	}

	agentByTmuxSession, err := c.paneAgentBinaries(ctx)
	if err != nil {
		return pass(fmt.Sprintf("unavailable: could not list AO sessions (%v)", err))
	}
	if len(agentByTmuxSession) == 0 {
		return pass("no AO sessions to inspect")
	}

	panes, err := c.listTmuxPanes(ctx, tmuxPath)
	if err != nil {
		return pass(fmt.Sprintf("unavailable: `tmux list-panes` failed (%v)", err))
	}

	type candidate struct {
		pid     int
		command string
		session string
	}
	candidates := []candidate{}
	ownedPanes := 0
	for _, pane := range panes {
		agent, owned := agentByTmuxSession[pane.tmuxSession]
		if !owned {
			continue
		}
		ownedPanes++
		if pane.command == agent {
			continue
		}
		candidates = append(candidates, candidate{pid: pane.pid, command: pane.command, session: pane.tmuxSession})
	}
	if len(candidates) == 0 {
		return pass(fmt.Sprintf("no stale non-agent processes in %d AO pane(s)", ownedPanes))
	}

	pids := make([]int, 0, len(candidates))
	for _, cand := range candidates {
		pids = append(pids, cand.pid)
	}
	elapsed, err := c.processElapsed(ctx, psPath, pids)
	if err != nil {
		return pass(fmt.Sprintf("unavailable: `ps` elapsed-time probe failed (%v)", err))
	}

	stale := []string{}
	for _, cand := range candidates {
		age, ok := elapsed[cand.pid]
		if !ok || age <= paneProcessStaleAfter {
			continue
		}
		stale = append(stale, fmt.Sprintf("pid %d (%s) in session %s for %s", cand.pid, cand.command, cand.session, formatUptime(age)))
	}
	if len(stale) == 0 {
		return pass(fmt.Sprintf("no non-agent pane process older than %s", formatUptime(paneProcessStaleAfter)))
	}
	return doctorCheck{
		Level: doctorWarn, Section: doctorSectionAgents, Name: name,
		Message: fmt.Sprintf("%d stale process(es) in AO panes, none of them the session's agent: %s — the pane is occupied, so the agent cannot be driven until it exits", len(stale), strings.Join(stale, "; ")),
	}
}

// paneAgentBinaries maps each live session's tmux session name to the agent
// binary AO launched in it. Sessions whose harness has no entry in
// doctorHarnesses are left out: without the expected binary there is nothing to
// compare a pane command against, and guessing would manufacture findings.
//
// The listing goes over the daemon's ordinary loopback HTTP API — never the
// supervisor stream socket — and getJSON already turns a stopped daemon into a
// clean "not running" error.
func (c *commandContext) paneAgentBinaries(ctx context.Context) (map[string]string, error) {
	params := url.Values{}
	params.Set("active", "true")
	var res sessionListResponse
	reqCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	if err := c.getJSON(reqCtx, apiPath("sessions", params), &res); err != nil {
		return nil, err
	}

	binaryByHarness := make(map[string]string, len(doctorHarnesses))
	for _, harness := range doctorHarnesses {
		binaryByHarness[harness.Name] = harness.BinaryName
	}

	agents := make(map[string]string, len(res.Sessions))
	for _, sess := range res.Sessions {
		if sess.ID == "" || sess.IsTerminated {
			continue
		}
		binary, ok := binaryByHarness[sess.Harness]
		if !ok {
			continue
		}
		agents[tmux.SessionName(sess.ID)] = binary
	}
	return agents, nil
}

// doctorPaneRow is one row of the read-only `tmux list-panes` inventory.
type doctorPaneRow struct {
	tmuxSession string
	pid         int
	command     string
}

func (c *commandContext) listTmuxPanes(ctx context.Context, tmuxPath string) ([]doctorPaneRow, error) {
	reqCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	out, err := c.deps.CommandOutput(reqCtx, tmuxPath, "list-panes", "-a", "-F", "#{session_name} #{pane_pid} #{pane_current_command}")
	if err != nil {
		return nil, err
	}
	rows := []doctorPaneRow{}
	for line := range strings.SplitSeq(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		pid, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		rows = append(rows, doctorPaneRow{tmuxSession: fields[0], pid: pid, command: fields[2]})
	}
	return rows, nil
}

// processElapsed reads how long each pid has been running from `ps etimes`.
// Elapsed time comes from the kernel's own accounting rather than a wall-clock
// comparison, so the answer does not depend on doctor's notion of "now".
func (c *commandContext) processElapsed(ctx context.Context, psPath string, pids []int) (map[int]time.Duration, error) {
	list := make([]string, 0, len(pids))
	for _, pid := range pids {
		list = append(list, strconv.Itoa(pid))
	}
	reqCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	out, err := c.deps.CommandOutput(reqCtx, psPath, "-o", "pid=,etimes=", "-p", strings.Join(list, ","))
	if err != nil {
		return nil, err
	}
	elapsed := make(map[int]time.Duration, len(pids))
	for line := range strings.SplitSeq(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		secs, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		elapsed[pid] = time.Duration(secs) * time.Second
	}
	return elapsed, nil
}

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
		journal := "journalctl -u " + unit
		if userScope {
			journal = "journalctl --user -u " + unit
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
	for line := range strings.SplitSeq(string(data), "\n") {
		matches := systemdServiceUnitRE.FindAllString(line, -1)
		if len(matches) == 0 {
			continue
		}
		unit = matches[len(matches)-1]
		userScope = false
		for _, enclosing := range matches[:len(matches)-1] {
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
