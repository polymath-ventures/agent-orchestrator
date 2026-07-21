package domain

import (
	"strings"
	"time"
)

// ModelAvailabilityStatus is the coarse cached verdict for a configured model
// pin. Unknown is advisory: it must not block spawning.
type ModelAvailabilityStatus string

const (
	// ModelAvailabilityUnknown means no definitive cached reachability verdict exists.
	ModelAvailabilityUnknown ModelAvailabilityStatus = "unknown"
	// ModelAvailabilityReachable means the latest probe reached the configured model.
	ModelAvailabilityReachable ModelAvailabilityStatus = "reachable"
	// ModelAvailabilityUnreachable means the latest probe proved the model unavailable.
	ModelAvailabilityUnreachable ModelAvailabilityStatus = "unreachable"
)

// Valid reports whether s is a supported model availability status.
func (s ModelAvailabilityStatus) Valid() bool {
	switch s {
	case ModelAvailabilityUnknown, ModelAvailabilityReachable, ModelAvailabilityUnreachable:
		return true
	default:
		return false
	}
}

// ModelAvailabilityReason explains why a model availability status has its
// current value.
type ModelAvailabilityReason string

const (
	// ModelAvailabilityReasonNotProbed means no cached probe result exists yet.
	ModelAvailabilityReasonNotProbed ModelAvailabilityReason = "not-probed"
	// ModelAvailabilityReasonNoCapability means the adapter cannot validate models.
	ModelAvailabilityReasonNoCapability ModelAvailabilityReason = "no-capability"
	// ModelAvailabilityReasonProbeUnavailable means the probe could not complete.
	ModelAvailabilityReasonProbeUnavailable ModelAvailabilityReason = "probe-unavailable"
	// ModelAvailabilityReasonReachable means the probe reached the model.
	ModelAvailabilityReasonReachable ModelAvailabilityReason = "reachable"
	// ModelAvailabilityReasonUnreachable means the probe proved the model unavailable.
	ModelAvailabilityReasonUnreachable ModelAvailabilityReason = "unreachable"
	// ModelAvailabilityReasonRecovered means an unreachable model became reachable.
	ModelAvailabilityReasonRecovered ModelAvailabilityReason = "recovered"
)

// Valid reports whether r is a supported reason code.
func (r ModelAvailabilityReason) Valid() bool {
	switch r {
	case ModelAvailabilityReasonNotProbed,
		ModelAvailabilityReasonNoCapability,
		ModelAvailabilityReasonProbeUnavailable,
		ModelAvailabilityReasonReachable,
		ModelAvailabilityReasonUnreachable,
		ModelAvailabilityReasonRecovered:
		return true
	default:
		return false
	}
}

// ModelAvailability is the API/storage read model for one configured model pin.
type ModelAvailability struct {
	ProjectID  ProjectID               `json:"projectId,omitempty"`
	Harness    AgentHarness            `json:"harness" enum:"claude-code,codex,codex-fugu,aider,opencode,grok,droid,amp,agy,crush,cursor,qwen,copilot,goose,auggie,continue,devin,cline,kimi,kiro,kilocode,vibe,pi,autohand"`
	Model      string                  `json:"model"`
	Status     ModelAvailabilityStatus `json:"status" enum:"unknown,reachable,unreachable"`
	Reason     ModelAvailabilityReason `json:"reason" enum:"not-probed,no-capability,probe-unavailable,reachable,unreachable,recovered"`
	Message    string                  `json:"message,omitempty"`
	ObservedAt time.Time               `json:"observedAt,omitempty"`
	UpdatedAt  time.Time               `json:"updatedAt,omitempty"`
}

// ModelAvailabilityKey identifies one cached model verdict.
type ModelAvailabilityKey struct {
	ProjectID ProjectID
	Harness   AgentHarness
	Model     string
}

// Key returns the normalized durable identity for this availability row.
func (m ModelAvailability) Key() ModelAvailabilityKey {
	return ModelAvailabilityKey{
		ProjectID: m.ProjectID,
		Harness:   m.Harness,
		Model:     strings.TrimSpace(m.Model),
	}
}
