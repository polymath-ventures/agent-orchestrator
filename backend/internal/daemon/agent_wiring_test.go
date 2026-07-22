package daemon

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	agentsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/agent"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/agenthealth"
	modelhealthsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/modelhealth"
)

type fakeConfiguredProjects struct {
	projects []domain.ProjectRecord
	err      error
}

func (f fakeConfiguredProjects) ListProjects(context.Context) ([]domain.ProjectRecord, error) {
	return append([]domain.ProjectRecord(nil), f.projects...), f.err
}

func TestConfiguredProjectModelsProjectsPinsOnceAndSortsAcrossProjects(t *testing.T) {
	source := fakeConfiguredProjects{projects: []domain.ProjectRecord{
		{ID: "b", Config: domain.ProjectConfig{AgentConfig: domain.AgentConfig{ModelByHarness: map[domain.AgentHarness]domain.HarnessModel{
			domain.HarnessOpenCode: {Model: "openai/gpt-5.4"},
			domain.HarnessCodex:    {Model: "gpt-5-codex"},
		}}}},
		{ID: "a", Config: domain.ProjectConfig{
			Worker: domain.RoleOverride{Harness: domain.HarnessAmp},
			AgentConfig: domain.AgentConfig{ModelByHarness: map[domain.AgentHarness]domain.HarnessModel{
				domain.HarnessCodex: {Model: "gpt-5-codex"},
			}},
		}},
	}}
	models := newConfiguredProjectModels(source, slog.New(slog.NewTextHandler(io.Discard, nil)))

	pins, err := models.ListModelPins(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	wantPins := []agentsvc.ModelPin{
		{Harness: domain.HarnessCodex, Model: "gpt-5-codex"},
		{Harness: domain.HarnessOpenCode, Model: "openai/gpt-5.4"},
	}
	if !reflect.DeepEqual(pins, wantPins) {
		t.Fatalf("pins = %#v, want %#v", pins, wantPins)
	}
	wantHarnesses := []string{"amp", "claude-code", "codex", "opencode"}
	if got := models.ConfiguredHarnesses(context.Background()); !reflect.DeepEqual(got, wantHarnesses) {
		t.Fatalf("harnesses = %#v, want %#v", got, wantHarnesses)
	}
	healthPins, err := models.ListModelHealthPins(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	wantHealthPins := []modelhealthsvc.Pin{
		{ProjectID: "a", Scope: "agentConfig.modelByHarness[codex]", Harness: domain.HarnessCodex, Model: "gpt-5-codex"},
		{ProjectID: "b", Scope: "agentConfig.modelByHarness[codex]", Harness: domain.HarnessCodex, Model: "gpt-5-codex"},
		{ProjectID: "b", Scope: "agentConfig.modelByHarness[opencode]", Harness: domain.HarnessOpenCode, Model: "openai/gpt-5.4"},
	}
	if !reflect.DeepEqual(healthPins, wantHealthPins) {
		t.Fatalf("health pins = %#v, want %#v", healthPins, wantHealthPins)
	}
}

func TestConfiguredProjectModelsIncludesResolvedDefaultReviewerHarness(t *testing.T) {
	source := fakeConfiguredProjects{projects: []domain.ProjectRecord{
		{ID: "codex-worker", Config: domain.ProjectConfig{Worker: domain.RoleOverride{Harness: domain.HarnessCodex}}},
	}}
	models := newConfiguredProjectModels(source, slog.New(slog.NewTextHandler(io.Discard, nil)))

	want := []string{"claude-code", "codex"}
	if got := models.ConfiguredHarnesses(context.Background()); !reflect.DeepEqual(got, want) {
		t.Fatalf("configured harnesses = %#v, want %#v", got, want)
	}
}

func TestConfiguredProjectModelsPreservesStoreError(t *testing.T) {
	boom := errors.New("projects unavailable")
	models := newConfiguredProjectModels(fakeConfiguredProjects{err: boom}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, err := models.ListModelPins(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("ListModelPins error = %v, want %v", err, boom)
	}
	if got := models.ConfiguredHarnesses(context.Background()); got != nil {
		t.Fatalf("configured harnesses = %#v, want nil on transient store failure", got)
	}
}

type fakeAgentProbeService struct {
	results map[string]agentsvc.ProbeResult
	err     error
	calls   []string
}

func (f *fakeAgentProbeService) Probe(_ context.Context, id string) (agentsvc.ProbeResult, error) {
	f.calls = append(f.calls, id)
	return f.results[id], f.err
}

func TestAgentHealthProberMapsAgentServiceResults(t *testing.T) {
	agents := &fakeAgentProbeService{results: map[string]agentsvc.ProbeResult{
		"codex": {
			Agent:     agentsvc.Info{ID: "codex", Label: "Codex", AuthStatus: ports.AgentAuthStatusAuthorized},
			Supported: true,
			Installed: true,
		},
		"opencode": {
			Agent:     agentsvc.Info{ID: "opencode", Label: "OpenCode", AuthStatus: ports.AgentAuthStatusUnauthorized},
			Supported: true,
			Installed: true,
		},
	}}
	probes, err := (agentHealthProber{agents: agents}).HarnessHealth(context.Background(), []string{"codex", "opencode"})
	if err != nil {
		t.Fatal(err)
	}
	want := []agenthealth.Probe{
		{ID: "codex", Label: "Codex", Installed: true, AuthStatus: ports.AgentAuthStatusAuthorized},
		{ID: "opencode", Label: "OpenCode", Installed: true, AuthStatus: ports.AgentAuthStatusUnauthorized},
	}
	if !reflect.DeepEqual(probes, want) {
		t.Fatalf("probes = %#v, want %#v", probes, want)
	}
}

type fakeAgentHealthRunner struct {
	mu       sync.Mutex
	calls    int
	interval time.Duration
}

func (f *fakeAgentHealthRunner) Run(ctx context.Context, interval time.Duration) error {
	f.mu.Lock()
	f.calls++
	f.interval = interval
	f.mu.Unlock()
	<-ctx.Done()
	return nil
}

func (f *fakeAgentHealthRunner) state() (int, time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls, f.interval
}

func TestStartAgentHealthMonitorHonorsDisabledAndCancellation(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	disabled := &fakeAgentHealthRunner{}
	done := startAgentHealthMonitor(context.Background(), disabled, 0, log)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("disabled monitor did not return a closed done channel")
	}
	if calls, _ := disabled.state(); calls != 0 {
		t.Fatalf("disabled monitor calls = %d, want 0", calls)
	}

	enabled := &fakeAgentHealthRunner{}
	ctx, cancel := context.WithCancel(context.Background())
	done = startAgentHealthMonitor(ctx, enabled, 25*time.Millisecond, log)
	deadline := time.Now().Add(time.Second)
	for {
		calls, interval := enabled.state()
		if calls == 1 {
			if interval != 25*time.Millisecond {
				t.Fatalf("interval = %s, want 25ms", interval)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("enabled monitor did not start")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("enabled monitor did not stop after cancellation")
	}
}
