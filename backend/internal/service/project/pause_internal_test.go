package project

import (
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestComputePauseState(t *testing.T) {
	cases := []struct {
		name          string
		projectPaused bool
		fleetPaused   bool
		live          int
		wantState     PauseState
		wantDraining  int
	}{
		{"neither paused", false, false, 3, PauseStateRunning, 0},
		{"project paused, workers finishing", true, false, 2, PauseStateDraining, 2},
		{"fleet paused, workers finishing", false, true, 1, PauseStateDraining, 1},
		{"project paused, drained", true, false, 0, PauseStatePaused, 0},
		{"fleet paused, drained", false, true, 0, PauseStatePaused, 0},
		{"both paused, drained", true, true, 0, PauseStatePaused, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state, draining := computePauseState(tc.projectPaused, tc.fleetPaused, tc.live)
			if state != tc.wantState || draining != tc.wantDraining {
				t.Fatalf("computePauseState(%v,%v,%d) = (%q,%d), want (%q,%d)",
					tc.projectPaused, tc.fleetPaused, tc.live, state, draining, tc.wantState, tc.wantDraining)
			}
		})
	}
}

func TestLiveWorkersByProject(t *testing.T) {
	sessions := []domain.SessionRecord{
		{ProjectID: "a", Kind: domain.KindWorker, IsTerminated: false},
		{ProjectID: "a", Kind: domain.KindWorker, IsTerminated: false},
		{ProjectID: "a", Kind: domain.KindWorker, IsTerminated: true},        // terminated — not counted
		{ProjectID: "a", Kind: domain.KindOrchestrator, IsTerminated: false}, // orchestrator — not counted
		{ProjectID: "b", Kind: domain.KindWorker, IsTerminated: false},
	}
	got := liveWorkersByProject(sessions)
	if got["a"] != 2 {
		t.Fatalf("project a live workers = %d, want 2", got["a"])
	}
	if got["b"] != 1 {
		t.Fatalf("project b live workers = %d, want 1", got["b"])
	}
}
