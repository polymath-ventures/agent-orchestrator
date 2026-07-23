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

	// reconcileMu serializes reconcile so two overlapping calls (e.g. a scheduled
	// ProbeAll and a force probe-all from the API) can never interleave their
	// out-of-lock enumerations and commit a stale snapshot over a newer one. Each
	// reconcile's enumeration therefore reflects the state left by the previous.
	reconcileMu sync.Mutex

	mu       sync.Mutex // guards statuses + live
	statuses map[domain.AgentHarness]domain.HarnessQuotaStatus
	// live is the set of harnesses currently enumerated (installed + probe-capable).
	// A gated (scheduled) record() writes a status only for a live harness, so a
	// straggler probe whose harness was evicted mid-flight (uninstalled during a
	// concurrent probe) can never resurrect a stale chip.
	//
	// Residual (accepted): a force-probe recording concurrently with a reconcile
	// that evicts the same harness can leave a one-cycle-stale chip (shown or
	// hidden) when a harness binary changes install-state during an in-flight
	// probe. It always self-heals on the next reconcile. Closing it fully would
	// require holding this hot status lock across the slow enumeration probes,
	// which would block every /metrics read — disproportionate to a one-cycle
	// flicker in an operationally near-impossible window.
	live map[domain.AgentHarness]struct{}

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
		live:     make(map[domain.AgentHarness]struct{}),
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

// Start seeds not_probed statuses for the currently enumerated harnesses so the
// widget renders "not probed yet" immediately, then runs ProbeAll once (inside
// the goroutine, so daemon startup is not blocked) and on every interval tick
// until ctx is done. The returned channel closes when the loop exits.
func (p *Prober) Start(ctx context.Context) <-chan struct{} {
	p.reconcile(ctx)
	return observe.StartPollLoop(ctx, p.interval, func(ctx context.Context) error {
		p.ProbeAll(ctx)
		return nil
	}, p.logger, "quota prober")
}

// ProbeAll probes every currently enumerated harness. Distinct harnesses probe
// concurrently; each still takes its own single-flight lock, so a concurrent
// force-probe of one harness never doubles up. It first reconciles the status
// map with the current enumeration (seeding not_probed, evicting stragglers).
func (p *Prober) ProbeAll(ctx context.Context) []domain.HarnessQuotaStatus {
	probers := p.reconcile(ctx)
	var wg sync.WaitGroup
	for _, hp := range probers {
		wg.Add(1)
		go func(hp ports.HarnessQuotaProber) {
			defer wg.Done()
			p.probe(ctx, hp, true) // gated: record only if still enumerated
		}(hp)
	}
	wg.Wait()
	return p.Statuses()
}

// reconcile aligns the in-memory status map with the current harness
// enumeration and returns that enumeration. Each newly-enumerated harness with
// no status yet is seeded QuotaProbeNotProbed (zero ProbedAt) so the widget can
// render "not probed yet" immediately; any status whose harness is no longer
// enumerated (e.g. an uninstalled harness) is evicted so a stale chip never
// lingers. Existing statuses for still-present harnesses are left untouched.
func (p *Prober) reconcile(ctx context.Context) []ports.HarnessQuotaProber {
	// Serialize with any other reconcile so overlapping enumerations cannot commit
	// a stale set over a newer one.
	p.reconcileMu.Lock()
	defer p.reconcileMu.Unlock()

	probers := p.enumerate(ctx)
	present := make(map[domain.AgentHarness]struct{}, len(probers))
	for _, hp := range probers {
		present[hp.Harness] = struct{}{}
	}

	p.mu.Lock()
	// reconcile is the SOLE writer of live, so a gated record() reads a consistent
	// snapshot and no concurrent force-probe can clobber the set.
	p.live = present
	for h := range present {
		if _, ok := p.statuses[h]; !ok {
			p.statuses[h] = domain.HarnessQuotaStatus{Harness: h, State: domain.QuotaProbeNotProbed}
		}
	}
	for h := range p.statuses {
		if _, ok := present[h]; !ok {
			delete(p.statuses, h)
		}
	}
	p.mu.Unlock()

	return probers
}

// ProbeHarness force-probes a single harness. It returns (_, false) when the
// harness is not among the current enumeration, so callers can 404 cleanly.
func (p *Prober) ProbeHarness(ctx context.Context, harness domain.AgentHarness) (domain.HarnessQuotaStatus, bool) {
	for _, hp := range p.enumerate(ctx) {
		if hp.Harness == harness {
			// A force-probe is an explicit request for a harness that is enumerated
			// (installed + probe-capable) right now, so it records unconditionally
			// (gated=false). It deliberately does not touch the `live` set — keeping
			// reconcile the sole writer of `live` avoids clobbering races with a
			// concurrent reconcile. Worst case, a harness uninstalled during this
			// probe shows its result for one cycle before the next reconcile evicts
			// it, which is harmless.
			return p.probe(ctx, hp, false), true
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
// probe runs one harness probe under its single-flight lock, persists ok
// snapshots, records the resulting status, and returns it. When gated is true
// (scheduled ProbeAll), the status is recorded only if the harness is still
// enumerated, so a straggler can't resurrect an evicted chip; a force-probe
// passes gated=false to record its explicit result unconditionally.
func (p *Prober) probe(ctx context.Context, hp ports.HarnessQuotaProber, gated bool) domain.HarnessQuotaStatus {
	lock := p.flightLock(hp.Harness)
	if !lock.TryLock() {
		// A probe for this harness is already in flight. Probing costs a real
		// quota turn (e.g. claude -p /usage), so skip the duplicate and return
		// the current cached status rather than serializing behind and re-probing.
		return p.currentStatus(hp.Harness)
	}
	defer lock.Unlock()

	now := p.now()
	result, err := p.callProbe(ctx, hp.Prober, now)
	if err != nil {
		return p.record(domain.HarnessQuotaStatus{
			Harness:  hp.Harness,
			State:    domain.QuotaProbeFailed,
			Reason:   err.Error(),
			ProbedAt: now,
		}, gated)
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
	}, gated)
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

func (p *Prober) record(status domain.HarnessQuotaStatus, gated bool) domain.HarnessQuotaStatus {
	p.mu.Lock()
	// A gated (scheduled) probe stores only if the harness is still live, so a
	// straggler cannot resurrect a chip a concurrent reconcile just evicted. A
	// force-probe (gated=false) stores unconditionally — it is an explicit request.
	if !gated {
		p.statuses[status.Harness] = status
	} else if _, ok := p.live[status.Harness]; ok {
		p.statuses[status.Harness] = status
	}
	p.mu.Unlock()
	return status
}

// currentStatus returns the cached status for harness, or a not_probed status
// when none is recorded yet. It backs the single-flight skip in probe: a caller
// whose probe is skipped still gets an honest current status to return.
func (p *Prober) currentStatus(harness domain.AgentHarness) domain.HarnessQuotaStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	if s, ok := p.statuses[harness]; ok {
		return s
	}
	return domain.HarnessQuotaStatus{Harness: harness, State: domain.QuotaProbeNotProbed}
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
