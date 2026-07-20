package domain

import "time"

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
	WindowStart   time.Time          `json:"windowStart,omitempty"`
	WindowEnd     time.Time          `json:"windowEnd,omitempty"`
	Used          *float64           `json:"used,omitempty"`
	Remaining     *float64           `json:"remaining,omitempty"`
	Limit         *float64           `json:"limit,omitempty"`
	SignalQuality QuotaSignalQuality `json:"signalQuality"`
	Source        string             `json:"source"`
	Basis         string             `json:"basis,omitempty"`
	ObservedAt    time.Time          `json:"observedAt"`
}
