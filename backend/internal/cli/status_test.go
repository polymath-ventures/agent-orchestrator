package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func TestWriteStatusShowsQuotaWindowUsedPercent(t *testing.T) {
	used, limit := 92.0, 100.0
	windowEnd := time.Date(2026, 7, 20, 19, 30, 0, 0, time.UTC)
	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)

	err := writeStatus(cmd, daemonStatus{
		State:   stateReady,
		RunFile: "/tmp/ao.json",
		DataDir: "/tmp/ao",
		Quotas: []statusQuota{{
			Harness:       "codex",
			AccountID:     "unknown",
			WindowName:    "primary",
			Used:          &used,
			Limit:         &limit,
			SignalQuality: "exact",
			WindowEnd:     &windowEnd,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	body := out.String()
	for _, want := range []string{"codex/unknown/primary", "exact 92.0% used", "until 2026-07-20T19:30:00Z"} {
		if !strings.Contains(body, want) {
			t.Fatalf("status output missing %q:\n%s", want, body)
		}
	}
}

func TestWriteStatusSkipsQuotaPercentWhenLimitIsZero(t *testing.T) {
	used, remaining, limit := 92.0, 8.0, 0.0
	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)

	err := writeStatus(cmd, daemonStatus{
		State:   stateReady,
		RunFile: "/tmp/ao.json",
		DataDir: "/tmp/ao",
		Quotas: []statusQuota{{
			Harness:       "codex",
			AccountID:     "unknown",
			WindowName:    "primary",
			Used:          &used,
			Remaining:     &remaining,
			Limit:         &limit,
			SignalQuality: "exact",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	body := out.String()
	if strings.Contains(body, "% used") || strings.Contains(body, "/0.0") {
		t.Fatalf("status output used zero limit in a ratio:\n%s", body)
	}
	if !strings.Contains(body, "exact 8.0 remaining") {
		t.Fatalf("status output missing remaining fallback:\n%s", body)
	}
}

// The revision is what an operator on the host reads to confirm which source
// the running daemon came from, so it has to reach the human-readable output,
// not just --json.
func TestWriteStatusShowsTheDaemonBuildRevision(t *testing.T) {
	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)

	st := daemonStatus{
		State:         stateReady,
		RunFile:       "/run.json",
		DataDir:       "/data",
		BuildRevision: "5ea0e09be000c3b019a9764002f4b0d6b3d799e6",
	}
	if err := writeStatus(cmd, st); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.Contains(got, "build: 5ea0e09be000c3b019a9764002f4b0d6b3d799e6") {
		t.Errorf("status output does not report the build revision:\n%s", got)
	}
}

// A daemon too old to report a revision must not render a dangling empty line —
// absent and unknown are different facts, and only the daemon gets to say
// "unknown".
func TestWriteStatusOmitsAnAbsentBuildRevision(t *testing.T) {
	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)

	if err := writeStatus(cmd, daemonStatus{State: stateReady, RunFile: "/run.json", DataDir: "/data"}); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); strings.Contains(got, "build:") {
		t.Errorf("status output invented a build line for a daemon that reported none:\n%s", got)
	}
}
