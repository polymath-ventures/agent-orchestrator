package roleprompt

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

type fakeSessions struct {
	prompts map[domain.SessionKind]string
	err     error
}

func (f fakeSessions) RoleSystemPrompt(_ context.Context, kind domain.SessionKind, _ domain.ProjectID) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.prompts[kind], nil
}

type fakeProjects struct {
	rec domain.ProjectRecord
	ok  bool
	err error
}

func (f fakeProjects) GetProject(_ context.Context, _ string) (domain.ProjectRecord, bool, error) {
	return f.rec, f.ok, f.err
}

func TestRolePrompt_WorkerOrchestratorAndPrime(t *testing.T) {
	sessions := fakeSessions{prompts: map[domain.SessionKind]string{
		domain.KindWorker:       "WORKER PROMPT",
		domain.KindOrchestrator: "ORCH PROMPT",
		domain.KindPrime:        "PRIME PROMPT",
	}}
	a := New(sessions, fakeProjects{rec: domain.ProjectRecord{ID: "mer"}, ok: true})

	for role, want := range map[string]string{RoleWorker: "WORKER PROMPT", RoleOrchestrator: "ORCH PROMPT", RolePrime: "PRIME PROMPT"} {
		got, err := a.RolePrompt(context.Background(), "mer", role)
		if err != nil {
			t.Fatalf("%s: %v", role, err)
		}
		if got != want {
			t.Fatalf("%s prompt = %q, want %q", role, got, want)
		}
	}
}

func TestRolePrompt_ReviewerIncludesRulesContent(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "rev.md"), []byte("Reviewer file rule.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := domain.ProjectRecord{ID: "mer", Path: dir, Config: domain.ProjectConfig{
		ReviewerRules:     "Inline reviewer rule.",
		ReviewerRulesFile: "rev.md",
	}}
	a := New(fakeSessions{}, fakeProjects{rec: rec, ok: true})

	got, err := a.RolePrompt(context.Background(), "mer", RoleReviewer)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"## Code reviewer role", "## Project-Specific Reviewer Rules", "Inline reviewer rule.", "Reviewer file rule."} {
		if !strings.Contains(got, want) {
			t.Fatalf("reviewer prompt missing %q:\n%s", want, got)
		}
	}
}

func TestRolePrompt_ReviewerFailsClosedOnMissingFile(t *testing.T) {
	rec := domain.ProjectRecord{ID: "mer", Path: t.TempDir(), Config: domain.ProjectConfig{
		ReviewerRulesFile: "missing.md",
	}}
	a := New(fakeSessions{}, fakeProjects{rec: rec, ok: true})

	if _, err := a.RolePrompt(context.Background(), "mer", RoleReviewer); err == nil {
		t.Fatal("expected fail-closed error for missing reviewer rules file")
	}
}

func TestRolePrompt_UnknownRole(t *testing.T) {
	a := New(fakeSessions{}, fakeProjects{rec: domain.ProjectRecord{ID: "mer"}, ok: true})
	_, err := a.RolePrompt(context.Background(), "mer", "unknown")
	if !errors.Is(err, ErrUnknownRole) {
		t.Fatalf("err = %v, want ErrUnknownRole", err)
	}
}

func TestRolePrompt_ProjectNotFound(t *testing.T) {
	a := New(fakeSessions{}, fakeProjects{ok: false})
	_, err := a.RolePrompt(context.Background(), "nope", RoleWorker)
	if !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("err = %v, want ErrProjectNotFound", err)
	}
}
