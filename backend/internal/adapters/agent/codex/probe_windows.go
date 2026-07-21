//go:build windows

package codex

import "os/exec"

// Windows has no POSIX process group to signal. CommandContext terminates the
// direct process and WaitDelay bounds inherited pipes.
func configureProbeProcessGroup(cmd *exec.Cmd) {}
