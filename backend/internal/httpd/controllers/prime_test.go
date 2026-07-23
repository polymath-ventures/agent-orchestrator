package controllers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/controllers"
	primesvc "github.com/aoagents/agent-orchestrator/backend/internal/service/prime"
)

func TestPrimeControllerGetSettings(t *testing.T) {
	svc := &fakePrimeService{view: primesvc.SettingsView{
		Settings: domain.PrimeSettings{
			Enabled:     true,
			DisplayName: "Fleet Lead",
			Harness:     domain.HarnessCodex,
		},
	}}
	rr := httptest.NewRecorder()

	primeRouter(svc).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/prime/settings", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("GET /prime/settings = %d body=%s", rr.Code, rr.Body.String())
	}
	var got primesvc.SettingsView
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !got.Settings.Enabled || got.Settings.DisplayName != "Fleet Lead" {
		t.Fatalf("response = %+v", got)
	}
}

func TestPrimeControllerPutSettingsRejectsUnknownFields(t *testing.T) {
	rr := httptest.NewRecorder()

	primeRouter(&fakePrimeService{}).ServeHTTP(rr, httptest.NewRequest(http.MethodPut, "/prime/settings", stringsReader(`{"settings":{"enabled":true},"surprise":true}`)))

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("PUT /prime/settings = %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
}

func TestPrimeControllerPutSettingsSaves(t *testing.T) {
	svc := &fakePrimeService{view: primesvc.SettingsView{Settings: domain.PrimeSettings{Enabled: true, DisplayName: "Fleet Lead"}}}
	rr := httptest.NewRecorder()

	primeRouter(svc).ServeHTTP(rr, httptest.NewRequest(http.MethodPut, "/prime/settings", stringsReader(`{"settings":{"enabled":true,"displayName":"Fleet Lead"}}`)))

	if rr.Code != http.StatusOK {
		t.Fatalf("PUT /prime/settings = %d body=%s", rr.Code, rr.Body.String())
	}
	if !svc.saved.Enabled || svc.saved.DisplayName != "Fleet Lead" {
		t.Fatalf("saved = %+v", svc.saved)
	}
}

func TestPrimeControllerPrompt(t *testing.T) {
	rr := httptest.NewRecorder()

	primeRouter(&fakePrimeService{prompt: "FLEET PROMPT"}).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/prime/prompt", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("GET /prime/prompt = %d body=%s", rr.Code, rr.Body.String())
	}
	var got controllers.RolePromptResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Role != "prime" || got.Prompt != "FLEET PROMPT" {
		t.Fatalf("response = %+v", got)
	}
}

func TestPrimeControllerNilServiceNotImplemented(t *testing.T) {
	rr := httptest.NewRecorder()

	primeRouter(nil).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/prime/settings", nil))

	if rr.Code != http.StatusNotImplemented {
		t.Fatalf("nil service status = %d, want 501; body=%s", rr.Code, rr.Body.String())
	}
}

func primeRouter(svc controllers.PrimeService) http.Handler {
	r := chi.NewRouter()
	(&controllers.PrimeController{Svc: svc}).Register(r)
	return r
}

type fakePrimeService struct {
	view   primesvc.SettingsView
	saved  domain.PrimeSettings
	prompt string
	err    error
}

func (f *fakePrimeService) GetSettings(context.Context) (primesvc.SettingsView, error) {
	return f.view, f.err
}

func (f *fakePrimeService) SetSettings(_ context.Context, settings domain.PrimeSettings) (primesvc.SettingsView, error) {
	f.saved = settings
	return f.view, f.err
}

func (f *fakePrimeService) Prompt(context.Context) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.prompt, nil
}

func stringsReader(s string) *strings.Reader {
	return strings.NewReader(s)
}
