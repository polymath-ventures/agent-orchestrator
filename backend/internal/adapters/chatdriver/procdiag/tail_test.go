package procdiag

import (
	"strings"
	"testing"
)

func TestTailRetainsTheTail(t *testing.T) {
	t.Run("keeps short output verbatim", func(t *testing.T) {
		tail := &Tail{}
		if _, err := tail.Write([]byte("Invalid API key · Fix external API key\n")); err != nil {
			t.Fatal(err)
		}
		if got := tail.String(); got != "Invalid API key · Fix external API key\n" {
			t.Fatalf("tail = %q", got)
		}
	})

	t.Run("keeps exact-limit output without marking truncation", func(t *testing.T) {
		tail := &Tail{}
		if _, err := tail.Write([]byte(strings.Repeat("x", Limit))); err != nil {
			t.Fatal(err)
		}
		if got := tail.String(); strings.HasPrefix(got, "…") {
			t.Fatalf("tail = %.40q…, want no truncation marker", got)
		}
	})

	t.Run("keeps the last bytes of a flood and marks the truncation", func(t *testing.T) {
		tail := &Tail{}
		if _, err := tail.Write([]byte(strings.Repeat("x", Limit*2))); err != nil {
			t.Fatal(err)
		}
		if _, err := tail.Write([]byte("FATAL: cannot open shared object file")); err != nil {
			t.Fatal(err)
		}
		got := tail.String()
		if !strings.HasPrefix(got, "…") {
			t.Fatalf("tail = %.40q…, want a truncation marker", got)
		}
		if !strings.HasSuffix(got, "FATAL: cannot open shared object file") {
			t.Fatalf("tail lost the most recent output: %.60q", got[len(got)-60:])
		}
		if len(got) > Limit+len("…") {
			t.Fatalf("tail is %d bytes, want at most %d", len(got), Limit+len("…"))
		}
	})

	t.Run("redacts obvious secret assignments and shapes", func(t *testing.T) {
		tail := &Tail{}
		jwt := "eyJ" + strings.Repeat("a", 24) + "." + strings.Repeat("b", 12) + "." + strings.Repeat("c", 12)
		key := "sk-" + strings.Repeat("x", 24)
		if _, err := tail.Write([]byte("Authorization: Bearer opaque-bearer\n\"Authorization\": \"Bearer quoted-bearer\"\nauthorization: 'Bearer single-quoted-bearer'\napi_key=abc123\n\"password\": \"quoted-secret\"\n--api-key flag-secret\n" + key + "\n" + jwt + "\n")); err != nil {
			t.Fatal(err)
		}
		got := tail.String()
		leaked := []string{"opaque-bearer", "quoted-bearer", "single-quoted-bearer", "abc123", "quoted-secret", "flag-secret", key, jwt}
		for _, value := range leaked {
			if strings.Contains(got, value) {
				t.Fatalf("tail leaked %q in %q", value, got)
			}
		}

		tail = &Tail{}
		if _, err := tail.Write([]byte("error: failed to refresh token\nplease run 'ao login' again\n")); err != nil {
			t.Fatal(err)
		}
		if got = tail.String(); !strings.Contains(got, "please run") {
			t.Fatalf("tail redacted across a newline: %q", got)
		}
	})
}

func TestDiagnosticsAreAppendableAndSafeWhenAbsent(t *testing.T) {
	if got := Diagnostics("agent stderr", "  \n "); got != "" {
		t.Fatalf("whitespace-only diagnostics = %q, want empty", got)
	}
	got := Diagnostics("agent stderr", "error: unknown option '--acp'\n")
	if !strings.Contains(got, "agent stderr:") || !strings.Contains(got, "unknown option '--acp'") {
		t.Fatalf("diagnostics = %q", got)
	}
}
