package domain

import "regexp"

// ProjectIDPattern is the single source of truth for the project-id character
// set. A session id is composed as {project}-{num}-{generation}, so the project
// id inherits every constraint of every surface derived from it. tmux is the
// binding one: `.` and `:` are its target grammar (session:window.pane), and it
// silently rewrites them to `_` in a session name rather than rejecting them —
// leaving the name unaddressable and the session untrackable. git tolerates a
// lone `.` in a refname but rejects `..` and `:`; workspace paths and request
// paths tolerate all of them. The intersection is [A-Za-z0-9_-]. The
// leading-alphanumeric anchor plus this class already reject "", ".", "..",
// "/", and "\\".
//
// Any new surface stricter than this belongs here, not as a local sanitizer at
// the point of use — a surface that quietly rewrites its own copy of the id
// produces a second identity AO cannot map back. See docs/session-identity.md.
var ProjectIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

// IsValidProjectID reports whether id is usable, unchanged, across every surface
// derived from it (tmux session names, git refnames, workspace paths, request
// paths).
func IsValidProjectID(id string) bool {
	return ProjectIDPattern.MatchString(id)
}
