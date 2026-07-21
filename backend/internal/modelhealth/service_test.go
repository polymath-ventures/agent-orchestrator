package modelhealth

import (
	"context"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

type fakeStore struct {
	projects []domain.ProjectRecord
	health   map[domain.ModelAvailabilityKey]domain.ModelAvailability
}

func (f *fakeStore) ListProjects(context.Context) ([]domain.ProjectRecord, error) {
	return f.projects, nil
}

func (f *fakeStore) GetProject(_ context.Context, id string) (domain.ProjectRecord, bool, error) {
	for _, row := range f.projects {
		if row.ID == id {
			return row, true, nil
		}
	}
	return domain.ProjectRecord{}, false, nil
}

func (f *fakeStore) ListModelHealthByProject(_ context.Context, projectID domain.ProjectID) ([]domain.ModelAvailability, error) {
	var out []domain.ModelAvailability
	for key, rec := range f.health {
		if key.ProjectID == projectID {
			out = append(out, rec)
		}
	}
	return out, nil
}

func (f *fakeStore) UpsertModelHealth(_ context.Context, rec domain.ModelAvailability) (domain.ModelAvailability, error) {
	if f.health == nil {
		f.health = map[domain.ModelAvailabilityKey]domain.ModelAvailability{}
	}
	f.health[rec.Key()] = rec
	return rec, nil
}

type fakeAgent struct {
	result ports.ModelValidationResult
}

func (fakeAgent) GetConfigSpec(context.Context) (ports.ConfigSpec, error) {
	return ports.ConfigSpec{}, nil
}
func (fakeAgent) GetLaunchCommand(context.Context, ports.LaunchConfig) ([]string, error) {
	return nil, nil
}
func (fakeAgent) GetPromptDeliveryStrategy(context.Context, ports.LaunchConfig) (ports.PromptDeliveryStrategy, error) {
	return ports.PromptDeliveryInCommand, nil
}
func (fakeAgent) GetAgentHooks(context.Context, ports.WorkspaceHookConfig) error { return nil }
func (fakeAgent) GetRestoreCommand(context.Context, ports.RestoreConfig) ([]string, bool, error) {
	return nil, false, nil
}
func (fakeAgent) SessionInfo(context.Context, ports.SessionRef) (ports.SessionInfo, bool, error) {
	return ports.SessionInfo{}, false, nil
}
func (f fakeAgent) ValidateModel(context.Context, string) (ports.ModelValidationResult, error) {
	return f.result, nil
}

type notifying struct {
	intents []ports.NotificationIntent
}

func (n *notifying) Notify(_ context.Context, intent ports.NotificationIntent) error {
	n.intents = append(n.intents, intent)
	return nil
}

func TestListProjectReportsNotProbedAndNoCapability(t *testing.T) {
	st := &fakeStore{projects: []domain.ProjectRecord{{
		ID: "mer",
		Config: domain.ProjectConfig{
			AgentConfig: domain.AgentConfig{
				ModelByHarness: map[domain.AgentHarness]domain.HarnessModel{
					domain.HarnessCodex:      {Model: "gpt-5-codex"},
					domain.HarnessClaudeCode: {Model: "opus"},
				},
			},
		},
	}}}
	svc := New(Deps{
		Store:          st,
		DefaultHarness: domain.HarnessCodex,
		Agents:         []AgentEntry{{Harness: domain.HarnessCodex, Agent: fakeAgent{}}},
	})

	rows, err := svc.ListProject(context.Background(), "mer")
	if err != nil {
		t.Fatalf("ListProject: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %+v, want two configured pins", rows)
	}
	for _, row := range rows {
		switch row.Harness {
		case domain.HarnessCodex:
			if row.Reason != domain.ModelAvailabilityReasonNotProbed {
				t.Fatalf("codex reason = %q, want not-probed", row.Reason)
			}
		case domain.HarnessClaudeCode:
			if row.Reason != domain.ModelAvailabilityReasonNoCapability {
				t.Fatalf("claude-code reason = %q, want no-capability", row.Reason)
			}
		}
	}
}

func TestRefreshProjectEmitsTransitionNotification(t *testing.T) {
	now := time.Date(2026, 7, 20, 15, 0, 0, 0, time.UTC)
	project := domain.ProjectRecord{
		ID: "mer",
		Config: domain.ProjectConfig{AgentConfig: domain.AgentConfig{ModelByHarness: map[domain.AgentHarness]domain.HarnessModel{
			domain.HarnessCodex: {Model: "gpt-5-codex"},
		}}},
	}
	st := &fakeStore{
		projects: []domain.ProjectRecord{project},
		health: map[domain.ModelAvailabilityKey]domain.ModelAvailability{
			{ProjectID: "mer", Harness: domain.HarnessCodex, Model: "gpt-5-codex"}: {
				ProjectID: "mer",
				Harness:   domain.HarnessCodex,
				Model:     "gpt-5-codex",
				Status:    domain.ModelAvailabilityReachable,
				Reason:    domain.ModelAvailabilityReasonReachable,
			},
		},
	}
	notifier := &notifying{}
	svc := New(Deps{
		Store:          st,
		DefaultHarness: domain.HarnessCodex,
		Clock:          func() time.Time { return now },
		Notifier:       notifier,
		Agents: []AgentEntry{{Harness: domain.HarnessCodex, Agent: fakeAgent{
			result: ports.ModelValidationResult{Status: ports.ModelValidationUnreachable, Message: "unknown model"},
		}}},
	})

	if err := svc.RefreshProject(context.Background(), project); err != nil {
		t.Fatalf("RefreshProject: %v", err)
	}
	key := domain.ModelAvailabilityKey{ProjectID: "mer", Harness: domain.HarnessCodex, Model: "gpt-5-codex"}
	if got := st.health[key]; got.Status != domain.ModelAvailabilityUnreachable || got.Reason != domain.ModelAvailabilityReasonUnreachable {
		t.Fatalf("cached = %+v, want unreachable", got)
	}
	if len(notifier.intents) != 1 || notifier.intents[0].Type != domain.NotificationModelUnreachable {
		t.Fatalf("notifications = %+v, want one model_unreachable", notifier.intents)
	}
}
