package domain

import "testing"

func TestNormalizeEffortForHarness(t *testing.T) {
	tests := []struct {
		name    string
		harness AgentHarness
		effort  Effort
		want    Effort
	}{
		{"fugu max maps to native xhigh", HarnessCodexFugu, EffortMax, EffortXHigh},
		{"fugu xhigh stays xhigh", HarnessCodexFugu, EffortXHigh, EffortXHigh},
		{"plain codex max is unchanged", HarnessCodex, EffortMax, EffortMax},
		{"unset stays unset", HarnessCodexFugu, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeEffortForHarness(tt.harness, tt.effort); got != tt.want {
				t.Fatalf("NormalizeEffortForHarness(%q, %q) = %q, want %q", tt.harness, tt.effort, got, tt.want)
			}
		})
	}
}
