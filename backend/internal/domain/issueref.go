package domain

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// CanonicalIssueID stores tracker issue ids in sessions.issue_id with the
// provider included, so future providers cannot collide on native ids.
//
// The canonical string deliberately carries no GitLab host: it is a key, and a
// self-managed instance host is recovered from the owning project's origin when
// the id is parsed back (see ParseIssueRef).
func CanonicalIssueID(id TrackerID) IssueID {
	provider := id.Provider
	if provider == "" {
		provider = TrackerProviderGitHub
	}
	native := strings.TrimSpace(id.Native)
	if native == "" {
		return ""
	}
	return IssueID(string(provider) + ":" + native)
}

// SplitCanonicalIssueID reverses CanonicalIssueID. It reports false for
// anything that is not already in canonical form: issue URLs, whose scheme
// separator would otherwise read as a provider prefix, and a known prefix
// carrying a native part no adapter could fetch ("github:12").
func SplitCanonicalIssueID(id IssueID) (TrackerProvider, string, bool) {
	prefix, native, ok := strings.Cut(strings.TrimSpace(string(id)), ":")
	if !ok {
		return "", "", false
	}
	provider := TrackerProvider(strings.ToLower(strings.TrimSpace(prefix)))
	if provider != TrackerProviderGitHub && provider != TrackerProviderGitLab {
		return "", "", false
	}
	native = strings.TrimSpace(native)
	if native == "" || strings.HasPrefix(native, "//") {
		return "", "", false
	}
	if repoQualifiedIssueNative(native, provider) != native {
		return "", "", false
	}
	return provider, native, true
}

// NativeIssueRef renders an issue id the way a person or a tracker CLI would
// write it, relative to the repository the session works in:
//
//	github:acme/code#242, scope acme/code   -> "242"
//	github:acme/other#242, scope acme/code  -> "acme/other#242"
//
// The qualifier is what keeps a cross-repo reference addressing the right
// issue. Dropping it would tell a worker to work on issue 242 of its own repo,
// which is a different ticket and a silent one — so the number alone is only
// ever used when the issue does belong to the session's own repo, which is also
// the case for everything tracker intake dispatches.
//
// Prompts address the issue; they do not key storage. Canonicalising ids at the
// spawn boundary must not put "github:acme/code#242" — a token no gh or glab
// invocation accepts — into an agent's task message. Shapes this does not
// recognise are returned unchanged.
func NativeIssueRef(id IssueID, scope TrackerRepo) string {
	ref := strings.TrimSpace(string(id))
	provider, native, ok := SplitCanonicalIssueID(IssueID(ref))
	if !ok {
		return strings.TrimPrefix(ref, "#")
	}
	repo, number, _ := strings.Cut(native, "#")
	// An unset scope provider is unknown, not a match. Reading it as a match
	// would drop the qualifier for a project whose scope could not be resolved,
	// which is how an unqualified reference ends up naming the wrong tracker's
	// issue of that number.
	sameProvider := scope.Provider != "" && scope.Provider == provider
	if scopeRepo := strings.TrimSpace(scope.Native); scopeRepo != "" &&
		sameProvider && strings.EqualFold(repo, scopeRepo) {
		return number
	}
	if !sameProvider {
		// Same repo path on a different tracker is a different issue. Without
		// the provider, "acme/code#242" on a GitHub-backed project reads as the
		// GitHub issue, which is the one thing this must not say.
		return string(provider) + ":" + native
	}
	return native
}

// ParseIssueRef resolves an operator-supplied issue reference into the
// TrackerID it names. Accepted forms, in order of precedence:
//
//	github:owner/repo#12                        already canonical
//	https://github.com/owner/repo/issues/12     provider-explicit URL
//	https://gitlab.com/group/proj/-/issues/12   provider-explicit URL
//	owner/repo#12                               repo-qualified, provider from scope
//	#12 / 12                                    bare, repo and provider from scope
//
// scope is the project's tracker repository. It is only consulted for the forms
// that do not name a repository or host themselves.
//
// This is the single definition of "which strings name this issue". The spawn
// path canonicalises through it before writing a session record, and tracker
// intake resolves already-stored ids through it, so the persisted value and the
// intake lookup key cannot drift apart.
func ParseIssueRef(raw string, scope TrackerRepo) (TrackerID, bool) {
	ref := strings.TrimSpace(raw)
	if ref == "" {
		return TrackerID{}, false
	}
	if provider, native, ok := SplitCanonicalIssueID(IssueID(ref)); ok {
		host := ""
		if provider == TrackerProviderGitLab {
			host = scope.Host
		}
		return TrackerID{Provider: provider, Native: native, Host: host}, true
	}
	ref = strings.TrimPrefix(ref, "#")
	if ref == "" {
		return TrackerID{}, false
	}
	if native, ok := gitHubIssueURLNative(ref); ok {
		return TrackerID{Provider: TrackerProviderGitHub, Native: native}, true
	}
	if native, host, ok := gitLabIssueURLNative(ref); ok {
		return TrackerID{Provider: TrackerProviderGitLab, Native: native, Host: host}, true
	}
	provider := scope.Provider
	if provider == "" {
		provider = TrackerProviderGitHub
	}
	if native := repoQualifiedIssueNative(ref, provider); native != "" {
		host := ""
		if provider == TrackerProviderGitLab {
			host = scope.Host
		}
		return TrackerID{Provider: provider, Native: native, Host: host}, true
	}
	n, err := strconv.Atoi(ref)
	if err != nil || n <= 0 {
		return TrackerID{}, false
	}
	if strings.TrimSpace(scope.Native) == "" {
		return TrackerID{}, false
	}
	return TrackerID{Provider: provider, Native: fmt.Sprintf("%s#%d", scope.Native, n), Host: scope.Host}, true
}

// NormalizeTrackerHost returns the host to set on a TrackerID/TrackerRepo. For
// GitHub the host is always "" — GitHub tracker IDs don't use Host. For GitLab,
// "gitlab.com" and "www.gitlab.com" normalize to "" (the zero value meaning
// gitlab.com) so callers don't need to special-case the default host.
// Self-managed hosts pass through unchanged.
func NormalizeTrackerHost(provider, host string) string {
	if provider != string(TrackerProviderGitLab) {
		return ""
	}
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "gitlab.com" || host == "www.gitlab.com" {
		return ""
	}
	return host
}

// repoQualifiedIssueNative parses the "<repo-path>#<number>" form. GitHub
// repo paths are exactly owner/repo; GitLab project paths may nest groups.
// It returns "" when raw does not name an issue in that shape. A caller can
// also compare the result against its input to test whether raw was already
// exactly the provider's native form.
func repoQualifiedIssueNative(raw string, provider TrackerProvider) string {
	if strings.Contains(raw, "://") {
		return ""
	}
	hash := strings.LastIndexByte(raw, '#')
	if hash <= 0 || hash == len(raw)-1 {
		return ""
	}
	n, err := strconv.Atoi(raw[hash+1:])
	if err != nil || n <= 0 {
		return ""
	}
	path, ok := cleanRepoPathSegments(raw[:hash], provider)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%s#%d", path, n)
}

// cleanRepoPathSegments trims and validates a repository path. GitHub requires
// exactly two segments; GitLab allows nested groups, so it requires at least
// two.
func cleanRepoPathSegments(raw string, provider TrackerProvider) (string, bool) {
	parts := strings.Split(strings.Trim(strings.TrimSpace(raw), "/"), "/")
	if len(parts) < 2 || (provider != TrackerProviderGitLab && len(parts) != 2) {
		return "", false
	}
	for i, part := range parts {
		parts[i] = strings.TrimSpace(part)
		if parts[i] == "" {
			return "", false
		}
	}
	parts[len(parts)-1] = strings.TrimSuffix(parts[len(parts)-1], ".git")
	if parts[len(parts)-1] == "" {
		return "", false
	}
	return strings.Join(parts, "/"), true
}

func gitHubIssueURLNative(raw string) (string, bool) {
	if !strings.Contains(raw, "://") {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Hostname(), "github.com") {
		return "", false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 4 || parts[2] != "issues" {
		return "", false
	}
	n, err := strconv.Atoi(parts[3])
	if err != nil || n <= 0 {
		return "", false
	}
	return fmt.Sprintf("%s/%s#%d", parts[0], strings.TrimSuffix(parts[1], ".git"), n), true
}

// gitLabIssueURLNative parses a GitLab issue URL into the native
// "project-path#iid" form and extracts the host. GitLab issue URLs use the
// /-/issues/ separator:
//
//   - https://gitlab.com/owner/repo/-/issues/123
//   - https://gitlab.com/group/subgroup/repo/-/issues/123
//   - https://gitlab.internal/owner/repo/-/issues/123  (self-managed)
//
// Any host is accepted because self-managed GitLab instances use arbitrary
// hostnames; the /-/issues/ path pattern is distinctive to GitLab.
//
// The returned host is the URL hostname for self-managed instances (e.g.
// "gitlab.internal") and "" for gitlab.com (the zero value, meaning the default
// host) so that callers can set TrackerID.Host without special-casing.
func gitLabIssueURLNative(raw string) (native, host string, ok bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", "", false
	}
	path := strings.Trim(u.Path, "/")
	idx := strings.Index(path, "/-/issues/")
	if idx <= 0 {
		return "", "", false
	}
	rest := path[idx+len("/-/issues/"):]
	n, err := strconv.Atoi(rest)
	if err != nil || n <= 0 {
		return "", "", false
	}
	projectPath, ok := cleanRepoPathSegments(path[:idx], TrackerProviderGitLab)
	if !ok {
		return "", "", false
	}
	// u.Host preserves the port (e.g. "gitlab.internal:8443") so that
	// self-managed hosts with non-default ports match AllowedHosts entries.
	// Hostnames are case-insensitive, and this host reaches a dedup key that is
	// compared byte for byte, so it is normalised the same way an origin-derived
	// host is.
	host = NormalizeTrackerHost(string(TrackerProviderGitLab), u.Host)
	return fmt.Sprintf("%s#%d", projectPath, n), host, true
}
