package domain

import "testing"

func TestClassifyModelProvider(t *testing.T) {
	tests := []struct {
		model string
		want  ModelProvider
	}{
		{"", ProviderUnknown},
		{"opus", ProviderAnthropic},
		{"claude-opus-4-8", ProviderAnthropic},
		{"sonnet", ProviderAnthropic},
		{"haiku", ProviderAnthropic},
		{"claude-fable-5", ProviderAnthropic},
		{"fable", ProviderAnthropic},
		{"gpt-5.5-codex", ProviderOpenAI},
		{"gpt-4o", ProviderOpenAI},
		{"o3", ProviderOpenAI},
		{"o1-mini", ProviderOpenAI},
		{"o4-mini", ProviderOpenAI},
		// Fugu reuses the Codex binary but owns a distinct model namespace. Match
		// the Fugu family before the generic Codex/OpenAI fragment.
		{"fugu", ProviderFugu},
		{"fugu-ultra", ProviderFugu},
		{"codex-fugu", ProviderFugu},
		// Classification is case-insensitive and trims surrounding whitespace, so a
		// model copied out of a config file classifies the same as a typed one.
		{"CLAUDE-OPUS-4-8", ProviderAnthropic},
		{"GPT-4o", ProviderOpenAI},
		{"  sonnet  ", ProviderAnthropic},
		{"\tcodex\n", ProviderOpenAI},
		{"   ", ProviderUnknown},
		// unrecognized models stay unknown so resolution is permissive.
		{"llama-3", ProviderUnknown},
		{"some-internal-model", ProviderUnknown},
		// A family fragment embedded in a longer word must NOT classify: "octopus"
		// contains "opus" but is not an Anthropic model, and misclassifying it
		// would falsely reject it on a non-Anthropic bucket.
		{"octopus-v1", ProviderUnknown},
		{"octopus", ProviderUnknown},
		{"opusculum", ProviderUnknown},
		// A multibyte letter adjacent to the fragment is a letter boundary too, not
		// a delimiter, so it must not classify.
		{"éopus", ProviderUnknown},
		{"opusé", ProviderUnknown},
		{"gpt4", ProviderOpenAI}, // digit boundary still matches the family
		{"claude-opus-4-1", ProviderAnthropic},
	}
	for _, tt := range tests {
		if got := ClassifyModelProvider(tt.model); got != tt.want {
			t.Errorf("ClassifyModelProvider(%q) = %q, want %q", tt.model, got, tt.want)
		}
	}
}

func TestAgentHarnessModelProvider(t *testing.T) {
	tests := []struct {
		harness AgentHarness
		want    ModelProvider
	}{
		{HarnessClaudeCode, ProviderAnthropic},
		{HarnessCodex, ProviderOpenAI},
		{HarnessCodexFugu, ProviderFugu},
		// Every harness AO has not mapped stays unknown (unguarded).
		{HarnessAider, ProviderUnknown},
		{HarnessGoose, ProviderUnknown},
		{"", ProviderUnknown},
	}
	for _, tt := range tests {
		if got := tt.harness.ModelProvider(); got != tt.want {
			t.Errorf("%q.ModelProvider() = %q, want %q", tt.harness, got, tt.want)
		}
	}
}

// TestModelProviderCompatibleWith pins the permissive-on-unknown rule: guarding
// only ever fires on a known-vs-known mismatch, in either direction.
func TestModelProviderCompatibleWith(t *testing.T) {
	tests := []struct {
		name    string
		model   ModelProvider
		harness ModelProvider
		want    bool
	}{
		{"same provider", ProviderAnthropic, ProviderAnthropic, true},
		{"same provider openai", ProviderOpenAI, ProviderOpenAI, true},
		{"unknown model permissive", ProviderUnknown, ProviderAnthropic, true},
		{"unknown harness permissive", ProviderAnthropic, ProviderUnknown, true},
		{"both unknown permissive", ProviderUnknown, ProviderUnknown, true},
		{"known mismatch rejected", ProviderAnthropic, ProviderOpenAI, false},
		{"known mismatch rejected reversed", ProviderOpenAI, ProviderAnthropic, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.model.CompatibleWith(tt.harness); got != tt.want {
				t.Fatalf("%q.CompatibleWith(%q) = %v, want %v", tt.model, tt.harness, got, tt.want)
			}
		})
	}
}
