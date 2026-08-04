package domain_test

import (
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestIsValidProjectID(t *testing.T) {
	cases := []struct {
		id   string
		want bool
	}{
		// Accepted: the intersection charset [A-Za-z0-9_-] with a leading
		// alphanumeric.
		{"goodadbadad-net", true},
		{"a", true},
		{"Project123", true},
		{"a_b-9", true},

		// Rejected: a dot makes the derived session id unaddressable by tmux
		// (#256). A colon is likewise tmux target grammar.
		{"goodadbadad.net", false},
		{"a.b", false},
		{"a:b", false},

		// Rejected: cases the old compound guard covered, now subsumed by the
		// pattern alone.
		{"", false},
		{".", false},
		{"a..b", false},
		{".hidden", false}, // leading non-alphanumeric
		{"-lead", false},   // leading non-alphanumeric
		{"a/b", false},
		{`a\b`, false},
	}
	for _, tc := range cases {
		if got := domain.IsValidProjectID(tc.id); got != tc.want {
			t.Errorf("IsValidProjectID(%q) = %v, want %v", tc.id, got, tc.want)
		}
	}
}
