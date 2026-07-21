// Package agenthealth monitors configured agent harnesses for local
// installation and authentication readiness. It owns only a cached read model
// and transition callbacks; it never persists session-scoped notifications.
package agenthealth

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// Health is the resolved functional state of one harness.
type Health string

const (
	HealthHealthy      Health = "healthy"
	HealthMissing      Health = "missing"
	HealthUnauthorized Health = "unauthorized"
	HealthUnknown      Health = "unknown"
)

// Actionable reports whether the state warrants operator remediation.
func (h Health) Actionable() bool {
	return h == HealthMissing || h == HealthUnauthorized
}

// Probe is one harness's raw install/auth result from the agent catalog.
type Probe struct {
	ID         string
	Label      string
	Installed  bool
	AuthStatus ports.AgentAuthStatus
}

// Prober runs bounded local install/auth checks for the requested harnesses.
// A daemon wiring adapter can map the agent service's native probe shape onto
// this narrow contract without creating a service-package import cycle.
type Prober interface {
	HarnessHealth(ctx context.Context, ids []string) ([]Probe, error)
}

// HarnessLister returns the configured harness IDs for the current cycle.
type HarnessLister func(ctx context.Context) []string

// HarnessHealth is the cached read model for one harness.
type HarnessHealth struct {
	ID         string                `json:"id"`
	Label      string                `json:"label"`
	Health     Health                `json:"health"`
	AuthStatus ports.AgentAuthStatus `json:"authStatus,omitempty"`
	Reason     string                `json:"reason,omitempty"`
	Remedy     string                `json:"remedy,omitempty"`
	ChangedAt  time.Time             `json:"changedAt"`
	CheckedAt  time.Time             `json:"checkedAt"`
}

// Snapshot is the current cached health for all monitored harnesses.
type Snapshot struct {
	Harnesses []HarnessHealth `json:"harnesses"`
	CheckedAt time.Time       `json:"checkedAt"`
}

// Transition describes one actionable unhealthy or recovery transition.
type Transition struct {
	Prev    Health
	Current HarnessHealth
}

// Deps configures a Monitor.
type Deps struct {
	Prober       Prober
	Harnesses    HarnessLister
	Clock        func() time.Time
	Logger       *slog.Logger
	OnTransition func(Transition)
}

// Monitor holds the cached health read model. Snapshot reads and RunOnce calls
// are concurrency-safe.
type Monitor struct {
	prober    Prober
	harnesses HarnessLister
	clock     func() time.Time
	log       *slog.Logger
	onTrans   func(Transition)

	mu        sync.RWMutex
	state     map[string]HarnessHealth
	checkedAt time.Time
}

// New constructs an agent-health monitor.
func New(deps Deps) *Monitor {
	monitor := &Monitor{
		prober:    deps.Prober,
		harnesses: deps.Harnesses,
		clock:     deps.Clock,
		log:       deps.Logger,
		onTrans:   deps.OnTransition,
		state:     map[string]HarnessHealth{},
	}
	if monitor.clock == nil {
		monitor.clock = time.Now
	}
	if monitor.log == nil {
		monitor.log = slog.Default()
	}
	return monitor
}

// RunOnce performs one explicit health refresh. Configured IDs are trimmed,
// deduplicated, and sorted before probing. Probe errors retain the prior
// snapshot; an empty lister result is treated as a transient no-op.
func (m *Monitor) RunOnce(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.prober == nil {
		return nil
	}
	var ids []string
	if m.harnesses != nil {
		ids = normalizeHarnessIDs(m.harnesses(ctx))
	}
	if len(ids) == 0 {
		return nil
	}
	probes, err := m.prober.HarnessHealth(ctx, ids)
	if err != nil {
		return err
	}
	now := m.clock()

	m.mu.Lock()
	previous := m.state
	next := make(map[string]HarnessHealth, len(probes))
	changes := make([]Transition, 0, len(probes))
	for _, probe := range probes {
		current := resolve(probe, now)
		old, existed := previous[probe.ID]
		if existed && old.Health == current.Health {
			current.ChangedAt = old.ChangedAt
		} else {
			changes = append(changes, Transition{Prev: old.Health, Current: current})
		}
		next[probe.ID] = current
	}
	m.state = next
	m.checkedAt = now
	m.mu.Unlock()

	for _, transition := range changes {
		m.logTransition(transition)
		if m.onTrans != nil && actionableTransition(transition.Prev, transition.Current.Health) {
			m.onTrans(transition)
		}
	}
	return nil
}

// Check preserves the reference polling seam while RunOnce names the explicit
// read-side refresh operation used by tests and callers.
func (m *Monitor) Check(ctx context.Context) error {
	return m.RunOnce(ctx)
}

// Run performs an immediate refresh and then refreshes at interval until ctx
// is canceled. Non-positive intervals disable scheduling. Individual probe
// errors are logged and retried on the next tick rather than terminating the
// monitor.
func (m *Monitor) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		return nil
	}
	run := func() {
		if err := m.RunOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
			m.log.Error("agent health check failed", "err", err)
		}
	}
	run()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			run()
		}
	}
}

// Snapshot returns an immutable ID-sorted copy of the current read model.
func (m *Monitor) Snapshot() Snapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	harnesses := make([]HarnessHealth, 0, len(m.state))
	for _, health := range m.state {
		harnesses = append(harnesses, health)
	}
	sort.Slice(harnesses, func(i, j int) bool { return harnesses[i].ID < harnesses[j].ID })
	return Snapshot{Harnesses: harnesses, CheckedAt: m.checkedAt}
}

func normalizeHarnessIDs(ids []string) []string {
	set := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" {
			set[id] = struct{}{}
		}
	}
	normalized := make([]string, 0, len(set))
	for id := range set {
		normalized = append(normalized, id)
	}
	sort.Strings(normalized)
	return normalized
}

func actionableTransition(previous, current Health) bool {
	if current.Actionable() {
		return previous != current
	}
	return current == HealthHealthy && previous.Actionable()
}

func (m *Monitor) logTransition(transition Transition) {
	current := transition.Current
	switch {
	case current.Health.Actionable():
		m.log.Warn("agent harness unhealthy", "harness", current.ID, "health", current.Health, "reason", current.Reason, "remedy", current.Remedy, "prev", transition.Prev)
	case current.Health == HealthHealthy && transition.Prev.Actionable():
		m.log.Info("agent harness recovered", "harness", current.ID, "prev", transition.Prev)
	default:
		m.log.Debug("agent harness health advisory", "harness", current.ID, "health", current.Health, "prev", transition.Prev)
	}
}

func resolve(probe Probe, now time.Time) HarnessHealth {
	health := HarnessHealth{
		ID:         probe.ID,
		Label:      labelOr(probe.ID, probe.Label),
		AuthStatus: probe.AuthStatus,
		ChangedAt:  now,
		CheckedAt:  now,
	}
	switch {
	case !probe.Installed:
		health.Health = HealthMissing
		health.Reason = "binary not found on PATH"
		health.Remedy = "install the " + health.Label + " CLI and ensure it is on the daemon's PATH"
	case probe.AuthStatus == ports.AgentAuthStatusAuthorized:
		health.Health = HealthHealthy
	case probe.AuthStatus == ports.AgentAuthStatusUnauthorized:
		health.Health = HealthUnauthorized
		health.Reason = "not authenticated (login expired or logged out)"
		health.Remedy = loginRemedy(probe.ID, health.Label)
	default:
		health.Health = HealthUnknown
		health.Reason = "auth status could not be determined"
	}
	return health
}

func labelOr(id, label string) string {
	if strings.TrimSpace(label) != "" {
		return label
	}
	return id
}

func loginRemedy(id, label string) string {
	switch id {
	case "claude-code":
		return "run `claude` and sign in, or set ANTHROPIC_API_KEY"
	case "codex":
		return "run `codex login`"
	case "codex-fugu":
		return "run `codex-fugu login` (or `codex login` for the shared account)"
	case "copilot":
		return "run `gh auth login` / re-authenticate GitHub Copilot"
	default:
		return "re-run the " + label + " login/auth command"
	}
}
