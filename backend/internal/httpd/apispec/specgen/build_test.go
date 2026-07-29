package specgen_test

import (
	"bytes"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apispec"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apispec/specgen"
)

// TestBuild_MatchesEmbedded is the drift guard: the committed (embedded)
// openapi.yaml must equal fresh Build() output. If this fails, run
// `go generate ./...` and commit the result.
func TestBuild_MatchesEmbedded(t *testing.T) {
	got, err := specgen.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	embedded := apispec.Default().YAML()
	if !bytes.Equal(normalizeYAML(got), normalizeYAML(embedded)) {
		t.Fatalf("embedded openapi.yaml is stale — run `go generate ./...` and commit.\n"+
			"len(fresh)=%d len(embedded)=%d", len(got), len(embedded))
	}
}

// TestBuild_Deterministic guards against nondeterministic output (which would
// make the drift check flaky in CI).
func TestBuild_Deterministic(t *testing.T) {
	a, err := specgen.Build()
	if err != nil {
		t.Fatalf("Build #1: %v", err)
	}
	b, err := specgen.Build()
	if err != nil {
		t.Fatalf("Build #2: %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("Build() is not deterministic across calls")
	}
}

func TestBuildIncludesAgentModelsAndHealthContracts(t *testing.T) {
	got, err := specgen.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, want := range [][]byte{
		[]byte("/api/v1/agents/models:"),
		[]byte("operationId: listAgentModels"),
		[]byte("name: force"),
		[]byte("catalogSource:"),
		[]byte("catalogReason:"),
		[]byte("catalogVerified:"),
		[]byte("reviewerCapable:"),
		[]byte("defaultEffort:"),
		[]byte("/api/v1/agents/health:"),
		[]byte("operationId: getAgentHealth"),
		[]byte("remedy:"),
	} {
		if !bytes.Contains(got, want) {
			t.Fatalf("generated spec missing %q", want)
		}
	}
}

func TestBuildIncludesReviewerCapabilityOnAgentInventory(t *testing.T) {
	got, err := specgen.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	want := []byte("AgentInfo:\n      properties:\n        authStatus:")
	start := bytes.Index(got, want)
	if start < 0 {
		t.Fatalf("generated spec missing AgentInfo schema")
	}
	end := bytes.Index(got[start:], []byte("    AgentModelAvailability:"))
	if end < 0 {
		t.Fatalf("generated spec missing schema after AgentInfo")
	}
	agentInfo := got[start : start+end]
	if !bytes.Contains(agentInfo, []byte("reviewerCapable:")) {
		t.Fatalf("generated AgentInfo schema missing reviewerCapable: %s", agentInfo)
	}
}

func normalizeYAML(in []byte) []byte {
	return bytes.ReplaceAll(in, []byte("\r\n"), []byte("\n"))
}
