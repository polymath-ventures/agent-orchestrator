// Package prime owns daemon-global Prime settings use-cases.
package prime

import (
	"context"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
)

// Store is the durable fleet Prime settings persistence surface.
type Store interface {
	GetPrimeSettings(ctx context.Context) (domain.PrimeSettings, error)
	SetPrimeSettings(ctx context.Context, settings domain.PrimeSettings) error
}

// PromptAssembler assembles the exact system prompt for inspectable roles.
type PromptAssembler interface {
	RoleSystemPrompt(ctx context.Context, kind domain.SessionKind, projectID domain.ProjectID) (string, error)
}

// SettingsView is the wire-ready settings read model.
type SettingsView struct {
	Settings domain.PrimeSettings `json:"settings"`
}

// Service implements fleet Prime settings and prompt inspection.
type Service struct {
	store     Store
	prompts   PromptAssembler
	onChanged func()
}

// Deps captures Service collaborators.
type Deps struct {
	Store   Store
	Prompts PromptAssembler
	// OnSettingsChanged is invoked after settings are persisted, so the Prime
	// supervisor reconciles immediately instead of on its next tick. Optional.
	OnSettingsChanged func()
}

// New builds a Service.
func New(d Deps) *Service {
	return &Service{store: d.Store, prompts: d.Prompts, onChanged: d.OnSettingsChanged}
}

// GetSettings returns the persisted fleet Prime settings with defaults.
func (s *Service) GetSettings(ctx context.Context) (SettingsView, error) {
	if s.store == nil {
		return SettingsView{}, apierr.Internal("PRIME_SETTINGS_UNAVAILABLE", "Prime settings are unavailable")
	}
	settings, err := s.store.GetPrimeSettings(ctx)
	if err != nil {
		return SettingsView{}, err
	}
	return s.view(settings), nil
}

// SetSettings validates and persists the complete fleet Prime settings object.
func (s *Service) SetSettings(ctx context.Context, settings domain.PrimeSettings) (SettingsView, error) {
	if s.store == nil {
		return SettingsView{}, apierr.Internal("PRIME_SETTINGS_UNAVAILABLE", "Prime settings are unavailable")
	}
	settings = settings.WithDefaults()
	if err := settings.Validate(); err != nil {
		return SettingsView{}, apierr.Invalid("INVALID_PRIME_SETTINGS", err.Error(), nil)
	}
	if err := settings.ValidateDisplayNameForWrite(); err != nil {
		return SettingsView{}, apierr.Invalid("DISPLAY_NAME_UNSAFE", err.Error(), nil)
	}
	if err := s.store.SetPrimeSettings(ctx, settings); err != nil {
		return SettingsView{}, err
	}
	// Reconcile now rather than on the next supervisor tick. Without this an
	// off/save/on/save cycle only restarts Prime if a tick happens to land in
	// the disabled window — the "hold Prime disabled long enough for a tick"
	// workaround the operator had to time by hand.
	if s.onChanged != nil {
		s.onChanged()
	}
	return s.view(settings), nil
}

// Prompt returns the projectless fleet Prime system prompt.
func (s *Service) Prompt(ctx context.Context) (string, error) {
	if s.prompts == nil {
		return "", apierr.Internal("PRIME_PROMPT_UNAVAILABLE", "Prime prompt inspection is unavailable")
	}
	return s.prompts.RoleSystemPrompt(ctx, domain.KindPrime, "")
}

func (s *Service) view(settings domain.PrimeSettings) SettingsView {
	return SettingsView{Settings: settings.WithDefaults()}
}
