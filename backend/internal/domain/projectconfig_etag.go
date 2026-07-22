package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
)

// EmptyConfigETag is the token for a project that has no config stored. It has to
// be a real, comparable value rather than "": a caller that has read an empty
// config and one that has read nothing at all are indistinguishable otherwise,
// and the first write would be unable to prove it was not stale.
const EmptyConfigETag = "empty"

// ETag returns a token identifying this exact config content.
//
// It is the concurrency primitive for the config write path. The stored config is
// a single deterministic JSON blob — Go emits struct fields in declaration order
// and sorts map keys — so hashing that blob yields a token that changes whenever
// the content does, with no schema change and no new column. That matters: the
// alternative, a version column, needs a migration, and a migration needs a
// version number, which is the one thing concurrent PRs collide on.
func (c ProjectConfig) ETag() string {
	if c.IsZero() {
		return EmptyConfigETag
	}
	b, err := json.Marshal(c)
	if err != nil {
		// A ProjectConfig contains no channel, func, or cyclic value, so Marshal
		// cannot fail. If it somehow did, returning a token that matches nothing is
		// the safe direction: writes conflict rather than silently clobber.
		return "unhashable"
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// ETagMatches reports whether an If-Match token authorizes a write over this
// config. The wildcard "*" means "I know I am overwriting whatever is there" — it
// is how a deliberate whole-object writer (the config-as-code restore path, which
// exists precisely to overwrite drift) opts out of the check. An empty token means
// the client sent none. Strong (`"tok"`), weak (`W/"tok"`), and comma-separated
// list forms are all accepted so a standard HTTP If-Match header value matches.
func (c ProjectConfig) ETagMatches(token string) bool {
	want := c.ETag()
	for _, candidate := range strings.Split(token, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" {
			return true
		}
		candidate = strings.TrimPrefix(candidate, "W/")
		if unquoted, err := strconv.Unquote(candidate); err == nil {
			candidate = unquoted
		}
		if candidate == want {
			return true
		}
	}
	return false
}
