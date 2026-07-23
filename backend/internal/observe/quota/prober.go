// Package quota contains the daemon's harness quota prober. It drives each
// installed harness adapter that implements ports.AgentQuotaProber on a slow
// cadence (and on demand), persists real usage snapshots to durable storage so
// alerts/history/immediate-load all see probe data, and holds honest per-harness
// probe status in memory (rebuilt each daemon run, never persisted).
//
// It depends only on the Enumerator and Store interfaces, not on
// service/agent, so the daemon owns the coupling and this package stays testable
// with fakes.
package quota

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/observe"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// DefaultInterval is the probe cadence when Deps.Interval is zero. Probing can
// cost a real quota turn (e.g. claude -p "/usage"), so the default is slow.
const DefaultInterval = time.Hour

// Enumerator yields the installed harnesses whose adapter can probe quota. The
// agent Service satisfies it; the prober re-enumerates on every run so newly
// installed harnesses appear without a restart.
type Enumerator interface {
	QuotaProbers(ctx context.Context) []ports.HarnessQuotaProber
}

// Store persists real usage snapshots. Only successful (ok) probes with usage
// windows write; failed/empty probes never touch durable state.
type Store interface {
	UpsertQuotaSnapshot(ctx context.Context, snap domain.QuotaSnapshot) (domain.QuotaSnapshot, error)
}

// Deps bundles the prober's collaborators and knobs. Zero-value Interval, Logger,
// and Now fall back to safe defaults.
type Deps struct {
	Enumerator Enumerator
	Store      Store
	Interval   time.Duration
	Logger     *slog.Logger
	Now        func() time.Time
}

// Prober schedules harness quota probes, enforces single-flight per harness,
// persists ok snapshots, and holds honest per-harness status in memory.
type Prober struct {
	enum     Enumerator
	store    Store
	interval time.Duration
	logger   *slog.Logger
	now      func() time.Time

	mu       sync.Mutex // guards statuses
	statuses map[domain.AgentHarness]domain.HarnessQuotaStatus

	flightMu sync.Mutex                          // guards flights map
	flights  map[domain.AgentHarness]*sync.Mutex // per-harness single-flight locks
}

// New constructs a Prober with defaults applied.
func New(deps Deps) *Prober {
	p := &Prober{
		enum:     deps.Enumerator,
		store:    deps.Store,
		interval: deps.Interval,
		logger:   deps.Logger,
		now:      deps.Now,
		statuses: make(map[domain.AgentHarness]domain.HarnessQuotaStatus),
		flights:  make(map[domain.AgentHarness]*sync.Mutex),
	}
	if p.interval <= 0 {
		p.interval = DefaultInterval
	}
	if p.logger == nil {
		p.logger = slog.Default()
	}
	if p.now == nil {
		p.now = time.Now
	}
	return p
}

// Start runs ProbeAll once immediately (inside the goroutine, so daemon startup
// is not blocked), then on every interval tick until ctx is done. The returned
// channel closes when the loop exits.
func (p *Prober) Start(ctx context.Context) <-chan struct{} {
	return observe.StartPollLoop(ctx, p.interval, func(ctx context.Context) error {
		p.ProbeAll(ctx)
		return nil
	}, p.logger, "quota prober")
}

// ProbeAll probes every currently enumerated harness. Distinct harnesses probe
// concurrently; each still takes its own single-flight lock, so a concurrent
// force-probe of one harness never doubles up.
func (p *Prober) ProbeAll(ctx context.Context) []domain.HarnessQuotaStatus {
	probers := p.enumerate(ctx)
	var wg sync.WaitGroup
	for _, hp := range probers {
		wg.Add(1)
		go func(hp ports.HarnessQuotaProber) {
			defer wg.Done()
			p.probe(ctx, hp)
		}(hp)
	}
	wg.Wait()
	return p.Statuses()
}

// ProbeHarness force-probes a single harness. It returns (_, false) when the
// harness is not among the current enumeration, so callers can 404 cleanly.
func (p *Prober) ProbeHarness(ctx context.Context, harness domain.AgentHarness) (domain.HarnessQuotaStatus, bool) {
	for _, hp := range p.enumerate(ctx) {
		if hp.Harness == harness {
			return p.probe(ctx, hp), true
		}
	}
	return domain.HarnessQuotaStatus{}, false
}

// Statuses returns a snapshot of the in-memory status map, sorted by harness so
// the wire output is deterministic.
func (p *Prober) Statuses() []domain.HarnessQuotaStatus {
	p.mu.Lock()
	out := make([]domain.HarnessQuotaStatus, 0, len(p.statuses))
	for _, s := range p.statuses {
		out = append(out, s)
	}
	p.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Harness < out[j].Harness })
	return out
}

func (p *Prober) enumerate(ctx context.Context) []ports.HarnessQuotaProber {
	if p.enum == nil {
		return nil
	}
	return p.enum.QuotaProbers(ctx)
}

// probe runs one harness probe under its single-flight lock, persists ok
// snapshots, records the resulting status, and returns it. A returned Go error
// (or a panic) is turned into a failed status rather than propagated, so a
// misbehaving adapter never wedges the prober.
func (p *Prober) probe(ctx context.Context, hp ports.HarnessQuotaProber) domain.HarnessQuotaStatus {
	lock := p.flightLock(hp.Harness)
	lock.Lock()
	defer lock.Unlock()

	now := p.now()
	result, err := p.callProbe(ctx, hp.Prober, now)
	if err != nil {
		return p.record(domain.HarnessQuotaStatus{
			Harness:  hp.Harness,
			State:    domain.QuotaProbeFailed,
			Reason:   err.Error(),
			ProbedAt: now,
		})
	}

	if result.State == domain.QuotaProbeOK {
		for _, s := range result.Snapshots {
			if p.store == nil {
				break
			}
			if _, uerr := p.store.UpsertQuotaSnapshot(ctx, s); uerr != nil {
				p.logger.Warn("quota prober: persist snapshot failed", "harness", hp.Harness, "err", uerr)
			}
		}
	}

	return p.record(domain.HarnessQuotaStatus{
		Harness:   hp.Harness,
		State:     result.State,
		Reason:    result.Reason,
		ProbedAt:  now,
		HasData:   len(result.Snapshots) > 0,
		Snapshots: result.Snapshots,
	})
}

// callProbe invokes the adapter, converting a panic into an error so a
// malformed local state can never take down the prober goroutine.
func (p *Prober) callProbe(ctx context.Context, prober ports.AgentQuotaProber, now time.Time) (result ports.QuotaProbeResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			result = ports.QuotaProbeResult{}
			err = errors.New("quota probe panicked")
		}
	}()
	return prober.ProbeQuota(ctx, now)
}

func (p *Prober) record(status domain.HarnessQuotaStatus) domain.HarnessQuotaStatus {
	p.mu.Lock()
	p.statuses[status.Harness] = status
	p.mu.Unlock()
	return status
}

func (p *Prober) flightLock(harness domain.AgentHarness) *sync.Mutex {
	p.flightMu.Lock()
	defer p.flightMu.Unlock()
	lock, ok := p.flights[harness]
	if !ok {
		lock = &sync.Mutex{}
		p.flights[harness] = lock
	}
	return lock
}
