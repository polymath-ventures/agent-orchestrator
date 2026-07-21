//go:build !windows

package claudecode

import (
	"os/exec"
	"syscall"
)

// configureProbeProcessGroup ensures cancellation kills provider/MCP
// descendants as well as the Claude shim process.
func configureProbeProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
}
