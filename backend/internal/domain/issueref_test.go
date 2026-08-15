package domain

import "testing"

var githubScope = TrackerRepo{Provider: TrackerProviderGitHub, Native: "acme/demo"}

func TestParseIssueRefResolvesEveryAcceptedForm(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		scope     TrackerRepo
		wantID    TrackerID
		wantOk    bool
		wantCanon IssueID
	}{
		{
			name:      "bare number",
			raw:       "12",
			scope:     githubScope,
			wantID:    TrackerID{Provider: TrackerProviderGitHub, Native: "acme/demo#12"},
			wantOk:    true,
			wantCanon: "github:acme/demo#12",
		},
		{
			name:      "hash prefixed",
			raw:       "#12",
			scope:     githubScope,
			wantID:    TrackerID{Provider: TrackerProviderGitHub, Native: "acme/demo#12"},
			wantOk:    true,
			wantCanon: "github:acme/demo#12",
		},
		{
			name:      "surrounding whitespace",
			raw:       "  12  ",
			scope:     githubScope,
			wantID:    TrackerID{Provider: TrackerProviderGitHub, Native: "acme/demo#12"},
			wantOk:    true,
			wantCanon: "github:acme/demo#12",
		},
		{
			name:      "repo qualified",
			raw:       "other/repo#12",
			scope:     githubScope,
			wantID:    TrackerID{Provider: TrackerProviderGitHub, Native: "other/repo#12"},
			wantOk:    true,
			wantCanon: "github:other/repo#12",
		},
		{
			name:      "github issue url",
			raw:       "https://github.com/acme/demo/issues/12",
			scope:     githubScope,
			wantID:    TrackerID{Provider: TrackerProviderGitHub, Native: "acme/demo#12"},
			wantOk:    true,
			wantCanon: "github:acme/demo#12",
		},
		{
			name:      "already canonical is idempotent",
			raw:       "github:acme/demo#12",
			scope:     githubScope,
			wantID:    TrackerID{Provider: TrackerProviderGitHub, Native: "acme/demo#12"},
			wantOk:    true,
			wantCanon: "github:acme/demo#12",
		},
		{
			name:      "canonical gitlab id recovers the self-managed host from scope",
			raw:       "gitlab:group/project#7",
			scope:     TrackerRepo{Provider: TrackerProviderGitLab, Native: "group/project", Host: "gitlab.internal"},
			wantID:    TrackerID{Provider: TrackerProviderGitLab, Native: "group/project#7", Host: "gitlab.internal"},
			wantOk:    true,
			wantCanon: "gitlab:group/project#7",
		},
		{
			name:      "bare number in a gitlab scope stays gitlab",
			raw:       "7",
			scope:     TrackerRepo{Provider: TrackerProviderGitLab, Native: "group/sub/project", Host: "gitlab.internal"},
			wantID:    TrackerID{Provider: TrackerProviderGitLab, Native: "group/sub/project#7", Host: "gitlab.internal"},
			wantOk:    true,
			wantCanon: "gitlab:group/sub/project#7",
		},
		{
			name:      "repo qualified takes its provider from the scope",
			raw:       "group/sub/project#7",
			scope:     TrackerRepo{Provider: TrackerProviderGitLab, Native: "group/sub/project", Host: "gitlab.internal"},
			wantID:    TrackerID{Provider: TrackerProviderGitLab, Native: "group/sub/project#7", Host: "gitlab.internal"},
			wantOk:    true,
			wantCanon: "gitlab:group/sub/project#7",
		},
		{
			name:      "trailing .git is trimmed",
			raw:       "acme/demo.git#12",
			scope:     githubScope,
			wantID:    TrackerID{Provider: TrackerProviderGitHub, Native: "acme/demo#12"},
			wantOk:    true,
			wantCanon: "github:acme/demo#12",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParseIssueRef(tt.raw, tt.scope)
			if ok != tt.wantOk {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOk)
			}
			if got != tt.wantID {
				t.Fatalf("id = %+v, want %+v", got, tt.wantID)
			}
			if canon := CanonicalIssueID(got); canon != tt.wantCanon {
				t.Fatalf("canonical = %q, want %q", canon, tt.wantCanon)
			}
			// Canonicalising a canonical id must be a no-op, or the spawn
			// boundary would rewrite ids it has already normalised.
			again, ok := ParseIssueRef(string(tt.wantCanon), tt.scope)
			if !ok || CanonicalIssueID(again) != tt.wantCanon {
				t.Fatalf("re-parse of %q = %+v (ok=%v), want a stable canonical id", tt.wantCanon, again, ok)
			}
		})
	}
}

func TestParseIssueRefRejectsReferencesItCannotResolve(t *testing.T) {
	tests := []struct {
		name  string
		raw   string
		scope TrackerRepo
	}{
		{name: "empty", raw: "", scope: githubScope},
		{name: "whitespace only", raw: "   ", scope: githubScope},
		{name: "hash only", raw: "#", scope: githubScope},
		{name: "non numeric", raw: "ISS-1", scope: githubScope},
		{name: "zero", raw: "0", scope: githubScope},
		{name: "negative", raw: "-3", scope: githubScope},
		{name: "bare number without a scope", raw: "12", scope: TrackerRepo{}},
		{name: "github repo path needs exactly two segments", raw: "a/b/c#12", scope: githubScope},
		{name: "empty owner", raw: "/demo#12", scope: githubScope},
		{name: "missing issue number", raw: "acme/demo#", scope: githubScope},
		{name: "unknown provider prefix", raw: "jira:ACME-1", scope: githubScope},
		{name: "gitlab url without an issue number", raw: "https://gitlab.com/group/project/-/issues/", scope: githubScope},
		{name: "gitlab url without a project path", raw: "https://gitlab.com/-/issues/42", scope: githubScope},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, ok := ParseIssueRef(tt.raw, tt.scope); ok {
				t.Fatalf("ParseIssueRef(%q) = %+v, want not ok", tt.raw, got)
			}
		})
	}
}

// A GitLab issue URL names its own host, so it must win over the scope and
// carry the port through for self-managed instances.
func TestParseIssueRefGitLabIssueURLs(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		wantNative string
		wantHost   string
	}{
		{name: "gitlab.com simple repo", raw: "https://gitlab.com/group/project/-/issues/42", wantNative: "group/project#42", wantHost: ""},
		{name: "gitlab.com nested namespace", raw: "https://gitlab.com/group/subgroup/project/-/issues/42", wantNative: "group/subgroup/project#42", wantHost: ""},
		{name: "self-managed with port", raw: "https://gitlab.local:8443/group/project/-/issues/7", wantNative: "group/project#7", wantHost: "gitlab.local:8443"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParseIssueRef(tt.raw, githubScope)
			if !ok {
				t.Fatalf("ParseIssueRef(%q) not ok", tt.raw)
			}
			if got.Provider != TrackerProviderGitLab {
				t.Errorf("provider = %q, want gitlab", got.Provider)
			}
			if got.Native != tt.wantNative {
				t.Errorf("native = %q, want %q", got.Native, tt.wantNative)
			}
			if got.Host != tt.wantHost {
				t.Errorf("host = %q, want %q", got.Host, tt.wantHost)
			}
		})
	}
}

// SplitCanonicalIssueID must not mistake a URL scheme for a provider prefix,
// or every issue URL would parse as an already-canonical id.
func TestSplitCanonicalIssueIDIgnoresURLSchemes(t *testing.T) {
	for _, raw := range []string{
		"https://github.com/acme/demo/issues/12",
		"http://gitlab.com/group/project/-/issues/12",
		"acme/demo#12",
		"12",
	} {
		if _, _, ok := SplitCanonicalIssueID(IssueID(raw)); ok {
			t.Errorf("SplitCanonicalIssueID(%q) = ok, want not ok", raw)
		}
	}
	provider, native, ok := SplitCanonicalIssueID("github:acme/demo#12")
	if !ok || provider != TrackerProviderGitHub || native != "acme/demo#12" {
		t.Fatalf("SplitCanonicalIssueID = (%q, %q, %v), want (github, acme/demo#12, true)", provider, native, ok)
	}
}

func TestCanonicalIssueIDDefaultsProviderAndRejectsEmptyNative(t *testing.T) {
	if got := CanonicalIssueID(TrackerID{Native: "acme/demo#12"}); got != "github:acme/demo#12" {
		t.Errorf("CanonicalIssueID = %q, want github:acme/demo#12", got)
	}
	if got := CanonicalIssueID(TrackerID{Provider: TrackerProviderGitHub, Native: "  "}); got != "" {
		t.Errorf("CanonicalIssueID = %q, want empty", got)
	}
}

func TestTrackerIntakeOptOutLabel(t *testing.T) {
	optedOut := []string{"feature", "No-AO"}

	var unset TrackerIntakeConfig
	if unset.OptOutLabelName() != DefaultTrackerOptOutLabel {
		t.Fatalf("default opt-out label = %q, want %q", unset.OptOutLabelName(), DefaultTrackerOptOutLabel)
	}
	if !unset.OptedOut(optedOut) {
		t.Error("the default opt-out label should match case-insensitively")
	}
	if unset.OptedOut([]string{"feature"}) {
		t.Error("an issue without the opt-out label should not be excluded")
	}
	if unset.OptedOut(nil) {
		t.Error("an unlabelled issue should not be excluded")
	}

	custom := TrackerIntakeConfig{OptOutLabel: "hands-off"}
	if !custom.OptedOut([]string{"hands-off"}) {
		t.Error("the configured opt-out label should exclude the issue")
	}
	if custom.OptedOut(optedOut) {
		t.Error("a configured label replaces the default rather than adding to it")
	}

	disabled := TrackerIntakeConfig{OptOutLabel: "None"}
	if disabled.OptOutLabelName() != "" {
		t.Errorf("opt-out label = %q, want disabled", disabled.OptOutLabelName())
	}
	if disabled.OptedOut(optedOut) {
		t.Error(`optOutLabel "none" should disable the opt-out`)
	}
}
