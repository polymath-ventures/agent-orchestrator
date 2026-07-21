package daemon

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/config"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func TestStartPrimeSupervisorDisabledReturnsClosed(t *testing.T) {
	done := startPrimeSupervisor(context.Background(), config.Config{}, &fakePrimeSessions{}, nil, nil)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("disabled prime supervisor did not return a closed done channel")
	}
}

func TestEnsurePrimeSpawnsWhenMissing(t *testing.T) {
	now := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	sessions := &fakePrimeSessions{}
	ensurePrime(context.Background(), testPrimeConfig(now), &primeSupervisorState{}, sessions, nil)

	if sessions.spawnCalls != 1 {
		t.Fatalf("SpawnPrime calls = %d, want 1", sessions.spawnCalls)
	}
	if sessions.lastProjectID != "ao" || sessions.lastClean {
		t.Fatalf("SpawnPrime project=%q clean=%v, want ao false", sessions.lastProjectID, sessions.lastClean)
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

func TestEnsurePrimeLogsAndContinuesOnLookupError(t *testing.T) {
	now := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	sessions := &fakePrimeSessions{activeErr: errors.New("db down")}
	ensurePrime(context.Background(), testPrimeConfig(now), &primeSupervisorState{}, sessions, nil)

	if sessions.spawnCalls != 0 {
		t.Fatalf("SpawnPrime calls = %d, want none after lookup error", sessions.spawnCalls)
	}
}

func testPrimeConfig(now time.Time) primeSupervisorConfig {
	return primeSupervisorConfig{
		ProjectID:       "ao",
		Interval:        time.Millisecond,
		UnhealthyAfter:  5 * time.Minute,
		RestartWindow:   time.Hour,
		RestartLimit:    3,
		RestartBackoff:  time.Minute,
		MaxBackoff:      10 * time.Minute,
		IdleWakeAfter:   15 * time.Minute,
		IdleWakeBackoff: time.Minute,
		IdleWakeMaxBack: 5 * time.Minute,
		Now:             func() time.Time { return now },
	}
}

func primeSession(id domain.SessionID, status domain.SessionStatus, activity domain.ActivityState, last time.Time) domain.Session {
	return domain.Session{
		SessionRecord: domain.SessionRecord{
			ID:          id,
			ProjectID:   "ao",
			Kind:        domain.KindPrime,
			DisplayName: "AO Prime",
			Activity:    domain.Activity{State: activity, LastActivityAt: last},
			CreatedAt:   last,
			UpdatedAt:   last,
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

func (f *fakePrimeSessions) Send(_ context.Context, id domain.SessionID, message string) error {
	f.sent = append(f.sent, struct {
		id      domain.SessionID
		message string
	}{id: id, message: message})
	return nil
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
	// No session/project references: the configured project may not exist and
	// a dangling id would be rejected by the notifications project FK. The
	// configured value travels in the message and dedupe key.
	if got.SessionID != "" || got.ProjectID != "" {
		t.Fatalf("notification refs = session %q project %q, want both empty", got.SessionID, got.ProjectID)
	}
	if !strings.Contains(got.Message, string(testPrimeConfig(base).ProjectID)) || !strings.Contains(got.DedupeKey, string(testPrimeConfig(base).ProjectID)) {
		t.Fatalf("message/dedupe must name the configured project: %+v", got)
	}
}
