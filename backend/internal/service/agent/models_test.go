package agent

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/adapters"
	agentregistry "github.com/aoagents/agent-orchestrator/backend/internal/adapters/agent/registry"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

type catalogProbeAgent struct {
	fakeAgent
	mu           sync.Mutex
	catalog      []ports.ModelCatalogEntry
	catalogErr   error
	catalogCalls int
	probeResult  ports.ModelValidationResult
	probeErr     error
	probedModels []string
}

func (f *catalogProbeAgent) AvailableModels(context.Context) ([]ports.ModelCatalogEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.catalogCalls++
	return cloneCatalogEntries(f.catalog), f.catalogErr
}

func (f *catalogProbeAgent) ValidateModel(_ context.Context, model string) (ports.ModelValidationResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.probedModels = append(f.probedModels, model)
	return f.probeResult, f.probeErr
}

func harnessCatalogAgent(harness domain.AgentHarness, label string, agent ports.Agent) agentregistry.HarnessAgent {
	return agentregistry.HarnessAgent{
		Harness:  harness,
		Manifest: adapters.Manifest{ID: string(harness), Name: label},
		Agent:    agent,
	}
}

func TestModelAvailabilityCatalogFailureIsNeverEmptySuccess(t *testing.T) {
	agent := &catalogProbeAgent{catalogErr: errors.New("native catalog unavailable")}
	svc := NewWithAgents([]agentregistry.HarnessAgent{
		harnessCatalogAgent(domain.HarnessOpenCode, "OpenCode", agent),
	})

	got, err := svc.ModelAvailability(context.Background(), ModelAvailabilityRequest{Force: true})
	if err == nil {
		t.Fatalf("ModelAvailability() = (%#v, nil), want visible error", got)
	}
	if !strings.Contains(err.Error(), "native catalog unavailable") {
		t.Fatalf("error = %q, want adapter catalog failure", err)
	}
}

func TestModelAvailabilityCatalogFailureUsesVisibleConfiguredPinFallback(t *testing.T) {
	agent := &catalogProbeAgent{
		catalogErr:  errors.New("refresh failed"),
		probeResult: ports.ModelValidationResult{Status: ports.ModelValidationReachable},
	}
	svc := NewWithAgents([]agentregistry.HarnessAgent{
		harnessCatalogAgent(domain.HarnessOpenCode, "OpenCode", agent),
	})

	got, err := svc.ModelAvailability(context.Background(), ModelAvailabilityRequest{
		Force: true,
		Pins:  []ModelPin{{Harness: domain.HarnessOpenCode, Model: "openai/gpt-5.4"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	harness := got.Harnesses[0]
	if harness.CatalogSource != ModelCatalogPins || harness.CatalogVerified {
		t.Fatalf("harness = %#v, want unverified configured-pin fallback", harness)
	}
	if !strings.Contains(harness.CatalogReason, "refresh failed") {
		t.Fatalf("catalog reason = %q, want refresh failure", harness.CatalogReason)
	}
	if len(harness.Models) != 1 || harness.Models[0].Model != "openai/gpt-5.4" || harness.Models[0].Verified {
		t.Fatalf("models = %#v, want one unverified configured pin", harness.Models)
	}
}

func TestModelAvailabilityCatalogFailureUsesLastSuccessfulCatalog(t *testing.T) {
	agent := &catalogProbeAgent{catalog: []ports.ModelCatalogEntry{{
		ID:            "openai/gpt-5.4",
		Label:         "GPT-5.4",
		Efforts:       []domain.Effort{domain.EffortLow, domain.EffortHigh},
		DefaultEffort: domain.EffortHigh,
		Dynamic:       true,
	}}}
	svc := NewWithAgents([]agentregistry.HarnessAgent{
		harnessCatalogAgent(domain.HarnessOpenCode, "OpenCode", agent),
	})

	first, err := svc.ModelAvailability(context.Background(), ModelAvailabilityRequest{Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if first.Harnesses[0].CatalogSource != ModelCatalogAdapter || !first.Harnesses[0].CatalogVerified {
		t.Fatalf("first harness = %#v, want verified adapter catalog", first.Harnesses[0])
	}
	agent.mu.Lock()
	agent.catalog = nil
	agent.catalogErr = errors.New("offline")
	agent.mu.Unlock()

	second, err := svc.ModelAvailability(context.Background(), ModelAvailabilityRequest{Force: true})
	if err != nil {
		t.Fatal(err)
	}
	harness := second.Harnesses[0]
	if harness.CatalogSource != ModelCatalogCachedAdapter || harness.CatalogVerified {
		t.Fatalf("second harness = %#v, want visible cached adapter fallback", harness)
	}
	if !strings.Contains(harness.CatalogReason, "offline") {
		t.Fatalf("catalog reason = %q, want offline", harness.CatalogReason)
	}
	model := harness.Models[0]
	if model.Label != "GPT-5.4" || model.DefaultEffort != domain.EffortHigh || !model.Dynamic || model.Verified {
		t.Fatalf("cached model metadata = %#v", model)
	}
	if !reflect.DeepEqual(model.Efforts, []domain.Effort{domain.EffortLow, domain.EffortHigh}) {
		t.Fatalf("cached efforts = %#v", model.Efforts)
	}
}

func TestModelAvailabilityMergesConfiguredPinsAndSortsDeterministically(t *testing.T) {
	agent := &catalogProbeAgent{
		catalog: []ports.ModelCatalogEntry{
			{ID: "provider/z", Label: "Zed", Efforts: []domain.Effort{domain.EffortHigh}},
			{ID: "provider/a", Label: "Alpha", Dynamic: true},
			{ID: "provider/z", Label: "duplicate must not replace metadata"},
		},
		probeResult: ports.ModelValidationResult{Status: ports.ModelValidationReachable},
	}
	svc := NewWithAgents([]agentregistry.HarnessAgent{
		harnessCatalogAgent(domain.HarnessOpenCode, "OpenCode", agent),
	})
	req := ModelAvailabilityRequest{Pins: []ModelPin{
		{Harness: domain.HarnessOpenCode, Model: "provider/m"},
		{Harness: domain.HarnessOpenCode, Model: "provider/z"},
	}}

	got, err := svc.ModelAvailability(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	models := got.Harnesses[0].Models
	if len(models) != 3 || models[0].Model != "provider/a" || models[1].Model != "provider/m" || models[2].Model != "provider/z" {
		t.Fatalf("models = %#v, want deterministic a/m/z", models)
	}
	if models[2].Label != "Zed" || !reflect.DeepEqual(models[2].Efforts, []domain.Effort{domain.EffortHigh}) {
		t.Fatalf("adapter metadata lost for configured catalog pin: %#v", models[2])
	}
	if models[1].Verified {
		t.Fatalf("synthetic configured pin unexpectedly verified: %#v", models[1])
	}
	if models[2].Status != ModelStatusReachable {
		t.Fatalf("configured catalog pin status = %q, want reachable", models[2].Status)
	}
}

func TestModelAvailabilityMarksConfiguredPinWithoutValidatorAsNoCapability(t *testing.T) {
	svc := NewWithAgents([]agentregistry.HarnessAgent{
		harnessCatalogAgent(domain.HarnessClaudeCode, "Claude Code", fakeAgent{}),
	})

	got, err := svc.ModelAvailability(context.Background(), ModelAvailabilityRequest{
		Pins: []ModelPin{{Harness: domain.HarnessClaudeCode, Model: "custom-claude-model"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var custom ModelAvailability
	for _, model := range got.Harnesses[0].Models {
		if model.Model == "custom-claude-model" {
			custom = model
			break
		}
	}
	if custom.Model == "" || custom.Status != ModelStatusUnknown || custom.ReasonCode != ModelReasonNoCapability {
		t.Fatalf("custom model = %#v, want unknown no-capability", custom)
	}
}

func TestModelAvailabilityCachesRequestsForceBypassesAndClones(t *testing.T) {
	agent := &catalogProbeAgent{catalog: []ports.ModelCatalogEntry{{ID: "provider/model", Label: "Model", Efforts: []domain.Effort{domain.EffortHigh}}}}
	svc := NewWithAgents([]agentregistry.HarnessAgent{
		harnessCatalogAgent(domain.HarnessOpenCode, "OpenCode", agent),
	})

	first, err := svc.ModelAvailability(context.Background(), ModelAvailabilityRequest{})
	if err != nil {
		t.Fatal(err)
	}
	first.Harnesses[0].Models[0].Label = "mutated"
	first.Harnesses[0].Models[0].Efforts[0] = domain.EffortLow
	second, err := svc.ModelAvailability(context.Background(), ModelAvailabilityRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if second.Harnesses[0].Models[0].Label != "Model" || second.Harnesses[0].Models[0].Efforts[0] != domain.EffortHigh {
		t.Fatalf("cached response was not deeply cloned: %#v", second)
	}
	agent.mu.Lock()
	calls := agent.catalogCalls
	agent.mu.Unlock()
	if calls != 1 {
		t.Fatalf("catalog calls = %d, want one cached call", calls)
	}
	if _, err := svc.ModelAvailability(context.Background(), ModelAvailabilityRequest{Force: true}); err != nil {
		t.Fatal(err)
	}
	agent.mu.Lock()
	calls = agent.catalogCalls
	agent.mu.Unlock()
	if calls != 2 {
		t.Fatalf("catalog calls after force = %d, want 2", calls)
	}
}

func TestModelAvailabilityCacheExpiresAfterFiveMinutes(t *testing.T) {
	agent := &catalogProbeAgent{catalog: []ports.ModelCatalogEntry{{ID: "provider/model"}}}
	svc := NewWithAgents([]agentregistry.HarnessAgent{
		harnessCatalogAgent(domain.HarnessOpenCode, "OpenCode", agent),
	})
	if _, err := svc.ModelAvailability(context.Background(), ModelAvailabilityRequest{}); err != nil {
		t.Fatal(err)
	}
	svc.modelMu.Lock()
	svc.modelCache.checkedAt = time.Now().Add(-modelAvailabilityCacheTTL - time.Second)
	svc.modelMu.Unlock()
	if _, err := svc.ModelAvailability(context.Background(), ModelAvailabilityRequest{}); err != nil {
		t.Fatal(err)
	}
	agent.mu.Lock()
	calls := agent.catalogCalls
	agent.mu.Unlock()
	if calls != 2 {
		t.Fatalf("catalog calls = %d, want expired cache to refresh", calls)
	}
}

type boundedProbeAgent struct {
	fakeAgent
	mu        sync.Mutex
	active    int
	maxActive int
	started   chan string
	release   <-chan struct{}
}

func (f *boundedProbeAgent) ValidateModel(ctx context.Context, model string) (ports.ModelValidationResult, error) {
	f.mu.Lock()
	f.active++
	if f.active > f.maxActive {
		f.maxActive = f.active
	}
	f.mu.Unlock()
	f.started <- model
	select {
	case <-f.release:
	case <-ctx.Done():
	}
	f.mu.Lock()
	f.active--
	f.mu.Unlock()
	return ports.ModelValidationResult{Status: ports.ModelValidationReachable}, nil
}

func TestModelAvailabilityBoundsConcurrentPinProbesAtFour(t *testing.T) {
	release := make(chan struct{})
	agent := &boundedProbeAgent{started: make(chan string, 6), release: release}
	svc := NewWithAgents([]agentregistry.HarnessAgent{
		harnessCatalogAgent(domain.HarnessOpenCode, "OpenCode", agent),
	})
	pins := make([]ModelPin, 6)
	for i := range pins {
		pins[i] = ModelPin{Harness: domain.HarnessOpenCode, Model: fmt.Sprintf("provider/model-%d", i)}
	}
	done := make(chan error, 1)
	go func() {
		_, err := svc.ModelAvailability(context.Background(), ModelAvailabilityRequest{Pins: pins, Force: true})
		done <- err
	}()
	for range 4 {
		select {
		case <-agent.started:
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for four concurrent probes")
		}
	}
	select {
	case fifth := <-agent.started:
		t.Fatalf("fifth probe %q started before a slot was released", fifth)
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	agent.mu.Lock()
	maxActive := agent.maxActive
	agent.mu.Unlock()
	if maxActive != 4 {
		t.Fatalf("max active probes = %d, want 4", maxActive)
	}
}

type deadlineProbeAgent struct {
	fakeAgent
	mu        sync.Mutex
	remaining []time.Duration
}

func (f *deadlineProbeAgent) ValidateModel(ctx context.Context, _ string) (ports.ModelValidationResult, error) {
	deadline, ok := ctx.Deadline()
	if !ok {
		return ports.ModelValidationResult{}, errors.New("probe context has no deadline")
	}
	f.mu.Lock()
	f.remaining = append(f.remaining, time.Until(deadline))
	f.mu.Unlock()
	return ports.ModelValidationResult{Status: ports.ModelValidationReachable}, nil
}

func TestValidateModelUsesFreshIndependentDeadlinePerPin(t *testing.T) {
	previous := agentModelProbeTimeout
	agentModelProbeTimeout = 200 * time.Millisecond
	t.Cleanup(func() { agentModelProbeTimeout = previous })
	agent := &deadlineProbeAgent{}
	svc := NewWithAgents([]agentregistry.HarnessAgent{
		harnessCatalogAgent(domain.HarnessCodex, "Codex", agent),
	})

	for i := range 3 {
		time.Sleep(25 * time.Millisecond)
		result, err := svc.ValidateModel(context.Background(), domain.HarnessCodex, fmt.Sprintf("model-%d", i))
		if err != nil || result.Status != ports.ModelValidationReachable {
			t.Fatalf("ValidateModel %d = (%#v, %v)", i, result, err)
		}
	}
	agent.mu.Lock()
	remaining := append([]time.Duration(nil), agent.remaining...)
	agent.mu.Unlock()
	for i, duration := range remaining {
		if duration < 150*time.Millisecond {
			t.Fatalf("probe %d inherited a shared deadline: remaining %s", i, duration)
		}
	}
}

func TestClaudeValidationUsesMaintainedCatalogWithoutPaidProbe(t *testing.T) {
	agent := &catalogProbeAgent{
		catalog:     []ports.ModelCatalogEntry{{ID: "opus", Label: "Opus"}},
		probeResult: ports.ModelValidationResult{Status: ports.ModelValidationUnreachable, Message: "paid probe must not run"},
	}
	svc := NewWithAgents([]agentregistry.HarnessAgent{
		harnessCatalogAgent(domain.HarnessClaudeCode, "Claude Code", agent),
	})

	result, err := svc.ValidateModel(context.Background(), domain.HarnessClaudeCode, "opus")
	if err != nil || result.Status != ports.ModelValidationReachable {
		t.Fatalf("ValidateModel = (%#v, %v), want local alias acceptance", result, err)
	}
	availability, err := svc.ModelAvailability(context.Background(), ModelAvailabilityRequest{
		Force: true,
		Pins:  []ModelPin{{Harness: domain.HarnessClaudeCode, Model: "opus"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if availability.Harnesses[0].Models[0].Status != ModelStatusReachable {
		t.Fatalf("Claude availability = %#v, want catalog-verified reachable", availability)
	}
	agent.mu.Lock()
	probes := append([]string(nil), agent.probedModels...)
	agent.mu.Unlock()
	if len(probes) != 0 {
		t.Fatalf("Claude catalog/save path ran paid probes: %#v", probes)
	}
}

func TestThreeWayVerdictsFeedCacheOnlySpawnValidation(t *testing.T) {
	agent := &catalogProbeAgent{}
	svc := NewWithAgents([]agentregistry.HarnessAgent{
		harnessCatalogAgent(domain.HarnessCodex, "Codex", agent),
	})

	miss, err := svc.ValidateSpawnModel(context.Background(), domain.HarnessCodex, "missing")
	if err != nil || miss.Status != ports.ModelValidationProbeUnavailable {
		t.Fatalf("cache miss = (%#v, %v), want probe-unavailable", miss, err)
	}
	agent.mu.Lock()
	if len(agent.probedModels) != 0 {
		t.Fatalf("spawn validation performed network probes: %#v", agent.probedModels)
	}
	agent.probeResult = ports.ModelValidationResult{Status: ports.ModelValidationUnreachable, Message: "400 model rejected"}
	agent.mu.Unlock()
	result, err := svc.ValidateModel(context.Background(), domain.HarnessCodex, "blocked")
	if err != nil || result.Status != ports.ModelValidationUnreachable {
		t.Fatalf("ValidateModel = (%#v, %v)", result, err)
	}
	blocked, err := svc.ValidateSpawnModel(context.Background(), domain.HarnessCodex, "blocked")
	if err != nil || blocked.Status != ports.ModelValidationUnreachable || !strings.Contains(blocked.Message, "400") {
		t.Fatalf("cached rejection = (%#v, %v)", blocked, err)
	}
}

func TestValidateSpawnSelectionRejectsUnsupportedEffortFromFreshCatalogWithoutProbing(t *testing.T) {
	agent := &catalogProbeAgent{
		catalog: []ports.ModelCatalogEntry{{ID: "gpt-5-codex", Efforts: []domain.Effort{domain.EffortHigh}}},
	}
	svc := NewWithAgents([]agentregistry.HarnessAgent{
		harnessCatalogAgent(domain.HarnessCodex, "Codex", agent),
	})
	svc.storeSuccessfulCatalog(domain.HarnessCodex, agent.catalog)
	svc.recordPinVerdict(domain.HarnessCodex, "gpt-5-codex", pinVerdict{
		result: ports.ModelValidationResult{Status: ports.ModelValidationReachable},
	})

	result, err := svc.ValidateSpawnSelection(context.Background(), domain.HarnessCodex, "gpt-5-codex", domain.EffortXHigh)
	if err != nil || result.Status != ports.ModelValidationUnreachable {
		t.Fatalf("ValidateSpawnSelection = (%#v, %v), want unreachable", result, err)
	}
	if !strings.Contains(result.Message, "xhigh") || !strings.Contains(result.Message, "high") {
		t.Fatalf("message = %q, want requested and supported efforts", result.Message)
	}
	agent.mu.Lock()
	catalogCalls := agent.catalogCalls
	probeCalls := len(agent.probedModels)
	agent.mu.Unlock()
	if catalogCalls != 0 || probeCalls != 0 {
		t.Fatalf("spawn selection ran catalog=%d probe=%d calls, want cache-only", catalogCalls, probeCalls)
	}
}

func TestValidateSpawnSelectionAcceptsSupportedEffortFromFreshCatalog(t *testing.T) {
	svc := NewWithAgents(nil)
	svc.storeSuccessfulCatalog(domain.HarnessCodex, []ports.ModelCatalogEntry{{
		ID:      "gpt-5-codex",
		Efforts: []domain.Effort{domain.EffortHigh, domain.EffortXHigh},
	}})
	svc.recordPinVerdict(domain.HarnessCodex, "gpt-5-codex", pinVerdict{
		result: ports.ModelValidationResult{Status: ports.ModelValidationReachable},
	})

	result, err := svc.ValidateSpawnSelection(context.Background(), domain.HarnessCodex, "gpt-5-codex", domain.EffortXHigh)
	if err != nil || result.Status != ports.ModelValidationReachable {
		t.Fatalf("ValidateSpawnSelection = (%#v, %v), want reachable", result, err)
	}
}

func TestValidateSpawnSelectionFailsOpenWhenCatalogIsMissingOrStale(t *testing.T) {
	for _, tc := range []struct {
		name string
		seed func(*Service)
	}{
		{name: "missing"},
		{name: "stale", seed: func(svc *Service) {
			svc.catalogCache[domain.HarnessCodex] = cachedModelCatalog{
				entries:   []ports.ModelCatalogEntry{{ID: "gpt-5-codex", Efforts: []domain.Effort{domain.EffortHigh}}},
				checkedAt: time.Now().Add(-spawnPinVerdictTTL - time.Minute),
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewWithAgents(nil)
			svc.recordPinVerdict(domain.HarnessCodex, "gpt-5-codex", pinVerdict{
				result: ports.ModelValidationResult{Status: ports.ModelValidationReachable},
			})
			if tc.seed != nil {
				tc.seed(svc)
			}

			result, err := svc.ValidateSpawnSelection(context.Background(), domain.HarnessCodex, "gpt-5-codex", domain.EffortXHigh)
			if err != nil || result.Status != ports.ModelValidationProbeUnavailable {
				t.Fatalf("ValidateSpawnSelection = (%#v, %v), want probe-unavailable", result, err)
			}
		})
	}
}

func TestValidateModelSelectionChecksFreshCatalogEffortAndClaudeRemainsPromptFree(t *testing.T) {
	agent := &catalogProbeAgent{
		catalog: []ports.ModelCatalogEntry{{ID: "opus", Efforts: []domain.Effort{domain.EffortHigh}}},
		probeResult: ports.ModelValidationResult{
			Status:  ports.ModelValidationUnreachable,
			Message: "paid probe must not run",
		},
	}
	svc := NewWithAgents([]agentregistry.HarnessAgent{
		harnessCatalogAgent(domain.HarnessClaudeCode, "Claude Code", agent),
	})

	result, err := svc.ValidateModelSelection(context.Background(), domain.HarnessClaudeCode, "opus", domain.EffortXHigh)
	if err != nil || result.Status != ports.ModelValidationUnreachable {
		t.Fatalf("ValidateModelSelection = (%#v, %v), want unsupported effort rejection", result, err)
	}
	agent.mu.Lock()
	probes := append([]string(nil), agent.probedModels...)
	agent.mu.Unlock()
	if len(probes) != 0 {
		t.Fatalf("Claude selection validation ran paid probes: %#v", probes)
	}
}

func TestPinVerdictTTLAndEvictionPreserveFreshRejections(t *testing.T) {
	svc := NewWithAgents(nil)
	svc.recordPinVerdict(domain.HarnessCodex, "expired", pinVerdict{
		result:    ports.ModelValidationResult{Status: ports.ModelValidationUnreachable},
		checkedAt: time.Now().Add(-spawnPinVerdictTTL - time.Minute),
	})
	svc.recordPinVerdict(domain.HarnessCodex, "blocked", pinVerdict{
		result: ports.ModelValidationResult{Status: ports.ModelValidationUnreachable, Message: "rejected"},
	})
	stale := time.Now().Add(-likelyUnconfiguredPinAge - time.Hour)
	for i := range maxPinVerdicts + 50 {
		svc.recordPinVerdict(domain.HarnessCodex, fmt.Sprintf("churn-%03d", i), pinVerdict{
			result:    ports.ModelValidationResult{Status: ports.ModelValidationReachable},
			checkedAt: stale.Add(time.Duration(i) * time.Millisecond),
		})
	}
	svc.modelMu.Lock()
	size := len(svc.pinVerdicts)
	_, expired := svc.pinVerdicts[modelPinKey(domain.HarnessCodex, "expired")]
	_, blocked := svc.pinVerdicts[modelPinKey(domain.HarnessCodex, "blocked")]
	svc.modelMu.Unlock()
	if size > maxPinVerdicts {
		t.Fatalf("verdict cache size = %d, want <= %d", size, maxPinVerdicts)
	}
	if expired {
		t.Fatal("expired verdict survived pruning")
	}
	if !blocked {
		t.Fatal("fresh definitive rejection was evicted before cheaper entries")
	}
}
