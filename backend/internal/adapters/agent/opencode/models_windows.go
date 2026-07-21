//go:build windows

package opencode

import "os/exec"

// CommandContext terminates the direct process on Windows; WaitDelay bounds
// inherited output pipes where POSIX process groups are unavailable.
func configureModelCatalogProcessGroup(cmd *exec.Cmd) {}
