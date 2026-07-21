package cli

import (
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
	if !strings.Contains(out, "ASSEMBLED WORKER PROMPT") {
		t.Fatalf("output missing assembled prompt:\n%s", out)
	}
}

func TestRolePrompt_UnknownRoleIsUsageError(t *testing.T) {
	setConfigEnv(t)
	_, _, err := executeCLI(t, Deps{}, "role", "prompt", "demo", "prime")
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
