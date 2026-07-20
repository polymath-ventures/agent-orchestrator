package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

// UpsertQuotaSnapshot stores the latest quota state for one harness/account/window.
func (s *Store) UpsertQuotaSnapshot(ctx context.Context, snap domain.QuotaSnapshot) (domain.QuotaSnapshot, error) {
	if snap.ObservedAt.IsZero() {
		snap.ObservedAt = time.Now().UTC()
	}
	row, err := s.qw.UpsertQuotaSnapshot(ctx, gen.UpsertQuotaSnapshotParams{
		ID:            "qts_" + uuid.NewString(),
		Harness:       string(snap.Harness),
		AccountID:     snap.AccountID,
		Model:         snap.Model,
		WindowStart:   quotaNullTime(snap.WindowStart),
		WindowEnd:     quotaNullTime(snap.WindowEnd),
		Used:          nullFloat(snap.Used),
		Remaining:     nullFloat(snap.Remaining),
		LimitValue:    nullFloat(snap.Limit),
		SignalQuality: string(snap.SignalQuality),
		Source:        snap.Source,
		Basis:         snap.Basis,
		ObservedAt:    snap.ObservedAt.UTC(),
	})
	if err != nil {
		return domain.QuotaSnapshot{}, fmt.Errorf("upsert quota snapshot %s/%s: %w", snap.Harness, snap.AccountID, err)
	}
	return quotaSnapshotFromGen(row), nil
}

// ListLatestQuotaSnapshots returns one latest snapshot per harness/account/model.
func (s *Store) ListLatestQuotaSnapshots(ctx context.Context) ([]domain.QuotaSnapshot, error) {
	rows, err := s.qr.ListLatestQuotaSnapshots(ctx)
	if err != nil {
		return nil, fmt.Errorf("list latest quota snapshots: %w", err)
	}
	out := make([]domain.QuotaSnapshot, 0, len(rows))
	for _, row := range rows {
		out = append(out, quotaSnapshotFromGen(row))
	}
	return out, nil
}

func quotaSnapshotFromGen(row gen.QuotaSnapshot) domain.QuotaSnapshot {
	return domain.QuotaSnapshot{
		Harness:       domain.AgentHarness(row.Harness),
		AccountID:     row.AccountID,
		Model:         row.Model,
		WindowStart:   quotaTimeFromNull(row.WindowStart),
		WindowEnd:     quotaTimeFromNull(row.WindowEnd),
		Used:          floatFromNull(row.Used),
		Remaining:     floatFromNull(row.Remaining),
		Limit:         floatFromNull(row.LimitValue),
		SignalQuality: domain.QuotaSignalQuality(row.SignalQuality),
		Source:        row.Source,
		Basis:         row.Basis,
		ObservedAt:    row.ObservedAt,
	}
}

func quotaNullTime(t time.Time) sql.NullTime {
	if t.IsZero() {
		return sql.NullTime{Time: time.Time{}.UTC(), Valid: true}
	}
	return sql.NullTime{Time: t.UTC(), Valid: true}
}

func quotaTimeFromNull(t sql.NullTime) time.Time {
	if !t.Valid {
		return time.Time{}
	}
	if t.Time.IsZero() {
		return time.Time{}
	}
	return t.Time
}

func nullFloat(v *float64) sql.NullFloat64 {
	if v == nil {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: *v, Valid: true}
}

func floatFromNull(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	f := v.Float64
	return &f
}
