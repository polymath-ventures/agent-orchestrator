package sessionmanager

import (
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// generatedGuidance is every agent-facing instruction the daemon composes at
// runtime, as opposed to the static skill files embedded under skillassets.
// Both ship to agents and both are executable configuration on the naming path:
// an orchestrator told to pass a name will pass one, and an explicit name
// outranks the daemon's computed one.
//
// The skillassets guard covers the embedded tree; this one covers what that
// guard structurally cannot see. Listing the producers explicitly is what lets
// it fail when one is renamed away rather than silently covering nothing.
func generatedGuidance() map[string]string {
	return map[string]string{
		"copilotOrchestratorMessage": copilotOrchestratorMessage("demo", "do the thing"),
	}
}

func TestGeneratedGuidanceDoesNotTeachAgentsToNameSessions(t *testing.T) {
	guidance := generatedGuidance()
	if len(guidance) == 0 {
		t.Fatal("generatedGuidance is empty, so this guard covers nothing")
	}
	for producer, body := range guidance {
		t.Run(producer, func(t *testing.T) {
			if strings.TrimSpace(body) == "" {
				t.Fatalf("%s produced no text; the guard would pass vacuously", producer)
			}
			// Whole-body, not line-by-line: a spawn example wrapped across lines
			// would slip past a per-line check, and no generated guidance has a
			// legitimate reason to mention the name flag at all once it mentions
			// spawning.
			if (strings.Contains(body, "ao spawn") || strings.Contains(body, "aong spawn")) && strings.Contains(body, "--name") {
				t.Errorf("%s describes spawning and mentions the name flag; the daemon computes the name:\n%s", producer, body)
			}
		})
	}
}

// The producer must still emit the directive it exists for, so the guard above
// cannot be satisfied by gutting the text.
func TestCopilotOrchestratorMessageStillDirectsASpawn(t *testing.T) {
	body := copilotOrchestratorMessage(domain.ProjectID("demo"), "do the thing")
	for _, want := range []string{"ao spawn", "--project demo", "--prompt", "do the thing"} {
		if !strings.Contains(body, want) {
			t.Errorf("copilot orchestrator directive is missing %q:\n%s", want, body)
		}
	}
}
