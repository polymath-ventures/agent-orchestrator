package project_test

import (
	"context"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/project"
)

func addConfigProject(t *testing.T, m project.Manager) {
	t.Helper()
	cfg := domain.ProjectConfig{DefaultBranch: "main"}
	if _, err := m.Add(context.Background(), project.AddInput{
		Path: gitRepo(t), ProjectID: ptr("ao"), Config: &cfg,
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}
}

func TestSetConfigRejectsWriteBuiltOnStaleRead(t *testing.T) {
	ctx := context.Background()
	m := newManager(t)
	addConfigProject(t, m)

	opened, err := m.Get(ctx, "ao")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	staleToken := opened.Project.ConfigETag
	if staleToken == "" {
		t.Fatal("a read must return a config token")
	}
	snapshot := *opened.Project.Config

	concurrent := snapshot
	concurrent.SessionPrefix = "concurrent"
	if _, err := m.SetConfig(ctx, "ao", project.SetConfigInput{Config: concurrent}); err != nil {
		t.Fatalf("concurrent write: %v", err)
	}

	edited := snapshot
	edited.DefaultBranch = "develop"
	_, err = m.SetConfig(ctx, "ao", project.SetConfigInput{Config: edited, IfMatch: staleToken})
	wantCode(t, err, "PROJECT_CONFIG_STALE")

	after, err := m.Get(ctx, "ao")
	if err != nil {
		t.Fatalf("Get after rejected write: %v", err)
	}
	if after.Project.Config.SessionPrefix != "concurrent" {
		t.Fatalf("concurrent field = %q, want preserved", after.Project.Config.SessionPrefix)
	}
	if after.Project.Config.DefaultBranch == "develop" {
		t.Fatal("stale edit must not be applied")
	}
}

func TestSetConfigAcceptsCurrentReadAndMovesToken(t *testing.T) {
	ctx := context.Background()
	m := newManager(t)
	addConfigProject(t, m)

	got, err := m.Get(ctx, "ao")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	edited := *got.Project.Config
	edited.DefaultBranch = "develop"

	updated, err := m.SetConfig(ctx, "ao", project.SetConfigInput{
		Config: edited, IfMatch: got.Project.ConfigETag,
	})
	if err != nil {
		t.Fatalf("current write: %v", err)
	}
	if updated.Config.DefaultBranch != "develop" {
		t.Fatalf("edit was not applied: %#v", updated.Config)
	}
	if updated.ConfigETag == got.Project.ConfigETag {
		t.Fatal("config token must change when config changes")
	}
}

func TestSetConfigWildcardAndTokenlessWritesRemainAccepted(t *testing.T) {
	ctx := context.Background()
	m := newManager(t)
	addConfigProject(t, m)

	if _, err := m.SetConfig(ctx, "ao", project.SetConfigInput{
		Config: domain.ProjectConfig{DefaultBranch: "develop"}, IfMatch: "*",
	}); err != nil {
		t.Fatalf("wildcard write: %v", err)
	}
	if _, err := m.SetConfig(ctx, "ao", project.SetConfigInput{
		Config: domain.ProjectConfig{DefaultBranch: "release"},
	}); err != nil {
		t.Fatalf("tokenless write: %v", err)
	}
}
