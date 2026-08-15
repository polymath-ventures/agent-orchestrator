package session

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

const issueContextBodyLimit = 12000

// withIssueDetails fills in whatever the request did not already carry about its
// work item: the task-prompt context, and the title the daemon slugs into the
// session's display name. Both come from one tracker fetch, in the one service
// every spawn path funnels through, so the CLI, the HTTP API, and tracker intake
// cannot disagree about either. A tracker that is absent, unresolvable, or down
// costs the enrichment, never the spawn.
func (s *Service) withIssueDetails(ctx context.Context, cfg ports.SpawnConfig, project domain.ProjectRecord) ports.SpawnConfig {
	if cfg.IssueID == "" || s.tracker == nil {
		return cfg
	}
	if cfg.Kind != "" && cfg.Kind != domain.KindWorker {
		return cfg
	}
	if cfg.IssueContext != "" && cfg.IssueTitle != "" {
		return cfg
	}
	id, ok := s.trackerIDForIssue(cfg, project)
	if !ok {
		// Deliberately no origin URL: a remote is whatever `git remote get-url`
		// returned, which can carry userinfo or a token, and a warning is not
		// worth putting a credential in the log. The project id identifies the
		// remote for anyone diagnosing this.
		slog.Default().Warn("spawn: work item id is not resolvable against a tracker; session name and task prompt degrade",
			"projectID", cfg.ProjectID, "issueID", cfg.IssueID)
		return cfg
	}
	issue, err := s.tracker.Get(ctx, id)
	if err != nil {
		// Silence here is what makes a degraded name undiagnosable: the manager
		// can report that the title was missing, but only this call knows why.
		slog.Default().Warn("spawn: work item lookup failed; session name and task prompt degrade",
			"projectID", cfg.ProjectID, "issueID", cfg.IssueID, "trackerID", id.Native, "error", err)
		return cfg
	}
	if cfg.IssueTitle == "" {
		cfg.IssueTitle = strings.TrimSpace(issue.Title)
	}
	if cfg.IssueContext == "" {
		if issueContext := formatIssueContext(issue); issueContext != "" {
			cfg.IssueContext = issueContext
		}
	}
	return cfg
}

// withCanonicalIssueID rewrites the request's issue id into the canonical
// `<provider>:<native>` form before anything persists it.
//
// This is the one boundary at which an issue id enters a session record: the
// CLI, the HTTP API, and tracker intake all reach the store through
// Service.spawn. Canonicalising here rather than at each reader is what keeps
// the stored value and intake's dedup key from diverging — the divergence that
// made intake spawn an unbounded stream of duplicate workers (#298).
//
// A reference that names no issue this project's tracker could serve is left
// exactly as the caller supplied it. Rejecting it would be a new failure mode
// for ids the daemon has always accepted, and it cannot cause a duplicate
// spawn: intake only ever dispatches issues its own tracker listed, and those
// always resolve.
func (s *Service) withCanonicalIssueID(cfg ports.SpawnConfig, project domain.ProjectRecord) ports.SpawnConfig {
	if cfg.IssueID == "" {
		return cfg
	}
	id, ok := s.trackerIDForIssue(cfg, project)
	if !ok {
		return cfg
	}
	cfg.IssueID = domain.CanonicalIssueID(id)
	return cfg
}

func (s *Service) trackerIDForIssue(cfg ports.SpawnConfig, project domain.ProjectRecord) (domain.TrackerID, bool) {
	scope, _ := s.trackerScope(project, cfg.TrackerProvider)
	return domain.ParseIssueRef(string(cfg.IssueID), scope)
}

// trackerScope resolves the project's tracker repository, which bare and
// repo-qualified issue references are interpreted against.
func (s *Service) trackerScope(project domain.ProjectRecord, fallbackProvider domain.TrackerProvider) (domain.TrackerRepo, bool) {
	provider, host, repo, ok := s.repoForTracker(project, fallbackProvider)
	if !ok {
		return domain.TrackerRepo{}, false
	}
	return domain.TrackerRepo{Provider: provider, Native: repo, Host: host}, true
}

func (s *Service) repoForTracker(project domain.ProjectRecord, fallbackProvider domain.TrackerProvider) (domain.TrackerProvider, string, string, bool) {
	if s.scm != nil {
		repo, ok := s.scm.ParseRepository(project.RepoOriginURL)
		if ok && repo.Provider != "" && repo.Repo != "" {
			// SCM classified the origin (e.g. "github" or "gitlab"). Use the
			// resolved provider so the multi-tracker dispatches to the
			// correct adapter. The host is resolved from the SCM origin so
			// self-managed GitLab instances route correctly; gitlab.com and
			// GitHub always produce "" (zero value).
			return domain.TrackerProvider(repo.Provider), domain.NormalizeTrackerHost(repo.Provider, repo.Host), repo.Repo, true
		}
		// SCM could not classify the origin (ok == false), or classified it
		// with an empty repo; fall through to the URL-based heuristic below.
	}
	// SCM not available or couldn't resolve — use the tracker-provider hint
	// from the CLI flag (defaults to "github" for backward compat).
	host, owner, repo, err := repoFromURL(project.RepoOriginURL)
	if err != nil {
		return "", "", "", false
	}
	provider := fallbackProvider
	if provider == "" {
		provider = domain.TrackerProviderGitHub
	}
	return provider, domain.NormalizeTrackerHost(string(provider), host), owner + "/" + repo, true
}

func formatIssueContext(issue domain.Issue) string {
	var b strings.Builder
	writeIssueLine(&b, "Issue", issue.ID.Native)
	writeIssueLine(&b, "Title", issue.Title)
	writeIssueLine(&b, "State", string(issue.State))
	writeIssueLine(&b, "URL", issue.URL)
	if len(issue.Labels) > 0 {
		writeIssueLine(&b, "Labels", strings.Join(issue.Labels, ", "))
	}
	if len(issue.Assignees) > 0 {
		writeIssueLine(&b, "Assignees", strings.Join(issue.Assignees, ", "))
	}
	body := strings.TrimSpace(domain.SanitizeControlChars(issue.Body))
	if body != "" {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("Body:\n")
		b.WriteString(truncateIssueBody(body, issueContextBodyLimit))
	}
	return strings.TrimSpace(b.String())
}

func writeIssueLine(b *strings.Builder, label, value string) {
	value = strings.TrimSpace(domain.SanitizeControlChars(value))
	if value == "" {
		return
	}
	if b.Len() > 0 {
		b.WriteByte('\n')
	}
	fmt.Fprintf(b, "%s: %s", label, value)
}

func truncateIssueBody(body string, limit int) string {
	runes := []rune(body)
	if limit <= 0 || len(runes) <= limit {
		return body
	}
	return string(runes[:limit]) + fmt.Sprintf("\n\n[Issue body truncated to %d characters.]", limit)
}
