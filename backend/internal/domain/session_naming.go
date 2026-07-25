package domain

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// orchestratorNameSuffix marks a project's orchestrator session. The harness is
// deliberately absent from the grammar: it is surfaced separately in the UI, and
// spending runes on it would clip the title slug that distinguishes workers.
const orchestratorNameSuffix = " Orc"

// ComposeWorkerDisplayName builds a worker's name from the project's session
// prefix, the work item number, and a slug of the work item title:
// `<prefix> #<issue> <slug>`.
//
// Every part is optional except the prefix, and the name degrades from the tail
// inward: an unresolvable title yields `<prefix> #<issue>`, and an unresolvable
// work item yields `<prefix>`. A worker always belongs to a project, so the
// prefix is always present and the result is never empty in practice.
//
// The result is capped at MaxSessionDisplayNameRunes with the head — prefix and
// issue number — preserved, because the head is what identifies the session; the
// title slug is the part that may be clipped.
func ComposeWorkerDisplayName(prefix, issueID, issueTitle string) string {
	head := roleDisplayName(sanitizeNamePart(prefix), workerIssueSuffix(issueID), MaxSessionDisplayNameRunes)
	if head == "" {
		return ""
	}
	slug := sanitizeNamePart(issueTitle)
	if slug == "" {
		return head
	}
	// One rune for the separator; anything less leaves no room for a slug.
	budget := MaxSessionDisplayNameRunes - len([]rune(head)) - 1
	if budget <= 0 {
		return head
	}
	slug = fitWords(slug, budget)
	if slug == "" {
		return head
	}
	return head + " " + slug
}

// ComposeOrchestratorDisplayName builds a project orchestrator's name from the
// project's session prefix and the orchestrator role suffix: `<prefix> Orc`.
func ComposeOrchestratorDisplayName(prefix string) string {
	return roleDisplayName(sanitizeNamePart(prefix), orchestratorNameSuffix, MaxSessionDisplayNameRunes)
}

// ErrDisplayNameTooLong reports a session name over MaxSessionDisplayNameRunes.
var ErrDisplayNameTooLong = fmt.Errorf("display name must be at most %d characters", MaxSessionDisplayNameRunes)

// ErrDisplayNameEmpty reports a session name that is blank once trimmed.
var ErrDisplayNameEmpty = errors.New("display name is required")

// ErrDisplayNameUnsafe reports a session name carrying a character AO will not
// type into a terminal. See NameRuneAllowed.
var ErrDisplayNameUnsafe = errors.New("display name may not contain control characters or shell syntax")

// ValidateSessionDisplayName checks an operator-supplied name against the one
// cap every session name obeys, and returns the trimmed value.
//
// It rejects rather than repairs, and it lives here rather than in each entry
// point on purpose: a caller that silently shortened a name would persist a
// string the operator did not choose, and a caller that skipped the check
// entirely — as the rename endpoint did — would persist a name that exceeds the
// cap the spawn path enforces. Composed names satisfy all of this by
// construction, so this guards the override paths.
//
// It holds a supplied name to the same character set delivery does. Accepting a
// name here that delivery would later refuse is the one outcome worth avoiding:
// AO would display a name the harness never received, which is precisely the
// divergence this change exists to remove.
func ValidateSessionDisplayName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", ErrDisplayNameEmpty
	}
	if utf8.RuneCountInString(trimmed) > MaxSessionDisplayNameRunes {
		return "", ErrDisplayNameTooLong
	}
	for _, r := range trimmed {
		if !NameRuneAllowed(r) {
			return "", ErrDisplayNameUnsafe
		}
	}
	return trimmed, nil
}

// workerIssueSuffix renders the work item number as the second element of the
// name's head. Tracker ids reach AO in several shapes — `150`, `#150`, and
// `owner/repo#150` all name the same issue — so only the trailing number is
// kept. An id with no number yields no suffix, degrading the name to the prefix.
func workerIssueSuffix(issueID string) string {
	id := strings.TrimSpace(issueID)
	if hash := strings.LastIndexByte(id, '#'); hash >= 0 {
		id = id[hash+1:]
	}
	// Sanitized like every other tracker-supplied part: a work item id reaches AO
	// from a CLI flag, an HTTP body, or tracker intake, so it is no more trusted
	// than the title beside it.
	id = sanitizeNamePart(id)
	if id == "" {
		return ""
	}
	return " #" + id
}

// sanitizeNamePart makes a tracker-supplied string safe to both display and
// type into a harness: anything outside NameRuneAllowed becomes a space (a
// newline typed into a rename command would submit it mid-name, and dropping a
// character outright would weld the surrounding words together) and whitespace
// runs collapse to single spaces.
//
// Issue titles are arbitrary user-authored text and they flow into names, so
// this is where that text stops being arbitrary.
func sanitizeNamePart(s string) string {
	stripped := strings.Map(func(r rune) rune {
		if !NameRuneAllowed(r) {
			return ' '
		}
		return r
	}, s)
	return strings.Join(strings.Fields(stripped), " ")
}

// nameShellActive is the ASCII shell grammar: everything that can start a
// command, substitute one, expand, quote, escape, or glob in sh/bash/zsh. `!`
// is here because the terminal a stray name could reach is an interactive
// shell, where it is history expansion.
//
// `#` is deliberately absent — the naming grammar is built on it, and in command
// position it opens a comment, which is inert. `=`, `%`, `+`, `@`, `:` and `/`
// are likewise ordinary once a command word precedes them.
const nameShellActive = "\"$&'()*;<>?[\\]^`{|}~!"

// NameRuneAllowed reports whether a rune may appear in a session name.
//
// The restriction exists because a name is the one string AO types into a
// terminal it does not fully control. A session's pane outlives its agent — the
// runtime execs an interactive shell to keep the terminal inspectable — so a
// naming write can, in a narrow window, land at a shell prompt instead of in a
// TUI. Callers check that the agent is still running first, but that check
// cannot be atomic with the keystroke.
//
// Rather than narrow the race, this removes what the race could cost: a name
// that cannot start, substitute, or expand a command cannot become one no matter
// when it lands. `/rename my-session` at a shell prompt is a command-not-found;
// `/rename x; curl …` would not have been. The property holds by construction,
// so no future timing change can reopen it.
//
// It is a deny list rather than an allow list because shell grammar is entirely
// ASCII: to a shell, every non-ASCII rune is an ordinary word character. An
// allow list would have to enumerate the world's punctuation to avoid mangling
// legitimate titles — curly apostrophes, en dashes, emoji — for no security
// gain. Invisible runes are still refused: a format character can hide content
// from the operator reading the name.
func NameRuneAllowed(r rune) bool {
	if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
		return false
	}
	if r < utf8.RuneSelf {
		return !strings.ContainsRune(nameShellActive, r)
	}
	return true
}

// fitWords returns the longest whole-word prefix of s that fits in limit runes,
// falling back to a hard clip when even the first word is too long.
func fitWords(s string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len([]rune(s)) <= limit {
		return s
	}
	fitted := ""
	for _, word := range strings.Fields(s) {
		candidate := word
		if fitted != "" {
			candidate = fitted + " " + word
		}
		if len([]rune(candidate)) > limit {
			break
		}
		fitted = candidate
	}
	if fitted == "" {
		return capRunes(s, limit)
	}
	return fitted
}
