//go:build !windows

package codex

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func TestValidateModelTimeoutKillsProbeDescendants(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	bin := writeFakeScript(t, `#!/bin/sh
sleep 30 &
echo $! > "$AO_CHILD_PID_FILE"
wait
`)
	t.Setenv("AO_CHILD_PID_FILE", pidFile)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	got, err := (&Plugin{resolvedBinary: bin}).ValidateModel(ctx, "gpt-native")
	if err != nil {
		t.Fatalf("ValidateModel: %v", err)
	}
	if got.Status != ports.ModelValidationProbeUnavailable {
		t.Fatalf("status = %q, want probe-unavailable", got.Status)
	}
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("read child pid: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("parse child pid: %v", err)
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })

	deadline := time.Now().Add(probeWaitDelay + 3*time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err != nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("probe descendant pid %d survived timeout", pid)
}
