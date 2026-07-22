package agenthealth

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

type stubProber struct {
	mu     sync.Mutex
	probes map[string]Probe
	err    error
	calls  [][]string
}

func (s *stubProber) HarnessHealth(_ context.Context, ids []string) ([]Probe, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, append([]string(nil), ids...))
	if s.err != nil {
		return nil, s.err
	}
	out := make([]Probe, 0, len(ids))
	for _, id := range ids {
		if probe, ok := s.probes[id]; ok {
			out = append(out, probe)
		} else {
			out = append(out, Probe{ID: id})
		}
	}
	return out, nil
}

func (s *stubProber) set(probe Probe) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.probes[probe.ID] = probe
}

func (s *stubProber) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

func authorized(id, label string) Probe {
	return Probe{ID: id, Label: label, Installed: true, AuthStatus: ports.AgentAuthStatusAuthorized}
}

func TestRunOnceMapsTypedHealthAndRemedies(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	prober := &stubProber{probes: map[string]Probe{
		"claude-code": authorized("claude-code", "Claude Code"),
		"codex":       {ID: "codex", Label: "Codex", Installed: true, AuthStatus: ports.AgentAuthStatusUnauthorized},
		"codex-fugu":  {ID: "codex-fugu", Label: "Codex Fugu", Installed: false},
		"grok":        {ID: "grok", Label: "Grok", Installed: true, AuthStatus: ports.AgentAuthStatusUnknown},
	}}
	monitor := New(Deps{
		Prober:    prober,
		Harnesses: func(context.Context) []string { return []string{"grok", "codex", "claude-code", "codex-fugu"} },
		Clock:     func() time.Time { return now },
	})

	if err := monitor.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	snapshot := monitor.Snapshot()
	if !snapshot.CheckedAt.Equal(now) {
		t.Fatalf("CheckedAt = %v, want %v", snapshot.CheckedAt, now)
	}
	byID := map[string]HarnessHealth{}
	for _, health := range snapshot.Harnesses {
		byID[health.ID] = health
	}
	if byID["claude-code"].Health != HealthHealthy {
		t.Fatalf("claude health = %q, want healthy", byID["claude-code"].Health)
	}
	if byID["codex"].Health != HealthUnauthorized || byID["codex"].Remedy != "run `codex login`" {
		t.Fatalf("codex health = %+v, want unauthorized with login remedy", byID["codex"])
	}
	if byID["codex-fugu"].Health != HealthMissing || byID["codex-fugu"].Remedy == "" {
		t.Fatalf("fugu health = %+v, want missing with install remedy", byID["codex-fugu"])
	}
	if byID["grok"].Health != HealthUnknown || byID["grok"].Remedy != "" {
		t.Fatalf("grok health = %+v, want advisory unknown", byID["grok"])
	}
}

func TestRunOnceDeduplicatesAndSortsConfiguredHarnesses(t *testing.T) {
	prober := &stubProber{probes: map[string]Probe{
		"claude-code": authorized("claude-code", "Claude Code"),
		"codex":       authorized("codex", "Codex"),
	}}
	monitor := New(Deps{
		Prober: prober,
		Harnesses: func(context.Context) []string {
			return []string{" codex ", "", "claude-code", "codex", "  "}
		},
	})
	if err := monitor.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(prober.calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(prober.calls))
	}
	want := []string{"claude-code", "codex"}
	if got := prober.calls[0]; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("probed ids = %#v, want %#v", got, want)
	}
	snapshot := monitor.Snapshot()
	if len(snapshot.Harnesses) != 2 || snapshot.Harnesses[0].ID != "claude-code" || snapshot.Harnesses[1].ID != "codex" {
		t.Fatalf("snapshot order = %+v", snapshot.Harnesses)
	}
}

func TestActionableTransitionCallbackFiresExactlyOnce(t *testing.T) {
	prober := &stubProber{probes: map[string]Probe{
		"codex": {ID: "codex", Label: "Codex", Installed: true, AuthStatus: ports.AgentAuthStatusUnknown},
	}}
	var transitions []Transition
	monitor := New(Deps{
		Prober:       prober,
		Harnesses:    func(context.Context) []string { return []string{"codex"} },
		OnTransition: func(transition Transition) { transitions = append(transitions, transition) },
	})
	ctx := context.Background()
	if err := monitor.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(transitions) != 0 {
		t.Fatalf("initial unknown emitted actionable transition: %+v", transitions)
	}

	prober.set(Probe{ID: "codex", Label: "Codex", Installed: true, AuthStatus: ports.AgentAuthStatusUnauthorized})
	if err := monitor.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if err := monitor.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(transitions) != 1 || transitions[0].Current.Health != HealthUnauthorized {
		t.Fatalf("unauthorized transitions = %+v, want one", transitions)
	}

	prober.set(authorized("codex", "Codex"))
	if err := monitor.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if err := monitor.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(transitions) != 2 || transitions[1].Prev != HealthUnauthorized || transitions[1].Current.Health != HealthHealthy {
		t.Fatalf("recovery transitions = %+v, want one recovery", transitions)
	}

	prober.set(Probe{ID: "codex", Label: "Codex", Installed: true, AuthStatus: ports.AgentAuthStatusUnknown})
	if err := monitor.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(transitions) != 2 {
		t.Fatalf("unknown transition must remain advisory: %+v", transitions)
	}
}

func TestInitialActionableObservationEmitsOneTransition(t *testing.T) {
	prober := &stubProber{probes: map[string]Probe{"codex": {ID: "codex", Label: "Codex"}}}
	var transitions []Transition
	monitor := New(Deps{
		Prober:       prober,
		Harnesses:    func(context.Context) []string { return []string{"codex"} },
		OnTransition: func(transition Transition) { transitions = append(transitions, transition) },
	})
	if err := monitor.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := monitor.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(transitions) != 1 || transitions[0].Prev != "" || transitions[0].Current.Health != HealthMissing {
		t.Fatalf("transitions = %+v, want one initial missing transition", transitions)
	}
}

func TestHealthyFirstObservationDoesNotEmitActionableTransition(t *testing.T) {
	prober := &stubProber{probes: map[string]Probe{"codex": authorized("codex", "Codex")}}
	var transitions []Transition
	monitor := New(Deps{
		Prober:       prober,
		Harnesses:    func(context.Context) []string { return []string{"codex"} },
		OnTransition: func(transition Transition) { transitions = append(transitions, transition) },
	})
	if err := monitor.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(transitions) != 0 {
		t.Fatalf("initial healthy observation emitted actionable transition: %+v", transitions)
	}
}

func TestChangedAtStableAndCheckedAtRefreshes(t *testing.T) {
	first := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	now := first
	prober := &stubProber{probes: map[string]Probe{"codex": authorized("codex", "Codex")}}
	monitor := New(Deps{
		Prober:    prober,
		Harnesses: func(context.Context) []string { return []string{"codex"} },
		Clock:     func() time.Time { return now },
	})
	if err := monitor.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	now = first.Add(time.Hour)
	if err := monitor.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	health := monitor.Snapshot().Harnesses[0]
	if !health.ChangedAt.Equal(first) {
		t.Fatalf("ChangedAt = %v, want %v", health.ChangedAt, first)
	}
	if !health.CheckedAt.Equal(now) {
		t.Fatalf("CheckedAt = %v, want %v", health.CheckedAt, now)
	}
}

func TestDroppedHarnessLeavesSnapshot(t *testing.T) {
	ids := []string{"codex", "claude-code"}
	prober := &stubProber{probes: map[string]Probe{
		"codex":       authorized("codex", "Codex"),
		"claude-code": authorized("claude-code", "Claude Code"),
	}}
	monitor := New(Deps{Prober: prober, Harnesses: func(context.Context) []string { return ids }})
	if err := monitor.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	ids = []string{"codex"}
	if err := monitor.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	snapshot := monitor.Snapshot()
	if len(snapshot.Harnesses) != 1 || snapshot.Harnesses[0].ID != "codex" {
		t.Fatalf("snapshot = %+v, want only codex", snapshot.Harnesses)
	}
}

func TestEmptyHarnessListRetainsPriorSnapshot(t *testing.T) {
	ids := []string{"codex"}
	prober := &stubProber{probes: map[string]Probe{"codex": authorized("codex", "Codex")}}
	monitor := New(Deps{Prober: prober, Harnesses: func(context.Context) []string { return ids }})
	if err := monitor.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	ids = nil
	if err := monitor.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(monitor.Snapshot().Harnesses) != 1 {
		t.Fatal("transient empty harness list wiped prior snapshot")
	}
}

func TestRunOnceErrorRetainsPreviousSnapshot(t *testing.T) {
	prober := &stubProber{probes: map[string]Probe{"codex": authorized("codex", "Codex")}}
	monitor := New(Deps{Prober: prober, Harnesses: func(context.Context) []string { return []string{"codex"} }})
	if err := monitor.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	prober.err = errors.New("probe failed")
	if err := monitor.RunOnce(context.Background()); err == nil {
		t.Fatal("RunOnce error = nil, want probe failure")
	}
	snapshot := monitor.Snapshot()
	if len(snapshot.Harnesses) != 1 || snapshot.Harnesses[0].Health != HealthHealthy {
		t.Fatalf("errored check mutated snapshot: %+v", snapshot)
	}
}

func TestRunPerformsImmediateAndIntervalChecksUntilCanceled(t *testing.T) {
	prober := &stubProber{probes: map[string]Probe{"codex": authorized("codex", "Codex")}}
	monitor := New(Deps{Prober: prober, Harnesses: func(context.Context) []string { return []string{"codex"} }})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- monitor.Run(ctx, 5*time.Millisecond) }()
	deadline := time.Now().Add(time.Second)
	for prober.callCount() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not stop after cancellation")
	}
	if prober.callCount() < 2 {
		t.Fatalf("probe calls = %d, want immediate plus interval", prober.callCount())
	}
}

func TestRunWithDisabledIntervalDoesNotProbe(t *testing.T) {
	prober := &stubProber{probes: map[string]Probe{"codex": authorized("codex", "Codex")}}
	monitor := New(Deps{Prober: prober, Harnesses: func(context.Context) []string { return []string{"codex"} }})
	if err := monitor.Run(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if prober.callCount() != 0 {
		t.Fatalf("disabled interval made %d calls", prober.callCount())
	}
}
