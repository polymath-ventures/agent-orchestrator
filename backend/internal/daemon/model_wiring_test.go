package daemon

import (
	"context"
	"io"
	"log/slog"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	agentsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/agent"
	modelhealthsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/modelhealth"
)

type fakeModelHealthSnapshot struct {
	snapshot modelhealthsvc.Snapshot
}

func (f fakeModelHealthSnapshot) Snapshot() modelhealthsvc.Snapshot { return f.snapshot }

func TestModelHealthProjectionPreservesProjectDetailRows(t *testing.T) {
	checked := time.Date(2026, 7, 22, 1, 2, 3, 0, time.UTC)
	pins := []modelhealthsvc.Pin{
		{ProjectID: "a", Scope: "worker", Harness: domain.HarnessCodex, Model: "gpt-codex"},
		{ProjectID: "a", Scope: "workerMix[0]", Harness: domain.HarnessAmp, Model: "amp-model"},
		{ProjectID: "a", Scope: "reviewers[0]", Harness: domain.HarnessOpenCode, Model: "openai/gpt"},
		{ProjectID: "b", Scope: "worker", Harness: domain.HarnessCodex, Model: "other"},
	}
	projection := modelHealthProjection{
		monitor: fakeModelHealthSnapshot{snapshot: modelhealthsvc.Snapshot{
			CheckedAt: checked,
			Verdicts: []modelhealthsvc.Verdict{
				{Pin: pins[0], Status: agentsvc.ModelStatusReachable, CheckedAt: checked},
				{Pin: pins[3], Status: agentsvc.ModelStatusUnreachable, Reason: "not for project a", CheckedAt: checked},
			},
		}},
		pins: func(context.Context) ([]modelhealthsvc.Pin, error) { return pins, nil },
		supportsValidation: func(harness domain.AgentHarness) bool {
			return harness != domain.HarnessAmp
		},
	}
	rows, err := projection.ListProject(context.Background(), "a")
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.ModelAvailability{
		{ProjectID: "a", Harness: domain.HarnessAmp, Model: "amp-model", Status: domain.ModelAvailabilityUnknown, Reason: domain.ModelAvailabilityReasonNoCapability},
		{ProjectID: "a", Harness: domain.HarnessCodex, Model: "gpt-codex", Status: domain.ModelAvailabilityReachable, Reason: domain.ModelAvailabilityReasonReachable, ObservedAt: checked, UpdatedAt: checked},
		{ProjectID: "a", Harness: domain.HarnessOpenCode, Model: "openai/gpt", Status: domain.ModelAvailabilityUnknown, Reason: domain.ModelAvailabilityReasonNotProbed},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("rows = %#v, want %#v", rows, want)
	}
}

type fakeModelHealthRunner struct {
	mu       sync.Mutex
	calls    int
	interval time.Duration
}

func (f *fakeModelHealthRunner) Run(ctx context.Context, interval time.Duration) error {
	f.mu.Lock()
	f.calls++
	f.interval = interval
	f.mu.Unlock()
	<-ctx.Done()
	return nil
}

func (f *fakeModelHealthRunner) state() (int, time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls, f.interval
}

func TestStartModelHealthMonitorUsesConfiguredIntervalAndZeroDisables(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	disabled := &fakeModelHealthRunner{}
	done := startModelHealthMonitor(context.Background(), disabled, 0, logger)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("disabled model monitor did not return a closed channel")
	}
	if calls, _ := disabled.state(); calls != 0 {
		t.Fatalf("disabled calls = %d, want 0", calls)
	}

	enabled := &fakeModelHealthRunner{}
	ctx, cancel := context.WithCancel(context.Background())
	done = startModelHealthMonitor(ctx, enabled, 24*time.Hour, logger)
	deadline := time.Now().Add(time.Second)
	for {
		calls, interval := enabled.state()
		if calls == 1 {
			if interval != 24*time.Hour {
				t.Fatalf("interval = %s, want 24h", interval)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("enabled model monitor did not start")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("enabled model monitor did not stop")
	}
}
