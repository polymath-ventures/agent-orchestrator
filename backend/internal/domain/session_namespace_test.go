package domain

import (
	"strings"
	"testing"
)

func TestComposeSessionNamespaceKey(t *testing.T) {
	t.Parallel()

	const id = SessionID("agent-orchestrator-14-0123456789abcdef")
	tests := []struct {
		name        string
		displayName string
		wantLabel   string
	}{
		{name: "readable work label", displayName: "ao #255 Readable work", wantLabel: "ao-255-readable-work"},
		{name: "unsafe runs collapse", displayName: " Fix / path \\ and...spaces ", wantLabel: "fix-path-and-spaces"},
		{name: "unicode keeps ascii work context", displayName: "ao #255 Café 🚀", wantLabel: "ao-255-caf"},
		{name: "fallback", displayName: "🚀🚀", wantLabel: "work"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ComposeSessionNamespaceKey(tt.displayName, id)
			if err != nil {
				t.Fatalf("ComposeSessionNamespaceKey: %v", err)
			}
			want := tt.wantLabel + "--" + string(id)
			if got != want {
				t.Fatalf("ComposeSessionNamespaceKey(%q, %q) = %q, want %q", tt.displayName, id, got, want)
			}
		})
	}
}

func TestComposeSessionNamespaceKeyDoesNotTruncateTheReadableLabel(t *testing.T) {
	t.Parallel()

	const id = SessionID("agent-orchestrator-14-fedcba9876543210")
	displayName := strings.Repeat("readable-", 20) + "tail"
	key, err := ComposeSessionNamespaceKey(displayName, id)
	if err != nil {
		t.Fatal(err)
	}
	label, suffix, ok := strings.Cut(key, "--")
	if !ok {
		t.Fatalf("namespace key %q has no separator", key)
	}
	wantLabel := strings.TrimSuffix(displayName, "-")
	if label != wantLabel {
		t.Fatalf("label = %q, want the complete normalized label %q", label, wantLabel)
	}
	if suffix != string(id) {
		t.Fatalf("identity suffix = %q, want complete id %q", suffix, id)
	}
}

func TestComposeSessionNamespaceKeyCannotLoseGenerationIdentity(t *testing.T) {
	t.Parallel()

	idA := SessionID("agent-orchestrator-1-0123456789abcdef")
	idB := SessionID("agent-orchestrator-1-fedcba9876543210")
	keyA, err := ComposeSessionNamespaceKey("ao #255 readable", idA)
	if err != nil {
		t.Fatal(err)
	}
	keyB, err := ComposeSessionNamespaceKey("ao #255 readable", idB)
	if err != nil {
		t.Fatal(err)
	}
	if keyA == keyB {
		t.Fatalf("different generation-qualified ids produced the same key %q", keyA)
	}
	if !strings.HasSuffix(keyA, "--"+string(idA)) || !strings.HasSuffix(keyB, "--"+string(idB)) {
		t.Fatalf("keys do not retain complete ids: %q, %q", keyA, keyB)
	}
}

func TestComposeSessionNamespaceKeyRejectsEmptyIdentity(t *testing.T) {
	t.Parallel()

	if _, err := ComposeSessionNamespaceKey("readable", ""); err == nil {
		t.Fatal("ComposeSessionNamespaceKey with empty id succeeded")
	}
}
