package daemon

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	trackergithub "github.com/aoagents/agent-orchestrator/backend/internal/adapters/tracker/github"
)

// capturingHandler records the level and message of every emitted record so a
// test can assert the level a call logged at.
type capturingHandler struct {
	records *[]slog.Record
}

func (h capturingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h capturingHandler) Handle(_ context.Context, r slog.Record) error {
	// slog.Record shares backing storage; clone before retaining it so later
	// assertions (level, and any future attr checks) never read reused state.
	*h.records = append(*h.records, r.Clone())
	return nil
}

func (h capturingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h capturingHandler) WithGroup(string) slog.Handler      { return h }

func capturingLogger() (*slog.Logger, *[]slog.Record) {
	recs := &[]slog.Record{}
	return slog.New(capturingHandler{records: recs}), recs
}

// TestLogTrackerDisabled_NoToken_IsNotWarn pins the intended boot behavior for
// GH #39 / ao-2kj: when tracker prompt enrichment is disabled because no GitHub
// token is configured, that is an intentional deployment state and must not be
// logged at WARN. It should be recorded at INFO (or lower) so a deploy-verify
// boot-log scan sees no untracked WARN for this expected condition.
func TestLogTrackerDisabled_NoToken_IsNotWarn(t *testing.T) {
	logger, recs := capturingLogger()

	logTrackerDisabled(logger, trackergithub.ErrNoToken)

	if len(*recs) != 1 {
		t.Fatalf("expected exactly 1 log record, got %d", len(*recs))
	}
	got := (*recs)[0].Level
	if got >= slog.LevelWarn {
		t.Fatalf("no-token tracker enrichment disabled logged at %v, want < WARN (intentional disabled state)", got)
	}
	if got != slog.LevelInfo {
		t.Fatalf("no-token tracker enrichment disabled logged at %v, want INFO", got)
	}
}

// TestLogTrackerDisabled_OtherError_StaysWarn locks in that a genuinely
// unexpected tracker setup failure (not the intentional no-token state) still
// surfaces at WARN — the downgrade must be narrow to ErrNoToken only.
func TestLogTrackerDisabled_OtherError_StaysWarn(t *testing.T) {
	logger, recs := capturingLogger()

	logTrackerDisabled(logger, errors.New("github tracker: setup exploded"))

	if len(*recs) != 1 {
		t.Fatalf("expected exactly 1 log record, got %d", len(*recs))
	}
	if got := (*recs)[0].Level; got != slog.LevelWarn {
		t.Fatalf("unexpected tracker setup failure logged at %v, want WARN", got)
	}
}
