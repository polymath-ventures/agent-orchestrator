package cli

import (
	"net/http"
	"strings"
	"testing"
)

const diffLiveConfig = `{"defaultBranch":"main","sessionPrefix":"demo","maxLiveWorkers":3}`

func TestProjectConfigDiff_MatchingExitsZero(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := startConfigRoundTripServer(t, diffLiveConfig, http.StatusOK)
	writeRunFileFor(t, cfg, srv)

	spec := writeSpecFile(t, `{"defaultBranch":"main"}`)

	_, errOut, err := executeCLI(t, Deps{
		ProcessAlive: func(int) bool { return true },
	}, "project", "config", "diff", "demo", spec)
	if err != nil {
		t.Fatalf("unexpected error on matching diff: %v\nstderr=%s", err, errOut)
	}
	if capture.putCalled {
		t.Fatal("diff must never PUT")
	}
}

func TestProjectConfigDiff_DriftExitsNonzeroAndNamesFields(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := startConfigRoundTripServer(t, diffLiveConfig, http.StatusOK)
	writeRunFileFor(t, cfg, srv)

	spec := writeSpecFile(t, `{"defaultBranch":"release","maxLiveWorkers":5}`)

	out, _, err := executeCLI(t, Deps{
		ProcessAlive: func(int) bool { return true },
	}, "project", "config", "diff", "demo", spec)
	if err == nil {
		t.Fatal("expected nonzero exit on drift")
	}
	if got := ExitCode(err); got == 0 {
		t.Fatalf("exit code = %d, want nonzero", got)
	}
	if capture.putCalled {
		t.Fatal("diff must never PUT")
	}
	// Each drifted field named with spec vs live values.
	for _, want := range []string{"defaultBranch", "release", "main", "maxLiveWorkers", "5", "3"} {
		if !strings.Contains(out, want) {
			t.Fatalf("diff output %q missing %q", out, want)
		}
	}
	// Unnamed matching field should not appear as drift.
	if strings.Contains(out, "sessionPrefix") {
		t.Fatalf("diff output %q should not mention unnamed sessionPrefix", out)
	}
}

func TestProjectConfigDiff_IgnoresUnnamedFields(t *testing.T) {
	cfg := setConfigEnv(t)
	// Live sessionPrefix differs from nothing in spec — spec only names defaultBranch.
	srv, capture := startConfigRoundTripServer(t, diffLiveConfig, http.StatusOK)
	writeRunFileFor(t, cfg, srv)

	spec := writeSpecFile(t, `{"defaultBranch":"main"}`)

	_, _, err := executeCLI(t, Deps{
		ProcessAlive: func(int) bool { return true },
	}, "project", "config", "diff", "demo", spec)
	if err != nil {
		t.Fatalf("unexpected drift: %v", err)
	}
	if capture.putCalled {
		t.Fatal("diff must never PUT")
	}
}

func TestProjectConfigDiff_RedactsEnvValues(t *testing.T) {
	cfg := setConfigEnv(t)
	// Live and spec both carry an env secret that differs → env drifts.
	srv, capture := startConfigRoundTripServer(t, `{"env":{"TOKEN":"live-secret"}}`, http.StatusOK)
	writeRunFileFor(t, cfg, srv)

	spec := writeSpecFile(t, `{"env":{"TOKEN":"spec-secret"}}`)

	out, _, err := executeCLI(t, Deps{
		ProcessAlive: func(int) bool { return true },
	}, "project", "config", "diff", "demo", spec)
	if err == nil {
		t.Fatal("expected drift on env")
	}
	if capture.putCalled {
		t.Fatal("diff must never PUT")
	}
	if strings.Contains(out, "live-secret") || strings.Contains(out, "spec-secret") {
		t.Fatalf("diff leaked env secret values into output: %q", out)
	}
	if !strings.Contains(out, "<redacted>") {
		t.Fatalf("diff should redact the env field value: %q", out)
	}
	if !strings.Contains(out, "env") {
		t.Fatalf("diff should still name the drifted env field: %q", out)
	}
}
