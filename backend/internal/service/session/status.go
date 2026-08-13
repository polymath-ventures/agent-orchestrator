package session

import (
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/pkg/contract"
)

// NoSignalGrace is how long after spawn/restore a session may stay silent
// before its idle reading is downgraded to StatusNoSignal. It covers the
// agent's TUI boot plus the gap to the first activity-bearing hook callback
// (for Codex that is UserPromptSubmit, seconds after the auto-submitted spawn
// prompt — its SessionStart hook fires earlier but carries no activity state);
// past it, a silent session is indistinguishable from one with a broken hook
// pipeline, and the dashboard must not claim a confident "idle".
const NoSignalGrace = 90 * time.Second

func deriveStatus(rec domain.SessionRecord, prs []domain.PRFacts, now time.Time, signalCapable bool) domain.SessionStatus {
	if rec.IsTerminated {
		if anyMerged(prs) {
			return domain.StatusMerged
		}
		return domain.StatusTerminated
	}

	switch rec.Activity.State {
	case domain.ActivityActive:
		return domain.StatusWorking
	case domain.ActivityExited:
		return domain.StatusExited
	case domain.ActivityWaitingInput, domain.ActivityBlocked:
		return domain.StatusNeedsInput
	}

	if scmStatus := deriveSCMStatus(prs); scmStatus != "" {
		return scmStatus
	}

	// No hook callback has ever arrived for this spawn/restore even though the
	// harness has a hook pipeline. The seeded LastActivityAt marks the launch,
	// so once the grace passes the honest status is "no signal", not "idle".
	//
	// Chat sessions are exempt regardless of harness. no_signal means "AO cannot
	// tell what this agent is doing", which is inferred from silence because a TUI
	// agent reports through a hook pipeline AO does not own. In chat mode AO holds
	// the provider connection itself: it knows the controller's state directly, and
	// a controller that dies is reported as exited rather than guessed at from
	// silence. An idle chat session waiting on the user emits nothing by design, so
	// inferring a broken pipeline from that silence would be wrong every time.
	if signalCapable && rec.Mode != domain.SessionModeChat &&
		rec.FirstSignalAt.IsZero() && now.Sub(rec.Activity.LastActivityAt) > NoSignalGrace {
		return domain.StatusNoSignal
	}
	return domain.StatusIdle
}

func anyMerged(prs []domain.PRFacts) bool {
	for _, pr := range prs {
		if pr.Merged {
			return true
		}
	}
	return false
}

func deriveSCMStatus(prs []domain.PRFacts) domain.SessionStatus {
	return domain.SessionStatus(contract.DeriveSCMStatus(toContractPRFacts(prs)))
}

func toContractPRFacts(prs []domain.PRFacts) []contract.PRFacts {
	facts := make([]contract.PRFacts, len(prs))
	for i, pr := range prs {
		facts[i] = contract.PRFacts{
			URL:            pr.URL,
			Draft:          pr.Draft,
			Merged:         pr.Merged,
			Closed:         pr.Closed,
			CI:             pr.CI,
			Review:         pr.Review,
			Mergeability:   pr.Mergeability,
			ReviewComments: pr.ReviewComments,
			SourceBranch:   pr.SourceBranch,
			TargetBranch:   pr.TargetBranch,
		}
	}
	return facts
}
