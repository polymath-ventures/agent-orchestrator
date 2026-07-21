package daemon

import (
	"errors"
	"log/slog"

	trackergithub "github.com/aoagents/agent-orchestrator/backend/internal/adapters/tracker/github"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func newGitHubTracker() (ports.Tracker, error) {
	return trackergithub.New(trackergithub.Options{Token: trackergithub.EnvTokenSource{EnvVars: []string{"AO_GITHUB_TOKEN"}}})
}

func logTrackerDisabled(logger *slog.Logger, err error) {
	if errors.Is(err, trackergithub.ErrNoToken) {
		// No token configured is an intentional deployment state (enrichment is
		// simply off), not a fault — record it at INFO so it documents the
		// choice in the boot log without surfacing as recurring WARN noise
		// (GH #39). A genuine setup failure below is unexpected and stays WARN.
		logger.Info("tracker issue prompt enrichment disabled: no GitHub token configured", "err", err)
	} else {
		logger.Warn("tracker issue prompt enrichment disabled: GitHub tracker setup failed", "err", err)
	}
}
