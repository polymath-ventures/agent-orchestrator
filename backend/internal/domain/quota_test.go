package domain

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestQuotaSnapshotJSONOmitsZeroWindowTimes(t *testing.T) {
	raw, err := json.Marshal(QuotaSnapshot{
		Harness:       HarnessCodex,
		AccountID:     "unknown",
		SignalQuality: QuotaSignalNone,
		Source:        "test",
		ObservedAt:    time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	body := string(raw)
	if strings.Contains(body, "windowStart") || strings.Contains(body, "windowEnd") || strings.Contains(body, "0001-01-01") {
		t.Fatalf("zero quota window leaked into JSON: %s", body)
	}
}

func TestQuotaSnapshotJSONIncludesKnownWindowTimes(t *testing.T) {
	start := time.Date(2026, 7, 20, 12, 0, 0, 0, time.FixedZone("offset", -4*60*60))
	end := start.Add(5 * time.Hour)
	raw, err := json.Marshal(QuotaSnapshot{
		Harness:       HarnessCodex,
		AccountID:     "chatgpt",
		WindowStart:   start,
		WindowEnd:     end,
		SignalQuality: QuotaSignalEstimated,
		Source:        "test",
		ObservedAt:    start,
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	body := string(raw)
	if !strings.Contains(body, `"windowStart":"2026-07-20T16:00:00Z"`) || !strings.Contains(body, `"windowEnd":"2026-07-20T21:00:00Z"`) {
		t.Fatalf("known quota window missing or not normalized to UTC: %s", body)
	}
}
