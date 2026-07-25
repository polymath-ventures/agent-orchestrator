package ports_test

import (
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// The gate on what may be typed into a harness. A name that reaches a shell
// instead of a TUI must be inert, so an operator override carrying shell syntax
// is refused outright rather than repaired into a different name.
func TestDeliverableName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{"computed worker name", "ao #150 Unified", "ao #150 Unified", true},
		{"orchestrator name", "ao Orc", "ao Orc", true},
		{"trims surrounding space", "  ao Orc  ", "ao Orc", true},
		{"empty", "", "", false},
		{"blank", "   ", "", false},
		{"newline", "one\ntwo", "", false},
		{"command separator", "x; touch /tmp/pwn", "", false},
		{"command substitution", "x`id`", "", false},
		{"dollar expansion", "x$HOME", "", false},
		{"pipe", "x | sh", "", false},
		{"background", "x & sh", "", false},
		{"glob", "x*", "", false},
		{"quote", `x"y`, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ports.DeliverableName(tt.in)
			if ok != tt.ok || got != tt.want {
				t.Fatalf("DeliverableName(%q) = %q, %v; want %q, %v", tt.in, got, ok, tt.want, tt.ok)
			}
		})
	}
}
