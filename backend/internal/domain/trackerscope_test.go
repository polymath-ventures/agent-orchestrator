package domain

import "testing"

func TestTrackerScope(t *testing.T) {
	tests := []struct {
		name             string
		origin           string
		cfg              TrackerIntakeConfig
		fallbackProvider TrackerProvider
		want             TrackerRepo
		wantOk           bool
	}{
		{
			name:   "github origin",
			origin: "https://github.com/acme/demo.git",
			want:   TrackerRepo{Provider: TrackerProviderGitHub, Native: "acme/demo"},
			wantOk: true,
		},
		{
			name:   "ssh github origin",
			origin: "git@github.com:acme/demo.git",
			want:   TrackerRepo{Provider: TrackerProviderGitHub, Native: "acme/demo"},
			wantOk: true,
		},
		{
			// The whole point of trackerIntake.repo: issues live somewhere other
			// than the code. Both halves of canonicalisation must honour it.
			name:   "configured repo overrides the origin",
			origin: "https://github.com/acme/code.git",
			cfg:    TrackerIntakeConfig{Repo: "acme/tracker"},
			want:   TrackerRepo{Provider: TrackerProviderGitHub, Native: "acme/tracker"},
			wantOk: true,
		},
		{
			// Truncating a GitLab namespace names a different project, or none.
			name:   "nested gitlab group keeps its full namespace",
			origin: "https://gitlab.com/group/sub/proj.git",
			cfg:    TrackerIntakeConfig{Provider: TrackerProviderGitLab},
			want:   TrackerRepo{Provider: TrackerProviderGitLab, Native: "group/sub/proj"},
			wantOk: true,
		},
		{
			name:   "self-managed gitlab keeps its host",
			origin: "https://gitlab.internal:8443/group/proj.git",
			cfg:    TrackerIntakeConfig{Provider: TrackerProviderGitLab},
			want:   TrackerRepo{Provider: TrackerProviderGitLab, Native: "group/proj", Host: "gitlab.internal:8443"},
			wantOk: true,
		},
		{
			name:             "fallback provider applies when the config names none",
			origin:           "https://gitlab.com/group/sub/proj.git",
			fallbackProvider: TrackerProviderGitLab,
			want:             TrackerRepo{Provider: TrackerProviderGitLab, Native: "group/sub/proj"},
			wantOk:           true,
		},
		{
			name:             "config provider wins over the fallback",
			origin:           "https://github.com/acme/demo.git",
			cfg:              TrackerIntakeConfig{Provider: TrackerProviderGitHub},
			fallbackProvider: TrackerProviderGitLab,
			want:             TrackerRepo{Provider: TrackerProviderGitHub, Native: "acme/demo"},
			wantOk:           true,
		},
		{
			name:   "github provider rejects a non-github origin",
			origin: "https://gitlab.com/group/proj.git",
			wantOk: false,
		},
		{name: "no origin and no configured repo", wantOk: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := TrackerScope(tt.origin, tt.cfg, tt.fallbackProvider)
			if ok != tt.wantOk {
				t.Fatalf("ok = %v, want %v (got %+v)", ok, tt.wantOk, got)
			}
			if ok && got != tt.want {
				t.Fatalf("scope = %+v, want %+v", got, tt.want)
			}
		})
	}
}
