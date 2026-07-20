package codex

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// fuguSessionHookFlags mirrors sessionHookFlags but for the fugu hook token.
// Asserted literally for the same reason: Codex parses these values as TOML,
// and a fugu session whose hooks say "codex" reports its activity onto the
// wrong harness.
func fuguSessionHookFlags() []string {
	return []string{
		"-c", `hooks.SessionStart=[{hooks=[{type="command",command="ao hooks codex-fugu session-start",timeout=5}]}]`,
		"-c", `hooks.UserPromptSubmit=[{hooks=[{type="command",command="ao hooks codex-fugu user-prompt-submit",timeout=5}]}]`,
		"-c", `hooks.PermissionRequest=[{hooks=[{type="command",command="ao hooks codex-fugu permission-request",timeout=5}]}]`,
		"-c", `hooks.Stop=[{hooks=[{type="command",command="ao hooks codex-fugu stop",timeout=5}]}]`,
	}
}

// writeFakeBinary drops an executable shell script at dir/name that writes the
// given output and exits with code. Used to script login-status probes.
func writeFakeBinary(t *testing.T, dir, name, script string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil { //nolint:gosec // test fixture must be executable
		t.Fatal(err)
	}
	return path
}

// The command-building tests preset resolvedBinary, so they don't exercise that
// NewFugu actually resolves the codex-fugu binary. This one puts both codex and
// codex-fugu on PATH and requires ResolveBinary to select fugu's — it would fail
// if the binary-name parameterization regressed to plain codex.
func TestFuguResolveBinarySelectsCodexFuguOverCodex(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("scripted binary fakes are Unix-specific")
	}
	dir := t.TempDir()
	codexPath := writeFakeBinary(t, dir, "codex", `exit 0`)
	fuguPath := writeFakeBinary(t, dir, "codex-fugu", `exit 0`)
	t.Setenv("PATH", dir)

	got, err := NewFugu().ResolveBinary(context.Background())
	if err != nil {
		t.Fatalf("ResolveBinary: %v", err)
	}
	if got != fuguPath {
		t.Fatalf("ResolveBinary = %q, want %q (must not resolve plain codex at %q)", got, fuguPath, codexPath)
	}
}

func TestFuguManifestReportsItsOwnIdentity(t *testing.T) {
	got := NewFugu().Manifest()
	if got.ID != "codex-fugu" {
		t.Fatalf("manifest ID = %q, want %q", got.ID, "codex-fugu")
	}
	if got.Name != "Codex Fugu" {
		t.Fatalf("manifest Name = %q, want %q", got.Name, "Codex Fugu")
	}
}

// The parameterization must leave plain Codex untouched — the empty-string
// fallback is the whole reason this is one adapter and not two.
func TestCodexManifestUnchangedByParameterization(t *testing.T) {
	got := New().Manifest()
	if got.ID != "codex" {
		t.Fatalf("manifest ID = %q, want %q", got.ID, "codex")
	}
	if got.Name != "Codex" {
		t.Fatalf("manifest Name = %q, want %q", got.Name, "Codex")
	}
}

func TestFuguLaunchCommandPlacesWrapperFlagFirstAndUsesFuguHookToken(t *testing.T) {
	plugin := NewFugu()
	plugin.resolvedBinary = "codex-fugu"
	workspace := canonicalTempDir(t)

	cmd, err := plugin.GetLaunchCommand(context.Background(), ports.LaunchConfig{
		Permissions:   ports.PermissionModeBypassPermissions,
		Prompt:        "-fix this",
		SystemPrompt:  "inline wins",
		WorkspacePath: workspace,
	})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		"codex-fugu",
		"--no-update",
		"-c", "check_for_update_on_startup=false",
		"-c", "notice.hide_rate_limit_model_nudge=true",
		"--dangerously-bypass-hook-trust",
		"--dangerously-bypass-approvals-and-sandbox",
	}
	want = append(want, fuguSessionHookFlags()...)
	if runtime.GOOS == "windows" {
		want = append(want, "--no-alt-screen")
	}
	want = append(want,
		"-c", `projects={`+codexTOMLConfigString(workspace)+`={trust_level="trusted"}}`,
		"-c", "developer_instructions="+codexTOMLConfigString("inline wins"),
		"--", "-fix this",
	)
	if !reflect.DeepEqual(cmd, want) {
		t.Fatalf("fugu launch cmd\nwant: %#v\n got: %#v", want, cmd)
	}
}

// The wrapper rejects --no-update behind a subcommand, so position matters:
// it must precede `resume`, not follow it.
func TestFuguRestoreCommandPlacesWrapperFlagBeforeResume(t *testing.T) {
	plugin := NewFugu()
	plugin.resolvedBinary = "codex-fugu"
	workspace := canonicalTempDir(t)

	cmd, ok, err := plugin.GetRestoreCommand(context.Background(), ports.RestoreConfig{
		Permissions: ports.PermissionModeBypassPermissions,
		Session: ports.SessionRef{
			Metadata:      map[string]string{ports.MetadataKeyAgentSessionID: "thread-123"},
			WorkspacePath: workspace,
		},
	})
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if !ok {
		t.Fatal("ok = false, want true")
	}

	want := []string{
		"codex-fugu",
		"--no-update",
		"resume",
		"-c", "check_for_update_on_startup=false",
		"-c", "notice.hide_rate_limit_model_nudge=true",
		"--dangerously-bypass-hook-trust",
		"--dangerously-bypass-approvals-and-sandbox",
	}
	want = append(want, fuguSessionHookFlags()...)
	if runtime.GOOS == "windows" {
		want = append(want, "--no-alt-screen")
	}
	want = append(want,
		"-c", `projects={`+codexTOMLConfigString(workspace)+`={trust_level="trusted"}}`,
		"thread-123",
	)
	if !reflect.DeepEqual(cmd, want) {
		t.Fatalf("fugu restore cmd\nwant: %#v\n got: %#v", want, cmd)
	}
}

func TestCodexCommandsOmitFuguWrapperFlag(t *testing.T) {
	plugin := &Plugin{resolvedBinary: "codex"}

	launch, err := plugin.GetLaunchCommand(context.Background(), ports.LaunchConfig{})
	if err != nil {
		t.Fatal(err)
	}
	restore, _, err := plugin.GetRestoreCommand(context.Background(), ports.RestoreConfig{
		Session: ports.SessionRef{
			Metadata: map[string]string{ports.MetadataKeyAgentSessionID: "thread-123"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	for name, cmd := range map[string][]string{"launch": launch, "restore": restore} {
		for _, arg := range cmd {
			if arg == "--no-update" {
				t.Fatalf("codex %s cmd %#v carries the fugu wrapper flag", name, cmd)
			}
		}
	}
}

func TestFuguAuthStatusFallsBackToSharedCodexLogin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("scripted binary fakes are Unix-specific")
	}

	const profileError = `echo "Error: --profile only applies to runtime commands and codex mcp" >&2; exit 1`

	tests := []struct {
		name       string
		fuguScript string
		codexShell string
		want       ports.AgentAuthStatus
	}{
		{
			name:       "profile error falls back to a logged-in codex",
			fuguScript: profileError,
			codexShell: `echo "Logged in as dev@example.com"`,
			want:       ports.AgentAuthStatusAuthorized,
		},
		{
			name:       "shared login is logged out",
			fuguScript: profileError,
			codexShell: `echo "Not logged in"; exit 1`,
			want:       ports.AgentAuthStatusUnauthorized,
		},
		{
			// A clean exit says nothing about login state. Reporting a broken
			// worker as healthy is worse than reporting it as unknown.
			name:       "clean exit with unrecognizable output is not authorization",
			fuguScript: profileError,
			codexShell: `echo "Usage: codex [OPTIONS] <COMMAND>"`,
			want:       ports.AgentAuthStatusUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			argsFile := filepath.Join(dir, "fugu-args")
			// Record the argv the fugu binary was invoked with, then run the
			// scripted behavior. The probe must pass --no-update first, or the
			// wrapper can block on its update prompt before printing a state.
			fugu := writeFakeBinary(t, dir, "codex-fugu", `echo "$@" > `+argsFile+`; `+tt.fuguScript)
			writeFakeBinary(t, dir, "codex", tt.codexShell)
			t.Setenv("PATH", dir)

			plugin := NewFugu()
			plugin.resolvedBinary = fugu

			got, err := plugin.AuthStatus(context.Background())
			if err != nil {
				t.Fatalf("AuthStatus err = %v, want nil", err)
			}
			if got != tt.want {
				t.Fatalf("AuthStatus = %q, want %q", got, tt.want)
			}
			gotArgs, err := os.ReadFile(argsFile)
			if err != nil {
				t.Fatalf("fugu binary was not invoked: %v", err)
			}
			if strings.TrimSpace(string(gotArgs)) != "--no-update login status" {
				t.Fatalf("fugu login probe argv = %q, want %q", strings.TrimSpace(string(gotArgs)), "--no-update login status")
			}
		})
	}
}

// The fallback is gated on the profile error specifically. An unrelated fugu
// failure must not consult Codex, and must report unknown — not unauthorized,
// which would tell an operator to log in when the binary is actually broken.
func TestFuguAuthStatusDoesNotConsultCodexOnUnrelatedFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("scripted binary fakes are Unix-specific")
	}

	dir := t.TempDir()
	sentinel := filepath.Join(dir, "codex-was-consulted")
	fugu := writeFakeBinary(t, dir, "codex-fugu", `echo "boom" >&2; exit 1`)
	// Record the consultation with a shell redirect, not `touch`: PATH is the
	// temp dir only, so an external `touch` may not resolve and the sentinel
	// would never be written even if codex ran — which would let this test pass
	// on a broken implementation.
	writeFakeBinary(t, dir, "codex", `echo consulted > `+sentinel+`; echo "Logged in as dev@example.com"`)
	t.Setenv("PATH", dir)

	plugin := NewFugu()
	plugin.resolvedBinary = fugu

	got, err := plugin.AuthStatus(context.Background())
	if err != nil {
		t.Fatalf("AuthStatus err = %v, want nil", err)
	}
	if got != ports.AgentAuthStatusUnknown {
		t.Fatalf("AuthStatus = %q, want unknown for an unrelated fugu failure", got)
	}
	if _, err := os.Stat(sentinel); err == nil {
		t.Fatal("shared codex login was consulted for a non-profile failure")
	}
}
