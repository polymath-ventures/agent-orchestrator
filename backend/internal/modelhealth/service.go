// Package modelhealth owns cached, advisory model availability.
package modelhealth

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/config"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// DefaultRefreshInterval is the daemon's background model-health probe cadence.
const DefaultRefreshInterval = 30 * time.Minute

// Store is the durable persistence surface required by model health.
type Store interface {
	ListProjects(ctx context.Context) ([]domain.ProjectRecord, error)
	GetProject(ctx context.Context, id string) (domain.ProjectRecord, bool, error)
	ListModelHealthByProject(ctx context.Context, projectID domain.ProjectID) ([]domain.ModelAvailability, error)
	UpsertModelHealth(ctx context.Context, rec domain.ModelAvailability) (domain.ModelAvailability, error)
}

// AgentEntry pairs a harness with its adapter for capability lookup.
type AgentEntry struct {
	Harness domain.AgentHarness
	Agent   ports.Agent
}

type notifier interface {
	Notify(context.Context, ports.NotificationIntent) error
}

// Service lists cached availability and refreshes it from optional adapter
// probes. Refresh is advisory; spawn remains the authoritative launch path.
type Service struct {
	store          Store
	agents         map[domain.AgentHarness]ports.Agent
	notifier       notifier
	defaultHarness domain.AgentHarness
	clock          func() time.Time
	logger         *slog.Logger
}

// Deps wires the model-health service.
type Deps struct {
	Store          Store
	Agents         []AgentEntry
	Notifier       notifier
	DefaultHarness domain.AgentHarness
	Clock          func() time.Time
	Logger         *slog.Logger
}

// New constructs a model-health service from its dependencies.
func New(d Deps) *Service {
	defaultHarness := d.DefaultHarness
	if defaultHarness == "" {
		defaultHarness = domain.AgentHarness(config.DefaultAgent)
	}
	s := &Service{
		store:          d.Store,
		agents:         map[domain.AgentHarness]ports.Agent{},
		notifier:       d.Notifier,
		defaultHarness: defaultHarness,
		clock:          d.Clock,
		logger:         d.Logger,
	}
	if s.clock == nil {
		s.clock = time.Now
	}
	if s.logger == nil {
		s.logger = slog.Default()
	}
	for _, a := range d.Agents {
		if a.Harness != "" && a.Agent != nil {
			s.agents[a.Harness] = a.Agent
		}
	}
	return s
}

// ListProject returns one availability row for each configured model pin. It
// does not run live probes.
func (s *Service) ListProject(ctx context.Context, projectID domain.ProjectID) ([]domain.ModelAvailability, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("modelhealth: store is required")
	}
	row, ok, err := s.store.GetProject(ctx, string(projectID))
	if err != nil {
		return nil, err
	}
	if !ok || !row.ArchivedAt.IsZero() {
		return nil, nil
	}
	return s.listForProjectRecord(ctx, row)
}

func (s *Service) listForProjectRecord(ctx context.Context, row domain.ProjectRecord) ([]domain.ModelAvailability, error) {
	pins := configuredPins(row.Config, s.defaultHarness)
	cachedRows, err := s.store.ListModelHealthByProject(ctx, domain.ProjectID(row.ID))
	if err != nil {
		return nil, err
	}
	cached := map[domain.ModelAvailabilityKey]domain.ModelAvailability{}
	for _, rec := range cachedRows {
		cached[rec.Key()] = rec
	}
	out := make([]domain.ModelAvailability, 0, len(pins))
	for _, pin := range pins {
		key := domain.ModelAvailabilityKey{ProjectID: domain.ProjectID(row.ID), Harness: pin.Harness, Model: pin.Model}
		rec, ok := cached[key]
		if !ok {
			rec = domain.ModelAvailability{
				ProjectID: domain.ProjectID(row.ID),
				Harness:   pin.Harness,
				Model:     pin.Model,
				Status:    domain.ModelAvailabilityUnknown,
				Reason:    domain.ModelAvailabilityReasonNotProbed,
			}
		}
		if _, ok := s.validator(pin.Harness); !ok {
			rec.Status = domain.ModelAvailabilityUnknown
			rec.Reason = domain.ModelAvailabilityReasonNoCapability
			rec.Message = ""
		}
		out = append(out, rec)
	}
	return out, nil
}

// RefreshAll probes every configured model pin and updates the cache.
func (s *Service) RefreshAll(ctx context.Context) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("modelhealth: store is required")
	}
	projects, err := s.store.ListProjects(ctx)
	if err != nil {
		return err
	}
	for _, row := range projects {
		if err := ctx.Err(); err != nil {
			return err
		}
		if row.ArchivedAt.IsZero() {
			if err := s.RefreshProject(ctx, row); err != nil {
				s.logger.Warn("model health refresh failed", "project", row.ID, "err", err)
			}
		}
	}
	return nil
}

// RefreshProject probes every configured pin for one project record.
func (s *Service) RefreshProject(ctx context.Context, row domain.ProjectRecord) error {
	cachedRows, err := s.store.ListModelHealthByProject(ctx, domain.ProjectID(row.ID))
	if err != nil {
		return err
	}
	cached := map[domain.ModelAvailabilityKey]domain.ModelAvailability{}
	for _, rec := range cachedRows {
		cached[rec.Key()] = rec
	}
	for _, pin := range configuredPins(row.Config, s.defaultHarness) {
		if err := ctx.Err(); err != nil {
			return err
		}
		previous := cached[domain.ModelAvailabilityKey{ProjectID: domain.ProjectID(row.ID), Harness: pin.Harness, Model: pin.Model}]
		next := s.probe(ctx, domain.ProjectID(row.ID), pin)
		if previous.Status == domain.ModelAvailabilityUnreachable && next.Status == domain.ModelAvailabilityReachable {
			next.Reason = domain.ModelAvailabilityReasonRecovered
		}
		stored, err := s.store.UpsertModelHealth(ctx, next)
		if err != nil {
			return err
		}
		s.notifyTransition(ctx, previous, stored)
	}
	return nil
}

func (s *Service) probe(ctx context.Context, projectID domain.ProjectID, pin modelPin) domain.ModelAvailability {
	now := s.clock().UTC()
	rec := domain.ModelAvailability{
		ProjectID:  projectID,
		Harness:    pin.Harness,
		Model:      pin.Model,
		Status:     domain.ModelAvailabilityUnknown,
		Reason:     domain.ModelAvailabilityReasonNoCapability,
		ObservedAt: now,
		UpdatedAt:  now,
	}
	validator, ok := s.validator(pin.Harness)
	if !ok {
		return rec
	}
	result, err := validator.ValidateModel(ctx, pin.Model)
	if err != nil {
		rec.Reason = domain.ModelAvailabilityReasonProbeUnavailable
		rec.Message = err.Error()
		return rec
	}
	rec.Message = strings.TrimSpace(result.Message)
	switch result.Status {
	case ports.ModelValidationReachable:
		rec.Status = domain.ModelAvailabilityReachable
		rec.Reason = domain.ModelAvailabilityReasonReachable
	case ports.ModelValidationUnreachable:
		rec.Status = domain.ModelAvailabilityUnreachable
		rec.Reason = domain.ModelAvailabilityReasonUnreachable
	default:
		rec.Status = domain.ModelAvailabilityUnknown
		rec.Reason = domain.ModelAvailabilityReasonProbeUnavailable
	}
	return rec
}

func (s *Service) validator(harness domain.AgentHarness) (ports.AgentModelValidator, bool) {
	agent, ok := s.agents[harness]
	if !ok {
		return nil, false
	}
	validator, ok := agent.(ports.AgentModelValidator)
	return validator, ok
}

func (s *Service) notifyTransition(ctx context.Context, previous, next domain.ModelAvailability) {
	if s.notifier == nil || previous.Status == "" || previous.Status == next.Status {
		return
	}
	var typ domain.NotificationType
	switch {
	case previous.Status == domain.ModelAvailabilityReachable && next.Status == domain.ModelAvailabilityUnreachable:
		typ = domain.NotificationModelUnreachable
	case previous.Status == domain.ModelAvailabilityUnreachable && next.Status == domain.ModelAvailabilityReachable:
		typ = domain.NotificationModelRecovered
	default:
		return
	}
	_ = s.notifier.Notify(ctx, ports.NotificationIntent{
		Type:      typ,
		ProjectID: next.ProjectID,
		DedupeKey: fmt.Sprintf("model-health:%s:%s:%s:%s", next.ProjectID, next.Harness, next.Model, typ),
		Message:   fmt.Sprintf("%s/%s: %s", next.Harness, next.Model, firstNonEmpty(next.Message, string(next.Reason))),
	})
}

// Start runs periodic background revalidation until ctx is cancelled.
func (s *Service) Start(ctx context.Context, interval time.Duration) <-chan struct{} {
	done := make(chan struct{})
	if interval <= 0 {
		interval = DefaultRefreshInterval
	}
	go func() {
		defer close(done)
		if err := s.RefreshAll(ctx); err != nil && ctx.Err() == nil {
			s.logger.Warn("initial model health refresh failed", "err", err)
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.RefreshAll(ctx); err != nil && ctx.Err() == nil {
					s.logger.Warn("model health refresh failed", "err", err)
				}
			}
		}
	}()
	return done
}

type modelPin struct {
	Harness domain.AgentHarness
	Model   string
}

func configuredPins(cfg domain.ProjectConfig, defaultHarness domain.AgentHarness) []modelPin {
	seen := map[modelPin]struct{}{}
	add := func(h domain.AgentHarness, model string) {
		model = strings.TrimSpace(model)
		if h == "" || model == "" {
			return
		}
		seen[modelPin{Harness: h, Model: model}] = struct{}{}
	}
	roleHarnesses := []domain.AgentHarness{defaultHarness}
	for _, h := range []domain.AgentHarness{cfg.Worker.Harness, cfg.Orchestrator.Harness} {
		if h != "" {
			roleHarnesses = append(roleHarnesses, h)
		}
	}
	for _, h := range roleHarnesses {
		add(h, cfg.AgentConfig.Model)
	}
	addFromHarnessMap(seen, cfg.AgentConfig.ModelByHarness)
	addRolePins(seen, cfg.Worker, defaultHarness)
	addRolePins(seen, cfg.Orchestrator, defaultHarness)
	for _, bucket := range cfg.WorkerMix {
		add(bucket.Harness, bucket.Model)
	}
	for _, reviewer := range cfg.Reviewers {
		h := reviewer.Harness.AgentHarness()
		add(h, reviewer.AgentConfig.Model)
		addFromHarnessMap(seen, reviewer.AgentConfig.ModelByHarness)
	}
	out := make([]modelPin, 0, len(seen))
	for pin := range seen {
		out = append(out, pin)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Harness == out[j].Harness {
			return out[i].Model < out[j].Model
		}
		return out[i].Harness < out[j].Harness
	})
	return out
}

func addRolePins(seen map[modelPin]struct{}, role domain.RoleOverride, defaultHarness domain.AgentHarness) {
	h := role.Harness
	if h == "" {
		h = defaultHarness
	}
	if model := strings.TrimSpace(role.AgentConfig.Model); model != "" && h != "" {
		seen[modelPin{Harness: h, Model: model}] = struct{}{}
	}
	addFromHarnessMap(seen, role.AgentConfig.ModelByHarness)
}

func addFromHarnessMap(seen map[modelPin]struct{}, byHarness map[domain.AgentHarness]domain.HarnessModel) {
	for h, hm := range byHarness {
		if model := strings.TrimSpace(hm.Model); model != "" && h != "" {
			seen[modelPin{Harness: h, Model: model}] = struct{}{}
		}
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
