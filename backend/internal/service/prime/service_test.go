package prime

import (
	"context"
	"errors"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
)

func TestServiceGetSettingsIncludesDefaultsOnly(t *testing.T) {
	store := &fakeStore{settings: domain.DefaultPrimeSettings()}
	svc := New(Deps{Store: store})

	got, err := svc.GetSettings(context.Background())
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if got.Settings.Enabled {
		t.Fatal("default prime settings should stay disabled")
	}
	if got.Settings.DisplayName != "AO Prime" {
		t.Fatalf("displayName = %q, want AO Prime", got.Settings.DisplayName)
	}
}

func TestServiceSetSettingsValidatesAndPersists(t *testing.T) {
	store := &fakeStore{settings: domain.DefaultPrimeSettings()}
	svc := New(Deps{Store: store})

	got, err := svc.SetSettings(context.Background(), domain.PrimeSettings{
		Enabled:      true,
		DisplayName:  "Fleet Lead",
		Harness:      domain.HarnessCodex,
		AgentConfig:  domain.AgentConfig{Model: "gpt-5-codex", Effort: domain.EffortHigh},
		Rules:        "Keep watch.",
		WakeInterval: "20m",
	})
	if err != nil {
		t.Fatalf("SetSettings: %v", err)
	}
	if !store.saved.Enabled || store.saved.DisplayName != "Fleet Lead" || store.saved.Harness != domain.HarnessCodex {
		t.Fatalf("saved settings = %+v", store.saved)
	}
	if got.Settings.AgentConfig.Model != "gpt-5-codex" || got.Settings.AgentConfig.Effort != domain.EffortHigh {
		t.Fatalf("returned model/effort = %+v", got.Settings.AgentConfig)
	}
}

func TestServiceSetSettingsValidationErrorIsAPIInvalid(t *testing.T) {
	svc := New(Deps{Store: &fakeStore{settings: domain.DefaultPrimeSettings()}})

	_, err := svc.SetSettings(context.Background(), domain.PrimeSettings{Enabled: true, DisplayName: " bad "})
	var api *apierr.Error
	if !errors.As(err, &api) || api.Kind != apierr.KindInvalid || api.Code != "INVALID_PRIME_SETTINGS" {
		t.Fatalf("err = %v, want INVALID_PRIME_SETTINGS api error", err)
	}
}

func TestServicePromptUsesProjectlessPrimePrompt(t *testing.T) {
	prompts := &fakePrompts{prompt: "FLEET PRIME PROMPT"}
	svc := New(Deps{Store: &fakeStore{settings: domain.DefaultPrimeSettings()}, Prompts: prompts})

	got, err := svc.Prompt(context.Background())
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if got != "FLEET PRIME PROMPT" {
		t.Fatalf("prompt = %q", got)
	}
	if prompts.kind != domain.KindPrime || prompts.projectID != "" {
		t.Fatalf("prompt target = kind %q project %q, want projectless prime", prompts.kind, prompts.projectID)
	}
}

type fakeStore struct {
	settings domain.PrimeSettings
	saved    domain.PrimeSettings
}

func (f *fakeStore) GetPrimeSettings(context.Context) (domain.PrimeSettings, error) {
	return f.settings, nil
}

func (f *fakeStore) SetPrimeSettings(_ context.Context, settings domain.PrimeSettings) error {
	f.saved = settings
	f.settings = settings
	return nil
}

type fakePrompts struct {
	prompt    string
	kind      domain.SessionKind
	projectID domain.ProjectID
}

func (f *fakePrompts) RoleSystemPrompt(_ context.Context, kind domain.SessionKind, projectID domain.ProjectID) (string, error) {
	f.kind = kind
	f.projectID = projectID
	return f.prompt, nil
}
