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

// Prime is projectless: it has no ProjectConfig.AgentConfig to inherit a
// permission mode from the way orchestrator and worker sessions do, so these
// settings are the only source the launch reads. An empty mode normalizes to
// PermissionModeDefault, which emits no --permission-mode flag, and an enabled
// Prime then blocks on its first tool prompt with nobody at its pane.
func TestPrimeSettingsDefaultToUnattendedPermissions(t *testing.T) {
	for name, got := range map[string]PrimeSettings{
		"fresh daemon":      DefaultPrimeSettings(),
		"settings unset":    (PrimeSettings{}).WithDefaults(),
		"harness set alone": (PrimeSettings{Enabled: true, Harness: HarnessClaudeCode}).WithDefaults(),
	} {
		t.Run(name, func(t *testing.T) {
			switch got.AgentConfig.Permissions {
			case "", PermissionModeDefault:
				t.Fatalf("Prime resolves permissions %q: no --permission-mode flag is emitted, so an enabled Prime stalls on the first permission prompt",
					got.AgentConfig.Permissions)
			}
			if got.AgentConfig.Permissions != PermissionModeBypassPermissions {
				t.Fatalf("Prime resolves permissions %q, want %q", got.AgentConfig.Permissions, PermissionModeBypassPermissions)
			}
		})
	}
}

// The escape hatch: an operator who wants Prime to prompt sets the mode
// explicitly. WithDefaults fills the field only when it is empty, so an explicit
// "default" survives and is not overwritten by the unattended default above.
//
// This is what makes an empty stored value mean "never configured" and nothing
// else, and therefore what makes the unattended default safe to apply on read
// instead of migrating stored rows. WithDefaults runs on EVERY settings read, so
// the property is asserted under repetition too: a mode that survived once but
// decayed on a later read would escalate the operator's choice away silently,
// exactly what the on-read default is claimed not to do.
func TestPrimeSettingsWithDefaultsPreservesExplicitPermissions(t *testing.T) {
	for _, want := range []PermissionMode{PermissionModeDefault, PermissionModeAcceptEdits, PermissionModeAuto} {
		got := (PrimeSettings{AgentConfig: AgentConfig{Permissions: want}}).WithDefaults()
		if got.AgentConfig.Permissions != want {
			t.Fatalf("explicit permissions %q overwritten with %q", want, got.AgentConfig.Permissions)
		}
		for read := 2; read <= 5; read++ {
			got = got.WithDefaults()
			if got.AgentConfig.Permissions != want {
				t.Fatalf("explicit permissions %q became %q by settings read %d; the operator's mode must survive every read",
					want, got.AgentConfig.Permissions, read)
			}
		}
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
		"unknown harness":             {Harness: "nope"},
		"enabled without harness":     {Enabled: true},
		"invalid effort":              {AgentConfig: AgentConfig{Effort: "turbo"}},
		"bad wake interval":           {WakeInterval: "-1m"},
		"wake interval below minimum": {WakeInterval: "30s"},
		"wake interval above maximum": {WakeInterval: "361m"},
		"escaping rules":              {RulesFile: "../prime.md"},
		"blank display":               {DisplayName: strings.Repeat(" ", 3)},
	} {
		t.Run(name, func(t *testing.T) {
			if err := cfg.Validate(); err == nil {
				t.Fatalf("Validate() = nil, want error for %+v", cfg)
			}
		})
	}
}

// Prime's name is delivered into its harness like any other, so a NEW name must
// not be one delivery would refuse — Prime would spawn with no name at all.
func TestPrimeSettingsRejectAnUnsafeDisplayNameOnWrite(t *testing.T) {
	s := DefaultPrimeSettings()
	s.DisplayName = "x; touch /tmp/pwn"
	if err := s.ValidateDisplayNameForWrite(); err == nil {
		t.Fatal("write validation accepted a display name carrying shell syntax")
	}
	s.DisplayName = "AO Prime"
	if err := s.ValidateDisplayNameForWrite(); err != nil {
		t.Fatalf("write validation rejected a normal name: %v", err)
	}
}

// Validate also runs when stored settings are READ, so it must stay tolerant of
// a name that was legal when it was saved. Rejecting one there would fail every
// subsequent read and take the Prime supervisor down with it — the settings
// would be unreachable, so the operator could not even correct the name.
func TestPrimeSettingsValidateStaysReadableForLegacyNames(t *testing.T) {
	s := DefaultPrimeSettings()
	s.DisplayName = "Nick's Prime"
	if err := s.Validate(); err != nil {
		t.Fatalf("Validate rejected a name that predates the character rule: %v", err)
	}
	if err := s.ValidateDisplayNameForWrite(); err == nil {
		t.Fatal("write validation accepted the same name; the split has collapsed")
	}
}
