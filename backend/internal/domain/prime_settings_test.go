package domain

import (
	"strings"
	"testing"
	"time"
)

func TestDefaultPrimeSettingsDisabled(t *testing.T) {
	got := DefaultPrimeSettings()
	if got.Enabled {
		t.Fatal("DefaultPrimeSettings().Enabled = true, want false")
	}
	if got.DisplayName != "AO Prime" {
		t.Fatalf("default display name = %q, want AO Prime", got.DisplayName)
	}
	policy, err := got.WakeBackoffPolicy()
	if err != nil {
		t.Fatalf("default wake policy: %v", err)
	}
	if policy.Base != DefaultPrimeWakeInterval || policy.Max != DefaultWakeBackoffMaxInterval || !policy.Enabled {
		t.Fatalf("default wake policy = %+v, want base %s max %s enabled", policy, DefaultPrimeWakeInterval, DefaultWakeBackoffMaxInterval)
	}
}

func TestPrimeSettingsWithDefaultsPreservesConfiguredValues(t *testing.T) {
	disabled := false
	got := (PrimeSettings{
		Enabled:      true,
		DisplayName:  "Fleet Lead",
		Harness:      HarnessCodex,
		AgentConfig:  AgentConfig{Model: "gpt-5-codex", Effort: EffortHigh},
		Rules:        "watch the fleet",
		RulesFile:    "/etc/ao/prime.md",
		WakeInterval: "30m",
		WakeBackoff:  &WakeBackoffConfig{Enabled: &disabled},
	}).WithDefaults()
	if !got.Enabled || got.DisplayName != "Fleet Lead" || got.Harness != HarnessCodex {
		t.Fatalf("configured values not preserved: %+v", got)
	}
	if got.AgentConfig.Model != "gpt-5-codex" || got.AgentConfig.Effort != EffortHigh {
		t.Fatalf("agent config not preserved: %+v", got.AgentConfig)
	}
	policy, err := got.WakeBackoffPolicy()
	if err != nil {
		t.Fatalf("wake policy: %v", err)
	}
	if policy.Enabled || policy.Base != 30*time.Minute {
		t.Fatalf("wake policy = %+v, want disabled base 30m", policy)
	}
}

func TestPrimeSettingsValidateRejectsBadValues(t *testing.T) {
	for name, cfg := range map[string]PrimeSettings{
		"unknown harness":   {Harness: "nope"},
		"invalid effort":    {AgentConfig: AgentConfig{Effort: "turbo"}},
		"bad wake interval": {WakeInterval: "-1m"},
		"escaping rules":    {RulesFile: "../prime.md"},
		"blank display":     {DisplayName: strings.Repeat(" ", 3)},
	} {
		t.Run(name, func(t *testing.T) {
			if err := cfg.Validate(); err == nil {
				t.Fatalf("Validate() = nil, want error for %+v", cfg)
			}
		})
	}
}
