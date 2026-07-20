package metrics

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/observe"
)

// DefaultTickInterval is the coarse cadence used when Config.Tick is zero.
const DefaultTickInterval = 30 * time.Second

// DefaultHistory is how many recent snapshots the observer retains for the
// /api/v1/metrics short-history payload when Config.History is zero.
const DefaultHistory = 20

// DefaultCostWindow is the rolling window used for token/cost aggregation when
// Config.CostWindow is zero.
const DefaultCostWindow = time.Hour

// CostAggregator sums token/cost telemetry over a rolling window ending now.
type CostAggregator interface {
	Aggregate(ctx context.Context, since time.Time) (Cost, error)
}

// QuotaCollector refreshes and returns quota snapshots.
type QuotaCollector interface {
	CollectQuota(ctx context.Context, observedAt time.Time) ([]domain.QuotaSnapshot, error)
}

// AlertSink receives one call per alert state transition (firing or cleared).
// It is best-effort: a slow or failing sink must never stall the tick.
type AlertSink interface {
	EmitAlert(ctx context.Context, t AlertTransition)
}

// AlertSinkFunc adapts a function to AlertSink.
type AlertSinkFunc func(ctx context.Context, t AlertTransition)

// EmitAlert calls f.
func (f AlertSinkFunc) EmitAlert(ctx context.Context, t AlertTransition) { f(ctx, t) }

// Config holds the observer's tunable knobs. Zero values fall back to defaults.
type Config struct {
	// Tick is the poll cadence. <=0 uses DefaultTickInterval.
	Tick time.Duration
	// History is the number of retained snapshots. <=0 uses DefaultHistory.
	History int
	// CostWindow is the rolling window for cost aggregation when Config.CostWindow is zero.
	CostWindow time.Duration
	// Thresholds configures alerting; a zero field disables that alert.
	Thresholds Thresholds
	// Clock supplies snapshot timestamps. nil means time.Now.
	Clock func() time.Time
	// Logger receives operational diagnostics. nil means slog.Default.
	Logger *slog.Logger
}

// Deps bundles the collectors the observer reads from. Nil collectors are
// treated as unavailable, so the observer degrades cleanly while the daemon
// continues to serve the endpoint.
type Deps struct {
	Cost   CostAggregator
	Quota  QuotaCollector
	Alerts AlertSink
}

// Observer polls usage/quota collectors, retains a bounded history, and emits
// low-quota alert transitions.
type Observer struct {
	deps       Deps
	tick       time.Duration
	maxHistory int
	costWindow time.Duration
	clock      func() time.Time
	logger     *slog.Logger

	evalMu sync.Mutex // guards eval; Tick may be driven from tests concurrently
	eval   *evaluator

	mu      sync.RWMutex
	history []Snapshot
}

// New constructs an Observer with safe defaults.
func New(deps Deps, cfg Config) *Observer {
	o := &Observer{
		deps:       deps,
		tick:       cfg.Tick,
		maxHistory: cfg.History,
		costWindow: cfg.CostWindow,
		clock:      cfg.Clock,
		logger:     cfg.Logger,
		eval:       newEvaluator(cfg.Thresholds),
	}
	if o.tick <= 0 {
		o.tick = DefaultTickInterval
	}
	if o.maxHistory <= 0 {
		o.maxHistory = DefaultHistory
	}
	if o.costWindow <= 0 {
		o.costWindow = DefaultCostWindow
	}
	if o.clock == nil {
		o.clock = time.Now
	}
	if o.logger == nil {
		o.logger = slog.Default()
	}
	return o
}

// Start launches the poll loop; the first poll runs immediately inside the
// goroutine so daemon startup is not blocked. The returned channel closes when
// the loop exits.
func (o *Observer) Start(ctx context.Context) <-chan struct{} {
	return observe.StartPollLoop(ctx, o.tick, o.pollErr, o.logger, "metrics observer")
}

func (o *Observer) pollErr(ctx context.Context) error {
	o.Tick(ctx)
	return nil
}

// Tick runs one observation cycle synchronously: collect, aggregate, evaluate
// thresholds, retain, and emit transitions. It returns the snapshot it produced.
func (o *Observer) Tick(ctx context.Context) Snapshot {
	now := o.clock().UTC()
	snap := Snapshot{
		CollectedAt: now,
		Cost: Cost{
			WindowSeconds: int64(o.costWindow / time.Second),
			ByProject:     []ProjectCost{},
			ByHarness:     []HarnessCost{},
		},
		Quotas: []domain.QuotaSnapshot{},
	}

	if o.deps.Cost != nil {
		if cost, err := o.deps.Cost.Aggregate(ctx, now.Add(-o.costWindow)); err != nil {
			o.logger.Warn("metrics observer: cost aggregate failed", "err", err)
		} else {
			cost.WindowSeconds = int64(o.costWindow / time.Second)
			snap.Cost = cost
		}
	}

	if o.deps.Quota != nil {
		if quotas, err := o.deps.Quota.CollectQuota(ctx, now); err != nil {
			o.logger.Warn("metrics observer: quota collect failed", "err", err)
		} else {
			snap.Quotas = quotas
		}
	}

	o.evalMu.Lock()
	alerts, transitions := o.eval.evaluate(snap)
	o.evalMu.Unlock()
	snap.Alerts = alerts

	o.retain(snap)

	for _, t := range transitions {
		if o.deps.Alerts != nil {
			o.deps.Alerts.EmitAlert(ctx, t)
		}
		o.logAlert(t)
	}
	return snap
}

func (o *Observer) logAlert(t AlertTransition) {
	if t.Firing {
		o.logger.Warn("metrics observer: alert firing", "kind", t.Alert.Kind, "subject", t.Alert.Subject, "value", t.Alert.Value, "threshold", t.Alert.Threshold)
		return
	}
	o.logger.Info("metrics observer: alert cleared", "kind", t.Alert.Kind, "subject", t.Alert.Subject)
}

func (o *Observer) retain(s Snapshot) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.history = append(o.history, s)
	if len(o.history) > o.maxHistory {
		o.history = o.history[len(o.history)-o.maxHistory:]
	}
}

// Latest returns the most recent snapshot and whether one has been produced.
func (o *Observer) Latest() (Snapshot, bool) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	if len(o.history) == 0 {
		return Snapshot{}, false
	}
	return o.history[len(o.history)-1], true
}

// History returns a copy of the retained snapshots, oldest-first.
func (o *Observer) History() []Snapshot {
	o.mu.RLock()
	defer o.mu.RUnlock()
	out := make([]Snapshot, len(o.history))
	copy(out, o.history)
	return out
}

// Snapshots returns the retained history and latest snapshot under a single
// read lock, so a tick landing between two separate calls cannot yield a latest
// newer than the last history element.
func (o *Observer) Snapshots() (history []Snapshot, latest Snapshot, hasLatest bool) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	history = make([]Snapshot, len(o.history))
	copy(history, o.history)
	if len(o.history) == 0 {
		return history, Snapshot{}, false
	}
	return history, o.history[len(o.history)-1], true
}
