package cli

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestRolePrompt_PrintsAssembledPrompt(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := projectServer(t, http.StatusOK, `{"role":"worker","prompt":"ASSEMBLED WORKER PROMPT"}`)
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, Deps{
		ProcessAlive: func(int) bool { return true },
	}, "role", "prompt", "demo", "worker")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	if capture.method != http.MethodGet || capture.path != "/api/v1/projects/demo/roles/worker/prompt" {
		t.Fatalf("request = %s %s, want GET /api/v1/projects/demo/roles/worker/prompt", capture.method, capture.path)
	}
	if out != "ASSEMBLED WORKER PROMPT\n" {
		t.Fatalf("output = %q, want exact legacy prompt output", out)
	}
}

func TestRolePrompt_PrintsConfiguredWorkerTaskTemplateSeparately(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, _ := projectServer(t, http.StatusOK, `{"role":"worker","prompt":"ASSEMBLED WORKER PROMPT","taskPromptTemplate":"/address-issue {issue}","taskPromptSource":"project"}`)
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "role", "prompt", "demo", "worker")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	want := "Worker task prompt template (project):\n/address-issue {issue}\n\nSystem prompt:\nASSEMBLED WORKER PROMPT\n"
	if out != want {
		t.Fatalf("output = %q, want %q", out, want)
	}
}

func TestRolePrompt_JSONPreservesTaskTemplateMetadata(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, _ := projectServer(t, http.StatusOK, `{"role":"worker","prompt":"SYSTEM","taskPromptTemplate":"/address-issue {issue}","taskPromptSource":"global"}`)
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "role", "prompt", "demo", "worker", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	var got rolePromptResult
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode output: %v; output=%s", err, out)
	}
	if got.TaskPromptTemplate != "/address-issue {issue}" || got.TaskPromptSource != "global" || got.Prompt != "SYSTEM" {
		t.Fatalf("result = %+v", got)
	}
}

func TestRolePrompt_PrimeIsUsageError(t *testing.T) {
	setConfigEnv(t)
	_, _, err := executeCLI(t, Deps{}, "role", "prompt", "demo", "prime")
	if err == nil {
		t.Fatal("expected usage error for project-scoped prime prompt")
	}
	if got := ExitCode(err); got != 2 {
		t.Fatalf("exit code = %d, want 2", got)
	}
	if !strings.Contains(err.Error(), "ao prime prompt") {
		t.Fatalf("error missing fleet prime guidance: %v", err)
	}
}

func TestRolePrompt_UnknownRoleIsUsageError(t *testing.T) {
	setConfigEnv(t)
	_, _, err := executeCLI(t, Deps{}, "role", "prompt", "demo", "unknown")
	if err == nil {
		t.Fatal("expected usage error for unknown role")
	}
	if got := ExitCode(err); got != 2 {
		t.Fatalf("exit code = %d, want 2", got)
	}
}

func TestRolePrompt_MissingArgsIsUsageError(t *testing.T) {
	setConfigEnv(t)
	_, _, err := executeCLI(t, Deps{}, "role", "prompt", "demo")
	if err == nil {
		t.Fatal("expected usage error for missing role arg")
	}
	if got := ExitCode(err); got != 2 {
		t.Fatalf("exit code = %d, want 2", got)
	}
}
