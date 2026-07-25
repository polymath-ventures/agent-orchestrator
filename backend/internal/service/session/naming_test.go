package session

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// The title reaches the manager on the same tracker fetch that already builds
// the issue context — one lookup, on every spawn path, in the one service they
// all funnel through.
func TestSpawnResolvesTheWorkItemTitleForNaming(t *testing.T) {
	st := newFakeStore()
	st.projects["mer"] = domain.ProjectRecord{ID: "mer", RepoOriginURL: "https://github.com/acme/repo.git"}
	fc := &fakeCommander{}
	tracker := &fakeTracker{issue: domain.Issue{
		ID:    domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/repo#42"},
		Title: "Fix generated prompts",
	}}
	svc := NewWithDeps(Deps{Manager: fc, Store: st, Tracker: tracker})

	if _, _, _, err := svc.Spawn(context.Background(), ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker, IssueID: "42"}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if fc.spawnedCfg.IssueTitle != "Fix generated prompts" {
		t.Fatalf("IssueTitle = %q, want the tracker title", fc.spawnedCfg.IssueTitle)
	}
	if len(tracker.ids) != 1 {
		t.Fatalf("tracker calls = %d, want a single fetch shared with the issue context", len(tracker.ids))
	}
}

// A caller that pre-supplies the issue context still needs a title, or its
// sessions silently degrade to head-only names.
func TestSpawnResolvesTheTitleEvenWithPreSuppliedIssueContext(t *testing.T) {
	st := newFakeStore()
	st.projects["mer"] = domain.ProjectRecord{ID: "mer", RepoOriginURL: "https://github.com/acme/repo.git"}
	fc := &fakeCommander{}
	tracker := &fakeTracker{issue: domain.Issue{Title: "Fix generated prompts"}}
	svc := NewWithDeps(Deps{Manager: fc, Store: st, Tracker: tracker})

	if _, _, _, err := svc.Spawn(context.Background(), ports.SpawnConfig{
		ProjectID:    "mer",
		Kind:         domain.KindWorker,
		IssueID:      "42",
		IssueContext: "already fetched by the caller",
	}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if fc.spawnedCfg.IssueTitle != "Fix generated prompts" {
		t.Fatalf("IssueTitle = %q, want the tracker title", fc.spawnedCfg.IssueTitle)
	}
	if fc.spawnedCfg.IssueContext != "already fetched by the caller" {
		t.Fatalf("IssueContext = %q, want the caller's value preserved", fc.spawnedCfg.IssueContext)
	}
}

// A tracker outage costs the title slug, not the spawn.
func TestSpawnTitleLookupFailureDegradesTheName(t *testing.T) {
	st := newFakeStore()
	st.projects["mer"] = domain.ProjectRecord{ID: "mer", RepoOriginURL: "https://github.com/acme/repo"}
	fc := &fakeCommander{}
	svc := NewWithDeps(Deps{Manager: fc, Store: st, Tracker: &fakeTracker{err: errors.New("tracker unavailable")}})

	if _, _, _, err := svc.Spawn(context.Background(), ports.SpawnConfig{ProjectID: "mer", Kind: domain.KindWorker, IssueID: "42"}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if fc.spawnedCfg.IssueTitle != "" {
		t.Fatalf("IssueTitle = %q, want empty so the name degrades to its head", fc.spawnedCfg.IssueTitle)
	}
}

// A rename that stops at the database row is the current behavior and the whole
// reason the sidebar and the harness disagree.
func TestRenameDeliversTheNewNameToTheRunningHarness(t *testing.T) {
	st := newFakeStore()
	st.projects["mer"] = domain.ProjectRecord{ID: "mer"}
	st.sessions["mer-1"] = domain.SessionRecord{ID: "mer-1", ProjectID: "mer"}
	fc := &fakeCommander{}
	svc := NewWithDeps(Deps{Manager: fc, Store: st})

	if err := svc.Rename(context.Background(), "mer-1", "ao #7 renamed"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if st.sessions["mer-1"].DisplayName != "ao #7 renamed" {
		t.Fatalf("persisted display name = %q, want the new name", st.sessions["mer-1"].DisplayName)
	}
	if fc.deliveredNames != 1 {
		t.Fatalf("harness name deliveries = %d, want 1", fc.deliveredNames)
	}
}

// The harness write is best-effort; the rename itself already succeeded.
func TestRenameSucceedsWhenHarnessDeliveryFails(t *testing.T) {
	st := newFakeStore()
	st.projects["mer"] = domain.ProjectRecord{ID: "mer"}
	st.sessions["mer-1"] = domain.SessionRecord{ID: "mer-1", ProjectID: "mer"}
	fc := &fakeCommander{deliverNameErr: errors.New("pane write failed")}
	svc := NewWithDeps(Deps{Manager: fc, Store: st})

	if err := svc.Rename(context.Background(), "mer-1", "ao #7 renamed"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if st.sessions["mer-1"].DisplayName != "ao #7 renamed" {
		t.Fatalf("persisted display name = %q, want the new name", st.sessions["mer-1"].DisplayName)
	}
}

// An unknown session never reaches the harness.
func TestRenameOfUnknownSessionDeliversNothing(t *testing.T) {
	st := newFakeStore()
	fc := &fakeCommander{}
	svc := NewWithDeps(Deps{Manager: fc, Store: st})

	if err := svc.Rename(context.Background(), "nope-1", "ao #7 renamed"); err == nil {
		t.Fatal("Rename of an unknown session succeeded, want a not-found error")
	}
	if fc.deliveredNames != 0 {
		t.Fatalf("harness name deliveries = %d, want 0", fc.deliveredNames)
	}
}

// The rename endpoint enforced only non-emptiness, so a name well over the cap
// every other path obeys could be persisted — and, now that AO delivers the
// name, typed into the harness too.
func TestRenameRejectsAnOverCapName(t *testing.T) {
	st := newFakeStore()
	st.projects["mer"] = domain.ProjectRecord{ID: "mer"}
	st.sessions["mer-1"] = domain.SessionRecord{ID: "mer-1", ProjectID: "mer", DisplayName: "ao #7"}
	fc := &fakeCommander{}
	svc := NewWithDeps(Deps{Manager: fc, Store: st})

	err := svc.Rename(context.Background(), "mer-1", strings.Repeat("x", domain.MaxSessionDisplayNameRunes+1))
	if err == nil {
		t.Fatal("Rename accepted a name over the display-name cap")
	}
	if got := st.sessions["mer-1"].DisplayName; got != "ao #7" {
		t.Fatalf("display name = %q, want the previous name kept", got)
	}
	if fc.deliveredNames != 0 {
		t.Fatalf("harness name deliveries = %d, want 0", fc.deliveredNames)
	}
}

// A name exactly at the cap is still accepted.
func TestRenameAcceptsANameAtTheCap(t *testing.T) {
	st := newFakeStore()
	st.projects["mer"] = domain.ProjectRecord{ID: "mer"}
	st.sessions["mer-1"] = domain.SessionRecord{ID: "mer-1", ProjectID: "mer"}
	svc := NewWithDeps(Deps{Manager: &fakeCommander{}, Store: st})

	name := strings.Repeat("x", domain.MaxSessionDisplayNameRunes)
	if err := svc.Rename(context.Background(), "mer-1", name); err != nil {
		t.Fatalf("Rename at the cap: %v", err)
	}
	if st.sessions["mer-1"].DisplayName != name {
		t.Fatalf("display name = %q, want %q", st.sessions["mer-1"].DisplayName, name)
	}
}
