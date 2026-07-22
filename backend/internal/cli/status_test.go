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
