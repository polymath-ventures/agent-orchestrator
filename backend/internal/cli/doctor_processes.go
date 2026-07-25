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
		case !fg.ageKnown:
			// ps reported an elapsed time doctor could not parse. An age that
			// could not be read is never grounds for a warning.
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

// paneProcess is one process living on an AO pane's tty. ageKnown is false when
// ps reported an elapsed time this could not parse; an age doctor could not read
// is never grounds for a warning.
type paneProcess struct {
	pid      int
	comm     string
	elapsed  time.Duration
	ageKnown bool
}

// paneProcessTree is the process table of AO's pane ttys, indexed so a pane's
// own pid can be walked down to whatever is actually in its foreground.
type paneProcessTree struct {
	byPID    map[int]paneProcess
	children map[int][]int
}

// paneProcessTable reads the processes on the given ttys. Elapsed time comes
// from the kernel's own accounting (`etime`), so the answer never depends on
// doctor's notion of "now". Restricting the probe to AO's pane ttys is what
// keeps foreign panes out of the picture entirely.
//
// Every field here is POSIX ps, because AO supports tmux on Darwin as well as
// Linux. `etimes` (elapsed seconds as an integer) is a procps extension that
// macOS ps does not have, so this asks for the portable `etime` and parses it.
// Likewise `comm` is POSIX but not uniform — macOS reports the full executable
// path where Linux reports the bare name — so the value is reduced to its base
// name and compared on that.
func (c *commandContext) paneProcessTable(ctx context.Context, psPath string, ttys []string) (paneProcessTree, error) {
	reqCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	out, err := c.deps.CommandOutput(reqCtx, psPath, "-o", "pid=,ppid=,etime=,comm=", "-t", strings.Join(ttys, ","))
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
		// A process whose age will not parse stays in the tree so the walk's
		// parent links survive, but carries ageKnown=false so it cannot be
		// flagged as stale.
		elapsed, ageKnown := parseETime(fields[2])
		tree.byPID[pid] = paneProcess{
			pid:      pid,
			comm:     filepath.Base(strings.Join(fields[3:], " ")),
			elapsed:  elapsed,
			ageKnown: ageKnown,
		}
		tree.children[ppid] = append(tree.children[ppid], pid)
	}
	return tree, nil
}

// parseETime reads the POSIX ps `etime` field, whose format is `[[dd-]hh:]mm:ss`
// — so `mm:ss`, `hh:mm:ss`, or `dd-hh:mm:ss`. Anything else reports false rather
// than a guessed duration.
func parseETime(raw string) (time.Duration, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	days := 0
	if before, after, hasDays := strings.Cut(raw, "-"); hasDays {
		parsed, err := strconv.Atoi(before)
		if err != nil || parsed < 0 {
			return 0, false
		}
		days, raw = parsed, after
	}
	parts := strings.Split(raw, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, false
	}
	hours := 0
	if len(parts) == 3 {
		parsed, err := strconv.Atoi(parts[0])
		if err != nil || parsed < 0 {
			return 0, false
		}
		hours, parts = parsed, parts[1:]
	}
	minutes, err := strconv.Atoi(parts[0])
	if err != nil || minutes < 0 {
		return 0, false
	}
	seconds, err := strconv.Atoi(parts[1])
	if err != nil || seconds < 0 {
		return 0, false
	}
	return time.Duration(days)*24*time.Hour +
		time.Duration(hours)*time.Hour +
		time.Duration(minutes)*time.Minute +
		time.Duration(seconds)*time.Second, true
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
// A branch — any process with more than one child — makes the answer a guess: a
// background helper (an MCP server, a dev server) can sit deeper than whatever
// is actually blocking the pane. Rather than pick, the walk declines and the
// pane goes unflagged. Honest silence is the same posture the rest of this check
// takes toward signals it cannot read.
func (t paneProcessTree) foreground(panePID int) (paneProcess, bool) {
	var (
		deepest paneProcess
		found   bool
	)
	// visited guards against a malformed ppid cycle in external command output;
	// doctor must not spin on it.
	visited := map[int]bool{panePID: true}
	for current := panePID; ; {
		children := t.children[current]
		switch {
		case len(children) == 0:
			return deepest, found
		case len(children) > 1:
			// Ambiguous: more than one subtree to choose between.
			return paneProcess{}, false
		}
		child := children[0]
		if visited[child] {
			return paneProcess{}, false
		}
		visited[child] = true
		proc, ok := t.byPID[child]
		if !ok {
			return paneProcess{}, false
		}
		deepest, found = proc, true
		current = child
	}
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
