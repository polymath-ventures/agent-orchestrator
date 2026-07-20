package domain

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// ModelProvider identifies the vendor family a model string or agent harness
// belongs to. Model names are provider-specific — a Claude model is invalid for
// a Codex agent and vice versa — so model resolution keys the model to the
// spawn's harness through this family. ProviderUnknown means "not classified"
// and is treated permissively: novel models and the many harnesses AO has not
// mapped are passed through as configured rather than rejected.
type ModelProvider string

// The model vendor families AO classifies. Everything else is ProviderUnknown.
const (
	ProviderUnknown   ModelProvider = ""
	ProviderAnthropic ModelProvider = "anthropic"
	ProviderOpenAI    ModelProvider = "openai"
)

// ClassifyModelProvider infers the vendor family of a model string from
// well-known name fragments. It is deliberately conservative: an unrecognized
// model returns ProviderUnknown so callers stay permissive rather than reject a
// model AO simply has not seen. Family fragments match on letter boundaries so a
// longer word that merely contains one — e.g. "octopus" containing "opus" — is
// not misread as that family and falsely rejected on a cross-provider bucket.
func ClassifyModelProvider(model string) ModelProvider {
	m := strings.ToLower(strings.TrimSpace(model))
	if m == "" {
		return ProviderUnknown
	}
	switch {
	case hasModelFamily(m, "claude"),
		hasModelFamily(m, "opus"),
		hasModelFamily(m, "sonnet"),
		hasModelFamily(m, "haiku"),
		hasModelFamily(m, "fable"):
		return ProviderAnthropic
	case hasModelFamily(m, "gpt"),
		hasModelFamily(m, "codex"),
		strings.HasPrefix(m, "o1"),
		strings.HasPrefix(m, "o3"),
		strings.HasPrefix(m, "o4"):
		return ProviderOpenAI
	default:
		return ProviderUnknown
	}
}

// hasModelFamily reports whether frag appears in m delimited by non-letters on
// both sides (string edges, digits, or separators like '-'/'.' all count as
// boundaries). The adjacent characters are decoded as runes and tested with
// unicode.IsLetter, so a multibyte letter next to the fragment is a letter, not
// a boundary — "éopus" does not classify as the "opus" family. This keeps
// "claude-opus-4" and "gpt-4o" matching while rejecting an embedded substring
// such as "opus" inside "octopus".
func hasModelFamily(m, frag string) bool {
	for start := 0; ; {
		i := strings.Index(m[start:], frag)
		if i < 0 {
			return false
		}
		lo := start + i
		hi := lo + len(frag)
		beforeOK := lo == 0
		if !beforeOK {
			r, _ := utf8.DecodeLastRuneInString(m[:lo])
			beforeOK = !unicode.IsLetter(r)
		}
		afterOK := hi == len(m)
		if !afterOK {
			r, _ := utf8.DecodeRuneInString(m[hi:])
			afterOK = !unicode.IsLetter(r)
		}
		if beforeOK && afterOK {
			return true
		}
		start = lo + 1
	}
}

// CompatibleWith reports whether a model of provider p may be passed to a
// harness of provider harnessProvider. It is permissive by design: an unknown
// provider on either side (an unclassified model or an unmapped harness) is
// always compatible, so guarding only ever fires on a known-vs-known mismatch.
func (p ModelProvider) CompatibleWith(harnessProvider ModelProvider) bool {
	if p == ProviderUnknown || harnessProvider == ProviderUnknown {
		return true
	}
	return p == harnessProvider
}
