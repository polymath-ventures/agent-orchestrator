package domain

import "fmt"

// IsRole reports whether this kind is a durable *role* session — one AO keeps
// present by desired-state reconciliation rather than spawning on demand.
// Workers are not roles: they are spawned for a unit of work and are correctly
// allowed to stay terminated.
func (k SessionKind) IsRole() bool {
	return k == KindOrchestrator || k == KindPrime
}

// RoleTarget identifies one reconcilable role session: the fleet-wide Prime
// singleton, or one project's Orchestrator. It is the unit both the
// reconciliation entry point and the role lock are keyed on, so Prime and
// Orchestrator cannot drift into separate recovery implementations.
type RoleTarget struct {
	Kind      SessionKind
	ProjectID ProjectID
}

// PrimeTarget returns the fleet Prime role target. Prime is projectless.
func PrimeTarget() RoleTarget {
	return RoleTarget{Kind: KindPrime}
}

// OrchestratorTarget returns the role target for one project's Orchestrator.
func OrchestratorTarget(projectID ProjectID) RoleTarget {
	return RoleTarget{Kind: KindOrchestrator, ProjectID: projectID}
}

// RoleTargetForSession derives the role target a session belongs to, reporting
// false for sessions that are not role sessions.
func RoleTargetForSession(rec SessionRecord) (RoleTarget, bool) {
	if !rec.Kind.IsRole() {
		return RoleTarget{}, false
	}
	target := RoleTarget{Kind: rec.Kind}
	if rec.Kind != KindPrime {
		target.ProjectID = rec.ProjectID
	}
	return target, true
}

// Validate rejects targets that cannot name a real role session: a non-role
// kind, an Orchestrator without a project, or a Prime carrying one (Prime is
// fleet-owned and projectless).
func (t RoleTarget) Validate() error {
	if !t.Kind.IsRole() {
		return fmt.Errorf("role target: kind %q is not a role session kind", t.Kind)
	}
	if t.Kind == KindPrime && t.ProjectID != "" {
		return fmt.Errorf("role target: prime is projectless but carries project %q", t.ProjectID)
	}
	if t.Kind == KindOrchestrator && t.ProjectID == "" {
		return fmt.Errorf("role target: orchestrator requires a project id")
	}
	return nil
}

// Key is the target's stable identity, used to serialize reconciliation per
// role. The kind is included so an Orchestrator can never collide with Prime —
// including a project whose id happens to match the old "__fleet_prime__"
// sentinel this replaces.
func (t RoleTarget) Key() string {
	return fmt.Sprintf("%s/%s", t.Kind, t.ProjectID)
}

// String renders the target for logs and error messages.
func (t RoleTarget) String() string {
	if t.Kind == KindPrime {
		return string(KindPrime)
	}
	return fmt.Sprintf("%s(%s)", t.Kind, t.ProjectID)
}
