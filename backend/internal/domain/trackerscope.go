package domain

import (
	"net/url"
	"strings"
)

// TrackerScope resolves the repository a project's issue references are
// interpreted against: the configured trackerIntake.repo when set, otherwise
// the project's SCM origin.
//
// Both halves of issue-id canonicalisation depend on this answer — the spawn
// boundary to build the stored id, tracker intake to build the id it looks that
// storage up by — so they must resolve it identically or a canonical id still
// fails to dedup. Two resolvers that merely agreed today is how #298 happened;
// this is the one that both call.
//
// fallbackProvider supplies the provider when the project's intake config does
// not name one, so a caller that classified the origin itself (the session
// service, via its SCM port) can pass what it learned.
func TrackerScope(originURL string, cfg TrackerIntakeConfig, fallbackProvider TrackerProvider) (TrackerRepo, bool) {
	provider := cfg.Provider
	if provider == "" {
		provider = fallbackProvider
	}
	if provider == "" {
		provider = TrackerProviderGitHub
	}
	native := strings.TrimSpace(cfg.Repo)
	if native == "" {
		native, _ = parseRepoNative(originURL, provider)
	}
	if native == "" {
		return TrackerRepo{}, false
	}
	return TrackerRepo{Provider: provider, Native: native, Host: repoHostFromOrigin(originURL, provider)}, true
}

// parseRepoNative extracts the provider-native repo identifier ("owner/repo"
// or "group/subgroup/repo") from a remote URL. For GitHub it accepts
// github.com, *.github.com, and *.ghe.io hosts. For GitLab it accepts any
// host (self-managed hosts are validated by the SCM provider at fetch time).
func parseRepoNative(remote string, provider TrackerProvider) (string, bool) {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return "", false
	}
	if strings.HasPrefix(remote, "git@") {
		if _, rest, ok := strings.Cut(remote, ":"); ok {
			return cleanRepoPath(rest, provider), true
		}
		return "", false
	}
	if u, err := url.Parse(remote); err == nil && u.Host != "" {
		if provider == TrackerProviderGitHub {
			host := strings.TrimPrefix(strings.ToLower(u.Host), "www.")
			if host == "github.com" || strings.HasSuffix(host, ".github.com") || strings.HasSuffix(host, ".ghe.io") {
				return cleanRepoPath(u.Path, provider), true
			}
			return "", false
		}
		// GitLab: accept any host.
		return cleanRepoPath(u.Path, provider), true
	}
	return cleanRepoPath(remote, provider), true
}

// repoHostFromOrigin extracts the host from the SCM origin URL for the given
// provider. For GitHub the host is always "" (GitHub tracker IDs don't use
// Host). For GitLab, "gitlab.com" and "www.gitlab.com" normalize to ""
// (zero value = gitlab.com); self-managed hosts pass through unchanged.
func repoHostFromOrigin(remote string, provider TrackerProvider) string {
	if provider != TrackerProviderGitLab {
		return ""
	}
	return NormalizeTrackerHost(string(provider), hostFromRemote(remote))
}

// hostFromRemote extracts the hostname (with port) from a git remote URL,
// supporting both HTTPS and SSH scp-like forms.
func hostFromRemote(remote string) string {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return ""
	}
	if strings.HasPrefix(remote, "git@") {
		rest := strings.TrimPrefix(remote, "git@")
		if colonIdx := strings.Index(rest, ":"); colonIdx > 0 {
			return rest[:colonIdx]
		}
		return ""
	}
	if u, err := url.Parse(remote); err == nil && u.Host != "" {
		return u.Host // includes port if present
	}
	return ""
}

// cleanRepoPath reduces a remote's path to the provider's native repo
// identifier. A GitHub repo is always owner/repo, so trailing segments of a
// deep URL are dropped. A GitLab project path keeps its full namespace: the
// group is part of the project's identity, and truncating it names a different
// project (or none).
func cleanRepoPath(path string, provider TrackerProvider) string {
	path = strings.Trim(strings.TrimSpace(path), "/")
	path = strings.TrimSuffix(path, ".git")
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		return ""
	}
	if provider != TrackerProviderGitLab {
		parts = parts[len(parts)-2:]
	}
	for i, part := range parts {
		parts[i] = strings.TrimSpace(part)
		if parts[i] == "" {
			return ""
		}
	}
	return strings.Join(parts, "/")
}
