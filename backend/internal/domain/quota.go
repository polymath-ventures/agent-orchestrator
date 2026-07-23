package domain

import (
	"encoding/json"
	"time"
)

// QuotaSignalQuality states how trustworthy a quota snapshot is.
type QuotaSignalQuality string

const (
	// QuotaSignalExact means the harness reported authoritative quota values.
	QuotaSignalExact QuotaSignalQuality = "exact"
	// QuotaSignalEstimated means the harness reported best-effort quota values.
	QuotaSignalEstimated QuotaSignalQuality = "estimated"
	// QuotaSignalNone means the harness exposed no machine-readable quota values.
	QuotaSignalNone QuotaSignalQuality = "none"
)

// Valid reports whether q is a supported quota signal quality.
func (q QuotaSignalQuality) Valid() bool {
	switch q {
	case QuotaSignalExact, QuotaSignalEstimated, QuotaSignalNone:
		return true
	default:
		return false
	}
}

// QuotaSnapshot is the latest known subscription quota window for one
// harness/account. Nil numeric fields mean the harness exposed no such value.
type QuotaSnapshot struct {
	Harness       AgentHarness       `json:"harness"`
	AccountID     string             `json:"accountId"`
	Model         string             `json:"model,omitempty"`
	WindowName    string             `json:"windowName,omitempty"`
	WindowStart   time.Time          `json:"windowStart,omitempty"`
	WindowEnd     time.Time          `json:"windowEnd,omitempty"`
	Used          *float64           `json:"used,omitempty"`
	Remaining     *float64           `json:"remaining,omitempty"`
	Limit         *float64           `json:"limit,omitempty"`
	SignalQuality QuotaSignalQuality `json:"signalQuality" enum:"exact,estimated,none"`
	Source        string             `json:"source"`
	Basis         string             `json:"basis,omitempty"`
	ObservedAt    time.Time          `json:"observedAt"`
}

// MarshalJSON omits absent quota windows. time.Time is a struct, so
// `omitempty` alone would serialize the zero time as year 0001 and make
// no-signal snapshots look like they have a real window.
func (q QuotaSnapshot) MarshalJSON() ([]byte, error) {
	type quotaSnapshotJSON struct {
		Harness       AgentHarness       `json:"harness"`
		AccountID     string             `json:"accountId"`
		Model         string             `json:"model,omitempty"`
		WindowName    string             `json:"windowName,omitempty"`
		WindowStart   *time.Time         `json:"windowStart,omitempty"`
		WindowEnd     *time.Time         `json:"windowEnd,omitempty"`
		Used          *float64           `json:"used,omitempty"`
		Remaining     *float64           `json:"remaining,omitempty"`
		Limit         *float64           `json:"limit,omitempty"`
		SignalQuality QuotaSignalQuality `json:"signalQuality" enum:"exact,estimated,none"`
		Source        string             `json:"source"`
		Basis         string             `json:"basis,omitempty"`
		ObservedAt    time.Time          `json:"observedAt"`
	}
	return json.Marshal(quotaSnapshotJSON{
		Harness:       q.Harness,
		AccountID:     q.AccountID,
		Model:         q.Model,
		WindowName:    q.WindowName,
		WindowStart:   quotaWindowTime(q.WindowStart),
		WindowEnd:     quotaWindowTime(q.WindowEnd),
		Used:          q.Used,
		Remaining:     q.Remaining,
		Limit:         q.Limit,
		SignalQuality: q.SignalQuality,
		Source:        q.Source,
		Basis:         q.Basis,
		ObservedAt:    q.ObservedAt,
	})
}

func quotaWindowTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	utc := t.UTC()
	return &utc
}

// QuotaProbeState is the honest, user-facing outcome of a harness quota probe.
// It replaces tooltip-only error messaging: every state renders inline in the
// widget. Adapters return ok/failed/no_source; not_probed is the daemon's state
// for a harness it has not yet probed this run.
type QuotaProbeState string

const (
	// QuotaProbeNotProbed means the daemon has not yet probed this harness.
	QuotaProbeNotProbed QuotaProbeState = "not_probed"
	// QuotaProbeOK means the probe succeeded. Snapshots may be present (real
	// usage) or empty (source works but no usage recorded in the window yet).
	QuotaProbeOK QuotaProbeState = "ok"
	// QuotaProbeFailed means the probe was attempted but errored, timed out, or
	// returned output that could not be parsed. Reason carries a short excerpt.
	QuotaProbeFailed QuotaProbeState = "failed"
	// QuotaProbeNoSource means it is established, with recorded evidence, that the
	// harness exposes no machine-readable quota source. Reason carries the basis.
	QuotaProbeNoSource QuotaProbeState = "no_source"
)

// Valid reports whether s is a supported quota probe state.
func (s QuotaProbeState) Valid() bool {
	switch s {
	case QuotaProbeNotProbed, QuotaProbeOK, QuotaProbeFailed, QuotaProbeNoSource:
		return true
	default:
		return false
	}
}

// HarnessQuotaStatus is the current probe status for one harness, surfaced
// alongside quota snapshots so the widget can render an honest inline state
// without a tooltip. It is rebuilt in memory each daemon run by the QuotaProber;
// it is not persisted (a fresh run re-probes).
type HarnessQuotaStatus struct {
	Harness  AgentHarness    `json:"harness"`
	State    QuotaProbeState `json:"state" enum:"not_probed,ok,failed,no_source"`
	Reason   string          `json:"reason,omitempty"`
	ProbedAt time.Time       `json:"probedAt,omitempty"`
	// HasData reports whether the latest successful probe produced any usage
	// windows. It lets the widget distinguish ok-with-data from ok-but-empty
	// ("no usage recorded yet") without inspecting the snapshot list.
	HasData bool `json:"hasData"`
	// Snapshots carries this harness's usage windows from the latest successful
	// probe, so the widget renders directly from the status without waiting on
	// the separate metrics tick that refreshes persisted snapshots for alerting.
	Snapshots []QuotaSnapshot `json:"snapshots,omitempty"`
}

// MarshalJSON omits a zero ProbedAt. A not_probed status has never been probed,
// so its zero time must not serialize as year 0001 despite `omitempty` (time.Time
// is a struct, which `omitempty` cannot detect as empty). It mirrors
// QuotaSnapshot.MarshalJSON's pointer trick and also omits an empty Snapshots.
func (s HarnessQuotaStatus) MarshalJSON() ([]byte, error) {
	type harnessQuotaStatusJSON struct {
		Harness   AgentHarness    `json:"harness"`
		State     QuotaProbeState `json:"state" enum:"not_probed,ok,failed,no_source"`
		Reason    string          `json:"reason,omitempty"`
		ProbedAt  *time.Time      `json:"probedAt,omitempty"`
		HasData   bool            `json:"hasData"`
		Snapshots []QuotaSnapshot `json:"snapshots,omitempty"`
	}
	return json.Marshal(harnessQuotaStatusJSON{
		Harness:   s.Harness,
		State:     s.State,
		Reason:    s.Reason,
		ProbedAt:  quotaWindowTime(s.ProbedAt),
		HasData:   s.HasData,
		Snapshots: s.Snapshots,
	})
}
