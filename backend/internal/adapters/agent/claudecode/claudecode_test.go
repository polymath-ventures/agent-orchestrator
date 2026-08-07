package claudecode

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/agent/hooksjson"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// Claude Code must opt into AO's process supervisor so an abnormal agent exit
// (no SessionEnd) is still detected on a keep-alive terminal. The supervisor
// also stamps the launch id that fences stale-generation callbacks. See GH #220.
func TestExitDetectionModeIsSupervisor(t *testing.T) {
	p := &Plugin{}
	if got := p.ExitDetectionMode(); got != ports.AgentExitDetectionSupervisor {
		t.Fatalf("ExitDetectionMode() = %q, want %q", got, ports.AgentExitDetectionSupervisor)
	}
}

func TestAvailableModelsReturnsMaintainedAliasesWithoutRunningClaude(t *testing.T) {
	p := &Plugin{resolvedBinary: filepath.Join(t.TempDir(), "must-not-run")}

	models, err := p.AvailableModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []ports.ModelCatalogEntry{
		{ID: "fable", Label: "Fable", Efforts: []domain.Effort{domain.EffortLow, domain.EffortMedium, domain.EffortHigh, domain.EffortXHigh, domain.EffortMax}, DefaultEffort: domain.EffortHigh, Dynamic: true},
		{ID: "opus", Label: "Opus", Efforts: []domain.Effort{domain.EffortLow, domain.EffortMedium, domain.EffortHigh, domain.EffortXHigh, domain.EffortMax}, DefaultEffort: domain.EffortHigh, Dynamic: true},
		{ID: "sonnet", Label: "Sonnet", Efforts: []domain.Effort{domain.EffortLow, domain.EffortMedium, domain.EffortHigh, domain.EffortXHigh, domain.EffortMax}, DefaultEffort: domain.EffortHigh, Dynamic: true},
		{ID: "haiku", Label: "Haiku", Dynamic: true},
	}
	if !reflect.DeepEqual(models, want) {
		t.Fatalf("models\nwant: %#v\n got: %#v", want, models)
	}
}

func TestGetLaunchCommandPassesEffortPerProcessAndUsesSupportedFlag(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake is Unix-specific")
	}
	bin := writeFakeClaudeScript(t, `#!/bin/sh
if [ "$1" = "--help" ]; then
  echo 'Usage: claude [--effort <level>]'
  exit 0
fi
exit 99
`)

	cmd, err := (&Plugin{resolvedBinary: bin}).GetLaunchCommand(context.Background(), ports.LaunchConfig{
		Config: ports.AgentConfig{Model: "opus", Effort: domain.EffortHigh},
		Prompt: "go",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"env", "CLAUDE_CODE_EFFORT_LEVEL=high", bin, "--model", "opus", "--effort", "high", "--", "go"}
	if !reflect.DeepEqual(cmd, want) {
		t.Fatalf("command\nwant: %#v\n got: %#v", want, cmd)
	}
}

func TestGetLaunchCommandUsesEffortEnvironmentWhenFlagUnsupported(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake is Unix-specific")
	}
	callsFile := filepath.Join(t.TempDir(), "help-calls")
	t.Setenv("AO_CLAUDE_HELP_CALLS", callsFile)
	bin := writeFakeClaudeScript(t, `#!/bin/sh
calls=0
if [ -f "$AO_CLAUDE_HELP_CALLS" ]; then calls=$(cat "$AO_CLAUDE_HELP_CALLS"); fi
printf '%s' "$((calls + 1))" > "$AO_CLAUDE_HELP_CALLS"
echo 'Usage: claude [options]'
`)

	plugin := &Plugin{resolvedBinary: bin}
	config := ports.LaunchConfig{
		Config: ports.AgentConfig{Effort: domain.EffortMax},
	}
	cmd, err := plugin.GetLaunchCommand(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"env", "CLAUDE_CODE_EFFORT_LEVEL=max", bin}
	if !reflect.DeepEqual(cmd, want) {
		t.Fatalf("command\nwant: %#v\n got: %#v", want, cmd)
	}
	if _, err := plugin.GetLaunchCommand(context.Background(), config); err != nil {
		t.Fatal(err)
	}
	calls, err := os.ReadFile(callsFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(calls) != "1" {
		t.Fatalf("successful negative help result was not cached: calls = %q", calls)
	}
}

func TestEffortFlagCapabilityRetriesTransientFailureThenCachesSuccess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake is Unix-specific")
	}
	callsFile := filepath.Join(t.TempDir(), "help-calls")
	t.Setenv("AO_CLAUDE_HELP_CALLS", callsFile)
	bin := writeFakeClaudeScript(t, `#!/bin/sh
calls=0
if [ -f "$AO_CLAUDE_HELP_CALLS" ]; then calls=$(cat "$AO_CLAUDE_HELP_CALLS"); fi
calls=$((calls + 1))
printf '%s' "$calls" > "$AO_CLAUDE_HELP_CALLS"
if [ "$calls" -eq 1 ]; then exit 1; fi
echo 'Usage: claude [--effort <level>]'
`)
	plugin := &Plugin{resolvedBinary: bin}
	config := ports.LaunchConfig{Config: ports.AgentConfig{Effort: domain.EffortHigh}}

	first, err := plugin.GetLaunchCommand(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	if contains(first, "--effort") {
		t.Fatalf("first command %#v used flag after failed capability check", first)
	}
	second, err := plugin.GetLaunchCommand(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	if !containsSubsequence(second, []string{"--effort", "high"}) {
		t.Fatalf("second command %#v did not retry and discover --effort", second)
	}
	third, err := plugin.GetLaunchCommand(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	if !containsSubsequence(third, []string{"--effort", "high"}) {
		t.Fatalf("third command %#v lost cached support", third)
	}
	calls, err := os.ReadFile(callsFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(calls) != "2" {
		t.Fatalf("help calls = %q, want 2 (one failure, one cached success)", calls)
	}
}

func TestGetRestoreCommandPassesEffortPerProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake is Unix-specific")
	}
	bin := writeFakeClaudeScript(t, `#!/bin/sh
echo 'Usage: claude [--effort <level>]'
`)
	cmd, ok, err := (&Plugin{resolvedBinary: bin}).GetRestoreCommand(context.Background(), ports.RestoreConfig{
		Config: ports.AgentConfig{Model: "sonnet", Effort: domain.EffortMedium},
		Session: ports.SessionRef{Metadata: map[string]string{
			ports.MetadataKeyAgentSessionID: "claude-native-1",
		}},
	})
	if err != nil || !ok {
		t.Fatalf("restore = (ok=%v, err=%v), want ok", ok, err)
	}
	want := []string{"env", "CLAUDE_CODE_EFFORT_LEVEL=medium", bin, "--model", "sonnet", "--effort", "medium", "--resume", "claude-native-1"}
	if !reflect.DeepEqual(cmd, want) {
		t.Fatalf("command\nwant: %#v\n got: %#v", want, cmd)
	}
}

func TestNativeConversationIDUsesTheSameClaudeUUIDAcrossInterfaces(t *testing.T) {
	p := &Plugin{}
	tuiID, ok, err := p.NativeConversationID(context.Background(), ports.SessionRef{
		ID: "ao-session-1", Metadata: map[string]string{},
	}, domain.SessionModeTUI, "")
	if err != nil || !ok || tuiID != claudeSessionUUID("ao-session-1") {
		t.Fatalf("TUI native id = %q ok=%v err=%v", tuiID, ok, err)
	}
	chatID, ok, err := p.NativeConversationID(context.Background(), ports.SessionRef{
		ID: "ao-session-1", Metadata: map[string]string{ports.MetadataKeyAgentSessionID: "stale"},
	}, domain.SessionModeChat, tuiID)
	if err != nil || !ok || chatID != tuiID {
		t.Fatalf("Chat native id = %q ok=%v err=%v", chatID, ok, err)
	}
}

func TestNativeConversationExistsRequiresPersistedClaudeTranscript(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)
	p := &Plugin{}
	id := claudeSessionUUID("ao-session-1")

	exists, err := p.NativeConversationExists(context.Background(), ports.SessionRef{}, id, nil)
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("reserved id without a transcript reported as persisted")
	}

	projectDir := filepath.Join(configDir, "projects", "-tmp-worktree")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, id+".jsonl"), []byte("{\"type\":\"user\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	exists, err = p.NativeConversationExists(context.Background(), ports.SessionRef{}, id, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("persisted transcript was not found")
	}

	// Project/session env is the environment Claude itself receives and must win
	// over the daemon's ambient configuration directory.
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	exists, err = p.NativeConversationExists(context.Background(), ports.SessionRef{}, id,
		map[string]string{"CLAUDE_CONFIG_DIR": configDir})
	if err != nil || !exists {
		t.Fatalf("session env transcript lookup: exists=%v err=%v", exists, err)
	}
}

func TestGetLaunchCommandBypassWithPrompt(t *testing.T) {
	p := &Plugin{resolvedBinary: "claude"}

	cmd, err := p.GetLaunchCommand(context.Background(), ports.LaunchConfig{
		Permissions: ports.PermissionModeBypassPermissions,
		Prompt:      "-add a health check",
	})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		"claude",
		"--permission-mode", "bypassPermissions",
		"--", "-add a health check",
	}
	if !reflect.DeepEqual(cmd, want) {
		t.Fatalf("unexpected command\nwant: %#v\n got: %#v", want, cmd)
	}
}

func TestGetLaunchCommandMapsPermissionModes(t *testing.T) {
	tests := []struct {
		name        string
		permission  ports.PermissionMode
		want        []string
		notExpected string
	}{
		{"default omits flag (defers to settings.json)", ports.PermissionModeDefault, nil, "--permission-mode"},
		{"accept-edits", ports.PermissionModeAcceptEdits, []string{"--permission-mode", "acceptEdits"}, ""},
		{"auto", ports.PermissionModeAuto, []string{"--permission-mode", "auto"}, ""},
		{"bypass-permissions", ports.PermissionModeBypassPermissions, []string{"--permission-mode", "bypassPermissions"}, ""},
		{"empty omits permission flags", "", nil, "--permission-mode"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Plugin{resolvedBinary: "claude"}
			cmd, err := p.GetLaunchCommand(context.Background(), ports.LaunchConfig{
				Permissions: tt.permission,
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(tt.want) > 0 && !containsSubsequence(cmd, tt.want) {
				t.Fatalf("command %#v does not contain %#v", cmd, tt.want)
			}
			if tt.notExpected != "" && contains(cmd, tt.notExpected) {
				t.Fatalf("command %#v unexpectedly contains %q", cmd, tt.notExpected)
			}
		})
	}
}

func TestGetLaunchCommandAppendsSystemPromptFromFile(t *testing.T) {
	dir := t.TempDir()
	promptFile := filepath.Join(dir, "system.md")
	if err := os.WriteFile(promptFile, []byte("You are an orchestrator.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	p := &Plugin{resolvedBinary: "claude"}
	cmd, err := p.GetLaunchCommand(context.Background(), ports.LaunchConfig{
		SystemPromptFile: promptFile,
		Prompt:           "do the thing",
	})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		"claude",
		"--append-system-prompt", "You are an orchestrator.",
		"--", "do the thing",
	}
	if !reflect.DeepEqual(cmd, want) {
		t.Fatalf("unexpected command\nwant: %#v\n got: %#v", want, cmd)
	}
}

func TestGetLaunchCommandInlineSystemPrompt(t *testing.T) {
	promptFile := filepath.Join(t.TempDir(), "system.md")
	if err := os.WriteFile(promptFile, []byte("file ignored\n"), 0600); err != nil {
		t.Fatal(err)
	}

	p := &Plugin{resolvedBinary: "claude"}
	cmd, err := p.GetLaunchCommand(context.Background(), ports.LaunchConfig{
		SystemPrompt:     "inline instructions",
		SystemPromptFile: promptFile,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !containsSubsequence(cmd, []string{"--append-system-prompt", "inline instructions"}) {
		t.Fatalf("command %#v does not append inline system prompt", cmd)
	}
}

func TestGetLaunchCommandMissingSystemPromptFileErrors(t *testing.T) {
	p := &Plugin{resolvedBinary: "claude"}
	_, err := p.GetLaunchCommand(context.Background(), ports.LaunchConfig{
		SystemPromptFile: filepath.Join(t.TempDir(), "does-not-exist.md"),
	})
	if err == nil {
		t.Fatal("expected error for missing system prompt file")
	}
}

func TestGetLaunchCommandInjectsSessionID(t *testing.T) {
	p := &Plugin{resolvedBinary: "claude"}
	const agentSessionID = "94a576ee-7d58-4d11-8562-aa89de0a7bd0"
	cmd, err := p.GetLaunchCommand(context.Background(), ports.LaunchConfig{
		SessionID:      "e0tt49",
		AgentSessionID: agentSessionID,
		Prompt:         "do the thing",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !containsSubsequence(cmd, []string{"--session-id", agentSessionID}) {
		t.Fatalf("command %#v missing --session-id %q", cmd, agentSessionID)
	}

	// A command without any AO session keeps the established no-session shape.
	cmd, err = p.GetLaunchCommand(context.Background(), ports.LaunchConfig{Prompt: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if contains(cmd, "--session-id") {
		t.Fatalf("command %#v unexpectedly contains --session-id", cmd)
	}
}

func TestGetLaunchCommandGivesRecycledAOSessionIDsDistinctPersistentClaudeIDs(t *testing.T) {
	p := &Plugin{resolvedBinary: "claude"}

	// These launch configs represent two persisted AO records from separate
	// daemon lifetimes. Their recyclable display/session ID is identical, but
	// each record must own a distinct Claude transcript identity.
	first, err := p.GetLaunchCommand(context.Background(), ports.LaunchConfig{SessionID: "agent-orchestrator-8"})
	if err != nil {
		t.Fatal(err)
	}
	recycled, err := p.GetLaunchCommand(context.Background(), ports.LaunchConfig{SessionID: "agent-orchestrator-8"})
	if err != nil {
		t.Fatal(err)
	}

	firstID := argumentAfter(t, first, "--session-id")
	recycledID := argumentAfter(t, recycled, "--session-id")
	if firstID == recycledID {
		t.Fatalf("recycled AO session ID received colliding Claude IDs: %q", firstID)
	}

	// Once persisted on the first AO record, the Claude ID must be used for
	// restore rather than being regenerated.
	restore, ok, err := p.GetRestoreCommand(context.Background(), ports.RestoreConfig{
		Session: ports.SessionRef{
			ID:       "agent-orchestrator-8",
			Metadata: map[string]string{ports.MetadataKeyAgentSessionID: firstID},
		},
	})
	if err != nil || !ok {
		t.Fatalf("restore = (ok=%v, err=%v), want ok", ok, err)
	}
	if got := restore[len(restore)-1]; got != firstID {
		t.Fatalf("restore ID = %q, want persisted Claude ID %q", got, firstID)
	}
}

func TestClaudeSessionUUIDDeterministicAndUnique(t *testing.T) {
	a1 := claudeSessionUUID("alpha")
	a2 := claudeSessionUUID("alpha")
	b := claudeSessionUUID("beta")
	if a1 != a2 {
		t.Fatalf("derivation not deterministic: %q != %q", a1, a2)
	}
	if a1 == b {
		t.Fatalf("distinct ids collided: both %q", a1)
	}
	if _, err := uuid.Parse(a1); err != nil {
		t.Fatalf("derived value is not a valid UUID: %q (%v)", a1, err)
	}
}

func TestGetAgentHooksInstallsClaudeHooks(t *testing.T) {
	p := &Plugin{resolvedBinary: "claude"}
	workspace := t.TempDir()
	settingsDir := filepath.Join(workspace, ".claude")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(settingsDir, "settings.local.json")
	// Pre-seed a user's own Stop hook + an unrelated setting; both must survive.
	existing := `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"my own stop hook","timeout":5}]}]},"permissions":{"defaultMode":"plan"}}`
	if err := os.WriteFile(settingsPath, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := ports.WorkspaceHookConfig{DataDir: t.TempDir(), SessionID: "sess-1", WorkspacePath: workspace}
	if err := p.GetAgentHooks(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	// A second install must not duplicate AO hook commands.
	if err := p.GetAgentHooks(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Hooks       map[string][]hooksjson.MatcherGroup `json:"hooks"`
		Permissions json.RawMessage                     `json:"permissions"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if config.Hooks == nil {
		t.Fatalf("hooks object missing: %s", data)
	}

	// Every managed command is installed exactly once under its event.
	for _, spec := range claudeManagedHooks {
		if got := countClaudeHookCommand(config.Hooks[spec.Event], spec.Command); got != 1 {
			t.Fatalf("%s command %q count = %d, want 1", spec.Event, spec.Command, got)
		}
	}
	// Existing user hook preserved.
	if countClaudeHookCommand(config.Hooks["Stop"], "my own stop hook") != 1 {
		t.Fatalf("existing Stop hook not preserved: %#v", config.Hooks["Stop"])
	}
	// Unrelated settings preserved.
	if len(config.Permissions) == 0 {
		t.Fatalf("unrelated settings clobbered: %s", data)
	}
	// SessionStart carries the required matcher; UserPromptSubmit omits it.
	if m := matcherForCommand(config.Hooks["SessionStart"], "ao hooks claude-code session-start"); m == nil || *m != "startup" {
		t.Fatalf("SessionStart matcher = %v, want startup", m)
	}
	if m := matcherForCommand(config.Hooks["UserPromptSubmit"], "ao hooks claude-code user-prompt-submit"); m != nil {
		t.Fatalf("UserPromptSubmit matcher = %v, want none", m)
	}
	// Notification and SessionEnd install with no matcher (they fire for all
	// sub-types; the handler filters on the payload).
	if m := matcherForCommand(config.Hooks["Notification"], "ao hooks claude-code notification"); m != nil {
		t.Fatalf("Notification matcher = %v, want none", m)
	}
	if m := matcherForCommand(config.Hooks["SessionEnd"], "ao hooks claude-code session-end"); m != nil {
		t.Fatalf("SessionEnd matcher = %v, want none", m)
	}
}

func TestUninstallHooksRemovesClaudeHooks(t *testing.T) {
	p := &Plugin{resolvedBinary: "claude"}
	workspace := t.TempDir()
	settingsPath := filepath.Join(workspace, ".claude", "settings.local.json")

	ctx := context.Background()
	cfg := ports.WorkspaceHookConfig{DataDir: t.TempDir(), SessionID: "sess-1", WorkspacePath: workspace}

	// Pre-seed a user's own Stop hook + an unrelated setting; both must survive.
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	existing := `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"my own stop hook","timeout":5}]}]},"permissions":{"defaultMode":"plan"}}`
	if err := os.WriteFile(settingsPath, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := p.GetAgentHooks(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	if installed, err := p.AreHooksInstalled(ctx, workspace); err != nil || !installed {
		t.Fatalf("AreHooksInstalled after install = (%v, %v), want (true, nil)", installed, err)
	}

	if err := p.UninstallHooks(ctx, workspace); err != nil {
		t.Fatal(err)
	}
	if installed, err := p.AreHooksInstalled(ctx, workspace); err != nil || installed {
		t.Fatalf("AreHooksInstalled after uninstall = (%v, %v), want (false, nil)", installed, err)
	}

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Hooks       map[string][]hooksjson.MatcherGroup `json:"hooks"`
		Permissions json.RawMessage                     `json:"permissions"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	// No managed command survives; the SessionStart/UserPromptSubmit events,
	// which held only AO hooks, are removed entirely.
	for _, spec := range claudeManagedHooks {
		if got := countClaudeHookCommand(config.Hooks[spec.Event], spec.Command); got != 0 {
			t.Fatalf("%s command %q count = %d after uninstall, want 0", spec.Event, spec.Command, got)
		}
	}
	// The user's own Stop hook and unrelated settings are preserved.
	if countClaudeHookCommand(config.Hooks["Stop"], "my own stop hook") != 1 {
		t.Fatalf("user Stop hook not preserved: %#v", config.Hooks["Stop"])
	}
	if len(config.Permissions) == 0 {
		t.Fatalf("unrelated settings clobbered: %s", data)
	}

	// Uninstall is idempotent: a second call is a clean no-op.
	if err := p.UninstallHooks(ctx, workspace); err != nil {
		t.Fatalf("second uninstall: %v", err)
	}
}

func TestUninstallHooksNoSettingsFile(t *testing.T) {
	p := &Plugin{resolvedBinary: "claude"}
	workspace := t.TempDir()
	if err := p.UninstallHooks(context.Background(), workspace); err != nil {
		t.Fatalf("uninstall with no settings file: %v", err)
	}
	if installed, err := p.AreHooksInstalled(context.Background(), workspace); err != nil || installed {
		t.Fatalf("AreHooksInstalled = (%v, %v), want (false, nil)", installed, err)
	}
}

func TestSessionInfoReadsHookMetadata(t *testing.T) {
	info, ok, err := (&Plugin{resolvedBinary: "claude"}).SessionInfo(context.Background(), ports.SessionRef{
		WorkspacePath: "/some/path",
		Metadata: map[string]string{
			ports.MetadataKeyAgentSessionID: "claude-native-1",
			ports.MetadataKeyTitle:          "Fix login redirect",
			ports.MetadataKeySummary:        "Updated the auth callback and tests.",
			"ignored":                       "not returned",
		},
	})
	if err != nil || !ok {
		t.Fatalf("SessionInfo = (ok=%v, err=%v), want ok", ok, err)
	}
	if info.AgentSessionID != "claude-native-1" {
		t.Fatalf("AgentSessionID = %q", info.AgentSessionID)
	}
	if info.Title != "Fix login redirect" {
		t.Fatalf("Title = %q", info.Title)
	}
	if info.Summary != "Updated the auth callback and tests." {
		t.Fatalf("Summary = %q", info.Summary)
	}
	if info.Metadata != nil {
		t.Fatalf("Metadata = %#v, want nil for Claude", info.Metadata)
	}
}

func TestSessionInfoFalseWhenNoHookMetadata(t *testing.T) {
	info, ok, err := (&Plugin{resolvedBinary: "claude"}).SessionInfo(context.Background(), ports.SessionRef{
		WorkspacePath: "/some/path",
		Metadata:      map[string]string{},
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if ok {
		t.Fatalf("ok = true, want false")
	}
	if !reflect.DeepEqual(info, ports.SessionInfo{}) {
		t.Fatalf("info = %#v, want zero", info)
	}
}

// countClaudeHookCommand counts how many hook entries under one event register
// the given command — used to prove no duplicate AO hooks.
func countClaudeHookCommand(groups []hooksjson.MatcherGroup, command string) int {
	count := 0
	for _, group := range groups {
		for _, hook := range group.Hooks {
			if hook.Command == command {
				count++
			}
		}
	}
	return count
}

// matcherForCommand returns the matcher on the group that registers the given
// command (nil if the group has no matcher).
func matcherForCommand(groups []hooksjson.MatcherGroup, command string) *string {
	for _, group := range groups {
		for _, hook := range group.Hooks {
			if hook.Command == command {
				return group.Matcher
			}
		}
	}
	return nil
}

func TestGetRestoreCommandReadsAgentSessionID(t *testing.T) {
	cmd, ok, err := (&Plugin{resolvedBinary: "claude"}).GetRestoreCommand(context.Background(), ports.RestoreConfig{
		Permissions: ports.PermissionModeBypassPermissions,
		Session: ports.SessionRef{
			ID:       "sess-r",
			Metadata: map[string]string{ports.MetadataKeyAgentSessionID: "claude-native-1"},
		},
	})
	if err != nil || !ok {
		t.Fatalf("restore = (ok=%v, err=%v), want ok", ok, err)
	}
	// The hook-captured native id wins over the derived fallback.
	want := []string{"claude", "--permission-mode", "bypassPermissions", "--resume", "claude-native-1"}
	if !reflect.DeepEqual(cmd, want) {
		t.Fatalf("restore cmd\nwant: %#v\n got: %#v", want, cmd)
	}
}

func TestGetRestoreCommandReappendsSystemPrompt(t *testing.T) {
	// --resume rebuilds the system prompt from flags, so standing instructions
	// (e.g. the orchestrator role) must be re-appended on restore.
	cmd, ok, err := (&Plugin{resolvedBinary: "claude"}).GetRestoreCommand(context.Background(), ports.RestoreConfig{
		Permissions:  ports.PermissionModeBypassPermissions,
		SystemPrompt: "You are an orchestrator.",
		Session: ports.SessionRef{
			ID:       "sess-r",
			Metadata: map[string]string{ports.MetadataKeyAgentSessionID: "claude-native-1"},
		},
	})
	if err != nil || !ok {
		t.Fatalf("restore = (ok=%v, err=%v), want ok", ok, err)
	}
	want := []string{"claude", "--permission-mode", "bypassPermissions", "--append-system-prompt", "You are an orchestrator.", "--resume", "claude-native-1"}
	if !reflect.DeepEqual(cmd, want) {
		t.Fatalf("restore cmd\nwant: %#v\n got: %#v", want, cmd)
	}
}

func TestGetRestoreCommandReappliesModel(t *testing.T) {
	cmd, ok, err := (&Plugin{resolvedBinary: "claude"}).GetRestoreCommand(context.Background(), ports.RestoreConfig{
		Config: ports.AgentConfig{Model: "claude-opus-4-5"},
		Session: ports.SessionRef{
			ID:       "sess-r",
			Metadata: map[string]string{ports.MetadataKeyAgentSessionID: "claude-native-1"},
		},
	})
	if err != nil || !ok {
		t.Fatalf("restore = (ok=%v, err=%v), want ok", ok, err)
	}
	want := []string{"claude", "--model", "claude-opus-4-5", "--resume", "claude-native-1"}
	if !reflect.DeepEqual(cmd, want) {
		t.Fatalf("restore cmd\nwant: %#v\n got: %#v", want, cmd)
	}
}

func TestGetRestoreCommandReappendsSystemPromptFromFile(t *testing.T) {
	promptFile := filepath.Join(t.TempDir(), "system.md")
	if err := os.WriteFile(promptFile, []byte("file instructions\n"), 0600); err != nil {
		t.Fatal(err)
	}

	cmd, ok, err := (&Plugin{resolvedBinary: "claude"}).GetRestoreCommand(context.Background(), ports.RestoreConfig{
		Permissions:      ports.PermissionModeBypassPermissions,
		SystemPrompt:     "inline wins",
		SystemPromptFile: promptFile,
		Session: ports.SessionRef{
			ID:       "sess-r",
			Metadata: map[string]string{ports.MetadataKeyAgentSessionID: "claude-native-1"},
		},
	})
	if err != nil || !ok {
		t.Fatalf("restore = (ok=%v, err=%v), want ok", ok, err)
	}
	want := []string{"claude", "--permission-mode", "bypassPermissions", "--append-system-prompt", "inline wins", "--resume", "claude-native-1"}
	if !reflect.DeepEqual(cmd, want) {
		t.Fatalf("restore cmd\nwant: %#v\n got: %#v", want, cmd)
	}
}

func TestGetRestoreCommandFallsBackToDerivedUUID(t *testing.T) {
	// No agentSessionId captured (pre-hook session) → derive deterministically
	// from the AO session id, the explicit fallback.
	cmd, ok, err := (&Plugin{resolvedBinary: "claude"}).GetRestoreCommand(context.Background(), ports.RestoreConfig{
		Permissions: ports.PermissionModeBypassPermissions,
		Session:     ports.SessionRef{ID: "sess-r"},
	})
	if err != nil || !ok {
		t.Fatalf("restore = (ok=%v, err=%v), want ok", ok, err)
	}
	want := []string{"claude", "--permission-mode", "bypassPermissions", "--resume", claudeSessionUUID("sess-r")}
	if !reflect.DeepEqual(cmd, want) {
		t.Fatalf("restore cmd\nwant: %#v\n got: %#v", want, cmd)
	}
}

func TestGetRestoreCommandFalseWithoutSessionID(t *testing.T) {
	cases := []struct {
		name string
		ref  ports.SessionRef
	}{
		{"empty ref", ports.SessionRef{}},
		{"blank agent session, no id", ports.SessionRef{Metadata: map[string]string{ports.MetadataKeyAgentSessionID: "   "}}},
		{"workspace path only", ports.SessionRef{WorkspacePath: "/some/path"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd, ok, err := (&Plugin{resolvedBinary: "claude"}).GetRestoreCommand(context.Background(),
				ports.RestoreConfig{Permissions: ports.PermissionModeBypassPermissions, Session: tc.ref})
			if err != nil || ok || cmd != nil {
				t.Fatalf("restore = (%#v, %v, %v), want (nil,false,nil)", cmd, ok, err)
			}
		})
	}
}

func TestGetLaunchCommandAppliesAgentConfig(t *testing.T) {
	p := &Plugin{resolvedBinary: "claude"}
	cmd, err := p.GetLaunchCommand(context.Background(), ports.LaunchConfig{
		Config: ports.AgentConfig{
			Model:       "claude-opus-4-5",
			Permissions: ports.PermissionModeAcceptEdits,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !containsSubsequence(cmd, []string{"--model", "claude-opus-4-5"}) {
		t.Fatalf("command %#v missing --model flag", cmd)
	}
	if !containsSubsequence(cmd, []string{"--permission-mode", "acceptEdits"}) {
		t.Fatalf("command %#v missing config-driven permission mode", cmd)
	}
}

func TestGetLaunchCommandExplicitPermissionsOverrideConfig(t *testing.T) {
	p := &Plugin{resolvedBinary: "claude"}
	cmd, err := p.GetLaunchCommand(context.Background(), ports.LaunchConfig{
		Permissions: ports.PermissionModeBypassPermissions,
		Config:      ports.AgentConfig{Permissions: ports.PermissionModeAcceptEdits},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !containsSubsequence(cmd, []string{"--permission-mode", "bypassPermissions"}) {
		t.Fatalf("explicit Permissions should win; got %#v", cmd)
	}
}

func TestGetLaunchCommandRejectsInvalidConfig(t *testing.T) {
	p := &Plugin{resolvedBinary: "claude"}
	if _, err := p.GetLaunchCommand(context.Background(), ports.LaunchConfig{
		Config: ports.AgentConfig{Permissions: "yolo"},
	}); err == nil {
		t.Fatal("expected error for invalid permission mode")
	}
}

func TestManifestID(t *testing.T) {
	if got := New().Manifest().ID; got != "claude-code" {
		t.Fatalf("manifest id = %q, want claude-code", got)
	}
}

func TestClaudeConfigAuthStatusAuthorizedWithOAuthSubscription(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude.json")
	content := `{
		"hasAvailableSubscription": true,
		"oauthAccount": {
			"accountUuid": "account-1",
			"subscriptionCreatedAt": "2026-01-01T00:00:00Z"
		}
	}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	status, ok, err := claudeConfigAuthStatus(path)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || status != ports.AgentAuthStatusAuthorized {
		t.Fatalf("status = (%q, %v), want (%q, true)", status, ok, ports.AgentAuthStatusAuthorized)
	}
}

func TestClaudeConfigAuthStatusAuthorizedWithOAuthAccount(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude.json")
	content := `{"oauthAccount":{"accountUuid":"account-1"}}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	status, ok, err := claudeConfigAuthStatus(path)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || status != ports.AgentAuthStatusAuthorized {
		t.Fatalf("status = (%q, %v), want (%q, true)", status, ok, ports.AgentAuthStatusAuthorized)
	}
}

func TestClaudeConfigAuthStatusAuthorizedWithUserID(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude.json")
	if err := os.WriteFile(path, []byte(`{"userID":"user-1"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	status, ok, err := claudeConfigAuthStatus(path)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || status != ports.AgentAuthStatusAuthorized {
		t.Fatalf("status = (%q, %v), want (%q, true)", status, ok, ports.AgentAuthStatusAuthorized)
	}
}

func TestClaudeConfigAuthStatusUnknownWithoutOAuthIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude.json")
	content := `{"oauthAccount":{}}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	status, ok, err := claudeConfigAuthStatus(path)
	if err != nil {
		t.Fatal(err)
	}
	if ok || status != ports.AgentAuthStatusUnknown {
		t.Fatalf("status = (%q, %v), want (%q, false)", status, ok, ports.AgentAuthStatusUnknown)
	}
}

func TestClaudeAuthStatusFromOutputAuthorizedWithCleanJSON(t *testing.T) {
	status, ok := claudeAuthStatusFromOutput([]byte(`{"loggedIn":true,"authMethod":"oauth_token"}`))
	if !ok || status != ports.AgentAuthStatusAuthorized {
		t.Fatalf("status = (%q, %v), want (%q, true)", status, ok, ports.AgentAuthStatusAuthorized)
	}
}

func TestClaudeAuthStatusFromOutputAuthorizedWithPrefixedWarning(t *testing.T) {
	output := []byte("warning: ignored config line\n{\"loggedIn\":true,\"authMethod\":\"oauth_token\"}\n")
	status, ok := claudeAuthStatusFromOutput(output)
	if !ok || status != ports.AgentAuthStatusAuthorized {
		t.Fatalf("status = (%q, %v), want (%q, true)", status, ok, ports.AgentAuthStatusAuthorized)
	}
}

func TestClaudeAuthStatusFromOutputUnauthorized(t *testing.T) {
	status, ok := claudeAuthStatusFromOutput([]byte(`{"loggedIn":false}`))
	if !ok || status != ports.AgentAuthStatusUnauthorized {
		t.Fatalf("status = (%q, %v), want (%q, true)", status, ok, ports.AgentAuthStatusUnauthorized)
	}
}

func TestEnsureWorkspaceTrustedCreatesEntry(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".claude.json")
	// Seed an existing config with another project + a top-level key, to
	// prove we preserve unrelated state.
	seed := `{"userID":"abc","projects":{"/existing/proj":{"hasTrustDialogAccepted":true,"lastCost":1.5}}}`
	if err := os.WriteFile(cfgPath, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}

	work := "/Users/me/.ao/worktrees/01ABC"
	if err := ensureWorkspaceTrusted(cfgPath, work); err != nil {
		t.Fatalf("ensureWorkspaceTrusted: %v", err)
	}

	root := readJSON(t, cfgPath)
	projects := root["projects"].(map[string]any)

	// New entry trusted.
	newEntry := projects[work].(map[string]any)
	if newEntry["hasTrustDialogAccepted"] != true {
		t.Fatalf("new entry not trusted: %#v", newEntry)
	}
	// Existing project preserved (including its other fields).
	existing := projects["/existing/proj"].(map[string]any)
	if existing["hasTrustDialogAccepted"] != true || existing["lastCost"].(float64) != 1.5 {
		t.Fatalf("existing project clobbered: %#v", existing)
	}
	// Top-level key preserved.
	if root["userID"] != "abc" {
		t.Fatalf("top-level key clobbered: %#v", root["userID"])
	}
}

func TestEnsureWorkspaceTrustedIsIdempotentAndNoWriteWhenAlreadyTrusted(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".claude.json")
	work := "/w"
	if err := os.WriteFile(cfgPath, []byte(`{"projects":{"/w":{"hasTrustDialogAccepted":true}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	info1, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	if err := ensureWorkspaceTrusted(cfgPath, work); err != nil {
		t.Fatalf("ensureWorkspaceTrusted: %v", err)
	}

	// Already trusted → no rewrite → mtime unchanged.
	info2, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !info1.ModTime().Equal(info2.ModTime()) {
		t.Fatal("expected no rewrite when already trusted")
	}
}

func TestEnsureWorkspaceTrustedCreatesMissingConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".claude.json") // does not exist yet
	work := "/fresh/worktree"

	if err := ensureWorkspaceTrusted(cfgPath, work); err != nil {
		t.Fatalf("ensureWorkspaceTrusted: %v", err)
	}

	root := readJSON(t, cfgPath)
	projects := root["projects"].(map[string]any)
	entry := projects[work].(map[string]any)
	if entry["hasTrustDialogAccepted"] != true {
		t.Fatalf("entry not trusted in freshly-created config: %#v", entry)
	}
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return m
}

func TestGetLaunchCommandEmitsToolAllowlist(t *testing.T) {
	p := &Plugin{resolvedBinary: "claude"}

	cmd, err := p.GetLaunchCommand(context.Background(), ports.LaunchConfig{
		AllowedTools:    []string{"Read", "Grep", "Bash(git diff:*)"},
		DisallowedTools: []string{"Edit", "Write", "Bash(git push:*)"},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Each list is one comma-joined value so a rule with spaces stays intact.
	if !containsSubsequence(cmd, []string{"--allowedTools", "Read,Grep,Bash(git diff:*)"}) {
		t.Fatalf("missing joined --allowedTools value; got %#v", cmd)
	}
	if !containsSubsequence(cmd, []string{"--disallowedTools", "Edit,Write,Bash(git push:*)"}) {
		t.Fatalf("missing joined --disallowedTools value; got %#v", cmd)
	}
}

func TestGetLaunchCommandOmitsToolFlagsWhenUnset(t *testing.T) {
	p := &Plugin{resolvedBinary: "claude"}

	cmd, err := p.GetLaunchCommand(context.Background(), ports.LaunchConfig{Prompt: "do it"})
	if err != nil {
		t.Fatal(err)
	}
	if contains(cmd, "--allowedTools") || contains(cmd, "--disallowedTools") {
		t.Fatalf("unrestricted launch should emit no tool flags; got %#v", cmd)
	}
}

func TestClaudeModelProbeResultClassifiesUnsupportedModel(t *testing.T) {
	got := claudeModelProbeResultFromOutput([]byte("error: invalid model claude-404"), errors.New("exit 1"))
	if got.Status != ports.ModelValidationUnreachable {
		t.Fatalf("status = %q, want unreachable", got.Status)
	}

	got = claudeModelProbeResultFromOutput([]byte("authentication required"), errors.New("exit 1"))
	if got.Status != ports.ModelValidationProbeUnavailable {
		t.Fatalf("status = %q, want probe-unavailable", got.Status)
	}

	got = claudeModelProbeResultFromOutput([]byte("OK"), nil)
	if got.Status != ports.ModelValidationReachable {
		t.Fatalf("status = %q, want reachable", got.Status)
	}
}

func TestValidateModelUsesHermeticJSONEnvelopeProbe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake is Unix-specific")
	}
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args.txt")
	stdinFile := filepath.Join(dir, "stdin.txt")
	t.Setenv("AO_CLAUDE_ARGS_FILE", argsFile)
	t.Setenv("AO_CLAUDE_STDIN_FILE", stdinFile)
	bin := writeFakeClaudeScript(t, `#!/bin/sh
printf '%s\n' "$@" > "$AO_CLAUDE_ARGS_FILE"
cat > "$AO_CLAUDE_STDIN_FILE"
printf '%s\n' '{"type":"result","is_error":false,"result":"OK"}'
`)

	got, err := (&Plugin{resolvedBinary: bin}).ValidateModel(context.Background(), " opus ")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != ports.ModelValidationReachable {
		t.Fatalf("status = %q, want reachable (%s)", got.Status, got.Message)
	}
	data, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Split(strings.TrimSpace(string(data)), "\n")
	for _, want := range [][]string{
		{"--print"},
		{"--model", "opus"},
		{"--output-format", "json"},
		{"--permission-mode", "dontAsk"},
		{"--setting-sources", ""},
		{"--no-session-persistence"},
		{"--strict-mcp-config"},
		{"--mcp-config", `{"mcpServers":{}}`},
		{"--disallowedTools"},
	} {
		if !containsSubsequence(args, want) {
			t.Fatalf("probe args %#v missing %#v", args, want)
		}
	}
	for _, forbidden := range []string{"{}", "MultiEdit", claudeProbePrompt} {
		if contains(args, forbidden) {
			t.Fatalf("probe args %#v must not contain %q", args, forbidden)
		}
	}
	stdin, err := os.ReadFile(stdinFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(stdin)) == "" {
		t.Fatal("probe prompt must be delivered over stdin")
	}
}

func TestValidateModelClassifiesJSONEnvelopeVerdicts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake is Unix-specific")
	}
	tests := []struct {
		name   string
		body   string
		exit   string
		status ports.ModelValidationStatus
	}{
		{name: "reachable", body: `{"type":"result","is_error":false,"result":"OK"}`, exit: "0", status: ports.ModelValidationReachable},
		{name: "system prelude ignored", body: "{}\n{\"type\":\"system\"}\n{\"type\":\"result\",\"is_error\":false,\"result\":\"OK\"}", exit: "0", status: ports.ModelValidationReachable},
		{name: "model rejected", body: `{"type":"result","is_error":true,"api_error_status":404,"result":"model not found"}`, exit: "1", status: ports.ModelValidationUnreachable},
		{name: "bad request rejected", body: `{"type":"result","is_error":true,"api_error_status":400,"result":"bad request"}`, exit: "0", status: ports.ModelValidationUnreachable},
		{name: "unprocessable rejected", body: `{"type":"result","is_error":true,"api_error_status":422,"result":"unprocessable"}`, exit: "0", status: ports.ModelValidationUnreachable},
		{name: "rate limited", body: `{"type":"result","is_error":true,"api_error_status":429,"result":"rate limited"}`, exit: "1", status: ports.ModelValidationProbeUnavailable},
		{name: "server unavailable", body: `{"type":"result","is_error":true,"api_error_status":503,"result":"unavailable"}`, exit: "1", status: ports.ModelValidationProbeUnavailable},
		{name: "authentication failure", body: `{"type":"result","is_error":true,"api_error_status":401,"result":"unauthorized"}`, exit: "0", status: ports.ModelValidationProbeUnavailable},
		{name: "no provider verdict", body: `{"type":"result","is_error":true,"result":"authentication required"}`, exit: "1", status: ports.ModelValidationProbeUnavailable},
		{name: "bare object is no verdict", body: `{}`, exit: "0", status: ports.ModelValidationProbeUnavailable},
		{name: "malformed envelope", body: `not json`, exit: "1", status: ports.ModelValidationProbeUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("AO_CLAUDE_BODY", tt.body)
			t.Setenv("AO_CLAUDE_EXIT", tt.exit)
			bin := writeFakeClaudeScript(t, `#!/bin/sh
cat >/dev/null
printf '%s\n' "$AO_CLAUDE_BODY"
exit "$AO_CLAUDE_EXIT"
`)
			got, err := (&Plugin{resolvedBinary: bin}).ValidateModel(context.Background(), "opus")
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != tt.status {
				t.Fatalf("status = %q, want %q (%s)", got.Status, tt.status, got.Message)
			}
		})
	}
}

func TestValidateModelTimeoutIsProbeUnavailable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake is Unix-specific")
	}
	bin := writeFakeClaudeScript(t, `#!/bin/sh
sleep 5
`)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	started := time.Now()
	got, err := (&Plugin{resolvedBinary: bin}).ValidateModel(ctx, "opus")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != ports.ModelValidationProbeUnavailable {
		t.Fatalf("status = %q, want probe-unavailable", got.Status)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("probe cleanup took %s, want under 1s", elapsed)
	}
}

func writeFakeClaudeScript(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil { //nolint:gosec // executable test fixture
		t.Fatal(err)
	}
	return path
}

func contains(values []string, needle string) bool {
	for _, v := range values {
		if v == needle {
			return true
		}
	}
	return false
}

func argumentAfter(t *testing.T, values []string, flag string) string {
	t.Helper()
	for i, value := range values {
		if value == flag && i+1 < len(values) {
			return values[i+1]
		}
	}
	t.Fatalf("command %#v missing value after %q", values, flag)
	return ""
}

func containsSubsequence(values, needle []string) bool {
	if len(needle) == 0 {
		return true
	}
	for start := 0; start+len(needle) <= len(values); start++ {
		ok := true
		for i, w := range needle {
			if values[start+i] != w {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}
