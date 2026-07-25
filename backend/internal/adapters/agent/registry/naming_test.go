package registry

import (
	"context"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

const namingProbeName = "ao #150 naming"
const namingProbePrompt = "/address-issue 150"

// TestNamingNeverTakesThePositionalSlot pins the invariant that cost the prior
// implementation dearly: claude and codex each have exactly one positional
// startup argument, and spending it on a rename pushed the real prompt onto a
// post-start keystroke write that then collided with the rename. The pane showed
// both concatenated and the worker never ran its task.
//
// Two halves, because each alone can pass vacuously. The capability half is
// host-independent: no adapter's launch-naming form may smuggle in a rename
// command or a positional separator. The argv half exercises the real launch
// command wherever the harness binary resolves on this host, and fails if it
// managed to exercise nothing at all.
func TestNamingNeverTakesThePositionalSlot(t *testing.T) {
	for _, ha := range Harnessed() {
		namer, ok := ha.Agent.(ports.AgentNamer)
		if !ok {
			continue
		}
		t.Run(string(ha.Harness)+"/capability", func(t *testing.T) {
			rename, ok := namer.InHarnessRenameCommand(namingProbeName)
			if !ok {
				t.Fatalf("%s declares ports.AgentNamer but has no in-harness rename command; the in-harness form is the universal path", ha.Harness)
			}
			if !strings.Contains(rename, namingProbeName) {
				t.Fatalf("in-harness rename %q does not carry the name verbatim", rename)
			}
			for _, arg := range namer.LaunchNameArgs(namingProbeName) {
				if arg == "--" {
					t.Fatalf("%s: LaunchNameArgs emits a positional separator", ha.Harness)
				}
				if strings.Contains(arg, rename) {
					t.Fatalf("%s: LaunchNameArgs carries the in-harness rename command %q", ha.Harness, rename)
				}
			}
		})
	}

	exercised := 0
	for _, ha := range Harnessed() {
		cmd, err := ha.Agent.GetLaunchCommand(context.Background(), ports.LaunchConfig{
			SessionID:   "11111111-1111-4111-8111-111111111111",
			DisplayName: namingProbeName,
			Prompt:      namingProbePrompt,
			DataDir:     t.TempDir(),
		})
		if err != nil {
			// The harness binary is not installed on this host; nothing to inspect.
			continue
		}
		exercised++
		t.Run(string(ha.Harness)+"/argv", func(t *testing.T) {
			for _, arg := range cmd {
				if strings.Contains(arg, "/rename") {
					t.Fatalf("%s launch command %v carries a rename", ha.Harness, cmd)
				}
			}
			sep := -1
			for i, arg := range cmd {
				if arg == "--" {
					sep = i
					break
				}
			}
			if sep < 0 {
				return
			}
			positional := cmd[sep+1:]
			if len(positional) != 1 || positional[0] != namingProbePrompt {
				t.Fatalf("%s positional args = %v, want exactly the prompt %q", ha.Harness, positional, namingProbePrompt)
			}
		})
	}
	// Hermetic without any proprietary CLI installed: the fake adapter resolves a
	// shell rather than a harness binary, so at least one adapter always builds a
	// launch command on a POSIX host. Failing here rather than skipping is the
	// point — a skip is exactly the vacuous pass this half exists to prevent.
	if exercised == 0 {
		t.Fatal("no adapter produced a launch command, not even the shell-backed fake adapter, so the argv half checked nothing")
	}
}
