package codex

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestAvailableModelsUsesAppServerHandshakeAndPreservesMetadata(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script app-server fake is Unix-specific")
	}
	dir := t.TempDir()
	requests := filepath.Join(dir, "requests.jsonl")
	bin := filepath.Join(dir, "codex")
	script := `#!/bin/sh
read init
printf '%s\n' "$init" >> "$AO_REQUESTS"
printf '%s\n' '{"id":1,"result":{"userAgent":"fake","codexHome":"/tmp","platformFamily":"unix","platformOs":"linux"}}'
read initialized
printf '%s\n' "$initialized" >> "$AO_REQUESTS"
read list
printf '%s\n' "$list" >> "$AO_REQUESTS"
printf '%s\n' '{"method":"server/notification","params":{}}'
printf '%s\n' '{"id":2,"result":{"data":[{"id":"gpt-native","model":"gpt-native","displayName":"GPT Native","description":"native","hidden":false,"isDefault":true,"defaultReasoningEffort":"medium","supportedReasoningEfforts":[{"reasoningEffort":"low","description":"fast"},{"reasoningEffort":"medium","description":"balanced"},{"reasoningEffort":"high","description":"deep"},{"reasoningEffort":"Future-Native","description":"future"}]}],"nextCursor":null}}'
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AO_REQUESTS", requests)

	plugin := &Plugin{resolvedBinary: bin}
	models, err := plugin.AvailableModels(context.Background())
	if err != nil {
		t.Fatalf("AvailableModels: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("models = %#v, want one", models)
	}
	got := models[0]
	if got.ID != "gpt-native" || got.Label != "GPT Native" || !got.Dynamic {
		t.Fatalf("model identity = %#v", got)
	}
	wantEfforts := []domain.Effort{domain.EffortLow, domain.EffortMedium, domain.EffortHigh, domain.Effort("Future-Native")}
	if strings.Join(effortStrings(got.Efforts), ",") != strings.Join(effortStrings(wantEfforts), ",") {
		t.Fatalf("efforts = %#v, want %#v", got.Efforts, wantEfforts)
	}
	if got.DefaultEffort != domain.EffortMedium {
		t.Fatalf("default effort = %q, want medium", got.DefaultEffort)
	}

	data, err := os.ReadFile(requests)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 {
		t.Fatalf("requests = %q, want initialize, initialized, model/list", data)
	}
	var initialize, initialized, list struct {
		ID     int            `json:"id"`
		Method string         `json:"method"`
		Params map[string]any `json:"params"`
	}
	for i, target := range []any{&initialize, &initialized, &list} {
		if err := json.Unmarshal([]byte(lines[i]), target); err != nil {
			t.Fatalf("decode request %d: %v", i, err)
		}
	}
	if initialize.ID != 1 || initialize.Method != "initialize" {
		t.Fatalf("initialize request = %s", lines[0])
	}
	if initialized.Method != "initialized" {
		t.Fatalf("initialized notification = %s", lines[1])
	}
	if list.ID != 2 || list.Method != "model/list" || list.Params["includeHidden"] != false {
		t.Fatalf("model/list request = %s", lines[2])
	}
}

func TestAvailableModelsReturnsErrorForEmptyAppServerCatalog(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script app-server fake is Unix-specific")
	}
	bin := writeFakeScript(t, `#!/bin/sh
read init
printf '%s\n' '{"id":1,"result":{"userAgent":"fake","codexHome":"/tmp","platformFamily":"unix","platformOs":"linux"}}'
read initialized
read list
printf '%s\n' '{"id":2,"result":{"data":[]}}'
`)
	_, err := (&Plugin{resolvedBinary: bin}).AvailableModels(context.Background())
	if err == nil {
		t.Fatal("AvailableModels error = nil, want empty catalog failure")
	}
}

func TestAvailableModelsAgainstInstalledCodex(t *testing.T) {
	if os.Getenv("AO_TEST_CODEX_CATALOG") != "1" {
		t.Skip("set AO_TEST_CODEX_CATALOG=1 to exercise the installed app-server")
	}
	models, err := New().AvailableModels(context.Background())
	if err != nil {
		t.Fatalf("installed Codex catalog: %v", err)
	}
	if len(models) == 0 {
		t.Fatal("installed Codex catalog is empty")
	}
	for _, model := range models {
		if model.ID == "" || model.Label == "" || len(model.Efforts) == 0 || model.DefaultEffort == "" {
			t.Fatalf("installed Codex model lost native metadata: %#v", model)
		}
	}
	foundUltra := false
	for _, model := range models {
		for _, effort := range model.Efforts {
			foundUltra = foundUltra || effort == domain.Effort("ultra")
		}
	}
	if !foundUltra {
		t.Fatal("installed Codex catalog advertised ultra but AO did not preserve it")
	}
}

func TestFuguAvailableModelsReadsInstalledCatalogAndNormalizesMax(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	catalogDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(catalogDir, 0o755); err != nil {
		t.Fatal(err)
	}
	data := `{"models":[{"slug":"fugu-custom","display_name":"Fugu Custom","supported_reasoning_levels":[{"effort":"high"},{"effort":"max"},{"effort":"Future-Fugu"}]}]}`
	if err := os.WriteFile(filepath.Join(catalogDir, "fugu.json"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	models, err := NewFugu().AvailableModels(context.Background())
	if err != nil {
		t.Fatalf("AvailableModels: %v", err)
	}
	if len(models) != 1 || models[0].ID != "fugu-custom" || models[0].Label != "Fugu Custom" {
		t.Fatalf("models = %#v", models)
	}
	want := []domain.Effort{domain.EffortHigh, domain.EffortXHigh, domain.Effort("Future-Fugu")}
	if strings.Join(effortStrings(models[0].Efforts), ",") != strings.Join(effortStrings(want), ",") {
		t.Fatalf("efforts = %#v, want %#v", models[0].Efforts, want)
	}
	if models[0].DefaultEffort != domain.EffortHigh || !models[0].Dynamic {
		t.Fatalf("metadata = %#v", models[0])
	}
}

func TestFuguAvailableModelsReturnsVisibleKnownFallback(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	models, err := NewFugu().AvailableModels(context.Background())
	if err != nil {
		t.Fatalf("AvailableModels fallback: %v", err)
	}
	if len(models) != 2 || models[0].ID != "fugu" || models[1].ID != "fugu-ultra" {
		t.Fatalf("fallback = %#v", models)
	}
	for _, model := range models {
		if model.Dynamic {
			t.Fatalf("fallback row must remain distinguishable from installed data: %#v", model)
		}
	}
}

func effortStrings(efforts []domain.Effort) []string {
	out := make([]string, len(efforts))
	for i, effort := range efforts {
		out[i] = string(effort)
	}
	return out
}
