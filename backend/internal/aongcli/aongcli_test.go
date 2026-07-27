package aongcli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	aocli "github.com/aoagents/agent-orchestrator/backend/internal/cli"
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
	stream   func(call recordedCall, in io.Reader, out, errOut io.Writer) error
	lookPath map[string]string
	exePath  string
	in       io.Reader
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
	if h.in == nil {
		h.in = strings.NewReader("")
	}
	return Deps{
		In:         h.in,
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
		RunStreamingCommand: func(_ context.Context, name string, args []string, in io.Reader, out io.Writer, errOut io.Writer) error {
			call := recordedCall{name: name, args: args}
			h.calls = append(h.calls, call)
			if h.stream == nil {
				return nil
			}
			return h.stream(call, in, out, errOut)
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
	err := executeWithDeps(h.deps(&out, &errOut), args)
	return out.String(), errOut.String(), err
}

type exitCodeError int

func (e exitCodeError) Error() string { return fmt.Sprintf("exit status %d", e) }
func (e exitCodeError) ExitCode() int { return int(e) }

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

func TestResolveAONonExecutableSiblingDoesNotDisplacePath(t *testing.T) {
	h := newFakeHost(t)
	// A file that merely shares the name — a stray artifact, a half-written
	// download — must not beat a working ao on PATH.
	sibling := filepath.Join(filepath.Dir(h.exePath), aoBinaryName())
	if err := os.Chmod(sibling, 0o644); err != nil {
		t.Fatal(err)
	}
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
		t.Fatalf("resolveAO() = %q, want the executable on PATH %q", got, onPath)
	}
}

// An aong installed as a symlink into a release directory must look for its
// sibling in the release directory, not in the bin directory holding the link.
func TestResolveAOResolvesSymlinkedSelf(t *testing.T) {
	h := newFakeHost(t)
	releaseDir := filepath.Dir(h.exePath) // already holds an executable `ao`
	realAong := h.exePath
	if err := os.WriteFile(realAong, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	linkDir := t.TempDir()
	link := filepath.Join(linkDir, "aong")
	if err := os.Symlink(realAong, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	// Executable() reports the link path, as it does on platforms that do not
	// resolve /proc/self/exe.
	h.exePath = link

	c := &commandContext{deps: h.deps(&bytes.Buffer{}, &bytes.Buffer{}).withDefaults()}
	got, err := c.resolveAO()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(releaseDir, aoBinaryName())
	if got != want {
		t.Fatalf("resolveAO() = %q, want the sibling of the symlink TARGET %q", got, want)
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

// --- passthrough ---------------------------------------------------------

func TestPassthroughForwardsNonOverriddenVerb(t *testing.T) {
	h := newFakeHost(t)
	h.in = strings.NewReader("payload")
	h.stream = func(call recordedCall, in io.Reader, out, errOut io.Writer) error {
		body, err := io.ReadAll(in)
		if err != nil {
			t.Fatal(err)
		}
		if string(body) != "payload" {
			t.Fatalf("stdin = %q, want payload", body)
		}
		_, _ = fmt.Fprint(out, "project ok\n")
		_, _ = fmt.Fprint(errOut, "project warn\n")
		return nil
	}

	out, errOut, err := run(t, h, "project", "list")
	if err != nil {
		t.Fatal(err)
	}
	if got := h.aoArgv(); len(got) != 1 || got[0] != aoBinaryName()+" project list" {
		t.Fatalf("ao calls = %v, want one `ao project list`", got)
	}
	if out != "project ok\n" {
		t.Fatalf("stdout = %q, want streamed ao stdout", out)
	}
	if errOut != "project warn\n" {
		t.Fatalf("stderr = %q, want streamed ao stderr", errOut)
	}
}

func TestPassthroughPreservesAOExitCode(t *testing.T) {
	h := newFakeHost(t)
	h.stream = func(recordedCall, io.Reader, io.Writer, io.Writer) error {
		return exitCodeError(7)
	}

	_, _, err := run(t, h, "project")
	if got := ExitCode(err); got != 7 {
		t.Fatalf("ExitCode(%v) = %d, want 7", err, got)
	}
}

func TestShouldPrintErrorSuppressesOnlySilentPassthroughErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "passthrough exit", err: passthroughError{err: &exec.ExitError{}}, want: false},
		{name: "passthrough start failure", err: passthroughError{err: errors.New("exec format error")}, want: true},
		{name: "usage", err: usageError{err: errors.New("bad args")}, want: true},
		{name: "runtime", err: errors.New("boom"), want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ShouldPrintError(tc.err); got != tc.want {
				t.Fatalf("ShouldPrintError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestPassthroughSignalExitCodeUsesShellConvention(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signal exit status is Unix-specific")
	}
	err := exec.Command("sh", "-c", "kill -TERM $$").Run()
	if err == nil {
		t.Fatal("expected command to terminate by signal")
	}
	if got := (passthroughError{err: err}).ExitCode(); got != 143 {
		t.Fatalf("ExitCode(%v) = %d, want 143", err, got)
	}
}

func TestPassthroughVerboseReportsAOInvocation(t *testing.T) {
	h := newFakeHost(t)

	_, errOut, err := run(t, h, "--verbose", "project", "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errOut, "ao project list") {
		t.Fatalf("verbose output %q does not name passthrough invocation", errOut)
	}
}

func TestPassthroughKeepsFlagsAfterVerb(t *testing.T) {
	h := newFakeHost(t)

	if _, _, err := run(t, h, "project", "--verbose"); err != nil {
		t.Fatal(err)
	}
	if got := h.aoArgv(); len(got) != 1 || got[0] != aoBinaryName()+" project --verbose" {
		t.Fatalf("ao calls = %v, want one `ao project --verbose`", got)
	}
}

func TestHelpForPassthroughVerbDelegatesToAO(t *testing.T) {
	for _, args := range [][]string{{"project", "--help"}, {"help", "project"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			h := newFakeHost(t)
			if _, _, err := run(t, h, args...); err != nil {
				t.Fatal(err)
			}
			if got := h.aoArgv(); len(got) != 1 || got[0] != aoBinaryName()+" project --help" {
				t.Fatalf("ao calls = %v, want one `ao project --help`", got)
			}
		})
	}
}

func TestCurrentAOCommandsAreReachableThroughAONG(t *testing.T) {
	aoRoot := aocli.NewRootCommand(aocli.Deps{Out: &bytes.Buffer{}, Err: &bytes.Buffer{}, In: strings.NewReader("")})
	for _, aoCmd := range aoRoot.Commands() {
		if aoCmd.Hidden || isAONGOverride(aoCmd.Name()) {
			continue
		}
		t.Run(aoCmd.Name(), func(t *testing.T) {
			h := newFakeHost(t)
			if _, _, err := run(t, h, aoCmd.Name(), "--help"); err != nil {
				t.Fatal(err)
			}
			want := aoBinaryName() + " " + aoCmd.Name() + " --help"
			if got := h.aoArgv(); len(got) != 1 || got[0] != want {
				t.Fatalf("ao calls = %v, want [%q]", got, want)
			}
		})
	}
}

func TestAONGOverrideTableMatchesRegisteredCommands(t *testing.T) {
	root := NewRootCommand(Deps{Out: &bytes.Buffer{}, Err: &bytes.Buffer{}, In: strings.NewReader("")})
	registered := map[string]struct{}{}
	for _, cmd := range root.Commands() {
		if cmd.Hidden {
			continue
		}
		registered[cmd.Name()] = struct{}{}
	}
	delete(registered, "help")

	for name := range aongOverrideNames {
		if _, ok := registered[name]; !ok {
			t.Fatalf("override table includes %q, but root does not register that command", name)
		}
	}
	for name := range registered {
		if _, ok := aongOverrideNames[name]; !ok {
			t.Fatalf("root registers %q, but override table does not include it", name)
		}
	}
}

// --- environment detection ----------------------------------------------

func TestDetectEnvironmentSystemd(t *testing.T) {
	h := systemdHost(t, "ao.service")
	c := &commandContext{deps: h.deps(&bytes.Buffer{}, &bytes.Buffer{}).withDefaults()}

	env, err := c.detectEnvironment(context.Background())
	if err != nil {
		t.Fatal(err)
	}
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

	env, err := c.detectEnvironment(context.Background())
	if err != nil {
		t.Fatal(err)
	}
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

	env, err := c.detectEnvironment(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if env.Kind != envPlain {
		t.Fatalf("Kind = %q, want %q", env.Kind, envPlain)
	}
}

// A probe that could not be answered is not evidence that AO is absent. A
// missing user bus or a permission failure on a real systemd host would
// otherwise silently look identical to a laptop with no units at all.
func TestDetectEnvironmentReportsProbeFailureInsteadOfPlain(t *testing.T) {
	h := newFakeHost(t)
	h.lookPath["systemctl"] = "/usr/bin/systemctl"
	h.respond = func(recordedCall) ([]byte, error) {
		return []byte("Failed to connect to bus\n"), errors.New("exit status 1")
	}
	c := &commandContext{deps: h.deps(&bytes.Buffer{}, &bytes.Buffer{}).withDefaults()}

	env, err := c.detectEnvironment(context.Background())
	if err == nil {
		t.Fatalf("probe failure was swallowed; env = %+v", env)
	}
	if env.Kind == envPlain {
		t.Fatal("probe failure was classified as a plain environment")
	}
	if !strings.Contains(err.Error(), "Failed to connect to bus") {
		t.Fatalf("error %q does not include systemctl's own output", err)
	}
}

func TestStartFailsLoudlyOnProbeFailure(t *testing.T) {
	h := newFakeHost(t)
	h.lookPath["systemctl"] = "/usr/bin/systemctl"
	h.respond = func(recordedCall) ([]byte, error) {
		return []byte("Failed to connect to bus\n"), errors.New("exit status 1")
	}

	_, _, err := run(t, h, "start")
	if err == nil {
		t.Fatal("expected start to fail when the unit probe fails")
	}
	if strings.Contains(err.Error(), "no AO service units") {
		t.Fatalf("probe failure was reported as an absent deployment: %v", err)
	}
}

func TestStatusReportsProbeFailureWithoutFailing(t *testing.T) {
	h := newFakeHost(t)
	h.lookPath["systemctl"] = "/usr/bin/systemctl"
	h.respond = func(call recordedCall) ([]byte, error) {
		if filepath.Base(call.name) == aoBinaryName() {
			return []byte("AO daemon: ready\n"), nil
		}
		return []byte("Failed to connect to bus\n"), errors.New("exit status 1")
	}

	out, _, err := run(t, h, "status")
	if err != nil {
		t.Fatalf("status failed on a systemd probe failure: %v", err)
	}
	if !strings.Contains(out, "AO daemon: ready") {
		t.Fatalf("status dropped the daemon state:\n%s", out)
	}
	if !strings.Contains(out, "environment: unknown") {
		t.Fatalf("status did not surface the probe failure:\n%s", out)
	}
	if strings.Contains(out, "environment: plain") {
		t.Fatalf("status reported a probe failure as a plain environment:\n%s", out)
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

// --- doctor --------------------------------------------------------------

func TestDoctorRunsAODoctorAndReportsNoForkUnitsOnPlainHost(t *testing.T) {
	h := newFakeHost(t)
	h.stream = func(_ recordedCall, _ io.Reader, out, _ io.Writer) error {
		_, _ = fmt.Fprint(out, "ao doctor ok\n")
		return nil
	}

	out, _, err := run(t, h, "doctor")
	if err != nil {
		t.Fatal(err)
	}
	if got := h.aoArgv(); len(got) != 1 || got[0] != aoBinaryName()+" doctor" {
		t.Fatalf("ao calls = %v, want one `ao doctor`", got)
	}
	for _, want := range []string{"ao doctor ok", "fork services: not found"} {
		if !strings.Contains(out, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, out)
		}
	}
}

func TestDoctorReportsLoadedForkUnits(t *testing.T) {
	h := systemdHost(t, "ao-tmux.service", "ao.service", "ao-web.service")
	h.stream = func(_ recordedCall, _ io.Reader, out, _ io.Writer) error {
		_, _ = fmt.Fprint(out, "ao doctor ok\n")
		return nil
	}

	out, _, err := run(t, h, "doctor")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"fork service ao-web.service: active", "fork service ao-tmux.service: active"} {
		if !strings.Contains(out, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "fork service ao.service") {
		t.Fatalf("doctor should not duplicate ao.service health:\n%s", out)
	}
}

func TestDoctorReportsForkUnitsEvenWhenAODoctorFails(t *testing.T) {
	h := systemdHost(t, "ao-tmux.service", "ao-web.service")
	h.stream = func(_ recordedCall, _ io.Reader, out, _ io.Writer) error {
		_, _ = fmt.Fprint(out, "ao doctor failed\n")
		return exitCodeError(3)
	}

	out, _, err := run(t, h, "doctor")
	if err == nil {
		t.Fatal("expected doctor to fail when ao doctor fails")
	}
	for _, want := range []string{"ao doctor failed", "fork service ao-web.service: active", "fork service ao-tmux.service: active"} {
		if !strings.Contains(out, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, out)
		}
	}
	if got := ExitCode(err); got != 1 {
		t.Fatalf("ExitCode(%v) = %d, want override runtime failure exit 1", err, got)
	}
}

func TestDoctorFailsLoadedUnhealthyForkUnit(t *testing.T) {
	h := forkUnitStateHost(t, map[string]string{
		"ao-tmux.service": "active",
		"ao-web.service":  "failed",
	})
	h.stream = func(_ recordedCall, _ io.Reader, out, _ io.Writer) error {
		_, _ = fmt.Fprint(out, "ao doctor ok\n")
		return nil
	}

	out, _, err := run(t, h, "doctor")
	if err == nil {
		t.Fatal("expected doctor to fail on a loaded unhealthy fork unit")
	}
	if !strings.Contains(out, "fork service ao-web.service: failed") {
		t.Fatalf("doctor output does not report failed unit:\n%s", out)
	}
}

func TestDoctorJSONAugmentsAOReport(t *testing.T) {
	for _, args := range [][]string{{"doctor", "--json"}, {"doctor", "--json=true"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			h := systemdHost(t, "ao-tmux.service", "ao-web.service")
			h.stream = func(_ recordedCall, _ io.Reader, out, _ io.Writer) error {
				_, _ = fmt.Fprint(out, `{"ok":true,"failures":0,"generatedAt":"later","checks":[]}`)
				return nil
			}

			out, _, err := run(t, h, args...)
			if err != nil {
				t.Fatal(err)
			}
			if got := h.aoArgv(); len(got) != 1 || got[0] != aoBinaryName()+" doctor --json" {
				t.Fatalf("ao calls = %v, want one `ao doctor --json`", got)
			}
			var report aongDoctorReport
			if err := json.Unmarshal([]byte(out), &report); err != nil {
				t.Fatalf("decode doctor json: %v\nout=%s", err, out)
			}
			if !strings.Contains(out, "\n  ") {
				t.Fatalf("doctor json was not pretty-printed like ao doctor json:\n%s", out)
			}
			var raw map[string]any
			if err := json.Unmarshal([]byte(out), &raw); err != nil {
				t.Fatalf("decode doctor json as map: %v\nout=%s", err, out)
			}
			if raw["generatedAt"] != "later" {
				t.Fatalf("doctor json dropped unknown report field: %+v", raw)
			}
			for _, want := range []string{"ao-web.service", "ao-tmux.service"} {
				var found bool
				for _, check := range report.Checks {
					if check.Name == want && check.Level == "PASS" && check.Section == "Fork services" {
						found = true
					}
				}
				if !found {
					t.Fatalf("doctor json missing fork check %q: %+v", want, report.Checks)
				}
			}
			if !report.OK || report.Failures != 0 {
				t.Fatalf("doctor json = %+v, want ok", report)
			}
		})
	}
}

func TestDoctorJSONPreservesAOReportWhenForkProbeFails(t *testing.T) {
	h := newFakeHost(t)
	h.lookPath["systemctl"] = "/usr/bin/systemctl"
	h.stream = func(_ recordedCall, _ io.Reader, out, _ io.Writer) error {
		_, _ = fmt.Fprint(out, `{"ok":true,"failures":0,"checks":[]}`)
		return nil
	}
	h.respond = func(call recordedCall) ([]byte, error) {
		if filepath.Base(call.name) == "systemctl" {
			return []byte("Failed to connect to bus\n"), errors.New("exit status 1")
		}
		return nil, nil
	}

	out, _, err := run(t, h, "doctor", "--json")
	if err == nil {
		t.Fatal("expected doctor json to fail when fork service probe fails")
	}
	if !strings.Contains(out, `"ok":true`) {
		t.Fatalf("doctor json discarded ao report after fork probe failure:\n%s", out)
	}
}

func forkUnitStateHost(t *testing.T, activeStates map[string]string) *fakeHost {
	t.Helper()
	h := newFakeHost(t)
	h.lookPath["systemctl"] = "/usr/bin/systemctl"
	h.respond = func(call recordedCall) ([]byte, error) {
		if filepath.Base(call.name) != "systemctl" || len(call.args) < 4 || call.args[1] != "show" {
			return nil, nil
		}
		unit, property := call.args[2], call.args[len(call.args)-1]
		switch property {
		case "LoadState":
			if _, ok := activeStates[unit]; ok {
				return []byte("loaded\n"), nil
			}
			return []byte("not-found\n"), nil
		case "ActiveState":
			if state, ok := activeStates[unit]; ok {
				return []byte(state + "\n"), nil
			}
			return []byte("inactive\n"), nil
		}
		return nil, nil
	}
	return h
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

func TestOverrideVerboseReportsComposedAOInvocation(t *testing.T) {
	h := newFakeHost(t)

	_, errOut, err := run(t, h, "--verbose", "drain")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errOut, "ao pause --all") {
		t.Fatalf("verbose output %q does not name composed invocation", errOut)
	}
}

func TestFlagsAfterOverrideVerbAreNotStolenByAONG(t *testing.T) {
	h := newFakeHost(t)

	if _, _, err := run(t, h, "drain", "--verbose"); ExitCode(err) != 2 {
		t.Fatalf("ExitCode(%v) = %d, want usage exit 2", err, ExitCode(err))
	}
	if got := h.aoArgv(); len(got) != 0 {
		t.Fatalf("bad override args invoked ao: %v", got)
	}
}

func TestOverrideHelpStaysLocal(t *testing.T) {
	for _, args := range [][]string{{"drain", "--help"}, {"help", "drain"}, {"doctor", "--help"}, {"help", "doctor"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			h := newFakeHost(t)
			out, _, err := run(t, h, args...)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out, "Run ao doctor") && !strings.Contains(out, "Gate new work") {
				t.Fatalf("override help did not render local help:\n%s", out)
			}
			if got := h.aoArgv(); len(got) != 0 {
				t.Fatalf("override help delegated to ao: %v", got)
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

func TestPauseRedirectsWithoutAliasingDrain(t *testing.T) {
	// `ao` has no gate-without-draining capability, so `aong pause` must teach
	// the honest verbs instead of silently aliasing to drain.
	h := newFakeHost(t)
	out, _, err := run(t, h, "pause")
	if ExitCode(err) != 2 {
		t.Fatalf("ExitCode(%v) = %d, want usage exit 2", err, ExitCode(err))
	}
	if got := h.aoArgv(); len(got) != 0 {
		t.Fatalf("pause invoked ao instead of redirecting: %v", got)
	}
	for _, want := range []string{"aong drain", "aong stop-work"} {
		if !strings.Contains(out, want) {
			t.Fatalf("pause output missing %q:\n%s", want, out)
		}
	}
}

func TestPauseAllRedirectsWithoutAliasingDrain(t *testing.T) {
	h := newFakeHost(t)

	out, _, err := run(t, h, "pause", "--all")
	if ExitCode(err) != 2 {
		t.Fatalf("ExitCode(%v) = %d, want usage exit 2", err, ExitCode(err))
	}
	if got := h.aoArgv(); len(got) != 0 {
		t.Fatalf("pause --all invoked ao instead of redirecting: %v", got)
	}
	for _, want := range []string{"aong drain", "aong stop-work"} {
		if !strings.Contains(out, want) {
			t.Fatalf("pause --all output missing %q:\n%s", want, out)
		}
	}
}

func TestPauseProjectPassesThrough(t *testing.T) {
	h := newFakeHost(t)
	h.respond = func(recordedCall) ([]byte, error) { return []byte("project paused\n"), nil }

	out, _, err := run(t, h, "pause", "mercury")
	if err != nil {
		t.Fatal(err)
	}
	if got := h.aoArgv(); len(got) != 1 || got[0] != aoBinaryName()+" pause mercury" {
		t.Fatalf("ao calls = %v, want one `ao pause mercury`", got)
	}
	for _, want := range []string{"project paused", "aong resume mercury"} {
		if !strings.Contains(out, want) {
			t.Fatalf("pause project output missing %q:\n%s", want, out)
		}
	}
}

func TestPauseProjectHardPassesThrough(t *testing.T) {
	h := newFakeHost(t)
	h.respond = func(recordedCall) ([]byte, error) { return []byte("project stopped\n"), nil }

	out, _, err := run(t, h, "pause", "mercury", "--hard")
	if err != nil {
		t.Fatal(err)
	}
	if got := h.aoArgv(); len(got) != 1 || got[0] != aoBinaryName()+" pause mercury --hard" {
		t.Fatalf("ao calls = %v, want one `ao pause mercury --hard`", got)
	}
	for _, want := range []string{"project stopped", "aong resume mercury"} {
		if !strings.Contains(out, want) {
			t.Fatalf("pause project hard output missing %q:\n%s", want, out)
		}
	}
}

func TestPauseRejectsMultipleProjects(t *testing.T) {
	h := newFakeHost(t)

	_, _, err := run(t, h, "pause", "mercury", "venus")
	if ExitCode(err) != 2 {
		t.Fatalf("ExitCode(%v) = %d, want usage exit 2", err, ExitCode(err))
	}
	if got := h.aoArgv(); len(got) != 0 {
		t.Fatalf("pause with multiple projects invoked ao: %v", got)
	}
	if !strings.Contains(err.Error(), "expected at most one project") {
		t.Fatalf("pause multiple-project error = %q, want arity error", err)
	}
}

func TestResumeProjectPassesThrough(t *testing.T) {
	h := newFakeHost(t)
	h.respond = func(recordedCall) ([]byte, error) { return []byte("project resumed\n"), nil }

	out, _, err := run(t, h, "resume", "mercury")
	if err != nil {
		t.Fatal(err)
	}
	if got := h.aoArgv(); len(got) != 1 || got[0] != aoBinaryName()+" resume mercury" {
		t.Fatalf("ao calls = %v, want one `ao resume mercury`", got)
	}
	if !strings.Contains(out, "project resumed") {
		t.Fatalf("resume project output missing ao output:\n%s", out)
	}
}

func TestPauseVerboseNamesDivergence(t *testing.T) {
	h := newFakeHost(t)

	_, errOut, err := run(t, h, "--verbose", "pause")
	if ExitCode(err) != 2 {
		t.Fatalf("ExitCode(%v) = %d, want usage exit 2", err, ExitCode(err))
	}
	if !strings.Contains(errOut, "diverges") {
		t.Fatalf("verbose pause output %q does not name divergence", errOut)
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

func TestShutdownVerboseReportsStatusProbe(t *testing.T) {
	h := newFakeHost(t)
	h.respond = func(call recordedCall) ([]byte, error) {
		if len(call.args) >= 2 && call.args[0] == "status" {
			return []byte(`{"state":"ready"}`), nil
		}
		return nil, nil
	}

	_, errOut, err := run(t, h, "--verbose", "shutdown")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ao status --json", "ao pause --all --hard", "ao stop"} {
		if !strings.Contains(errOut, want) {
			t.Fatalf("verbose shutdown output missing %q:\n%s", want, errOut)
		}
	}
}

// A failed stop-work never stops the daemon — for ANY state except a daemon
// proven absent. "stale" is in the list deliberately: `ao stop` would delete the
// stale run file and report success, so continuing would let shutdown exit 0
// while a live daemon it could not reach kept running.
func TestShutdownAbortsWhenStopWorkFails(t *testing.T) {
	for _, state := range []string{"ready", "unhealthy", "not_ready", "stale", "some-future-state"} {
		t.Run(state, func(t *testing.T) {
			h := newFakeHost(t)
			h.respond = func(call recordedCall) ([]byte, error) {
				switch {
				case len(call.args) >= 2 && call.args[0] == "status":
					return []byte(`{"state":"` + state + `"}`), nil
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
		})
	}
}

// Refusing must not be a dead end: the error has to name the verb that does
// reconcile a stale run file, or an operator whose daemon is already gone has
// no way forward from shutdown's message alone.
func TestShutdownFailureNamesTheRecoveryVerb(t *testing.T) {
	h := newFakeHost(t)
	h.respond = func(call recordedCall) ([]byte, error) {
		switch {
		case len(call.args) >= 2 && call.args[0] == "status":
			return []byte(`{"state":"stale"}`), nil
		case len(call.args) >= 1 && call.args[0] == "pause":
			return []byte("connection refused\n"), errors.New("exit status 1")
		}
		return nil, nil
	}

	_, _, err := run(t, h, "shutdown")
	if err == nil {
		t.Fatal("expected shutdown to fail when work could not be stopped")
	}
	if !strings.Contains(err.Error(), "aong stop") {
		t.Fatalf("error %q does not name the recovery verb", err)
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("error %q drops ao's own output", err)
	}
}

// Only a state that PROVES no live daemon exists may skip stop-work.
func TestShutdownSkipsStopWorkOnlyWhenDaemonIsProvenAbsent(t *testing.T) {
	for _, state := range []string{"stopped"} {
		t.Run(state, func(t *testing.T) {
			h := newFakeHost(t)
			h.respond = func(call recordedCall) ([]byte, error) {
				if len(call.args) >= 2 && call.args[0] == "status" {
					return []byte(`{"state":"` + state + `"}`), nil
				}
				return nil, nil
			}

			if _, _, err := run(t, h, "shutdown"); err != nil {
				t.Fatal(err)
			}
			got := h.aoArgv()
			for _, line := range got {
				if strings.Contains(line, "pause") {
					t.Fatalf("gated the fleet with no daemon: %v", got)
				}
			}
			if got[len(got)-1] != aoBinaryName()+" stop" {
				t.Fatalf("ao calls = %v, want the last to be `ao stop`", got)
			}
		})
	}
}

// A daemon whose health or readiness probe is currently failing is still a live
// daemon with live agents. Treating "not ready" as "nothing to stop" would stop
// the daemon out from under running work — the exact state shutdown prevents.
func TestShutdownStopsWorkWhenDaemonStateIsIndeterminate(t *testing.T) {
	// "stale" is in this list deliberately: `ao` reports it both for a run file
	// pointing at a dead process AND for a live process whose ownership probe
	// failed, so it is not proof that nothing is running and cannot skip the gate.
	for _, state := range []string{"unhealthy", "not_ready", "stale", "", "some-future-state"} {
		t.Run("state="+state, func(t *testing.T) {
			h := newFakeHost(t)
			h.respond = func(call recordedCall) ([]byte, error) {
				if len(call.args) >= 2 && call.args[0] == "status" {
					return []byte(`{"state":"` + state + `"}`), nil
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
		})
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
		{"--nope"},
		{"status", "--nope"},
		{"drain", "unexpected-arg"},
		{"help", "status", "typo"}, // including an unknown topic trailing a known one
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

func TestOverrideRuntimeFailureDoesNotPreserveChildExitCode(t *testing.T) {
	h := newFakeHost(t)
	h.respond = func(recordedCall) ([]byte, error) { return nil, exitCodeError(7) }

	_, _, err := run(t, h, "resume")
	if ExitCode(err) != 1 {
		t.Fatalf("ExitCode(%v) = %d, want override runtime failure exit 1", err, ExitCode(err))
	}
}

func TestSuccessExitsZero(t *testing.T) {
	h := newFakeHost(t)
	_, _, err := run(t, h, "resume")
	if ExitCode(err) != 0 {
		t.Fatalf("ExitCode(%v) = %d, want 0", err, ExitCode(err))
	}
}

// Making the root runnable (so an unknown verb is misuse) must not break the
// ordinary help and version paths.
func TestHelpAndVersionPathsStillSucceed(t *testing.T) {
	for _, args := range [][]string{{}, {"--help"}, {"--version"}, {"help"}, {"help", "-h"}, {"help", "--help"}, {"help", "status"}} {
		t.Run(strings.Join(append([]string{"aong"}, args...), " "), func(t *testing.T) {
			h := newFakeHost(t)
			out, _, err := run(t, h, args...)
			if ExitCode(err) != 0 {
				t.Fatalf("ExitCode(%v) = %d, want 0", err, ExitCode(err))
			}
			if strings.TrimSpace(out) == "" {
				t.Fatal("printed nothing")
			}
			if len(h.calls) != 0 {
				t.Fatalf("a help/version path ran commands: %v", h.argv())
			}
		})
	}
}
