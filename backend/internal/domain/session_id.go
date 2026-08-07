package domain

import (
	"regexp"
	"strings"
)

// sessionIDLookupPattern is the traversal-safe class for CONSUMING an
// already-composed session id. It tolerates a lone "." because a legacy dotted
// project (e.g. "example.net") yields a dotted-but-valid session id that must
// round-trip. It is the session-id analog of projectIDLookupPattern; see
// docs/session-identity.md for the full rationale (identity is checked at the
// point of use, never rewritten). Minting a NEW dotted id stays forbidden at
// registration (domain.IsValidProjectID).
var sessionIDLookupPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// IsPathSafeSessionID reports whether id is safe to use, unchanged, as a path
// component and a request-path segment. It is looser than the strict minting
// charset so a legacy dotted-project session id is not refused at launch (the
// #266 root cause), while still rejecting anything unsafe as a path component.
func IsPathSafeSessionID(id string) bool {
	// The anchored pattern already rejects "", ".", a leading dot, and path
	// separators. The remaining unsafe forms it admits are an embedded ".."
	// (e.g. "a..b") and a trailing "." (which Windows strips, aliasing paths),
	// so those are the only extra guards needed.
	return !strings.Contains(id, "..") && !strings.HasSuffix(id, ".") && sessionIDLookupPattern.MatchString(id)
}
