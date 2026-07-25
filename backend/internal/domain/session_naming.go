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

// ValidateSessionDisplayName checks an operator-supplied name against the one
// cap every session name obeys, and returns the trimmed value.
//
// It rejects rather than repairs, and it lives here rather than in each entry
// point on purpose: a caller that silently shortened a name would persist a
// string the operator did not choose, and a caller that skipped the check
// entirely — as the rename endpoint did — would persist a name that exceeds the
// cap the spawn path enforces. Composed names are already within the cap by
// construction, so this guards the override paths.
func ValidateSessionDisplayName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", ErrDisplayNameEmpty
	}
	if utf8.RuneCountInString(trimmed) > MaxSessionDisplayNameRunes {
		return "", ErrDisplayNameTooLong
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
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	return " #" + id
}

// sanitizeNamePart makes a tracker-supplied string safe to both display and
// type into a harness: control characters become spaces (a newline typed into a
// rename command would submit it mid-name, and dropping the character outright
// would weld the surrounding words together) and whitespace runs collapse to
// single spaces.
func sanitizeNamePart(s string) string {
	stripped := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	return strings.Join(strings.Fields(stripped), " ")
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
