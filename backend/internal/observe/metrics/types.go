// Package metrics implements the daemon usage/quota observer: a coarse-tick
// poller that aggregates token telemetry, records subscription quota snapshots,
// and emits quota threshold crossings.
package metrics

import (
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// Snapshot is one full usage/quota observation. It is the wire shape returned by
// GET /api/v1/metrics (latest) and stored in the observer's bounded history.
type Snapshot struct {
	// CollectedAt is when the snapshot was produced (UTC).
	CollectedAt time.Time `json:"collectedAt"`
	// Cost holds token/cost aggregates over the configured rolling window.
	Cost Cost `json:"cost"`
	// Quotas holds the latest subscription quota state per harness/account.
	Quotas []domain.QuotaSnapshot `json:"quotas"`
	// Alerts holds the alert conditions currently firing at snapshot time.
	Alerts []Alert `json:"alerts"`
}

// CostTotals is a token/cost aggregate for one dimension.
type CostTotals struct {
	// InputTokens/OutputTokens/TotalTokens sum the matching numeric payload
	// fields across telemetry events in the window.
	InputTokens  int64 `json:"inputTokens"`
	OutputTokens int64 `json:"outputTokens"`
	TotalTokens  int64 `json:"totalTokens"`
	// CostUSD sums the cost_usd payload field across events in the window.
	CostUSD float64 `json:"costUsd"`
	// Events is the number of cost-bearing telemetry events aggregated in the
	// window (events carrying at least one recognised token/cost field).
	Events int64 `json:"events"`
}

// ProjectCost is a cost aggregate grouped by telemetry project_id.
type ProjectCost struct {
	// ProjectID is the telemetry project id for this aggregate.
	ProjectID string `json:"projectId"`
	CostTotals
}

// HarnessCost is a cost aggregate grouped by the agent harness reported in
// telemetry payload metadata.
type HarnessCost struct {
	// Harness is the agent harness key (for example claude-code or codex).
	Harness string `json:"harness"`
	CostTotals
}

// Cost holds token/cost aggregates derived from cost-bearing telemetry events
// over the observer's rolling window. Producers must emit at least one of
// input_tokens, output_tokens, total_tokens, or cost_usd in payload_json for an
// event to be counted; if no current harness emits those keys, the aggregate is
// correctly zero while the schema remains ready for the producer. The top-level
// fields are fleet-wide totals; grouped slices are attributed subsets by project
// and by harness (events missing a project_id or harness/source still contribute
// to fleet totals but not to the corresponding grouping).
type Cost struct {
	// WindowSeconds is the length of the rolling aggregation window.
	WindowSeconds int64 `json:"windowSeconds"`
	CostTotals
	// ByProject groups cost-bearing telemetry by project_id.
	ByProject []ProjectCost `json:"byProject"`
	// ByHarness groups cost-bearing telemetry by payload harness metadata.
	ByHarness []HarnessCost `json:"byHarness"`
	// Truncated is true when the window held more telemetry rows than the scan
	// limit, so the aggregate covers only the most recent costScanLimit events.
	Truncated bool `json:"truncated"`
}
