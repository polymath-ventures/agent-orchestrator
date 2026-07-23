package metrics

import (
	"fmt"
	"sort"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// AlertKind identifies a metrics condition the observer tracks.
type AlertKind string

const (
	// AlertQuotaLow fires when an exact/estimated quota snapshot reports
	// remaining usage at or below the configured percentage.
	AlertQuotaLow AlertKind = "quota_low"
)

// Severity is the alert severity level.
type Severity string

const (
	// SeverityWarn is the only level the observer emits today; it is explicit so
	// the wire shape can carry richer levels later without a breaking change.
	SeverityWarn Severity = "warn"
)

// Alert is one firing metrics condition at snapshot time.
type Alert struct {
	// Kind is the condition that tripped.
	Kind AlertKind `json:"kind"`
	// Severity is the alert level.
	Severity Severity `json:"severity"`
	// Message is a human-readable one-line summary.
	Message string `json:"message"`
	// Subject is the stable alert subject, for example a harness/account/model
	// quota window.
	Subject string `json:"subject,omitempty"`
	// Value is the measured value that tripped the threshold.
	Value float64 `json:"value"`
	// Threshold is the configured limit the value crossed.
	Threshold float64 `json:"threshold"`
}

// AlertTransition is an alert crossing a state boundary: firing when it was
// clear, or clearing when it was firing. The observer emits one notification
// per transition, never one per tick, so a sustained condition is not a
// notification storm.
type AlertTransition struct {
	// Alert is the alert at the moment of transition. For a clear transition the
	// Value reflects the reading that ended the condition.
	Alert Alert
	// Firing is true for an enter (clear→firing) transition and false for an
	// exit (firing→clear) transition.
	Firing bool
}

// Thresholds holds the alerting limits. A zero field disables that specific
// alert, matching the "0 disables" convention used across daemon config.
type Thresholds struct {
	// LowQuotaPercent fires quota_low when remaining/limit is at or below this
	// percent for exact or estimated quota snapshots. Zero disables it.
	LowQuotaPercent float64
}

// evaluator is the stateful threshold machine. It converts a stream of
// snapshots into a stable set of firing alerts plus transitions between ticks.
type evaluator struct {
	th     Thresholds
	firing map[string]Alert
}

func newEvaluator(th Thresholds) *evaluator {
	return &evaluator{th: th, firing: map[string]Alert{}}
}

// evaluate folds one snapshot into the machine, returning currently-firing
// alerts and transitions since the prior tick.
func (e *evaluator) evaluate(s Snapshot) ([]Alert, []AlertTransition) {
	next := map[string]Alert{}
	if e.th.LowQuotaPercent > 0 {
		for _, alert := range lowQuotaAlerts(s.Quotas, e.th.LowQuotaPercent, s.CollectedAt) {
			next[alertKey(alert)] = alert
		}
	}

	transitions := e.diff(next)
	e.firing = next
	return sortedAlerts(next), transitions
}

func lowQuotaAlerts(quotas []domain.QuotaSnapshot, threshold float64, now time.Time) []Alert {
	var out []Alert
	for _, q := range quotas {
		if q.SignalQuality == domain.QuotaSignalNone || q.Limit == nil || *q.Limit <= 0 {
			continue
		}
		// An already-ended window is stale: the quota it measured has reset, so
		// its used% must not keep firing a low-quota alert. Skip a window whose
		// end is set and not in the future relative to the tick time. A zero
		// WindowEnd (unknown end) is not skipped.
		if !q.WindowEnd.IsZero() && !q.WindowEnd.After(now) {
			continue
		}
		pct, ok := remainingPercent(q)
		if !ok {
			continue
		}
		if pct > threshold {
			continue
		}
		account := q.AccountID
		if account == "" {
			account = "unknown account"
		}
		model := q.Model
		if model == "" {
			model = "default model"
		}
		windowName := q.WindowName
		if windowName == "" {
			windowName = "default"
		}
		subject := fmt.Sprintf("%s/%s/%s/%s/%s", q.Harness, account, model, windowName, quotaWindowKey(q.WindowStart, q.WindowEnd))
		out = append(out, Alert{
			Kind: AlertQuotaLow, Severity: SeverityWarn, Value: pct, Threshold: threshold,
			Subject: subject,
			Message: fmt.Sprintf("%s quota for %s %s %s is %.1f%% remaining; adjust the worker mix", q.Harness, account, model, windowName, pct),
		})
	}
	sort.Slice(out, func(i, j int) bool { return alertKey(out[i]) < alertKey(out[j]) })
	return out
}

func remainingPercent(q domain.QuotaSnapshot) (float64, bool) {
	if q.Remaining != nil {
		return 100 * *q.Remaining / *q.Limit, true
	}
	if q.Used != nil {
		return 100 - (100 * *q.Used / *q.Limit), true
	}
	return 0, false
}

func quotaWindowKey(start, end time.Time) string {
	if start.IsZero() && end.IsZero() {
		return "window:unknown"
	}
	return fmt.Sprintf("window:%s-%s", start.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339))
}

// diff computes enter/exit transitions between the previous firing set and next.
func (e *evaluator) diff(next map[string]Alert) []AlertTransition {
	var out []AlertTransition
	for key, a := range next {
		if _, was := e.firing[key]; !was {
			out = append(out, AlertTransition{Alert: a, Firing: true})
		}
	}
	for key, a := range e.firing {
		if _, still := next[key]; !still {
			cleared := a
			out = append(out, AlertTransition{Alert: cleared, Firing: false})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Alert.Kind != out[j].Alert.Kind {
			return out[i].Alert.Kind < out[j].Alert.Kind
		}
		return out[i].Firing && !out[j].Firing
	})
	return out
}

func sortedAlerts(m map[string]Alert) []Alert {
	out := make([]Alert, 0, len(m))
	for _, a := range m {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return alertKey(out[i]) < alertKey(out[j]) })
	return out
}

func alertKey(a Alert) string {
	return string(a.Kind) + ":" + a.Subject
}
