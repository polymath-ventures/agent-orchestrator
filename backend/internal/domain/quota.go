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
