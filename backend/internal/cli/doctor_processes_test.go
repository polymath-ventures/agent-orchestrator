package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

// doctorPane is one row of the read-only `tmux list-panes` probe.
type doctorPane struct {
	tmuxSession string
	pid         int
	command     string
	etimes      int
}

func doctorPaneOutput(panes []doctorPane) string {
	var b strings.Builder
	for _, p := range panes {
		fmt.Fprintf(&b, "%s %d %s\n", p.tmuxSession, p.pid, p.command)
	}
	return b.String()
}

// doctorPSOutput mimics `ps -o pid=,etimes= -p <csv>`: right-aligned pid and
// elapsed seconds, no header.
func doctorPSOutput(panes []doctorPane) string {
	var b strings.Builder
	for _, p := range panes {
		fmt.Fprintf(&b, "%7d %6d\n", p.pid, p.etimes)
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
			return []byte(doctorPSOutput(panes)), nil
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

// TestDoctorAgentProcessesWarnsOnStaleNonAgentPaneProcess models the reported
// bug: an 8-hour `curl` squatting in a pane AO owns. "Not the agent AO launched
// for this session, running longer than an hour, inside a pane AO owns" is the
// whole rule.
func TestDoctorAgentProcessesWarnsOnStaleNonAgentPaneProcess(t *testing.T) {
	cfg := setConfigEnv(t)
	sessions := []sessionDTO{
		{ID: "sess-stale", ProjectID: "demo", Kind: "worker", Harness: "claude-code", Status: "running"},
		{ID: "sess-ok", ProjectID: "demo", Kind: "worker", Harness: "codex", Status: "running"},
	}
	panes := []doctorPane{
		{tmuxSession: "sess-stale", pid: 4711, command: "curl", etimes: 28800},
		{tmuxSession: "sess-ok", pid: 4712, command: "codex", etimes: 28800},
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
		t.Fatalf("agent-processes = %+v, want WARN for an 8h non-agent pane process", check)
	}
	for _, want := range []string{"4711", "curl"} {
		if !strings.Contains(check.Message, want) {
			t.Fatalf("agent-processes message %q missing %q (must name the pid and the command)", check.Message, want)
		}
	}
	if !strings.Contains(check.Message, "8h") && !strings.Contains(check.Message, "28800") {
		t.Fatalf("agent-processes message %q does not name the elapsed time (8h/28800s)", check.Message)
	}
	if strings.Contains(check.Message, "4712") {
		t.Fatalf("agent-processes message %q flagged the session's own agent process", check.Message)
	}
	rec.assertNoSupervisorSocketProbe(t)
	httpRec.assertReadOnly(t)
}

// TestDoctorAgentProcessesPassesOnAgentAndShortLivedPanes covers the three
// non-findings: the session's own agent binary (including claude-code, whose
// binary name differs from the harness id), a short-lived helper under the
// staleness threshold, and a tmux session AO does not own.
func TestDoctorAgentProcessesPassesOnAgentAndShortLivedPanes(t *testing.T) {
	cfg := setConfigEnv(t)
	sessions := []sessionDTO{
		{ID: "sess-claude", ProjectID: "demo", Kind: "worker", Harness: "claude-code", Status: "running"},
		{ID: "sess-codex", ProjectID: "demo", Kind: "worker", Harness: "codex", Status: "running"},
		{ID: "sess-fresh", ProjectID: "demo", Kind: "worker", Harness: "codex", Status: "running"},
	}
	panes := []doctorPane{
		// The agent AO launched, long-running: expected, not a finding.
		{tmuxSession: "sess-claude", pid: 1001, command: "claude", etimes: 36000},
		{tmuxSession: "sess-codex", pid: 1002, command: "codex", etimes: 36000},
		// A non-agent command well under the 1h staleness threshold.
		{tmuxSession: "sess-fresh", pid: 1003, command: "rg", etimes: 900},
		// A tmux session AO knows nothing about: out of population entirely.
		{tmuxSession: "someones-shell", pid: 1004, command: "vim", etimes: 999999},
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
