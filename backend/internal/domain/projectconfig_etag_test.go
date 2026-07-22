package domain_test

import (
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestProjectConfigETagChangesWithContent(t *testing.T) {
	t.Parallel()

	base := domain.ProjectConfig{DefaultBranch: "main"}
	changed := base
	changed.DefaultBranch = "trunk"

	if base.ETag() == changed.ETag() {
		t.Fatal("a content change must change the ETag, or a stale write cannot be detected")
	}
}

func TestProjectConfigETagIsStableAcrossEqualConfigs(t *testing.T) {
	t.Parallel()

	a := domain.ProjectConfig{Env: map[string]string{"A": "1", "B": "2", "C": "3", "D": "4"}}
	b := domain.ProjectConfig{Env: map[string]string{"D": "4", "C": "3", "B": "2", "A": "1"}}
	if a.ETag() != b.ETag() {
		t.Fatalf("equal configs must produce equal ETags: %q vs %q", a.ETag(), b.ETag())
	}

	first := a.ETag()
	for range 50 {
		if got := a.ETag(); got != first {
			t.Fatalf("ETag is not deterministic across map iterations: %q vs %q", got, first)
		}
	}
}

func TestEmptyProjectConfigHasConcreteETag(t *testing.T) {
	t.Parallel()

	var empty domain.ProjectConfig
	if empty.ETag() != domain.EmptyConfigETag {
		t.Fatalf("empty config ETag = %q, want %q", empty.ETag(), domain.EmptyConfigETag)
	}
	if empty.ETag() == "" {
		t.Fatal("the empty-config ETag must not itself be empty")
	}
}

func TestProjectConfigETagMatchesHTTPForms(t *testing.T) {
	t.Parallel()

	cfg := domain.ProjectConfig{DefaultBranch: "main"}
	token := cfg.ETag()

	for _, candidate := range []string{
		token,
		`"` + token + `"`,
		`W/"` + token + `"`,
		`"some-older-token", "` + token + `"`,
		"*",
	} {
		if !cfg.ETagMatches(candidate) {
			t.Errorf("ETagMatches(%q) = false, want true", candidate)
		}
	}
	for _, candidate := range []string{"", "some-older-token", `"some-older-token"`} {
		if cfg.ETagMatches(candidate) {
			t.Errorf("ETagMatches(%q) = true, want false", candidate)
		}
	}
}
