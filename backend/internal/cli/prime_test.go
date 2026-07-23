package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type primeCapture struct {
	method string
	path   string
	body   []byte
}

func primeServer(t *testing.T, status int, respBody string) (*httptest.Server, *primeCapture) {
	t.Helper()
	capture := &primeCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capture.method = r.Method
		capture.path = r.URL.Path
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		capture.body = data
		if !strings.HasPrefix(r.URL.Path, "/api/v1/prime") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, respBody)
	}))
	t.Cleanup(srv.Close)
	return srv, capture
}

func TestPrimeSettings_PrintsGlobalSettings(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := primeServer(t, http.StatusOK, `{"settings":{"enabled":false,"displayName":"AO Prime","wakeInterval":"15m"},"legacyEnvironment":{"configured":true,"projectId":"ao"}}`)
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "prime", "settings")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	if capture.method != http.MethodGet || capture.path != "/api/v1/prime/settings" {
		t.Fatalf("request = %s %s, want GET /api/v1/prime/settings", capture.method, capture.path)
	}
	if !strings.Contains(out, "enabled=false") || !strings.Contains(out, "legacyProject=ao") {
		t.Fatalf("output = %q, want settings and legacy env", out)
	}
}

func TestPrimeEnable_PutsEnabledSettings(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := primeServer(t, http.StatusOK, `{"settings":{"enabled":true,"displayName":"Fleet Lead","agent":"codex","agentConfig":{"model":"gpt-5-codex","effort":"high"},"wakeInterval":"20m"},"legacyEnvironment":{"configured":false}}`)
	writeRunFileFor(t, cfg, srv)

	_, errOut, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }},
		"prime", "enable", "--name", "Fleet Lead", "--agent", "codex", "--model", "gpt-5-codex", "--effort", "high", "--wake-interval", "20m", "--rules", "Keep watch.")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	if capture.method != http.MethodPut || capture.path != "/api/v1/prime/settings" {
		t.Fatalf("request = %s %s, want PUT /api/v1/prime/settings", capture.method, capture.path)
	}
	var body struct {
		Settings struct {
			Enabled      bool   `json:"enabled"`
			DisplayName  string `json:"displayName"`
			Agent        string `json:"agent"`
			Rules        string `json:"rules"`
			WakeInterval string `json:"wakeInterval"`
			AgentConfig  struct {
				Model  string `json:"model"`
				Effort string `json:"effort"`
			} `json:"agentConfig"`
		} `json:"settings"`
	}
	if err := json.Unmarshal(capture.body, &body); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if !body.Settings.Enabled || body.Settings.DisplayName != "Fleet Lead" || body.Settings.Agent != "codex" || body.Settings.AgentConfig.Model != "gpt-5-codex" || body.Settings.AgentConfig.Effort != "high" || body.Settings.Rules != "Keep watch." {
		t.Fatalf("request body = %s", string(capture.body))
	}
}

func TestPrimeDisable_PutsDisabledSettings(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := primeServer(t, http.StatusOK, `{"settings":{"enabled":false,"displayName":"AO Prime","wakeInterval":"15m"},"legacyEnvironment":{"configured":false}}`)
	writeRunFileFor(t, cfg, srv)

	_, errOut, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "prime", "disable")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	if capture.method != http.MethodPut || capture.path != "/api/v1/prime/settings" {
		t.Fatalf("request = %s %s, want PUT /api/v1/prime/settings", capture.method, capture.path)
	}
	if !strings.Contains(string(capture.body), `"enabled":false`) {
		t.Fatalf("request body = %s, want enabled false", string(capture.body))
	}
}

func TestPrimePrompt_PrintsFleetPrompt(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := primeServer(t, http.StatusOK, `{"role":"prime","prompt":"FLEET PRIME PROMPT"}`)
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "prime", "prompt")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	if capture.method != http.MethodGet || capture.path != "/api/v1/prime/prompt" {
		t.Fatalf("request = %s %s, want GET /api/v1/prime/prompt", capture.method, capture.path)
	}
	if !strings.Contains(out, "FLEET PRIME PROMPT") {
		t.Fatalf("output missing prompt:\n%s", out)
	}
}
