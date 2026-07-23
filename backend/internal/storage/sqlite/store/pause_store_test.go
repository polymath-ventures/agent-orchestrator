package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

// Fleet pause is a single daemon-global bit, defaulting to unpaused, that
// round-trips independently of any project row.
func TestFleetPausedRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	paused, err := s.GetFleetPaused(ctx)
	if err != nil {
		t.Fatalf("GetFleetPaused: %v", err)
	}
	if paused {
		t.Fatalf("fresh daemon: fleet paused = true, want false (seeded unpaused)")
	}

	if err := s.SetFleetPaused(ctx, true); err != nil {
		t.Fatalf("SetFleetPaused(true): %v", err)
	}
	if paused, err = s.GetFleetPaused(ctx); err != nil || !paused {
		t.Fatalf("after SetFleetPaused(true): paused=%v err=%v, want true/nil", paused, err)
	}

	if err := s.SetFleetPaused(ctx, false); err != nil {
		t.Fatalf("SetFleetPaused(false): %v", err)
	}
	if paused, err = s.GetFleetPaused(ctx); err != nil || paused {
		t.Fatalf("after SetFleetPaused(false): paused=%v err=%v, want false/nil", paused, err)
	}
}

// Prime settings live beside fleet pause in daemon-owned storage. A fresh
// daemon starts disabled, and saving settings survives a store reopen because
// the database is the single source of truth.
func TestPrimeSettingsRoundTrip(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := sqlite.Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	settings, err := s.GetPrimeSettings(ctx)
	if err != nil {
		t.Fatalf("GetPrimeSettings fresh: %v", err)
	}
	if settings.Enabled {
		t.Fatalf("fresh Prime enabled = true, want false")
	}

	settings.Enabled = true
	settings.DisplayName = "Fleet Lead"
	settings.Harness = domain.HarnessCodex
	settings.AgentConfig = domain.AgentConfig{Model: "gpt-5-codex", Effort: domain.EffortHigh}
	settings.Rules = "watch the fleet"
	settings.RulesFile = "/etc/ao/prime.md"
	settings.WakeInterval = "30m"
	if err := s.SetPrimeSettings(ctx, settings); err != nil {
		t.Fatalf("SetPrimeSettings: %v", err)
	}
	_ = s.Close()

	reopened, err := sqlite.Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	got, err := reopened.GetPrimeSettings(ctx)
	if err != nil {
		t.Fatalf("GetPrimeSettings reopened: %v", err)
	}
	if !got.Enabled || got.DisplayName != "Fleet Lead" || got.Harness != domain.HarnessCodex {
		t.Fatalf("settings after reopen = %+v", got)
	}
	if got.AgentConfig.Model != "gpt-5-codex" || got.AgentConfig.Effort != domain.EffortHigh {
		t.Fatalf("agent config after reopen = %+v", got.AgentConfig)
	}
	if got.Rules != "watch the fleet" || got.RulesFile != "/etc/ao/prime.md" || got.WakeInterval != "30m" {
		t.Fatalf("settings text/wake after reopen = %+v", got)
	}
}

// The per-project pause bit round-trips through a dedicated column and is
// visible on the loaded ProjectRecord.
func TestSetProjectPausedRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s, "proj-a")

	rec, ok, err := s.GetProject(ctx, "proj-a")
	if err != nil || !ok {
		t.Fatalf("GetProject: ok=%v err=%v", ok, err)
	}
	if rec.Paused {
		t.Fatalf("fresh project: Paused = true, want false")
	}

	changed, err := s.SetProjectPaused(ctx, "proj-a", true)
	if err != nil || !changed {
		t.Fatalf("SetProjectPaused(true): changed=%v err=%v, want true/nil", changed, err)
	}
	if rec, _, err = s.GetProject(ctx, "proj-a"); err != nil || !rec.Paused {
		t.Fatalf("after pause: Paused=%v err=%v, want true/nil", rec.Paused, err)
	}

	if changed, err = s.SetProjectPaused(ctx, "proj-a", false); err != nil || !changed {
		t.Fatalf("SetProjectPaused(false): changed=%v err=%v", changed, err)
	}
	if rec, _, err = s.GetProject(ctx, "proj-a"); err != nil || rec.Paused {
		t.Fatalf("after resume: Paused=%v err=%v, want false/nil", rec.Paused, err)
	}
}

// Core axiom: "pause is a bit, not config surgery." Toggling the pause bit must
// not disturb the project's stored config, and saving config must not disturb
// the pause bit. Both directions are asserted here so the two facts stay in
// their own columns.
func TestPauseAndConfigAreIndependent(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	rec := domain.ProjectRecord{
		ID:           "proj-cfg",
		Path:         "/tmp/proj-cfg",
		RegisteredAt: time.Now().UTC().Truncate(time.Second),
		Config:       domain.ProjectConfig{DefaultBranch: "develop"},
	}
	if err := s.UpsertProject(ctx, rec); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// Pausing leaves config untouched.
	if _, err := s.SetProjectPaused(ctx, "proj-cfg", true); err != nil {
		t.Fatalf("SetProjectPaused: %v", err)
	}
	got, _, err := s.GetProject(ctx, "proj-cfg")
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if got.Config.DefaultBranch != "develop" {
		t.Fatalf("after pause: config DefaultBranch = %q, want develop (config mutated by pause)", got.Config.DefaultBranch)
	}
	if !got.Paused {
		t.Fatalf("after pause: Paused = false, want true")
	}

	// Saving config (an upsert that never sets paused) leaves the pause bit set.
	rec.Config.DefaultBranch = "main"
	if err := s.UpsertProject(ctx, rec); err != nil {
		t.Fatalf("upsert (config save): %v", err)
	}
	if got, _, err = s.GetProject(ctx, "proj-cfg"); err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if got.Config.DefaultBranch != "main" {
		t.Fatalf("config save did not take: DefaultBranch = %q, want main", got.Config.DefaultBranch)
	}
	if !got.Paused {
		t.Fatalf("config save cleared the pause bit: Paused = false, want true")
	}
}
