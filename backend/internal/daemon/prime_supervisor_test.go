package daemon

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/config"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func TestStartPrimeSupervisorRunsFromFleetSettingsWithoutEnvProject(t *testing.T) {
	source := &fakePrimeProjects{
		primeSettings: domain.PrimeSettings{Enabled: true, Harness: domain.HarnessCodex},
		settingsRead:  make(chan struct{}, 1),
	}
	sessions := &fakePrimeSessions{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := startPrimeSupervisor(ctx, config.Config{}, source, sessions, nil, nil)
	select {
	case <-source.settingsRead:
	case <-time.After(time.Second):
		t.Fatal("prime supervisor did not read fleet settings")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("prime supervisor did not stop")
	}
	if sessions.spawnCalls == 0 {
		t.Fatal("prime supervisor did not spawn from enabled fleet settings")
	}
}

func TestEnsurePrimeSpawnsWhenMissing(t *testing.T) {
	now := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	sessions := &fakePrimeSessions{}
	ensurePrime(context.Background(), testPrimeConfig(now), &primeSupervisorState{}, sessions, nil)

	if sessions.spawnCalls != 1 {
		t.Fatalf("SpawnPrime calls = %d, want 1", sessions.spawnCalls)
	}
	if sessions.lastProjectID != "" || sessions.lastClean {
		t.Fatalf("SpawnPrime project=%q clean=%v, want empty false", sessions.lastProjectID, sessions.lastClean)
	}
}

func TestEnsurePrimeRetiresActiveWhenDisabled(t *testing.T) {
	now := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	sessions := &fakePrimeSessions{
		active: primeSession("ao-prime", domain.StatusIdle, domain.ActivityIdle, now.Add(-20*time.Minute)),
		ok:     true,
	}
	cfg := testPrimeConfig(now)
	cfg.PrimeSettings = func(context.Context) (domain.PrimeSettings, bool) {
		return domain.PrimeSettings{Enabled: false}, true
	}

	ensurePrime(context.Background(), cfg, &primeSupervisorState{}, sessions, nil)

	if sessions.retired != "ao-prime" {
		t.Fatalf("retired prime = %q, want ao-prime", sessions.retired)
	}
	if sessions.spawnCalls != 0 || len(sessions.sent) != 0 {
		t.Fatalf("disabled prime spawn=%d wake=%d, want neither", sessions.spawnCalls, len(sessions.sent))
	}
}

func TestEnsurePrimeCapsMissingPrimeRespawnAttempts(t *testing.T) {
	base := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	sessions := &fakePrimeSessions{}
	state := &primeSupervisorState{
		lastPrime: primeSession("ao-prime", domain.StatusTerminated, domain.ActivityExited, base.Add(-10*time.Minute)),
		restartAttempts: []time.Time{
			base.Add(-50 * time.Minute),
			base.Add(-30 * time.Minute),
			base.Add(-10 * time.Minute),
		},
	}
	notifier := &fakePrimeNotifier{}

	ensurePrime(context.Background(), testPrimeConfig(base), state, sessions, notifier)

	if sessions.spawnCalls != 0 {
		t.Fatalf("SpawnPrime calls = %d, want capped at zero", sessions.spawnCalls)
	}
	if len(notifier.intents) != 1 {
		t.Fatalf("notifications = %d, want one cap alert", len(notifier.intents))
	}
	if notifier.intents[0].Type != domain.NotificationPrimeRestartCapped || notifier.intents[0].SessionID != "ao-prime" {
		t.Fatalf("notification = %+v, want prime restart cap for last prime", notifier.intents[0])
	}
}

func TestEnsurePrimeReplacesUnhealthyWithBudget(t *testing.T) {
	now := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	sessions := &fakePrimeSessions{
		active: primeSession("ao-prime", domain.StatusNoSignal, domain.ActivityIdle, now.Add(-10*time.Minute)),
		ok:     true,
	}
	state := &primeSupervisorState{}

	ensurePrime(context.Background(), testPrimeConfig(now), state, sessions, nil)

	if sessions.spawnCalls != 1 || !sessions.lastClean {
		t.Fatalf("SpawnPrime calls=%d clean=%v, want one clean replacement", sessions.spawnCalls, sessions.lastClean)
	}
}

func TestEnsurePrimeCapsUnhealthyReplacementAttempts(t *testing.T) {
	base := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	sessions := &fakePrimeSessions{
		active: primeSession("ao-prime", domain.StatusNoSignal, domain.ActivityIdle, base.Add(-10*time.Minute)),
		ok:     true,
	}
	state := &primeSupervisorState{
		restartAttempts: []time.Time{
			base.Add(-50 * time.Minute),
			base.Add(-30 * time.Minute),
			base.Add(-10 * time.Minute),
		},
	}
	notifier := &fakePrimeNotifier{}

	ensurePrime(context.Background(), testPrimeConfig(base), state, sessions, notifier)

	if sessions.spawnCalls != 0 {
		t.Fatalf("SpawnPrime calls = %d, want capped at zero", sessions.spawnCalls)
	}
	if len(notifier.intents) != 1 {
		t.Fatalf("notifications = %d, want one cap alert", len(notifier.intents))
	}
	if notifier.intents[0].Type != domain.NotificationPrimeRestartCapped || notifier.intents[0].SessionID != "ao-prime" {
		t.Fatalf("notification = %+v, want prime restart cap for ao-prime", notifier.intents[0])
	}
}

func TestEnsurePrimeHonorsReplacementBackoff(t *testing.T) {
	base := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	sessions := &fakePrimeSessions{
		active: primeSession("ao-prime", domain.StatusNoSignal, domain.ActivityIdle, base.Add(-10*time.Minute)),
		ok:     true,
	}
	state := &primeSupervisorState{nextRestartAt: base.Add(time.Minute)}

	ensurePrime(context.Background(), testPrimeConfig(base), state, sessions, nil)

	if sessions.spawnCalls != 0 {
		t.Fatalf("SpawnPrime calls = %d, want backoff to suppress replacement", sessions.spawnCalls)
	}
}

func TestEnsurePrimeResetsReplacementBackoffAfterHealthyPrime(t *testing.T) {
	base := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	sessions := &fakePrimeSessions{
		active: primeSession("ao-prime", domain.StatusWorking, domain.ActivityActive, base),
		ok:     true,
	}
	state := &primeSupervisorState{
		restartAttempts: []time.Time{base.Add(-10 * time.Minute)},
		nextRestartAt:   base.Add(time.Minute),
		restartBackoff:  10 * time.Minute,
	}

	ensurePrime(context.Background(), testPrimeConfig(base), state, sessions, nil)

	if len(state.restartAttempts) != 0 || !state.nextRestartAt.IsZero() || state.restartBackoff != 0 {
		t.Fatalf("restart state = %+v, want reset after healthy prime", state)
	}
}

func TestEnsurePrimeWakesIdleWithBackoff(t *testing.T) {
	base := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	sessions := &fakePrimeSessions{
		active: primeSession("ao-prime", domain.StatusIdle, domain.ActivityIdle, base.Add(-20*time.Minute)),
		ok:     true,
	}
	state := &primeSupervisorState{}
	cfg := testPrimeConfig(base)

	ensurePrime(context.Background(), cfg, state, sessions, nil)
	ensurePrime(context.Background(), cfg, state, sessions, nil)

	if len(sessions.sent) != 1 {
		t.Fatalf("sent wake messages = %d, want one before backoff", len(sessions.sent))
	}
	if sessions.sent[0].id != "ao-prime" || sessions.sent[0].message == "" {
		t.Fatalf("wake send = %+v, want ao-prime message", sessions.sent[0])
	}
}

func TestEnsurePrimeWakesWaitingInputWithBackoff(t *testing.T) {
	base := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	sessions := &fakePrimeSessions{
		active: primeSession("ao-prime", domain.StatusNeedsInput, domain.ActivityWaitingInput, base.Add(-20*time.Minute)),
		ok:     true,
	}
	state := &primeSupervisorState{}
	cfg := testPrimeConfig(base)

	ensurePrime(context.Background(), cfg, state, sessions, nil)
	ensurePrime(context.Background(), cfg, state, sessions, nil)

	if len(sessions.sent) != 1 {
		t.Fatalf("sent wake messages = %d, want one before backoff", len(sessions.sent))
	}
	if sessions.sent[0].id != "ao-prime" || sessions.sent[0].message == "" {
		t.Fatalf("wake send = %+v, want ao-prime message", sessions.sent[0])
	}
}

func TestEnsurePrimeUsesConfiguredWakeInterval(t *testing.T) {
	base := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	sessions := &fakePrimeSessions{
		active: primeSession("ao-prime", domain.StatusIdle, domain.ActivityIdle, base.Add(-20*time.Minute)),
		ok:     true,
	}
	cfg := testPrimeConfig(base)
	cfg.PrimeSettings = func(context.Context) (domain.PrimeSettings, bool) {
		return domain.PrimeSettings{Enabled: true, WakeInterval: "30m"}, true
	}

	ensurePrime(context.Background(), cfg, &primeSupervisorState{}, sessions, nil)
	if len(sessions.sent) != 0 {
		t.Fatalf("sent wake messages = %d, want none before configured interval", len(sessions.sent))
	}

	cfg.Now = func() time.Time { return base.Add(11 * time.Minute) }
	ensurePrime(context.Background(), cfg, &primeSupervisorState{}, sessions, nil)
	if len(sessions.sent) != 1 {
		t.Fatalf("sent wake messages = %d, want wake after configured interval", len(sessions.sent))
	}
}

func TestEnsurePrimeUsesDisabledWakeBackoffAsFixedInterval(t *testing.T) {
	base := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	disabled := false
	sessions := &fakePrimeSessions{
		active: primeSession("ao-prime", domain.StatusIdle, domain.ActivityIdle, base.Add(-20*time.Minute)),
		ok:     true,
	}
	cfg := testPrimeConfig(base)
	cfg.PrimeSettings = func(context.Context) (domain.PrimeSettings, bool) {
		return domain.PrimeSettings{
			Enabled:      true,
			WakeInterval: "15m",
			WakeBackoff:  &domain.WakeBackoffConfig{Enabled: &disabled},
		}, true
	}
	state := &primeSupervisorState{}

	ensurePrime(context.Background(), cfg, state, sessions, nil)
	cfg.Now = func() time.Time { return base.Add(16 * time.Minute) }
	ensurePrime(context.Background(), cfg, state, sessions, nil)
	cfg.Now = func() time.Time { return base.Add(32 * time.Minute) }
	ensurePrime(context.Background(), cfg, state, sessions, nil)

	if len(sessions.sent) != 3 {
		t.Fatalf("sent wake messages = %d, want fixed-interval wake on each elapsed interval", len(sessions.sent))
	}
}

func TestEnsurePrimeDoesNotWakeBeforeFirstSignal(t *testing.T) {
	now := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	active := primeSession("ao-prime", domain.StatusIdle, domain.ActivityIdle, now.Add(-20*time.Minute))
	active.FirstSignalAt = time.Time{}
	sessions := &fakePrimeSessions{
		active: active,
		ok:     true,
	}

	ensurePrime(context.Background(), testPrimeConfig(now), &primeSupervisorState{}, sessions, nil)

	if len(sessions.sent) != 0 {
		t.Fatalf("sent wake messages = %d, want none before first signal", len(sessions.sent))
	}
}

func TestEnsurePrimeDoesNotWakeBlockedPrime(t *testing.T) {
	now := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	sessions := &fakePrimeSessions{
		active: primeSession("ao-prime", domain.StatusNeedsInput, domain.ActivityBlocked, now.Add(-20*time.Minute)),
		ok:     true,
	}
	ensurePrime(context.Background(), testPrimeConfig(now), &primeSupervisorState{}, sessions, nil)

	if len(sessions.sent) != 0 {
		t.Fatalf("sent wake messages = %d, want none for blocked prime", len(sessions.sent))
	}
}

func TestEnsurePrimeSuppressesWakeWhenFleetPaused(t *testing.T) {
	now := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	sessions := &fakePrimeSessions{
		active: primeSession("ao-prime", domain.StatusIdle, domain.ActivityIdle, now.Add(-20*time.Minute)),
		ok:     true,
	}
	state := &primeSupervisorState{}
	cfg := testPrimeConfig(now)
	cfg.FleetPaused = func(context.Context) bool { return true }

	ensurePrime(context.Background(), cfg, state, sessions, nil)

	if len(sessions.sent) != 0 {
		t.Fatalf("sent wake messages = %d, want none while project paused", len(sessions.sent))
	}
}

func TestEnsurePrimeReplacesUnhealthyWhenFleetPaused(t *testing.T) {
	now := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	sessions := &fakePrimeSessions{
		active: primeSession("ao-prime", domain.StatusNoSignal, domain.ActivityIdle, now.Add(-10*time.Minute)),
		ok:     true,
	}
	cfg := testPrimeConfig(now)
	cfg.FleetPaused = func(context.Context) bool { return true }

	ensurePrime(context.Background(), cfg, &primeSupervisorState{}, sessions, nil)

	if sessions.spawnCalls != 1 || !sessions.lastClean {
		t.Fatalf("SpawnPrime calls=%d clean=%v, want one clean replacement while paused", sessions.spawnCalls, sessions.lastClean)
	}
}

func TestStartPrimeSupervisorSuppressesWakeWhenFleetPaused(t *testing.T) {
	now := time.Now().UTC()
	source := &fakePrimeProjects{
		primeSettings: domain.PrimeSettings{Enabled: true, Harness: domain.HarnessCodex},
		fleetPaused:   true,
		fleetRead:     make(chan struct{}, 1),
	}
	sessions := &fakePrimeSessions{
		active: primeSession("ao-prime", domain.StatusIdle, domain.ActivityIdle, now.Add(-20*time.Minute)),
		ok:     true,
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := startPrimeSupervisor(ctx, config.Config{}, source, sessions, nil, nil)
	select {
	case <-source.fleetRead:
	case <-time.After(time.Second):
		t.Fatal("prime supervisor did not read fleet pause state")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("prime supervisor did not stop")
	}
	if len(sessions.sent) != 0 {
		t.Fatalf("sent wake messages = %d, want none while fleet paused", len(sessions.sent))
	}
}

func TestEnsurePrimeLogsAndContinuesOnLookupError(t *testing.T) {
	now := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	sessions := &fakePrimeSessions{activeErr: errors.New("db down")}
	ensurePrime(context.Background(), testPrimeConfig(now), &primeSupervisorState{}, sessions, nil)

	if sessions.spawnCalls != 0 {
		t.Fatalf("SpawnPrime calls = %d, want none after lookup error", sessions.spawnCalls)
	}
}

func TestEnsurePrimeSkipsTickWhenSettingsUnreadable(t *testing.T) {
	now := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	sessions := &fakePrimeSessions{
		active: primeSession("ao-prime", domain.StatusIdle, domain.ActivityIdle, now),
		ok:     true,
	}
	cfg := testPrimeConfig(now)
	cfg.PrimeSettings = func(context.Context) (domain.PrimeSettings, bool) {
		return domain.PrimeSettings{}, false
	}

	ensurePrime(context.Background(), cfg, &primeSupervisorState{}, sessions, nil)

	if sessions.retired != "" {
		t.Fatalf("retired prime = %q, want no retire when settings are unreadable", sessions.retired)
	}
	if sessions.spawnCalls != 0 || len(sessions.sent) != 0 {
		t.Fatalf("unreadable settings caused spawn=%d wake=%d, want skipped tick", sessions.spawnCalls, len(sessions.sent))
	}
}

func testPrimeConfig(now time.Time) primeSupervisorConfig {
	return primeSupervisorConfig{
		Interval:        time.Millisecond,
		UnhealthyAfter:  5 * time.Minute,
		RestartWindow:   time.Hour,
		RestartLimit:    3,
		RestartBackoff:  time.Minute,
		MaxBackoff:      10 * time.Minute,
		IdleWakeAfter:   15 * time.Minute,
		IdleWakeBackoff: time.Minute,
		IdleWakeMaxBack: 5 * time.Minute,
		PrimeSettings: func(context.Context) (domain.PrimeSettings, bool) {
			return domain.PrimeSettings{Enabled: true, Harness: domain.HarnessCodex}, true
		},
		Now: func() time.Time { return now },
	}
}

func primeSession(id domain.SessionID, status domain.SessionStatus, activity domain.ActivityState, last time.Time) domain.Session {
	return domain.Session{
		SessionRecord: domain.SessionRecord{
			ID:            id,
			ProjectID:     "ao",
			Kind:          domain.KindPrime,
			DisplayName:   "AO Prime",
			Activity:      domain.Activity{State: activity, LastActivityAt: last},
			FirstSignalAt: last,
			CreatedAt:     last,
			UpdatedAt:     last,
		},
		Status: status,
	}
}

type fakePrimeSessions struct {
	active    domain.Session
	ok        bool
	activeErr error

	spawnCalls    int
	lastProjectID domain.ProjectID
	lastClean     bool
	retired       domain.SessionID
	sent          []struct {
		id      domain.SessionID
		message string
	}
}

func (f *fakePrimeSessions) ActivePrime(context.Context) (domain.Session, bool, error) {
	if f.activeErr != nil {
		return domain.Session{}, false, f.activeErr
	}
	return f.active, f.ok, nil
}

func (f *fakePrimeSessions) SpawnPrime(_ context.Context, projectID domain.ProjectID, clean bool) (domain.Session, error) {
	f.spawnCalls++
	f.lastProjectID = projectID
	f.lastClean = clean
	return primeSession("ao-prime-new", domain.StatusIdle, domain.ActivityIdle, time.Now()), nil
}

func (f *fakePrimeSessions) RetirePrime(_ context.Context, id domain.SessionID) error {
	f.retired = id
	return nil
}

func (f *fakePrimeSessions) Send(_ context.Context, id domain.SessionID, message string) error {
	f.sent = append(f.sent, struct {
		id      domain.SessionID
		message string
	}{id: id, message: message})
	return nil
}

type fakePrimeProjects struct {
	primeSettings domain.PrimeSettings
	settingsRead  chan struct{}
	fleetPaused   bool
	fleetRead     chan struct{}
}

func (f *fakePrimeProjects) GetPrimeSettings(context.Context) (domain.PrimeSettings, error) {
	if f.settingsRead != nil {
		select {
		case f.settingsRead <- struct{}{}:
		default:
		}
	}
	return f.primeSettings, nil
}

func (f *fakePrimeProjects) GetFleetPaused(context.Context) (bool, error) {
	if f.fleetRead != nil {
		select {
		case f.fleetRead <- struct{}{}:
		default:
		}
	}
	return f.fleetPaused, nil
}

type fakePrimeNotifier struct {
	intents []ports.NotificationIntent
}

func (f *fakePrimeNotifier) Notify(_ context.Context, intent ports.NotificationIntent) error {
	f.intents = append(f.intents, intent)
	return nil
}

// A prime that has never spawned successfully (e.g. mistyped
// AO_PRIME_PROJECT_ID) must still alert when the restart budget caps —
// silence here was the failure mode: three Warn logs and nothing else.
func TestEnsurePrimeCapAlertsWhenPrimeNeverSpawned(t *testing.T) {
	base := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	sessions := &fakePrimeSessions{}
	state := &primeSupervisorState{
		restartAttempts: []time.Time{
			base.Add(-50 * time.Minute),
			base.Add(-30 * time.Minute),
			base.Add(-10 * time.Minute),
		},
	}
	notifier := &fakePrimeNotifier{}

	ensurePrime(context.Background(), testPrimeConfig(base), state, sessions, notifier)

	if sessions.spawnCalls != 0 {
		t.Fatalf("SpawnPrime calls = %d, want capped at zero", sessions.spawnCalls)
	}
	if len(notifier.intents) != 1 {
		t.Fatalf("notifications = %d, want one cap alert even with no lastPrime", len(notifier.intents))
	}
	got := notifier.intents[0]
	if got.Type != domain.NotificationPrimeRestartCapped {
		t.Fatalf("notification type = %q, want prime restart cap", got.Type)
	}
	if got.SessionID != "" || got.ProjectID != "" {
		t.Fatalf("notification refs = session %q project %q, want both empty", got.SessionID, got.ProjectID)
	}
	if got.Message == "" || got.DedupeKey == "" {
		t.Fatalf("message/dedupe must describe fleet settings failure: %+v", got)
	}
}
