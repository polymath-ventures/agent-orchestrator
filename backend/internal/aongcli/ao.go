package aongcli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
)

// aoBinaryName is the co-installed `ao` executable's file name.
func aoBinaryName() string {
	if runtime.GOOS == "windows" {
		return "ao.exe"
	}
	return "ao"
}

// resolveAO finds the `ao` executable, preferring the one shipped beside this
// binary over whatever is on PATH. aong and ao are released as a pair, so
// "the pair that shipped together stays together" is a property worth keeping
// by construction: resolving PATH first would let a stale ao from an older
// install silently service a new aong.
func (c *commandContext) resolveAO() (string, error) {
	var searched []string

	exe, err := c.deps.Executable()
	if err == nil {
		// Resolve symlinks so an aong installed as a link into a release
		// directory looks for its sibling in the release directory, not in the
		// bin directory holding the link.
		if resolved, linkErr := filepath.EvalSymlinks(exe); linkErr == nil {
			exe = resolved
		}
		dir := filepath.Dir(exe)
		candidate := filepath.Join(dir, aoBinaryName())
		if isExecutableFile(candidate) {
			return candidate, nil
		}
		searched = append(searched, dir)
	}

	if path, lookErr := c.deps.LookPath(aoBinaryName()); lookErr == nil {
		return path, nil
	}
	searched = append(searched, "PATH")

	return "", fmt.Errorf("%s executable not found (searched %s)", aoBinaryName(), strings.Join(searched, ", "))
}

// isExecutableFile reports whether path is a regular file this process could
// exec. A sibling that merely has the right name — a stray data file, a
// half-written download — must not beat a working `ao` on PATH.
func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	if runtime.GOOS == "windows" {
		// Windows has no exec bit; the .exe extension is the marker, and
		// aoBinaryName already supplies it.
		return true
	}
	return info.Mode().Perm()&0o111 != 0
}

// runAO invokes `ao` and returns its combined output. A failure is reported
// with the exact command and that command's own output, so the operator debugs
// `ao`, not aong's paraphrase of it.
func (c *commandContext) runAO(ctx context.Context, args ...string) ([]byte, error) {
	aoPath, err := c.resolveAO()
	if err != nil {
		return nil, err
	}
	c.explainAO("run", args...)
	out, err := c.deps.RunCommand(ctx, aoPath, args...)
	if err != nil {
		return out, fmt.Errorf("ao %s: %w%s", strings.Join(args, " "), err, indentedOutput(out))
	}
	return out, nil
}

// echoAO runs an `ao` command and relays its output verbatim.
func (c *commandContext) echoAO(ctx context.Context, out io.Writer, args ...string) error {
	stdout, err := c.runAO(ctx, args...)
	if err != nil {
		return err
	}
	return relay(out, stdout)
}

func (c *commandContext) runAOPassthrough(ctx context.Context, args ...string) error {
	aoPath, err := c.resolveAO()
	if err != nil {
		return err
	}
	c.explainAO("passthrough", args...)
	if err := c.deps.RunStreamingCommand(ctx, aoPath, args, c.deps.In, c.deps.Out, c.deps.Err); err != nil {
		return passthroughError{err: err}
	}
	return nil
}

type passthroughError struct {
	err error
}

func (e passthroughError) Error() string { return e.err.Error() }
func (e passthroughError) Unwrap() error { return e.err }
func (e passthroughError) Silent() bool {
	var exitErr *exec.ExitError
	return errors.As(e.err, &exitErr)
}
func (e passthroughError) ExitCode() int {
	var exitErr *exec.ExitError
	if errors.As(e.err, &exitErr) {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			return 128 + int(status.Signal())
		}
	}
	var exitCoder interface{ ExitCode() int }
	if errors.As(e.err, &exitCoder) {
		return exitCoder.ExitCode()
	}
	return 1
}

func (c *commandContext) explainAO(kind string, args ...string) {
	if !c.verbose {
		return
	}
	_, _ = fmt.Fprintf(c.deps.Err, "aong: %s: ao %s\n", kind, strings.Join(args, " "))
}

// relay writes command output through with exactly one trailing newline, so
// composed output does not accumulate blank lines between sections.
func relay(out io.Writer, data []byte) error {
	text := strings.TrimRight(string(data), "\n")
	if text == "" {
		return nil
	}
	_, err := fmt.Fprintln(out, text)
	return err
}

func indentedOutput(out []byte) string {
	text := strings.TrimRight(string(out), "\n")
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = "  " + line
	}
	return "\n" + strings.Join(lines, "\n")
}
