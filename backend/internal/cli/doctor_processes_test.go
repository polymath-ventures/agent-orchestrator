package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/daemonmeta"
	"github.com/aoagents/agent-orchestrator/backend/internal/runfile"
)

// Coverage for the two read-only diagnostics #147 adds to `ao doctor`:
//
//   - agent-processes: stale/orphaned long-lived processes sitting in panes AO
//     owns (the reporter's 8-hour leaked `curl`).
//   - daemon-restarts: systemd restart churn for the unit that actually owns
//     the daemon pid.
//
// Both must be strictly read-only: no HTTP against supervise.sock, no mutating
// subprocess. The fakes below fail the test if doctor reaches for anything
// outside the read-only probe set, so the posture is asserted, not assumed.

const doctorFakeDaemonPID = 4242

// -- shared fakes -------------------------------------------------------------

// doctorProbeRecorder records every subprocess doctor runs so a test can assert
// the probe set stayed read-only.
type doctorProbeRecorder struct {
	mu       sync.Mutex
	commands []string
}

func (r *doctorProbeRecorder) record(name string, args []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.commands = append(r.commands, strings.TrimSpace(name+" "+strings.Join(args, " ")))
}

func (r *doctorProbeRecorder) all() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.commands...)
}

// assertNoSupervisorSocketProbe fails when any recorded invocation names the
// supervisor stream socket. #147's whole point is that touching supervise.sock
// perturbs daemon lifecycle, so doctor must never probe it.
func (r *doctorProbeRecorder) assertNoSupervisorSocketProbe(t *testing.T) {
	t.Helper()
	for _, cmd := range r.all() {
		if strings.Contains(cmd, "supervise.sock") {
			t.Fatalf("doctor probed the supervisor socket via subprocess: %q", cmd)
		}
	}
}

// doctorRequestRecorder wraps the daemon HTTP transport so tests can assert
// doctor only ever issued read-only requests at the daemon.
type doctorRequestRecorder struct {
	inner    http.RoundTripper
	mu       sync.Mutex
	requests []string
}

func (r *doctorRequestRecorder) RoundTrip(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	r.requests = append(r.requests, req.Method+" "+req.URL.String())
	r.mu.Unlock()
	return r.inner.RoundTrip(req)
}

func (r *doctorRequestRecorder) assertReadOnly(t *testing.T) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, req := range r.requests {
		method, target, _ := strings.Cut(req, " ")
		if method != http.MethodGet {
			t.Fatalf("doctor issued a mutating daemon request: %q", req)
		}
		if strings.Contains(target, "supervise.sock") || strings.HasPrefix(target, "unix") {
			t.Fatalf("doctor issued an HTTP request against the supervisor socket: %q", req)
		}
	}
}

// doctorProc is one process on a pane's tty, as `ps -o pid=,ppid=,etime=,comm=`
// reports it. ppid is what makes a pane's real foreground process findable:
// tmux.buildLaunchCommand wraps every pane in `sh -c '... <agent>; exec $SHELL
// -i'`, so the agent and anything it spawns hang off the pane's own pid.
//
// etime is a STRING in ps's own `[[dd-]hh:]mm:ss` format, not integer seconds:
// the integer `etimes` field is a procps extension macOS ps does not have, and
// AO supports tmux on Darwin too.
type doctorProc struct {
	pid   int
	ppid  int
	etime string
	comm  string
}

// doctorPane is one pane of the read-only `tmux list-panes` probe together with
// the process tree on its tty. panePID is `#{pane_pid}` — the wrapping shell,
// which is NOT necessarily what is running in the pane.
type doctorPane struct {
	tmuxSession string
	panePID     int
	tty         string
	procs       []doctorProc
}

// doctorPaneOutput mimics `tmux list-panes -a -F '#{session_name} #{pane_pid}
// #{pane_tty}'`, including the /dev/ prefix tmux really emits.
func doctorPaneOutput(panes []doctorPane) string {
	var b strings.Builder
	for _, p := range panes {
		fmt.Fprintf(&b, "%s %d /dev/%s\n", p.tmuxSession, p.panePID, p.tty)
	}
	return b.String()
}

// doctorPSOutput mimics `ps -o pid=,ppid=,etime=,comm= -t <csv>`: right-aligned
// columns, no header, and only the ttys actually asked for. Honouring -t is what
// lets a test prove foreign panes never even reach the probe.
//
// It also guards the probe's portability: `etimes` is a procps-only field, so
// reintroducing it would silently disable this whole check on macOS, where AO
// also supports tmux. Every pane test fails if it comes back.
func doctorPSOutput(t *testing.T, panes []doctorPane, args []string) string {
	t.Helper()
	spec := strings.Join(args, " ")
	if strings.Contains(spec, "etimes") {
		t.Errorf("ps probe asked for `etimes`, a Linux/procps-only field macOS ps does not have: ps %s", spec)
	}
	if !strings.Contains(spec, "etime=") {
		t.Errorf("ps probe does not request the portable `etime` field: ps %s", spec)
	}
	wanted := map[string]bool{}
	for i, arg := range args {
		if arg == "-t" && i+1 < len(args) {
			for _, tty := range strings.Split(args[i+1], ",") {
				wanted[tty] = true
			}
		}
	}
	if len(wanted) == 0 {
		t.Errorf("ps probe did not scope to any tty: ps %s", spec)
	}
	var b strings.Builder
	for _, p := range panes {
		if !wanted[p.tty] {
			continue
		}
		delete(wanted, p.tty)
		for _, proc := range p.procs {
			fmt.Fprintf(&b, "%7d %7d %12s %s\n", proc.pid, proc.ppid, proc.etime, proc.comm)
		}
	}
	for tty := range wanted {
		t.Errorf("ps probe asked about tty %q, which belongs to no AO-owned pane", tty)
	}
	return b.String()
}

// doctorPaneProbeFake answers the read-only pane probes and fails the test on
// anything else, so a mutating or state-changing invocation (kill, tmux
// kill-session, curl against supervise.sock) is a test failure by construction.
func doctorPaneProbeFake(t *testing.T, rec *doctorProbeRecorder, panes []doctorPane) func(context.Context, string, ...string) ([]byte, error) {
	t.Helper()
	return func(_ context.Context, name string, args ...string) ([]byte, error) {
		rec.record(name, args)
		switch base := filepath.Base(name); {
		case base == "git":
			return []byte("git version 2.43.0\n"), nil
		case base == "tmux" && len(args) == 1 && args[0] == "-V":
			return []byte("tmux 3.3a\n"), nil
		case base == "tmux" && len(args) > 0 && args[0] == "list-panes":
			return []byte(doctorPaneOutput(panes)), nil
		case base == "ps":
			return []byte(doctorPSOutput(t, panes, args)), nil
		default:
			t.Errorf("doctor ran a non read-only probe: %s %v", name, args)
			return nil, fmt.Errorf("unexpected command %s", name)
		}
	}
}

// doctorDaemonServer stands in for a live daemon: the read-only probes
// inspectDaemon already makes plus GET /api/v1/sessions, which is the only way
// the pane check may learn which sessions AO owns.
func doctorDaemonServer(t *testing.T, sessions []sessionDTO) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("daemon received a mutating request from doctor: %s %s", r.Method, r.URL.Path)
			http.Error(w, "read-only", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)
		switch {
		case r.URL.Path == "/healthz":
			_ = enc.Encode(probeResult{Status: "ok", Service: daemonmeta.ServiceName, PID: doctorFakeDaemonPID})
		case r.URL.Path == "/readyz":
			_ = enc.Encode(probeResult{Status: string(stateReady), Service: daemonmeta.ServiceName, PID: doctorFakeDaemonPID})
		case r.URL.Path == "/api/v1/fleet":
			_ = enc.Encode(map[string]bool{"paused": false})
		case r.URL.Path == "/api/v1/metrics":
			_ = enc.Encode(map[string]any{})
		case strings.HasPrefix(r.URL.Path, "/api/v1/sessions"):
			_ = enc.Encode(sessionListResponse{Sessions: sessions})
		default:
			http.NotFound(w, r)
		}
	}))
}

// doctorLiveDaemonContext wires doctor to the fake daemon: run-file present,
// pid alive, HTTP recorded.
func doctorLiveDaemonContext(t *testing.T, cfg testConfig, srv *httptest.Server, paths map[string]string, commandOutput func(context.Context, string, ...string) ([]byte, error)) (*commandContext, *doctorRequestRecorder) {
	t.Helper()
	if err := runfile.Write(cfg.runFile, runfile.Info{
		PID:       doctorFakeDaemonPID,
		Port:      serverPort(t, srv.URL),
		StartedAt: time.Unix(100, 0).UTC(),
	}); err != nil {
		t.Fatalf("write run-file: %v", err)
	}
	c := doctorContext(t, paths, commandOutput)
	c.deps.ProcessAlive = func(pid int) bool { return pid == doctorFakeDaemonPID }
	rec := &doctorRequestRecorder{inner: srv.Client().Transport}
	client := *srv.Client()
	client.Transport = rec
	c.deps.HTTPClient = &client
	return c, rec
}

// -- agent-processes (AC 3) ---------------------------------------------------

// TestDoctorAgentProcessesWarnsOnStaleGrandchildSquatter models the reported
// bug in its real shape: the agent AO launched is a child of the pane's shell,
// and the 8-hour `curl` it left behind is a GRANDCHILD. The finding must name
// that grandchild — its pid, its command, its own elapsed time — and must not
// name the pane shell (whose pid an operator would kill, destroying the pane) or
// the pane's age (which is just how long the session has been open).
func TestDoctorAgentProcessesWarnsOnStaleGrandchildSquatter(t *testing.T) {
	cfg := setConfigEnv(t)
	sessions := []sessionDTO{
		{ID: "sess-stale", ProjectID: "demo", Kind: "worker", Harness: "claude-code", Status: "running"},
		{ID: "sess-ok", ProjectID: "demo", Kind: "worker", Harness: "codex", Status: "running"},
	}
	panes := []doctorPane{
		{tmuxSession: "sess-stale", panePID: 5000, tty: "pts/1", procs: []doctorProc{
			{pid: 5000, ppid: 900, etime: "16:00:00", comm: "sh"},      // pane shell, up 16h
			{pid: 5100, ppid: 5000, etime: "15:00:00", comm: "claude"}, // the agent
			{pid: 5200, ppid: 5100, etime: "08:00:00", comm: "curl"},   // the 8h squatter
		}},
		{tmuxSession: "sess-ok", panePID: 6000, tty: "pts/2", procs: []doctorProc{
			{pid: 6000, ppid: 900, etime: "16:00:00", comm: "sh"},
			{pid: 6100, ppid: 6000, etime: "15:00:00", comm: "codex"},
		}},
	}
	srv := doctorDaemonServer(t, sessions)
	rec := &doctorProbeRecorder{}
	c, httpRec := doctorLiveDaemonContext(t, cfg,
		srv,
		map[string]string{"git": "/bin/git", "tmux": "/bin/tmux", "ps": "/bin/ps"},
		doctorPaneProbeFake(t, rec, panes),
	)

	check := findDoctorCheck(t, c.runDoctor(context.Background()), "agent-processes")
	if check.Level != doctorWarn {
		t.Fatalf("agent-processes = %+v, want WARN for an 8h grandchild squatter", check)
	}
	for _, want := range []string{"5200", "curl"} {
		if !strings.Contains(check.Message, want) {
			t.Fatalf("agent-processes message %q missing %q (must name the squatter's pid and command)", check.Message, want)
		}
	}
	if !strings.Contains(check.Message, "8h") && !strings.Contains(check.Message, "28800") {
		t.Fatalf("agent-processes message %q does not name the squatter's own elapsed time (8h/28800s)", check.Message)
	}
	// Reporting the pane shell's pid or the pane's 16h age is the bug, not the fix.
	if strings.Contains(check.Message, "5000") {
		t.Fatalf("agent-processes message %q named the pane shell's pid; killing it destroys the pane", check.Message)
	}
	if strings.Contains(check.Message, "16h") {
		t.Fatalf("agent-processes message %q reported the pane's age instead of the squatter's", check.Message)
	}
	// The healthy pane's agent and its own shell must not be flagged.
	for _, unwanted := range []string{"6000", "6100", "codex"} {
		if strings.Contains(check.Message, unwanted) {
			t.Fatalf("agent-processes message %q flagged the healthy session (%s)", check.Message, unwanted)
		}
	}
	rec.assertNoSupervisorSocketProbe(t)
	httpRec.assertReadOnly(t)
}

// TestDoctorAgentProcessesPassesOnRestingPaneShell covers the pane's designed
// resting state. tmux.buildLaunchCommand ends in `exec "${SHELL:-/bin/sh}" -i`,
// so once the agent exits the shell REPLACES the wrapper in place: #{pane_pid}
// is itself an interactive shell with no descendants, and #{pane_current_command}
// is `bash`. Every idle AO pane looks like this, and none of them is a finding —
// modelled on the real long-lived `prime-1` pane on the dev host.
func TestDoctorAgentProcessesPassesOnRestingPaneShell(t *testing.T) {
	cfg := setConfigEnv(t)
	sessions := []sessionDTO{
		{ID: "sess-idle", ProjectID: "demo", Kind: "worker", Harness: "codex", Status: "running"},
	}
	panes := []doctorPane{
		{tmuxSession: "sess-idle", panePID: 3194093, tty: "pts/9", procs: []doctorProc{
			// Up 16h, command `bash`, and crucially NO descendants.
			{pid: 3194093, ppid: 900, etime: "16:00:00", comm: "bash"},
		}},
	}
	srv := doctorDaemonServer(t, sessions)
	rec := &doctorProbeRecorder{}
	c, httpRec := doctorLiveDaemonContext(t, cfg,
		srv,
		map[string]string{"git": "/bin/git", "tmux": "/bin/tmux", "ps": "/bin/ps"},
		doctorPaneProbeFake(t, rec, panes),
	)

	check := findDoctorCheck(t, c.runDoctor(context.Background()), "agent-processes")
	if check.Level != doctorPass {
		t.Fatalf("agent-processes = %+v, want PASS for a pane resting at its own shell", check)
	}
	rec.assertNoSupervisorSocketProbe(t)
	httpRec.assertReadOnly(t)
}

// TestDoctorAgentProcessesPassesOnAgentAndShortLivedHelpers covers the
// remaining non-findings: the agent running as a direct child for many hours
// (including claude-code, whose binary name differs from the harness id), a
// short-lived helper the agent spawned as a grandchild, and a tmux session AO
// does not own. The short-lived grandchild is the false positive a pane-age
// threshold produces: the pane is 16h old, the helper is 2 minutes old.
func TestDoctorAgentProcessesPassesOnAgentAndShortLivedHelpers(t *testing.T) {
	cfg := setConfigEnv(t)
	sessions := []sessionDTO{
		{ID: "sess-claude", ProjectID: "demo", Kind: "worker", Harness: "claude-code", Status: "running"},
		{ID: "sess-codex", ProjectID: "demo", Kind: "worker", Harness: "codex", Status: "running"},
	}
	panes := []doctorPane{
		// The agent AO launched, as a direct child, running for 15h: expected.
		// comm is the full executable path macOS ps reports (Linux reports the
		// bare name), so this also pins the base-name normalization: without it
		// the agent itself would be flagged as a squatter on every Mac.
		{tmuxSession: "sess-claude", panePID: 1000, tty: "pts/3", procs: []doctorProc{
			{pid: 1000, ppid: 900, etime: "16:00:00", comm: "/bin/sh"},
			{pid: 1100, ppid: 1000, etime: "15:00:00", comm: "/opt/homebrew/bin/claude"},
		}},
		// The agent plus a 2-minute helper grandchild (mm:ss etime shape), inside
		// a 16h-old pane: the false positive a pane-age threshold produces.
		{tmuxSession: "sess-codex", panePID: 2000, tty: "pts/4", procs: []doctorProc{
			{pid: 2000, ppid: 900, etime: "16:00:00", comm: "sh"},
			{pid: 2100, ppid: 2000, etime: "15:00:00", comm: "codex"},
			{pid: 2200, ppid: 2100, etime: "02:00", comm: "rg"},
		}},
		// A tmux session AO knows nothing about: out of population entirely. Its
		// tty must never reach the ps probe (doctorPSOutput asserts that).
		{tmuxSession: "someones-shell", panePID: 4000, tty: "pts/5", procs: []doctorProc{
			{pid: 4000, ppid: 900, etime: "11-13:46:39", comm: "bash"},
			{pid: 4100, ppid: 4000, etime: "11-13:46:39", comm: "vim"},
		}},
	}
	srv := doctorDaemonServer(t, sessions)
	rec := &doctorProbeRecorder{}
	c, httpRec := doctorLiveDaemonContext(t, cfg,
		srv,
		map[string]string{"git": "/bin/git", "tmux": "/bin/tmux", "ps": "/bin/ps"},
		doctorPaneProbeFake(t, rec, panes),
	)

	check := findDoctorCheck(t, c.runDoctor(context.Background()), "agent-processes")
	if check.Level != doctorPass {
		t.Fatalf("agent-processes = %+v, want PASS for agent/short-lived/foreign panes", check)
	}
	rec.assertNoSupervisorSocketProbe(t)
	httpRec.assertReadOnly(t)
}

// TestDoctorAgentProcessesWarnsOnMultiDaySquatter exercises the third etime
// shape, `dd-hh:mm:ss`, driving the staleness threshold. A squatter that has
// outlived a whole weekend is the strongest form of the reported bug.
func TestDoctorAgentProcessesWarnsOnMultiDaySquatter(t *testing.T) {
	cfg := setConfigEnv(t)
	sessions := []sessionDTO{
		{ID: "sess-weekend", ProjectID: "demo", Kind: "worker", Harness: "codex", Status: "running"},
	}
	panes := []doctorPane{
		{tmuxSession: "sess-weekend", panePID: 7000, tty: "pts/6", procs: []doctorProc{
			{pid: 7000, ppid: 900, etime: "3-00:00:00", comm: "sh"},
			{pid: 7100, ppid: 7000, etime: "2-23:00:00", comm: "codex"},
			{pid: 7200, ppid: 7100, etime: "2-02:00:00", comm: "ssh"}, // 50h
		}},
	}
	srv := doctorDaemonServer(t, sessions)
	rec := &doctorProbeRecorder{}
	c, httpRec := doctorLiveDaemonContext(t, cfg,
		srv,
		map[string]string{"git": "/bin/git", "tmux": "/bin/tmux", "ps": "/bin/ps"},
		doctorPaneProbeFake(t, rec, panes),
	)

	check := findDoctorCheck(t, c.runDoctor(context.Background()), "agent-processes")
	if check.Level != doctorWarn {
		t.Fatalf("agent-processes = %+v, want WARN for a multi-day squatter", check)
	}
	for _, want := range []string{"7200", "ssh", "50h"} {
		if !strings.Contains(check.Message, want) {
			t.Fatalf("agent-processes message %q missing %q (dd-hh:mm:ss elapsed must parse)", check.Message, want)
		}
	}
	rec.assertNoSupervisorSocketProbe(t)
	httpRec.assertReadOnly(t)
}

// TestDoctorAgentProcessesPassesOnUnparseableElapsed: an age doctor could not
// read is never grounds for a warning. Guessing here would mean warning about a
// process whose age is unknown, which is exactly the wrong direction.
func TestDoctorAgentProcessesPassesOnUnparseableElapsed(t *testing.T) {
	cfg := setConfigEnv(t)
	sessions := []sessionDTO{
		{ID: "sess-odd", ProjectID: "demo", Kind: "worker", Harness: "codex", Status: "running"},
	}
	panes := []doctorPane{
		{tmuxSession: "sess-odd", panePID: 8000, tty: "pts/7", procs: []doctorProc{
			{pid: 8000, ppid: 900, etime: "16:00:00", comm: "sh"},
			{pid: 8100, ppid: 8000, etime: "not-a-duration", comm: "curl"},
		}},
	}
	srv := doctorDaemonServer(t, sessions)
	rec := &doctorProbeRecorder{}
	c, httpRec := doctorLiveDaemonContext(t, cfg,
		srv,
		map[string]string{"git": "/bin/git", "tmux": "/bin/tmux", "ps": "/bin/ps"},
		doctorPaneProbeFake(t, rec, panes),
	)

	check := findDoctorCheck(t, c.runDoctor(context.Background()), "agent-processes")
	if check.Level != doctorPass {
		t.Fatalf("agent-processes = %+v, want PASS when the elapsed time will not parse", check)
	}
	rec.assertNoSupervisorSocketProbe(t)
	httpRec.assertReadOnly(t)
}

// TestDoctorAgentProcessesPassesOnAmbiguousBranch: when the pane's process tree
// forks, the deepest node is not necessarily the one holding the foreground — a
// long-lived background helper (an MCP server, a dev server) can sit deeper than
// whatever is actually blocking. Doctor declines rather than naming a pid it
// guessed at, even though one branch is an old non-agent process.
func TestDoctorAgentProcessesPassesOnAmbiguousBranch(t *testing.T) {
	cfg := setConfigEnv(t)
	sessions := []sessionDTO{
		{ID: "sess-branch", ProjectID: "demo", Kind: "worker", Harness: "codex", Status: "running"},
	}
	panes := []doctorPane{
		{tmuxSession: "sess-branch", panePID: 9000, tty: "pts/8", procs: []doctorProc{
			{pid: 9000, ppid: 900, etime: "16:00:00", comm: "sh"},
			{pid: 9100, ppid: 9000, etime: "15:00:00", comm: "codex"},
			// Two sibling subtrees under the agent, BOTH old and neither the
			// agent: a background dev server and something that may or may not
			// be blocking the pane. Deliberately symmetric, so any implementation
			// that picks one instead of declining warns about a pid it guessed —
			// whichever branch it happens to walk first.
			{pid: 9200, ppid: 9100, etime: "09:00:00", comm: "node"},
			{pid: 9300, ppid: 9100, etime: "09:00:00", comm: "curl"},
		}},
	}
	srv := doctorDaemonServer(t, sessions)
	rec := &doctorProbeRecorder{}
	c, httpRec := doctorLiveDaemonContext(t, cfg,
		srv,
		map[string]string{"git": "/bin/git", "tmux": "/bin/tmux", "ps": "/bin/ps"},
		doctorPaneProbeFake(t, rec, panes),
	)

	check := findDoctorCheck(t, c.runDoctor(context.Background()), "agent-processes")
	if check.Level != doctorPass {
		t.Fatalf("agent-processes = %+v, want PASS when the process tree branches (foreground is unknowable)", check)
	}
	rec.assertNoSupervisorSocketProbe(t)
	httpRec.assertReadOnly(t)
}

// TestPaneProcessTableMarksUnreadableAgeUnknown pins the plumbing behind the
// "never warn on an age you could not read" rule: a row whose etime will not
// parse stays in the tree, so the walk's parent links survive, but is marked
// ageKnown=false rather than silently carrying a zero duration.
func TestPaneProcessTableMarksUnreadableAgeUnknown(t *testing.T) {
	c := &commandContext{deps: Deps{
		CommandOutput: func(_ context.Context, _ string, _ ...string) ([]byte, error) {
			return []byte("   4000    900     16:00:00 sh\n   4100   4000 not-a-duration curl\n"), nil
		},
	}.withDefaults()}

	tree, err := c.paneProcessTable(context.Background(), "/bin/ps", []string{"pts/1"})
	if err != nil {
		t.Fatalf("paneProcessTable: %v", err)
	}
	readable, ok := tree.byPID[4000]
	if !ok {
		t.Fatal("row with a readable etime missing from the tree")
	}
	if !readable.ageKnown || readable.elapsed != 16*time.Hour {
		t.Errorf("pid 4000 = %+v, want ageKnown with elapsed 16h", readable)
	}
	unreadable, ok := tree.byPID[4100]
	if !ok {
		t.Fatal("row with an unreadable etime was dropped from the tree; the walk's parent links must survive")
	}
	if unreadable.ageKnown {
		t.Errorf("pid 4100 = %+v, want ageKnown=false for an etime that will not parse", unreadable)
	}
	// The parent link must still be intact even though the age is unknown.
	if got := tree.children[4000]; len(got) != 1 || got[0] != 4100 {
		t.Errorf("children[4000] = %v, want [4100]", got)
	}
}

// TestParseETime pins the POSIX ps elapsed-time format, `[[dd-]hh:]mm:ss`. This
// is the field that replaced procps-only `etimes`, so its parsing is the whole
// reason agent-processes can run on macOS at all.
func TestParseETime(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want time.Duration
		ok   bool
	}{
		{raw: "00:00", want: 0, ok: true},
		{raw: "02:00", want: 2 * time.Minute, ok: true},
		{raw: "59:59", want: 59*time.Minute + 59*time.Second, ok: true},
		{raw: "08:00:00", want: 8 * time.Hour, ok: true},
		{raw: "16:00:00", want: 16 * time.Hour, ok: true},
		{raw: "7-18:10:32", want: 7*24*time.Hour + 18*time.Hour + 10*time.Minute + 32*time.Second, ok: true},
		{raw: "3-00:00:00", want: 72 * time.Hour, ok: true},
		{raw: "  08:00:00  ", want: 8 * time.Hour, ok: true},
		// Unparseable: never a guessed duration.
		{raw: ""},
		{raw: "12"},
		{raw: "not-a-duration"},
		{raw: "1:2:3:4"},
		{raw: "aa:bb"},
		{raw: "x-01:00:00"},
		{raw: "-1:00"},
		// The procps integer form must NOT be silently accepted as seconds.
		{raw: "28800"},
	} {
		got, ok := parseETime(tc.raw)
		if ok != tc.ok {
			t.Errorf("parseETime(%q) ok = %v, want %v", tc.raw, ok, tc.ok)
			continue
		}
		if ok && got != tc.want {
			t.Errorf("parseETime(%q) = %s, want %s", tc.raw, got, tc.want)
		}
	}
}

// TestDoctorAgentProcessesUnavailableWithoutTmux: no tmux means no pane
// inventory. Unavailable is not a failure.
func TestDoctorAgentProcessesUnavailableWithoutTmux(t *testing.T) {
	cfg := setConfigEnv(t)
	srv := doctorDaemonServer(t, []sessionDTO{
		{ID: "sess-codex", ProjectID: "demo", Kind: "worker", Harness: "codex", Status: "running"},
	})
	rec := &doctorProbeRecorder{}
	c, httpRec := doctorLiveDaemonContext(t, cfg, srv,
		map[string]string{"git": "/bin/git"},
		doctorPaneProbeFake(t, rec, nil),
	)

	check := findDoctorCheck(t, c.runDoctor(context.Background()), "agent-processes")
	assertDoctorUnavailable(t, "agent-processes", check)
	rec.assertNoSupervisorSocketProbe(t)
	httpRec.assertReadOnly(t)
}

// TestDoctorAgentProcessesUnavailableWithoutDaemon: with no daemon there is no
// session population to scope panes to, and doctor must not guess. Still not a
// failure.
func TestDoctorAgentProcessesUnavailableWithoutDaemon(t *testing.T) {
	setConfigEnv(t)
	rec := &doctorProbeRecorder{}
	c := doctorContext(t,
		map[string]string{"git": "/bin/git", "tmux": "/bin/tmux", "ps": "/bin/ps"},
		doctorPaneProbeFake(t, rec, nil),
	)

	check := findDoctorCheck(t, c.runDoctor(context.Background()), "agent-processes")
	assertDoctorUnavailable(t, "agent-processes", check)
	rec.assertNoSupervisorSocketProbe(t)
}

// -- daemon-restarts (AC 4) ---------------------------------------------------

// doctorSystemctlFake answers `systemctl show <unit> -p NRestarts --value` for
// exactly one expected unit, and fails the test if doctor asks about
// `ao.service` — the unit must be derived from the daemon pid's cgroup, not
// hardcoded.
func doctorSystemctlFake(t *testing.T, rec *doctorProbeRecorder, wantUnit, nrestarts string) func(context.Context, string, ...string) ([]byte, error) {
	t.Helper()
	return func(_ context.Context, name string, args ...string) ([]byte, error) {
		rec.record(name, args)
		joined := strings.Join(args, " ")
		switch filepath.Base(name) {
		case "git":
			return []byte("git version 2.43.0\n"), nil
		case "systemctl":
			if strings.Contains(joined, "ao.service") {
				t.Errorf("systemctl probed a hardcoded unit: systemctl %s", joined)
				return nil, fmt.Errorf("unknown unit")
			}
			if !strings.Contains(joined, wantUnit) {
				t.Errorf("systemctl probed %q, want the derived unit %q", joined, wantUnit)
				return nil, fmt.Errorf("unknown unit")
			}
			if !strings.Contains(joined, "NRestarts") {
				t.Errorf("systemctl args %q do not request NRestarts", joined)
			}
			return []byte(nrestarts + "\n"), nil
		default:
			t.Errorf("doctor ran a non read-only probe: %s %v", name, args)
			return nil, fmt.Errorf("unexpected command %s", name)
		}
	}
}

// writeProcCgroup fakes /proc/<pid>/cgroup under a test-owned root so the unit
// derivation is exercised without touching the real /proc.
func writeProcCgroup(t *testing.T, pid int, content string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, fmt.Sprint(pid))
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cgroup"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

// TestDoctorDaemonRestartsWarnsOnChurn drives the derived unit deliberately to
// something other than ao.service: a hardcoded-unit implementation cannot pass.
func TestDoctorDaemonRestartsWarnsOnChurn(t *testing.T) {
	cfg := setConfigEnv(t)
	srv := doctorDaemonServer(t, nil)
	rec := &doctorProbeRecorder{}
	c, httpRec := doctorLiveDaemonContext(t, cfg, srv,
		map[string]string{"git": "/bin/git", "systemctl": "/bin/systemctl"},
		doctorSystemctlFake(t, rec, "ao-dev.service", "42"),
	)
	c.deps.ProcRoot = writeProcCgroup(t, doctorFakeDaemonPID, "0::/system.slice/ao-dev.service\n")

	check := findDoctorCheck(t, c.runDoctor(context.Background()), "daemon-restarts")
	if check.Level != doctorWarn {
		t.Fatalf("daemon-restarts = %+v, want WARN on restart churn", check)
	}
	for _, want := range []string{"ao-dev.service", "42"} {
		if !strings.Contains(check.Message, want) {
			t.Fatalf("daemon-restarts message %q missing %q (must name the derived unit and the count)", check.Message, want)
		}
	}
	rec.assertNoSupervisorSocketProbe(t)
	httpRec.assertReadOnly(t)
}

func TestDoctorDaemonRestartsPassesWithoutChurn(t *testing.T) {
	cfg := setConfigEnv(t)
	srv := doctorDaemonServer(t, nil)
	rec := &doctorProbeRecorder{}
	c, httpRec := doctorLiveDaemonContext(t, cfg, srv,
		map[string]string{"git": "/bin/git", "systemctl": "/bin/systemctl"},
		doctorSystemctlFake(t, rec, "ao-dev.service", "0"),
	)
	c.deps.ProcRoot = writeProcCgroup(t, doctorFakeDaemonPID, "0::/system.slice/ao-dev.service\n")

	check := findDoctorCheck(t, c.runDoctor(context.Background()), "daemon-restarts")
	if check.Level != doctorPass {
		t.Fatalf("daemon-restarts = %+v, want PASS at NRestarts=0", check)
	}
	for _, want := range []string{"ao-dev.service", "0"} {
		if !strings.Contains(check.Message, want) {
			t.Fatalf("daemon-restarts message %q missing %q", check.Message, want)
		}
	}
	rec.assertNoSupervisorSocketProbe(t)
	httpRec.assertReadOnly(t)
}

// TestDoctorDaemonRestartsUnavailableWithoutSystemctl: no systemd, no signal —
// and doctor must not fail on a machine that simply is not systemd-managed.
func TestDoctorDaemonRestartsUnavailableWithoutSystemctl(t *testing.T) {
	cfg := setConfigEnv(t)
	srv := doctorDaemonServer(t, nil)
	rec := &doctorProbeRecorder{}
	c, httpRec := doctorLiveDaemonContext(t, cfg, srv,
		map[string]string{"git": "/bin/git"},
		doctorSystemctlFake(t, rec, "ao-dev.service", "0"),
	)
	c.deps.ProcRoot = writeProcCgroup(t, doctorFakeDaemonPID, "0::/system.slice/ao-dev.service\n")

	check := findDoctorCheck(t, c.runDoctor(context.Background()), "daemon-restarts")
	assertDoctorUnavailable(t, "daemon-restarts", check)
	rec.assertNoSupervisorSocketProbe(t)
	httpRec.assertReadOnly(t)
}

// TestDoctorDaemonRestartsUnavailableWithoutDerivableUnit: the cgroup names no
// systemd unit (container, plain `ao start`, non-Linux), so there is nothing to
// ask systemd about.
func TestDoctorDaemonRestartsUnavailableWithoutDerivableUnit(t *testing.T) {
	cfg := setConfigEnv(t)
	srv := doctorDaemonServer(t, nil)
	rec := &doctorProbeRecorder{}
	c, httpRec := doctorLiveDaemonContext(t, cfg, srv,
		map[string]string{"git": "/bin/git", "systemctl": "/bin/systemctl"},
		doctorSystemctlFake(t, rec, "ao-dev.service", "0"),
	)
	c.deps.ProcRoot = writeProcCgroup(t, doctorFakeDaemonPID, "0::/user.slice/user-1000.slice/session-3.scope\n")

	check := findDoctorCheck(t, c.runDoctor(context.Background()), "daemon-restarts")
	assertDoctorUnavailable(t, "daemon-restarts", check)
	rec.assertNoSupervisorSocketProbe(t)
	httpRec.assertReadOnly(t)
}

// TestDoctorDaemonRestartsUnavailableWithoutDaemon: no daemon pid, no unit to
// derive.
func TestDoctorDaemonRestartsUnavailableWithoutDaemon(t *testing.T) {
	setConfigEnv(t)
	rec := &doctorProbeRecorder{}
	c := doctorContext(t,
		map[string]string{"git": "/bin/git", "systemctl": "/bin/systemctl"},
		doctorSystemctlFake(t, rec, "ao-dev.service", "0"),
	)

	check := findDoctorCheck(t, c.runDoctor(context.Background()), "daemon-restarts")
	assertDoctorUnavailable(t, "daemon-restarts", check)
	rec.assertNoSupervisorSocketProbe(t)
}

// assertDoctorUnavailable encodes the shared contract for both new checks: an
// unavailable probe is reported, never a FAIL, and says so in the message.
func assertDoctorUnavailable(t *testing.T, name string, check doctorCheck) {
	t.Helper()
	if check.Level == doctorFail {
		t.Fatalf("%s = %+v, want a non-FAIL level when the probe is unavailable", name, check)
	}
	msg := strings.ToLower(check.Message)
	if !strings.Contains(msg, "unavailable") && !strings.Contains(msg, "skipped") {
		t.Fatalf("%s message %q does not report the probe as unavailable/skipped", name, check.Message)
	}
}

// scopedSystemctlFake asserts the --user flag is present exactly when the unit
// is owned by a per-user manager. Querying the wrong manager is not a loud
// failure: `systemctl show` answers 0 for a unit it does not know, so asking
// the system manager about a user unit reports a healthy zero forever and the
// restart-churn check silently never fires.
func scopedSystemctlFake(t *testing.T, rec *doctorProbeRecorder, wantUnit, nrestarts string, wantUserScope bool) func(context.Context, string, ...string) ([]byte, error) {
	t.Helper()
	return func(_ context.Context, name string, args ...string) ([]byte, error) {
		rec.record(name, args)
		joined := strings.Join(args, " ")
		switch filepath.Base(name) {
		case "git":
			return []byte("git version 2.43.0\n"), nil
		case "systemctl":
			if got := slices.Contains(args, "--user"); got != wantUserScope {
				t.Errorf("systemctl --user = %v, want %v (args: %s)", got, wantUserScope, joined)
			}
			if !strings.Contains(joined, wantUnit) {
				t.Errorf("systemctl probed %q, want the derived unit %q", joined, wantUnit)
			}
			return []byte(nrestarts + "\n"), nil
		default:
			t.Errorf("doctor ran a non read-only probe: %s %v", name, args)
			return nil, fmt.Errorf("unexpected command %s", name)
		}
	}
}

// TestDoctorDaemonRestartsQueriesUserManagerForUserScopedUnit covers a daemon
// running as a systemd *user* unit: the enclosing user@<uid>.service on the
// cgroup path is systemd's own marker for that, and the probe must be routed to
// the user manager rather than the system one.
func TestDoctorDaemonRestartsQueriesUserManagerForUserScopedUnit(t *testing.T) {
	cfg := setConfigEnv(t)
	srv := doctorDaemonServer(t, nil)
	rec := &doctorProbeRecorder{}
	c, httpRec := doctorLiveDaemonContext(t, cfg, srv,
		map[string]string{"git": "/bin/git", "systemctl": "/bin/systemctl"},
		scopedSystemctlFake(t, rec, "ao-dev.service", "7", true),
	)
	c.deps.ProcRoot = writeProcCgroup(t, doctorFakeDaemonPID,
		"0::/user.slice/user-1002.slice/user@1002.service/app.slice/ao-dev.service\n")

	check := findDoctorCheck(t, c.runDoctor(context.Background()), "daemon-restarts")
	if check.Level != doctorWarn {
		t.Fatalf("daemon-restarts = %+v, want WARN on restart churn", check)
	}
	for _, want := range []string{"ao-dev.service", "7", "--user"} {
		if !strings.Contains(check.Message, want) {
			t.Fatalf("daemon-restarts message %q missing %q (must name the unit, the count, and a user-scoped journal hint)", check.Message, want)
		}
	}
	rec.assertNoSupervisorSocketProbe(t)
	httpRec.assertReadOnly(t)
}

// TestDoctorDaemonRestartsUsesSystemManagerForSystemUnit is the other half of
// the scope pair: a system-slice unit must NOT be queried with --user.
func TestDoctorDaemonRestartsUsesSystemManagerForSystemUnit(t *testing.T) {
	cfg := setConfigEnv(t)
	srv := doctorDaemonServer(t, nil)
	rec := &doctorProbeRecorder{}
	c, httpRec := doctorLiveDaemonContext(t, cfg, srv,
		map[string]string{"git": "/bin/git", "systemctl": "/bin/systemctl"},
		scopedSystemctlFake(t, rec, "ao-dev.service", "0", false),
	)
	c.deps.ProcRoot = writeProcCgroup(t, doctorFakeDaemonPID, "0::/system.slice/ao-dev.service\n")

	check := findDoctorCheck(t, c.runDoctor(context.Background()), "daemon-restarts")
	if check.Level != doctorPass {
		t.Fatalf("daemon-restarts = %+v, want PASS without churn", check)
	}
	rec.assertNoSupervisorSocketProbe(t)
	httpRec.assertReadOnly(t)
}

// TestDoctorDaemonRestartsIgnoresNonSystemdCgroupHierarchies covers a cgroup v1
// host, where /proc/<pid>/cgroup carries one line per controller. Only the
// systemd-owned hierarchy may name the unit: scanning every line and letting
// the last `.service` win would pick a unit from an unrelated controller and
// silently reset the user/system scope, so the restart count would be read from
// the wrong unit or asked of the wrong manager.
func TestDoctorDaemonRestartsIgnoresNonSystemdCgroupHierarchies(t *testing.T) {
	cfg := setConfigEnv(t)
	srv := doctorDaemonServer(t, nil)
	rec := &doctorProbeRecorder{}
	c, httpRec := doctorLiveDaemonContext(t, cfg, srv,
		map[string]string{"git": "/bin/git", "systemctl": "/bin/systemctl"},
		scopedSystemctlFake(t, rec, "ao-dev.service", "9", true),
	)
	// The systemd line is FIRST and says user scope; the later controller lines
	// name a different unit in the system slice. The systemd line must win.
	c.deps.ProcRoot = writeProcCgroup(t, doctorFakeDaemonPID, strings.Join([]string{
		"1:name=systemd:/user.slice/user-1002.slice/user@1002.service/app.slice/ao-dev.service",
		"4:cpu,cpuacct:/system.slice/unrelated.service",
		"9:devices:/system.slice/another-decoy.service",
		"",
	}, "\n"))

	check := findDoctorCheck(t, c.runDoctor(context.Background()), "daemon-restarts")
	if check.Level != doctorWarn {
		t.Fatalf("daemon-restarts = %+v, want WARN on restart churn", check)
	}
	for _, want := range []string{"ao-dev.service", "9"} {
		if !strings.Contains(check.Message, want) {
			t.Fatalf("daemon-restarts message %q missing %q", check.Message, want)
		}
	}
	for _, unwanted := range []string{"unrelated.service", "another-decoy.service"} {
		if strings.Contains(check.Message, unwanted) {
			t.Fatalf("daemon-restarts message %q named a unit from a non-systemd hierarchy (%q)", check.Message, unwanted)
		}
	}
	rec.assertNoSupervisorSocketProbe(t)
	httpRec.assertReadOnly(t)
}

// TestDoctorDaemonRestartsRequiresWholePathComponent guards the anchored unit
// match: a directory that merely contains ".service" in its name is not a unit.
func TestDoctorDaemonRestartsRequiresWholePathComponent(t *testing.T) {
	cfg := setConfigEnv(t)
	srv := doctorDaemonServer(t, nil)
	c, _ := doctorLiveDaemonContext(t, cfg, srv,
		map[string]string{"git": "/bin/git", "systemctl": "/bin/systemctl"},
		func(_ context.Context, name string, args ...string) ([]byte, error) {
			if filepath.Base(name) == "git" {
				return []byte("git version 2.43.0\n"), nil
			}
			t.Errorf("systemctl was probed for a path with no real unit: %s %v", name, args)
			return nil, fmt.Errorf("unexpected command %s", name)
		},
	)
	c.deps.ProcRoot = writeProcCgroup(t, doctorFakeDaemonPID, "0::/system.slice/ao.service.d\n")

	assertDoctorUnavailable(t, "daemon-restarts", findDoctorCheck(t, c.runDoctor(context.Background()), "daemon-restarts"))
}

// TestDoctorDaemonRestartsAcceptsEscapedUnitNames covers systemd's real
// unit-name alphabet: `:` and `\xNN` escapes are legal, and rejecting them
// would silently report the check unavailable for a unit that exists.
func TestDoctorDaemonRestartsAcceptsEscapedUnitNames(t *testing.T) {
	cfg := setConfigEnv(t)
	srv := doctorDaemonServer(t, nil)
	rec := &doctorProbeRecorder{}
	const unit = `ao\x2dworker:main.service`
	c, httpRec := doctorLiveDaemonContext(t, cfg, srv,
		map[string]string{"git": "/bin/git", "systemctl": "/bin/systemctl"},
		scopedSystemctlFake(t, rec, unit, "0", false),
	)
	c.deps.ProcRoot = writeProcCgroup(t, doctorFakeDaemonPID, "0::/system.slice/"+unit+"\n")

	check := findDoctorCheck(t, c.runDoctor(context.Background()), "daemon-restarts")
	if check.Level != doctorPass {
		t.Fatalf("daemon-restarts = %+v, want PASS naming the escaped unit", check)
	}
	if !strings.Contains(check.Message, unit) {
		t.Fatalf("daemon-restarts message %q does not name the derived unit %q", check.Message, unit)
	}
	rec.assertNoSupervisorSocketProbe(t)
	httpRec.assertReadOnly(t)
}
