package trackerintake

import (
	"context"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func eligibleIssue(nativeRepo string, num string) domain.Issue {
	return domain.Issue{
		ID:        domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: nativeRepo + "#" + num},
		Title:     "Task " + num,
		Body:      "do the thing",
		State:     domain.IssueOpen,
		URL:       "https://github.com/" + nativeRepo + "/issues/" + num,
		Labels:    []string{"agent-ready"},
		Assignees: []string{"alice"},
	}
}

func enabledProject(id, nativeRepo string, paused bool) domain.ProjectRecord {
	return domain.ProjectRecord{
		ID:            id,
		RepoOriginURL: "https://github.com/" + nativeRepo + ".git",
		Paused:        paused,
		Config: domain.ProjectConfig{TrackerIntake: domain.TrackerIntakeConfig{
			Enabled: true, Assignee: "alice",
		}},
	}
}

// A fleet pause short-circuits the whole intake tick: no project is polled and
// no spawn is dispatched, even for an otherwise-eligible issue.
func TestPoll_FleetPausedSkipsAllIntake(t *testing.T) {
	store := &fakeStore{
		fleetPaused: true,
		projects:    []domain.ProjectRecord{enabledProject("demo", "acme/demo", false)},
	}
	tracker := &fakeTracker{issues: []domain.Issue{eligibleIssue("acme/demo", "12")}}
	spawner := &fakeSpawner{}

	if err := New(singleResolver(tracker), store, spawner, Config{Logger: discardLogger()}).Poll(context.Background()); err != nil {
		t.Fatalf("Poll() error = %v", err)
	}
	if len(spawner.calls) != 0 {
		t.Fatalf("fleet paused but %d spawn(s) dispatched", len(spawner.calls))
	}
	if len(tracker.repos) != 0 {
		t.Fatalf("fleet paused but %d tracker repo(s) polled", len(tracker.repos))
	}
}

// A per-project pause excludes only that project from intake; an unpaused
// sibling still intakes normally.
func TestPoll_PausedProjectSkippedSiblingIntakes(t *testing.T) {
	store := &fakeStore{
		projects: []domain.ProjectRecord{
			enabledProject("paused", "acme/paused", true),
			enabledProject("live", "acme/live", false),
		},
	}
	tracker := &fakeTracker{issuesByRepo: map[string][]domain.Issue{
		"acme/paused": {eligibleIssue("acme/paused", "1")},
		"acme/live":   {eligibleIssue("acme/live", "2")},
	}}
	spawner := &fakeSpawner{}

	if err := New(singleResolver(tracker), store, spawner, Config{Logger: discardLogger()}).Poll(context.Background()); err != nil {
		t.Fatalf("Poll() error = %v", err)
	}
	if len(spawner.calls) != 1 {
		t.Fatalf("spawn calls = %d, want 1 (only the unpaused sibling)", len(spawner.calls))
	}
	if got := spawner.calls[0].ProjectID; got != "live" {
		t.Fatalf("spawned for project %q, want live", got)
	}
}
