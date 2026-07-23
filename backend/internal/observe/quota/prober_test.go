package quota

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// fakeProber is an in-test AgentQuotaProber returning a fixed result, recording
// call count and observing concurrency with itself.
type fakeProber struct {
	result ports.QuotaProbeResult
	err    error

	mu       sync.Mutex
	calls    int
	inFlight int
	maxSeen  int
	hold     chan struct{} // when non-nil, ProbeQuota blocks until closed
}

func (f *fakeProber) ProbeQuota(ctx context.Context, _ time.Time) (ports.QuotaProbeResult, error) {
	f.mu.Lock()
	f.calls++
	f.inFlight++
	if f.inFlight > f.maxSeen {
		f.maxSeen = f.inFlight
	}
	hold := f.hold
	f.mu.Unlock()

	if hold != nil {
		select {
		case <-hold:
		case <-ctx.Done():
		}
	}

	f.mu.Lock()
	f.inFlight--
	f.mu.Unlock()
	return f.result, f.err
}

func (f *fakeProber) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeProber) maxConcurrency() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.maxSeen
}

// fakeEnumerator returns a fixed set of harness probers.
type fakeEnumerator struct {
	probers []ports.HarnessQuotaProber
}

func (e *fakeEnumerator) QuotaProbers(context.Context) []ports.HarnessQuotaProber {
	return e.probers
}

// fakeStore records every upserted snapshot.
type fakeStore struct {
	mu    sync.Mutex
	snaps []domain.QuotaSnapshot
}

func (s *fakeStore) UpsertQuotaSnapshot(_ context.Context, snap domain.QuotaSnapshot) (domain.QuotaSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snaps = append(s.snaps, snap)
	return snap, nil
}

func (s *fakeStore) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.snaps)
}

func snap(h domain.AgentHarness) domain.QuotaSnapshot {
	used := 42.0
	return domain.QuotaSnapshot{Harness: h, Used: &used, SignalQuality: domain.QuotaSignalExact}
}

var fixedNow = time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)

func newTestProber(enum Enumerator, store Store) *Prober {
	return New(Deps{
		Enumerator: enum,
		Store:      store,
		Now:        func() time.Time { return fixedNow },
	})
}

func TestProbeAllRecordsStatusesAndPersistsOKSnapshots(t *testing.T) {
	okWithData := &fakeProber{result: ports.QuotaProbeResult{
		State:     domain.QuotaProbeOK,
		Snapshots: []domain.QuotaSnapshot{snap(domain.HarnessClaudeCode)},
	}}
	failed := &fakeProber{result: ports.QuotaProbeResult{State: domain.QuotaProbeFailed, Reason: "boom"}}
	okEmpty := &fakeProber{result: ports.QuotaProbeResult{State: domain.QuotaProbeOK}}

	enum := &fakeEnumerator{probers: []ports.HarnessQuotaProber{
		{Harness: domain.HarnessClaudeCode, Prober: okWithData},
		{Harness: domain.HarnessCodex, Prober: failed},
		{Harness: domain.HarnessOpenCode, Prober: okEmpty},
	}}
	store := &fakeStore{}
	p := newTestProber(enum, store)

	statuses := p.ProbeAll(context.Background())
	if len(statuses) != 3 {
		t.Fatalf("ProbeAll returned %d statuses, want 3", len(statuses))
	}

	byHarness := map[domain.AgentHarness]domain.HarnessQuotaStatus{}
	for _, s := range statuses {
		byHarness[s.Harness] = s
	}

	cc := byHarness[domain.HarnessClaudeCode]
	if cc.State != domain.QuotaProbeOK || !cc.HasData {
		t.Fatalf("claude-code status = %#v, want ok+hasData", cc)
	}
	if !cc.ProbedAt.Equal(fixedNow) {
		t.Fatalf("claude-code ProbedAt = %v, want %v", cc.ProbedAt, fixedNow)
	}

	fl := byHarness[domain.HarnessCodex]
	if fl.State != domain.QuotaProbeFailed || fl.Reason != "boom" || fl.HasData {
		t.Fatalf("codex status = %#v, want failed+reason+noData", fl)
	}

	em := byHarness[domain.HarnessOpenCode]
	if em.State != domain.QuotaProbeOK || em.HasData {
		t.Fatalf("opencode status = %#v, want ok+noData", em)
	}

	// Only the ok-with-data prober persists a snapshot; failed does not.
	if store.count() != 1 {
		t.Fatalf("store has %d snapshots, want 1 (only ok-with-data persists)", store.count())
	}
}

func TestStatusesSortedByHarness(t *testing.T) {
	enum := &fakeEnumerator{probers: []ports.HarnessQuotaProber{
		{Harness: domain.HarnessOpenCode, Prober: &fakeProber{result: ports.QuotaProbeResult{State: domain.QuotaProbeOK}}},
		{Harness: domain.HarnessClaudeCode, Prober: &fakeProber{result: ports.QuotaProbeResult{State: domain.QuotaProbeOK}}},
	}}
	p := newTestProber(enum, &fakeStore{})
	p.ProbeAll(context.Background())

	statuses := p.Statuses()
	if len(statuses) != 2 {
		t.Fatalf("Statuses len = %d, want 2", len(statuses))
	}
	if statuses[0].Harness > statuses[1].Harness {
		t.Fatalf("Statuses not sorted: %q before %q", statuses[0].Harness, statuses[1].Harness)
	}
}

func TestProbeHarnessUnknownReturnsFalse(t *testing.T) {
	enum := &fakeEnumerator{probers: []ports.HarnessQuotaProber{
		{Harness: domain.HarnessClaudeCode, Prober: &fakeProber{result: ports.QuotaProbeResult{State: domain.QuotaProbeOK}}},
	}}
	p := newTestProber(enum, &fakeStore{})

	_, ok := p.ProbeHarness(context.Background(), domain.HarnessCodex)
	if ok {
		t.Fatal("ProbeHarness(unknown) ok = true, want false")
	}
}

func TestProbeQuotaGoErrorRecordsFailedStatus(t *testing.T) {
	boom := &fakeProber{err: errors.New("probe exploded")}
	enum := &fakeEnumerator{probers: []ports.HarnessQuotaProber{
		{Harness: domain.HarnessClaudeCode, Prober: boom},
	}}
	p := newTestProber(enum, &fakeStore{})

	status, ok := p.ProbeHarness(context.Background(), domain.HarnessClaudeCode)
	if !ok {
		t.Fatal("ProbeHarness(known) ok = false, want true")
	}
	if status.State != domain.QuotaProbeFailed {
		t.Fatalf("status.State = %q, want failed", status.State)
	}
	if status.Reason == "" {
		t.Fatal("failed status Reason is empty, want the error text")
	}
}

func TestProbeHarnessSingleFlight(t *testing.T) {
	hold := make(chan struct{})
	slow := &fakeProber{result: ports.QuotaProbeResult{State: domain.QuotaProbeOK}, hold: hold}
	enum := &fakeEnumerator{probers: []ports.HarnessQuotaProber{
		{Harness: domain.HarnessClaudeCode, Prober: slow},
	}}
	p := newTestProber(enum, &fakeStore{})

	var started sync.WaitGroup
	var done sync.WaitGroup
	var completed int32
	for i := 0; i < 2; i++ {
		started.Add(1)
		done.Add(1)
		go func() {
			started.Done()
			p.ProbeHarness(context.Background(), domain.HarnessClaudeCode)
			atomic.AddInt32(&completed, 1)
			done.Done()
		}()
	}
	started.Wait()
	// Give both goroutines a moment to contend on the flight lock.
	time.Sleep(50 * time.Millisecond)
	close(hold)
	done.Wait()

	if got := slow.maxConcurrency(); got != 1 {
		t.Fatalf("max concurrent ProbeQuota = %d, want 1 (single-flight per harness)", got)
	}
	if slow.callCount() < 1 {
		t.Fatal("ProbeQuota never called")
	}
}

func TestStartRunsInitialProbeAndStops(t *testing.T) {
	fp := &fakeProber{result: ports.QuotaProbeResult{State: domain.QuotaProbeOK}}
	enum := &fakeEnumerator{probers: []ports.HarnessQuotaProber{
		{Harness: domain.HarnessClaudeCode, Prober: fp},
	}}
	p := New(Deps{
		Enumerator: enum,
		Store:      &fakeStore{},
		Interval:   time.Hour,
		Now:        func() time.Time { return fixedNow },
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := p.Start(ctx)

	// The initial probe runs inside Start's goroutine; poll until it lands.
	deadline := time.After(2 * time.Second)
	for fp.callCount() == 0 {
		select {
		case <-deadline:
			t.Fatal("initial probe did not run")
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Start loop did not exit after ctx cancel")
	}
}
