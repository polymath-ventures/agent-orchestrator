package domain

import (
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
