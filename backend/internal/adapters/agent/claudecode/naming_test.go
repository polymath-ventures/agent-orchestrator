package claudecode

import (
	"context"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func TestPluginImplementsAgentNamer(t *testing.T) {
	var _ ports.AgentNamer = New()
}

func TestClaudeInHarnessRenameCommand(t *testing.T) {
	p := New()
	cmd, ok := p.InHarnessRenameCommand("ao #150 naming")
	if !ok {
		t.Fatal("InHarnessRenameCommand ok = false, want true")
	}
	if cmd != "/rename ao #150 naming" {
		t.Fatalf("InHarnessRenameCommand = %q, want %q", cmd, "/rename ao #150 naming")
	}
}

func TestClaudeInHarnessRenameCommandRejectsUnsafeNames(t *testing.T) {
	p := New()
	for _, name := range []string{"", "   ", "\n", "one\ntwo"} {
		if cmd, ok := p.InHarnessRenameCommand(name); ok {
			t.Fatalf("InHarnessRenameCommand(%q) = %q, true; want ok=false", name, cmd)
		}
	}
}

// claude-code's -n is a flag, not the positional, so it competes with nothing
// and names the session atomically with process start.
func TestClaudeLaunchNameArgs(t *testing.T) {
	p := New()
	got := p.LaunchNameArgs("ao #150 naming")
	want := []string{"-n", "ao #150 naming"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("LaunchNameArgs = %v, want %v", got, want)
	}
	if args := p.LaunchNameArgs("  "); args != nil {
		t.Fatalf("LaunchNameArgs(blank) = %v, want nil", args)
	}
	if args := p.LaunchNameArgs("one\ntwo"); args != nil {
		t.Fatalf("LaunchNameArgs with a newline = %v, want nil", args)
	}
}

// The launch flag is only load-bearing if the adapter actually splices it in —
// and it must land before the positional separator, or it becomes prompt text.
func TestClaudeLaunchCommandCarriesTheNameBeforeThePrompt(t *testing.T) {
	p := New()
	cmd, err := p.GetLaunchCommand(context.Background(), ports.LaunchConfig{
		SessionID:   "11111111-1111-4111-8111-111111111111",
		DisplayName: "ao #150 naming",
		Prompt:      "/address-issue 150",
	})
	if err != nil {
		t.Skipf("claude binary unavailable: %v", err)
	}

	sep := -1
	nameFlag := -1
	for i, arg := range cmd {
		switch {
		case arg == "--" && sep < 0:
			sep = i
		case arg == "-n" && nameFlag < 0:
			nameFlag = i
		}
	}
	if sep < 0 {
		t.Fatalf("launch command %v has no positional separator", cmd)
	}
	if nameFlag < 0 {
		t.Fatalf("launch command %v carries no -n flag", cmd)
	}
	if nameFlag > sep {
		t.Fatalf("launch command %v puts -n after the positional separator", cmd)
	}
	if cmd[nameFlag+1] != "ao #150 naming" {
		t.Fatalf("launch command %v carries name %q, want %q", cmd, cmd[nameFlag+1], "ao #150 naming")
	}
	positional := cmd[sep+1:]
	if len(positional) != 1 || positional[0] != "/address-issue 150" {
		t.Fatalf("positional args = %v, want exactly the prompt", positional)
	}
	for _, arg := range cmd {
		if strings.Contains(arg, "/rename") {
			t.Fatalf("launch command %v carries a rename; naming must never displace the prompt", cmd)
		}
	}
}

// Without a computed name the launch command is unchanged, so an explicit
// operator name and an absent name behave the same as they do today.
func TestClaudeLaunchCommandOmitsNameFlagWhenUnnamed(t *testing.T) {
	p := New()
	cmd, err := p.GetLaunchCommand(context.Background(), ports.LaunchConfig{
		SessionID: "11111111-1111-4111-8111-111111111111",
		Prompt:    "/address-issue 150",
	})
	if err != nil {
		t.Skipf("claude binary unavailable: %v", err)
	}
	for _, arg := range cmd {
		if arg == "-n" {
			t.Fatalf("launch command %v carries -n with no display name", cmd)
		}
	}
}

// The launch flag must stay an optimization, not the mechanism. Readiness hints
// are what let the universal in-harness path stand on its own: with `-n`
// disabled and no hints, the rename lands in a bare shell before Claude Code has
// drawn anything and is lost.
func TestClaudeDeclaresPromptReadinessHints(t *testing.T) {
	var provider ports.AgentPromptReadinessProvider = New()
	hints, err := provider.PromptReadinessHints(context.Background(), ports.LaunchConfig{})
	if err != nil {
		t.Fatalf("PromptReadinessHints: %v", err)
	}
	if len(hints.Patterns) == 0 {
		t.Fatal("no readiness patterns; a post-start write would race the TUI")
	}
	if hints.Timeout <= 0 {
		t.Fatalf("readiness timeout = %v, want a bounded deadline", hints.Timeout)
	}
}
