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
	// npm) an agent legitimately runs in the foreground. It is thresholded
	// against that process's own age, never the pane's — a pane that has been up
	// all day is normal, and says nothing about what is running in it now.
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
// Panes of tmux sessions AO does not own are ignored outright — their ttys are
// never even passed to the ps probe.
//
// Identity comes from a walk down the pane's process tree, not from
// `#{pane_pid}` or `#{pane_current_command}`. tmux.buildLaunchCommand launches
// every pane as `sh -c '<setup>; <agent argv>; exec "${SHELL:-/bin/sh}" -i'`,
// which makes those two fields describe different processes: `#{pane_pid}` is
// the wrapping shell and the agent (plus anything the agent spawns) is a
// descendant of it. Thresholding the pane's own age would flag a pane that has
// simply been open a long time, and reporting `#{pane_pid}` would hand the
// operator a pid whose kill destroys the pane rather than the squatter.
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

	owned := []doctorPaneRow{}
	ttys := []string{}
	seenTTY := map[string]bool{}
	for _, pane := range panes {
		if _, isOwned := agentByTmuxSession[pane.tmuxSession]; !isOwned {
			continue
		}
		owned = append(owned, pane)
		if pane.tty != "" && !seenTTY[pane.tty] {
			seenTTY[pane.tty] = true
			ttys = append(ttys, pane.tty)
		}
	}
	if len(owned) == 0 || len(ttys) == 0 {
		return pass("no AO panes to inspect")
	}

	tree, err := c.paneProcessTable(ctx, psPath, ttys)
	if err != nil {
		return pass(fmt.Sprintf("unavailable: `ps` pane process probe failed (%v)", err))
	}

	stale := []string{}
	for _, pane := range owned {
		fg, running := tree.foreground(pane.panePID)
		switch {
		case !running:
			// Nothing below the pane's own process: the launch command's trailing
			// `exec` has replaced the wrapper with the interactive shell it is
			// designed to rest at. An idle pane is not a squatter.
			continue
		case fg.comm == agentByTmuxSession[pane.tmuxSession]:
			// The agent AO launched for this session, doing its job.
			continue
		case fg.elapsed <= paneProcessStaleAfter:
			// A helper the agent is legitimately running right now.
			continue
		}
		stale = append(stale, fmt.Sprintf("pid %d (%s) in session %s for %s", fg.pid, fg.comm, pane.tmuxSession, formatUptime(fg.elapsed)))
	}
	if len(stale) == 0 {
		return pass(fmt.Sprintf("%d AO pane(s), no foreground process older than %s that is not the session's agent", len(owned), formatUptime(paneProcessStaleAfter)))
	}
	return doctorCheck{
		Level: doctorWarn, Section: doctorSectionAgents, Name: name,
		Message: fmt.Sprintf("%d stale process(es) holding the foreground in AO panes: %s — none is that session's agent, so the agent cannot be driven until it exits", len(stale), strings.Join(stale, "; ")),
	}
}

// paneAgentBinaries maps each live session's tmux session name to the agent
// binary AO launched in it. Sessions whose harness has no entry in
// doctorHarnesses are left out: without the expected binary there is nothing to
// compare the pane's foreground process against, and guessing would manufacture
// findings.
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
// panePID is `#{pane_pid}`: the shell tmux launched, which is the root of the
// pane's process tree rather than whatever is currently in the foreground.
type doctorPaneRow struct {
	tmuxSession string
	panePID     int
	tty         string
}

func (c *commandContext) listTmuxPanes(ctx context.Context, tmuxPath string) ([]doctorPaneRow, error) {
	reqCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	out, err := c.deps.CommandOutput(reqCtx, tmuxPath, "list-panes", "-a", "-F", "#{session_name} #{pane_pid} #{pane_tty}")
	if err != nil {
		return nil, err
	}
	rows := []doctorPaneRow{}
	for line := range strings.SplitSeq(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		panePID, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		// ps -t wants the tty without the /dev/ prefix tmux reports.
		rows = append(rows, doctorPaneRow{
			tmuxSession: fields[0],
			panePID:     panePID,
			tty:         strings.TrimPrefix(fields[2], "/dev/"),
		})
	}
	return rows, nil
}

// paneProcess is one process living on an AO pane's tty.
type paneProcess struct {
	pid     int
	comm    string
	elapsed time.Duration
}

// paneProcessTree is the process table of AO's pane ttys, indexed so a pane's
// own pid can be walked down to whatever is actually in its foreground.
type paneProcessTree struct {
	byPID    map[int]paneProcess
	children map[int][]int
}

// paneProcessTable reads the processes on the given ttys. Elapsed time comes
// from the kernel's own accounting (`etimes`), so the answer never depends on
// doctor's notion of "now". Restricting the probe to AO's pane ttys is what
// keeps foreign panes out of the picture entirely.
func (c *commandContext) paneProcessTable(ctx context.Context, psPath string, ttys []string) (paneProcessTree, error) {
	reqCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	out, err := c.deps.CommandOutput(reqCtx, psPath, "-o", "pid=,ppid=,etimes=,comm=", "-t", strings.Join(ttys, ","))
	if err != nil {
		return paneProcessTree{}, err
	}
	tree := paneProcessTree{byPID: map[int]paneProcess{}, children: map[int][]int{}}
	for line := range strings.SplitSeq(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		ppid, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		secs, err := strconv.Atoi(fields[2])
		if err != nil {
			continue
		}
		tree.byPID[pid] = paneProcess{pid: pid, comm: strings.Join(fields[3:], " "), elapsed: time.Duration(secs) * time.Second}
		tree.children[ppid] = append(tree.children[ppid], pid)
	}
	return tree, nil
}

// foreground returns the deepest descendant of panePID — what is actually
// running in the pane. tmux resolves #{pane_current_command} the same way, and
// the walk has to reach the deepest node rather than stopping at the first
// child because the leak's real shape is a grandchild: the agent AO launched is
// the child, and the process it left behind hangs off that.
//
// A pane with no descendants reports nothing running. That is the pane sitting
// at the interactive shell tmux.buildLaunchCommand execs into once the agent
// exits — the designed resting state, which this makes unflaggable by
// construction, with no shell-name allowlist to maintain.
func (t paneProcessTree) foreground(panePID int) (paneProcess, bool) {
	type node struct{ pid, depth int }
	var (
		best      paneProcess
		bestDepth int
		found     bool
	)
	// visited guards against a malformed ppid cycle in external command output;
	// doctor must not spin on it.
	visited := map[int]bool{panePID: true}
	stack := []node{{pid: panePID, depth: 0}}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if proc, ok := t.byPID[cur.pid]; ok && cur.depth > 0 {
			// Deepest wins; equal depth resolves to the lowest pid so the report
			// is stable rather than dependent on ps ordering.
			if !found || cur.depth > bestDepth || (cur.depth == bestDepth && proc.pid < best.pid) {
				best, bestDepth, found = proc, cur.depth, true
			}
		}
		for _, child := range t.children[cur.pid] {
			if visited[child] {
				continue
			}
			visited[child] = true
			stack = append(stack, node{pid: child, depth: cur.depth + 1})
		}
	}
	return best, found
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
