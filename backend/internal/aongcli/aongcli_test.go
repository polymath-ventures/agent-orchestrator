package aongcli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// recordedCall is one command aong asked the OS to run.
type recordedCall struct {
	name string
	args []string
}

func (r recordedCall) String() string {
	return strings.TrimSpace(r.name + " " + strings.Join(r.args, " "))
}

// fakeHost stands in for the machine: which binaries resolve, what each command
// prints, and whether it fails. Every test asserts on the recorded argv, which
// is the whole contract of a porcelain that only composes other commands.
type fakeHost struct {
	t     *testing.T
	calls []recordedCall
	// respond returns output and error for a call, keyed by the rendered argv
	// suffix (the binary's base name plus args).
	respond  func(call recordedCall) ([]byte, error)
	lookPath map[string]string
	exePath  string
}

func newFakeHost(t *testing.T) *fakeHost {
	t.Helper()
	dir := t.TempDir()
	aoPath := filepath.Join(dir, aoBinaryName())
	if err := os.WriteFile(aoPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return &fakeHost{
		t:        t,
		lookPath: map[string]string{},
		exePath:  filepath.Join(dir, "aong"),
	}
}

func (h *fakeHost) deps(out, errOut *bytes.Buffer) Deps {
	return Deps{
		Out:        out,
		Err:        errOut,
		Executable: func() (string, error) { return h.exePath, nil },
		LookPath: func(file string) (string, error) {
			if path, ok := h.lookPath[file]; ok {
				return path, nil
			}
			return "", fmt.Errorf("exec: %q: executable file not found in $PATH", file)
		},
		RunCommand: func(_ context.Context, name string, args ...string) ([]byte, error) {
			call := recordedCall{name: name, args: args}
			h.calls = append(h.calls, call)
			if h.respond == nil {
				return nil, nil
			}
			return h.respond(call)
		},
	}
}

// argv renders the recorded calls with binaries reduced to their base names so
// assertions are not tied to temp-dir paths.
func (h *fakeHost) argv() []string {
	rendered := make([]string, 0, len(h.calls))
	for _, call := range h.calls {
		rendered = append(rendered, strings.TrimSpace(filepath.Base(call.name)+" "+strings.Join(call.args, " ")))
	}
	return rendered
}

func (h *fakeHost) aoArgv() []string {
	var rendered []string
	for _, line := range h.argv() {
		if strings.HasPrefix(line, aoBinaryName()+" ") || line == aoBinaryName() {
			rendered = append(rendered, line)
		}
	}
	return rendered
}

// run executes aong with the given args against the fake host.
func run(t *testing.T, h *fakeHost, args ...string) (string, string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	cmd := NewRootCommand(h.deps(&out, &errOut))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), errOut.String(), err
}

// systemdHost configures the fake host as a systemd deployment where the named
// units are loaded and every other AO unit is not-found.
func systemdHost(t *testing.T, loaded ...string) *fakeHost {
	t.Helper()
	h := newFakeHost(t)
	h.lookPath["systemctl"] = "/usr/bin/systemctl"
	isLoaded := map[string]bool{}
	for _, unit := range loaded {
		isLoaded[unit] = true
	}
	h.respond = func(call recordedCall) ([]byte, error) {
		if filepath.Base(call.name) != "systemctl" {
			return nil, nil
		}
		if len(call.args) >= 4 && call.args[1] == "show" {
			unit, property := call.args[2], call.args[len(call.args)-1]
			switch property {
			case "LoadState":
				if isLoaded[unit] {
					return []byte("loaded\n"), nil
				}
				return []byte("not-found\n"), nil
			case "ActiveState":
				if isLoaded[unit] {
					return []byte("active\n"), nil
				}
				return []byte("inactive\n"), nil
			}
		}
		return nil, nil
	}
	return h
}

// --- ao resolution -------------------------------------------------------

func TestResolveAOPrefersSiblingOverPath(t *testing.T) {
	h := newFakeHost(t)
	otherDir := t.TempDir()
	stale := filepath.Join(otherDir, aoBinaryName())
	if err := os.WriteFile(stale, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	h.lookPath[aoBinaryName()] = stale

	c := &commandContext{deps: h.deps(&bytes.Buffer{}, &bytes.Buffer{}).withDefaults()}
	got, err := c.resolveAO()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(filepath.Dir(h.exePath), aoBinaryName())
	if got != want {
		t.Fatalf("resolveAO() = %q, want sibling %q (PATH had %q)", got, want, stale)
	}
}

func TestResolveAOFallsBackToPath(t *testing.T) {
	h := newFakeHost(t)
	// No sibling: point Executable at a directory that has no `ao`.
	h.exePath = filepath.Join(t.TempDir(), "aong")
	onPath := filepath.Join(t.TempDir(), aoBinaryName())
	if err := os.WriteFile(onPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	h.lookPath[aoBinaryName()] = onPath

	c := &commandContext{deps: h.deps(&bytes.Buffer{}, &bytes.Buffer{}).withDefaults()}
	got, err := c.resolveAO()
	if err != nil {
		t.Fatal(err)
	}
	if got != onPath {
		t.Fatalf("resolveAO() = %q, want %q", got, onPath)
	}
}

func TestResolveAOMissingNamesBothLocations(t *testing.T) {
	h := newFakeHost(t)
	siblingDir := t.TempDir()
	h.exePath = filepath.Join(siblingDir, "aong")

	c := &commandContext{deps: h.deps(&bytes.Buffer{}, &bytes.Buffer{}).withDefaults()}
	_, err := c.resolveAO()
	if err == nil {
		t.Fatal("expected an error when no ao executable exists")
	}
	if !strings.Contains(err.Error(), siblingDir) || !strings.Contains(err.Error(), "PATH") {
		t.Fatalf("error %q does not name both the sibling dir and PATH", err)
	}
}

func TestMissingAOPerformsNoLifecycleAction(t *testing.T) {
	h := newFakeHost(t)
	h.exePath = filepath.Join(t.TempDir(), "aong")

	if _, _, err := run(t, h, "drain"); err == nil {
		t.Fatal("expected drain to fail when ao is missing")
	}
	if len(h.calls) != 0 {
		t.Fatalf("ran commands despite unresolvable ao: %v", h.argv())
	}
}

func TestAOFailureSurfacesCommandAndOutput(t *testing.T) {
	h := newFakeHost(t)
	h.respond = func(recordedCall) ([]byte, error) {
		return []byte("AO daemon is not running\n"), errors.New("exit status 1")
	}

	_, _, err := run(t, h, "drain")
	if err == nil {
		t.Fatal("expected drain to fail")
	}
	if !strings.Contains(err.Error(), "ao pause --all") {
		t.Fatalf("error %q does not name the ao command that failed", err)
	}
	if !strings.Contains(err.Error(), "AO daemon is not running") {
		t.Fatalf("error %q does not include ao's own output", err)
	}
	if len(h.aoArgv()) != 1 {
		t.Fatalf("retried or substituted a fallback: %v", h.aoArgv())
	}
}

// --- environment detection ----------------------------------------------

func TestDetectEnvironmentSystemd(t *testing.T) {
	h := systemdHost(t, "ao.service")
	c := &commandContext{deps: h.deps(&bytes.Buffer{}, &bytes.Buffer{}).withDefaults()}

	env := c.detectEnvironment(context.Background())
	if env.Kind != envSystemd {
		t.Fatalf("Kind = %q, want %q", env.Kind, envSystemd)
	}
	if len(env.LoadedUnits) != 1 || env.LoadedUnits[0] != "ao.service" {
		t.Fatalf("LoadedUnits = %v, want [ao.service]", env.LoadedUnits)
	}
}

func TestDetectEnvironmentPlainWithoutSystemctl(t *testing.T) {
	h := newFakeHost(t)
	c := &commandContext{deps: h.deps(&bytes.Buffer{}, &bytes.Buffer{}).withDefaults()}

	env := c.detectEnvironment(context.Background())
	if env.Kind != envPlain {
		t.Fatalf("Kind = %q, want %q", env.Kind, envPlain)
	}
	if len(h.calls) != 0 {
		t.Fatalf("probed systemd despite no systemctl: %v", h.argv())
	}
}

func TestDetectEnvironmentPlainWhenNoUnitsLoaded(t *testing.T) {
	h := systemdHost(t) // systemctl present, nothing loaded
	c := &commandContext{deps: h.deps(&bytes.Buffer{}, &bytes.Buffer{}).withDefaults()}

	if env := c.detectEnvironment(context.Background()); env.Kind != envPlain {
		t.Fatalf("Kind = %q, want %q", env.Kind, envPlain)
	}
}

// --- status --------------------------------------------------------------

func TestStatusDelegatesToAOAndAddsUnits(t *testing.T) {
	h := systemdHost(t, "ao-tmux.service", "ao.service")
	systemctlRespond := h.respond
	h.respond = func(call recordedCall) ([]byte, error) {
		if filepath.Base(call.name) == aoBinaryName() {
			return []byte("AO daemon: ready\n  fleet: running\n"), nil
		}
		return systemctlRespond(call)
	}

	out, _, err := run(t, h, "status")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"AO daemon: ready", "fleet: running", "services:", "ao-tmux.service: active", "ao.service: active"} {
		if !strings.Contains(out, want) {
			t.Fatalf("status output missing %q:\n%s", want, out)
		}
	}
	if got := h.aoArgv(); len(got) != 1 || got[0] != aoBinaryName()+" status" {
		t.Fatalf("ao calls = %v, want one `ao status`", got)
	}
	// ao-web.service is not loaded, so it must not appear.
	if strings.Contains(out, "ao-web.service") {
		t.Fatalf("reported an unloaded unit:\n%s", out)
	}
}

func TestStatusInPlainEnvironmentHasNoServiceSection(t *testing.T) {
	h := newFakeHost(t)
	h.respond = func(recordedCall) ([]byte, error) { return []byte("AO daemon: ready\n"), nil }

	out, _, err := run(t, h, "status")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "environment: plain") {
		t.Fatalf("status did not report the detected environment:\n%s", out)
	}
	if strings.Contains(out, "services:") {
		t.Fatalf("plain environment reported a service section:\n%s", out)
	}
}

func TestStatusReportsStoppedDaemonWithoutFailing(t *testing.T) {
	h := systemdHost(t, "ao.service")
	systemctlRespond := h.respond
	h.respond = func(call recordedCall) ([]byte, error) {
		if filepath.Base(call.name) == aoBinaryName() {
			return []byte("AO daemon: stopped\n"), nil
		}
		return systemctlRespond(call)
	}

	out, _, err := run(t, h, "status")
	if err != nil {
		t.Fatalf("status failed with a stopped daemon: %v", err)
	}
	if !strings.Contains(out, "AO daemon: stopped") {
		t.Fatalf("status output missing the stopped daemon state:\n%s", out)
	}
}

// --- work-control verbs --------------------------------------------------

func TestWorkVerbsComposeTheRightAOCommands(t *testing.T) {
	for _, tc := range []struct {
		verb string
		want string
	}{
		{"drain", aoBinaryName() + " pause --all"},
		{"stop-work", aoBinaryName() + " pause --all --hard"},
		{"resume", aoBinaryName() + " resume --all"},
	} {
		t.Run(tc.verb, func(t *testing.T) {
			h := newFakeHost(t)
			if _, _, err := run(t, h, tc.verb); err != nil {
				t.Fatal(err)
			}
			got := h.aoArgv()
			if len(got) != 1 || got[0] != tc.want {
				t.Fatalf("ao calls = %v, want [%q]", got, tc.want)
			}
		})
	}
}

func TestGatingVerbsNameTheWayBack(t *testing.T) {
	for _, verb := range []string{"drain", "stop-work"} {
		t.Run(verb, func(t *testing.T) {
			h := newFakeHost(t)
			out, _, err := run(t, h, verb)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out, "aong resume") {
				t.Fatalf("%s output does not name `aong resume`:\n%s", verb, out)
			}
		})
	}
}

func TestNoPauseVerbIsRegistered(t *testing.T) {
	// `ao` has no gate-without-draining capability, so a `pause` distinct from
	// `drain` cannot be composed — and aliasing it to drain would restore the
	// dishonest name this CLI exists to remove.
	root := NewRootCommand(Deps{Out: &bytes.Buffer{}, Err: &bytes.Buffer{}})
	for _, sub := range root.Commands() {
		if sub.Name() == "pause" || sub.HasAlias("pause") {
			t.Fatalf("aong registers a `pause` verb: %q", sub.Use)
		}
	}
}

// --- stop / shutdown -----------------------------------------------------

func TestStopDelegatesAndDisclosesSurvivingSessions(t *testing.T) {
	h := newFakeHost(t)
	h.respond = func(recordedCall) ([]byte, error) { return []byte("AO daemon stopped\n"), nil }

	out, _, err := run(t, h, "stop")
	if err != nil {
		t.Fatal(err)
	}
	if got := h.aoArgv(); len(got) != 1 || got[0] != aoBinaryName()+" stop" {
		t.Fatalf("ao calls = %v, want one `ao stop`", got)
	}
	if !strings.Contains(out, "Agent sessions keep running") {
		t.Fatalf("stop output does not disclose that sessions survive:\n%s", out)
	}
	if !strings.Contains(out, "aong shutdown") {
		t.Fatalf("stop output does not name the verb that also stops work:\n%s", out)
	}
}

func TestShutdownStopsWorkBeforeDaemon(t *testing.T) {
	h := newFakeHost(t)
	h.respond = func(call recordedCall) ([]byte, error) {
		if len(call.args) >= 2 && call.args[0] == "status" {
			return []byte(`{"state":"ready"}`), nil
		}
		return nil, nil
	}

	if _, _, err := run(t, h, "shutdown"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		aoBinaryName() + " status --json",
		aoBinaryName() + " pause --all --hard",
		aoBinaryName() + " stop",
	}
	got := h.aoArgv()
	if len(got) != len(want) {
		t.Fatalf("ao calls = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ao calls = %v, want %v", got, want)
		}
	}
}

func TestShutdownAbortsWhenStopWorkFails(t *testing.T) {
	h := newFakeHost(t)
	h.respond = func(call recordedCall) ([]byte, error) {
		switch {
		case len(call.args) >= 2 && call.args[0] == "status":
			return []byte(`{"state":"ready"}`), nil
		case len(call.args) >= 1 && call.args[0] == "pause":
			return []byte("terminate failed\n"), errors.New("exit status 1")
		}
		return nil, nil
	}

	if _, _, err := run(t, h, "shutdown"); err == nil {
		t.Fatal("expected shutdown to fail when stop-work fails")
	}
	for _, line := range h.aoArgv() {
		if line == aoBinaryName()+" stop" {
			t.Fatalf("stopped the daemon after a failed stop-work: %v", h.aoArgv())
		}
	}
}

func TestShutdownSkipsStopWorkWhenDaemonNotReady(t *testing.T) {
	h := newFakeHost(t)
	h.respond = func(call recordedCall) ([]byte, error) {
		if len(call.args) >= 2 && call.args[0] == "status" {
			return []byte(`{"state":"stopped"}`), nil
		}
		return nil, nil
	}

	if _, _, err := run(t, h, "shutdown"); err != nil {
		t.Fatal(err)
	}
	got := h.aoArgv()
	for _, line := range got {
		if strings.Contains(line, "pause") {
			t.Fatalf("gated the fleet with no reachable daemon: %v", got)
		}
	}
	if got[len(got)-1] != aoBinaryName()+" stop" {
		t.Fatalf("ao calls = %v, want the last to be `ao stop`", got)
	}
}

// --- start ---------------------------------------------------------------

func TestStartStartsLoadedUnitsInOrder(t *testing.T) {
	h := systemdHost(t, "ao-tmux.service", "ao.service", "ao-web.service")

	out, _, err := run(t, h, "start")
	if err != nil {
		t.Fatal(err)
	}
	var started []string
	for _, call := range h.calls {
		if len(call.args) == 3 && call.args[1] == "start" {
			started = append(started, call.args[2])
		}
	}
	want := []string{"ao-tmux.service", "ao.service", "ao-web.service"}
	if len(started) != len(want) {
		t.Fatalf("started = %v, want %v", started, want)
	}
	for i := range want {
		if started[i] != want[i] {
			t.Fatalf("started = %v, want %v (dependency order)", started, want)
		}
	}
	for _, unit := range want {
		if !strings.Contains(out, "started "+unit) {
			t.Fatalf("start output does not report %q:\n%s", unit, out)
		}
	}
}

func TestStartSkipsUnloadedUnitsWithoutFailing(t *testing.T) {
	h := systemdHost(t, "ao.service")

	out, _, err := run(t, h, "start")
	if err != nil {
		t.Fatalf("start failed because some units are absent: %v", err)
	}
	for _, call := range h.calls {
		if len(call.args) == 3 && call.args[1] == "start" && call.args[2] != "ao.service" {
			t.Fatalf("started an unloaded unit: %v", call)
		}
	}
	if strings.Contains(out, "ao-web.service") {
		t.Fatalf("reported an unloaded unit:\n%s", out)
	}
}

func TestStartInPlainEnvironmentReportsInsteadOfPretending(t *testing.T) {
	h := newFakeHost(t)

	_, _, err := run(t, h, "start")
	if err == nil {
		t.Fatal("expected start to fail in a plain environment")
	}
	if !strings.Contains(err.Error(), "no AO service units") {
		t.Fatalf("error %q does not state that no units were found", err)
	}
	if !strings.Contains(err.Error(), "ao daemon") {
		t.Fatalf("error %q does not name how a daemon is started here", err)
	}
	if len(h.calls) != 0 {
		t.Fatalf("ran commands in a plain environment: %v", h.argv())
	}
}

// --- exit codes ----------------------------------------------------------

func TestUsageErrorsExitTwo(t *testing.T) {
	for _, args := range [][]string{
		{"status", "--nope"},
		{"drain", "unexpected-arg"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			h := newFakeHost(t)
			if _, _, err := run(t, h, args...); ExitCode(err) != 2 {
				t.Fatalf("ExitCode(%v) = %d, want 2", err, ExitCode(err))
			}
		})
	}
}

func TestRuntimeFailureExitsOne(t *testing.T) {
	h := newFakeHost(t)
	h.respond = func(recordedCall) ([]byte, error) { return nil, errors.New("exit status 1") }

	_, _, err := run(t, h, "resume")
	if ExitCode(err) != 1 {
		t.Fatalf("ExitCode(%v) = %d, want 1", err, ExitCode(err))
	}
}

func TestSuccessExitsZero(t *testing.T) {
	h := newFakeHost(t)
	_, _, err := run(t, h, "resume")
	if ExitCode(err) != 0 {
		t.Fatalf("ExitCode(%v) = %d, want 0", err, ExitCode(err))
	}
}
