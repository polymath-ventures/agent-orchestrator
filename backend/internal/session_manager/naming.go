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
	if strings.TrimSpace(cfg.DisplayName) != "" {
		// The entry points validate an operator's name and report a usable error;
		// this is the backstop for any caller that reaches the manager directly.
		// Falling back to the computed name rather than failing the spawn keeps a
		// cosmetic field from costing a session.
		name, err := domain.ValidateSessionDisplayName(cfg.DisplayName)
		if err == nil {
			return name
		}
		m.logger.Warn("spawn: supplied display name rejected; using the computed name instead",
			"projectID", cfg.ProjectID, "error", err)
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

// launchArgvCarriedName reports whether the command that just started this
// process already named the session.
//
// Only a fresh spawn can name in argv: a resume command carries no launch-time
// name flag, so a restored session always needs the in-harness path even on a
// harness that has one. Asking the adapter "can you name at launch?" instead of
// "did this launch name it?" is what silently skipped redelivery on restore.
func launchArgvCarriedName(agent ports.Agent, name string) bool {
	namer, ok := agent.(ports.AgentNamer)
	return ok && len(namer.LaunchNameArgs(name)) > 0
}

// deliverNameAfterStart pushes a session's name into an already-running harness.
//
// The write waits for the harness to be ready first: runtime creation returns as
// soon as the pane exists, which is before the TUI has drawn an input box to
// receive keystrokes. Callers that named the session in argv skip this entirely —
// a name delivered in argv lands atomically with process start, so for those
// spawns the pane-readiness race is absent rather than mitigated.
func (m *Manager) deliverNameAfterStart(ctx context.Context, agent ports.Agent, cfg ports.LaunchConfig, handle ports.RuntimeHandle, id domain.SessionID, name string) error {
	if strings.TrimSpace(name) == "" {
		return nil
	}
	if _, ok := agent.(ports.AgentNamer); !ok {
		return nil
	}
	if err := m.waitForPromptReadiness(ctx, agent, cfg, handle); err != nil {
		return err
	}
	rec, err := m.getRecord(ctx, id)
	if err != nil {
		return err
	}
	// A worker whose prompt rides argv may already be mid-turn by the time its
	// harness reports ready, so the spawn write must tolerate an active session
	// or exactly the sessions this change exists for would go unnamed. It must
	// still refuse when the session awaits the human: a paste plus Enter into a
	// permission dialog answers the dialog. Nudge is that policy.
	return m.deliverName(ctx, rec, m.messenger.Nudge)
}

// DeliverName pushes a session's persisted display name into its running
// harness. It is the delivery half of a rename: the session service owns the
// database write, this owns the pane.
func (m *Manager) DeliverName(ctx context.Context, id domain.SessionID) error {
	rec, err := m.getRecord(ctx, id)
	if err != nil {
		return err
	}
	// Unsolicited, relative to whatever the agent is doing: an operator renaming
	// a long-running session has no idea whether it is mid-turn or sitting on a
	// permission dialog. The coordination policy writes only while the session is
	// idle and re-reads that at the write boundary, so the check cannot go stale
	// between the decision and the keystroke. A harness that takes mid-turn input
	// treats it as steering and one that does not queues it as the next prompt —
	// either way the rename would reach the model as text, and a cosmetic write is
	// never worth disturbing work in progress. Passing no steers-active-turn
	// predicate is deliberate: no harness is exempt.
	return m.deliverName(ctx, rec, func(ctx context.Context, id domain.SessionID, msg string) (sessionguard.Outcome, error) {
		return m.messenger.NudgeCoordination(ctx, id, msg, nil)
	})
}

// deliverName is the single routine every naming path goes through — spawn and
// rename alike — so a surface cannot be forgotten and the skip conditions are
// written once. It reads the name off the record rather than accepting it as an
// argument, which is what makes the string delivered to the harness identical to
// the one AO displays by construction rather than by convention.
//
// A session that is terminated or has no runtime keeps its persisted name and is
// not written to; an adapter that declares no naming capability is left alone
// rather than having text typed blindly into a TUI AO does not understand. The
// caller supplies the delivery policy, which is the one thing that legitimately
// differs between the two paths — see the two call sites.
func (m *Manager) deliverName(ctx context.Context, rec domain.SessionRecord, send nameSender) error {
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
	if !m.agentStillRunning(ctx, rec) {
		return nil
	}
	outcome, err := send(ctx, rec.ID, cmd)
	if err != nil {
		return fmt.Errorf("deliver name %s: %w", rec.ID, err)
	}
	if outcome != sessionguard.Sent {
		m.logger.Warn("session name write suppressed; harness keeps its own name",
			"sessionID", rec.ID, "outcome", outcome.String())
	}
	return nil
}

// nameSender is the guarded pane-write policy a naming path uses. Spawn's write
// is solicited and rename's is not, and that is the only difference between
// them; everything else about delivery is shared.
type nameSender func(ctx context.Context, id domain.SessionID, msg string) (sessionguard.Outcome, error)

// agentStillRunning requires positive proof that the pane is still running the
// agent before any name is typed into it.
//
// This is a security boundary, not a nicety. AO keeps a session's pane alive
// after the agent exits by exec'ing an interactive shell into it, so the pane
// outlives the process AO thinks it is talking to — and a session whose agent
// has died is not marked terminated until the reaper notices. A name delivered
// in that window is not text in a TUI, it is a command line in a shell, and a
// name is the one field an operator types freely. Confirming the agent is still
// there is what makes the whole naming path unable to execute anything, rather
// than relying on every future name being harmless.
//
// It fails closed: without proof, no write. A runtime that cannot answer loses
// harness naming and keeps AO's own name, which is the pre-change behavior.
func (m *Manager) agentStillRunning(ctx context.Context, rec domain.SessionRecord) bool {
	prober, ok := m.runtime.(launchProcessProber)
	if !ok {
		m.logger.Warn("session name not delivered: runtime cannot confirm the agent is still running",
			"sessionID", rec.ID)
		return false
	}
	command := strings.TrimSpace(rec.Metadata.LaunchCommand)
	if command == "" {
		m.logger.Warn("session name not delivered: no recorded launch command to confirm against",
			"sessionID", rec.ID)
		return false
	}
	running, err := prober.IsRunningCommand(ctx, ports.RuntimeHandle{ID: rec.Metadata.RuntimeHandleID}, command)
	if err != nil || !running {
		m.logger.Warn("session name not delivered: the pane is no longer running the agent",
			"sessionID", rec.ID, "error", err)
		return false
	}
	return true
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

// deliverNameForSpawn names a freshly spawned session, skipping the post-start
// write when this launch's argv already carried the name.
func (m *Manager) deliverNameForSpawn(ctx context.Context, agent ports.Agent, cfg ports.LaunchConfig, handle ports.RuntimeHandle, id domain.SessionID, name string) error {
	if launchArgvCarriedName(agent, name) {
		return nil
	}
	return m.deliverNameAfterStart(ctx, agent, cfg, handle, id, name)
}
