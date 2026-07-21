// Package drain implements the fleet pause drain sweeper. It is deliberately
// separate from the reaper (internal/observe/reaper), which is fact-only by
// contract and never terminates a session. The sweeper actively terminates the
// drainable workers of paused projects through the clean session-teardown Kill
// path, so no zombie runtime or worktree is left behind. Soft pause = gate new
// work (intake + spawn guard) and let this sweeper drain workers as they reach
// an idle/terminal-complete state; mid-flight work is preserved.
package drain

import (
	"context"
	"log/slog"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/observe"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	sessionsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/session"
)

// DefaultTickInterval matches the reaper cadence: pause is not latency-sensitive,
// and a worker only becomes drainable after it goes idle, which the activity
// pipeline already debounces.
const DefaultTickInterval = 5 * time.Second

// Store is the narrow durable surface the sweeper reads: which projects exist
// and whether the fleet is paused.
type Store interface {
	ListProjects(ctx context.Context) ([]domain.ProjectRecord, error)
	GetFleetPaused(ctx context.Context) (bool, error)
}

// Sessions is the session-service surface the sweeper drives: list a project's
// sessions (with derived status) and terminate one through the clean teardown
// path.
type Sessions interface {
	List(ctx context.Context, filter sessionsvc.ListFilter) ([]domain.Session, error)
	Kill(ctx context.Context, id domain.SessionID) (bool, error)
}

// Config carries the sweeper's optional collaborators.
type Config struct {
	Telemetry ports.EventSink
	Logger    *slog.Logger
	Clock     func() time.Time
	Tick      time.Duration
}

// Sweeper terminates drainable workers of paused projects on a fixed tick.
type Sweeper struct {
	store     Store
	sessions  Sessions
	telemetry ports.EventSink
	logger    *slog.Logger
	clock     func() time.Time
	tick      time.Duration
	// hadLive tracks paused projects observed with at least one live worker, so
	// the drain-complete signal fires exactly once on the transition to zero.
	hadLive map[domain.ProjectID]bool
}

// New builds a sweeper with defaults for any unset collaborator.
func New(store Store, sessions Sessions, cfg Config) *Sweeper {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	clock := cfg.Clock
	if clock == nil {
		clock = time.Now
	}
	tick := cfg.Tick
	if tick <= 0 {
		tick = DefaultTickInterval
	}
	return &Sweeper{
		store:     store,
		sessions:  sessions,
		telemetry: cfg.Telemetry,
		logger:    logger,
		clock:     clock,
		tick:      tick,
		hadLive:   map[domain.ProjectID]bool{},
	}
}

// Start runs the sweep loop until ctx is cancelled and returns a channel closed
// when the goroutine has exited.
func (s *Sweeper) Start(ctx context.Context) <-chan struct{} {
	return observe.StartPollLoop(ctx, s.tick, s.Tick, s.logger, "fleet drain")
}

// Tick sweeps every project once: ungated projects are skipped (and their
// had-live marker cleared); gated projects have their drainable workers killed.
func (s *Sweeper) Tick(ctx context.Context) error {
	fleetPaused, err := s.store.GetFleetPaused(ctx)
	if err != nil {
		return err
	}
	projects, err := s.store.ListProjects(ctx)
	if err != nil {
		return err
	}
	for _, p := range projects {
		id := domain.ProjectID(p.ID)
		if !p.Paused && !fleetPaused {
			delete(s.hadLive, id)
			continue
		}
		if err := s.drainProject(ctx, id); err != nil {
			s.logger.Warn("fleet drain: project sweep failed", "project", id, "err", err)
		}
	}
	return nil
}

// drainProject terminates the drainable workers of one gated project. A worker
// that is still working (or whose Kill no-ops, e.g. a preserved dirty worktree)
// is counted live and retried next tick. When no live workers remain, the
// drain-complete signal fires once.
func (s *Sweeper) drainProject(ctx context.Context, id domain.ProjectID) error {
	sessions, err := s.sessions.List(ctx, sessionsvc.ListFilter{ProjectID: id})
	if err != nil {
		return err
	}
	live, drained := 0, 0
	for _, sess := range sessions {
		if sess.Kind != domain.KindWorker || sess.IsTerminated {
			continue
		}
		if !drainable(sess.Status) {
			live++
			continue
		}
		killed, err := s.sessions.Kill(ctx, sess.ID)
		if err != nil {
			live++ // transient: retry next tick
			continue
		}
		if killed {
			drained++
		} else {
			live++ // no-op kill (e.g. dirty worktree preserved) — still live
		}
	}
	if live > 0 {
		s.hadLive[id] = true
		return nil
	}
	if s.hadLive[id] || drained > 0 {
		s.emitDrainComplete(ctx, id, drained)
	}
	delete(s.hadLive, id)
	return nil
}

// drainable reports whether a worker in this status is safe to terminate under a
// soft pause: only idle and the terminal-complete "merged" state. Anything still
// working, awaiting a PR/review/input, or with no activity signal (a broken hook
// pipeline — indistinguishable from working) is left alone. Hard pause is the
// escape hatch for those.
func drainable(status domain.SessionStatus) bool {
	switch status {
	case domain.StatusIdle, domain.StatusMerged:
		return true
	default:
		return false
	}
}

func (s *Sweeper) emitDrainComplete(ctx context.Context, id domain.ProjectID, drained int) {
	s.logger.Info("fleet drain: project drained", "project", id, "drained", drained)
	if s.telemetry == nil {
		return
	}
	pid := id
	s.telemetry.Emit(ctx, ports.TelemetryEvent{
		Name:       "ao.fleet.drain_complete",
		Source:     "fleet_drain",
		OccurredAt: s.clock().UTC(),
		Level:      ports.TelemetryLevelInfo,
		ProjectID:  &pid,
		Payload:    map[string]any{"drained_workers": drained},
	})
}
