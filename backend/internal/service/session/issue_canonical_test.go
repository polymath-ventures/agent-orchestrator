package session

import (
	"context"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// Regression for #298 defect 1. Every spawn producer — the CLI, the HTTP API,
// and tracker intake — funnels through Service.spawn, so the issue id is
// canonicalised there, once, before the record is written. Storing the raw
// operator input is what let the persisted value and intake's lookup key
// diverge.
func TestSpawnCanonicalisesIssueIDIntoTheSessionRecord(t *testing.T) {
	cases := []struct {
		name  string
		input domain.IssueID
		want  domain.IssueID
	}{
		{name: "bare number", input: "42", want: "github:acme/demo#42"},
		{name: "hash prefixed", input: "#42", want: "github:acme/demo#42"},
		{name: "provider native", input: "acme/demo#42", want: "github:acme/demo#42"},
		{name: "issue url", input: "https://github.com/acme/demo/issues/42", want: "github:acme/demo#42"},
		{name: "already canonical", input: "github:acme/demo#42", want: "github:acme/demo#42"},
		{name: "unrecognised shape passes through", input: "ISS-1", want: "ISS-1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := newFakeStore()
			st.projects["demo"] = domain.ProjectRecord{ID: "demo", RepoOriginURL: "https://github.com/acme/demo.git"}
			fc := &fakeCommander{}
			svc := NewWithDeps(Deps{Manager: fc, Store: st, SCM: fakeSCM{}})

			if _, _, _, err := svc.Spawn(context.Background(), ports.SpawnConfig{ProjectID: "demo", Kind: domain.KindWorker, IssueID: tc.input}); err != nil {
				t.Fatalf("Spawn: %v", err)
			}
			if fc.spawnedCfg.IssueID != tc.want {
				t.Fatalf("persisted IssueID = %q, want %q", fc.spawnedCfg.IssueID, tc.want)
			}
		})
	}
}

// Canonicalisation must not depend on the tracker being configured: a daemon
// without a GitHub token still persists session records, and a non-canonical
// one there reopens the duplicate-spawn loop the moment a token appears.
func TestSpawnCanonicalisesIssueIDWithoutATracker(t *testing.T) {
	st := newFakeStore()
	st.projects["demo"] = domain.ProjectRecord{ID: "demo", RepoOriginURL: "https://github.com/acme/demo.git"}
	fc := &fakeCommander{}
	svc := NewWithDeps(Deps{Manager: fc, Store: st, SCM: fakeSCM{}})

	if _, _, _, err := svc.Spawn(context.Background(), ports.SpawnConfig{ProjectID: "demo", Kind: domain.KindWorker, IssueID: "42"}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if fc.spawnedCfg.IssueID != "github:acme/demo#42" {
		t.Fatalf("persisted IssueID = %q, want github:acme/demo#42", fc.spawnedCfg.IssueID)
	}
}

// An already-canonical id must resolve back to the provider-native form. Before
// #298 the provider prefix was parsed as part of the owner, so intake-spawned
// sessions asked the tracker for "github:acme/demo" and silently lost their
// title and issue context.
func TestSpawnResolvesCanonicalIssueIDBackToNativeForTheTracker(t *testing.T) {
	st := newFakeStore()
	st.projects["demo"] = domain.ProjectRecord{ID: "demo", RepoOriginURL: "https://github.com/acme/demo.git"}
	fc := &fakeCommander{}
	tracker := &fakeTracker{issue: domain.Issue{
		ID:    domain.TrackerID{Provider: domain.TrackerProviderGitHub, Native: "acme/demo#42"},
		Title: "Canonical round trip",
		State: domain.IssueOpen,
	}}
	svc := NewWithDeps(Deps{Manager: fc, Store: st, Tracker: tracker, SCM: fakeSCM{}})

	if _, _, _, err := svc.Spawn(context.Background(), ports.SpawnConfig{ProjectID: "demo", Kind: domain.KindWorker, IssueID: "github:acme/demo#42"}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if len(tracker.ids) != 1 {
		t.Fatalf("tracker calls = %d, want 1", len(tracker.ids))
	}
	if tracker.ids[0].Native != "acme/demo#42" {
		t.Fatalf("tracker id native = %q, want acme/demo#42", tracker.ids[0].Native)
	}
	if fc.spawnedCfg.IssueTitle != "Canonical round trip" {
		t.Fatalf("IssueTitle = %q, want the enriched title", fc.spawnedCfg.IssueTitle)
	}
}

// A self-managed GitLab canonical id keeps its instance host, which the
// canonical string itself does not carry, so it must come back from the
// project's origin.
func TestSpawnCanonicalGitLabIssueIDKeepsSelfManagedHost(t *testing.T) {
	st := newFakeStore()
	st.projects["demo"] = domain.ProjectRecord{ID: "demo", RepoOriginURL: "https://gitlab.internal/group/project.git"}
	fc := &fakeCommander{}
	tracker := &fakeTracker{issue: domain.Issue{
		ID:    domain.TrackerID{Provider: domain.TrackerProviderGitLab, Native: "group/project#7"},
		State: domain.IssueOpen,
	}}
	svc := NewWithDeps(Deps{Manager: fc, Store: st, Tracker: tracker, SCM: staticSCM{repo: ports.SCMRepo{
		Provider: "gitlab",
		Host:     "gitlab.internal",
		Owner:    "group",
		Name:     "project",
		Repo:     "group/project",
	}}})

	if _, _, _, err := svc.Spawn(context.Background(), ports.SpawnConfig{ProjectID: "demo", Kind: domain.KindWorker, IssueID: "gitlab:group/project#7"}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if len(tracker.ids) != 1 {
		t.Fatalf("tracker calls = %d, want 1", len(tracker.ids))
	}
	if tracker.ids[0].Host != "gitlab.internal" {
		t.Fatalf("tracker id host = %q, want gitlab.internal", tracker.ids[0].Host)
	}
	if fc.spawnedCfg.IssueID != "gitlab:group/project#7" {
		t.Fatalf("persisted IssueID = %q, want gitlab:group/project#7", fc.spawnedCfg.IssueID)
	}
}
