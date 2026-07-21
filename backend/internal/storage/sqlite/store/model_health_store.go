package store

import (
	"context"
	"fmt"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

// UpsertModelHealth stores the latest cached availability verdict for a
// configured project model pin.
func (s *Store) UpsertModelHealth(ctx context.Context, rec domain.ModelAvailability) (domain.ModelAvailability, error) {
	now := time.Now().UTC()
	if rec.ObservedAt.IsZero() {
		rec.ObservedAt = now
	}
	if rec.UpdatedAt.IsZero() {
		rec.UpdatedAt = now
	}
	row, err := s.qw.UpsertModelHealth(ctx, gen.UpsertModelHealthParams{
		ProjectID:  rec.ProjectID,
		Harness:    rec.Harness,
		Model:      rec.Model,
		Status:     rec.Status,
		Reason:     rec.Reason,
		Message:    rec.Message,
		ObservedAt: rec.ObservedAt.UTC(),
		UpdatedAt:  rec.UpdatedAt.UTC(),
	})
	if err != nil {
		return domain.ModelAvailability{}, fmt.Errorf("upsert model health %s/%s/%s: %w", rec.ProjectID, rec.Harness, rec.Model, err)
	}
	return modelHealthFromGen(row), nil
}

// ListModelHealthByProject returns cached verdicts for one project.
func (s *Store) ListModelHealthByProject(ctx context.Context, projectID domain.ProjectID) ([]domain.ModelAvailability, error) {
	rows, err := s.qr.ListModelHealthByProject(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list model health for project %s: %w", projectID, err)
	}
	out := make([]domain.ModelAvailability, 0, len(rows))
	for _, row := range rows {
		out = append(out, modelHealthFromGen(row))
	}
	return out, nil
}

func modelHealthFromGen(row gen.ModelHealth) domain.ModelAvailability {
	return domain.ModelAvailability{
		ProjectID:  row.ProjectID,
		Harness:    row.Harness,
		Model:      row.Model,
		Status:     row.Status,
		Reason:     row.Reason,
		Message:    row.Message,
		ObservedAt: row.ObservedAt,
		UpdatedAt:  row.UpdatedAt,
	}
}
