package domain

import "testing"

func TestComposePrimeDisplayName(t *testing.T) {
	tests := []struct {
		name        string
		projectName string
		want        string
	}{
		{"short project uppercases", "ao", "AO Prime"},
		{"long project caps before suffix", "Agent Orchestrator", "Agent Orchestr Prime"},
		{"trims trailing separator after cap", "abcdefghijklm-orchestrator", "abcdefghijklm Prime"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ComposePrimeDisplayName(tt.projectName); got != tt.want {
				t.Fatalf("ComposePrimeDisplayName(%q) = %q, want %q", tt.projectName, got, tt.want)
			}
		})
	}
}
