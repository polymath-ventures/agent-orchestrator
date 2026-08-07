package domain

import "testing"

func TestIsPathSafeSessionID(t *testing.T) {
	cases := []struct {
		name string
		id   string
		want bool
	}{
		// A legacy dotted-project session id is a real, addressable identity.
		{"dotted legacy project worker", "goodadbadad.net-5-9ed4c657e7777c8c", true},
		{"dotted legacy project orchestrator", "goodadbadad.net-1-9ed4c657e7777c8c", true},
		{"ordinary worker", "coachclaw-8-9ed4c657e7777c8c", true},
		{"prime", "prime-2-9ed4c657e7777c8c", true},
		// Path-unsafe ids must still be refused.
		{"empty", "", false},
		{"single dot", ".", false},
		{"parent traversal", "..", false},
		{"embedded traversal", "a..b-1-gen", false},
		{"leading dot", ".net-1-gen", false},
		{"forward slash", "a/b-1-gen", false},
		{"backslash", `a\b-1-gen`, false},
		{"space", "a b-1-gen", false},
		{"trailing dot", "abc.", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsPathSafeSessionID(tc.id); got != tc.want {
				t.Fatalf("IsPathSafeSessionID(%q) = %v, want %v", tc.id, got, tc.want)
			}
		})
	}
}
