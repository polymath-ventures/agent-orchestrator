package codex

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/harness/codexrollout"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// ProbeQuota implements the optional quota-prober capability (the interface
// assertion lives beside the others in codex.go). Both plain Codex (New) and
// Codex Fugu (NewFugu) share one CODEX_HOME — fugu is a wrapper around the same
// codex binary and stores its rollouts (and reads its fugu.json) under ~/.codex —
// so this single implementation, tagging every snapshot with the codex harness,
// makes the daemon collapse the two into one combined codex chip rather than
// emitting a duplicate fugu chip carrying identical data.
//
// ProbeQuota reports the Codex login's current usage by a passive, cwd-independent
// read of the newest rollout carrying a token_count.rate_limits event under
// CODEX_HOME. It is zero-cost (a local file read, no model turn), so the daemon
// may probe it freely.
//
// The read is best-effort and can never wedge the caller: an absent or empty
// CODEX_HOME, a fresh install with no usage yet, or a rollout whose rate-limit
// windows are all implausible are all reported as an ok result with no
// snapshots — the source works, there is just nothing trustworthy to report yet.
// That is deliberately NOT a failure, which is reserved for a real error such as
// a cancelled context.
// QuotaHarness reports the canonical harness this probe's usage belongs to.
// Both plain Codex and Codex Fugu share one CODEX_HOME and one usage pool, so
// both report domain.HarnessCodex — the daemon collapses them into a single
// combined codex status rather than duplicating identical data under a
// codex-fugu chip.
func (p *Plugin) QuotaHarness() domain.AgentHarness { return domain.HarnessCodex }

func (p *Plugin) ProbeQuota(ctx context.Context, observedAt time.Time) (ports.QuotaProbeResult, error) {
	if err := ctx.Err(); err != nil {
		return ports.QuotaProbeResult{}, err
	}

	snaps, found := codexrollout.NewestRateLimits(ctx, resolveCodexHome(), observedAt)
	if !found || len(snaps) == 0 {
		return ports.QuotaProbeResult{
			State:  domain.QuotaProbeOK,
			Reason: "no codex usage recorded yet",
		}, nil
	}
	return ports.QuotaProbeResult{State: domain.QuotaProbeOK, Snapshots: snaps}, nil
}

// resolveCodexHome mirrors the CLI extractor's resolution: the CODEX_HOME env var
// when set, otherwise ~/.codex. Fugu shares this location. It matches the
// directory the codex binary itself reads and writes, so a probe reads exactly
// what the running harness reports.
func resolveCodexHome() string {
	if home := strings.TrimSpace(os.Getenv("CODEX_HOME")); home != "" {
		return home
	}
	if userHome, err := os.UserHomeDir(); err == nil {
		return filepath.Join(userHome, ".codex")
	}
	return ".codex"
}
