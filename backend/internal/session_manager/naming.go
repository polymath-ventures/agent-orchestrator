package sessionmanager

import (
	"context"
	"fmt"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/sessionguard"
)

// resolveDisplayName decides what a session is called, once, at the moment its
// identity is set. An empty requested name is the signal that asks for the
// computed one; a supplied name is a deliberate operator override.
//
// Computing here rather than in each caller is the whole point: three spawn
// entry points (tracker intake, CLI, HTTP) funnel through one manager, and it is
// the divergence between per-caller names that this change removes.
func (m *Manager) resolveDisplayName(cfg ports.SpawnConfig, project domain.ProjectRecord) string {
	if name := strings.TrimSpace(cfg.DisplayName); name != "" {
		return name
	}
	switch cfg.Kind {
	case domain.KindWorker:
		title := strings.TrimSpace(cfg.IssueTitle)
		if cfg.IssueID != "" && title == "" {
			// A degraded name must never be silent: the head-only form is correct
			// behavior for an unresolvable work item, and indistinguishable from a
			// tracker outage unless it is logged.
			m.logger.Warn("spawn: work item title unavailable; naming session from the work item number alone",
				"projectID", cfg.ProjectID, "issueID", cfg.IssueID)
		}
		return domain.ComposeWorkerDisplayName(sessionPrefix(project), string(cfg.IssueID), title)
	case domain.KindOrchestrator:
		return domain.ComposeOrchestratorDisplayName(sessionPrefix(project))
	default:
		// Prime's name is owned by fleet Prime settings, which the session service
		// supplies on the request; there is nothing to compute here.
		return ""
	}
}

// deliverSpawnName pushes a freshly spawned session's name into its harness,
// unless the launch command already carried it.
//
// Preferring the launch-argument form is not a micro-optimization: a name
// delivered in argv lands atomically with process start, so for those harnesses
// the pane-readiness race is absent rather than mitigated. Only a harness with
// no launch-time flag needs the post-start write, and that write waits for the
// harness to be ready first — runtime creation returns as soon as the pane
// exists, which is before the TUI has drawn an input box to receive keystrokes.
func (m *Manager) deliverSpawnName(ctx context.Context, agent ports.Agent, cfg ports.LaunchConfig, handle ports.RuntimeHandle, id domain.SessionID, name string) error {
	if strings.TrimSpace(name) == "" {
		return nil
	}
	namer, ok := agent.(ports.AgentNamer)
	if !ok {
		return nil
	}
	if len(namer.LaunchNameArgs(name)) > 0 {
		return nil
	}
	if err := m.waitForPromptReadiness(ctx, agent, cfg, handle); err != nil {
		return err
	}
	rec, err := m.getRecord(ctx, id)
	if err != nil {
		return err
	}
	return m.deliverName(ctx, rec)
}

// DeliverName pushes a session's persisted display name into its running
// harness. It is the delivery half of a rename: the session service owns the
// database write, this owns the pane.
func (m *Manager) DeliverName(ctx context.Context, id domain.SessionID) error {
	rec, err := m.getRecord(ctx, id)
	if err != nil {
		return err
	}
	return m.deliverName(ctx, rec)
}

// deliverName is the single routine every naming path goes through — spawn and
// rename alike — so a surface cannot be forgotten and the skip conditions are
// written once. It reads the name off the record rather than accepting it as an
// argument, which is what makes the string delivered to the harness identical to
// the one AO displays by construction rather than by convention.
//
// A session that is terminated or has no runtime keeps its persisted name and is
// not written to; an adapter that declares no naming capability is left alone
// rather than having text typed blindly into a TUI AO does not understand.
func (m *Manager) deliverName(ctx context.Context, rec domain.SessionRecord) error {
	name := strings.TrimSpace(rec.DisplayName)
	if name == "" || rec.IsTerminated || rec.Metadata.RuntimeHandleID == "" {
		return nil
	}
	agent, ok := m.agents.Agent(rec.Harness)
	if !ok {
		return nil
	}
	namer, ok := agent.(ports.AgentNamer)
	if !ok {
		return nil
	}
	cmd, ok := namer.InHarnessRenameCommand(name)
	if !ok {
		m.logger.Warn("session name cannot be delivered to this harness verbatim; keeping AO's name only",
			"sessionID", rec.ID, "harness", string(rec.Harness))
		return nil
	}
	outcome, err := m.messenger.Deliver(ctx, rec.ID, cmd)
	if err != nil {
		return fmt.Errorf("deliver name %s: %w", rec.ID, err)
	}
	if outcome != sessionguard.Sent {
		m.logger.Warn("session name write suppressed; harness keeps its own name",
			"sessionID", rec.ID, "outcome", outcome.String())
	}
	return nil
}

// forgiveSpawnNameFailure reports whether a failed name delivery may be treated
// as cosmetic. It may — but only against a runtime proven alive.
//
// The asymmetry is load-bearing, not defensive habit. Once the prompt rides
// argv, the name write is the only thing that touches the pane during a spawn on
// a launch-named harness, so treating its failure as unconditionally cosmetic
// means a harness that died between creation and naming comes back as a live,
// idle session that never ran anything. Liveness is the signal that separates
// "cosmetic" from "dead".
func (m *Manager) forgiveSpawnNameFailure(ctx context.Context, handle ports.RuntimeHandle, id domain.SessionID, nameErr error) bool {
	alive, err := m.runtime.IsAlive(ctx, handle)
	if err != nil || !alive {
		return false
	}
	m.logger.Warn("spawn: session name not delivered to the harness; keeping the live session",
		"sessionID", id, "error", nameErr)
	return true
}
