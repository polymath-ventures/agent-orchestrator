// Package codex implements the Codex agent adapter: launching new sessions,
// resuming hook-tracked sessions, installing workspace-local hooks, and reading
// hook-derived session info.
//
// AO-managed sessions derive native session identity and display
// metadata from Codex hooks instead of transcript/cache scans.
package codex

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/adapters"
	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/agent/agentbase"
	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/agent/binaryutil"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// Plugin is the Codex agent adapter. It is safe for concurrent use; the binary
// path is resolved once and cached under binaryMu.
//
// The manifest/binary/hook fields are the parameterization that lets one
// adapter serve both `codex` and the fork-only `codex-fugu` harness: fugu is
// the same agent behind a different executable name, so a second package would
// duplicate the launch flags, hooks, restore logic, and activity derivation and
// then drift. Every field is read through an accessor that falls back to the
// Codex default, so a zero-valued Plugin is provably still plain Codex.
type Plugin struct {
	agentbase.Base
	manifestID          string
	manifestName        string
	manifestDescription string
	binaryName          string
	hookAgentToken      string
	binaryMu            sync.Mutex
	resolvedBinary      string
}

// New returns a ready-to-register Codex adapter.
func New() *Plugin {
	return &Plugin{}
}

// NewFugu returns a Codex-compatible adapter that launches the codex-fugu
// binary while preserving Codex's flags, hooks, auth probe, and activity model.
// Fork-only: codex-fugu is a Polymath-internal binary and never ships upstream.
func NewFugu() *Plugin {
	return &Plugin{
		manifestID:          fuguAdapterID,
		manifestName:        "Codex Fugu",
		manifestDescription: "Run Codex Fugu worker sessions.",
		binaryName:          fuguAdapterID,
		hookAgentToken:      fuguAdapterID,
	}
}

// fuguAdapterID is the harness id, adapter manifest id, binary name, and hook
// token — the registry derives the harness from the manifest id, so these must
// be the same string or resolution fails silently.
const fuguAdapterID = "codex-fugu"

func (p *Plugin) adapterID() string {
	if p.manifestID != "" {
		return p.manifestID
	}
	return "codex"
}

func (p *Plugin) adapterName() string {
	if p.manifestName != "" {
		return p.manifestName
	}
	return "Codex"
}

func (p *Plugin) adapterDescription() string {
	if p.manifestDescription != "" {
		return p.manifestDescription
	}
	return "Run Codex worker sessions."
}

func (p *Plugin) binaryCommand() string {
	if p.binaryName != "" {
		return p.binaryName
	}
	return "codex"
}

func (p *Plugin) hookToken() string {
	if p.hookAgentToken != "" {
		return p.hookAgentToken
	}
	return "codex"
}

// EmitsSubmitActivity signals Codex fires a user-prompt-submit hook under AO's
// launch. See ports.ActivitySignaler.
func (p *Plugin) EmitsSubmitActivity() bool { return true }

// EmitsBlockedActivity is false: codex reports permission prompts as
// waiting_input — it installs no post-tool-use hook, so a blocked state could
// never be cleared mid-turn. confirmActive must not nudge it (an Enter could
// answer a pending decision it cannot report as blocked). See
// ports.ActivitySignaler.
func (p *Plugin) EmitsBlockedActivity() bool { return false }

var _ adapters.Adapter = (*Plugin)(nil)
var _ ports.Agent = (*Plugin)(nil)
var _ ports.AgentAuthChecker = (*Plugin)(nil)
var _ ports.AgentModelValidator = (*Plugin)(nil)
var _ ports.AgentQuotaProber = (*Plugin)(nil)

// Manifest returns the adapter's static self-description.
func (p *Plugin) Manifest() adapters.Manifest {
	return adapters.Manifest{
		ID:          p.adapterID(),
		Name:        p.adapterName(),
		Description: p.adapterDescription(),
		Version:     "0.0.1",
		Capabilities: []adapters.Capability{
			adapters.CapabilityAgent,
		},
	}
}

// GetConfigSpec reports the per-project agent config keys Codex understands.
func (p *Plugin) GetConfigSpec(ctx context.Context) (ports.ConfigSpec, error) {
	if err := ctx.Err(); err != nil {
		return ports.ConfigSpec{}, err
	}
	return ports.ConfigSpec{
		Fields: []ports.ConfigField{
			{
				Key:         "model",
				Type:        ports.ConfigFieldString,
				Description: "Model override passed to `codex --model`.",
			},
		},
	}, nil
}

// GetLaunchCommand builds the argv to start a new Codex session, applying the
// no-update-check, hook-trust bypass, and approval flags, AO's session-flag
// activity hooks, the workspace trust override, optional system-prompt
// instructions, and the initial prompt (passed after `--` so a leading "-" is
// not read as a flag).
func (p *Plugin) GetLaunchCommand(ctx context.Context, cfg ports.LaunchConfig) (cmd []string, err error) {
	binary, err := p.agentBinary(ctx)
	if err != nil {
		return nil, err
	}

	cmd = []string{binary}
	p.appendWrapperFlags(&cmd)
	appendNoUpdateCheckFlag(&cmd)
	appendHideRateLimitNudgeFlag(&cmd)
	appendHookTrustBypassFlag(&cmd)
	appendApprovalFlags(&cmd, cfg.Permissions)
	appendSessionHookFlagsFor(&cmd, p.hookToken())
	appendTerminalCompatibilityFlags(&cmd)
	appendWorkspaceTrustFlag(&cmd, cfg.WorkspacePath)
	appendModelFlag(&cmd, cfg.Config.Model)
	p.appendReasoningEffortFlag(&cmd, cfg.Config.Effort)

	if cfg.SystemPrompt != "" {
		cmd = append(cmd, "-c", "developer_instructions="+codexTOMLConfigString(cfg.SystemPrompt))
	} else if cfg.SystemPromptFile != "" {
		cmd = append(cmd, "-c", "model_instructions_file="+cfg.SystemPromptFile)
	}

	if cfg.Prompt != "" {
		cmd = append(cmd, "--", cfg.Prompt)
	}

	return cmd, nil
}

// GetRestoreCommand rebuilds the argv that continues an existing Codex
// session: `codex resume <agentSessionId>`. ok is false when the hook-derived
// native session id has not landed yet, so callers can fall back to fresh
// launch behavior.
func (p *Plugin) GetRestoreCommand(ctx context.Context, cfg ports.RestoreConfig) (cmd []string, ok bool, err error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	agentSessionID := strings.TrimSpace(cfg.Session.Metadata[ports.MetadataKeyAgentSessionID])
	if agentSessionID == "" {
		return nil, false, nil
	}

	binary, err := p.agentBinary(ctx)
	if err != nil {
		return nil, false, err
	}

	cmd = make([]string, 0, 25)
	cmd = append(cmd, binary)
	// Wrapper flags precede the subcommand: the fugu wrapper parses
	// --no-update only at top level and rejects it behind `resume`.
	p.appendWrapperFlags(&cmd)
	cmd = append(cmd, "resume")
	appendNoUpdateCheckFlag(&cmd)
	appendHideRateLimitNudgeFlag(&cmd)
	appendHookTrustBypassFlag(&cmd)
	appendApprovalFlags(&cmd, cfg.Permissions)
	appendSessionHookFlagsFor(&cmd, p.hookToken())
	appendTerminalCompatibilityFlags(&cmd)
	appendWorkspaceTrustFlag(&cmd, cfg.Session.WorkspacePath)
	appendModelFlag(&cmd, cfg.Config.Model)
	p.appendReasoningEffortFlag(&cmd, cfg.Config.Effort)
	if cfg.SystemPrompt != "" {
		cmd = append(cmd, "-c", "developer_instructions="+codexTOMLConfigString(cfg.SystemPrompt))
	} else if cfg.SystemPromptFile != "" {
		cmd = append(cmd, "-c", "model_instructions_file="+cfg.SystemPromptFile)
	}
	cmd = append(cmd, agentSessionID)
	return cmd, true, nil
}

// SessionInfo surfaces Codex hook-derived metadata. Metadata is intentionally
// nil for Codex: callers get the normalized fields directly.
func (p *Plugin) SessionInfo(ctx context.Context, session ports.SessionRef) (ports.SessionInfo, bool, error) {
	if err := ctx.Err(); err != nil {
		return ports.SessionInfo{}, false, err
	}
	info, ok := agentbase.StandardSessionInfo(session)
	return info, ok, nil
}

// fuguProfileRejection is the error codex-fugu returns for `login status`.
// The fugu wrapper has no login of its own — its credential is the shared Codex
// login — so this specific rejection, and only this one, means "ask codex".
const fuguProfileRejection = "--profile only applies"

// AuthStatus checks Codex's local login state without making a model call.
func (p *Plugin) AuthStatus(ctx context.Context) (ports.AgentAuthStatus, error) {
	binary, err := p.agentBinary(ctx)
	if err != nil {
		return ports.AgentAuthStatusUnknown, err
	}

	// The probe runs the wrapper binary directly, so it needs the same leading
	// --no-update as launch/restore — otherwise the fugu wrapper can block on its
	// update prompt before ever printing a login state.
	var pre []string
	p.appendWrapperFlags(&pre)

	status, text, failed, err := loginStatusForBinary(ctx, binary, pre...)
	if err != nil {
		return ports.AgentAuthStatusUnknown, err
	}
	if status != ports.AgentAuthStatusUnknown {
		return status, nil
	}
	if p.adapterID() == fuguAdapterID {
		if failed && strings.Contains(text, fuguProfileRejection) {
			return sharedCodexAuthStatus(ctx)
		}
		// The profile rejection is fugu's only expected failure. Any other
		// unrecognized outcome is genuinely unknown — reporting it as
		// unauthorized would tell an operator to log in when the binary is
		// actually broken. `ao doctor` is the tool for that diagnosis.
		return ports.AgentAuthStatusUnknown, nil
	}
	// Plain Codex keeps its historical heuristic: a non-zero `login status`
	// (with no recognizable text) means logged out.
	if failed {
		return ports.AgentAuthStatusUnauthorized, nil
	}
	return ports.AgentAuthStatusUnknown, nil
}

const (
	probeTimeout   = 45 * time.Second
	probeWaitDelay = 2 * time.Second
)

// probeArgs builds the non-interactive, read-only, ephemeral Codex invocation.
// Interactive TUI flags (notably --ask-for-approval) are invalid on `exec` and
// must never be added here. It intentionally does not force a minimal reasoning
// effort: not every model advertises minimal, so that override could make the
// probe reject locally before the selected model receives the request.
func (p *Plugin) probeArgs(model string) []string {
	args := make([]string, 0, 14)
	p.appendWrapperFlags(&args)
	return append(args,
		"exec",
		"--model", model,
		"--sandbox", "read-only",
		"--skip-git-repo-check",
		"--ephemeral",
		"--ignore-user-config",
		"--ignore-rules",
		"--color", "never",
		"Reply exactly OK. Do not use tools.",
	)
}

// ValidateModel performs a bounded advisory model probe. It is used only by
// explicit/background validation paths; session spawn never calls it.
func (p *Plugin) ValidateModel(ctx context.Context, model string) (ports.ModelValidationResult, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return ports.ModelValidationResult{Status: ports.ModelValidationProbeUnavailable, Message: "model is blank"}, nil
	}
	binary, err := p.agentBinary(ctx)
	if err != nil {
		return ports.ModelValidationResult{Status: ports.ModelValidationProbeUnavailable, Message: err.Error()}, nil
	}
	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, binary, p.probeArgs(model)...)
	cmd.WaitDelay = probeWaitDelay
	configureProbeProcessGroup(cmd)
	out, cmdErr := cmd.CombinedOutput()
	if probeCtx.Err() != nil {
		return ports.ModelValidationResult{Status: ports.ModelValidationProbeUnavailable, Message: probeCtx.Err().Error()}, nil
	}
	return modelProbeResultFromOutput(out, cmdErr), nil
}

func modelProbeResultFromOutput(out []byte, cmdErr error) ports.ModelValidationResult {
	text := strings.TrimSpace(string(out))
	if cmdErr == nil {
		return ports.ModelValidationResult{Status: ports.ModelValidationReachable, Message: text}
	}
	if probeSawModelRejection(out) {
		return ports.ModelValidationResult{Status: ports.ModelValidationUnreachable, Message: text}
	}
	return ports.ModelValidationResult{Status: ports.ModelValidationProbeUnavailable, Message: text}
}

var (
	probeStatusJSONRe  = regexp.MustCompile(`"status"\s*:\s*(\d{3})`)
	probeStatusPlainRe = regexp.MustCompile(`(?m)^\s*(?:ERROR:\s*)?(\d{3})\s+\S`)
)

func probeSawModelRejection(out []byte) bool {
	statuses := make([]int, 0, 4)
	for _, re := range []*regexp.Regexp{probeStatusJSONRe, probeStatusPlainRe} {
		for _, match := range re.FindAllStringSubmatch(string(out), -1) {
			if code, err := strconv.Atoi(match[1]); err == nil {
				statuses = append(statuses, code)
			}
		}
	}
	rejected := false
	for _, status := range statuses {
		switch {
		case status >= 500, status == 429, status == 408, status == 401, status == 403:
			return false
		case status == 400, status == 404, status == 422:
			rejected = true
		}
	}
	return rejected
}

func formatProbeOutput(out []byte) string {
	text := strings.TrimSpace(string(out))
	if text == "" {
		return ""
	}
	runes := []rune(text)
	if len(runes) > 500 {
		text = string(runes[:500]) + "...[truncated]"
	}
	return ": " + text
}

// sharedCodexAuthStatus answers the fugu auth question by probing the plain
// codex binary, which owns the shared credential.
//
// It deliberately does not treat a clean exit as authorization: a runtime help
// dump exits zero and says nothing about login state, and reporting a broken
// worker as healthy is worse than reporting it as unknown.
func sharedCodexAuthStatus(ctx context.Context) (ports.AgentAuthStatus, error) {
	codexBinary, err := ResolveCodexBinary(ctx)
	if err != nil {
		// No codex binary to ask. The fugu harness's auth state is genuinely
		// unknown rather than unauthorized, and a missing shared binary is not
		// this probe's error to raise.
		return ports.AgentAuthStatusUnknown, nil //nolint:nilerr // absence of the shared binary is an unknown answer, not a probe failure
	}
	// The shared credential lives in plain codex, which takes no wrapper flags.
	status, _, failed, err := loginStatusForBinary(ctx, codexBinary)
	if err != nil {
		return ports.AgentAuthStatusUnknown, err
	}
	if status != ports.AgentAuthStatusUnknown {
		return status, nil
	}
	if failed {
		return ports.AgentAuthStatusUnauthorized, nil
	}
	return ports.AgentAuthStatusUnknown, nil
}

// loginStatusForBinary runs `<binary> login status` under a short budget and
// classifies the output. It returns the classification, the lowercased combined
// output (so callers can match on a specific rejection), whether the command
// itself exited non-zero, and a probe error.
//
// A non-zero exit is reported as a bool rather than an error because it is a
// signal about login state, not a failure to propagate: callers answer with an
// auth status either way. status is Unknown when the output carries no
// recognizable login state — that ambiguity is the caller's to resolve.
//
// preArgs are placed before the `login status` subcommand, for wrapper binaries
// (codex-fugu) that need a leading flag like --no-update parsed at top level.
func loginStatusForBinary(ctx context.Context, binary string, preArgs ...string) (status ports.AgentAuthStatus, text string, failed bool, err error) {
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	args := append(append([]string{}, preArgs...), "login", "status")
	out, cmdErr := exec.CommandContext(probeCtx, binary, args...).CombinedOutput()
	if probeCtx.Err() != nil {
		return ports.AgentAuthStatusUnknown, "", false, probeCtx.Err()
	}
	text = strings.ToLower(string(out))
	failed = cmdErr != nil
	if strings.Contains(text, "not logged in") || strings.Contains(text, "logged out") {
		return ports.AgentAuthStatusUnauthorized, text, failed, nil
	}
	if strings.Contains(text, "logged in") {
		return ports.AgentAuthStatusAuthorized, text, failed, nil
	}
	return ports.AgentAuthStatusUnknown, text, failed, nil
}

// ResolveCodexBinary returns the path to the codex binary on this machine,
// searching platform-specific well-known install locations and PATH.
func ResolveCodexBinary(ctx context.Context) (string, error) {
	return ResolveAgentBinary(ctx, "codex")
}

// ResolveAgentBinary resolves a Codex-family binary by name, searching
// platform-specific well-known install locations and PATH. The Windows
// npm-shim to native-exe indirection and the WindowsApps exclusion apply only
// to `codex` itself; they describe how the upstream Codex npm package installs
// and do not generalize to the fugu wrapper.
func ResolveAgentBinary(ctx context.Context, binaryName string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if binaryName == "" {
		binaryName = "codex"
	}
	isCodex := binaryName == "codex"

	if runtime.GOOS == "windows" {
		candidates := []string{}
		if appData := os.Getenv("APPDATA"); appData != "" {
			shim := filepath.Join(appData, "npm", binaryName+".cmd")
			if isCodex {
				candidates = append(candidates, windowsNativeCodexCandidatesForShim(shim)...)
			}
			candidates = append(candidates,
				filepath.Join(appData, "npm", binaryName+".exe"),
				shim,
			)
		}
		if home, err := os.UserHomeDir(); err == nil {
			candidates = append(candidates, filepath.Join(home, ".cargo", "bin", binaryName+".exe"))
		}
		for _, candidate := range candidates {
			if fileExists(candidate) {
				if isCodex {
					return resolveNativeWindowsCodex(candidate), nil
				}
				return candidate, nil
			}
			if err := ctx.Err(); err != nil {
				return "", err
			}
		}

		for _, name := range []string{binaryName + ".cmd", binaryName, binaryName + ".exe"} {
			path, err := exec.LookPath(name)
			if err == nil && path != "" {
				if isCodex {
					if isWindowsAppsCodexExecutable(path) {
						continue
					}
					return resolveNativeWindowsCodex(path), nil
				}
				return path, nil
			}
			if err := ctx.Err(); err != nil {
				return "", err
			}
		}

		return "", fmt.Errorf("%s: %w", binaryName, ports.ErrAgentBinaryNotFound)
	}

	if path, err := exec.LookPath(binaryName); err == nil && path != "" {
		return path, nil
	}

	candidates := []string{
		"/usr/local/bin/" + binaryName,
		"/opt/homebrew/bin/" + binaryName,
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates,
			filepath.Join(home, ".cargo", "bin", binaryName),
			filepath.Join(home, ".local", "bin", binaryName),
			filepath.Join(home, ".npm-global", "bin", binaryName),
			filepath.Join(home, ".npm", "bin", binaryName),
		)
		nodeManagerCandidates, err := binaryutil.UnixNodeManagerBinCandidates(ctx, home, binaryName)
		if err != nil {
			return "", err
		}
		candidates = append(candidates, nodeManagerCandidates...)
	}

	for _, candidate := range candidates {
		if fileExists(candidate) {
			return candidate, nil
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
	}

	return "", fmt.Errorf("%s: %w", binaryName, ports.ErrAgentBinaryNotFound)
}

func resolveNativeWindowsCodex(path string) string {
	if runtime.GOOS != "windows" || !strings.EqualFold(filepath.Ext(path), ".cmd") {
		return path
	}
	for _, candidate := range windowsNativeCodexCandidatesForShim(path) {
		if fileExists(candidate) {
			return candidate
		}
	}
	return path
}

func windowsNativeCodexCandidatesForShim(shim string) []string {
	dir := filepath.Dir(shim)
	return []string{
		filepath.Join(dir, "node_modules", "@openai", "codex", "node_modules", "@openai", "codex-win32-x64", "vendor", "x86_64-pc-windows-msvc", "bin", "codex.exe"),
		filepath.Join(dir, "node_modules", "@openai", "codex", "bin", "codex.exe"),
	}
}

func isWindowsAppsCodexExecutable(path string) bool {
	if runtime.GOOS != "windows" {
		return false
	}
	clean := strings.ToLower(filepath.Clean(path))
	base := filepath.Base(clean)
	return (base == "codex.exe" || base == "codex") &&
		strings.Contains(clean, string(filepath.Separator)+"windowsapps"+string(filepath.Separator)+"openai.codex_")
}

func (p *Plugin) agentBinary(ctx context.Context) (string, error) {
	p.binaryMu.Lock()
	defer p.binaryMu.Unlock()

	if p.resolvedBinary != "" {
		return p.resolvedBinary, nil
	}

	binary, err := ResolveAgentBinary(ctx, p.binaryCommand())
	if err != nil {
		return "", err
	}
	p.resolvedBinary = binary
	return binary, nil
}

// DoctorLaunchProbes returns argv tails `ao doctor` runs against the installed
// codex binary to smoke-test the launch surface AO's hook delivery depends on.
// Probe 1 confirms --dangerously-bypass-hook-trust still exists (clap rejects
// unknown flags with a non-zero exit even alongside --version). Probe 2 loads
// codex's config with AO's `-c` session-flag overrides through the offline
// `features list` subcommand, so an override-parse regression surfaces as a
// non-zero exit or warning output. Both are built from the same flag builders
// the launch command uses, so the probes cannot drift from the real spawn argv.
func DoctorLaunchProbes() [][]string {
	flagProbe := make([]string, 0, 2)
	appendHookTrustBypassFlag(&flagProbe)
	flagProbe = append(flagProbe, "--version")

	overrideProbe := []string{"features", "list"}
	appendNoUpdateCheckFlag(&overrideProbe)
	appendHideRateLimitNudgeFlag(&overrideProbe)
	appendSessionHookFlags(&overrideProbe)
	appendWorkspaceTrustFlag(&overrideProbe, os.TempDir())
	return [][]string{flagProbe, overrideProbe}
}

// appendWrapperFlags emits flags the wrapper binary itself consumes, ahead of
// any subcommand. codex-fugu is an auto-updating wrapper that blocks on an
// interactive update prompt, which hangs a headless AO pane invisibly; it
// accepts --no-update only at top level. This is distinct from
// appendNoUpdateCheckFlag, which sets an unrelated Codex config override.
func (p *Plugin) appendWrapperFlags(cmd *[]string) {
	if p.adapterID() == fuguAdapterID {
		*cmd = append(*cmd, "--no-update")
	}
}

func appendNoUpdateCheckFlag(cmd *[]string) {
	*cmd = append(*cmd, "-c", "check_for_update_on_startup=false")
}

func appendHideRateLimitNudgeFlag(cmd *[]string) {
	// When the account nears its rate limit, the Codex TUI interposes an
	// interactive "switch to a cheaper model?" dialog before the first turn.
	// In a headless AO pane that dialog hangs the session invisibly and
	// swallows the auto-submitted spawn prompt, so suppress it.
	*cmd = append(*cmd, "-c", "notice.hide_rate_limit_model_nudge=true")
}

func appendHookTrustBypassFlag(cmd *[]string) {
	// AO's activity hooks ride the launch command as session-flag config (see
	// appendSessionHookFlags) and carry no persisted trust hash in the user's
	// `[hooks.state]`. Without this flag Codex would hold them for an
	// interactive hooks review, leaving AO without activity signals.
	*cmd = append(*cmd, "--dangerously-bypass-hook-trust")
}

func appendModelFlag(cmd *[]string, model string) {
	if m := strings.TrimSpace(model); m != "" {
		*cmd = append(*cmd, "--model", m)
	}
}

// appendReasoningEffortFlag maps AO's typed effort onto the harness-native
// model_reasoning_effort override. Fugu's max compatibility alias becomes
// xhigh; plain Codex preserves every effort its catalog can advertise.
func (p *Plugin) appendReasoningEffortFlag(cmd *[]string, effort domain.Effort) {
	native := domain.NormalizeEffortForHarness(domain.AgentHarness(p.adapterID()), effort)
	if e := normalizeCodexEffort(string(native)); e != "" {
		*cmd = append(*cmd, "-c", fmt.Sprintf("model_reasoning_effort=%q", e))
	}
}

func normalizeCodexEffort(effort string) string {
	switch e := strings.ToLower(strings.TrimSpace(effort)); e {
	case "minimal", "low", "medium", "high", "xhigh", "max":
		return e
	default:
		return ""
	}
}

func appendTerminalCompatibilityFlags(cmd *[]string) {
	if runtime.GOOS == "windows" {
		*cmd = append(*cmd, "--no-alt-screen")
	}
}

func appendApprovalFlags(cmd *[]string, permissions ports.PermissionMode) {
	switch ports.NormalizePermissionMode(permissions) {
	case ports.PermissionModeDefault:
		// Codex sessions are AO-managed and run headlessly inside a terminal
		// mux pane; default to no approval prompts unless project settings
		// explicitly choose a more restrictive mode.
		*cmd = append(*cmd, "--dangerously-bypass-approvals-and-sandbox")
	case ports.PermissionModeAcceptEdits:
		*cmd = append(*cmd, "--ask-for-approval", "on-request")
	case ports.PermissionModeAuto:
		*cmd = append(*cmd, "--ask-for-approval", "on-request", "-c", `approvals_reviewer="auto_review"`)
	case ports.PermissionModeBypassPermissions:
		*cmd = append(*cmd, "--dangerously-bypass-approvals-and-sandbox")
	}
}

// fileExists is a package var so tests can stub it to scope candidate probing.
var fileExists = func(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
