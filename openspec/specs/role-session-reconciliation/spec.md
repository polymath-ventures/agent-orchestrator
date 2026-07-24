# role-session-reconciliation Specification

## Purpose

Defines the desired-state lifecycle contract shared by AO's two durable **role
sessions**: the fleet-wide Prime singleton and each project's Orchestrator.

Role sessions differ from worker sessions in that AO keeps them _present_: each
lives on a canonical role branch and should exist whenever it is desired, rather
than being spawned on demand and correctly left terminated. This capability owns
how a desired role session is reconciled into existence, how stale resources left
by terminated role rows (a worktree still holding the canonical role branch, a
leaked runtime) are released before a replacement is created, and the safety
invariant that historical terminated role rows can neither block a replacement
nor destroy the live one.

## Requirements

### Requirement: Role sessions share one desired-state reconciliation contract

The daemon SHALL treat fleet Prime and project Orchestrators as role sessions governed by a single
reconciliation operation that takes a role target and makes the desired state true. Role spawn,
role replacement, supervisor ensure paths, and user-initiated relaunch SHALL all route through that
operation rather than through role-specific spawn implementations.

#### Scenario: Both role kinds use the same reconciliation path

- **WHEN** a fleet Prime and a project Orchestrator are each reconciled after an identical failure
  (a terminated row still holding the canonical role branch)
- **THEN** both recover through the same reconciliation operation
- **AND** neither role kind requires a role-specific recovery implementation

#### Scenario: Supervisor ensure paths converge

- **WHEN** the Prime supervisor observes either a missing Prime or an unhealthy Prime
- **THEN** both observations drive the same reconciliation operation
- **AND** the two paths do not differ in which stale resources they release

#### Scenario: Reconciliation is idempotent

- **WHEN** reconciliation is invoked for a role target that already has a healthy active session
- **THEN** no replacement session is created
- **AND** the existing active role session remains the singleton for that target

### Requirement: A desired role session is created even when the newest role row is terminated

The daemon SHALL create or relaunch a role session whenever that role session is desired and no
active role session exists for the target, regardless of whether the most recent role row for that
target is terminated. A terminated role row SHALL NOT prevent reconciliation from producing an active
replacement.

#### Scenario: Terminated newest row does not block relaunch

- **WHEN** the only row for a role target is terminated and the role session is desired
- **THEN** reconciliation creates an active replacement role session
- **AND** the replacement is reported as the active session for that target

#### Scenario: Role killed from its terminal recovers

- **WHEN** a role session is killed from its terminal so that its row becomes terminated while it is
  still desired
- **THEN** the daemon either creates a replacement within the restart policy or reports a recoverable
  state that an explicit relaunch can resolve

### Requirement: Reconciliation releases stale role resources before creating a replacement

Before creating a replacement, reconciliation SHALL release stale resources held by terminated role
sessions for the same role target. Release SHALL be keyed on the canonical role branch, SHALL cover
the worktree holding that branch, and SHALL succeed when the holding worktree contains only
AO-managed runtime residue that is ignored by version control.

#### Scenario: Canonical role branch held by a terminated session is released

- **WHEN** a terminated role session's worktree still has the canonical role branch checked out and a
  replacement is requested
- **THEN** reconciliation releases that worktree before creating the replacement
- **AND** the replacement is created without a branch-already-checked-out failure

#### Scenario: Ignored runtime residue does not block release

- **WHEN** the worktree holding the canonical role branch is otherwise clean but contains
  version-control-ignored AO runtime residue
- **THEN** reconciliation still releases that worktree
- **AND** the release does not report a workspace teardown failure

#### Scenario: A replacement does not adopt a dead role worktree

- **WHEN** a role target uses a canonical workspace path and that path is held by a terminated role
  session
- **THEN** reconciliation releases the stale resource rather than adopting the existing worktree for
  the replacement

### Requirement: Reconciliation reaps runtimes leaked by terminated role sessions

Reconciliation SHALL destroy runtimes that are still alive for terminated role sessions of the target
role before creating a replacement. This behavior SHALL NOT depend on a daemon restart or a
boot-time-only reconcile pass.

#### Scenario: Leaked terminal runtime is reaped at relaunch

- **WHEN** a role session's row is terminated but its runtime is still alive, and a replacement is
  requested
- **THEN** reconciliation destroys the leaked runtime before creating the replacement

#### Scenario: Live role runtimes are not reaped

- **WHEN** reconciliation runs for a role target that has an active role session with a live runtime
- **THEN** the active session's runtime is left running

### Requirement: A workspace is destroyed only by the session that still owns it

Workspace teardown SHALL be gated by a single ownership determination shared by every teardown path.
A terminated session SHALL NOT be treated as the owner of a workspace path or canonical branch that
is currently held by an active session, and teardown for such a row SHALL be skipped with a reason
distinct from a teardown failure. A workspace no longer held by any active session SHALL remain
eligible for reclamation.

#### Scenario: Cleanup preserves the live replacement's workspace

- **WHEN** cleanup processes a terminated role row whose recorded workspace path is currently the
  active replacement role session's workspace
- **THEN** cleanup does not destroy that workspace
- **AND** the skip is reported as an ownership skip rather than a workspace teardown failure

#### Scenario: Cleanup preserves a workspace held on a live canonical branch

- **WHEN** cleanup processes a terminated role row whose canonical role branch is currently checked
  out by an active role session
- **THEN** cleanup does not destroy that worktree

#### Scenario: Unowned role workspaces are still reclaimed

- **WHEN** cleanup processes a terminated role row whose workspace path and canonical branch are held
  by no active session
- **THEN** cleanup destroys that workspace
- **AND** version-control-ignored runtime residue does not prevent the destruction

#### Scenario: Worker workspace teardown is unchanged

- **WHEN** cleanup processes a terminated worker session with uncommitted work in its workspace
- **THEN** teardown behavior for that worker session is unchanged by this capability
