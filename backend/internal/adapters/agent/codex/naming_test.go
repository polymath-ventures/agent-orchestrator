package codex

import (
	"context"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func TestPluginImplementsAgentNamer(t *testing.T) {
	var _ ports.AgentNamer = New()
	var _ ports.AgentNamer = NewFugu()
}

// codex has no launch-time naming flag, so it is named through the universal
// in-harness command after the TUI is ready.
func TestCodexInHarnessRenameCommand(t *testing.T) {
	for _, p := range []*Plugin{New(), NewFugu()} {
		cmd, ok := p.InHarnessRenameCommand("ao #1929 sqlite3")
		if !ok {
			t.Fatalf("%s: InHarnessRenameCommand ok = false, want true", p.adapterID())
		}
		if cmd != "/rename ao #1929 sqlite3" {
			t.Fatalf("%s: InHarnessRenameCommand = %q, want %q", p.adapterID(), cmd, "/rename ao #1929 sqlite3")
		}
		if args := p.LaunchNameArgs("ao #1929 sqlite3"); args != nil {
			t.Fatalf("%s: LaunchNameArgs = %v, want nil (codex has no launch-time naming flag)", p.adapterID(), args)
		}
	}
}

// A name that is empty, or that would carry a newline into the pane, must not
// produce a rename command: the newline would submit the command mid-name and
// leave the rest of the string as a prompt.
func TestCodexInHarnessRenameCommandRejectsUnsafeNames(t *testing.T) {
	p := New()
	for _, name := range []string{"", "   ", "\n", "one\ntwo"} {
		if cmd, ok := p.InHarnessRenameCommand(name); ok {
			t.Fatalf("InHarnessRenameCommand(%q) = %q, true; want ok=false", name, cmd)
		}
	}
}

// The single positional startup slot belongs to the prompt. A rename in argv is
// what made the prior implementation's workers come up idle with a concatenated
// pane, so assert the launch command never carries one.
func TestCodexLaunchCommandKeepsThePositionalForThePrompt(t *testing.T) {
	p := New()
	cmd, err := p.GetLaunchCommand(context.Background(), ports.LaunchConfig{
		SessionID:   "sess-1",
		DisplayName: "ao #150 naming",
		Prompt:      "/address-issue 150",
	})
	if err != nil {
		t.Skipf("codex binary unavailable: %v", err)
	}
	assertPromptOwnsPositional(t, cmd, "/address-issue 150")
}

func assertPromptOwnsPositional(t *testing.T, cmd []string, prompt string) {
	t.Helper()
	sep := -1
	for i, arg := range cmd {
		if arg == "--" {
			sep = i
			break
		}
	}
	if sep < 0 {
		t.Fatalf("launch command %v has no positional separator", cmd)
	}
	positional := cmd[sep+1:]
	if len(positional) != 1 || positional[0] != prompt {
		t.Fatalf("positional args = %v, want exactly the prompt %q", positional, prompt)
	}
	for _, arg := range cmd {
		if strings.Contains(arg, "/rename") {
			t.Fatalf("launch command %v carries a rename; naming must never displace the prompt", cmd)
		}
	}
}

// A codex session is named after start, so it must declare readiness hints —
// without them the write races the TUI and is dropped or, worse, queued and
// delivered to the model as a prompt.
func TestCodexDeclaresPromptReadinessHints(t *testing.T) {
	for _, p := range []*Plugin{New(), NewFugu()} {
		var provider ports.AgentPromptReadinessProvider = p
		hints, err := provider.PromptReadinessHints(context.Background(), ports.LaunchConfig{})
		if err != nil {
			t.Fatalf("%s: PromptReadinessHints: %v", p.adapterID(), err)
		}
		if len(hints.Patterns) == 0 {
			t.Fatalf("%s: no readiness patterns; a post-start write would race the TUI", p.adapterID())
		}
		if hints.Timeout <= 0 {
			t.Fatalf("%s: readiness timeout = %v, want a bounded deadline", p.adapterID(), hints.Timeout)
		}
	}
}
