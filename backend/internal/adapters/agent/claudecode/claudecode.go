// Package claudecode implements the Claude Code agent adapter.
//
// It builds the argv to launch `claude` as an interactive session inside a
// session's worktree, installs worktree-local hooks that report normalized
// session metadata (native id, title, summary) back into AO's store,
// and supports resume: GetLaunchCommand pins a unique `--session-id` so
// GetRestoreCommand can rebuild `claude --resume <uuid>`. SessionInfo reads the
// hook-captured metadata from the store — it does not parse transcripts.
// GetConfigSpec remains a no-op (no agent-specific config keys yet).
//
// Claude Code starts an interactive session by default (no -p/--print), which
// is exactly what AO wants: a live agent the user can attach to in the
// browser terminal or via `tmux attach`. The initial task prompt is passed
// as the positional argument; the orchestrator system prompt (if any) is
// appended to Claude's default system prompt so its built-in coding
// instructions are preserved.
package claudecode

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/aoagents/agent-orchestrator/backend/internal/adapters"
	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/agent/agentbase"
	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/agent/binaryutil"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	aoprocess "github.com/aoagents/agent-orchestrator/backend/internal/process"
)

const (
	// adapterID is the registry id and the value users pass to
	// `ao spawn --agent`.
	adapterID = "claude-code"
)

// claudeSessionNamespace seeds the UUIDv5 derivation used only to restore
// pre-existing sessions whose native Claude ID was not persisted. Fresh
// sessions receive a random native ID from AllocateAgentSessionID instead.
var claudeSessionNamespace = uuid.MustParse("a1f0c3d2-7b54-4e96-8a2b-0d9e1f2a3b4c")

// Plugin is the Claude Code agent adapter. It is safe for concurrent use; the
// binary path is resolved once and cached under binaryMu.
type Plugin struct {
	agentbase.Base
	binaryMu              sync.Mutex
	resolvedBinary        string
	effortFlagMu          sync.Mutex
	effortFlagChecked     bool
	effortFlagIsSupported bool
}

// New returns a ready-to-register Claude Code adapter.
func New() *Plugin {
	return &Plugin{}
}

// EmitsSubmitActivity signals that Claude Code fires a user-prompt-submit hook
// under AO's launch, so Activity.State can flip to active after a prompt is
// accepted. See ports.ActivitySignaler.
func (p *Plugin) EmitsSubmitActivity() bool { return true }

// EmitsBlockedActivity signals that Claude Code fires both pre- and post-tool
// hooks, so Activity.State can flip to blocked mid-turn on a permission dialog
// and the guarded send loop can clear it once the tool completes. Only
// claude-code (and its hook-delegators) carry this trio; see
// ports.ActivitySignaler.
func (p *Plugin) EmitsBlockedActivity() bool { return true }

// ExitDetectionMode opts Claude Code into AO's process supervisor. Claude Code's
// hooks report turn boundaries but no reliable session-end event, so an abnormal
// exit (crash, kill) that omits SessionEnd would otherwise leave the keep-alive
// terminal looking live indefinitely. Before the upstream sync this was covered
// by the fork's launch-process liveness sweep; the supervisor replaces it with a
// stronger signal (the launch id also fences stale-generation callbacks). Codex
// and Codex Fugu opt in the same way. See ports.AgentExitDetector.
func (p *Plugin) ExitDetectionMode() ports.AgentExitDetectionMode {
	return ports.AgentExitDetectionSupervisor
}

// InHarnessRenameCommand renames a running Claude Code session. This is the
// durable app-visible naming path; the -n launch flag is only an early
// accelerator, so spawn still redelivers the same AO-owned name here after
// readiness. See ports.AgentNamer.
func (p *Plugin) InHarnessRenameCommand(name string) (string, bool) {
	safe, ok := ports.DeliverableName(name)
	if !ok {
		return "", false
	}
	return "/rename " + safe, true
}

// LaunchNameArgs names the session in argv. -n is a flag, not the positional, so
// it competes with nothing: the name lands atomically with process start and the
// pane-readiness race is absent rather than mitigated. This is an optimization
// over the universal in-harness path, not a replacement for it — dropping it
// costs a race, not a name. See ports.AgentNamer.
func (p *Plugin) LaunchNameArgs(name string) []string {
	safe, ok := ports.DeliverableName(name)
	if !ok {
		return nil
	}
	return []string{"-n", safe}
}

// PromptReadinessHints waits for the Claude Code TUI to draw its composer before
// AO writes into the pane.
//
// The `-n` launch flag means a normal spawn never needs a post-start write at
// all, so these hints look redundant — they are what keeps that flag an
// optimization rather than the mechanism. Verified by disabling `-n` and
// spawning: runtime creation returns while the pane is still a bare shell, and
// the rename lands there and is lost entirely before Claude Code starts drawing.
// Without these hints the universal in-harness path could not stand on its own,
// which is precisely the dependency this change refuses to take on.
func (p *Plugin) PromptReadinessHints(ctx context.Context, _ ports.LaunchConfig) (ports.PromptReadinessHints, error) {
	if err := ctx.Err(); err != nil {
		return ports.PromptReadinessHints{}, err
	}
	return ports.PromptReadinessHints{
		InitialDelay: 500 * time.Millisecond,
		// The composer prompt glyph, and the shortcut hint drawn beside it.
		Patterns:     []string{"❯", "for shortcuts"},
		PollInterval: 200 * time.Millisecond,
		Timeout:      20 * time.Second,
		Lines:        80,
	}, nil
}

var _ adapters.Adapter = (*Plugin)(nil)
var _ ports.Agent = (*Plugin)(nil)
var _ ports.AgentSessionIDAllocator = (*Plugin)(nil)
var _ ports.AgentNamer = (*Plugin)(nil)
var _ ports.AgentPromptReadinessProvider = (*Plugin)(nil)
var _ ports.AgentAuthChecker = (*Plugin)(nil)
var _ ports.AgentModelCatalog = (*Plugin)(nil)
var _ ports.AgentModelValidator = (*Plugin)(nil)
var _ ports.AgentQuotaProber = (*Plugin)(nil)

// Manifest returns the adapter's static self-description.
func (p *Plugin) Manifest() adapters.Manifest {
	return adapters.Manifest{
		ID:          adapterID,
		Name:        "Claude Code",
		Description: "Run Claude Code worker sessions.",
		Version:     "0.0.1",
		Capabilities: []adapters.Capability{
			adapters.CapabilityAgent,
		},
	}
}

// permissionConfigEnum lists the permission modes the "permissions" config key
// accepts. It mirrors the ports.PermissionMode constants so a project's stored
// config validates against the same vocabulary the launch command maps.
var permissionConfigEnum = []string{
	string(ports.PermissionModeDefault),
	string(ports.PermissionModeAcceptEdits),
	string(ports.PermissionModeAuto),
	string(ports.PermissionModeBypassPermissions),
}

// GetConfigSpec reports the per-project agent config keys Claude Code
// understands: a model override and a starting permission mode.
func (p *Plugin) GetConfigSpec(ctx context.Context) (ports.ConfigSpec, error) {
	if err := ctx.Err(); err != nil {
		return ports.ConfigSpec{}, err
	}
	return ports.ConfigSpec{
		Fields: []ports.ConfigField{
			{
				Key:         "model",
				Type:        ports.ConfigFieldString,
				Description: "Model override passed to `claude --model` (e.g. claude-opus-4-5).",
			},
			{
				Key:         "effort",
				Type:        ports.ConfigFieldEnum,
				Description: "Per-process Claude reasoning-effort level.",
				Enum:        []string{"low", "medium", "high", "xhigh", "max"},
			},
			{
				Key:         "permissions",
				Type:        ports.ConfigFieldEnum,
				Description: "Starting permission mode.",
				Enum:        permissionConfigEnum,
			},
		},
	}, nil
}

// GetLaunchCommand builds the argv to start an interactive Claude Code
// session. Shape:
//
//	claude [--session-id <uuid>] \
//	       [--permission-mode <mode>] \
//	       [--append-system-prompt <system prompt>] \
//	       [-- <prompt>]
//
// --session-id pins Claude's native session UUID to the value Session Manager
// allocated and will persist for later resume. Direct callers that omit it get
// a fresh fallback UUID so they never collide on a recyclable AO session name.
//
// <mode> is acceptEdits, auto, or bypassPermissions. AO's "default"
// mode emits no --permission-mode flag, so Claude's TUI resolves the starting
// mode from ~/.claude/settings.json exactly as a normal launch.
//
// The prompt is passed after `--` so a prompt beginning with "-" is not
// mistaken for a flag.
func (p *Plugin) GetLaunchCommand(ctx context.Context, cfg ports.LaunchConfig) (cmd []string, err error) {
	// Defense-in-depth: the project service validates on write, but re-check
	// here so a config written by any other path can't launch a bad command.
	if err := cfg.Config.Validate(); err != nil {
		return nil, fmt.Errorf("claude-code: %w", err)
	}

	binary, err := p.claudeBinary(ctx)
	if err != nil {
		return nil, err
	}

	effort := normalizeClaudeEffort(string(cfg.Config.Effort))
	cmd = claudeCommandPrefix(binary, effort)
	agentSessionID := strings.TrimSpace(cfg.AgentSessionID)
	if agentSessionID == "" && cfg.SessionID != "" {
		agentSessionID = p.AllocateAgentSessionID()
	}
	if agentSessionID != "" {
		cmd = append(cmd, "--session-id", agentSessionID)
	}
	// Ahead of the positional separator by construction: the name is a flag, and
	// only the prompt may ever follow `--`.
	cmd = append(cmd, p.LaunchNameArgs(cfg.DisplayName)...)
	// A project's configured permissions drive the starting mode; the explicit
	// LaunchConfig.Permissions wins when set so a per-spawn override still takes
	// precedence over the stored project default.
	permissions := cfg.Permissions
	if permissions == "" {
		permissions = cfg.Config.Permissions
	}
	appendPermissionFlags(&cmd, permissions)
	appendToolFlags(&cmd, cfg.AllowedTools, cfg.DisallowedTools)

	if model := strings.TrimSpace(cfg.Config.Model); model != "" {
		cmd = append(cmd, "--model", model)
	}
	if effort != "" && p.supportsEffortFlag(ctx, binary) {
		cmd = append(cmd, "--effort", effort)
	}

	systemPrompt, err := resolveSystemPrompt(cfg)
	if err != nil {
		return nil, err
	}
	if systemPrompt != "" {
		// Append rather than replace: Claude Code's default system prompt
		// carries its tool-use and coding instructions, which we want to
		// keep. The orchestrator prompt layers on top.
		cmd = append(cmd, "--append-system-prompt", systemPrompt)
	}

	if cfg.Prompt != "" {
		cmd = append(cmd, "--", cfg.Prompt)
	}

	return cmd, nil
}

// AllocateAgentSessionID returns a unique Claude Code transcript identity for
// a fresh AO session. Session Manager owns persisting this value so restores use
// the same transcript even when AO's display/session counter has recycled.
func (p *Plugin) AllocateAgentSessionID() string {
	return uuid.NewString()
}

// PreLaunch is an optional capability the spawn engine invokes (via type
// assertion) immediately before creating the session. Claude Code shows a
// blocking "do you trust this folder?" dialog the first time it runs in any
// directory. Every AO worktree is a fresh path, so without this the
// agent would hang at that prompt with no one to answer it.
//
// An AO worktree is derived from the repo the user is already running
// AO in, so it is inherently trusted. PreLaunch records that trust in
// ~/.claude.json before launch, additively and atomically, so it cannot
// clobber a concurrently-running Claude instance's config.
func (p *Plugin) PreLaunch(ctx context.Context, cfg ports.LaunchConfig) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if cfg.WorkspacePath == "" {
		return nil
	}
	cfgPath, err := claudeConfigPath()
	if err != nil {
		return err
	}
	return ensureWorkspaceTrusted(cfgPath, cfg.WorkspacePath)
}

// GetRestoreCommand rebuilds the argv that continues an existing Claude Code
// session: `claude [--permission-mode <mode>] --resume <agentSessionId>`. It
// prefers the hook-captured native session id from
// cfg.Session.Metadata["agentSessionId"]; for sessions created before hooks
// captured it, it falls back to the deterministic UUID historical AO launches
// pinned via --session-id. ok is false only when neither is available, so the
// caller fresh-spawns. The command re-applies the permission mode (resume
// otherwise reverts to the configured default) but not the prompt/system
// prompt, which the session already carries.
func (p *Plugin) GetRestoreCommand(ctx context.Context, cfg ports.RestoreConfig) (cmd []string, ok bool, err error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}

	sessionID := strings.TrimSpace(cfg.Session.Metadata[ports.MetadataKeyAgentSessionID])
	if sessionID == "" && cfg.Session.ID != "" {
		// Explicit fallback for pre-existing rows created before AO persisted
		// native ids: their launches deterministically pinned this UUID.
		sessionID = claudeSessionUUID(cfg.Session.ID)
	}
	if sessionID == "" {
		return nil, false, nil
	}

	binary, err := p.claudeBinary(ctx)
	if err != nil {
		return nil, false, err
	}
	effort := normalizeClaudeEffort(string(cfg.Config.Effort))
	cmd = claudeCommandPrefix(binary, effort)
	appendPermissionFlags(&cmd, cfg.Permissions)
	if model := strings.TrimSpace(cfg.Config.Model); model != "" {
		cmd = append(cmd, "--model", model)
	}
	if effort != "" && p.supportsEffortFlag(ctx, binary) {
		cmd = append(cmd, "--effort", effort)
	}
	systemPrompt, err := resolveRestoreSystemPrompt(cfg)
	if err != nil {
		return nil, false, err
	}
	if systemPrompt != "" {
		// --resume rebuilds the system prompt from the current flags (it is
		// not stored in the transcript), so standing instructions must be
		// re-appended or a restored orchestrator loses its role.
		cmd = append(cmd, "--append-system-prompt", systemPrompt)
	}
	cmd = append(cmd, "--resume", sessionID)
	return cmd, true, nil
}

var claudeMaintainedModels = []ports.ModelCatalogEntry{
	{
		ID:            "fable",
		Label:         "Fable",
		Efforts:       []domain.Effort{domain.EffortLow, domain.EffortMedium, domain.EffortHigh, domain.EffortXHigh, domain.EffortMax},
		DefaultEffort: domain.EffortHigh,
		Dynamic:       true,
	},
	{
		ID:            "opus",
		Label:         "Opus",
		Efforts:       []domain.Effort{domain.EffortLow, domain.EffortMedium, domain.EffortHigh, domain.EffortXHigh, domain.EffortMax},
		DefaultEffort: domain.EffortHigh,
		Dynamic:       true,
	},
	{
		ID:            "sonnet",
		Label:         "Sonnet",
		Efforts:       []domain.Effort{domain.EffortLow, domain.EffortMedium, domain.EffortHigh, domain.EffortXHigh, domain.EffortMax},
		DefaultEffort: domain.EffortHigh,
		Dynamic:       true,
	},
	{ID: "haiku", Label: "Haiku", Dynamic: true},
}

// AvailableModels returns AO's maintained semantic Claude aliases. Discovery
// is deliberately local and static: Claude Code exposes no supported catalog
// command, and listing choices must never consume a paid model request.
func (p *Plugin) AvailableModels(ctx context.Context) ([]ports.ModelCatalogEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	models := make([]ports.ModelCatalogEntry, len(claudeMaintainedModels))
	for i, model := range claudeMaintainedModels {
		models[i] = model
		models[i].Efforts = append([]domain.Effort(nil), model.Efforts...)
	}
	return models, nil
}

// SessionInfo surfaces the normalized session metadata that the Claude Code
// hooks persisted into AO's store: the native session id, the title (the
// first user prompt), and the summary (the final assistant message). It reads
// only from session.Metadata — never from transcript files — and returns
// ok=false when none of those fields are present. Metadata is intentionally nil:
// there is no Claude-specific field callers need beyond the normalized ones.
func (p *Plugin) SessionInfo(ctx context.Context, session ports.SessionRef) (ports.SessionInfo, bool, error) {
	if err := ctx.Err(); err != nil {
		return ports.SessionInfo{}, false, err
	}
	info, ok := agentbase.StandardSessionInfo(session)
	return info, ok, nil
}

// AuthStatus checks Claude Code's local authentication state without starting a
// session.
func (p *Plugin) AuthStatus(ctx context.Context) (ports.AgentAuthStatus, error) {
	binary, err := p.claudeBinary(ctx)
	if err != nil {
		return ports.AgentAuthStatusUnknown, err
	}
	if status, ok, err := claudeLocalAuthStatus(ctx); err != nil {
		return ports.AgentAuthStatusUnknown, err
	} else if ok {
		return status, nil
	}
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	out, err := aoprocess.CommandContext(probeCtx, binary, "auth", "status").CombinedOutput()
	if probeCtx.Err() != nil {
		return ports.AgentAuthStatusUnknown, probeCtx.Err()
	}
	if status, ok := claudeAuthStatusFromOutput(out); ok {
		return status, nil
	}
	if err != nil {
		return ports.AgentAuthStatusUnauthorized, nil
	}
	return ports.AgentAuthStatusUnknown, nil
}

const (
	claudeProbeTimeout   = 45 * time.Second
	claudeProbeWaitDelay = 2 * time.Second
	claudeProbePrompt    = "Reply exactly OK. Do not use tools."
)

const claudeProbeDisallowedTools = "Task,Bash,Edit,Write,Read,Glob,Grep,WebFetch,WebSearch,TodoWrite,NotebookEdit"

// probeArgs builds the hermetic non-interactive invocation. The empty MCP
// record must include mcpServers: plain {} is rejected by Claude Code before
// it reaches the provider. MultiEdit is intentionally absent because older
// supported Claude releases reject that unknown tool name.
func (p *Plugin) probeArgs(model string) []string {
	return []string{
		"--print",
		"--output-format", "json",
		"--model", model,
		"--permission-mode", "dontAsk",
		"--no-session-persistence",
		"--setting-sources", "",
		"--strict-mcp-config",
		"--mcp-config", `{"mcpServers":{}}`,
		"--disallowedTools", claudeProbeDisallowedTools,
	}
}

// ValidateModel performs an explicit, bounded advisory model probe. Catalog
// discovery and config persistence never call this method. The probe disables
// operator settings, hooks, MCP servers, tools, and session persistence, then
// derives its verdict only from Claude Code's JSON result envelope.
func (p *Plugin) ValidateModel(ctx context.Context, model string) (ports.ModelValidationResult, error) {
	binary, err := p.claudeBinary(ctx)
	if err != nil {
		return ports.ModelValidationResult{}, err
	}
	probeCtx, cancel := context.WithTimeout(ctx, claudeProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, binary, p.probeArgs(strings.TrimSpace(model))...)
	cmd.Stdin = strings.NewReader(claudeProbePrompt)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.WaitDelay = claudeProbeWaitDelay
	configureProbeProcessGroup(cmd)
	cmdErr := cmd.Run()
	if probeCtx.Err() != nil {
		return ports.ModelValidationResult{Status: ports.ModelValidationProbeUnavailable, Message: probeCtx.Err().Error()}, nil
	}
	if envelope, ok := parseClaudeProbeEnvelope(stdout.Bytes()); ok {
		return claudeProbeVerdict(envelope), nil
	}
	diag := stderr.Bytes()
	if len(bytes.TrimSpace(diag)) == 0 {
		diag = stdout.Bytes()
	}
	message := formatClaudeProbeOutput(diag)
	if message == "" && cmdErr != nil {
		message = cmdErr.Error()
	}
	return ports.ModelValidationResult{Status: ports.ModelValidationProbeUnavailable, Message: message}, nil
}

type claudeProbeEnvelope struct {
	Type           string `json:"type"`
	IsError        bool   `json:"is_error"`
	APIErrorStatus *int   `json:"api_error_status"`
	Result         string `json:"result"`
}

// parseClaudeProbeEnvelope scans all top-level stdout objects and selects the
// last result envelope. A system prelude or bare {} must never become a false
// reachable verdict.
func parseClaudeProbeEnvelope(out []byte) (claudeProbeEnvelope, bool) {
	start := bytes.IndexByte(out, '{')
	if start < 0 {
		return claudeProbeEnvelope{}, false
	}
	decoder := json.NewDecoder(bytes.NewReader(out[start:]))
	var found claudeProbeEnvelope
	var ok bool
	for {
		var raw map[string]json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			break
		}
		if !isClaudeResultObject(raw) {
			continue
		}
		encoded, err := json.Marshal(raw)
		if err != nil {
			continue
		}
		var envelope claudeProbeEnvelope
		if json.Unmarshal(encoded, &envelope) == nil {
			found, ok = envelope, true
		}
	}
	return found, ok
}

func isClaudeResultObject(raw map[string]json.RawMessage) bool {
	if encodedType, present := raw["type"]; present {
		var objectType string
		if json.Unmarshal(encodedType, &objectType) == nil && objectType == "result" {
			return true
		}
	}
	_, hasIsError := raw["is_error"]
	return hasIsError
}

func claudeProbeVerdict(envelope claudeProbeEnvelope) ports.ModelValidationResult {
	message := strings.TrimSpace(envelope.Result)
	if !envelope.IsError {
		return ports.ModelValidationResult{Status: ports.ModelValidationReachable, Message: message}
	}
	if envelope.APIErrorStatus != nil {
		switch status := *envelope.APIErrorStatus; {
		case status == 400, status == 404, status == 422:
			return ports.ModelValidationResult{Status: ports.ModelValidationUnreachable, Message: message}
		case status >= 500, status == 429, status == 408:
			return ports.ModelValidationResult{Status: ports.ModelValidationProbeUnavailable, Message: message}
		}
	}
	return ports.ModelValidationResult{Status: ports.ModelValidationProbeUnavailable, Message: message}
}

func formatClaudeProbeOutput(out []byte) string {
	text := strings.TrimSpace(string(out))
	if text == "" {
		return ""
	}
	const maxProbeOutputRunes = 500
	runes := []rune(text)
	if len(runes) > maxProbeOutputRunes {
		text = string(runes[:maxProbeOutputRunes]) + "...[truncated]"
	}
	return text
}

func claudeModelProbeResultFromOutput(out []byte, cmdErr error) ports.ModelValidationResult {
	text := strings.TrimSpace(string(out))
	if cmdErr == nil {
		return ports.ModelValidationResult{Status: ports.ModelValidationReachable, Message: text}
	}
	if claudeOutputLooksLikeUnsupportedModel(text) {
		return ports.ModelValidationResult{Status: ports.ModelValidationUnreachable, Message: text}
	}
	return ports.ModelValidationResult{Status: ports.ModelValidationProbeUnavailable, Message: text}
}

func claudeOutputLooksLikeUnsupportedModel(text string) bool {
	lower := strings.ToLower(text)
	for _, needle := range []string{
		"unknown model",
		"invalid model",
		"unsupported model",
		"model not found",
		"model does not exist",
		"not available for model",
		"model unavailable",
	} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

func claudeAuthStatusFromOutput(out []byte) (ports.AgentAuthStatus, bool) {
	start := bytes.IndexByte(out, '{')
	end := bytes.LastIndexByte(out, '}')
	if start < 0 || end < start {
		return ports.AgentAuthStatusUnknown, false
	}
	var status struct {
		LoggedIn bool `json:"loggedIn"`
	}
	if json.Unmarshal(out[start:end+1], &status) != nil {
		return ports.AgentAuthStatusUnknown, false
	}
	if status.LoggedIn {
		return ports.AgentAuthStatusAuthorized, true
	}
	return ports.AgentAuthStatusUnauthorized, true
}

func claudeLocalAuthStatus(ctx context.Context) (ports.AgentAuthStatus, bool, error) {
	if err := ctx.Err(); err != nil {
		return ports.AgentAuthStatusUnknown, false, err
	}
	if strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")) != "" {
		return ports.AgentAuthStatusAuthorized, true, nil
	}
	cfgPath, err := claudeConfigPath()
	if err != nil {
		return ports.AgentAuthStatusUnknown, false, err
	}
	return claudeConfigAuthStatus(cfgPath)
}

func claudeConfigAuthStatus(path string) (ports.AgentAuthStatus, bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return ports.AgentAuthStatusUnknown, false, nil
	}
	if err != nil {
		return ports.AgentAuthStatusUnknown, false, err
	}
	if strings.TrimSpace(string(data)) == "" {
		return ports.AgentAuthStatusUnknown, false, nil
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return ports.AgentAuthStatusUnknown, false, err
	}
	var hasSubscription bool
	if raw := root["hasAvailableSubscription"]; len(raw) > 0 {
		_ = json.Unmarshal(raw, &hasSubscription)
	}
	var userID string
	if raw := root["userID"]; len(raw) > 0 {
		_ = json.Unmarshal(raw, &userID)
	}
	if strings.TrimSpace(userID) != "" {
		return ports.AgentAuthStatusAuthorized, true, nil
	}
	var oauthAccount map[string]any
	if raw := root["oauthAccount"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &oauthAccount); err != nil {
			return ports.AgentAuthStatusUnknown, false, err
		}
	}
	if len(oauthAccount) == 0 {
		return ports.AgentAuthStatusUnknown, false, nil
	}
	if hasSubscription {
		return ports.AgentAuthStatusAuthorized, true, nil
	}
	if accountUUID, ok := oauthAccount["accountUuid"].(string); ok && strings.TrimSpace(accountUUID) != "" {
		return ports.AgentAuthStatusAuthorized, true, nil
	}
	return ports.AgentAuthStatusUnknown, false, nil
}

// claudeSessionUUID maps an AO session id onto the deterministic Claude Code
// UUID used by historical launches. It remains only as the restore fallback for
// pre-existing rows without persisted native metadata.
func claudeSessionUUID(aoSessionID string) string {
	return uuid.NewSHA1(claudeSessionNamespace, []byte(aoSessionID)).String()
}

// resolveSystemPrompt returns the system prompt text to append, preferring
// inline instructions when AO has them.
func resolveSystemPrompt(cfg ports.LaunchConfig) (string, error) {
	if cfg.SystemPrompt != "" {
		return cfg.SystemPrompt, nil
	}
	if cfg.SystemPromptFile != "" {
		data, err := os.ReadFile(cfg.SystemPromptFile)
		if err != nil {
			return "", fmt.Errorf("claude-code: read system prompt file: %w", err)
		}
		return strings.TrimRight(string(data), "\n"), nil
	}
	return "", nil
}

func resolveRestoreSystemPrompt(cfg ports.RestoreConfig) (string, error) {
	if cfg.SystemPrompt != "" {
		return cfg.SystemPrompt, nil
	}
	if cfg.SystemPromptFile != "" {
		data, err := os.ReadFile(cfg.SystemPromptFile)
		if err != nil {
			return "", fmt.Errorf("claude-code: read system prompt file: %w", err)
		}
		return strings.TrimRight(string(data), "\n"), nil
	}
	return "", nil
}

// appendPermissionFlags maps AO's permission modes onto Claude Code's
// --permission-mode values:
//   - default            → no flag. Claude's TUI resolves the starting mode
//     from ~/.claude/settings.json (defaultMode), exactly as a normal launch.
//   - accept-edits       → --permission-mode acceptEdits (auto-accept edits +
//     safe filesystem bash; still prompts for network/system bash, MCP, web)
//   - auto               → --permission-mode auto (classifier-gated
//     auto-approval; auto-runs what a safety model deems safe)
//   - bypass-permissions → --permission-mode bypassPermissions (skip all
//     checks; equivalent to --dangerously-skip-permissions)
//
// Empty/unrecognized normalizes to default, so no flag is emitted.
func appendPermissionFlags(cmd *[]string, permissions ports.PermissionMode) {
	switch ports.NormalizePermissionMode(permissions) {
	case ports.PermissionModeDefault:
		// No flag: defer to the user's settings.json defaultMode.
	case ports.PermissionModeAcceptEdits:
		*cmd = append(*cmd, "--permission-mode", "acceptEdits")
	case ports.PermissionModeAuto:
		*cmd = append(*cmd, "--permission-mode", "auto")
	case ports.PermissionModeBypassPermissions:
		*cmd = append(*cmd, "--permission-mode", "bypassPermissions")
	}
}

// appendToolFlags emits --allowedTools / --disallowedTools for a tool-scoped
// launch. Each list is joined with commas into one value so rules that contain
// spaces (e.g. "Bash(git diff:*)") are not split into separate tool names.
// Empty lists emit nothing, so an unrestricted launch is unchanged. These rules
// only bite when the launch is off bypassPermissions, which ignores them.
func appendToolFlags(cmd *[]string, allowed, disallowed []string) {
	if len(allowed) > 0 {
		*cmd = append(*cmd, "--allowedTools", strings.Join(allowed, ","))
	}
	if len(disallowed) > 0 {
		*cmd = append(*cmd, "--disallowedTools", strings.Join(disallowed, ","))
	}
}

const claudeEffortEnvVar = "CLAUDE_CODE_EFFORT_LEVEL"

func claudeCommandPrefix(binary, effort string) []string {
	if effort == "" {
		return []string{binary}
	}
	return []string{"env", claudeEffortEnvVar + "=" + effort, binary}
}

// normalizeClaudeEffort maps AO's union effort vocabulary onto Claude Code's
// supported effort levels. Minimal is a Codex-only tier and clamps to low.
func normalizeClaudeEffort(effort string) string {
	switch normalized := strings.ToLower(strings.TrimSpace(effort)); normalized {
	case "low", "medium", "high", "xhigh", "max":
		return normalized
	case "minimal":
		return "low"
	default:
		return ""
	}
}

// supportsEffortFlag performs one cheap local capability check per plugin.
// The environment variable remains authoritative across Claude releases; the
// direct flag is emitted only when this installed CLI advertises it.
func (p *Plugin) supportsEffortFlag(ctx context.Context, binary string) bool {
	p.effortFlagMu.Lock()
	defer p.effortFlagMu.Unlock()
	if p.effortFlagChecked {
		return p.effortFlagIsSupported
	}
	helpCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(helpCtx, binary, "--help")
	cmd.WaitDelay = claudeProbeWaitDelay
	configureProbeProcessGroup(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil || helpCtx.Err() != nil {
		// A timeout/startup failure says nothing about capability. Leave the
		// cache unset so the next launch can retry against a healthy CLI.
		return false
	}
	p.effortFlagChecked = true
	p.effortFlagIsSupported = strings.Contains(string(out), "--effort")
	return p.effortFlagIsSupported
}

// claudeBinarySpec locates the claude binary: PATH first, then the native
// installer's locations, npm global, Homebrew, and the claude-managed dir.
var claudeBinarySpec = binaryutil.BinarySpec{
	Label:         "claude",
	Names:         []string{"claude"},
	WinNames:      []string{"claude.cmd", "claude.exe", "claude"},
	UnixPaths:     []string{"/usr/local/bin/claude", "/opt/homebrew/bin/claude"},
	UnixHomePaths: binaryutil.NodeManagedUnixHomePaths("claude", []string{".claude", "local", "claude"}),
	NodeManaged:   true,
	WinPaths: []binaryutil.WinPath{
		{Base: binaryutil.WinAppData, Parts: []string{"npm", "claude.cmd"}},
		{Base: binaryutil.WinAppData, Parts: []string{"npm", "claude.exe"}},
	},
}

// ResolveClaudeBinary returns the path to the claude binary, or a wrapped
// ports.ErrAgentBinaryNotFound when it is absent.
func ResolveClaudeBinary(ctx context.Context) (string, error) {
	return binaryutil.ResolveBinary(ctx, claudeBinarySpec)
}

func (p *Plugin) claudeBinary(ctx context.Context) (string, error) {
	p.binaryMu.Lock()
	defer p.binaryMu.Unlock()

	if p.resolvedBinary != "" {
		return p.resolvedBinary, nil
	}

	binary, err := ResolveClaudeBinary(ctx)
	if err != nil {
		return "", err
	}
	p.resolvedBinary = binary
	return binary, nil
}

// claudeConfigPath returns the path to Claude Code's global config file,
// ~/.claude.json.
func claudeConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("claude-code: resolve home directory: %w", err)
	}
	return filepath.Join(home, ".claude.json"), nil
}

// ensureWorkspaceTrusted records workspacePath as trusted in Claude Code's
// config so the interactive trust dialog does not block a spawned session.
//
// It is additive and concurrency-safe: it reads the existing config, sets
// only projects[workspacePath].hasTrustDialogAccepted = true (preserving the
// rest of the entry and every other project), and writes back via a
// temp-file + atomic rename. If the path is already trusted, it makes no
// write at all. A missing config file is treated as an empty one.
// claudeTrustMu serializes ensureWorkspaceTrusted within the process. Concurrent
// spawns to different workspaces otherwise read the same ~/.claude.json snapshot
// and the last rename drops the other's trust entry.
var claudeTrustMu sync.Mutex

func ensureWorkspaceTrusted(configPath, workspacePath string) error {
	claudeTrustMu.Lock()
	defer claudeTrustMu.Unlock()

	root := map[string]any{}
	data, err := os.ReadFile(configPath)
	switch {
	case err == nil:
		if len(data) > 0 {
			if err := json.Unmarshal(data, &root); err != nil {
				return fmt.Errorf("claude-code: parse %s: %w", configPath, err)
			}
		}
	case os.IsNotExist(err):
		// Treat as empty config; we'll create it.
	default:
		return fmt.Errorf("claude-code: read %s: %w", configPath, err)
	}

	projects, _ := root["projects"].(map[string]any)
	if projects == nil {
		projects = map[string]any{}
		root["projects"] = projects
	}

	entry, _ := projects[workspacePath].(map[string]any)
	if entry == nil {
		entry = map[string]any{}
		projects[workspacePath] = entry
	}

	if trusted, ok := entry["hasTrustDialogAccepted"].(bool); ok && trusted {
		// Already trusted — no write needed, so no race window at all.
		return nil
	}
	entry["hasTrustDialogAccepted"] = true

	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return fmt.Errorf("claude-code: encode %s: %w", configPath, err)
	}

	// Atomic write: temp file in the same directory, then rename. Matches
	// how Claude Code itself updates this file, so concurrent updates are
	// last-writer-wins rather than corrupting.
	dir := filepath.Dir(configPath)
	tmp, err := os.CreateTemp(dir, ".claude.json.tmp-*")
	if err != nil {
		return fmt.Errorf("claude-code: create temp config: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op once renamed

	if _, err := tmp.Write(out); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("claude-code: write temp config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("claude-code: close temp config: %w", err)
	}
	if err := os.Rename(tmpName, configPath); err != nil {
		return fmt.Errorf("claude-code: replace config: %w", err)
	}
	return nil
}
