package project

import "github.com/aoagents/agent-orchestrator/backend/internal/domain"

// GetResult is the discriminated result returned by Service.Get.
type GetResult struct {
	Status   string
	Project  *Project
	Degraded *Degraded
}

// AddInput is the body shape for POST /api/v1/projects.
type AddInput struct {
	Path        string                `json:"path"`
	ProjectID   *string               `json:"projectId,omitempty"`
	Name        *string               `json:"name,omitempty"`
	Config      *domain.ProjectConfig `json:"config,omitempty"`
	AsWorkspace bool                  `json:"asWorkspace,omitempty"`
}

// InitializeRepositoryInput is the body shape for POST /api/v1/projects/initialize.
type InitializeRepositoryInput struct {
	Path string `json:"path"`
}

// InitializeRepositoryResult reports the repository path initialized for onboarding.
type InitializeRepositoryResult struct {
	Path string `json:"path"`
}

// UpdateSettingsInput is the body shape for PUT /api/v1/projects/{id}. It
// atomically replaces the user-facing display name and per-project config.
type UpdateSettingsInput struct {
	DisplayName string               `json:"displayName" minLength:"1" maxLength:"20"`
	Config      domain.ProjectConfig `json:"config"`
	IfMatch     string               `json:"-"`
}

// SetConfigInput is the body shape for PUT /api/v1/projects/{id}/config. Config
// replaces the project's stored config wholesale; a zero-value config clears it.
//
// Because the write is a whole-object replace, a writer working from a stale read
// silently drops every field it never saw. IfMatch is how a writer proves it is
// not stale: it carries the ConfigETag from the read the edit was built on, and a
// mismatch is refused. "*" opts out deliberately — it is what a whole-object
// writer like the config-as-code restore path sends, since overwriting drift is
// its entire job. It is populated from the request's If-Match header, never from
// the JSON body.
type SetConfigInput struct {
	Config  domain.ProjectConfig `json:"config"`
	IfMatch string               `json:"-"`
}

// RemoveResult reports what DELETE /api/v1/projects/{id} actually did.
type RemoveResult struct {
	ProjectID         domain.ProjectID `json:"projectId"`
	RemovedStorageDir bool             `json:"removedStorageDir"`
}
