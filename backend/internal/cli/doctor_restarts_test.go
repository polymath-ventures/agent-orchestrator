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

// Coverage for the read-only diagnostic #147 adds to `ao doctor`:
//
//   - daemon-restarts: systemd restart churn for the unit that actually owns
//     the daemon pid.
//
// It must be strictly read-only: no HTTP against supervise.sock, no mutating
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

// doctorDaemonServer stands in for a live daemon: the read-only probes
// inspectDaemon already makes, and nothing else.
func doctorDaemonServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("daemon received a mutating request from doctor: %s %s", r.Method, r.URL.Path)
			http.Error(w, "read-only", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)
		switch r.URL.Path {
		case "/healthz":
			_ = enc.Encode(probeResult{Status: "ok", Service: daemonmeta.ServiceName, PID: doctorFakeDaemonPID})
		case "/readyz":
			_ = enc.Encode(probeResult{Status: string(stateReady), Service: daemonmeta.ServiceName, PID: doctorFakeDaemonPID})
		case "/api/v1/fleet":
			_ = enc.Encode(map[string]bool{"paused": false})
		case "/api/v1/metrics":
			_ = enc.Encode(map[string]any{})
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
	srv := doctorDaemonServer(t)
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
	srv := doctorDaemonServer(t)
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
	srv := doctorDaemonServer(t)
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
	srv := doctorDaemonServer(t)
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

// assertDoctorUnavailable encodes the check's contract: an unavailable probe is
// reported, never a FAIL, and says so in the message.
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
			// Exact argv element, not a substring: "__ao.service" CONTAINS
			// "_ao.service", so a substring check is satisfied even when the
			// unescape step is missing.
			if !slices.Contains(args, wantUnit) {
				t.Errorf("systemctl probed %q, want the derived unit %q as an exact argument", joined, wantUnit)
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
	srv := doctorDaemonServer(t)
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
	srv := doctorDaemonServer(t)
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
	srv := doctorDaemonServer(t)
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
	srv := doctorDaemonServer(t)
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
	srv := doctorDaemonServer(t)
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

// TestDoctorDaemonRestartsQuotesUnitInJournalHint: systemd unit names may carry
// \xNN escapes, and an unquoted backslash is eaten by the shell the operator
// pastes the suggested command into, so the hint would query a unit that does
// not exist.
func TestDoctorDaemonRestartsQuotesUnitInJournalHint(t *testing.T) {
	cfg := setConfigEnv(t)
	srv := doctorDaemonServer(t)
	rec := &doctorProbeRecorder{}
	const unit = `ao\x2dworker:main.service`
	c, _ := doctorLiveDaemonContext(t, cfg, srv,
		map[string]string{"git": "/bin/git", "systemctl": "/bin/systemctl"},
		scopedSystemctlFake(t, rec, unit, "42", false),
	)
	c.deps.ProcRoot = writeProcCgroup(t, doctorFakeDaemonPID, "0::/system.slice/"+unit+"\n")

	check := findDoctorCheck(t, c.runDoctor(context.Background()), "daemon-restarts")
	if check.Level != doctorWarn {
		t.Fatalf("daemon-restarts = %+v, want WARN on churn", check)
	}
	if !strings.Contains(check.Message, "-u '"+unit+"'") {
		t.Fatalf("journal hint in %q does not quote the unit; a shell would eat the escape", check.Message)
	}
}

// TestDoctorDaemonRestartsAcceptsLiteralBackslashUnits pins that a LITERAL
// backslash is a valid systemd unit-name character. systemd.unit(5) lists it
// among the valid characters; `\xNN` is the escaping convention
// unit_name_escape() emits, not a validity rule. A previous tightening required
// the escaped form and so rejected real units, reporting the check unavailable
// for a daemon that was running fine. This test exists to stop that recurring.
func TestDoctorDaemonRestartsAcceptsLiteralBackslashUnits(t *testing.T) {
	for _, unit := range []string{`ao\garbage.service`, `ao\x2.service`, `ao\xZZ.service`} {
		t.Run(unit, func(t *testing.T) {
			cfg := setConfigEnv(t)
			srv := doctorDaemonServer(t)
			rec := &doctorProbeRecorder{}
			c, _ := doctorLiveDaemonContext(t, cfg, srv,
				map[string]string{"git": "/bin/git", "systemctl": "/bin/systemctl"},
				scopedSystemctlFake(t, rec, unit, "0", false),
			)
			c.deps.ProcRoot = writeProcCgroup(t, doctorFakeDaemonPID, "0::/system.slice/"+unit+"\n")

			check := findDoctorCheck(t, c.runDoctor(context.Background()), "daemon-restarts")
			if check.Level != doctorPass || !strings.Contains(check.Message, unit) {
				t.Fatalf("daemon-restarts = %+v, want PASS naming the unit %q", check, unit)
			}
		})
	}
}

// TestDoctorDaemonRestartsUnescapesCgroupNames covers systemd's cgroup-filename
// escaping: a name starting with `_`, or colliding with a controller filename,
// is written with one extra leading underscore. Reading it back without
// reversing that asks systemd about a unit that does not exist — and systemctl
// answers 0 for an unknown unit, so doctor would report a healthy PASS.
func TestDoctorDaemonRestartsUnescapesCgroupNames(t *testing.T) {
	for _, tc := range []struct{ onCgroup, wantUnit string }{
		{"__ao.service", "_ao.service"},
		{"_cpu.service", "cpu.service"},
		{"ao.service", "ao.service"},
	} {
		t.Run(tc.onCgroup, func(t *testing.T) {
			cfg := setConfigEnv(t)
			srv := doctorDaemonServer(t)
			rec := &doctorProbeRecorder{}
			c, _ := doctorLiveDaemonContext(t, cfg, srv,
				map[string]string{"git": "/bin/git", "systemctl": "/bin/systemctl"},
				scopedSystemctlFake(t, rec, tc.wantUnit, "0", false),
			)
			c.deps.ProcRoot = writeProcCgroup(t, doctorFakeDaemonPID, "0::/system.slice/"+tc.onCgroup+"\n")

			check := findDoctorCheck(t, c.runDoctor(context.Background()), "daemon-restarts")
			// Anchored on the message's leading token: the escaped form is a
			// superstring of the unescaped one, so Contains cannot tell them apart.
			if check.Level != doctorPass || !strings.HasPrefix(check.Message, tc.wantUnit+" has restarted") {
				t.Fatalf("daemon-restarts = %+v, want PASS naming exactly the unescaped unit %q", check, tc.wantUnit)
			}
		})
	}
}

// TestDoctorDaemonRestartsAcceptsWellFormedEscapes is the other half: a real
// `\xNN` escape must still be recognized, or tightening the pattern would
// silently break the units it exists to match.
func TestDoctorDaemonRestartsAcceptsWellFormedEscapes(t *testing.T) {
	cfg := setConfigEnv(t)
	srv := doctorDaemonServer(t)
	rec := &doctorProbeRecorder{}
	const unit = `ao\x2dworker:main.service`
	c, _ := doctorLiveDaemonContext(t, cfg, srv,
		map[string]string{"git": "/bin/git", "systemctl": "/bin/systemctl"},
		scopedSystemctlFake(t, rec, unit, "0", false),
	)
	c.deps.ProcRoot = writeProcCgroup(t, doctorFakeDaemonPID, "0::/system.slice/"+unit+"\n")

	check := findDoctorCheck(t, c.runDoctor(context.Background()), "daemon-restarts")
	if check.Level != doctorPass || !strings.Contains(check.Message, unit) {
		t.Fatalf("daemon-restarts = %+v, want PASS naming the escaped unit %q", check, unit)
	}
}

// TestCgroupUnescape pins the transformation directly, by equality. The
// indirect checks cannot: systemd's escaped form is a superstring of the
// unescaped one, so `strings.Contains` is satisfied either way and a test built
// on it would pass with the unescape step deleted.
func TestCgroupUnescape(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"__ao.service", "_ao.service"}, // a unit literally named _ao.service
		{"_cpu.service", "cpu.service"}, // collided with a controller filename
		{"ao.service", "ao.service"},    // never escaped, must be untouched
		{"", ""},                        // degenerate, must not panic
		{"_", ""},                       // bare prefix
		{"a_b.service", "a_b.service"},  // underscore not leading, untouched
	} {
		t.Run(tc.in, func(t *testing.T) {
			if got := cgroupUnescape(tc.in); got != tc.want {
				t.Fatalf("cgroupUnescape(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
