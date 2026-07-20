package daemon

import (
	"context"
	"log/slog"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/config"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/controllers"
	"github.com/aoagents/agent-orchestrator/backend/internal/observe/metrics"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

type notificationIntentSink interface {
	Notify(context.Context, ports.NotificationIntent) error
}

// startMetricsObserver wires the usage/quota metrics observer behind config. A zero
// interval disables it entirely: the returned observer is nil (so the API mounts
// the endpoint as not-implemented) and the done channel is already closed.
//
// Alert transitions are emitted through the telemetry EventSink as
// `metrics_alert` events (level warn on firing, info on clear). The evaluator
// only surfaces transitions, so this is deduped on state change, never per tick.
// Low-quota firing transitions also produce a durable notification intent.
func startMetricsObserver(ctx context.Context, cfg config.Config, store *sqlite.Store, telemetry ports.EventSink, notifier notificationIntentSink, logger *slog.Logger) (*metrics.Observer, <-chan struct{}) {
	if cfg.Metrics.Interval <= 0 {
		logger.Info("metrics observer disabled (AO_METRICS_INTERVAL=0)")
		closed := make(chan struct{})
		close(closed)
		return nil, closed
	}

	obs := metrics.New(metrics.Deps{
		Cost:   metrics.NewStoreCostAggregator(store),
		Quota:  metrics.NewStoreQuotaCollector(store),
		Alerts: metricsAlertSink{telemetry: telemetry, notifications: notifier},
	}, metrics.Config{
		Tick: cfg.Metrics.Interval,
		Thresholds: metrics.Thresholds{
			LowQuotaPercent: cfg.Metrics.LowQuotaPercent,
		},
		Logger: logger,
	})
	done := obs.Start(ctx)
	return obs, done
}

// metricsProvider adapts the observer to the controller read interface, mapping
// a disabled (nil) observer to a nil interface so the endpoint reports
// not-implemented instead of wrapping a typed-nil pointer.
func metricsProvider(obs *metrics.Observer) controllers.MetricsProvider {
	if obs == nil {
		return nil
	}
	return obs
}

// metricsAlertSink forwards metrics alert transitions onto the daemon's
// telemetry event bus as `metrics_alert` events and turns low-quota firing
// transitions into durable notification intents.
type metricsAlertSink struct {
	telemetry     ports.EventSink
	notifications notificationIntentSink
}

// EmitAlert publishes one alert transition as a structured telemetry event.
func (s metricsAlertSink) EmitAlert(ctx context.Context, t metrics.AlertTransition) {
	level := ports.TelemetryLevelWarn
	state := "firing"
	if !t.Firing {
		level = ports.TelemetryLevelInfo
		state = "cleared"
	}
	if s.telemetry != nil {
		s.telemetry.Emit(ctx, ports.TelemetryEvent{
			Name:       "metrics_alert",
			Source:     "metrics",
			OccurredAt: time.Now().UTC(),
			Level:      level,
			Payload: map[string]any{
				"kind":      string(t.Alert.Kind),
				"state":     state,
				"severity":  string(t.Alert.Severity),
				"subject":   t.Alert.Subject,
				"value":     t.Alert.Value,
				"threshold": t.Alert.Threshold,
				"message":   t.Alert.Message,
			},
		})
	}
	if t.Firing && t.Alert.Kind == metrics.AlertQuotaLow && s.notifications != nil {
		if err := s.notifications.Notify(ctx, ports.NotificationIntent{
			Type:      domain.NotificationLowQuota,
			DedupeKey: "metrics:" + string(t.Alert.Kind) + ":" + t.Alert.Subject,
			Message:   t.Alert.Message,
			CreatedAt: time.Now().UTC(),
		}); err != nil {
			logger := slog.Default()
			logger.Warn("metrics: low quota notification failed", "subject", t.Alert.Subject, "err", err)
		}
	}
}
