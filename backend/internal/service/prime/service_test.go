package prime

import (
	"context"
	"errors"
	"testing"
	"time"

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

// An off/save/on/save cycle must restart Prime reliably. Before this, the
// supervisor only noticed on its next 30s tick, so the operator had to time the
// disabled window by hand — the documented workaround.
func TestSetSettingsSignalsImmediateReconcile(t *testing.T) {
	reconciler := &fakeSettingsReconciler{}
	svc := New(Deps{
		Store:              &fakeStore{settings: domain.DefaultPrimeSettings()},
		SettingsReconciler: reconciler,
	})

	settings := domain.PrimeSettings{Enabled: true, Harness: domain.HarnessClaudeCode}.WithDefaults()
	if _, err := svc.SetSettings(context.Background(), settings); err != nil {
		t.Fatalf("SetSettings: %v", err)
	}
	if reconciler.calls != 1 {
		t.Fatalf("reconcile calls = %d, want 1", reconciler.calls)
	}
	if !reconciler.settings.Enabled || reconciler.settings.Harness != domain.HarnessClaudeCode {
		t.Fatalf("reconciled settings = %+v, want the saved Prime settings", reconciler.settings)
	}
}

func TestSetSettingsSurfacesSaveAndReconcileFailure(t *testing.T) {
	reconciler := &fakeSettingsReconciler{err: errors.New("spawn failed")}
	svc := New(Deps{Store: &fakeStore{settings: domain.DefaultPrimeSettings()}, SettingsReconciler: reconciler})

	settings := domain.PrimeSettings{Enabled: true, Harness: domain.HarnessClaudeCode}.WithDefaults()
	if _, err := svc.SetSettings(context.Background(), settings); err == nil {
		t.Fatal("SetSettings() = nil error, want reconcile failure")
	}
	if !reconciler.settings.Enabled {
		t.Fatal("settings were not handed to the save-and-reconcile path before surfacing the reconcile failure")
	}
}

func TestSetSettingsTimeoutIsExplicit(t *testing.T) {
	reconciler := &fakeSettingsReconciler{waitForContext: true}
	svc := New(Deps{
		Store:                    &fakeStore{settings: domain.DefaultPrimeSettings()},
		SettingsReconciler:       reconciler,
		SettingsReconcileTimeout: time.Nanosecond,
	})

	settings := domain.PrimeSettings{Enabled: true, Harness: domain.HarnessClaudeCode}.WithDefaults()
	_, err := svc.SetSettings(context.Background(), settings)
	var api *apierr.Error
	if !errors.As(err, &api) || api.Code != "PRIME_RECONCILE_TIMEOUT" {
		t.Fatalf("err = %v, want PRIME_RECONCILE_TIMEOUT", err)
	}
	if !reconciler.settings.Enabled {
		t.Fatal("settings were not handed to the save-and-reconcile path before the timeout surfaced")
	}
}

// A rejected write must not claim a reconcile happened.
func TestSetSettingsDoesNotSignalOnValidationFailure(t *testing.T) {
	reconciler := &fakeSettingsReconciler{}
	svc := New(Deps{
		Store:              &fakeStore{settings: domain.DefaultPrimeSettings()},
		SettingsReconciler: reconciler,
	})

	// Enabled with no agent is invalid.
	if _, err := svc.SetSettings(context.Background(), domain.PrimeSettings{Enabled: true}); err == nil {
		t.Fatal("SetSettings() = nil error for invalid settings")
	}
	if reconciler.calls != 0 {
		t.Fatalf("reconcile calls = %d, want 0 on a rejected write", reconciler.calls)
	}
}

type fakeSettingsReconciler struct {
	calls          int
	err            error
	waitForContext bool
	settings       domain.PrimeSettings
}

func (f *fakeSettingsReconciler) SetAndReconcilePrimeSettings(ctx context.Context, settings domain.PrimeSettings) error {
	f.calls++
	f.settings = settings
	if f.waitForContext {
		<-ctx.Done()
		return ctx.Err()
	}
	return f.err
}
