package domain

import (
	"hash/fnv"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxSessionPrefixRunes caps a derived session prefix.
//
// Three, because the prefix is the head of a name capped at
// MaxSessionDisplayNameRunes and it is the least identifying part of one:
// `<prefix> #151 ` already spends six runes before the title slug starts, so a
// longer prefix buys project legibility with the words that distinguish one
// session from the next. The prefix only has to say which project; the work item
// number says which work.
const MaxSessionPrefixRunes = 3

// prefixTokenAlphabet is the character space the last-resort sweep walks. It is
// the lowercase alphanumerics, which is what derivation produces everywhere else.
const prefixTokenAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

// DeriveSessionPrefix returns a project's session prefix, derived from its name
// and distinct from every prefix in taken while the sweep below still has a free
// token.
//
// It exists because the prefix heads every session name, and the name is the
// only project cue once names propagate into a harness's own flat, cross-project
// session list. A prefix shared by unrelated projects therefore costs more than
// an unlovely one: every project falling through to the same `ao` is the defect
// this replaces. Uniqueness comes first and legibility second, and the result is
// short on purpose.
//
// The rule, in order: the initials of a multi-word name, or the leading
// characters of a single-word one; then a longer draw from the name's own
// characters; then the smallest free numeric suffix that still fits the cap; then
// a deterministic sweep of prefixTokenAlphabet at every width the cap allows.
// The sweep is what makes uniqueness a property rather than an aspiration: it
// searches that whole space, so it returns a free prefix whenever one exists in
// it.
//
// That alphabet is the bound on the guarantee, and it is narrower than what
// derivation can emit: a name-drawn prefix may carry any letter or digit,
// including non-ASCII, while the sweep walks only lowercase ASCII
// alphanumerics. So the guarantee is exactly this — unique while any of the
// sweep's tokens is free — and not the broader claim that it holds while any
// representable prefix is free. Once the sweep's space is full, the last
// candidate is returned even though it duplicates, deliberately: the caller's
// alternative is refusing to create the project, and a duplicate prefix an
// operator can retype beats a project that cannot be registered.
//
// A name with no usable characters derives from projectID instead, and inputs
// with nothing usable at all derive a token seeded by those inputs. Neither path
// emits a shared constant and neither fails: a project is always creatable.
//
// taken is compared case- and whitespace-insensitively, and callers pass the
// prefixes projects *resolve* to rather than only the ones they store — a project
// that stores none still displays one, and colliding with a displayed prefix is
// the collision an operator actually sees.
func DeriveSessionPrefix(projectName, projectID string, taken []string) string {
	takenSet := make(map[string]struct{}, len(taken))
	for _, p := range taken {
		if norm := strings.ToLower(strings.TrimSpace(p)); norm != "" {
			takenSet[norm] = struct{}{}
		}
	}
	free := func(candidate string) bool {
		if candidate == "" {
			return false
		}
		_, clash := takenSet[candidate]
		return !clash
	}

	// The name is the operator's own word for the project, so it is tried first;
	// the id is the fallback because it is derived from the path and still says
	// something about the project.
	words := prefixWords(projectName)
	if len(words) == 0 {
		words = prefixWords(projectID)
	}

	base := basePrefix(words)
	if free(base) {
		return base
	}

	// Lengthen from the project's own characters before reaching for an arbitrary
	// suffix: a prefix that still reads like the project beats one that does not.
	if squeezed := capRunes(strings.Join(words, ""), MaxSessionPrefixRunes); squeezed != base && free(squeezed) {
		return squeezed
	}

	if base != "" {
		for n := 2; n <= 99; n++ {
			suffix := strconv.Itoa(n)
			candidate := capRunes(base, MaxSessionPrefixRunes-len(suffix)) + suffix
			if free(candidate) {
				return candidate
			}
		}
	}

	// Nothing drawn from the project is free — or the project offered nothing to
	// draw from. Walk the token space from an input-seeded offset, so two projects
	// in this state land in different places and neither lands on a constant.
	//
	// Every width the cap allows is swept, widest first: a narrower token is still
	// a legal prefix, so stopping at one width would hand back a duplicate while
	// legal values sat free. Widest first because the widest space is the largest
	// and leaves the scarcer short tokens available.
	seed := prefixSeed(projectName, projectID)
	for width := MaxSessionPrefixRunes; width >= 1; width-- {
		space := prefixTokenSpace(width)
		start := seed % space
		for i := uint32(0); i < space; i++ {
			if candidate := prefixToken((start+i)%space, width); free(candidate) {
				return candidate
			}
		}
	}

	// Every token the sweep can produce is taken. This is the one path that
	// returns a duplicate, and it is the least-bad option: a blank prefix would put
	// the caller back where this started, and an error would fail project creation
	// over a display detail.
	if base != "" {
		return base
	}
	return prefixToken(seed%prefixTokenSpace(MaxSessionPrefixRunes), MaxSessionPrefixRunes)
}

// basePrefix is the first candidate: one leading character per word for a
// multi-word name, or the leading characters of a single word.
func basePrefix(words []string) string {
	switch len(words) {
	case 0:
		return ""
	case 1:
		return capRunes(words[0], MaxSessionPrefixRunes)
	}
	initials := make([]rune, 0, MaxSessionPrefixRunes)
	for _, word := range words {
		if len(initials) >= MaxSessionPrefixRunes {
			break
		}
		r, _ := utf8.DecodeRuneInString(word)
		initials = append(initials, r)
	}
	return string(initials)
}

// prefixWords splits arbitrary project text into lowercase alphanumeric words.
// Everything else — punctuation, separators, emoji, whitespace — is a boundary,
// so the words are what a reader would call the words of the name.
func prefixWords(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// prefixToken renders n as a width-character token over prefixTokenAlphabet.
func prefixToken(n uint32, width int) string {
	base := uint32(len(prefixTokenAlphabet))
	out := make([]byte, width)
	for i := width - 1; i >= 0; i-- {
		out[i] = prefixTokenAlphabet[n%base]
		n /= base
	}
	return string(out)
}

// prefixTokenSpace is how many distinct tokens of the given width exist. It is
// uint32 to match the seed it bounds, which keeps the sweep free of conversions.
func prefixTokenSpace(width int) uint32 {
	space := uint32(1)
	for i := 0; i < width; i++ {
		space *= uint32(len(prefixTokenAlphabet))
	}
	return space
}

// prefixSeed hashes the inputs so the sweep starts somewhere the inputs chose.
// The separator keeps ("ab", "c") and ("a", "bc") apart.
func prefixSeed(projectName, projectID string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(projectID))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(projectName))
	return h.Sum32()
}
