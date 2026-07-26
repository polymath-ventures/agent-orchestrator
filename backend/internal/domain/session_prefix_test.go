package domain

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestDeriveSessionPrefixFromName(t *testing.T) {
	tests := []struct {
		name        string
		projectName string
		projectID   string
		want        string
	}{
		{"two words yield their initials", "Coach Claw", "coachclaw", "cc"},
		{"three words yield three initials", "Polymath Ventures Inc", "pvi-repo", "pvi"},
		{"more words than the cap stop at the cap", "One Two Three Four Five", "otf", "ott"},
		{"a single word yields its leading characters", "mirrorborn", "mirrorborn", "mir"},
		{"a short single word is kept whole", "ao", "ao", "ao"},
		{"punctuation separates words", "agent-orchestrator", "agent-orchestrator", "ao"},
		{"mixed separators still split", "coach_claw.web", "coachclaw", "ccw"},
		{"case is normalized", "COACH CLAW", "coachclaw", "cc"},
		{"digits are usable characters", "Project 42", "p42", "p4"},
		{"surrounding whitespace is ignored", "  Coach   Claw  ", "coachclaw", "cc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DeriveSessionPrefix(tt.projectName, tt.projectID, nil)
			if got != tt.want {
				t.Fatalf("DeriveSessionPrefix(%q, %q, nil) = %q, want %q", tt.projectName, tt.projectID, got, tt.want)
			}
		})
	}
}

func TestDeriveSessionPrefixNeverExceedsTheCap(t *testing.T) {
	names := []string{
		"Coach Claw",
		"Polymath Ventures Incorporated Holdings",
		"mirrorborn",
		"a",
		"",
		strings.Repeat("word ", 40),
	}
	for _, name := range names {
		got := DeriveSessionPrefix(name, "some-project-id", nil)
		if n := utf8.RuneCountInString(got); n == 0 || n > MaxSessionPrefixRunes {
			t.Fatalf("DeriveSessionPrefix(%q, ...) = %q with %d runes, want 1..%d", name, got, n, MaxSessionPrefixRunes)
		}
	}
}

// Determinism is what lets the rule be stated once and reasoned about: the same
// name against the same taken set must not drift between calls.
func TestDeriveSessionPrefixIsDeterministic(t *testing.T) {
	taken := []string{"cc", "coa"}
	first := DeriveSessionPrefix("Coach Claw", "coachclaw", taken)
	for i := 0; i < 10; i++ {
		if got := DeriveSessionPrefix("Coach Claw", "coachclaw", taken); got != first {
			t.Fatalf("call %d returned %q, want %q — derivation is not deterministic", i, got, first)
		}
	}
}

func TestDeriveSessionPrefixResolvesCollisions(t *testing.T) {
	tests := []struct {
		name        string
		projectName string
		projectID   string
		taken       []string
		want        string
	}{
		{
			name:        "a taken candidate lengthens from the name's own characters",
			projectName: "Coach Claw",
			projectID:   "coachclaw",
			taken:       []string{"cc"},
			want:        "coa",
		},
		{
			name:        "an exhausted name falls back to the smallest free numeric suffix",
			projectName: "Coach Claw",
			projectID:   "coachclaw",
			taken:       []string{"cc", "coa"},
			want:        "cc2",
		},
		{
			name:        "the numeric suffix skips the ones already taken",
			projectName: "Coach Claw",
			projectID:   "coachclaw",
			taken:       []string{"cc", "coa", "cc2", "cc3"},
			want:        "cc4",
		},
		{
			name:        "a single word lengthens to its own next slice",
			projectName: "mirrorborn",
			projectID:   "mirrorborn",
			taken:       []string{"mir"},
			want:        "mi2",
		},
		{
			name:        "comparison against taken prefixes is case-insensitive",
			projectName: "Coach Claw",
			projectID:   "coachclaw",
			taken:       []string{"CC"},
			want:        "coa",
		},
		{
			name:        "surrounding whitespace in taken prefixes still counts as taken",
			projectName: "Coach Claw",
			projectID:   "coachclaw",
			taken:       []string{"  cc  "},
			want:        "coa",
		},
		{
			name:        "a prefix at the cap still resolves",
			projectName: "Polymath Ventures Inc",
			projectID:   "pvi-repo",
			taken:       []string{"pvi"},
			want:        "pol",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DeriveSessionPrefix(tt.projectName, tt.projectID, tt.taken)
			if got != tt.want {
				t.Fatalf("DeriveSessionPrefix(%q, %q, %v) = %q, want %q", tt.projectName, tt.projectID, tt.taken, got, tt.want)
			}
		})
	}
}

// The point of the change: contention resolves to a fresh prefix every time,
// well past the handful of candidates the name itself offers. The guarantee is
// bounded by the capped prefix space — see the exhaustion test below for the
// other side of that boundary.
func TestDeriveSessionPrefixIsUniqueUnderContention(t *testing.T) {
	var taken []string
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		got := DeriveSessionPrefix("Coach Claw", "coachclaw", taken)
		if got == "" {
			t.Fatalf("iteration %d derived an empty prefix", i)
		}
		if seen[got] {
			t.Fatalf("iteration %d re-derived %q, which was already taken", i, got)
		}
		seen[got] = true
		taken = append(taken, got)
	}
}

// The bounded side of the uniqueness contract. With every prefix the rule can
// produce already taken, derivation still returns a usable value rather than a
// blank or a failure — a duplicate an operator can retype beats a project that
// cannot be registered. This pins the deliberate terminal behavior so a future
// change cannot quietly turn it into an empty string.
func TestDeriveSessionPrefixDegradesToADuplicateWhenTheSpaceIsExhausted(t *testing.T) {
	alphabet := []rune(prefixTokenAlphabet)
	taken := make([]string, 0, len(alphabet)*len(alphabet)*len(alphabet)+2)
	for _, a := range alphabet {
		for _, b := range alphabet {
			for _, c := range alphabet {
				taken = append(taken, string([]rune{a, b, c}))
			}
		}
	}
	// The name-drawn candidates are shorter than the sweep's fixed width, so they
	// are not in the block above and must be taken explicitly.
	taken = append(taken, "cc", "coa")

	got := DeriveSessionPrefix("Coach Claw", "coachclaw", taken)
	if got == "" {
		t.Fatalf("exhausted space derived an empty prefix; a blank prefix is the state this rule exists to remove")
	}
	if n := utf8.RuneCountInString(got); n > MaxSessionPrefixRunes {
		t.Fatalf("exhausted space derived %q with %d runes, want at most %d", got, n, MaxSessionPrefixRunes)
	}
}

func TestDeriveSessionPrefixFallsBackToTheProjectID(t *testing.T) {
	// A name with no usable characters cannot produce a prefix, but creation must
	// not fail and must not land on a value every such project shares.
	got := DeriveSessionPrefix("!!! ???", "coachclaw", nil)
	if got != "coa" {
		t.Fatalf("DeriveSessionPrefix with an unusable name = %q, want the id-derived %q", got, "coa")
	}
}

func TestDeriveSessionPrefixIsDistinctWhenNothingIsUsable(t *testing.T) {
	// Neither the name nor the id yields characters. A shared literal here is the
	// defect this rule exists to remove — "ao" on every project — so two projects
	// in this state must still differ.
	first := DeriveSessionPrefix("!!!", "###", nil)
	second := DeriveSessionPrefix("???", "***", nil)
	if first == "" || second == "" {
		t.Fatalf("unusable inputs derived an empty prefix: %q, %q", first, second)
	}
	if first == second {
		t.Fatalf("two unusable-input projects both derived %q; the fallback is a shared literal", first)
	}
}

func TestDeriveSessionPrefixEmptyInputsStillYieldAPrefix(t *testing.T) {
	got := DeriveSessionPrefix("", "", nil)
	if n := utf8.RuneCountInString(got); n == 0 || n > MaxSessionPrefixRunes {
		t.Fatalf("DeriveSessionPrefix(\"\", \"\", nil) = %q with %d runes, want 1..%d", got, n, MaxSessionPrefixRunes)
	}
}

// A derived prefix is the head of a session name and is persisted into project
// config, so it must satisfy both gates by construction — never rejected by the
// validation that guards operator-typed values.
func TestDerivedSessionPrefixesAlwaysPassNameValidation(t *testing.T) {
	names := []string{
		"Coach Claw",
		"agent-orchestrator",
		"mirrorborn",
		"!!! ???",
		"",
		"Ünïcödé Prøject",
		"emoji 🎉 project",
		"tab\tand\nnewline",
	}
	for _, name := range names {
		got := DeriveSessionPrefix(name, "some-id", nil)
		if got == "" {
			t.Fatalf("DeriveSessionPrefix(%q, ...) returned empty", name)
		}
		if strings.TrimSpace(got) != got {
			t.Fatalf("DeriveSessionPrefix(%q, ...) = %q, which carries surrounding whitespace", name, got)
		}
		for _, r := range got {
			if !NameRuneAllowed(r) {
				t.Fatalf("DeriveSessionPrefix(%q, ...) = %q, which carries disallowed rune %q", name, got, r)
			}
		}
		if strings.ContainsAny(got, `/\`) || got == "." || got == ".." {
			t.Fatalf("DeriveSessionPrefix(%q, ...) = %q, which config validation rejects", name, got)
		}
		// The prefix heads every worker name, so it must leave room for the rest.
		if composed := ComposeWorkerDisplayName(got, "151", "some work item title"); composed == "" {
			t.Fatalf("DeriveSessionPrefix(%q, ...) = %q, which composes an empty worker name", name, got)
		}
	}
}
