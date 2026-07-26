package session

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	sessionmanager "github.com/aoagents/agent-orchestrator/backend/internal/session_manager"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// The reported outage, at the service level: the newest role row is terminated
// and nothing else is active. Reconciliation must still produce a live role
// session — previously both spawn paths only looked at active rows, so the
// terminated row's worktree kept holding the canonical branch and every attempt
// failed.
func TestReconcileRoleRelaunchesWhenNewestRowIsTerminated(t *testing.T) {
	for _, tc := range []struct {
		name   string
		target domain.RoleTarget
		seed   domain.SessionRecord
		spawn  domain.SessionRecord
	}{
		{
			name:   "prime",
			target: domain.PrimeTarget(),
			seed:   domain.SessionRecord{ID: "prime-1", Kind: domain.KindPrime, IsTerminated: true, Metadata: domain.SessionMetadata{Branch: "ao/prime"}},
			spawn:  domain.SessionRecord{ID: "prime-2", Kind: domain.KindPrime, Metadata: domain.SessionMetadata{Branch: "ao/prime"}},
		},
		{
			name:   "orchestrator",
			target: domain.OrchestratorTarget("mer"),
			seed:   domain.SessionRecord{ID: "mer-1", ProjectID: "mer", Kind: domain.KindOrchestrator, IsTerminated: true},
			spawn:  domain.SessionRecord{ID: "mer-2", ProjectID: "mer", Kind: domain.KindOrchestrator},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := newFakeStore()
			st.prime = domain.PrimeSettings{Enabled: true}.WithDefaults()
			st.projects["mer"] = domain.ProjectRecord{ID: "mer"}
			st.sessions[tc.seed.ID] = tc.seed

			fc := &fakeCommander{spawnRecord: tc.spawn}
			svc := &Service{manager: fc, store: st}

			got, err := svc.ReconcileRole(context.Background(), tc.target, ReconcileOptions{})
			if err != nil {
				t.Fatalf("ReconcileRole: %v", err)
			}
			if got.ID != tc.spawn.ID {
				t.Fatalf("session = %q, want the replacement %q", got.ID, tc.spawn.ID)
			}
			if len(fc.released) != 1 || fc.released[0] != tc.target {
				t.Fatalf("released = %v, want the stale resources for %v released before spawning", fc.released, tc.target)
			}
		})
	}
}

// Reconciliation is idempotent: a healthy active role session is returned
// as-is, and nothing is spawned or released.
func TestReconcileRoleIsIdempotentWhenHealthy(t *testing.T) {
	st := newFakeStore()
	st.prime = domain.PrimeSettings{Enabled: true}.WithDefaults()
	st.sessions["prime-1"] = domain.SessionRecord{ID: "prime-1", Kind: domain.KindPrime}

	fc := &fakeCommander{spawnRecord: domain.SessionRecord{ID: "prime-2", Kind: domain.KindPrime}}
	svc := &Service{manager: fc, store: st}

	got, err := svc.ReconcileRole(context.Background(), domain.PrimeTarget(), ReconcileOptions{})
	if err != nil {
		t.Fatalf("ReconcileRole: %v", err)
	}
	if got.ID != "prime-1" {
		t.Fatalf("session = %q, want the existing active prime", got.ID)
	}
	if fc.spawned {
		t.Fatal("a healthy role session must not be replaced")
	}
	if len(fc.released) != 0 {
		t.Fatalf("released = %v, want no release when nothing is being replaced", fc.released)
	}
}

// Clean retires the active role session first, then releases stale resources,
// then spawns.
func TestReconcileRoleCleanRetiresThenReleasesThenSpawns(t *testing.T) {
	st := newFakeStore()
	st.prime = domain.PrimeSettings{Enabled: true}.WithDefaults()
	st.sessions["prime-1"] = domain.SessionRecord{ID: "prime-1", Kind: domain.KindPrime}

	fc := &fakeCommander{spawnRecord: domain.SessionRecord{ID: "prime-2", Kind: domain.KindPrime, Metadata: domain.SessionMetadata{Branch: "ao/prime"}}}
	svc := &Service{manager: fc, store: st}

	if _, err := svc.ReconcileRole(context.Background(), domain.PrimeTarget(), ReconcileOptions{Clean: true}); err != nil {
		t.Fatalf("ReconcileRole: %v", err)
	}
	if len(fc.retired) != 1 || fc.retired[0] != "prime-1" {
		t.Fatalf("retired = %v, want prime-1", fc.retired)
	}
	if len(fc.released) != 1 {
		t.Fatalf("released = %v, want the stale-resource release to run", fc.released)
	}
	if !fc.spawned {
		t.Fatal("a replacement must be spawned")
	}
}

// A non-role target is a client error, not a silent no-op.
func TestReconcileRoleRejectsNonRoleTarget(t *testing.T) {
	svc := &Service{manager: &fakeCommander{}, store: newFakeStore()}
	if _, err := svc.ReconcileRole(context.Background(), domain.RoleTarget{Kind: domain.KindWorker, ProjectID: "mer"}, ReconcileOptions{}); err == nil {
		t.Fatal("ReconcileRole() = nil error for a worker target, want an error")
	}
}

// A release failure must surface rather than being papered over with a spawn
// that is about to fail on the branch the release did not free.
func TestReconcileRolePropagatesReleaseFailure(t *testing.T) {
	st := newFakeStore()
	st.prime = domain.PrimeSettings{Enabled: true}.WithDefaults()
	st.sessions["prime-1"] = domain.SessionRecord{ID: "prime-1", Kind: domain.KindPrime, IsTerminated: true}

	fc := &fakeCommander{
		spawnRecord: domain.SessionRecord{ID: "prime-2", Kind: domain.KindPrime},
		releaseErr:  context.DeadlineExceeded,
	}
	svc := &Service{manager: fc, store: st}

	if _, err := svc.ReconcileRole(context.Background(), domain.PrimeTarget(), ReconcileOptions{}); err == nil {
		t.Fatal("ReconcileRole() = nil error, want the release failure surfaced")
	}
	if fc.spawned {
		t.Fatal("no replacement may be spawned when stale resources could not be released")
	}
}

// Persisted settings are the single source of truth for Prime lifecycle, so a
// relaunch must not resurrect a Prime the operator disabled. Found by exercising
// the live endpoint: it previously fell through to a spawn and failed with an
// unrelated "agent harness required".
func TestReconcileRoleRefusesPrimeWhenDisabled(t *testing.T) {
	st := newFakeStore()
	st.prime = domain.PrimeSettings{Enabled: false}.WithDefaults()

	fc := &fakeCommander{spawnRecord: domain.SessionRecord{ID: "prime-1", Kind: domain.KindPrime}}
	svc := &Service{manager: fc, store: st}

	_, err := svc.ReconcileRole(context.Background(), domain.PrimeTarget(), ReconcileOptions{})
	if err == nil {
		t.Fatal("ReconcileRole() = nil error while Prime is disabled, want a refusal")
	}
	if !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("error = %q, want it to explain Prime is disabled", err)
	}
	if fc.spawned {
		t.Fatal("no Prime may be spawned while Prime is disabled")
	}
	if len(fc.released) != 0 {
		t.Fatalf("released = %v, want no stale-resource release for a disabled role", fc.released)
	}
}

// An enabled Prime still reconciles normally.
func TestReconcileRoleAllowsPrimeWhenEnabled(t *testing.T) {
	st := newFakeStore()
	st.prime = domain.PrimeSettings{Enabled: true, Harness: domain.HarnessClaudeCode}.WithDefaults()

	fc := &fakeCommander{spawnRecord: domain.SessionRecord{ID: "prime-1", Kind: domain.KindPrime, Harness: domain.HarnessClaudeCode}}
	svc := &Service{manager: fc, store: st}

	if _, err := svc.ReconcileRole(context.Background(), domain.PrimeTarget(), ReconcileOptions{}); err != nil {
		t.Fatalf("ReconcileRole: %v", err)
	}
	if !fc.spawned {
		t.Fatal("an enabled Prime must reconcile into existence")
	}
}

// The singleton guarantee under concurrency. Two callers race the same role
// target (the supervisor tick and a user relaunch is the real pairing); the
// role lock must serialize them so exactly one spawn happens and the second
// caller observes the first caller's session.
//
// This test was listed in the plan and checked off before it existed — a
// reviewer caught that. Without it, a future change that narrows the lock scope
// would silently reintroduce double-Prime.
func TestReconcileRoleSerializesCompetingCallers(t *testing.T) {
	st := newFakeStore()
	st.prime = domain.PrimeSettings{Enabled: true}.WithDefaults()

	fc := &concurrentCommander{
		store:       st,
		spawnRecord: domain.SessionRecord{ID: "prime-1", Kind: domain.KindPrime, Metadata: domain.SessionMetadata{Branch: "ao/prime"}},
	}
	svc := &Service{manager: fc, store: st}

	const callers = 8
	var start sync.WaitGroup
	var done sync.WaitGroup
	start.Add(1)
	results := make([]domain.Session, callers)
	errs := make([]error, callers)
	for i := 0; i < callers; i++ {
		done.Add(1)
		go func(i int) {
			defer done.Done()
			start.Wait()
			results[i], errs[i] = svc.ReconcileRole(context.Background(), domain.PrimeTarget(), ReconcileOptions{})
		}(i)
	}
	start.Done()
	done.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("caller %d: ReconcileRole: %v", i, err)
		}
	}
	if got := fc.spawnCount(); got != 1 {
		t.Fatalf("spawns = %d, want exactly 1 — the role lock must serialize competing reconciles", got)
	}
	for i, sess := range results {
		if sess.ID != "prime-1" {
			t.Fatalf("caller %d saw session %q, want the single prime-1", i, sess.ID)
		}
	}
}

// concurrentCommander is a fakeCommander that publishes the spawned session into
// the store, so a second caller's active-session lookup can observe it — which
// is what makes the idempotence half of the guarantee testable.
type concurrentCommander struct {
	fakeCommander
	mu          sync.Mutex
	store       *fakeStore
	spawnRecord domain.SessionRecord
	spawns      int
}

func (f *concurrentCommander) Spawn(_ context.Context, cfg ports.SpawnConfig) (domain.SessionRecord, int, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.spawns++
	rec := f.spawnRecord
	rec.Kind = cfg.Kind
	// The fake store has no lock of its own; the commander's mutex is the one
	// serializing writes here, and ReconcileRole's role lock serializes readers.
	f.store.sessions[rec.ID] = rec
	return rec, 0, 0, nil
}

func (f *concurrentCommander) spawnCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.spawns
}

// disconnectingCommander models the caller going away during the role spawn:
// the replacement is created and is live, and only then does the request context
// die. RetireForReplacement honors the context it is handed — the real one kills
// a tmux session and writes terminal state, both of which fail immediately on a
// cancelled context — so a retire routed through the caller's context records
// "skipped" instead of taking effect.
type disconnectingCommander struct {
	*fakeCommander
	cancel        context.CancelFunc
	retiredLive   int
	retireSkipped int
}

func (f *disconnectingCommander) Spawn(ctx context.Context, cfg ports.SpawnConfig) (domain.SessionRecord, int, int, error) {
	f.cancel()
	return f.fakeCommander.Spawn(ctx, cfg)
}

func (f *disconnectingCommander) RetireForReplacement(ctx context.Context, id domain.SessionID) error {
	if err := ctx.Err(); err != nil {
		f.retireSkipped++
		return err
	}
	f.retiredLive++
	return f.fakeCommander.RetireForReplacement(ctx, id)
}

// The unverified replacement is already LIVE when verification fails, and a
// disconnected caller is one of the ordinary reasons verification fails at all.
// The retire must therefore outlive the caller's cancellation: skipping it
// leaves an unverified singleton running, which the next ensure pass sees as an
// active role session and silently adopts — the exact outcome this branch exists
// to prevent, now with a session on the wrong harness pinned as fleet Prime.
func TestReconcileRoleRetiresUnverifiedReplacementWhenCallerDisconnects(t *testing.T) {
	st := newFakeStore()
	st.prime = domain.PrimeSettings{Enabled: true, Harness: domain.HarnessCodex}.WithDefaults()
	cctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fc := &disconnectingCommander{
		fakeCommander: &fakeCommander{spawnRecord: domain.SessionRecord{
			ID:       "prime-1",
			Kind:     domain.KindPrime,
			Harness:  domain.HarnessClaudeCode, // mismatch → verification failure
			Metadata: domain.SessionMetadata{Branch: "ao/prime"},
		}},
		cancel: cancel,
	}
	svc := &Service{manager: fc, store: st}

	_, err := svc.ReconcileRole(cctx, domain.PrimeTarget(), ReconcileOptions{})
	if err == nil {
		t.Fatal("ReconcileRole() = nil error, want the replacement verification failure")
	}
	if cctx.Err() == nil {
		t.Fatal("setup: the spawn must have cancelled the caller context")
	}
	if fc.retiredLive != 1 {
		t.Fatalf("retires that took effect = %d (skipped on cancelled ctx = %d, err = %v); the unverified live prime was left for the next ensure pass to adopt",
			fc.retiredLive, fc.retireSkipped, err)
	}
	if len(fc.retired) != 1 || fc.retired[0] != "prime-1" {
		t.Fatalf("retired = %v, want the unverified prime-1 retired", fc.retired)
	}
}

// roleLockProbeCommander parks ReconcileRole inside the role lock — at
// ReleaseStaleRoleResources, which the reconcile reaches only after it has taken
// the lock — and records whether a restore reached the manager while it was
// parked.
type roleLockProbeCommander struct {
	*fakeCommander
	entered chan struct{}
	release chan struct{}
	// reconcileParked is true for exactly the span in which ReconcileRole holds
	// the role lock and is stopped inside it.
	reconcileParked        atomic.Bool
	restoreDuringReconcile atomic.Bool
}

func (f *roleLockProbeCommander) ReleaseStaleRoleResources(ctx context.Context, target domain.RoleTarget) (sessionmanager.ReleaseResult, error) {
	f.reconcileParked.Store(true)
	close(f.entered)
	<-f.release
	f.reconcileParked.Store(false)
	return f.fakeCommander.ReleaseStaleRoleResources(ctx, target)
}

func (f *roleLockProbeCommander) RestoreWithMode(ctx context.Context, id domain.SessionID) (sessionmanager.RestoreResult, error) {
	if f.reconcileParked.Load() {
		f.restoreDuringReconcile.Store(true)
	}
	return f.fakeCommander.RestoreWithMode(ctx, id)
}

// restoreHandoffStore signals the moment Restore has resolved the row it is
// about to relaunch. That read is the last thing Restore does BEFORE taking the
// role lock, so it is the handoff point: once it fires, the restore's very next
// act is either to block on the lock (correct) or to call straight into the
// manager (the defect).
type restoreHandoffStore struct {
	*fakeStore
	id     domain.SessionID
	looked chan struct{}
	once   sync.Once
}

func (s *restoreHandoffStore) GetSession(ctx context.Context, id domain.SessionID) (domain.SessionRecord, bool, error) {
	rec, ok, err := s.fakeStore.GetSession(ctx, id)
	if id == s.id {
		s.once.Do(func() { close(s.looked) })
	}
	return rec, ok, err
}

// The manager's restore-side ownership guard reads the live sessions and only
// then relaunches. That is a check-then-act: a ReconcileRole for the same role
// target landing in between creates or releases the canonical role workspace and
// puts two runtimes on one worktree — the exact defect the guard exists to
// prevent. Restore runs in the service that owns the per-RoleTarget lock, so the
// two must serialize on it.
//
// Driven by channel handoff, not timing: the reconcile is parked inside the lock
// before the restore starts, and the restore is released to take the lock before
// the reconcile is let go.
func TestRestoreSerializesWithRoleReconcileForTheSameTarget(t *testing.T) {
	st := newFakeStore()
	st.projects["mer"] = domain.ProjectRecord{ID: "mer"}
	st.sessions["mer-orch-1"] = domain.SessionRecord{
		ID: "mer-orch-1", ProjectID: "mer", Kind: domain.KindOrchestrator, IsTerminated: true,
	}
	handoff := &restoreHandoffStore{fakeStore: st, id: "mer-orch-1", looked: make(chan struct{})}
	fc := &roleLockProbeCommander{
		fakeCommander: &fakeCommander{
			spawnRecord:   domain.SessionRecord{ID: "mer-2", ProjectID: "mer", Kind: domain.KindOrchestrator},
			restoreResult: sessionmanager.RestoreResult{Session: st.sessions["mer-orch-1"], Mode: sessionmanager.RestoreModeNative},
		},
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	svc := &Service{manager: fc, store: handoff}

	reconcileDone := make(chan error, 1)
	go func() {
		_, err := svc.ReconcileRole(context.Background(), domain.OrchestratorTarget("mer"), ReconcileOptions{})
		reconcileDone <- err
	}()
	<-fc.entered // the reconcile now provably holds the orchestrator role lock

	restoreDone := make(chan error, 1)
	go func() {
		_, err := svc.Restore(context.Background(), "mer-orch-1")
		restoreDone <- err
	}()
	<-handoff.looked // the restore has resolved its row and is about to take the lock

	close(fc.release)
	if err := <-reconcileDone; err != nil {
		t.Fatalf("ReconcileRole: %v", err)
	}
	if err := <-restoreDone; err != nil {
		t.Fatalf("Restore: %v", err)
	}

	if fc.restoreDuringReconcile.Load() {
		t.Fatal("Restore reached the manager while a ReconcileRole for the same role target was mid-flight; the reconcile can create or release the canonical role workspace under the restore's ownership check, putting two runtimes on one worktree")
	}
	if fc.restoreCalls != 1 {
		t.Fatalf("RestoreWithMode calls = %d, want 1 — the restore must still run, just after the reconcile", fc.restoreCalls)
	}
}
