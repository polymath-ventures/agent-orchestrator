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
// disabled with the daemon's standard display name, wake policy defaults, and
// the unattended permission mode every AO-managed role needs.
//
// Prime is projectless, so unlike orchestrator and worker sessions it has no
// ProjectConfig.AgentConfig to inherit a permission mode from: whatever these
// settings carry is what the harness is launched with. An empty permission mode
// normalizes to PermissionModeDefault, which emits no --permission-mode flag at
// all, so an enabled Prime blocks on the first tool prompt with nobody at its
// pane to answer — and the supervisor's only recourse is to declare it unhealthy
// and spawn a replacement that stalls identically. Prime is off until an
// operator explicitly enables it, and its whole job is to supervise the fleet
// unattended, so the useful default is the unattended one. An operator who wants
// prompting back sets permissions explicitly to "default"; WithDefaults only
// fills the field when it is empty.
func DefaultPrimeSettings() PrimeSettings {
	return PrimeSettings{
		DisplayName:  defaultPrimeDisplayName,
		WakeInterval: defaultPrimeWakeIntervalConfig,
		AgentConfig:  AgentConfig{Permissions: PermissionModeBypassPermissions},
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
	if s.AgentConfig.Permissions == "" {
		s.AgentConfig.Permissions = def.AgentConfig.Permissions
	}
	return s
}

// ValidateDisplayNameForWrite holds a NEW Prime name to the same character rule
// every other session name obeys, so settings cannot persist a name that
// delivery would refuse.
//
// It is separate from Validate because Validate also runs when stored settings
// are READ: folding the rule in there would make a name that was legal when it
// was saved — an apostrophe is the obvious case — fail every subsequent read and
// take the Prime supervisor down with it. New writes are held to the rule; an
// older stored name degrades at spawn instead.
func (s PrimeSettings) ValidateDisplayNameForWrite() error {
	for _, r := range s.DisplayName {
		if !NameRuneAllowed(r) {
			return fmt.Errorf("displayName: %w", ErrDisplayNameUnsafe)
		}
	}
	return nil
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
