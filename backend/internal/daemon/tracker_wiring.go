package daemon

import (
	"errors"
	"log/slog"
	"strings"

	scmgitlab "github.com/aoagents/agent-orchestrator/backend/internal/adapters/scm/gitlab"
	trackergithub "github.com/aoagents/agent-orchestrator/backend/internal/adapters/tracker/github"
	trackergitlab "github.com/aoagents/agent-orchestrator/backend/internal/adapters/tracker/gitlab"
	trackermulti "github.com/aoagents/agent-orchestrator/backend/internal/adapters/tracker/multi"
	"github.com/aoagents/agent-orchestrator/backend/internal/config"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func newGitHubTracker() (ports.Tracker, error) {
	return trackergithub.New(trackergithub.Options{Token: trackergithub.EnvTokenSource{EnvVars: []string{"AO_GITHUB_TOKEN"}}})
}

// newGitLabTracker constructs a host-aware GitLab tracker. AllowedHosts and
// HostTokens from GitLabConfig are passed through so the tracker can route
// self-managed GitLab issue lookups to the correct host with the correct token.
func newGitLabTracker(gitlabCfg config.GitLabConfig) (ports.Tracker, error) {
	hostTokens := make(map[string]scmgitlab.TokenSource, len(gitlabCfg.HostTokens))
	for host, token := range gitlabCfg.HostTokens {
		host = strings.ToLower(strings.TrimSpace(host))
		if host != "" {
			hostTokens[host] = scmgitlab.StaticTokenSource(token)
		}
	}
	return trackergitlab.New(trackergitlab.Options{
		Token:        trackergitlab.DefaultTokenSource(),
		AllowedHosts: gitlabCfg.AllowedHosts,
		HostTokens:   hostTokens,
	})
}

// newMultiTracker builds a multi-tracker dispatching to both GitHub and GitLab.
// Missing credentials for one tracker do not prevent the other from serving
// issue lookups. Returns nil when no tracker has usable credentials.
func newMultiTracker(gitlabCfg config.GitLabConfig, logger *slog.Logger) ports.Tracker {
	var named []trackermulti.NamedTracker

	if t, err := newGitHubTracker(); err != nil {
		logTrackerDisabled(logger, "github", err)
	} else {
		named = append(named, trackermulti.NamedTracker{Key: "github", Tracker: t})
	}

	if t, err := newGitLabTracker(gitlabCfg); err != nil {
		logTrackerDisabled(logger, "gitlab", err)
	} else {
		named = append(named, trackermulti.NamedTracker{Key: "gitlab", Tracker: t})
	}

	if len(named) == 0 {
		return nil
	}
	return trackermulti.New(named...)
}

func logTrackerDisabled(logger *slog.Logger, args ...any) {
	provider := "github"
	var err error
	switch len(args) {
	case 1:
		err, _ = args[0].(error)
	case 2:
		provider, _ = args[0].(string)
		err, _ = args[1].(error)
	}
	if err == nil {
		return
	}
	if errors.Is(err, trackergithub.ErrNoToken) || errors.Is(err, trackergitlab.ErrNoToken) {
		logger.Info("tracker disabled: no usable token", "provider", provider, "err", err)
	} else {
		logger.Warn("tracker disabled: setup failed", "provider", provider, "err", err)
	}
}
