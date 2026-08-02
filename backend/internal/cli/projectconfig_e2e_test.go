package cli

// projectconfig_e2e_test.go drives `ao project config export/apply/diff` through
// the REAL daemon HTTP router + REAL controllers (fakes only below the
// controller, at the service layer), over a genuine loopback round trip. It is
// the DTO-drift + round-trip guard for config-as-code: it proves export is
// lossless through the real serializer, that an export→apply round trip changes
// nothing (and performs no write), and that a surgical apply's merged PUT body
// decodes cleanly through the controller's strict decoder into
// domain.ProjectConfig.

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	projectsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/project"
)

// statefulProjectManager persists SetConfig so a subsequent Get reflects it,
// enabling a genuine export→apply→export round trip. It embeds fakeProjectManager
// for the no-op methods and overrides only Get and SetConfig.
type statefulProjectManager struct {
	*fakeProjectManager
	config   domain.ProjectConfig
	setCalls int
}

func (m *statefulProjectManager) Get(_ context.Context, id domain.ProjectID) (projectsvc.GetResult, error) {
	cfg := m.config
	p := projectsvc.Project{ID: id, Path: "/repo/" + string(id), Config: &cfg, ConfigETag: cfg.ETag()}
	return projectsvc.GetResult{Status: "ok", Project: &p}, nil
}

func (m *statefulProjectManager) SetConfig(_ context.Context, id domain.ProjectID, in projectsvc.SetConfigInput) (projectsvc.Project, error) {
	m.config = in.Config
	m.setCalls++
	cfg := m.config
	return projectsvc.Project{ID: id, Config: &cfg, ConfigETag: cfg.ETag()}, nil
}

func runConfigCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	root := NewRootCommand(Deps{
		Out:          &out,
		Err:          &out,
		HTTPClient:   &http.Client{},
		ProcessAlive: func(int) bool { return true },
	})
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func TestE2E_ProjectConfigRoundTrip(t *testing.T) {
	// Seed includes MaxLiveWorkers, which the typed CLI config mirror does not
	// carry — export must still round-trip it (lossless via raw JSON).
	pm := &statefulProjectManager{
		fakeProjectManager: &fakeProjectManager{},
		config: domain.ProjectConfig{
			DefaultBranch:    "main",
			SessionPrefix:    "demo",
			WorkerTaskPrompt: "/address-issue {issue}",
			MaxLiveWorkers:   7,
		},
	}
	startDriftTestDaemon(t, &fakeSessionService{}, pm)

	// Export captures MaxLiveWorkers even though the CLI mirror lacks it.
	e1, err := runConfigCLI(t, "project", "config", "export", "demo")
	if err != nil {
		t.Fatalf("export: %v\n%s", err, e1)
	}
	if !strings.Contains(e1, "maxLiveWorkers") || !strings.Contains(e1, "7") {
		t.Fatalf("export dropped MaxLiveWorkers (not in CLI mirror): %s", e1)
	}
	if !strings.Contains(e1, "workerTaskPrompt") || !strings.Contains(e1, "/address-issue {issue}") {
		t.Fatalf("export dropped WorkerTaskPrompt: %s", e1)
	}

	// Round trip: applying the full export changes nothing and performs no write.
	specFull := writeSpecFile(t, e1)
	appliedOut, err := runConfigCLI(t, "project", "config", "apply", "demo", specFull)
	if err != nil {
		t.Fatalf("apply full export: %v\n%s", err, appliedOut)
	}
	if pm.setCalls != 0 {
		t.Fatalf("round-trip apply wrote config (%d SetConfig calls); want 0", pm.setCalls)
	}

	// Export again is byte-identical.
	e2, err := runConfigCLI(t, "project", "config", "export", "demo")
	if err != nil {
		t.Fatalf("second export: %v\n%s", err, e2)
	}
	if e1 != e2 {
		t.Fatalf("export not byte-stable across round trip:\nA=%q\nB=%q", e1, e2)
	}

	// Diff of the full export against live is clean (exit zero).
	if _, err := runConfigCLI(t, "project", "config", "diff", "demo", specFull); err != nil {
		t.Fatalf("diff of matching config should be clean: %v", err)
	}

	// Surgical apply of a two-field spec persists exactly those fields through the
	// real controller's strict decoder; the merged body must decode into
	// domain.ProjectConfig without an unknown-field 400.
	specTwo := writeSpecFile(t, `{"sessionPrefix":"prod","workerTaskPrompt":"/fix {issue}","maxLiveWorkers":9}`)
	twoOut, err := runConfigCLI(t, "project", "config", "apply", "demo", specTwo)
	if err != nil {
		t.Fatalf("surgical apply: %v\n%s", err, twoOut)
	}
	if pm.setCalls != 1 {
		t.Fatalf("SetConfig calls = %d, want 1", pm.setCalls)
	}
	if pm.config.SessionPrefix != "prod" {
		t.Errorf("SessionPrefix = %q, want prod", pm.config.SessionPrefix)
	}
	if pm.config.MaxLiveWorkers != 9 {
		t.Errorf("MaxLiveWorkers = %d, want 9", pm.config.MaxLiveWorkers)
	}
	if pm.config.WorkerTaskPrompt != "/fix {issue}" {
		t.Errorf("WorkerTaskPrompt = %q, want /fix {issue}", pm.config.WorkerTaskPrompt)
	}
	// Unnamed field preserved.
	if pm.config.DefaultBranch != "main" {
		t.Errorf("DefaultBranch = %q, want main (preserved)", pm.config.DefaultBranch)
	}

	// Diff now reports drift against the original full export (exit nonzero).
	if _, err := runConfigCLI(t, "project", "config", "diff", "demo", specFull); err == nil {
		t.Fatal("diff should report drift after surgical change")
	}
}

// TestE2E_ProjectConfigApply_UnknownKeyRejectedByRealDecoder proves the design's
// D5 claim end-to-end: an unknown config key is rejected by the controller's
// strict decoder (DisallowUnknownFields) with a nonzero exit and no SetConfig
// call — the daemon, not the CLI, is the authoritative key validator.
func TestE2E_ProjectConfigApply_UnknownKeyRejectedByRealDecoder(t *testing.T) {
	pm := &statefulProjectManager{
		fakeProjectManager: &fakeProjectManager{},
		config:             domain.ProjectConfig{DefaultBranch: "main"},
	}
	startDriftTestDaemon(t, &fakeSessionService{}, pm)

	spec := writeSpecFile(t, `{"notARealConfigField":"x"}`)
	out, err := runConfigCLI(t, "project", "config", "apply", "demo", spec)
	if err == nil {
		t.Fatalf("expected nonzero exit for unknown config key; output=%s", out)
	}
	if got := ExitCode(err); got != 1 {
		t.Fatalf("exit code = %d, want 1 (daemon rejection)", got)
	}
	if pm.setCalls != 0 {
		t.Fatalf("SetConfig calls = %d, want 0 (strict decoder rejects before service)", pm.setCalls)
	}
}
