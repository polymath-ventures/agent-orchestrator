package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/config"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

const (
	defaultPrimeSupervisorInterval = 30 * time.Second
	defaultPrimeUnhealthyAfter     = 5 * time.Minute
	defaultPrimeRestartWindow      = time.Hour
	defaultPrimeRestartLimit       = 3
	defaultPrimeRestartBackoff     = 30 * time.Second
	defaultPrimeRestartMaxBackoff  = 15 * time.Minute
	defaultPrimeIdleWakeAfter      = 15 * time.Minute
	defaultPrimeIdleWakeBackoff    = 5 * time.Minute
	defaultPrimeIdleWakeMaxBackoff = 30 * time.Minute
)

type primeSessionService interface {
	SpawnPrime(ctx context.Context, projectID domain.ProjectID, clean bool) (domain.Session, error)
	ActivePrime(ctx context.Context) (domain.Session, bool, error)
	Send(ctx context.Context, id domain.SessionID, message string) error
}

type primeSupervisorConfig struct {
	ProjectID       domain.ProjectID
	Interval        time.Duration
	UnhealthyAfter  time.Duration
	RestartWindow   time.Duration
	RestartLimit    int
	RestartBackoff  time.Duration
	MaxBackoff      time.Duration
	IdleWakeAfter   time.Duration
	IdleWakeBackoff time.Duration
	IdleWakeMaxBack time.Duration
	Now             func() time.Time
	Logger          *slog.Logger
}

type primeSupervisorState struct {
	restartAttempts []time.Time
	nextRestartAt   time.Time
	restartBackoff  time.Duration

	nextIdleWakeAt  time.Time
	idleWakeBackoff time.Duration
}

func startPrimeSupervisor(ctx context.Context, cfg config.Config, sessions primeSessionService, notifier notificationSink, logger *slog.Logger) <-chan struct{} {
	projectID := domain.ProjectID(cfg.PrimeProjectID)
	if projectID == "" {
		done := make(chan struct{})
		close(done)
		return done
	}
	return startPrimeSupervisorWithConfig(ctx, primeSupervisorConfig{
		ProjectID:       projectID,
		Interval:        defaultPrimeSupervisorInterval,
		UnhealthyAfter:  defaultPrimeUnhealthyAfter,
		RestartWindow:   defaultPrimeRestartWindow,
		RestartLimit:    defaultPrimeRestartLimit,
		RestartBackoff:  defaultPrimeRestartBackoff,
		MaxBackoff:      defaultPrimeRestartMaxBackoff,
		IdleWakeAfter:   defaultPrimeIdleWakeAfter,
		IdleWakeBackoff: defaultPrimeIdleWakeBackoff,
		IdleWakeMaxBack: defaultPrimeIdleWakeMaxBackoff,
		Now:             time.Now,
		Logger:          logger,
	}, sessions, notifier)
}

func startPrimeSupervisorWithConfig(ctx context.Context, cfg primeSupervisorConfig, sessions primeSessionService, notifier notificationSink) <-chan struct{} {
	done := make(chan struct{})
	if cfg.ProjectID == "" {
		close(done)
		return done
	}
	cfg = cfg.withDefaults()
	go func() {
		defer close(done)
		state := &primeSupervisorState{}
		ensurePrime(ctx, cfg, state, sessions, notifier)
		ticker := time.NewTicker(cfg.Interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				ensurePrime(ctx, cfg, state, sessions, notifier)
			}
		}
	}()
	return done
}

func (c primeSupervisorConfig) withDefaults() primeSupervisorConfig {
	if c.Interval <= 0 {
		c.Interval = defaultPrimeSupervisorInterval
	}
	if c.UnhealthyAfter <= 0 {
		c.UnhealthyAfter = defaultPrimeUnhealthyAfter
	}
	if c.RestartWindow <= 0 {
		c.RestartWindow = defaultPrimeRestartWindow
	}
	if c.RestartLimit <= 0 {
		c.RestartLimit = defaultPrimeRestartLimit
	}
	if c.RestartBackoff <= 0 {
		c.RestartBackoff = defaultPrimeRestartBackoff
	}
	if c.MaxBackoff <= 0 {
		c.MaxBackoff = defaultPrimeRestartMaxBackoff
	}
	if c.IdleWakeAfter <= 0 {
		c.IdleWakeAfter = defaultPrimeIdleWakeAfter
	}
	if c.IdleWakeBackoff <= 0 {
		c.IdleWakeBackoff = defaultPrimeIdleWakeBackoff
	}
	if c.IdleWakeMaxBack <= 0 {
		c.IdleWakeMaxBack = defaultPrimeIdleWakeMaxBackoff
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	return c
}

func ensurePrime(ctx context.Context, cfg primeSupervisorConfig, state *primeSupervisorState, sessions primeSessionService, notifier notificationSink) {
	if sessions == nil || state == nil {
		return
	}
	cfg = cfg.withDefaults()
	now := cfg.Now().UTC()
	active, ok, err := sessions.ActivePrime(ctx)
	if err != nil {
		cfg.Logger.Warn("prime supervisor: active lookup failed", "err", err)
		return
	}
	if !ok {
		if _, err := sessions.SpawnPrime(ctx, cfg.ProjectID, false); err != nil {
			cfg.Logger.Warn("prime supervisor: spawn failed", "err", err)
		}
		state.resetIdleWake()
		return
	}
	if primeNeedsReplacement(active, now, cfg.UnhealthyAfter) {
		allowed, capped := state.reserveRestart(now, cfg)
		if capped {
			notifyPrimeRestartCapped(ctx, notifier, active, now)
		}
		if !allowed {
			return
		}
		if _, err := sessions.SpawnPrime(ctx, cfg.ProjectID, true); err != nil {
			cfg.Logger.Warn("prime supervisor: replacement failed", "session", active.ID, "err", err)
		}
		state.resetIdleWake()
		return
	}
	if primeShouldWake(active, now, cfg.IdleWakeAfter) {
		if state.reserveIdleWake(now, cfg) {
			if err := sessions.Send(ctx, active.ID, primeIdleWakeMessage); err != nil {
				cfg.Logger.Warn("prime supervisor: idle wake failed", "session", active.ID, "err", err)
			}
		}
		return
	}
	state.resetIdleWake()
}

func primeNeedsReplacement(sess domain.Session, now time.Time, unhealthyAfter time.Duration) bool {
	if sess.Status != domain.StatusNoSignal && sess.Activity.State != domain.ActivityExited {
		return false
	}
	return now.Sub(primeActivityTime(sess)) >= unhealthyAfter
}

func primeShouldWake(sess domain.Session, now time.Time, idleAfter time.Duration) bool {
	if sess.Status != domain.StatusIdle || sess.Activity.State != domain.ActivityIdle {
		return false
	}
	return now.Sub(primeActivityTime(sess)) >= idleAfter
}

func primeActivityTime(sess domain.Session) time.Time {
	if !sess.Activity.LastActivityAt.IsZero() {
		return sess.Activity.LastActivityAt
	}
	if !sess.UpdatedAt.IsZero() {
		return sess.UpdatedAt
	}
	return sess.CreatedAt
}

func (s *primeSupervisorState) reserveRestart(now time.Time, cfg primeSupervisorConfig) (allowed bool, capped bool) {
	cutoff := now.Add(-cfg.RestartWindow)
	kept := s.restartAttempts[:0]
	for _, t := range s.restartAttempts {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	s.restartAttempts = kept
	if now.Before(s.nextRestartAt) {
		return false, false
	}
	if len(s.restartAttempts) >= cfg.RestartLimit {
		return false, true
	}
	s.restartAttempts = append(s.restartAttempts, now)
	backoff := s.restartBackoff
	if backoff <= 0 {
		backoff = cfg.RestartBackoff
	} else {
		backoff *= 2
	}
	if backoff > cfg.MaxBackoff {
		backoff = cfg.MaxBackoff
	}
	s.restartBackoff = backoff
	s.nextRestartAt = now.Add(backoff)
	return true, false
}

func (s *primeSupervisorState) reserveIdleWake(now time.Time, cfg primeSupervisorConfig) bool {
	if now.Before(s.nextIdleWakeAt) {
		return false
	}
	backoff := s.idleWakeBackoff
	if backoff <= 0 {
		backoff = cfg.IdleWakeBackoff
	} else {
		backoff *= 2
	}
	if backoff > cfg.IdleWakeMaxBack {
		backoff = cfg.IdleWakeMaxBack
	}
	s.idleWakeBackoff = backoff
	s.nextIdleWakeAt = now.Add(backoff)
	return true
}

func (s *primeSupervisorState) resetIdleWake() {
	s.nextIdleWakeAt = time.Time{}
	s.idleWakeBackoff = 0
}

func notifyPrimeRestartCapped(ctx context.Context, notifier notificationSink, sess domain.Session, now time.Time) {
	if notifier == nil {
		return
	}
	_ = notifier.Notify(ctx, ports.NotificationIntent{
		Type:               domain.NotificationPrimeRestartCapped,
		SessionID:          sess.ID,
		ProjectID:          sess.ProjectID,
		DedupeKey:          fmt.Sprintf("prime:restart-capped:%s", sess.ID),
		CreatedAt:          now,
		SessionDisplayName: sess.DisplayName,
		Message:            "AO tried to replace the unhealthy prime three times in the last hour and paused automatic replacement. Inspect the active prime before restarting it.",
	})
}

const primeIdleWakeMessage = "Prime status check: if you are at an idle prompt, summarize current fleet health and continue supervising. Do not claim, dispatch, merge, or command worker sessions directly."
