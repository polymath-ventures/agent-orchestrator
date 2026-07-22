package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// configRoundTripCapture records the GET (config read) and PUT (config write)
// halves of an apply/diff round-trip so tests can assert both.
type configRoundTripCapture struct {
	getCalled  bool
	putCalled  bool
	putBody    []byte
	putIfMatch string
}

// startConfigRoundTripServer serves GET /projects/{id} with liveConfig as the
// config field and captures PUT /projects/{id}/config, returning putStatus.
func startConfigRoundTripServer(t *testing.T, liveConfig string, putStatus int) (*httptest.Server, *configRoundTripCapture) {
	t.Helper()
	capture := &configRoundTripCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/projects/demo":
			capture.getCalled = true
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok","project":{"id":"demo","path":"/repo/demo","configETag":"etag-live","config":` + liveConfig + `}}`))
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/projects/demo/config":
			capture.putCalled = true
			capture.putIfMatch = r.Header.Get("If-Match")
			body, _ := io.ReadAll(r.Body)
			capture.putBody = body
			w.WriteHeader(putStatus)
			if putStatus == http.StatusConflict {
				_, _ = w.Write([]byte(`{"message":"The project config changed since it was read; reload and reapply the edit.","code":"PROJECT_CONFIG_STALE"}`))
			} else if putStatus >= 400 {
				_, _ = w.Write([]byte(`{"message":"json: unknown field \"bogus\"","code":"bad_request"}`))
			} else {
				_, _ = w.Write([]byte(`{"project":{"id":"demo","path":"/repo/demo"}}`))
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, capture
}

func writeSpecFile(t *testing.T, contents string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write spec file: %v", err)
	}
	return path
}

const applyLiveConfig = `{"defaultBranch":"main","sessionPrefix":"demo","maxLiveWorkers":3}`

func TestProjectConfigApply_TwoFieldsChangesExactlyThose(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := startConfigRoundTripServer(t, applyLiveConfig, http.StatusOK)
	writeRunFileFor(t, cfg, srv)

	spec := writeSpecFile(t, `{"sessionPrefix":"prod","maxLiveWorkers":5}`)

	out, errOut, err := executeCLI(t, Deps{
		ProcessAlive: func(int) bool { return true },
	}, "project", "config", "apply", "demo", spec)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	if !capture.getCalled || !capture.putCalled {
		t.Fatalf("expected GET then PUT; getCalled=%v putCalled=%v", capture.getCalled, capture.putCalled)
	}
	if capture.putIfMatch != "etag-live" {
		t.Fatalf("PUT If-Match = %q, want etag-live", capture.putIfMatch)
	}
	// PUT body must be live-config-plus-the-two-named-fields.
	var req map[string]any
	if err := json.Unmarshal(capture.putBody, &req); err != nil {
		t.Fatalf("decode PUT body: %v\nbody=%s", err, capture.putBody)
	}
	config, _ := req["config"].(map[string]any)
	want := map[string]any{
		"defaultBranch":  "main",     // unchanged
		"sessionPrefix":  "prod",     // changed
		"maxLiveWorkers": float64(5), // changed
	}
	if !reflect.DeepEqual(config, want) {
		t.Fatalf("PUT config = %v, want %v", config, want)
	}
	// Reports exactly the two changed fields.
	if !strings.Contains(out, "maxLiveWorkers") || !strings.Contains(out, "sessionPrefix") {
		t.Fatalf("output %q should name changed fields maxLiveWorkers and sessionPrefix", out)
	}
	if strings.Contains(out, "defaultBranch") {
		t.Fatalf("output %q should not name unchanged defaultBranch", out)
	}
}

func TestProjectConfigApply_StaleWriteSurfacesConflict(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := startConfigRoundTripServer(t, applyLiveConfig, http.StatusConflict)
	writeRunFileFor(t, cfg, srv)

	spec := writeSpecFile(t, `{"sessionPrefix":"prod"}`)
	_, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }},
		"project", "config", "apply", "demo", spec)
	if err == nil {
		t.Fatal("expected stale apply to fail")
	}
	if got := ExitCode(err); got != 1 {
		t.Fatalf("exit code = %d, want 1", got)
	}
	if !capture.putCalled || capture.putIfMatch != "etag-live" {
		t.Fatalf("stale PUT called=%v If-Match=%q", capture.putCalled, capture.putIfMatch)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "reload") ||
		!strings.Contains(err.Error(), "PROJECT_CONFIG_STALE") {
		t.Fatalf("error = %v, want stale reload instruction and code", err)
	}
}

func TestProjectConfigApply_OnlyRestoresSelectedNestedPath(t *testing.T) {
	cfg := setConfigEnv(t)
	live := `{"worker":{"agent":"codex","agentConfig":{"model":"old","effort":"high"}},"defaultBranch":"main"}`
	srv, capture := startConfigRoundTripServer(t, live, http.StatusOK)
	writeRunFileFor(t, cfg, srv)

	spec := writeSpecFile(t, `{"worker":{"agent":"claude-code","agentConfig":{"model":"new","effort":"low"}}}`)
	out, errOut, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }},
		"project", "config", "apply", "demo", spec,
		"--only", "worker.agentConfig.model")
	if err != nil {
		t.Fatalf("apply --only: %v\nstderr=%s", err, errOut)
	}
	var req map[string]any
	if err := json.Unmarshal(capture.putBody, &req); err != nil {
		t.Fatalf("decode PUT: %v", err)
	}
	worker := req["config"].(map[string]any)["worker"].(map[string]any)
	agentConfig := worker["agentConfig"].(map[string]any)
	if worker["agent"] != "codex" || agentConfig["model"] != "new" || agentConfig["effort"] != "high" {
		t.Fatalf("nested surgical config = %#v", req["config"])
	}
	if !strings.Contains(out, "worker.agentConfig.model") {
		t.Fatalf("output %q missing selected path", out)
	}
}

func TestProjectConfigApply_OnlyRejectsUnsafeOrMissingPath(t *testing.T) {
	for _, path := range []string{"worker.missing.model", "worker..model"} {
		t.Run(path, func(t *testing.T) {
			cfg := setConfigEnv(t)
			srv, capture := startConfigRoundTripServer(t, `{"worker":{"agent":"codex"}}`, http.StatusOK)
			writeRunFileFor(t, cfg, srv)
			spec := writeSpecFile(t, `{"worker":{"agent":"claude-code"}}`)

			_, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }},
				"project", "config", "apply", "demo", spec, "--only", path)
			if err == nil || ExitCode(err) != 2 {
				t.Fatalf("error = %v code=%d, want usage error", err, ExitCode(err))
			}
			if capture.putCalled {
				t.Fatal("invalid --only path must not PUT")
			}
		})
	}
}

func TestProjectConfigApply_NoOpSkipsPut(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := startConfigRoundTripServer(t, applyLiveConfig, http.StatusOK)
	writeRunFileFor(t, cfg, srv)

	// Spec equals live config exactly → no change.
	spec := writeSpecFile(t, applyLiveConfig)

	out, errOut, err := executeCLI(t, Deps{
		ProcessAlive: func(int) bool { return true },
	}, "project", "config", "apply", "demo", spec)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	if capture.putCalled {
		t.Fatal("no-op apply must not PUT")
	}
	if !strings.Contains(strings.ToLower(out), "no change") && !strings.Contains(out, "0") {
		t.Fatalf("output %q should indicate no changes", out)
	}
}

func TestProjectConfigApply_MissingFileIsUsageError(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := startConfigRoundTripServer(t, applyLiveConfig, http.StatusOK)
	writeRunFileFor(t, cfg, srv)

	_, _, err := executeCLI(t, Deps{
		ProcessAlive: func(int) bool { return true },
	}, "project", "config", "apply", "demo", "/no/such/file.json")
	if err == nil {
		t.Fatal("expected error for missing spec file")
	}
	if got := ExitCode(err); got != 2 {
		t.Fatalf("exit code = %d, want 2 (usage error)", got)
	}
	if capture.putCalled {
		t.Fatal("must not PUT on a missing spec file")
	}
}

func TestProjectConfigApply_InvalidJSONIsUsageError(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := startConfigRoundTripServer(t, applyLiveConfig, http.StatusOK)
	writeRunFileFor(t, cfg, srv)

	spec := writeSpecFile(t, `{not valid json`)

	_, _, err := executeCLI(t, Deps{
		ProcessAlive: func(int) bool { return true },
	}, "project", "config", "apply", "demo", spec)
	if err == nil {
		t.Fatal("expected error for invalid JSON spec")
	}
	if got := ExitCode(err); got != 2 {
		t.Fatalf("exit code = %d, want 2 (usage error)", got)
	}
	if capture.putCalled {
		t.Fatal("must not PUT on an invalid spec file")
	}
}

func TestProjectConfigApply_EmptySpecIsUsageError(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := startConfigRoundTripServer(t, applyLiveConfig, http.StatusOK)
	writeRunFileFor(t, cfg, srv)

	// An empty spec file must not be treated as a no-op — it is operator error.
	spec := writeSpecFile(t, "   \n")

	_, _, err := executeCLI(t, Deps{
		ProcessAlive: func(int) bool { return true },
	}, "project", "config", "apply", "demo", spec)
	if err == nil {
		t.Fatal("expected error for empty spec file")
	}
	if got := ExitCode(err); got != 2 {
		t.Fatalf("exit code = %d, want 2 (usage error)", got)
	}
	if capture.putCalled {
		t.Fatal("must not PUT on an empty spec file")
	}
}

func TestProjectConfigApply_UnknownFieldRejectedByDaemon(t *testing.T) {
	cfg := setConfigEnv(t)
	// Daemon 400s the PUT (simulating DisallowUnknownFields).
	srv, capture := startConfigRoundTripServer(t, applyLiveConfig, http.StatusBadRequest)
	writeRunFileFor(t, cfg, srv)

	spec := writeSpecFile(t, `{"bogus":"x"}`)

	_, _, err := executeCLI(t, Deps{
		ProcessAlive: func(int) bool { return true },
	}, "project", "config", "apply", "demo", spec)
	if err == nil {
		t.Fatal("expected error when daemon rejects unknown field")
	}
	if got := ExitCode(err); got != 1 {
		t.Fatalf("exit code = %d, want 1 (daemon failure)", got)
	}
	if !capture.putCalled {
		t.Fatal("apply should attempt the PUT (daemon is the key validator)")
	}
	if !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("error = %v, want it to surface the daemon rejection", err)
	}
}
