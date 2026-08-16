package session

import (
	"context"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// staticSCM is a minimal scmProvider stub that returns a canned SCMRepo for
// any remote. It lets trackerIDForIssue tests exercise the SCM-backed
// repoForTracker path without a full provider stack.
type staticSCM struct {
	repo ports.SCMRepo
}

func (s staticSCM) ParseRepository(string) (ports.SCMRepo, bool) {
	if s.repo.Provider == "" || s.repo.Repo == "" {
		return ports.SCMRepo{}, false
	}
	return s.repo, true
}
func (staticSCM) FetchPullRequests(context.Context, []ports.SCMPRRef) ([]ports.SCMObservation, error) {
	return nil, nil
}
func (staticSCM) FetchReviewThreads(context.Context, ports.SCMPRRef) (ports.SCMReviewObservation, error) {
	return ports.SCMReviewObservation{}, nil
}

func TestTrackerIDForIssue_PlainNumberSelfManagedGitLab(t *testing.T) {
	svc := NewWithDeps(Deps{SCM: staticSCM{repo: ports.SCMRepo{
		Provider: "gitlab",
		Host:     "gitlab.internal",
		Owner:    "group",
		Name:     "project",
		Repo:     "group/project",
	}}})

	id, ok := svc.trackerIDForIssue(ports.SpawnConfig{IssueID: "42"}, domain.ProjectRecord{
		RepoOriginURL: "https://gitlab.internal/group/project.git",
	})
	if !ok {
		t.Fatal("trackerIDForIssue ok = false")
	}
	if id.Provider != domain.TrackerProviderGitLab {
		t.Errorf("Provider = %q, want gitlab", id.Provider)
	}
	if id.Host != "gitlab.internal" {
		t.Errorf("Host = %q, want gitlab.internal", id.Host)
	}
	if id.Native != "group/project#42" {
		t.Errorf("Native = %q, want group/project#42", id.Native)
	}
}

func TestTrackerIDForIssue_PlainNumberGitLabDotCom(t *testing.T) {
	svc := NewWithDeps(Deps{SCM: staticSCM{repo: ports.SCMRepo{
		Provider: "gitlab",
		Host:     "gitlab.com",
		Owner:    "group",
		Name:     "project",
		Repo:     "group/project",
	}}})

	id, ok := svc.trackerIDForIssue(ports.SpawnConfig{IssueID: "42"}, domain.ProjectRecord{
		RepoOriginURL: "https://gitlab.com/group/project.git",
	})
	if !ok {
		t.Fatal("trackerIDForIssue ok = false")
	}
	if id.Provider != domain.TrackerProviderGitLab {
		t.Errorf("Provider = %q, want gitlab", id.Provider)
	}
	if id.Host != "" {
		t.Errorf("Host = %q, want \"\" (gitlab.com zero value)", id.Host)
	}
	if id.Native != "group/project#42" {
		t.Errorf("Native = %q, want group/project#42", id.Native)
	}
}

func TestTrackerIDForIssue_PlainNumberGitHub(t *testing.T) {
	svc := NewWithDeps(Deps{SCM: staticSCM{repo: ports.SCMRepo{
		Provider: "github",
		Host:     "github.com",
		Owner:    "owner",
		Name:     "repo",
		Repo:     "owner/repo",
	}}})

	id, ok := svc.trackerIDForIssue(ports.SpawnConfig{IssueID: "42"}, domain.ProjectRecord{
		RepoOriginURL: "https://github.com/owner/repo.git",
	})
	if !ok {
		t.Fatal("trackerIDForIssue ok = false")
	}
	if id.Provider != domain.TrackerProviderGitHub {
		t.Errorf("Provider = %q, want github", id.Provider)
	}
	if id.Host != "" {
		t.Errorf("Host = %q, want \"\" (GitHub never uses Host)", id.Host)
	}
	if id.Native != "owner/repo#42" {
		t.Errorf("Native = %q, want owner/repo#42", id.Native)
	}
}
