package ports

import (
	"context"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

type catalogStub struct{}

func (catalogStub) AvailableModels(context.Context) ([]ModelCatalogEntry, error) {
	return []ModelCatalogEntry{{
		ID:            "fugu-ultra",
		Label:         "Fugu Ultra",
		Efforts:       []domain.Effort{domain.EffortHigh, domain.EffortXHigh},
		DefaultEffort: domain.EffortHigh,
		Dynamic:       true,
	}}, nil
}

func TestAgentModelCatalogCarriesHarnessNativeMetadata(t *testing.T) {
	var catalog AgentAvailableModels = catalogStub{}
	models, err := catalog.AvailableModels(context.Background())
	if err != nil {
		t.Fatalf("AvailableModels: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("models = %#v, want one", models)
	}
	got := models[0]
	if got.ID != "fugu-ultra" || got.Label != "Fugu Ultra" || got.DefaultEffort != domain.EffortHigh || !got.Dynamic {
		t.Fatalf("model = %#v, want native id/label/default/dynamic metadata", got)
	}
	if len(got.Efforts) != 2 || got.Efforts[0] != domain.EffortHigh || got.Efforts[1] != domain.EffortXHigh {
		t.Fatalf("efforts = %#v, want [high xhigh]", got.Efforts)
	}
}

func TestModelValidationStatusValid(t *testing.T) {
	for _, status := range []ModelValidationStatus{
		ModelValidationReachable,
		ModelValidationUnreachable,
		ModelValidationProbeUnavailable,
	} {
		if !status.Valid() {
			t.Errorf("%q.Valid() = false, want true", status)
		}
	}
	if ModelValidationStatus("maybe").Valid() {
		t.Fatal("unknown validation status must be invalid")
	}
}
