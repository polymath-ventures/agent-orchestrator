package domain

import (
	"fmt"
	"strings"
	"time"
)

const (
	defaultPrimeDisplayName = "AO Prime"
	minPrimeWakeInterval    = time.Minute
	maxPrimeWakeInterval    = 360 * time.Minute
)

// PrimeSettings is the daemon-owned configuration for the fleet Prime
// singleton. ProjectConfig no longer owns Prime launch settings.
type PrimeSettings struct {
	Enabled      bool               `json:"enabled"`
	DisplayName  string             `json:"displayName,omitempty"`
	Harness      AgentHarness       `json:"agent,omitempty"`
	AgentConfig  AgentConfig        `json:"agentConfig,omitempty"`
	Rules        string             `json:"rules,omitempty"`
	RulesFile    string             `json:"rulesFile,omitempty"`
	WakeInterval string             `json:"wakeInterval,omitempty"`
	WakeBackoff  *WakeBackoffConfig `json:"wakeBackoff,omitempty"`
}

// DefaultPrimeSettings returns the persisted default for fresh daemons: Prime
// disabled with the daemon's standard display name and wake policy defaults.
func DefaultPrimeSettings() PrimeSettings {
	return PrimeSettings{
		DisplayName:  defaultPrimeDisplayName,
		WakeInterval: defaultPrimeWakeIntervalConfig,
	}
}

// WithDefaults overlays daemon defaults onto unset Prime settings.
func (s PrimeSettings) WithDefaults() PrimeSettings {
	def := DefaultPrimeSettings()
	if s.DisplayName == "" {
		s.DisplayName = def.DisplayName
	}
	if s.WakeInterval == "" {
		s.WakeInterval = def.WakeInterval
	}
	return s
}

// Validate rejects invalid Prime settings at the write boundary.
func (s PrimeSettings) Validate() error {
	if strings.TrimSpace(s.DisplayName) == "" {
		return fmt.Errorf("displayName: must not be blank")
	}
	if s.DisplayName != strings.TrimSpace(s.DisplayName) {
		return fmt.Errorf("displayName: must not have leading or trailing whitespace")
	}
	if len([]rune(s.DisplayName)) > MaxSessionDisplayNameRunes {
		return fmt.Errorf("displayName: must be at most %d characters", MaxSessionDisplayNameRunes)
	}
	// Prime's name is delivered into its harness like any other, so it is held to
	// the same character rule. Without this, settings could persist a name that
	// delivery refuses and Prime would spawn with no name at all.
	for _, r := range s.DisplayName {
		if !NameRuneAllowed(r) {
			return fmt.Errorf("displayName: %w", ErrDisplayNameUnsafe)
		}
	}
	if s.Harness != "" && !s.Harness.IsKnown() {
		return fmt.Errorf("agent: unknown harness %q", s.Harness)
	}
	if s.Enabled && s.Harness == "" {
		return fmt.Errorf("agent: required when Prime is enabled")
	}
	if err := s.AgentConfig.Validate(); err != nil {
		return err
	}
	if s.WakeInterval != "" {
		d, err := s.WakeIntervalDuration()
		if err != nil {
			return fmt.Errorf("wakeInterval: %w", err)
		}
		if d < minPrimeWakeInterval || d > maxPrimeWakeInterval {
			return fmt.Errorf("wakeInterval: must be between 1m and 360m")
		}
	}
	if _, err := s.WakeBackoffPolicy(); err != nil {
		return fmt.Errorf("wakeBackoff: %w", err)
	}
	if err := validateRoleRulesFilePath(s.RulesFile); err != nil {
		return fmt.Errorf("rulesFile %q: %w", s.RulesFile, err)
	}
	return nil
}

// RoleOverride converts fleet Prime settings into the existing role override
// shape for shared launch and wake-policy resolution helpers.
func (s PrimeSettings) RoleOverride() RoleOverride {
	return RoleOverride{
		Harness:      s.Harness,
		AgentConfig:  s.AgentConfig,
		WakeInterval: s.WakeInterval,
		WakeBackoff:  s.WakeBackoff,
	}
}

// WakeIntervalDuration parses the configured Prime wake interval.
func (s PrimeSettings) WakeIntervalDuration() (time.Duration, error) {
	return s.WithDefaults().RoleOverride().WakeIntervalDuration()
}

// WakeBackoffPolicy parses the configured Prime wake backoff policy.
func (s PrimeSettings) WakeBackoffPolicy() (WakeBackoffPolicy, error) {
	return s.WithDefaults().RoleOverride().WakeBackoffPolicy()
}
