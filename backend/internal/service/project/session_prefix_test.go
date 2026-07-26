package project_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/project"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

func TestManager_AddDerivesSessionPrefixFromTheName(t *testing.T) {
	ctx := context.Background()
	m := newManager(t)

	proj, err := m.Add(ctx, project.AddInput{Path: gitRepo(t), ProjectID: ptr("coachclaw"), Name: ptr("Coach Claw")})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if proj.Config == nil {
		t.Fatalf("Add returned a nil config; the derived prefix was not persisted")
	}
	if proj.Config.SessionPrefix != "cc" {
		t.Fatalf("derived session prefix = %q, want %q", proj.Config.SessionPrefix, "cc")
	}

	// It is persisted, not just returned: the settings form reads the stored value.
	list, err := m.List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("List() = %v, %v; want one project", list, err)
	}
	if list[0].SessionPrefix != "cc" {
		t.Fatalf("listed session prefix = %q, want %q", list[0].SessionPrefix, "cc")
	}
}

func TestManager_AddKeepsAnOperatorSuppliedSessionPrefix(t *testing.T) {
	ctx := context.Background()
	m := newManager(t)

	proj, err := m.Add(ctx, project.AddInput{
		Path:      gitRepo(t),
		ProjectID: ptr("coachclaw"),
		Name:      ptr("Coach Claw"),
		Config:    &domain.ProjectConfig{SessionPrefix: "zzz"},
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if proj.Config == nil || proj.Config.SessionPrefix != "zzz" {
		t.Fatalf("session prefix = %#v, want the operator's %q", proj.Config, "zzz")
	}
}

// The load-bearing collision: a project that stores no prefix still *displays*
// one, and a short id displays a short prefix. A project whose id is literally
// "ao" displays "ao", which is exactly what a newly added "Agent Orchestrator"
// would otherwise derive — the collision this change exists to remove.
func TestManager_AddAvoidsAPrefixAnExistingProjectResolvesTo(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	m := project.NewWithDeps(project.Deps{Store: store})

	// A project as it exists today: registered before this change, storing no
	// prefix at all, so it resolves to its id.
	legacy := domain.ProjectRecord{ID: "ao", Path: gitRepo(t), DisplayName: "ao", Kind: domain.ProjectKindSingleRepo}
	if err := store.UpsertProject(ctx, legacy); err != nil {
		t.Fatalf("seed legacy project: %v", err)
	}

	proj, err := m.Add(ctx, project.AddInput{Path: gitRepo(t), ProjectID: ptr("agent-orchestrator"), Name: ptr("Agent Orchestrator")})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if proj.Config == nil || proj.Config.SessionPrefix == "" {
		t.Fatalf("Add persisted no session prefix: %#v", proj.Config)
	}
	if proj.Config.SessionPrefix == "ao" {
		t.Fatalf("derived %q, which the existing project already resolves to", proj.Config.SessionPrefix)
	}
}

func TestManager_AddDerivesDistinctPrefixesForCollidingNames(t *testing.T) {
	ctx := context.Background()
	m := newManager(t)

	first, err := m.Add(ctx, project.AddInput{Path: gitRepo(t), ProjectID: ptr("coachclaw"), Name: ptr("Coach Claw")})
	if err != nil {
		t.Fatalf("Add first: %v", err)
	}
	second, err := m.Add(ctx, project.AddInput{Path: gitRepo(t), ProjectID: ptr("codecleanup"), Name: ptr("Code Cleanup")})
	if err != nil {
		t.Fatalf("Add second: %v", err)
	}
	if first.Config == nil || second.Config == nil {
		t.Fatalf("a project persisted no config: %#v / %#v", first.Config, second.Config)
	}
	if first.Config.SessionPrefix == second.Config.SessionPrefix {
		t.Fatalf("both projects derived %q; prefixes must be distinct", first.Config.SessionPrefix)
	}
}

func TestManager_AddSucceedsWhenTheNameYieldsNoUsableCharacters(t *testing.T) {
	ctx := context.Background()
	m := newManager(t)

	proj, err := m.Add(ctx, project.AddInput{Path: gitRepo(t), ProjectID: ptr("coachclaw"), Name: ptr("!!! ???")})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if proj.Config == nil || proj.Config.SessionPrefix == "" {
		t.Fatalf("Add persisted no session prefix for an unusable name: %#v", proj.Config)
	}
}

func TestManager_AddDerivesSessionPrefixForWorkspaceProjects(t *testing.T) {
	ctx := context.Background()
	m := newManager(t)

	root := t.TempDir()
	gitRepoWithCommit(t, filepath.Join(root, "api"))

	proj, err := m.Add(ctx, project.AddInput{Path: root, ProjectID: ptr("coachclaw"), Name: ptr("Coach Claw"), AsWorkspace: true})
	if err != nil {
		t.Fatalf("Add workspace: %v", err)
	}
	if proj.Config == nil || proj.Config.SessionPrefix != "cc" {
		t.Fatalf("workspace session prefix = %#v, want %q", proj.Config, "cc")
	}
}

func TestManager_EnsureDefaultScratchProjectDerivesASessionPrefix(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	m := project.NewWithDeps(project.Deps{Store: store, DefaultHarness: domain.HarnessCodex})

	proj, err := m.EnsureDefaultScratchProject(ctx, filepath.Join(t.TempDir(), "scratch", "default"))
	if err != nil {
		t.Fatalf("EnsureDefaultScratchProject: %v", err)
	}
	if proj.Config == nil || proj.Config.SessionPrefix != "scr" {
		t.Fatalf("scratch session prefix = %#v, want %q", proj.Config, "scr")
	}
}

// Derivation runs at creation only. A project that already stores no prefix keeps
// resolving through the legacy id-derived fallback, so nothing renames itself.
func TestManager_ExistingProjectsKeepTheirResolvedPrefix(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	m := project.NewWithDeps(project.Deps{Store: store})

	legacy := domain.ProjectRecord{
		ID:          "a-very-long-project-id",
		Path:        gitRepo(t),
		DisplayName: "A Very Long Project",
		Kind:        domain.ProjectKindSingleRepo,
	}
	if err := store.UpsertProject(ctx, legacy); err != nil {
		t.Fatalf("seed legacy project: %v", err)
	}

	list, err := m.List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("List() = %v, %v; want one project", list, err)
	}
	if got, want := list[0].SessionPrefix, "a-very-long-"; got != want {
		t.Fatalf("legacy session prefix = %q, want the unchanged id-derived %q", got, want)
	}

	res, err := m.Get(ctx, "a-very-long-project-id")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if res.Project == nil {
		t.Fatalf("Get returned no project: %#v", res)
	}
	if res.Project.Config != nil && res.Project.Config.SessionPrefix != "" {
		t.Fatalf("a stored prefix appeared on an existing project: %q", res.Project.Config.SessionPrefix)
	}
}
