//go:build !windows

package opencode

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestAvailableModelsCancellationKillsDescendants(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "child.pid")
	scriptPath := filepath.Join(dir, "opencode")
	script := `#!/bin/sh
sleep 30 &
echo $! > "$OPENCODE_CHILD_PID"
wait
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENCODE_CHILD_PID", pidPath)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	models, err := (&Plugin{resolvedBinary: scriptPath}).AvailableModels(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("AvailableModels() = (%#v, %v), want deadline", models, err)
	}
	raw, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("read child pid: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("parse child pid: %v", err)
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })

	deadline := time.Now().Add(modelCatalogWaitDelay + 3*time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err != nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("catalog descendant pid %d survived cancellation", pid)
}
