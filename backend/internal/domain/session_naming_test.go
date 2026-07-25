package domain

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestComposeWorkerDisplayName(t *testing.T) {
	tests := []struct {
		name    string
		prefix  string
		issueID string
		title   string
		want    string
	}{
		{"prefix, issue, and title slug", "ao", "150", "Unified naming", "ao #150 Unified"},
		{"issue id already carries the hash", "ao", "#150", "Unified naming", "ao #150 Unified"},
		{"repo-qualified issue id keeps only the number", "ao", "polymath/agent-orchestrator#150", "Unified naming", "ao #150 Unified"},
		{"missing title degrades to the head", "ao", "150", "", "ao #150"},
		{"missing issue drops only the issue element", "ao", "", "Unified naming", "ao Unified naming"},
		{"whitespace in the title collapses", "ao", "7", "  Fix   the\tthing ", "ao #7 Fix the thing"},
		{"control characters are stripped", "ao", "7", "Fix\x07 thing", "ao #7 Fix thing"},
		{"newlines never reach the name", "ao", "7", "Fix\nthing", "ao #7 Fix thing"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ComposeWorkerDisplayName(tt.prefix, tt.issueID, tt.title); got != tt.want {
				t.Fatalf("ComposeWorkerDisplayName(%q, %q, %q) = %q, want %q", tt.prefix, tt.issueID, tt.title, got, tt.want)
			}
		})
	}
}

// The head — prefix plus issue number — is the part that identifies the session
// uniquely, so the cap must clip the trailing title slug and never the head.
func TestComposeWorkerDisplayNameKeepsTheHeadWhenCapping(t *testing.T) {
	got := ComposeWorkerDisplayName("ao", "1929", "better sqlite3 upgrade CI failures")

	if n := utf8.RuneCountInString(got); n > MaxSessionDisplayNameRunes {
		t.Fatalf("ComposeWorkerDisplayName = %q (%d runes), want at most %d", got, n, MaxSessionDisplayNameRunes)
	}
	if !strings.HasPrefix(got, "ao #1929") {
		t.Fatalf("ComposeWorkerDisplayName = %q, want the head %q intact", got, "ao #1929")
	}
	if strings.HasSuffix(got, " ") {
		t.Fatalf("ComposeWorkerDisplayName = %q, want no trailing separator", got)
	}
}

// A head that is itself at or over the cap must still yield a capped name
// rather than overflowing, and the issue number must survive.
func TestComposeWorkerDisplayNameCapsAnOverlongHead(t *testing.T) {
	got := ComposeWorkerDisplayName("a-very-long-project-prefix", "1929", "anything")

	if n := utf8.RuneCountInString(got); n > MaxSessionDisplayNameRunes {
		t.Fatalf("ComposeWorkerDisplayName = %q (%d runes), want at most %d", got, n, MaxSessionDisplayNameRunes)
	}
	if !strings.HasSuffix(got, "#1929") {
		t.Fatalf("ComposeWorkerDisplayName = %q, want the issue number preserved", got)
	}
}

// A worker whose work item cannot be resolved at all still gets a name: the
// project prefix alone. Only a project-less caller yields no name, and no
// worker is project-less.
func TestComposeWorkerDisplayNameIsNonEmptyWithoutAWorkItem(t *testing.T) {
	if got := ComposeWorkerDisplayName("ao", "", ""); got == "" {
		t.Fatal("ComposeWorkerDisplayName with no work item = empty, want the project prefix")
	}
	if got := ComposeWorkerDisplayName("", "", ""); got != "" {
		t.Fatalf("ComposeWorkerDisplayName with nothing = %q, want empty", got)
	}
}

func TestComposeOrchestratorDisplayName(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		want   string
	}{
		{"prefix plus role suffix", "ao", "ao Orc"},
		{"long prefix caps before the suffix", "a-very-long-project-prefix", "a-very-long-proj Orc"},
		{"empty prefix yields no name", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComposeOrchestratorDisplayName(tt.prefix)
			if got != tt.want {
				t.Fatalf("ComposeOrchestratorDisplayName(%q) = %q, want %q", tt.prefix, got, tt.want)
			}
			if n := utf8.RuneCountInString(got); n > MaxSessionDisplayNameRunes {
				t.Fatalf("ComposeOrchestratorDisplayName(%q) = %q (%d runes), want at most %d", tt.prefix, got, n, MaxSessionDisplayNameRunes)
			}
		})
	}
}

func TestValidateSessionDisplayName(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr error
	}{
		{"trims", "  ao #7  ", "ao #7", nil},
		{"blank is rejected", "   ", "", ErrDisplayNameEmpty},
		{"empty is rejected", "", "", ErrDisplayNameEmpty},
		{"at the cap is accepted", strings.Repeat("x", MaxSessionDisplayNameRunes), strings.Repeat("x", MaxSessionDisplayNameRunes), nil},
		{"over the cap is rejected, not shortened", strings.Repeat("x", MaxSessionDisplayNameRunes+1), "", ErrDisplayNameTooLong},
		// Accepting a name here that delivery would refuse would leave AO showing
		// a name the harness never received.
		{"shell syntax is rejected", "x; touch /tmp/pwn", "", ErrDisplayNameUnsafe},
		{"command substitution is rejected", "x`id`", "", ErrDisplayNameUnsafe},
		{"an invisible rune is rejected", "ao\u200b Orc", "", ErrDisplayNameUnsafe},
		{"realistic punctuation is accepted", "Don’t regress", "Don’t regress", nil},
		{"a percentage is accepted", "100% rollout", "100% rollout", nil},
		{"an emoji is accepted", "🚀 release", "🚀 release", nil},
		// The cap counts runes, so a multi-byte name is not penalized for its
		// encoding: 20 emoji are 20 characters to the operator.
		{"counts runes not bytes", strings.Repeat("é", MaxSessionDisplayNameRunes), strings.Repeat("é", MaxSessionDisplayNameRunes), nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ValidateSessionDisplayName(tt.in)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ValidateSessionDisplayName(%q) err = %v, want %v", tt.in, err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("ValidateSessionDisplayName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// Every computed name is within the cap by construction, so validation never
// has to repair one.
func TestComposedNamesAlwaysPassValidation(t *testing.T) {
	for _, name := range []string{
		ComposeWorkerDisplayName("ao", "1929", "better sqlite3 upgrade CI failures"),
		ComposeWorkerDisplayName("a-very-long-project-prefix", "1929", "anything at all"),
		ComposeOrchestratorDisplayName("a-very-long-project-prefix"),
		ComposePrimeDisplayName("Agent Orchestrator"),
	} {
		if _, err := ValidateSessionDisplayName(name); err != nil {
			t.Fatalf("composed name %q failed validation: %v", name, err)
		}
	}
}

// A session name is the one string AO types into a terminal it does not fully
// control, so it must not carry anything that can start, substitute, quote,
// escape, or redirect a command. That is what makes a misdirected naming write
// unable to cause execution — a property no timing change can reopen.
func TestNameRuneAllowedRejectsShellActiveCharacters(t *testing.T) {
	// Everything that can start, substitute, expand, quote, escape, or glob a
	// command — plus invisible runes, which can hide content from the operator.
	for _, r := range ";&|$`()<>\\'\"*?[]{}~!^\n\r\t\x00\u200b\u2066" {
		if NameRuneAllowed(r) {
			t.Errorf("NameRuneAllowed(%q) = true, want false", r)
		}
	}
	// Shell grammar is entirely ASCII, so non-ASCII text is ordinary to a shell
	// and must survive: mangling real titles buys no safety.
	for _, r := range "abcXYZ019 #-_.,:+/@=%éü漢’–—🚀" {
		if !NameRuneAllowed(r) {
			t.Errorf("NameRuneAllowed(%q) = false, want true", r)
		}
	}
}

// Issue titles are arbitrary user-authored text and they flow into names, so a
// title carrying shell syntax must not produce a name that carries it too.
func TestComposedNamesNeverCarryShellSyntax(t *testing.T) {
	got := ComposeWorkerDisplayName("ao", "7", "fix; rm -rf $HOME && echo `id`")
	for _, bad := range []string{";", "$", "&", "`"} {
		if strings.Contains(got, bad) {
			t.Fatalf("ComposeWorkerDisplayName = %q, still carries %q", got, bad)
		}
	}
	if got == "" {
		t.Fatal("ComposeWorkerDisplayName = empty; a hostile title must degrade, not erase the name")
	}
}

// A work item id arrives from a CLI flag, an HTTP body, or tracker intake, so it
// is no more trusted than the title beside it.
func TestComposeWorkerDisplayNameSanitizesTheWorkItemID(t *testing.T) {
	got := ComposeWorkerDisplayName("ao", "7;touch", "")
	if strings.ContainsAny(got, ";`$&|") {
		t.Fatalf("ComposeWorkerDisplayName = %q, still carries shell syntax from the work item id", got)
	}
	if _, err := ValidateSessionDisplayName(got); err != nil {
		t.Fatalf("composed name %q failed validation: %v", got, err)
	}
}

// Real-world titles must survive the restriction, or it costs more than it buys.
func TestComposedNamesKeepRealisticTitleText(t *testing.T) {
	if got := ComposeWorkerDisplayName("ao", "7", "Don’t regress"); got != "ao #7 Don’t regress" {
		t.Fatalf("ComposeWorkerDisplayName = %q, want the curly apostrophe preserved", got)
	}
	if got := ComposeWorkerDisplayName("ao", "7", "100% rollout"); got != "ao #7 100% rollout" {
		t.Fatalf("ComposeWorkerDisplayName = %q, want the percent sign preserved", got)
	}
}
