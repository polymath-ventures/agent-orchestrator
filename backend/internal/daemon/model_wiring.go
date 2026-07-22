package daemon

import (
	"context"
	"log/slog"
	"sort"
	"time"

	agentregistry "github.com/aoagents/agent-orchestrator/backend/internal/adapters/agent/registry"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	agentsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/agent"
	modelhealthsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/modelhealth"
)

type modelHealthSnapshotProvider interface {
	Snapshot() modelhealthsvc.Snapshot
}

func newModelHealthMonitor(validator modelhealthsvc.Validator, configured *configuredProjectModels, logger *slog.Logger) *modelhealthsvc.Monitor {
	if logger == nil {
		logger = slog.Default()
	}
	return modelhealthsvc.New(modelhealthsvc.Deps{
		Pins:      configured.ListModelHealthPins,
		Validator: validator,
		Logger:    logger,
		OnIntent: func(intent modelhealthsvc.Intent) {
			level := slog.LevelWarn
			if intent.Kind == modelhealthsvc.IntentRecovered {
				level = slog.LevelInfo
			}
			logger.Log(context.Background(), level, "configured model health intent",
				"kind", intent.Kind,
				"project", intent.Pin.ProjectID,
				"scope", intent.Pin.Scope,
				"harness", intent.Pin.Harness,
				"model", intent.Pin.Model,
				"previous", intent.Previous,
				"current", intent.Current,
				"reason", intent.Reason,
			)
		},
	})
}

// modelHealthProjection preserves the project detail read contract while the
// authoritative verdict cache lives in service/modelhealth rather than the
// legacy SQLite-backed refresh service.
type modelHealthProjection struct {
	monitor            modelHealthSnapshotProvider
	pins               modelhealthsvc.PinLister
	supportsValidation func(domain.AgentHarness) bool
}

func newModelHealthProjection(monitor modelHealthSnapshotProvider, pins modelhealthsvc.PinLister) *modelHealthProjection {
	return &modelHealthProjection{
		monitor:            monitor,
		pins:               pins,
		supportsValidation: registeredHarnessSupportsModelValidation,
	}
}

func (p modelHealthProjection) ListProject(ctx context.Context, projectID domain.ProjectID) ([]domain.ModelAvailability, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	pins, err := p.pins(ctx)
	if err != nil {
		return nil, err
	}
	snapshot := p.monitor.Snapshot()
	verdicts := make(map[string]modelhealthsvc.Verdict, len(snapshot.Verdicts))
	for _, verdict := range snapshot.Verdicts {
		verdicts[verdict.Pin.Key()] = verdict
	}
	rows := make([]domain.ModelAvailability, 0)
	for _, pin := range pins {
		if pin.ProjectID != projectID {
			continue
		}
		row := domain.ModelAvailability{
			ProjectID: projectID,
			Harness:   pin.Harness,
			Model:     pin.Model,
			Status:    domain.ModelAvailabilityUnknown,
			Reason:    domain.ModelAvailabilityReasonNotProbed,
		}
		if verdict, ok := verdicts[pin.Key()]; ok {
			row.Message = verdict.Reason
			row.ObservedAt = verdict.CheckedAt
			row.UpdatedAt = verdict.CheckedAt
			switch verdict.Status {
			case agentsvc.ModelStatusReachable:
				row.Status = domain.ModelAvailabilityReachable
				row.Reason = domain.ModelAvailabilityReasonReachable
			case agentsvc.ModelStatusUnreachable:
				row.Status = domain.ModelAvailabilityUnreachable
				row.Reason = domain.ModelAvailabilityReasonUnreachable
			default:
				row.Reason = domain.ModelAvailabilityReasonProbeUnavailable
			}
		}
		if p.supportsValidation != nil && !p.supportsValidation(pin.Harness) {
			row.Status = domain.ModelAvailabilityUnknown
			row.Reason = domain.ModelAvailabilityReasonNoCapability
			row.Message = ""
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Harness != rows[j].Harness {
			return rows[i].Harness < rows[j].Harness
		}
		return rows[i].Model < rows[j].Model
	})
	return rows, nil
}

func registeredHarnessSupportsModelValidation(harness domain.AgentHarness) bool {
	for _, item := range agentregistry.Harnessed() {
		if item.Harness == harness {
			_, ok := item.Agent.(ports.AgentModelValidator)
			return ok
		}
	}
	return false
}

type modelHealthRunner interface {
	Run(context.Context, time.Duration) error
}

func startModelHealthMonitor(ctx context.Context, monitor modelHealthRunner, interval time.Duration, logger *slog.Logger) <-chan struct{} {
	if logger == nil {
		logger = slog.Default()
	}
	if interval <= 0 {
		logger.Info("model health monitor disabled (AO_MODEL_REVALIDATION_INTERVAL=0)")
		return closedDone()
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := monitor.Run(ctx, interval); err != nil && ctx.Err() == nil {
			logger.Error("model health monitor stopped", "err", err)
		}
	}()
	return done
}
