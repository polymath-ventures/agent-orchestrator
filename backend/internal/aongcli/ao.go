package aongcli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
		dir := filepath.Dir(exe)
		candidate := filepath.Join(dir, aoBinaryName())
		if info, statErr := os.Stat(candidate); statErr == nil && info.Mode().IsRegular() {
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

// runAO invokes `ao` and returns its combined output. A failure is reported
// with the exact command and that command's own output, so the operator debugs
// `ao`, not aong's paraphrase of it.
func (c *commandContext) runAO(ctx context.Context, args ...string) ([]byte, error) {
	aoPath, err := c.resolveAO()
	if err != nil {
		return nil, err
	}
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
