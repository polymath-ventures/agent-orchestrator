package cli

import (
	"net/http"
	"strings"
	"testing"
)

// configExportResponse is a project-get envelope whose config includes fields
// the typed CLI mirror does not carry (maxLiveWorkers, workerMix), so tests can
// assert export is lossless.
const configExportResponse = `{"status":"ok","project":{"id":"demo","path":"/repo/demo","config":{"sessionPrefix":"demo","defaultBranch":"main","maxLiveWorkers":4,"workerMix":{"ratios":{"codex":2}}}}}`

func TestProjectConfigExport_CanonicalJSON(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := projectServer(t, http.StatusOK, configExportResponse)
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, Deps{
		ProcessAlive: func(int) bool { return true },
	}, "project", "config", "export", "demo")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	if capture.method != http.MethodGet || capture.path != "/api/v1/projects/demo" {
		t.Fatalf("request = %s %s, want GET /api/v1/projects/demo", capture.method, capture.path)
	}
	// Canonical: sorted keys, indented, trailing newline.
	want := "{\n" +
		"  \"defaultBranch\": \"main\",\n" +
		"  \"maxLiveWorkers\": 4,\n" +
		"  \"sessionPrefix\": \"demo\",\n" +
		"  \"workerMix\": {\n" +
		"    \"ratios\": {\n" +
		"      \"codex\": 2\n" +
		"    }\n" +
		"  }\n" +
		"}\n"
	if out != want {
		t.Fatalf("export output:\n%q\nwant:\n%q", out, want)
	}
}

func TestProjectConfigExport_ByteStable(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, _ := projectServer(t, http.StatusOK, configExportResponse)
	writeRunFileFor(t, cfg, srv)

	run := func() string {
		out, errOut, err := executeCLI(t, Deps{
			ProcessAlive: func(int) bool { return true },
		}, "project", "config", "export", "demo")
		if err != nil {
			t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
		}
		return out
	}
	if a, b := run(), run(); a != b {
		t.Fatalf("export not byte-stable across runs:\nA=%q\nB=%q", a, b)
	}
}

func TestProjectConfigExport_MissingArgIsUsageError(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := projectServer(t, http.StatusOK, configExportResponse)
	writeRunFileFor(t, cfg, srv)

	_, _, err := executeCLI(t, Deps{
		ProcessAlive: func(int) bool { return true },
	}, "project", "config", "export")
	if err == nil {
		t.Fatal("expected error for missing project argument")
	}
	if got := ExitCode(err); got != 2 {
		t.Fatalf("exit code = %d, want 2 (usage error)", got)
	}
	if capture.method != "" {
		t.Fatalf("made a daemon call (%s %s) on usage error; want none", capture.method, capture.path)
	}
}

func TestProjectConfigExport_DaemonErrorExitsNonzero(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, _ := projectServer(t, http.StatusNotFound, `{"message":"project not found","code":"not_found"}`)
	writeRunFileFor(t, cfg, srv)

	_, _, err := executeCLI(t, Deps{
		ProcessAlive: func(int) bool { return true },
	}, "project", "config", "export", "ghost")
	if err == nil {
		t.Fatal("expected error for unknown project")
	}
	if got := ExitCode(err); got != 1 {
		t.Fatalf("exit code = %d, want 1 (daemon failure)", got)
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("error = %v, want it to surface the daemon message", err)
	}
}
