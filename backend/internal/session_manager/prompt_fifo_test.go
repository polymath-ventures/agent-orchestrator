//go:build unix

package sessionmanager

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestLoadRoleRules_FifoFailsClosedWithoutBlocking guards the regression where a
// rules-file path points at a FIFO: a blocking open for read waits until a
// writer appears, so the loader must open non-blocking and reject the file via
// an f.Stat() on that handle before any read, rather than hanging the
// spawn/inspection goroutine.
func TestLoadRoleRules_FifoFailsClosedWithoutBlocking(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "rules.md")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("mkfifo unsupported: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := LoadRoleRules(RoleRulesConfig{
			Role:        "worker",
			ProjectID:   "mer",
			ProjectPath: dir,
			RulesFile:   "rules.md",
		})
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected a FIFO rules file to fail closed")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("LoadRoleRules blocked on a FIFO instead of failing closed")
	}
}
