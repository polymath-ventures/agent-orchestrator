package domain

import "regexp"

// projectIDPattern is the single source of truth for the project-id character
// set that a NEW id must satisfy. A session id is composed as
// {project}-{num}-{generation}, so the project id inherits every constraint of
// every surface derived from it. tmux is the binding one: `.` and `:` are its
// target grammar (session:window.pane), and it silently rewrites them to `_` in
// a session name rather than rejecting them — leaving the name unaddressable and
// the session untrackable. git tolerates a lone `.` in a refname but rejects
// `..` and `:`; workspace paths and request paths tolerate all of them. The
// intersection is [A-Za-z0-9_-]. The leading-alphanumeric anchor plus this class
// already rejects "", ".", "..", "/", and "\\".
//
// It is unexported on purpose: callers get the predicate, not a mutable
// *regexp.Regexp they could reassign or call Longest on and change the invariant
// globally. Any new surface stricter than this belongs here, not as a local
// sanitizer at the point of use — a surface that quietly rewrites its own copy of
// the id produces a second identity AO cannot map back. See
// docs/session-identity.md.
var projectIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

// IsValidProjectID reports whether id is usable, unchanged, across every surface
// derived from it (tmux session names, git refnames, workspace paths, request
// paths). It is the rule for admitting a NEW project id; operations on an
// already-stored id use a looser traversal-safe check so a project registered
// before this rule tightened stays manageable (see the project service).
func IsValidProjectID(id string) bool {
	return projectIDPattern.MatchString(id)
}
