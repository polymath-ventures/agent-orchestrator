package claudeacp

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	acpdriver "github.com/aoagents/agent-orchestrator/backend/internal/adapters/chatdriver/acp"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func TestClaudeSessionMetaAppendsWithoutReplacingPreset(t *testing.T) {
	if got := claudeSessionMeta(acpdriver.LaunchConfig{}); got != nil {
		t.Fatalf("empty prompt metadata = %#v", got)
	}
	meta := claudeSessionMeta(acpdriver.LaunchConfig{SystemPrompt: "AO standing instructions"})
	prompt, ok := meta["systemPrompt"].(map[string]any)
	if !ok {
		t.Fatalf("systemPrompt = %#v", meta["systemPrompt"])
	}
	if prompt["type"] != "preset" || prompt["preset"] != "claude_code" || prompt["append"] != "AO standing instructions" {
		t.Fatalf("systemPrompt = %#v", prompt)
	}
}

func TestClaudeSessionModeUsesAdapterModeIDs(t *testing.T) {
	tests := map[ports.PermissionMode]string{
		ports.PermissionModeDefault:           "",
		ports.PermissionModeAcceptEdits:       "acceptEdits",
		ports.PermissionModeAuto:              "auto",
		ports.PermissionModeBypassPermissions: "bypassPermissions",
	}
	for permission, want := range tests {
		if got := claudeSessionMode(permission); got != want {
			t.Errorf("mode(%q) = %q, want %q", permission, got, want)
		}
	}
}

func TestClaudeSessionOptionsUseACPConfigIDs(t *testing.T) {
	got := claudeSessionOptions(ports.ChatTurnSettings{Model: "sonnet", Effort: "high"})
	want := []acpdriver.SessionOption{{ID: "model", Value: "sonnet"}, {ID: "effort", Value: "high"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("options = %#v, want %#v", got, want)
	}
}

func TestValidateClaudeACPExecutableRejectsWindowsCommandShims(t *testing.T) {
	tests := []struct {
		name    string
		binary  string
		goos    string
		wantErr bool
	}{
		{name: "native executable", binary: `C:\\npm\\claude.exe`, goos: "windows"},
		{name: "cmd shim", binary: `C:\\npm\\claude.cmd`, goos: "windows", wantErr: true},
		{name: "bat shim", binary: `C:\\npm\\claude.BAT`, goos: "windows", wantErr: true},
		{name: "non-Windows shim", binary: "/tmp/claude.cmd", goos: "linux"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateClaudeACPExecutable(tc.binary, tc.goos)
			if (err != nil) != tc.wantErr {
				t.Fatalf("validateClaudeACPExecutable(%q, %q) error = %v, wantErr %v", tc.binary, tc.goos, err, tc.wantErr)
			}
			if tc.wantErr && !strings.Contains(err.Error(), "native claude.exe") {
				t.Fatalf("error = %q, want actionable native executable guidance", err)
			}
		})
	}
}

func TestRuntimeCommandOverride(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("AO_CLAUDE_ACP_COMMAND", executable)
	launch, err := resolveRuntime(context.Background())
	if err != nil {
		t.Fatalf("resolveRuntime: %v", err)
	}
	if launch.command != executable || len(launch.args) != 0 {
		t.Fatalf("runtime = %#v", launch)
	}
}

func TestRuntimeDirectoryBesideFindsNonElectronInstallPrefix(t *testing.T) {
	root := t.TempDir()
	runtimeDir := filepath.Join(root, "acp-runtime")
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		t.Fatal(err)
	}

	got := runtimeDirectoryBeside(filepath.Join(root, "bin", "ao"))
	if got != runtimeDir {
		t.Fatalf("runtimeDirectoryBeside = %q, want %q", got, runtimeDir)
	}
}

func TestCheckRuntimeIncludesActionableRemediation(t *testing.T) {
	t.Setenv("AO_CLAUDE_ACP_COMMAND", "ao-definitely-missing-acp-runtime-command")
	err := CheckRuntime(context.Background())
	if err == nil {
		t.Fatal("CheckRuntime succeeded, want missing-command error")
	}
	for _, want := range []string{"rerun ops/deploy.sh", "AO_ACP_RUNTIME_DIR", "AO_CLAUDE_ACP_COMMAND"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("CheckRuntime error = %q, want remediation %q", err, want)
		}
	}
}

func TestRequireNodeVersionReportsMinimumAndDetectedVersion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}
	node := filepath.Join(t.TempDir(), "node")
	if err := os.WriteFile(node, []byte("#!/bin/sh\necho v21.9.0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := requireNodeVersion(context.Background(), node)
	if err == nil || !strings.Contains(err.Error(), "Node 22+") || !strings.Contains(err.Error(), "v21.9.0") {
		t.Fatalf("requireNodeVersion error = %v, want minimum and detected version", err)
	}
}
