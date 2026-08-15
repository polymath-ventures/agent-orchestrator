// Package trackerintake implements the opt-in issue-intake observer. It polls a
// project's configured tracker for eligible issues and starts one worker session
// per issue, leaving PR/lifecycle handling to the existing observers.
package trackerintake

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/observe"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	sessionmanager "github.com/aoagents/agent-orchestrator/backend/internal/session_manager"
)

const (
	// DefaultTickInterval is intentionally slower than runtime liveness checks:
	// intake is a backlog sweep, not an interactive status surface.
	DefaultTickInterval = time.Minute
	// DefaultFailureBackoff suppresses repeated polls for a project after an
	// intake failure. The observer retries automatically after this window.
	DefaultFailureBackoff = 5 * time.Minute
	// maxIntakePromptLen mirrors the session HTTP prompt limit. Intake uses the
	// session service directly, so it must enforce the same boundary itself.
	maxIntakePromptLen = 4096

	intakePromptTruncationNotice = "\n\n[Issue content truncated to fit the session prompt limit. Open the linked issue for the full details.]\n"
	intakePromptFooter           = "\nImplement the requested change in this repository, run the relevant checks, and open or update a pull request when ready."
)

// Store is the durable read surface the observer needs.
type Store interface {
	ListProjects(ctx context.Context) ([]domain.ProjectRecord, error)
	ListAllSessions(ctx context.Context) ([]domain.SessionRecord, error)
	// GetFleetPaused reports the daemon-global fleet pause flag. When set, the
	// whole intake tick is skipped.
	GetFleetPaused(ctx context.Context) (bool, error)
}

// Spawner is the session creation surface used by intake.
type Spawner interface {
	Spawn(ctx context.Context, cfg ports.SpawnConfig) (domain.Session, int, int, error)
}

// TrackerResolver picks the tracker adapter for a project's configured
// provider.
type TrackerResolver interface {
	Resolve(provider domain.TrackerProvider) (ports.Tracker, error)
}

// SingleTrackerResolver returns the same tracker for one specific provider and
// refuses every other provider. It exists so single-provider deployments don't
// need to construct a map.
type SingleTrackerResolver struct {
	Provider domain.TrackerProvider
	Adapter  ports.Tracker
}

// Resolve returns the wrapped adapter when the requested provider matches, or
// when the resolver was constructed without a provider pin.
func (s SingleTrackerResolver) Resolve(provider domain.TrackerProvider) (ports.Tracker, error) {
	if s.Adapter == nil {
		return nil, fmt.Errorf("tracker intake: no adapter for provider %q", provider)
	}
	if s.Provider == "" || provider == "" || provider == s.Provider {
		return s.Adapter, nil
	}
	return nil, fmt.Errorf("tracker intake: no adapter for provider %q", provider)
}

// Config holds optional observer knobs. Zero values use production defaults.
type Config struct {
	Tick           time.Duration
	FailureBackoff time.Duration
	Clock          func() time.Time
	Logger         *slog.Logger
	// ProjectDefaults are daemon-wide typed defaults. Project configuration
	// takes precedence when resolving a worker task prompt.
	ProjectDefaults domain.ProjectConfig
}

// Observer polls configured projects and starts sessions for eligible issues.
type Observer struct {
	resolver        TrackerResolver
	store           Store
	spawner         Spawner
	tick            time.Duration
	failureBackoff  time.Duration
	clock           func() time.Time
	logger          *slog.Logger
	backoffUntil    map[string]time.Time
	projectDefaults domain.ProjectConfig
}

// New constructs an Observer with safe defaults.
func New(resolver TrackerResolver, store Store, spawner Spawner, cfg Config) *Observer {
	o := &Observer{resolver: resolver, store: store, spawner: spawner, tick: cfg.Tick, failureBackoff: cfg.FailureBackoff, clock: cfg.Clock, logger: cfg.Logger, backoffUntil: map[string]time.Time{}, projectDefaults: cfg.ProjectDefaults}
	if o.tick <= 0 {
		o.tick = DefaultTickInterval
	}
	if o.failureBackoff <= 0 {
		o.failureBackoff = DefaultFailureBackoff
	}
	if o.clock == nil {
		o.clock = time.Now
	}
	if o.logger == nil {
		o.logger = slog.Default()
	}
	return o
}

// Start launches the observer loop. The first poll runs immediately inside the
// goroutine, keeping daemon startup non-blocking.
func (o *Observer) Start(ctx context.Context) <-chan struct{} {
	return observe.StartPollLoop(ctx, o.tick, o.Poll, o.logger, "tracker intake")
}

// Poll runs one synchronous intake pass. Store discovery failures are returned
// because they prevent the pass from knowing the current world; provider and
// spawn failures are logged and skipped so one bad issue/project does not block
// the rest of the daemon.
func (o *Observer) Poll(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if o.resolver == nil || o.store == nil || o.spawner == nil {
		return nil
	}
	now := o.clock().UTC()
	// A fleet pause gates every project: skip the whole tick before touching any
	// tracker. This is an authoritative safety gate, so a read error FAILS CLOSED
	// — abort the tick rather than dispatch work while the fleet may be paused.
	if paused, err := o.store.GetFleetPaused(ctx); err != nil {
		return err
	} else if paused {
		o.logger.Debug("tracker intake: fleet paused, skipping tick")
		return nil
	}
	projects, err := o.store.ListProjects(ctx)
	if err != nil {
		return err
	}
	enabledProjects := make([]domain.ProjectRecord, 0, len(projects))
	for _, project := range projects {
		// A paused project keeps its intake config but dispatches nothing.
		if project.Config.TrackerIntake.Enabled && !project.Paused {
			enabledProjects = append(enabledProjects, project)
		}
	}
	if len(enabledProjects) == 0 {
		return nil
	}
	sessions, err := o.store.ListAllSessions(ctx)
	if err != nil {
		return err
	}
	seen := seenIssueIDs(sessions, projects)
	for _, project := range enabledProjects {
		if err := ctx.Err(); err != nil {
			return err
		}
		if until, ok := o.backoffUntil[project.ID]; ok && now.Before(until) {
			o.logger.Debug("tracker intake: project in failure backoff", "project", project.ID, "until", until)
			continue
		}
		if failed := o.pollProject(ctx, project, seen); failed {
			o.backoffUntil[project.ID] = now.Add(o.failureBackoff)
		} else {
			delete(o.backoffUntil, project.ID)
		}
	}
	return nil
}

// pollProject returns failed=true for conditions that should be retried after a
// backoff window rather than logged on every poll.
func (o *Observer) pollProject(ctx context.Context, project domain.ProjectRecord, seen map[string]bool) (failed bool) {
	cfg := project.Config.TrackerIntake.WithDefaults()
	if !cfg.Enabled {
		return false
	}
	if err := cfg.Validate(); err != nil {
		o.logger.Warn("tracker intake: skipping project with invalid config", "project", project.ID, "err", err)
		return true
	}
	repo, ok := trackerRepo(project, cfg)
	if !ok {
		o.logger.Warn("tracker intake: skipping project without tracker scope", "project", project.ID, "provider", cfg.Provider, "origin", project.RepoOriginURL)
		return true
	}
	tracker, err := o.resolver.Resolve(cfg.Provider)
	if err != nil {
		o.logger.Warn("tracker intake: no adapter for provider", "project", project.ID, "provider", cfg.Provider, "err", err)
		return true
	}
	issues, err := tracker.List(ctx, repo, domain.ListFilter{
		State:    domain.ListOpen,
		Assignee: cfg.Assignee,
	})
	if err != nil {
		o.logger.Error("tracker intake: list issues failed", "project", project.ID, "repo", repo.Native, "err", err)
		return true
	}
	// Intake keeps its richer BuildIssuePrompt legacy fallback, so it resolves
	// configured templates here and passes the exact rendered prompt to Spawn.
	// The manager uses the same resolver/renderer for promptless manual spawns.
	taskTemplate, taskSource := domain.ResolveWorkerTaskPrompt(project.Config, o.projectDefaults)
	var spawnFailed bool
	workerPoolFull := false
	for _, issue := range issues {
		if ctx.Err() != nil {
			return true
		}
		if issue.State != domain.IssueOpen {
			continue
		}
		if !issueMatchesConfig(issue, cfg) {
			continue
		}
		issueID := domain.CanonicalIssueID(issue.ID)
		// Intake lists one repository at a time, so that repo's host is the
		// host of every issue in this pass — the adapter does not stamp it on
		// each issue.
		coverage := dedupKey(domain.TrackerID{Provider: issue.ID.Provider, Native: issue.ID.Native, Host: repo.Host})
		if issueID == "" || coverage == "" || seen[coverage] {
			continue
		}
		if workerPoolFull {
			o.logger.Debug("tracker intake: worker pool already full, deferring issue", "project", project.ID, "issue", issueID)
			continue
		}
		var prompt string
		if taskTemplate != "" {
			var renderErr error
			prompt, renderErr = sessionmanager.RenderWorkerTaskPrompt(taskTemplate, issueID, repo)
			if renderErr != nil {
				o.logger.Error("tracker intake: invalid worker task prompt configuration", "project", project.ID, "source", taskSource, "issue", issueID, "err", renderErr)
				return true
			}
		} else {
			prompt = BuildIssuePrompt(issue, repo)
		}
		if _, _, _, err := o.spawner.Spawn(ctx, ports.SpawnConfig{
			ProjectID: domain.ProjectID(project.ID),
			IssueID:   issueID,
			Kind:      domain.KindWorker,
			Prompt:    prompt,
		}); err != nil {
			// Worker-cap, pause, and worker-mix health refusals are healthy
			// capacity states, not intake faults. Leave the issue unseen so a
			// later poll retries it without putting the whole project in backoff.
			if isWorkerDeferral(err) {
				o.logger.Debug("tracker intake: deferring issue, worker capacity unavailable", "project", project.ID, "issue", issueID, "err", err)
				if isWorkerConcurrencyCap(err) {
					workerPoolFull = true
				}
				continue
			}
			o.logger.Error("tracker intake: spawn issue session failed", "project", project.ID, "issue", issueID, "err", err)
			spawnFailed = true
			continue
		}
		seen[coverage] = true
	}
	return spawnFailed
}

func isWorkerConcurrencyCap(err error) bool {
	if errors.Is(err, sessionmanager.ErrWorkerConcurrencyCap) {
		return true
	}
	var apiError *apierr.Error
	return errors.As(err, &apiError) && apiError.Code == "WORKER_CONCURRENCY_CAP"
}

func isWorkerDeferral(err error) bool {
	if errors.Is(err, sessionmanager.ErrWorkerConcurrencyCap) ||
		errors.Is(err, sessionmanager.ErrProjectPaused) ||
		errors.Is(err, sessionmanager.ErrWorkerMixExhausted) ||
		errors.Is(err, sessionmanager.ErrWorkerMixBucketDown) {
		return true
	}
	var apiError *apierr.Error
	if !errors.As(err, &apiError) {
		return false
	}
	switch apiError.Code {
	case "WORKER_CONCURRENCY_CAP", "PROJECT_PAUSED", "WORKER_MIX_EXHAUSTED", "WORKER_MIX_BUCKET_DOWN":
		return true
	default:
		return false
	}
}

func issueMatchesConfig(issue domain.Issue, cfg domain.TrackerIntakeConfig) bool {
	// The opt-out label is an operator's explicit "do not auto-work this", so
	// it overrides every eligibility rule rather than joining them.
	if cfg.OptedOut(issue.Labels) {
		return false
	}
	assignee := strings.TrimSpace(cfg.Assignee)
	switch {
	case assignee == "":
		return true
	case assignee == "*":
		return len(issue.Assignees) > 0
	case strings.EqualFold(assignee, "none"):
		return len(issue.Assignees) == 0
	default:
		return containsFold(issue.Assignees, assignee)
	}
}

func containsFold(values []string, needle string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), needle) {
			return true
		}
	}
	return false
}

// dedupKey identifies an issue for coverage purposes.
//
// The canonical id stored in sessions.issue_id carries no GitLab instance host,
// so two self-managed instances that share a project path produce the same
// stored value and would collide in this fleet-wide set — one instance's live
// session suppressing intake for a different issue on the other. The host is
// recovered from the owning project's tracker scope and folded in here, at read
// time, so coverage is host-accurate without changing what is stored.
func dedupKey(id domain.TrackerID) string {
	canonical := domain.CanonicalIssueID(id)
	if canonical == "" {
		return ""
	}
	if id.Host == "" {
		return string(canonical)
	}
	return string(canonical) + "@" + id.Host
}

// seenIssueIDs is the set of issues a live session already services, keyed the
// way the spawn loop looks them up.
//
// Sessions created before the spawn boundary canonicalised issue ids (#298), or
// by an operator typing a bare `--issue 243`, hold whatever was supplied. Those
// rows are resolved here through the same domain.ParseIssueRef the spawn path
// canonicalises with, against their own project's tracker scope, so a legacy
// row covers its issue exactly as a canonical one does. Scoping per project is
// what keeps project A's bare "12" from suppressing project B's issue 12.
//
// This is transitional: it stops covering anything once the last pre-fix
// session terminates. It is deliberately not a migration, because the rows that
// matter belong to sessions that are mid-work, and rewriting a running
// session's issue_id would detach it from the worktree state its owner is
// holding.
func seenIssueIDs(sessions []domain.SessionRecord, projects []domain.ProjectRecord) map[string]bool {
	byID := make(map[domain.ProjectID]domain.ProjectRecord, len(projects))
	for _, project := range projects {
		byID[domain.ProjectID(project.ID)] = project
	}
	seen := make(map[string]bool, len(sessions))
	for _, sess := range sessions {
		if sess.IssueID == "" || sess.IsTerminated {
			continue
		}
		if project, known := byID[sess.ProjectID]; known {
			if scope, ok := sessionTrackerScope(project, sess.IssueID); ok {
				if id, ok := domain.ParseIssueRef(string(sess.IssueID), scope); ok {
					if key := dedupKey(id); key != "" {
						seen[key] = true
						continue
					}
				}
			}
		}
		// Without its project's scope there is no way to know which instance a
		// stored id belongs to: `gitlab:group/proj#7` would read as gitlab.com
		// and suppress an unrelated project's issue while still missing its
		// own. Such a row is parked in a namespace no coverage key can equal,
		// so it suppresses nothing. It costs no coverage either: a project
		// whose scope will not resolve is one intake already skips, so its
		// issues are never dispatched in the first place.
		seen[unscopedKey(sess.IssueID)] = true
	}
	return seen
}

// sessionTrackerScope resolves the repository a session's stored issue id is
// interpreted against.
//
// A project that has not configured intake names no provider, so the shared
// resolver would default to GitHub and fail to parse a GitLab origin — parking
// a perfectly resolvable session and letting another project polling that same
// repository spawn a duplicate. The stored canonical id already names its
// provider, so it supplies the hint, exactly as the session manager does when
// it renders a prompt.
func sessionTrackerScope(project domain.ProjectRecord, issueID domain.IssueID) (domain.TrackerRepo, bool) {
	var fallback domain.TrackerProvider
	if provider, _, ok := domain.SplitCanonicalIssueID(issueID); ok {
		fallback = provider
	}
	return domain.TrackerScope(project.RepoOriginURL, project.Config.TrackerIntake.WithDefaults(), fallback)
}

// unscopedKey namespaces a session whose issue could not be resolved against a
// tracker scope, so it can never collide with a dedupKey.
func unscopedKey(id domain.IssueID) string {
	return "unscoped:" + string(id)
}

// BuildIssuePrompt turns normalized issue facts into the worker's initial task.
//
// scope is the repository intake is polling, so the issue is addressed the way
// the configured-template path addresses it: a bare number for the project's
// own repo, qualified otherwise. Rendering the canonical id here would hand the
// agent a storage key no tracker CLI accepts.
func BuildIssuePrompt(issue domain.Issue, scope domain.TrackerRepo) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Work on tracker issue %s.\n\n", domain.NativeIssueRef(domain.CanonicalIssueID(issue.ID), scope))
	if issue.Title != "" {
		fmt.Fprintf(&b, "Title: %s\n", issue.Title)
	}
	if issue.URL != "" {
		fmt.Fprintf(&b, "URL: %s\n", issue.URL)
	}
	if len(issue.Labels) > 0 {
		fmt.Fprintf(&b, "Labels: %s\n", strings.Join(issue.Labels, ", "))
	}
	if len(issue.Assignees) > 0 {
		fmt.Fprintf(&b, "Assignees: %s\n", strings.Join(issue.Assignees, ", "))
	}
	body := strings.TrimSpace(issue.Body)
	if body != "" {
		fmt.Fprintf(&b, "\nBody:\n%s\n", body)
	}
	b.WriteString(intakePromptFooter)
	return capIntakePrompt(b.String())
}

func capIntakePrompt(prompt string) string {
	if len(prompt) <= maxIntakePromptLen {
		return prompt
	}
	prefix := strings.TrimSuffix(prompt, intakePromptFooter)
	prefixBudget := maxIntakePromptLen - len(intakePromptTruncationNotice) - len(intakePromptFooter)
	if prefixBudget <= 0 {
		return truncateUTF8(prompt, maxIntakePromptLen)
	}
	return truncateUTF8(prefix, prefixBudget) + intakePromptTruncationNotice + intakePromptFooter
}

func truncateUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	cut := 0
	for i := range s {
		if i > maxBytes {
			break
		}
		cut = i
	}
	return s[:cut]
}

func trackerRepo(project domain.ProjectRecord, cfg domain.TrackerIntakeConfig) (domain.TrackerRepo, bool) {
	return domain.TrackerScope(project.RepoOriginURL, cfg, "")
}
