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

// The stored id and intake's lookup key must be computed from ONE scope answer.
// These two cases are where the two resolvers disagreed: a project that points
// intake at a different repo than its own origin, and a GitLab project under a
// nested group. Either disagreement puts the duplicate-spawn loop back.
func TestSpawnResolvesTheSameScopeTrackerIntakeDoes(t *testing.T) {
	cases := []struct {
		name   string
		origin string
		intake domain.TrackerIntakeConfig
		scm    ports.SCMRepo
		want   domain.IssueID
	}{
		{
			name:   "configured tracker repo overrides the origin",
			origin: "https://github.com/acme/code.git",
			intake: domain.TrackerIntakeConfig{Enabled: true, Assignee: "alice", Repo: "acme/tracker"},
			scm:    ports.SCMRepo{Provider: "github", Host: "github.com", Owner: "acme", Name: "code", Repo: "acme/code"},
			want:   "github:acme/tracker#12",
		},
		{
			name:   "nested gitlab group keeps its full namespace",
			origin: "https://gitlab.com/group/sub/proj.git",
			intake: domain.TrackerIntakeConfig{Enabled: true, Assignee: "alice", Provider: domain.TrackerProviderGitLab},
			scm:    ports.SCMRepo{Provider: "gitlab", Host: "gitlab.com", Owner: "sub", Name: "proj", Repo: "group/sub/proj"},
			want:   "gitlab:group/sub/proj#12",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			project := domain.ProjectRecord{
				ID:            "demo",
				RepoOriginURL: tc.origin,
				Config:        domain.ProjectConfig{TrackerIntake: tc.intake},
			}
			st := newFakeStore()
			st.projects["demo"] = project
			fc := &fakeCommander{}
			svc := NewWithDeps(Deps{Manager: fc, Store: st, SCM: staticSCM{repo: tc.scm}})

			if _, _, _, err := svc.Spawn(context.Background(), ports.SpawnConfig{ProjectID: "demo", Kind: domain.KindWorker, IssueID: "12"}); err != nil {
				t.Fatalf("Spawn: %v", err)
			}
			if fc.spawnedCfg.IssueID != tc.want {
				t.Fatalf("persisted IssueID = %q, want %q", fc.spawnedCfg.IssueID, tc.want)
			}
			// The same scope, resolved the way tracker intake resolves it.
			scope, ok := domain.TrackerScope(tc.origin, tc.intake.WithDefaults(), "")
			if !ok {
				t.Fatal("domain.TrackerScope not ok")
			}
			id, ok := domain.ParseIssueRef("12", scope)
			if !ok {
				t.Fatal("intake-side ParseIssueRef not ok")
			}
			if got := domain.CanonicalIssueID(id); got != tc.want {
				t.Fatalf("intake lookup key = %q, want %q — the two scopes disagree", got, tc.want)
			}
		})
	}
}

// A shared resolver reached with different arguments is still two answers.
// These are the configurations where the spawn boundary and tracker intake
// previously normalised the intake config differently.
func TestSpawnScopeMatchesIntakeWhenTheProviderIsUnset(t *testing.T) {
	cases := []struct {
		name            string
		origin          string
		intake          domain.TrackerIntakeConfig
		scm             ports.SCMRepo
		trackerProvider domain.TrackerProvider
	}{
		{
			name:   "gitlab origin, intake provider unset",
			origin: "https://gitlab.com/group/proj.git",
			intake: domain.TrackerIntakeConfig{Enabled: true, Assignee: "me", Repo: "acme/tracker"},
			scm:    ports.SCMRepo{Provider: "gitlab", Host: "gitlab.com", Owner: "group", Name: "proj", Repo: "group/proj"},
		},
		{
			name:            "unclassifiable origin with a CLI provider hint",
			origin:          "https://git.corp.example.com/acme/code.git",
			intake:          domain.TrackerIntakeConfig{Enabled: true, Assignee: "me", Repo: "acme/tracker"},
			trackerProvider: domain.TrackerProviderGitLab,
		},
		{
			name:            "non-github ssh origin with a CLI provider hint",
			origin:          "git@bitbucket.org:acme/code.git",
			intake:          domain.TrackerIntakeConfig{Enabled: true, Assignee: "me"},
			trackerProvider: domain.TrackerProviderGitLab,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			project := domain.ProjectRecord{
				ID:            "demo",
				RepoOriginURL: tc.origin,
				Config:        domain.ProjectConfig{TrackerIntake: tc.intake},
			}
			st := newFakeStore()
			st.projects["demo"] = project
			fc := &fakeCommander{}
			svc := NewWithDeps(Deps{Manager: fc, Store: st, SCM: staticSCM{repo: tc.scm}})

			if _, _, _, err := svc.Spawn(context.Background(), ports.SpawnConfig{
				ProjectID:       "demo",
				Kind:            domain.KindWorker,
				IssueID:         "42",
				TrackerProvider: tc.trackerProvider,
			}); err != nil {
				t.Fatalf("Spawn: %v", err)
			}

			scope, ok := domain.TrackerScope(tc.origin, tc.intake.WithDefaults(), "")
			if !ok {
				t.Fatal("intake-side TrackerScope not ok")
			}
			id, ok := domain.ParseIssueRef("42", scope)
			if !ok {
				t.Fatal("intake-side ParseIssueRef not ok")
			}
			want := domain.CanonicalIssueID(id)
			if fc.spawnedCfg.IssueID != want {
				t.Fatalf("persisted %q but intake looks up %q — the two scopes disagree", fc.spawnedCfg.IssueID, want)
			}
		})
	}
}

// The canonical form carries no GitLab instance host, so it can only stand in
// for an issue on the project's own instance. Flattening a reference that names
// a second self-managed host into it would re-resolve to the project's host —
// a different instance, and a real issue number on it.
func TestSpawnLeavesCrossHostGitLabReferencesAlone(t *testing.T) {
	st := newFakeStore()
	st.projects["demo"] = domain.ProjectRecord{ID: "demo", RepoOriginURL: "https://gitlab.internal/group/project.git"}
	fc := &fakeCommander{}
	svc := NewWithDeps(Deps{Manager: fc, Store: st, SCM: staticSCM{repo: ports.SCMRepo{
		Provider: "gitlab",
		Host:     "gitlab.internal",
		Owner:    "group",
		Name:     "project",
		Repo:     "group/project",
	}}})

	const other = "https://gitlab.other.example/group/project/-/issues/7"
	if _, _, _, err := svc.Spawn(context.Background(), ports.SpawnConfig{ProjectID: "demo", Kind: domain.KindWorker, IssueID: other}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if fc.spawnedCfg.IssueID != other {
		t.Fatalf("persisted IssueID = %q, want the reference kept verbatim", fc.spawnedCfg.IssueID)
	}

	// The project's own instance still canonicalises.
	if _, _, _, err := svc.Spawn(context.Background(), ports.SpawnConfig{
		ProjectID: "demo",
		Kind:      domain.KindWorker,
		IssueID:   "https://gitlab.internal/group/project/-/issues/7",
	}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if fc.spawnedCfg.IssueID != "gitlab:group/project#7" {
		t.Fatalf("persisted IssueID = %q, want gitlab:group/project#7", fc.spawnedCfg.IssueID)
	}
}

// A GitHub reference carries no host, so the cross-host guard must not catch it
// on a self-managed GitLab project: nothing is lost by canonicalising it, and
// leaving it raw breaks the invariant that stored ids are canonical.
func TestSpawnCanonicalisesAGitHubURLOnASelfManagedGitLabProject(t *testing.T) {
	st := newFakeStore()
	st.projects["demo"] = domain.ProjectRecord{ID: "demo", RepoOriginURL: "https://gitlab.internal/group/project.git"}
	fc := &fakeCommander{}
	svc := NewWithDeps(Deps{Manager: fc, Store: st, SCM: staticSCM{repo: ports.SCMRepo{
		Provider: "gitlab",
		Host:     "gitlab.internal",
		Owner:    "group",
		Name:     "project",
		Repo:     "group/project",
	}}})

	if _, _, _, err := svc.Spawn(context.Background(), ports.SpawnConfig{
		ProjectID: "demo",
		Kind:      domain.KindWorker,
		IssueID:   "https://github.com/acme/demo/issues/12",
	}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if fc.spawnedCfg.IssueID != "github:acme/demo#12" {
		t.Fatalf("persisted IssueID = %q, want github:acme/demo#12", fc.spawnedCfg.IssueID)
	}
}
