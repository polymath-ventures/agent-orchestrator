package trackerintake

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	sessionmanager "github.com/aoagents/agent-orchestrator/backend/internal/session_manager"
)

func TestPollSpawnsWorkerForEligibleIssue(t *testing.T) {
	store := &fakeStore{
		projects: []domain.ProjectRecord{{
			ID:            "demo",
			RepoOriginURL: "https://github.com/acme/demo.git",
			Config: domain.ProjectConfig{TrackerIntake: domain.TrackerIntakeConfig{
				Enabled:  true,
				Assignee: "alice",
			}},
		}},
	}
	tracker := &fakeTracker{issues: []domain.Issue{{
		ID:        domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/demo#12"},
		Title:     "Fix login",
		Body:      "The login form submits twice.",
		State:     domain.IssueOpen,
		URL:       "https://github.com/acme/demo/issues/12",
		Labels:    []string{"agent-ready"},
		Assignees: []string{"alice"},
	}}}
	spawner := &fakeSpawner{}

	if err := New(singleResolver(tracker), store, spawner, Config{Logger: discardLogger()}).Poll(context.Background()); err != nil {
		t.Fatalf("Poll() error = %v", err)
	}
	if len(spawner.calls) != 1 {
		t.Fatalf("spawn calls = %d, want 1", len(spawner.calls))
	}
	call := spawner.calls[0]
	if call.ProjectID != "demo" || call.Kind != domain.KindWorker {
		t.Fatalf("spawn config = %+v", call)
	}
	if call.IssueID != "github:acme/demo#12" {
		t.Fatalf("IssueID = %q, want canonical github id", call.IssueID)
	}
	if !strings.Contains(call.Prompt, "Fix login") || !strings.Contains(call.Prompt, "The login form submits twice.") {
		t.Fatalf("prompt missing issue context:\n%s", call.Prompt)
	}
	if len(tracker.filters) != 1 {
		t.Fatalf("tracker filters = %d, want 1", len(tracker.filters))
	}
	if got := tracker.filters[0]; got.State != domain.ListOpen || got.Assignee != "alice" || len(got.Labels) != 0 {
		t.Fatalf("tracker filter = %+v", got)
	}
}

// A prior launch that FAILED (its session is terminated) must not suppress a
// fresh attempt: seenIssueIDs skips terminated rows, so intake retries the
// issue instead of leaving it stranded on a dead session. This is the
// tracker-intake half of #266 — a silently failed launch used to leave a live
// (non-terminated) session that marked the issue serviced forever.
func TestPollRetriesIssueWhoseEarlierLaunchFailed(t *testing.T) {
	store := &fakeStore{
		projects: []domain.ProjectRecord{{
			ID:            "demo",
			RepoOriginURL: "https://github.com/acme/demo.git",
			Config:        domain.ProjectConfig{TrackerIntake: domain.TrackerIntakeConfig{Enabled: true, Assignee: "alice"}},
		}},
		sessions: []domain.SessionRecord{{
			ID:           "demo-1-gen",
			ProjectID:    "demo",
			IssueID:      "github:acme/demo#12",
			IsTerminated: true,
			LastError:    "agent exited before launch completed: invalid session id",
		}},
	}
	tracker := &fakeTracker{issues: []domain.Issue{{
		ID:        domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/demo#12"},
		Title:     "Fix login",
		State:     domain.IssueOpen,
		Assignees: []string{"alice"},
	}}}
	spawner := &fakeSpawner{}

	if err := New(singleResolver(tracker), store, spawner, Config{Logger: discardLogger()}).Poll(context.Background()); err != nil {
		t.Fatalf("Poll() error = %v", err)
	}
	if len(spawner.calls) != 1 {
		t.Fatalf("spawn calls = %d, want 1 — a terminated failed-launch session must not strand the issue", len(spawner.calls))
	}
	if spawner.calls[0].IssueID != "github:acme/demo#12" {
		t.Fatalf("retried issue = %q, want github:acme/demo#12", spawner.calls[0].IssueID)
	}
}

// A live (non-terminated) session for an issue still suppresses a duplicate
// spawn — the retry above must be scoped to failed launches, not every session.
func TestPollDoesNotRespawnIssueWithLiveSession(t *testing.T) {
	store := &fakeStore{
		projects: []domain.ProjectRecord{{
			ID:            "demo",
			RepoOriginURL: "https://github.com/acme/demo.git",
			Config:        domain.ProjectConfig{TrackerIntake: domain.TrackerIntakeConfig{Enabled: true, Assignee: "alice"}},
		}},
		sessions: []domain.SessionRecord{{
			ID:        "demo-1-gen",
			ProjectID: "demo",
			IssueID:   "github:acme/demo#12",
		}},
	}
	tracker := &fakeTracker{issues: []domain.Issue{{
		ID:        domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/demo#12"},
		State:     domain.IssueOpen,
		Assignees: []string{"alice"},
	}}}
	spawner := &fakeSpawner{}

	if err := New(singleResolver(tracker), store, spawner, Config{Logger: discardLogger()}).Poll(context.Background()); err != nil {
		t.Fatalf("Poll() error = %v", err)
	}
	if len(spawner.calls) != 0 {
		t.Fatalf("spawn calls = %d, want 0 — a live session already services the issue", len(spawner.calls))
	}
}

func TestPollConfiguredWorkerTaskPromptPrecedence(t *testing.T) {
	store := &fakeStore{projects: []domain.ProjectRecord{{
		ID:            "demo",
		RepoOriginURL: "https://github.com/acme/demo.git",
		Config: domain.ProjectConfig{
			WorkerTaskPrompt: "/project {issue}\n",
			TrackerIntake:    domain.TrackerIntakeConfig{Enabled: true, Assignee: "alice"},
		},
	}}}
	tracker := &fakeTracker{issues: []domain.Issue{{
		ID: domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/demo#242"}, Title: "must not be injected", Body: "nor this body", State: domain.IssueOpen, Assignees: []string{"alice"},
	}}}
	spawner := &fakeSpawner{}

	err := New(singleResolver(tracker), store, spawner, Config{
		Logger:          discardLogger(),
		ProjectDefaults: domain.ProjectConfig{WorkerTaskPrompt: "/global {issue}"},
	}).Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(spawner.calls) != 1 || spawner.calls[0].Prompt != "/project 242\n" {
		t.Fatalf("spawn calls = %+v, want exact rendered project prompt", spawner.calls)
	}
}

func TestPollUsesGlobalWorkerTaskPrompt(t *testing.T) {
	store := &fakeStore{projects: []domain.ProjectRecord{{
		ID: "demo", RepoOriginURL: "https://github.com/acme/demo.git",
		Config: domain.ProjectConfig{TrackerIntake: domain.TrackerIntakeConfig{Enabled: true, Assignee: "alice"}},
	}}}
	tracker := &fakeTracker{issues: []domain.Issue{{
		ID: domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/demo#242"}, State: domain.IssueOpen, Assignees: []string{"alice"},
	}}}
	spawner := &fakeSpawner{}
	err := New(singleResolver(tracker), store, spawner, Config{Logger: discardLogger(), ProjectDefaults: domain.ProjectConfig{WorkerTaskPrompt: "/global {issue}"}}).Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(spawner.calls) != 1 || spawner.calls[0].Prompt != "/global 242" {
		t.Fatalf("spawn calls = %+v, want exact rendered global prompt", spawner.calls)
	}
}

func TestPollInvalidProjectWorkerTaskPromptDoesNotFallBackOrSpawn(t *testing.T) {
	store := &fakeStore{projects: []domain.ProjectRecord{{
		ID: "demo", RepoOriginURL: "https://github.com/acme/demo.git",
		Config: domain.ProjectConfig{WorkerTaskPrompt: " \n", TrackerIntake: domain.TrackerIntakeConfig{Enabled: true, Assignee: "alice"}},
	}}}
	tracker := &fakeTracker{issues: []domain.Issue{
		{ID: domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/demo#242"}, State: domain.IssueOpen, Assignees: []string{"alice"}},
		{ID: domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/demo#243"}, State: domain.IssueOpen, Assignees: []string{"alice"}},
	}}
	spawner := &fakeSpawner{}
	var logs bytes.Buffer
	err := New(singleResolver(tracker), store, spawner, Config{Logger: slog.New(slog.NewTextHandler(&logs, nil)), ProjectDefaults: domain.ProjectConfig{WorkerTaskPrompt: "/global {issue}"}}).Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(spawner.calls) != 0 {
		t.Fatalf("spawn calls = %+v, want none for invalid project prompt", spawner.calls)
	}
	if got := strings.Count(logs.String(), "invalid worker task prompt configuration"); got != 1 {
		t.Fatalf("invalid-template log count = %d, want one project-level failure; logs=%s", got, logs.String())
	}
}

func TestPollSkipsExistingIssueSessionsAfterRestart(t *testing.T) {
	store := &fakeStore{
		projects: []domain.ProjectRecord{{
			ID:            "demo",
			RepoOriginURL: "https://github.com/acme/demo.git",
			Config:        domain.ProjectConfig{TrackerIntake: domain.TrackerIntakeConfig{Enabled: true, Assignee: "alice"}},
		}},
		sessions: []domain.SessionRecord{{ID: "demo-1", ProjectID: "demo", IssueID: "github:acme/demo#12"}},
	}
	tracker := &fakeTracker{issues: []domain.Issue{{
		ID:        domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/demo#12"},
		Title:     "Already running",
		State:     domain.IssueOpen,
		Assignees: []string{"alice"},
	}}}
	spawner := &fakeSpawner{}

	if err := New(singleResolver(tracker), store, spawner, Config{Logger: discardLogger()}).Poll(context.Background()); err != nil {
		t.Fatalf("Poll() error = %v", err)
	}
	if len(spawner.calls) != 0 {
		t.Fatalf("spawn calls = %d, want 0", len(spawner.calls))
	}
}

func TestPollRespawnsIssueAfterTerminatedSession(t *testing.T) {
	store := &fakeStore{
		projects: []domain.ProjectRecord{{
			ID:            "demo",
			RepoOriginURL: "https://github.com/acme/demo.git",
			Config:        domain.ProjectConfig{TrackerIntake: domain.TrackerIntakeConfig{Enabled: true, Assignee: "alice"}},
		}},
		sessions: []domain.SessionRecord{{ID: "demo-1", ProjectID: "demo", IssueID: "github:acme/demo#12", IsTerminated: true}},
	}
	tracker := &fakeTracker{issues: []domain.Issue{{
		ID:        domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/demo#12"},
		Title:     "Killed session should respawn",
		State:     domain.IssueOpen,
		Assignees: []string{"alice"},
	}}}
	spawner := &fakeSpawner{}

	if err := New(singleResolver(tracker), store, spawner, Config{Logger: discardLogger()}).Poll(context.Background()); err != nil {
		t.Fatalf("Poll() error = %v", err)
	}
	if len(spawner.calls) != 1 || spawner.calls[0].IssueID != "github:acme/demo#12" {
		t.Fatalf("spawn calls = %+v, want one spawn for issue #12 (terminated session should not block respawn)", spawner.calls)
	}
}

func TestSeenIssueIDsExcludesTerminatedSessions(t *testing.T) {
	sessions := []domain.SessionRecord{
		{ID: "demo-1", IssueID: "github:acme/demo#12", IsTerminated: true},
		{ID: "demo-2", IssueID: "github:acme/demo#12", IsTerminated: false},
	}
	seen := seenIssueIDs(sessions, nil)
	if !seen["github:acme/demo#12"] {
		t.Fatal("issue with a live session alongside a terminated one should still be seen")
	}
	if len(seen) != 1 {
		t.Fatalf("seen = %+v, want exactly one issue", seen)
	}
}

func TestSeenIssueIDsIgnoresOnlyTerminatedSession(t *testing.T) {
	sessions := []domain.SessionRecord{
		{ID: "demo-1", IssueID: "github:acme/demo#12", IsTerminated: true},
	}
	seen := seenIssueIDs(sessions, nil)
	if seen["github:acme/demo#12"] {
		t.Fatal("issue with only a terminated session should not be marked as seen")
	}
}

func TestPollSkipsSessionScanWhenIntakeDisabled(t *testing.T) {
	store := &fakeStore{
		projects:    []domain.ProjectRecord{{ID: "demo"}},
		sessionsErr: errors.New("session scan should not run"),
	}

	if err := New(singleResolver(&fakeTracker{}), store, &fakeSpawner{}, Config{Logger: discardLogger()}).Poll(context.Background()); err != nil {
		t.Fatalf("Poll() error = %v, want nil", err)
	}
}

// Fleet pause is an authoritative safety gate: a storage read failure must
// abort the tick before tracker discovery or spawning rather than failing open.
func TestPollFleetPauseReadErrorFailsClosed(t *testing.T) {
	readErr := errors.New("pause store unavailable")
	store := &fakeStore{fleetPausedErr: readErr}
	tracker := &fakeTracker{}
	spawner := &fakeSpawner{}

	err := New(singleResolver(tracker), store, spawner, Config{Logger: discardLogger()}).Poll(context.Background())
	if !errors.Is(err, readErr) {
		t.Fatalf("Poll() error = %v, want %v", err, readErr)
	}
	if len(tracker.repos) != 0 {
		t.Fatalf("tracker calls = %v, want none after fail-closed pause read", tracker.repos)
	}
	if len(spawner.calls) != 0 {
		t.Fatalf("spawn calls = %d, want 0", len(spawner.calls))
	}
}

func TestPollSkipsIneligibleAndInvalidProjects(t *testing.T) {
	store := &fakeStore{
		projects: []domain.ProjectRecord{
			{ID: "off", RepoOriginURL: "https://github.com/acme/off.git"},
			{ID: "broad", RepoOriginURL: "https://github.com/acme/broad.git", Config: domain.ProjectConfig{TrackerIntake: domain.TrackerIntakeConfig{Enabled: true}}},
			{ID: "missing-origin", Config: domain.ProjectConfig{TrackerIntake: domain.TrackerIntakeConfig{Enabled: true, Assignee: "alice"}}},
		},
	}
	tracker := &fakeTracker{issues: []domain.Issue{{
		ID:    domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/off#1"},
		Title: "ignored",
		State: domain.IssueOpen,
	}}}
	spawner := &fakeSpawner{}

	if err := New(singleResolver(tracker), store, spawner, Config{Logger: discardLogger()}).Poll(context.Background()); err != nil {
		t.Fatalf("Poll() error = %v", err)
	}
	if len(tracker.repos) != 0 {
		t.Fatalf("tracker was called for invalid/off projects: %+v", tracker.repos)
	}
	if len(spawner.calls) != 0 {
		t.Fatalf("spawn calls = %d, want 0", len(spawner.calls))
	}
}

func TestPollContinuesAfterTrackerAndSpawnFailures(t *testing.T) {
	store := &fakeStore{projects: []domain.ProjectRecord{
		{ID: "bad", RepoOriginURL: "https://github.com/acme/bad.git", Config: domain.ProjectConfig{TrackerIntake: domain.TrackerIntakeConfig{Enabled: true, Assignee: "alice"}}},
		{ID: "good", RepoOriginURL: "https://github.com/acme/good.git", Config: domain.ProjectConfig{TrackerIntake: domain.TrackerIntakeConfig{Enabled: true, Assignee: "alice"}}},
	}}
	tracker := &fakeTracker{
		failRepos: map[string]error{"acme/bad": errors.New("rate limited")},
		issuesByRepo: map[string][]domain.Issue{
			"acme/good": {
				{ID: domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/good#1"}, Title: "first", State: domain.IssueOpen, Assignees: []string{"alice"}},
				{ID: domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/good#2"}, Title: "second", State: domain.IssueOpen, Assignees: []string{"alice"}},
			},
		},
	}
	spawner := &fakeSpawner{failIssue: domain.IssueID("github:acme/good#1")}

	if err := New(singleResolver(tracker), store, spawner, Config{Logger: discardLogger()}).Poll(context.Background()); err != nil {
		t.Fatalf("Poll() error = %v", err)
	}
	if len(spawner.calls) != 2 {
		t.Fatalf("spawn attempts = %d, want 2", len(spawner.calls))
	}
	if spawner.calls[1].IssueID != "github:acme/good#2" {
		t.Fatalf("second spawn issue = %q", spawner.calls[1].IssueID)
	}
}

func TestPollBacksOffProjectAfterFailure(t *testing.T) {
	now := time.Date(2026, 6, 27, 10, 0, 0, 0, time.UTC)
	store := &fakeStore{projects: []domain.ProjectRecord{{
		ID:            "demo",
		RepoOriginURL: "https://github.com/acme/demo.git",
		Config:        domain.ProjectConfig{TrackerIntake: domain.TrackerIntakeConfig{Enabled: true, Assignee: "alice"}},
	}}}
	tracker := &fakeTracker{failRepos: map[string]error{"acme/demo": errors.New("rate limited")}}
	observer := New(singleResolver(tracker), store, &fakeSpawner{}, Config{
		Clock:          func() time.Time { return now },
		FailureBackoff: time.Minute,
		Logger:         discardLogger(),
	})

	if err := observer.Poll(context.Background()); err != nil {
		t.Fatalf("first Poll() error = %v", err)
	}
	if len(tracker.repos) != 1 {
		t.Fatalf("tracker calls after first poll = %d, want 1", len(tracker.repos))
	}

	if err := observer.Poll(context.Background()); err != nil {
		t.Fatalf("second Poll() error = %v", err)
	}
	if len(tracker.repos) != 1 {
		t.Fatalf("tracker calls during backoff = %d, want still 1", len(tracker.repos))
	}

	now = now.Add(time.Minute + time.Nanosecond)
	if err := observer.Poll(context.Background()); err != nil {
		t.Fatalf("third Poll() error = %v", err)
	}
	if len(tracker.repos) != 2 {
		t.Fatalf("tracker calls after backoff = %d, want 2", len(tracker.repos))
	}
}

func TestPollSkipsNonOpenIssueStates(t *testing.T) {
	store := &fakeStore{projects: []domain.ProjectRecord{{
		ID:            "demo",
		RepoOriginURL: "https://github.com/acme/demo.git",
		Config:        domain.ProjectConfig{TrackerIntake: domain.TrackerIntakeConfig{Enabled: true, Assignee: "alice"}},
	}}}
	tracker := &fakeTracker{issues: []domain.Issue{
		{ID: domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/demo#1"}, Title: "already active", State: domain.IssueInProgress, Assignees: []string{"alice"}},
		{ID: domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/demo#2"}, Title: "ready", State: domain.IssueOpen, Assignees: []string{"alice"}},
	}}
	spawner := &fakeSpawner{}

	if err := New(singleResolver(tracker), store, spawner, Config{Logger: discardLogger()}).Poll(context.Background()); err != nil {
		t.Fatalf("Poll() error = %v", err)
	}
	if len(spawner.calls) != 1 || spawner.calls[0].IssueID != "github:acme/demo#2" {
		t.Fatalf("spawn calls = %+v, want only open issue #2", spawner.calls)
	}
}

func TestPollAppliesLocalEligibilityFilter(t *testing.T) {
	store := &fakeStore{projects: []domain.ProjectRecord{{
		ID:            "demo",
		RepoOriginURL: "https://github.com/acme/demo.git",
		Config:        domain.ProjectConfig{TrackerIntake: domain.TrackerIntakeConfig{Enabled: true, Assignee: "alice"}},
	}}}
	tracker := &fakeTracker{issues: []domain.Issue{
		{ID: domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/demo#1"}, Title: "unassigned", State: domain.IssueOpen},
		{ID: domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/demo#2"}, Title: "wrong assignee", State: domain.IssueOpen, Assignees: []string{"bob"}},
		{ID: domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/demo#3"}, Title: "eligible", State: domain.IssueOpen, Labels: []string{"Agent-Ready"}, Assignees: []string{"Alice"}},
	}}
	spawner := &fakeSpawner{}

	if err := New(singleResolver(tracker), store, spawner, Config{Logger: discardLogger()}).Poll(context.Background()); err != nil {
		t.Fatalf("Poll() error = %v", err)
	}
	if len(spawner.calls) != 1 || spawner.calls[0].IssueID != "github:acme/demo#3" {
		t.Fatalf("spawn calls = %+v, want only eligible issue #3", spawner.calls)
	}
}

func TestIssueMatchesConfigAssigneeSpecialValues(t *testing.T) {
	assigned := domain.Issue{Assignees: []string{"alice"}}
	unassigned := domain.Issue{}
	if !issueMatchesConfig(assigned, domain.TrackerIntakeConfig{Assignee: "*"}) {
		t.Fatal("assigned issue should match assignee=*")
	}
	if issueMatchesConfig(unassigned, domain.TrackerIntakeConfig{Assignee: "*"}) {
		t.Fatal("unassigned issue should not match assignee=*")
	}
	if !issueMatchesConfig(unassigned, domain.TrackerIntakeConfig{Assignee: "none"}) {
		t.Fatal("unassigned issue should match assignee=none")
	}
	if issueMatchesConfig(assigned, domain.TrackerIntakeConfig{Assignee: "none"}) {
		t.Fatal("assigned issue should not match assignee=none")
	}
}

func TestBuildIssuePromptCapsLargeIssueBody(t *testing.T) {
	prompt := BuildIssuePrompt(domain.Issue{
		ID:    domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/demo#99"},
		Title: "Large issue",
		URL:   "https://github.com/acme/demo/issues/99",
		Body:  strings.Repeat("body ", 2000),
	}, domain.TrackerRepo{Provider: domain.TrackerProviderGitHub, Native: "acme/demo"})
	if len(prompt) > maxIntakePromptLen {
		t.Fatalf("prompt length = %d, want <= %d", len(prompt), maxIntakePromptLen)
	}
	if !strings.Contains(prompt, "Issue content truncated") {
		t.Fatalf("prompt missing truncation notice:\n%s", prompt)
	}
	if !strings.Contains(prompt, "https://github.com/acme/demo/issues/99") {
		t.Fatalf("prompt missing issue URL:\n%s", prompt)
	}
	if !strings.HasSuffix(prompt, intakePromptFooter) {
		t.Fatalf("prompt missing footer:\n%s", prompt)
	}
}

func TestBuildIssuePromptPreservesLegacyBytes(t *testing.T) {
	issue := domain.Issue{
		ID:        domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/demo#12"},
		Title:     "Fix login",
		URL:       "https://github.com/acme/demo/issues/12",
		Labels:    []string{"bug", "agent-ready"},
		Assignees: []string{"alice"},
		Body:      "The login form submits twice.\n",
	}
	// The opening line deliberately changed with #298: it addresses the issue
	// (`12`) instead of naming the storage key (`github:acme/demo#12`), which no
	// tracker CLI accepts and which the configured-template path never emitted.
	// Every other byte of this legacy prompt is unchanged.
	want := `Work on tracker issue 12.

Title: Fix login
URL: https://github.com/acme/demo/issues/12
Labels: bug, agent-ready
Assignees: alice

Body:
The login form submits twice.

Implement the requested change in this repository, run the relevant checks, and open or update a pull request when ready.`
	scope := domain.TrackerRepo{Provider: domain.TrackerProviderGitHub, Native: "acme/demo"}
	if got := BuildIssuePrompt(issue, scope); got != want {
		t.Fatalf("intake prompt changed from legacy bytes:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestTrackerRepoUsesConfiguredRepo(t *testing.T) {
	project := domain.ProjectRecord{
		ID:            "demo",
		RepoOriginURL: "https://github.com/wrong/repo.git",
		Config: domain.ProjectConfig{TrackerIntake: domain.TrackerIntakeConfig{
			Enabled:  true,
			Repo:     "acme/demo",
			Assignee: "alice",
		}},
	}
	repo, ok := trackerRepo(project, project.Config.TrackerIntake.WithDefaults())
	if !ok {
		t.Fatal("trackerRepo ok = false")
	}
	if repo.Native != "acme/demo" {
		t.Fatalf("repo.Native = %q, want acme/demo", repo.Native)
	}
}

// capIntakeFixtures returns a one-project store and a tracker holding one
// eligible open issue (github:acme/demo#12), the shared setup for the cap
// deferral tests.
func capIntakeFixtures() (*fakeStore, *fakeTracker) {
	store := &fakeStore{projects: []domain.ProjectRecord{{
		ID:            "demo",
		RepoOriginURL: "https://github.com/acme/demo.git",
		Config:        domain.ProjectConfig{TrackerIntake: domain.TrackerIntakeConfig{Enabled: true, Assignee: "alice"}},
	}}}
	tracker := &fakeTracker{issues: []domain.Issue{{
		ID:        domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/demo#12"},
		Title:     "capped",
		State:     domain.IssueOpen,
		Assignees: []string{"alice"},
	}}}
	return store, tracker
}

// A spawn refused because the project is at its worker cap is a deferral, not a
// failure: the issue is attempted but left unclaimed, and the project does not
// enter failure backoff.
func TestPollDefersCappedIssueWithoutBackoff(t *testing.T) {
	store, tracker := capIntakeFixtures()
	spawner := &fakeSpawner{capIssue: "github:acme/demo#12", capActive: true}
	observer := New(singleResolver(tracker), store, spawner, Config{Logger: discardLogger()})

	if err := observer.Poll(context.Background()); err != nil {
		t.Fatalf("Poll() error = %v", err)
	}
	if len(spawner.calls) != 1 {
		t.Fatalf("spawn attempts = %d, want 1 — the capped issue is still attempted", len(spawner.calls))
	}
	if len(observer.backoffUntil) != 0 {
		t.Fatalf("backoffUntil = %v, want empty — a cap deferral must not trigger failure backoff", observer.backoffUntil)
	}
}

// Once a poll hits the worker cap, the normal worker pool is known full for the
// rest of that project pass. Later issues stay unseen without burning more
// spawn attempts that would return the same capacity refusal.
func TestPollMemoizesWorkerCapForRestOfProjectPass(t *testing.T) {
	store, tracker := capIntakeFixtures()
	tracker.issues = append(tracker.issues, domain.Issue{
		ID:        domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/demo#13"},
		Title:     "also capped",
		State:     domain.IssueOpen,
		Assignees: []string{"alice"},
	})
	spawner := &fakeSpawner{failErrByIssue: map[domain.IssueID]error{
		"github:acme/demo#12": apierr.Conflict("WORKER_CONCURRENCY_CAP", "session: worker concurrency cap reached", nil),
	}}
	observer := New(singleResolver(tracker), store, spawner, Config{Logger: discardLogger()})

	if err := observer.Poll(context.Background()); err != nil {
		t.Fatalf("Poll() error = %v", err)
	}
	if len(spawner.calls) != 1 {
		t.Fatalf("spawn attempts = %d, want only the first cap collision", len(spawner.calls))
	}
	if len(observer.backoffUntil) != 0 {
		t.Fatalf("backoffUntil = %v, want empty — cap memoization must not trigger failure backoff", observer.backoffUntil)
	}
}

// A genuine (non-cap) spawn failure still trips the existing failure backoff,
// unchanged by the cap deferral path.
func TestPollGenuineSpawnFailureStillBacksOff(t *testing.T) {
	now := time.Date(2026, 6, 27, 10, 0, 0, 0, time.UTC)
	store, tracker := capIntakeFixtures()
	spawner := &fakeSpawner{failIssue: "github:acme/demo#12"}
	observer := New(singleResolver(tracker), store, spawner, Config{
		Clock:          func() time.Time { return now },
		FailureBackoff: time.Minute,
		Logger:         discardLogger(),
	})

	if err := observer.Poll(context.Background()); err != nil {
		t.Fatalf("Poll() error = %v", err)
	}
	if len(spawner.calls) != 1 {
		t.Fatalf("spawn attempts = %d, want 1", len(spawner.calls))
	}
	if _, ok := observer.backoffUntil["demo"]; !ok {
		t.Fatal("a genuine spawn failure must put the project into failure backoff")
	}
}

// An issue deferred at the cap is picked up on a later poll once capacity frees.
func TestPollRetriesDeferredIssueOnceCapacityFrees(t *testing.T) {
	store, tracker := capIntakeFixtures()
	spawner := &fakeSpawner{capIssue: "github:acme/demo#12", capActive: true}
	observer := New(singleResolver(tracker), store, spawner, Config{Logger: discardLogger()})

	// First poll: at the cap, so the issue is deferred and left unclaimed.
	if err := observer.Poll(context.Background()); err != nil {
		t.Fatalf("first Poll() error = %v", err)
	}
	if len(spawner.calls) != 1 {
		t.Fatalf("first poll spawn attempts = %d, want 1", len(spawner.calls))
	}

	// Capacity frees; the next poll spawns the previously-deferred issue.
	spawner.capActive = false
	if err := observer.Poll(context.Background()); err != nil {
		t.Fatalf("second Poll() error = %v", err)
	}
	if len(spawner.calls) != 2 {
		t.Fatalf("spawn attempts after capacity freed = %d, want 2 — the deferred issue must be retried", len(spawner.calls))
	}
	if spawner.calls[1].IssueID != "github:acme/demo#12" {
		t.Fatalf("retried issue = %q, want github:acme/demo#12", spawner.calls[1].IssueID)
	}
}

func singleResolver(tracker ports.Tracker) TrackerResolver {
	return SingleTrackerResolver{Provider: domain.TrackerProviderGitHub, Adapter: tracker}
}

type fakeStore struct {
	projects       []domain.ProjectRecord
	sessions       []domain.SessionRecord
	sessionsErr    error
	fleetPaused    bool
	fleetPausedErr error
}

func (f *fakeStore) ListProjects(context.Context) ([]domain.ProjectRecord, error) {
	return append([]domain.ProjectRecord(nil), f.projects...), nil
}

func (f *fakeStore) GetFleetPaused(context.Context) (bool, error) {
	return f.fleetPaused, f.fleetPausedErr
}

func (f *fakeStore) ListAllSessions(context.Context) ([]domain.SessionRecord, error) {
	return append([]domain.SessionRecord(nil), f.sessions...), f.sessionsErr
}

type fakeTracker struct {
	issues       []domain.Issue
	issuesByRepo map[string][]domain.Issue
	failRepos    map[string]error
	repos        []domain.TrackerRepo
	filters      []domain.ListFilter
}

func (f *fakeTracker) Get(context.Context, domain.TrackerID) (domain.Issue, error) {
	return domain.Issue{}, nil
}

func (f *fakeTracker) List(_ context.Context, repo domain.TrackerRepo, filter domain.ListFilter) ([]domain.Issue, error) {
	f.repos = append(f.repos, repo)
	f.filters = append(f.filters, filter)
	if err := f.failRepos[repo.Native]; err != nil {
		return nil, err
	}
	if f.issuesByRepo != nil {
		return append([]domain.Issue(nil), f.issuesByRepo[repo.Native]...), nil
	}
	return append([]domain.Issue(nil), f.issues...), nil
}

func (f *fakeTracker) Preflight(context.Context) error { return nil }

type fakeSpawner struct {
	calls     []ports.SpawnConfig
	failIssue domain.IssueID
	// capIssue returns the concurrency-cap sentinel while capActive is true,
	// modeling a project sitting at its worker cap. The sentinel is wrapped with
	// %w exactly as Manager.Spawn (through the service's pass-through) does, so
	// the test exercises the same errors.Is traversal the daemon relies on.
	capIssue  domain.IssueID
	capActive bool
	// pausedIssue returns the project-paused sentinel while pausedActive is
	// true, modeling a pause gate racing the intake poll.
	pausedIssue    domain.IssueID
	pausedActive   bool
	failErrByIssue map[domain.IssueID]error
}

func (f *fakeSpawner) Spawn(_ context.Context, cfg ports.SpawnConfig) (domain.Session, int, int, error) {
	f.calls = append(f.calls, cfg)
	if f.failErrByIssue != nil {
		if err := f.failErrByIssue[cfg.IssueID]; err != nil {
			return domain.Session{}, 0, 0, err
		}
	}
	if cfg.IssueID == f.failIssue {
		return domain.Session{}, 0, 0, errors.New("spawn failed")
	}
	if f.capActive && cfg.IssueID == f.capIssue {
		return domain.Session{}, 0, 0, fmt.Errorf("spawn: %w", sessionmanager.ErrWorkerConcurrencyCap)
	}
	if f.pausedActive && cfg.IssueID == f.pausedIssue {
		return domain.Session{}, 0, 0, fmt.Errorf("spawn: %w", sessionmanager.ErrProjectPaused)
	}
	return domain.Session{SessionRecord: domain.SessionRecord{ID: domain.SessionID(string(cfg.ProjectID) + "-1"), ProjectID: cfg.ProjectID, IssueID: cfg.IssueID, Kind: cfg.Kind}}, len(cfg.Prompt), 0, nil
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// A pause gate racing the poll refuses the spawn; that is an operator state,
// not a fault — the issue defers without tripping the failure backoff, so
// intake resumes immediately after the project is unpaused.
func TestPollDefersPausedProjectIssueWithoutBackoff(t *testing.T) {
	store, tracker := capIntakeFixtures()
	spawner := &fakeSpawner{pausedIssue: "github:acme/demo#12", pausedActive: true}
	observer := New(singleResolver(tracker), store, spawner, Config{Logger: discardLogger()})

	if err := observer.Poll(context.Background()); err != nil {
		t.Fatalf("Poll() error = %v", err)
	}
	if len(spawner.calls) != 1 {
		t.Fatalf("spawn attempts = %d, want 1", len(spawner.calls))
	}
	if len(observer.backoffUntil) != 0 {
		t.Fatalf("backoffUntil = %v, want empty — a pause deferral must not trigger failure backoff", observer.backoffUntil)
	}
}

// A worker-mix bucket/capacity refusal is a deferral, not an intake fault. The
// service maps these to API conflict codes, so the observer must recognize the
// codes and retry the issue on later polls.
func TestPollDefersWorkerMixCapacityWithoutBackoff(t *testing.T) {
	cases := []struct {
		name string
		code string
	}{
		{"selected bucket down", "WORKER_MIX_BUCKET_DOWN"},
		{"mix exhausted", "WORKER_MIX_EXHAUSTED"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Date(2026, 7, 8, 10, 0, 0, 0, time.UTC)
			store, tracker := capIntakeFixtures()
			spawner := &fakeSpawner{failErrByIssue: map[domain.IssueID]error{
				"github:acme/demo#12": apierr.Conflict(tc.code, "worker mix capacity unavailable", nil),
			}}
			observer := New(singleResolver(tracker), store, spawner, Config{
				Clock:          func() time.Time { return now },
				FailureBackoff: time.Hour,
				Logger:         discardLogger(),
			})

			if err := observer.Poll(context.Background()); err != nil {
				t.Fatalf("first Poll() error = %v", err)
			}
			if len(spawner.calls) != 1 {
				t.Fatalf("first poll spawn attempts = %d, want 1", len(spawner.calls))
			}
			if len(observer.backoffUntil) != 0 {
				t.Fatalf("backoffUntil = %v, want empty — worker-mix capacity deferral must not trigger failure backoff", observer.backoffUntil)
			}

			spawner.failErrByIssue = nil
			if err := observer.Poll(context.Background()); err != nil {
				t.Fatalf("second Poll() error = %v", err)
			}
			if len(spawner.calls) != 2 {
				t.Fatalf("spawn attempts after capacity recovers = %d, want 2", len(spawner.calls))
			}
			if spawner.calls[1].IssueID != "github:acme/demo#12" {
				t.Fatalf("retried issue = %q, want github:acme/demo#12", spawner.calls[1].IssueID)
			}
		})
	}
}

// Regression for #298 defect 1. Intake writes `github:owner/repo#N` but the
// dedup set was built from the raw stored value, so a session spawned with the
// bare native id the CLI and docs advertise never matched the lookup key and
// intake spawned a fresh duplicate worker on every tick, indefinitely.
func TestPollDoesNotRespawnIssueHeldByNonCanonicalSession(t *testing.T) {
	cases := []struct {
		name    string
		issueID domain.IssueID
	}{
		{name: "bare number", issueID: "12"},
		{name: "hash prefixed", issueID: "#12"},
		{name: "provider native", issueID: "acme/demo#12"},
		{name: "issue url", issueID: "https://github.com/acme/demo/issues/12"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeStore{
				projects: []domain.ProjectRecord{{
					ID:            "demo",
					RepoOriginURL: "https://github.com/acme/demo.git",
					Config:        domain.ProjectConfig{TrackerIntake: domain.TrackerIntakeConfig{Enabled: true, Assignee: "alice"}},
				}},
				sessions: []domain.SessionRecord{{ID: "demo-1", ProjectID: "demo", IssueID: tc.issueID}},
			}
			tracker := &fakeTracker{issues: []domain.Issue{{
				ID:        domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/demo#12"},
				State:     domain.IssueOpen,
				Assignees: []string{"alice"},
			}}}
			spawner := &fakeSpawner{}

			if err := New(singleResolver(tracker), store, spawner, Config{Logger: discardLogger()}).Poll(context.Background()); err != nil {
				t.Fatalf("Poll() error = %v", err)
			}
			if len(spawner.calls) != 0 {
				t.Fatalf("spawn calls = %+v, want 0 — session %q already services acme/demo#12", spawner.calls, tc.issueID)
			}
		})
	}
}

// A non-canonical id belonging to a different project must not suppress intake:
// bare ids are only equivalent to a canonical id within their own project's
// tracker scope.
func TestPollRespawnsWhenNonCanonicalSessionBelongsToAnotherProject(t *testing.T) {
	store := &fakeStore{
		projects: []domain.ProjectRecord{{
			ID:            "demo",
			RepoOriginURL: "https://github.com/acme/demo.git",
			Config:        domain.ProjectConfig{TrackerIntake: domain.TrackerIntakeConfig{Enabled: true, Assignee: "alice"}},
		}},
		sessions: []domain.SessionRecord{{ID: "other-1", ProjectID: "other", IssueID: "12"}},
	}
	tracker := &fakeTracker{issues: []domain.Issue{{
		ID:        domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/demo#12"},
		State:     domain.IssueOpen,
		Assignees: []string{"alice"},
	}}}
	spawner := &fakeSpawner{}

	if err := New(singleResolver(tracker), store, spawner, Config{Logger: discardLogger()}).Poll(context.Background()); err != nil {
		t.Fatalf("Poll() error = %v", err)
	}
	if len(spawner.calls) != 1 || spawner.calls[0].IssueID != "github:acme/demo#12" {
		t.Fatalf("spawn calls = %+v, want one spawn: project \"other\"'s bare 12 is a different issue", spawner.calls)
	}
}

// Regression for #298 defect 2. The `no-ao` label documents itself as
// "Opt-out: ao must not auto-work this issue" and was read by nothing.
func TestPollSkipsIssueCarryingOptOutLabel(t *testing.T) {
	store := &fakeStore{projects: []domain.ProjectRecord{{
		ID:            "demo",
		RepoOriginURL: "https://github.com/acme/demo.git",
		Config:        domain.ProjectConfig{TrackerIntake: domain.TrackerIntakeConfig{Enabled: true, Assignee: "alice"}},
	}}}
	tracker := &fakeTracker{issues: []domain.Issue{
		{ID: domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/demo#1"}, Title: "opted out", State: domain.IssueOpen, Labels: []string{"no-ao", "feature"}, Assignees: []string{"alice"}},
		{ID: domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/demo#2"}, Title: "eligible", State: domain.IssueOpen, Labels: []string{"feature"}, Assignees: []string{"alice"}},
	}}
	spawner := &fakeSpawner{}

	if err := New(singleResolver(tracker), store, spawner, Config{Logger: discardLogger()}).Poll(context.Background()); err != nil {
		t.Fatalf("Poll() error = %v", err)
	}
	if len(spawner.calls) != 1 || spawner.calls[0].IssueID != "github:acme/demo#2" {
		t.Fatalf("spawn calls = %+v, want only issue #2 — #1 carries the opt-out label", spawner.calls)
	}
}

func TestIssueMatchesConfigOptOutLabel(t *testing.T) {
	cfg := domain.TrackerIntakeConfig{Enabled: true, Assignee: "alice"}
	assigned := domain.Issue{Assignees: []string{"alice"}}
	optedOut := domain.Issue{Assignees: []string{"alice"}, Labels: []string{"feature", "No-AO"}}

	if !issueMatchesConfig(assigned, cfg) {
		t.Fatal("issue without the opt-out label should match")
	}
	if issueMatchesConfig(optedOut, cfg) {
		t.Fatal("the default opt-out label should exclude the issue, case-insensitively")
	}

	custom := domain.TrackerIntakeConfig{Enabled: true, Assignee: "alice", OptOutLabel: "hands-off"}
	if issueMatchesConfig(domain.Issue{Assignees: []string{"alice"}, Labels: []string{"hands-off"}}, custom) {
		t.Fatal("configured opt-out label should exclude the issue")
	}
	if !issueMatchesConfig(optedOut, custom) {
		t.Fatal("a configured label replaces the default rather than adding to it")
	}

	disabled := domain.TrackerIntakeConfig{Enabled: true, Assignee: "alice", OptOutLabel: "none"}
	if !issueMatchesConfig(optedOut, disabled) {
		t.Fatal(`optOutLabel "none" should disable the opt-out`)
	}

	// The opt-out overrides eligibility rather than joining it: an unassigned
	// issue matching assignee=none is still excluded when it opts out.
	if issueMatchesConfig(domain.Issue{Labels: []string{"no-ao"}}, domain.TrackerIntakeConfig{Enabled: true, Assignee: "none"}) {
		t.Fatal("opt-out should override the assignee=none rule")
	}
}

// seenIssueIDs is the dedup key intake looks issues up by; it must accept the
// non-canonical shapes older sessions hold, but only inside their own project.
func TestSeenIssueIDsResolvesNonCanonicalSessionsPerProject(t *testing.T) {
	projects := []domain.ProjectRecord{
		{ID: "demo", RepoOriginURL: "https://github.com/acme/demo.git"},
		{ID: "other", RepoOriginURL: "https://github.com/acme/other.git"},
	}
	sessions := []domain.SessionRecord{
		{ID: "demo-1", ProjectID: "demo", IssueID: "12"},
		{ID: "other-1", ProjectID: "other", IssueID: "#34"},
		{ID: "demo-2", ProjectID: "demo", IssueID: "56", IsTerminated: true},
		{ID: "demo-3", ProjectID: "demo", IssueID: "ISS-1"},
	}

	seen := seenIssueIDs(sessions, projects)

	if !seen["github:acme/demo#12"] {
		t.Error("a bare id should cover its own project's canonical issue")
	}
	if !seen["github:acme/other#34"] {
		t.Error("a hash-prefixed id should cover its own project's canonical issue")
	}
	if seen["github:acme/other#12"] {
		t.Error("a bare id must not cover another project's issue 12")
	}
	if seen["github:acme/demo#56"] {
		t.Error("a terminated session should not cover its issue")
	}
	if !seen["ISS-1"] {
		t.Error("an unresolvable id should still cover itself verbatim")
	}
}

// An issue outside the polled repository keeps its qualifier, so the legacy
// prompt cannot tell a worker to work on its own repo's issue of that number.
func TestBuildIssuePromptQualifiesAnIssueOutsideTheScope(t *testing.T) {
	issue := domain.Issue{
		ID:    domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/other#12"},
		Title: "Elsewhere",
	}
	scope := domain.TrackerRepo{Provider: domain.TrackerProviderGitHub, Native: "acme/demo"}
	if got := BuildIssuePrompt(issue, scope); !strings.HasPrefix(got, "Work on tracker issue acme/other#12.") {
		t.Fatalf("prompt = %q, want it to open with the qualified reference", got)
	}
}

// Two self-managed GitLab instances can share a project path. The stored
// canonical id cannot tell them apart, so coverage must not either — a live
// session on one instance must not suppress intake for the other's issue.
func TestSeenIssueIDsSeparatesSelfManagedGitLabInstances(t *testing.T) {
	projects := []domain.ProjectRecord{
		{
			ID:            "alpha",
			RepoOriginURL: "https://gitlab.alpha.example/group/proj.git",
			Config:        domain.ProjectConfig{TrackerIntake: domain.TrackerIntakeConfig{Enabled: true, Assignee: "alice", Provider: domain.TrackerProviderGitLab}},
		},
		{
			ID:            "beta",
			RepoOriginURL: "https://gitlab.beta.example/group/proj.git",
			Config:        domain.ProjectConfig{TrackerIntake: domain.TrackerIntakeConfig{Enabled: true, Assignee: "alice", Provider: domain.TrackerProviderGitLab}},
		},
	}
	sessions := []domain.SessionRecord{{ID: "alpha-1", ProjectID: "alpha", IssueID: "gitlab:group/proj#7"}}

	seen := seenIssueIDs(sessions, projects)

	alpha := dedupKey(domain.TrackerID{Provider: domain.TrackerProviderGitLab, Native: "group/proj#7", Host: "gitlab.alpha.example"})
	beta := dedupKey(domain.TrackerID{Provider: domain.TrackerProviderGitLab, Native: "group/proj#7", Host: "gitlab.beta.example"})
	if !seen[alpha] {
		t.Fatal("the instance that has a live session should be covered")
	}
	if seen[beta] {
		t.Fatal("a different instance sharing the project path must not be suppressed")
	}
}
