package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

// UpsertProject inserts or replaces a registered project row.
func (s *Store) UpsertProject(ctx context.Context, r domain.ProjectRecord) error {
	config, err := marshalProjectConfig(r.Config)
	if err != nil {
		return err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return upsertProject(ctx, s.qw, r, config)
}

// UpsertWorkspaceProject inserts or replaces a workspace project and its child
// repository registry in one transaction. The child set is authoritative.
func (s *Store) UpsertWorkspaceProject(ctx context.Context, r domain.ProjectRecord, repos []domain.WorkspaceRepoRecord) error {
	config, err := marshalProjectConfig(r.Config)
	if err != nil {
		return err
	}
	return s.writeWorkspaceProject(ctx, "upsert workspace project", r, repos, func(q *gen.Queries) error {
		return upsertProject(ctx, q, r, config)
	})
}

// ImportWorkspaceProject inserts or replaces a workspace project from an
// authoritative import source, including the source registration timestamp.
func (s *Store) ImportWorkspaceProject(ctx context.Context, r domain.ProjectRecord, repos []domain.WorkspaceRepoRecord) error {
	config, err := marshalProjectConfig(r.Config)
	if err != nil {
		return err
	}
	return s.writeWorkspaceProject(ctx, "import workspace project", r, repos, func(q *gen.Queries) error {
		return importProject(ctx, q, r, config)
	})
}

func (s *Store) writeWorkspaceProject(ctx context.Context, label string, r domain.ProjectRecord, repos []domain.WorkspaceRepoRecord, writeProject func(*gen.Queries) error) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.inTx(ctx, label, func(q *gen.Queries) error {
		if err := writeProject(q); err != nil {
			return err
		}
		if err := q.DeleteWorkspaceReposByProject(ctx, domain.ProjectID(r.ID)); err != nil {
			return err
		}
		for _, repo := range repos {
			if err := q.UpsertWorkspaceRepo(ctx, gen.UpsertWorkspaceRepoParams{
				ProjectID:     domain.ProjectID(r.ID),
				Name:          repo.Name,
				RelativePath:  repo.RelativePath,
				RepoOriginURL: repo.RepoOriginURL,
				RegisteredAt:  repo.RegisteredAt,
			}); err != nil {
				return err
			}
		}
		return nil
	})
}

// ListWorkspaceRepos returns the registered direct child repos for a workspace project.
func (s *Store) ListWorkspaceRepos(ctx context.Context, projectID string) ([]domain.WorkspaceRepoRecord, error) {
	rows, err := s.qr.ListWorkspaceRepos(ctx, domain.ProjectID(projectID))
	if err != nil {
		return nil, fmt.Errorf("list workspace repos for %s: %w", projectID, err)
	}
	out := make([]domain.WorkspaceRepoRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.WorkspaceRepoRecord{
			ProjectID:     row.ProjectID,
			Name:          row.Name,
			RelativePath:  row.RelativePath,
			RepoOriginURL: row.RepoOriginURL,
			RegisteredAt:  row.RegisteredAt,
		})
	}
	return out, nil
}

func upsertProject(ctx context.Context, q *gen.Queries, r domain.ProjectRecord, config sql.NullString) error {
	kind := r.Kind.WithDefault()
	return q.UpsertProject(ctx, gen.UpsertProjectParams{
		ID:            domain.ProjectID(r.ID),
		Path:          r.Path,
		RepoOriginURL: r.RepoOriginURL,
		DisplayName:   r.DisplayName,
		RegisteredAt:  r.RegisteredAt,
		ArchivedAt:    nullTime(r.ArchivedAt),
		Config:        config,
		Kind:          string(kind),
	})
}

func importProject(ctx context.Context, q *gen.Queries, r domain.ProjectRecord, config sql.NullString) error {
	existingByID, err := q.GetProject(ctx, domain.ProjectID(r.ID))
	if err == nil {
		if existingByID.ArchivedAt.Valid {
			return &domain.ProjectImportConflictError{Conflict: domain.ProjectImportConflict{
				ProjectID:  r.ID,
				Path:       r.Path,
				Reason:     domain.ProjectImportConflictSameIDArchivedTarget,
				TargetID:   string(existingByID.ID),
				TargetPath: existingByID.Path,
			}}
		}
		if existingByID.Path != r.Path {
			return &domain.ProjectImportConflictError{Conflict: domain.ProjectImportConflict{
				ProjectID:  r.ID,
				Path:       r.Path,
				Reason:     domain.ProjectImportConflictSameIDDifferentActivePath,
				TargetID:   string(existingByID.ID),
				TargetPath: existingByID.Path,
			}}
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("check imported project id conflict: %w", err)
	}

	existingByPath, err := q.FindProjectByPath(ctx, r.Path)
	if err == nil {
		if existingByPath.ID != domain.ProjectID(r.ID) {
			return &domain.ProjectImportConflictError{Conflict: domain.ProjectImportConflict{
				ProjectID:  r.ID,
				Path:       r.Path,
				Reason:     domain.ProjectImportConflictSamePathDifferentActiveID,
				TargetID:   string(existingByPath.ID),
				TargetPath: existingByPath.Path,
			}}
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("check imported project path conflict: %w", err)
	}

	kind := r.Kind.WithDefault()
	return q.UpsertImportedProject(ctx, gen.UpsertImportedProjectParams{
		ID:            domain.ProjectID(r.ID),
		Path:          r.Path,
		RepoOriginURL: r.RepoOriginURL,
		DisplayName:   r.DisplayName,
		RegisteredAt:  r.RegisteredAt,
		ArchivedAt:    nullTime(r.ArchivedAt),
		Config:        config,
		Kind:          string(kind),
	})
}

// GetProject returns a project by id, active or archived.
func (s *Store) GetProject(ctx context.Context, id string) (domain.ProjectRecord, bool, error) {
	p, err := s.qr.GetProject(ctx, domain.ProjectID(id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ProjectRecord{}, false, nil
	}
	if err != nil {
		return domain.ProjectRecord{}, false, fmt.Errorf("get project %s: %w", id, err)
	}
	return projectRowFromGen(p), true, nil
}

// FindProjectByPath returns a project registered at path, active or archived.
func (s *Store) FindProjectByPath(ctx context.Context, path string) (domain.ProjectRecord, bool, error) {
	p, err := s.qr.FindProjectByPath(ctx, path)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ProjectRecord{}, false, nil
	}
	if err != nil {
		return domain.ProjectRecord{}, false, fmt.Errorf("find project by path %s: %w", path, err)
	}
	return projectRowFromGen(p), true, nil
}

// ListProjects returns active projects ordered by id.
func (s *Store) ListProjects(ctx context.Context) ([]domain.ProjectRecord, error) {
	rows, err := s.qr.ListProjects(ctx)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	out := make([]domain.ProjectRecord, 0, len(rows))
	for _, p := range rows {
		out = append(out, projectRowFromGen(p))
	}
	return out, nil
}

// UpdateProjectSettings atomically updates the user-facing display name and
// config for an active project. It returns ok=false when the project is missing
// or archived.
func (s *Store) UpdateProjectSettings(ctx context.Context, id, displayName string, config domain.ProjectConfig) (bool, error) {
	encodedConfig, err := marshalProjectConfig(config)
	if err != nil {
		return false, err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	rows, err := s.qw.UpdateProjectSettings(ctx, gen.UpdateProjectSettingsParams{
		ID:          domain.ProjectID(id),
		DisplayName: displayName,
		Config:      encodedConfig,
	})
	if err != nil {
		return false, fmt.Errorf("update project settings %s: %w", id, err)
	}
	return rows > 0, nil
}

// CountProjectsIncludingArchived returns all registry rows, including projects
// the user archived. It is intentionally separate from ListProjects so first-run
// seeding does not recreate Scratch after any project has existed.
func (s *Store) CountProjectsIncludingArchived(ctx context.Context) (int, error) {
	count, err := s.qr.CountProjectsIncludingArchived(ctx)
	if err != nil {
		return 0, fmt.Errorf("count projects including archived: %w", err)
	}
	return int(count), nil
}

// ArchiveProject soft-deletes a project and reports whether a row was affected.
func (s *Store) ArchiveProject(ctx context.Context, id string, at time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	n, err := s.qw.ArchiveProject(ctx, gen.ArchiveProjectParams{
		ArchivedAt: nullTime(at),
		ID:         domain.ProjectID(id),
	})
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// SetProjectPaused writes only the per-project pause bit, leaving the config
// column untouched. Reports whether a matching row was updated.
func (s *Store) SetProjectPaused(ctx context.Context, id string, paused bool) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	n, err := s.qw.SetProjectPaused(ctx, gen.SetProjectPausedParams{
		Paused: paused,
		ID:     domain.ProjectID(id),
	})
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// SetProjectOriginURL updates only the project's origin URL. The SCM observer
// must not upsert a previously-read whole project row because that would rewrite
// config and could revert a concurrent config edit.
func (s *Store) SetProjectOriginURL(ctx context.Context, id, originURL string) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	n, err := s.qw.SetProjectOriginURL(ctx, gen.SetProjectOriginURLParams{
		RepoOriginURL: originURL,
		ID:            domain.ProjectID(id),
	})
	if err != nil {
		return false, fmt.Errorf("set project %s origin url: %w", id, err)
	}
	return n > 0, nil
}

// SetProjectConfig updates only the project's config column.
func (s *Store) SetProjectConfig(ctx context.Context, id string, config domain.ProjectConfig) (bool, error) {
	raw, err := marshalProjectConfig(config)
	if err != nil {
		return false, err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	n, err := s.qw.SetProjectConfig(ctx, gen.SetProjectConfigParams{
		Config: raw,
		ID:     domain.ProjectID(id),
	})
	if err != nil {
		return false, fmt.Errorf("set project %s config: %w", id, err)
	}
	return n > 0, nil
}

// GetFleetPaused reads the daemon-global fleet pause flag. The single
// daemon_settings row is seeded unpaused at migration time.
func (s *Store) GetFleetPaused(ctx context.Context) (bool, error) {
	return s.qr.GetFleetPaused(ctx)
}

// SetFleetPaused writes the daemon-global fleet pause flag.
func (s *Store) SetFleetPaused(ctx context.Context, paused bool) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.qw.SetFleetPaused(ctx, paused)
}

// GetPrimeSettings reads the daemon-owned fleet Prime settings.
func (s *Store) GetPrimeSettings(ctx context.Context) (domain.PrimeSettings, error) {
	raw, err := s.qr.GetPrimeSettingsJSON(ctx)
	if err != nil {
		return domain.PrimeSettings{}, fmt.Errorf("get prime settings: %w", err)
	}
	return unmarshalPrimeSettings(raw)
}

// SetPrimeSettings validates and writes the daemon-owned fleet Prime settings.
func (s *Store) SetPrimeSettings(ctx context.Context, settings domain.PrimeSettings) error {
	settings = settings.WithDefaults()
	if err := settings.Validate(); err != nil {
		return err
	}
	raw, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("marshal prime settings: %w", err)
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.qw.SetPrimeSettingsJSON(ctx, string(raw))
}

func unmarshalPrimeSettings(raw string) (domain.PrimeSettings, error) {
	if raw == "" || raw == "{}" {
		return domain.DefaultPrimeSettings(), nil
	}
	var settings domain.PrimeSettings
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		return domain.PrimeSettings{}, fmt.Errorf("decode prime settings: %w", err)
	}
	settings = settings.WithDefaults()
	if err := settings.Validate(); err != nil {
		return domain.PrimeSettings{}, fmt.Errorf("stored prime settings invalid: %w", err)
	}
	return settings, nil
}

func projectRowFromGen(p gen.Project) domain.ProjectRecord {
	r := domain.ProjectRecord{
		ID:            string(p.ID),
		Path:          p.Path,
		RepoOriginURL: p.RepoOriginURL,
		DisplayName:   p.DisplayName,
		RegisteredAt:  p.RegisteredAt,
		Kind:          domain.ProjectKind(p.Kind).WithDefault(),
		Config:        unmarshalProjectConfig(p.Config),
		Paused:        p.Paused,
	}
	if p.ArchivedAt.Valid {
		r.ArchivedAt = p.ArchivedAt.Time
	}
	return r
}

// marshalProjectConfig encodes the typed per-project config into the nullable
// JSON column. An IsZero config stores SQL NULL so an unset config round-trips
// back to a zero value rather than an empty object.
func marshalProjectConfig(cfg domain.ProjectConfig) (sql.NullString, error) {
	if cfg.IsZero() {
		return sql.NullString{}, nil
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return sql.NullString{}, fmt.Errorf("marshal project config: %w", err)
	}
	return sql.NullString{String: string(data), Valid: true}, nil
}

// unmarshalProjectConfig decodes the nullable JSON column back into the typed
// struct. SQL NULL (an unset config) decodes to a zero value. A damaged config
// (invalid JSON from a direct DB edit or migration bug) also degrades to a zero
// config rather than erroring — a corrupt config must never block access to the
// project row, nor fail an entire ListProjects.
func unmarshalProjectConfig(s sql.NullString) domain.ProjectConfig {
	if !s.Valid || s.String == "" {
		return domain.ProjectConfig{}
	}
	var cfg domain.ProjectConfig
	if err := json.Unmarshal([]byte(s.String), &cfg); err != nil {
		return domain.ProjectConfig{}
	}
	return cfg
}

func nullTime(t time.Time) sql.NullTime {
	if t.IsZero() {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: t, Valid: true}
}
