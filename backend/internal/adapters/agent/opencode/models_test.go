package opencode

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

const verboseModelsFixture = `[92m[1mModels cache refreshed[0m
anthropic/claude-sonnet-4-5
{
  "id": "claude-sonnet-4-5",
  "providerID": "anthropic",
  "name": "Claude Sonnet 4.5",
  "variants": {
    "low": {"reasoningEffort": "low"},
    "high": {"reasoningEffort": "high"},
    "turbo": {"reasoningEffort": "turbo"}
  }
}
openai/gpt-5.4
{
  "id": "gpt-5.4",
  "providerID": "openai",
  "name": "GPT-5.4",
  "variants": {
    "minimal": {"reasoningEffort": "minimal"},
    "medium": {"reasoningEffort": "medium"},
    "max": {"reasoningEffort": "max"}
  }
}
`

func TestParseVerboseModelsUsesProviderIDsAndDeclaredVariants(t *testing.T) {
	models, err := parseVerboseModels([]byte(verboseModelsFixture))
	if err != nil {
		t.Fatal(err)
	}

	want := []ports.ModelCatalogEntry{
		{
			ID:      "anthropic/claude-sonnet-4-5",
			Label:   "Claude Sonnet 4.5",
			Efforts: []domain.Effort{domain.EffortLow, domain.EffortHigh, domain.Effort("turbo")},
			Dynamic: true,
		},
		{
			ID:      "openai/gpt-5.4",
			Label:   "GPT-5.4",
			Efforts: []domain.Effort{domain.EffortMinimal, domain.EffortMedium, domain.EffortMax},
			Dynamic: true,
		},
	}
	if !reflect.DeepEqual(models, want) {
		t.Fatalf("models\nwant: %#v\n got: %#v", want, models)
	}
	if models[0].DefaultEffort != "" || models[1].DefaultEffort != "" {
		t.Fatalf("parser invented a default effort: %#v", models)
	}
}

func TestParseVerboseModelsRejectsMissingOrMalformedCatalog(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "empty", body: "Models cache refreshed\n"},
		{name: "malformed JSON", body: "anthropic/model\n{not-json}\n"},
		{name: "missing provider", body: `{"id":"model","name":"Model","variants":{}}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			models, err := parseVerboseModels([]byte(tt.body))
			if err == nil {
				t.Fatalf("parseVerboseModels() = (%#v, nil), want error", models)
			}
			if models != nil {
				t.Fatalf("models = %#v, want nil on error", models)
			}
		})
	}
}

func TestAvailableModelsRunsNativeRefreshVerboseCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test helper uses a POSIX shell script")
	}
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args")
	fixturePath := filepath.Join(dir, "models.txt")
	if err := os.WriteFile(fixturePath, []byte(verboseModelsFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	scriptPath := filepath.Join(dir, "opencode")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$OPENCODE_TEST_ARGS\"\ncat \"$OPENCODE_TEST_FIXTURE\"\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENCODE_TEST_ARGS", argsPath)
	t.Setenv("OPENCODE_TEST_FIXTURE", fixturePath)

	models, err := (&Plugin{resolvedBinary: scriptPath}).AvailableModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 {
		t.Fatalf("len(models) = %d, want 2", len(models))
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Fields(string(args)), []string{"models", "--refresh", "--verbose"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("command args = %#v, want %#v", got, want)
	}
}

func TestAvailableModelsReturnsCommandFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test helper uses a POSIX shell script")
	}
	scriptPath := filepath.Join(t.TempDir(), "opencode")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\necho 'refresh failed' >&2\nexit 17\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	models, err := (&Plugin{resolvedBinary: scriptPath}).AvailableModels(context.Background())
	if err == nil {
		t.Fatalf("AvailableModels() = (%#v, nil), want error", models)
	}
	if models != nil {
		t.Fatalf("models = %#v, want nil on command failure", models)
	}
	if !strings.Contains(err.Error(), "refresh failed") {
		t.Fatalf("error = %q, want command output", err)
	}
}

func TestAvailableModelsPreservesContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	models, err := (&Plugin{resolvedBinary: "opencode"}).AvailableModels(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if models != nil {
		t.Fatalf("models = %#v, want nil", models)
	}
}
