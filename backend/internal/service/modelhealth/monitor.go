// Package modelhealth revalidates configured model pins through the shared
// agent validator and emits typed read-side transition intents. It owns no
// notification persistence or delivery channel.
package modelhealth

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	agentsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/agent"
)

// Pin identifies one exact configured model selection and its source scope.
type Pin struct {
	ProjectID domain.ProjectID    `json:"projectId"`
	Scope     string              `json:"scope"`
	Harness   domain.AgentHarness `json:"harness"`
	Model     string              `json:"model"`
}

// Key returns the stable identity used for transition tracking.
func (p Pin) Key() string {
	return strings.TrimSpace(string(p.ProjectID)) + "|" + strings.TrimSpace(p.Scope) + "|" + strings.TrimSpace(string(p.Harness)) + "|" + strings.TrimSpace(p.Model)
}

// PinLister returns the complete configured pin set for one cycle.
type PinLister func(ctx context.Context) ([]Pin, error)

// Validator is the shared authoritative model-validation seam implemented by
// the agent service. Each call owns its independent timeout and three-way
// reachable/unreachable/probe-unavailable classification.
type Validator interface {
	ValidateModel(ctx context.Context, harness domain.AgentHarness, model string) (ports.ModelValidationResult, error)
}

// Verdict is the latest retained real or advisory state for one configured
// pin. Probe-unavailable maps to Status=unknown with a typed reason code.
type Verdict struct {
	Pin        Pin                      `json:"pin"`
	Status     agentsvc.ModelStatus     `json:"status"`
	Reason     string                   `json:"reason,omitempty"`
	ReasonCode agentsvc.ModelReasonCode `json:"reasonCode,omitempty"`
	CheckedAt  time.Time                `json:"checkedAt"`
}

// Snapshot is the cached key-sorted monitor read model.
type Snapshot struct {
	Verdicts  []Verdict `json:"verdicts"`
	CheckedAt time.Time `json:"checkedAt"`
}

// IntentKind identifies a transition that downstream operator-notification
// wiring may consume without coupling this monitor to any delivery channel.
type IntentKind string

const (
	// IntentUnreachable reports a configured model's definitive rejection.
	IntentUnreachable IntentKind = "unreachable"
	// IntentRecovered reports recovery from a previous definitive rejection.
	IntentRecovered IntentKind = "recovered"
)

// Intent is one typed actionable model transition.
type Intent struct {
	Kind       IntentKind           `json:"kind"`
	Pin        Pin                  `json:"pin"`
	Previous   agentsvc.ModelStatus `json:"previous,omitempty"`
	Current    agentsvc.ModelStatus `json:"current"`
	Reason     string               `json:"reason,omitempty"`
	OccurredAt time.Time            `json:"occurredAt"`
}

// Deps configures a Monitor.
type Deps struct {
	Pins      PinLister
	Validator Validator
	Clock     func() time.Time
	Logger    *slog.Logger
	OnIntent  func(Intent)
}

// Monitor stores the latest verdict per configured pin. It is safe for
// concurrent RunOnce and Snapshot calls.
type Monitor struct {
	pins      PinLister
	validator Validator
	clock     func() time.Time
	log       *slog.Logger
	onIntent  func(Intent)

	runMu     sync.Mutex
	mu        sync.RWMutex
	state     map[string]Verdict
	checkedAt time.Time
}

// New constructs a model-health monitor.
func New(deps Deps) *Monitor {
	monitor := &Monitor{
		pins:      deps.Pins,
		validator: deps.Validator,
		clock:     deps.Clock,
		log:       deps.Logger,
		onIntent:  deps.OnIntent,
		state:     map[string]Verdict{},
	}
	if monitor.clock == nil {
		monitor.clock = time.Now
	}
	if monitor.log == nil {
		monitor.log = slog.Default()
	}
	return monitor
}

// RunOnce performs one authoritative revalidation cycle. A lister or context
// error retains the prior snapshot. Uncertain probe results are cached for a
// first observation but never overwrite an existing reachable/unreachable
// verdict, preventing false unhealthy or recovery intents.
func (m *Monitor) RunOnce(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.runMu.Lock()
	defer m.runMu.Unlock()
	var listed []Pin
	if m.pins != nil {
		var err error
		listed, err = m.pins(ctx)
		if err != nil {
			return err
		}
	}
	pins := normalizePins(listed)
	if len(pins) == 0 {
		m.mu.Lock()
		clear(m.state)
		m.checkedAt = m.clock()
		m.mu.Unlock()
		return nil
	}

	results := make(map[string]validationOutcome, len(pins))
	for _, pin := range pins {
		if err := ctx.Err(); err != nil {
			return err
		}
		targetKey := validationTargetKey(pin.Harness, pin.Model)
		if _, exists := results[targetKey]; exists {
			continue
		}
		if m.validator == nil {
			results[targetKey] = validationOutcome{
				status:     agentsvc.ModelStatusUnknown,
				reason:     "model validator unavailable",
				reasonCode: agentsvc.ModelReasonProbeUnavailable,
			}
			continue
		}
		result, err := m.validator.ValidateModel(ctx, pin.Harness, pin.Model)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		results[targetKey] = normalizeValidationResult(result, err)
	}

	now := m.clock()
	configured := make(map[string]struct{}, len(pins))
	for _, pin := range pins {
		configured[pin.Key()] = struct{}{}
	}
	intents := make([]Intent, 0)

	m.mu.Lock()
	for key := range m.state {
		if _, exists := configured[key]; !exists {
			delete(m.state, key)
		}
	}
	for _, pin := range pins {
		result := results[validationTargetKey(pin.Harness, pin.Model)]
		incoming := Verdict{
			Pin:        pin,
			Status:     result.status,
			Reason:     result.reason,
			ReasonCode: result.reasonCode,
			CheckedAt:  now,
		}
		key := pin.Key()
		previous, existed := m.state[key]
		if incoming.Status == agentsvc.ModelStatusUnknown && existed && definitive(previous.Status) {
			continue
		}
		if incoming.Status == agentsvc.ModelStatusUnreachable && (!existed || previous.Status != agentsvc.ModelStatusUnreachable) {
			intents = append(intents, Intent{
				Kind:       IntentUnreachable,
				Pin:        pin,
				Previous:   previous.Status,
				Current:    incoming.Status,
				Reason:     incoming.Reason,
				OccurredAt: now,
			})
		} else if incoming.Status == agentsvc.ModelStatusReachable && existed && previous.Status == agentsvc.ModelStatusUnreachable {
			intents = append(intents, Intent{
				Kind:       IntentRecovered,
				Pin:        pin,
				Previous:   previous.Status,
				Current:    incoming.Status,
				OccurredAt: now,
			})
		}
		m.state[key] = incoming
	}
	m.checkedAt = now
	m.mu.Unlock()

	for _, intent := range intents {
		m.logIntent(intent)
		if m.onIntent != nil {
			m.onIntent(intent)
		}
	}
	return nil
}

// Check retains the conventional polling seam while RunOnce names explicit
// refreshes directly.
func (m *Monitor) Check(ctx context.Context) error {
	return m.RunOnce(ctx)
}

// Run performs an immediate cycle and then repeats at interval until canceled.
// A non-positive interval disables scheduling. Cycle errors are logged and
// retried on the next tick without discarding the previous snapshot.
func (m *Monitor) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		return nil
	}
	run := func() {
		if err := m.RunOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
			m.log.Error("model health check failed", "err", err)
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

// Snapshot returns a key-sorted copy of the cached read model.
func (m *Monitor) Snapshot() Snapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	verdicts := make([]Verdict, 0, len(m.state))
	for _, verdict := range m.state {
		verdicts = append(verdicts, verdict)
	}
	sort.Slice(verdicts, func(i, j int) bool { return verdicts[i].Pin.Key() < verdicts[j].Pin.Key() })
	return Snapshot{Verdicts: verdicts, CheckedAt: m.checkedAt}
}

func normalizePins(pins []Pin) []Pin {
	byKey := make(map[string]Pin, len(pins))
	for _, pin := range pins {
		pin.ProjectID = domain.ProjectID(strings.TrimSpace(string(pin.ProjectID)))
		pin.Scope = strings.TrimSpace(pin.Scope)
		pin.Harness = domain.AgentHarness(strings.TrimSpace(string(pin.Harness)))
		pin.Model = strings.TrimSpace(pin.Model)
		if pin.ProjectID == "" || pin.Harness == "" || pin.Model == "" {
			continue
		}
		byKey[pin.Key()] = pin
	}
	normalized := make([]Pin, 0, len(byKey))
	for _, pin := range byKey {
		normalized = append(normalized, pin)
	}
	sort.Slice(normalized, func(i, j int) bool { return normalized[i].Key() < normalized[j].Key() })
	return normalized
}

func validationTargetKey(harness domain.AgentHarness, model string) string {
	return string(harness) + "|" + strings.TrimSpace(model)
}

type validationOutcome struct {
	status     agentsvc.ModelStatus
	reason     string
	reasonCode agentsvc.ModelReasonCode
}

func normalizeValidationResult(result ports.ModelValidationResult, err error) validationOutcome {
	if err != nil {
		return validationOutcome{agentsvc.ModelStatusUnknown, err.Error(), agentsvc.ModelReasonProbeUnavailable}
	}
	switch result.Status {
	case ports.ModelValidationReachable:
		return validationOutcome{status: agentsvc.ModelStatusReachable, reason: result.Message}
	case ports.ModelValidationUnreachable:
		return validationOutcome{status: agentsvc.ModelStatusUnreachable, reason: result.Message}
	default:
		reason := strings.TrimSpace(result.Message)
		if reason == "" {
			reason = "model probe returned no definitive verdict"
		}
		return validationOutcome{agentsvc.ModelStatusUnknown, reason, agentsvc.ModelReasonProbeUnavailable}
	}
}

func definitive(status agentsvc.ModelStatus) bool {
	return status == agentsvc.ModelStatusReachable || status == agentsvc.ModelStatusUnreachable
}

func (m *Monitor) logIntent(intent Intent) {
	if intent.Kind == IntentRecovered {
		m.log.Info("configured model recovered", "project", intent.Pin.ProjectID, "scope", intent.Pin.Scope, "harness", intent.Pin.Harness, "model", intent.Pin.Model)
		return
	}
	m.log.Warn("configured model unreachable", "project", intent.Pin.ProjectID, "scope", intent.Pin.Scope, "harness", intent.Pin.Harness, "model", intent.Pin.Model, "reason", intent.Reason)
}
