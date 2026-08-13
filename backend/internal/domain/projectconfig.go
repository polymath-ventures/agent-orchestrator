package domain

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"time"
)

// ProjectConfig is the typed per-project configuration — the SQLite twin of the
// legacy agent-orchestrator.yaml `projects.<id>` block. It is persisted as one
// JSON blob per project and resolved at spawn. Each field is typed and
// validated; there is no free-form map.
//
// Only fields with a live consumer are modeled: DefaultBranch, Env, Symlinks,
// PostCreate, AgentConfig, prompt rules, and the role overrides are consumed at
// spawn; SessionPrefix feeds the display prefix. Settings whose consumers do not
// yet exist (tracker/SCM per-project config) are intentionally absent and land in
// focused follow-up PRs alongside the code that reads them.
type ProjectConfig struct {
	// DefaultBranch is the base branch new session worktrees are created from.
	DefaultBranch string `json:"defaultBranch,omitempty"`
	// SessionPrefix overrides the displayed session-id prefix.
	SessionPrefix string `json:"sessionPrefix,omitempty"`

	// Env are extra environment variables forwarded into worker session
	// runtimes. AO-internal vars (AO_SESSION, AO_PROJECT_ID, …) always win.
	Env map[string]string `json:"env,omitempty"`
	// Symlinks are repo-relative paths symlinked into each session workspace.
	Symlinks []string `json:"symlinks,omitempty"`
	// PostCreate are shell commands run in the workspace after it is created.
	PostCreate []string `json:"postCreate,omitempty"`

	// AgentRules are project-specific standing instructions for worker sessions.
	AgentRules string `json:"agentRules,omitempty"`
	// WorkerTaskPrompt fully replaces AO's built-in issue-driven worker task
	// message. Literal {issue} tokens are rendered at spawn; other bytes are
	// preserved exactly. Unlike AgentRules, this is a task message, not part of
	// the worker system prompt.
	WorkerTaskPrompt string `json:"workerTaskPrompt,omitempty"`
	// AgentRulesFile is a repo-relative Markdown/text file whose contents are
	// appended to AgentRules for worker sessions.
	AgentRulesFile string `json:"agentRulesFile,omitempty"`
	// OrchestratorRules are project-specific standing instructions for
	// orchestrator sessions.
	OrchestratorRules string `json:"orchestratorRules,omitempty"`
	// OrchestratorRulesFile is a repo-relative Markdown/text file whose contents
	// are appended to OrchestratorRules for orchestrator sessions.
	OrchestratorRulesFile string `json:"orchestratorRulesFile,omitempty"`
	// ReviewerRules are project-specific standing instructions injected into
	// reviewer sessions.
	ReviewerRules string `json:"reviewerRules,omitempty"`
	// ReviewerRulesFile is a repo-relative Markdown/text file whose contents are
	// appended to ReviewerRules for reviewer sessions.
	ReviewerRulesFile string `json:"reviewerRulesFile,omitempty"`
	// PrimeRules are project-specific standing instructions for prime sessions.
	PrimeRules string `json:"primeRules,omitempty"`
	// PrimeRulesFile is a repo-relative Markdown/text file whose contents are
	// appended to PrimeRules for prime sessions.
	PrimeRulesFile string `json:"primeRulesFile,omitempty"`

	// AgentConfig is the default agent config for the project.
	AgentConfig AgentConfig `json:"agentConfig,omitempty"`
	// Worker, Orchestrator, and Prime are role-specific harness/agent-config
	// overrides.
	Worker       RoleOverride `json:"worker,omitempty"`
	Orchestrator RoleOverride `json:"orchestrator,omitempty"`
	Prime        RoleOverride `json:"prime,omitempty"`

	// Reviewers names the agent(s) that review a worker's PR when a review is
	// triggered. It is configured independently of the Worker override; an empty
	// list resolves to a cross-family default (see ResolveReviewerHarness).
	Reviewers []ReviewerConfig `json:"reviewers,omitempty"`

	// TrackerIntake controls issue-driven worker spawning. It is opt-in and
	// read-only toward the tracker in v1: matching issues spawn sessions, but the
	// tracker is not commented on or transitioned.
	TrackerIntake TrackerIntakeConfig `json:"trackerIntake,omitempty"`

	// WorkerMix distributes unpinned worker spawns across weighted agent/model
	// buckets. It is opt-in and has no default: an empty mix leaves harness
	// resolution to the Worker role override exactly as before.
	WorkerMix WorkerMix `json:"workerMix,omitempty"`

	// MaxLiveWorkers optionally caps the number of concurrently-live worker
	// sessions a project may run. Zero (the default) means unbounded: worker
	// spawns are not limited by this field, which preserves the pre-cap
	// behavior and keeps the zero value out of IsZero so an unset config still
	// persists SQL NULL. A negative value is invalid.
	MaxLiveWorkers int `json:"maxLiveWorkers,omitempty"`

	// ContainerReap controls whether AO reaps a worker session's ao.session-
	// labeled Docker containers on terminal state / kill. Enabled by default;
	// set Disabled to opt a project out entirely. Per-container sparing uses
	// the ao.spare=true label instead (see dockerreap.SpareLabel) so the
	// opt-out travels with the container at `docker run` time rather than
	// drifting out of sync with a project-config list.
	ContainerReap ContainerReapConfig `json:"containerReap,omitempty"`
}

// ContainerReapConfig is the project-level opt-out for #2652's Docker
// container reaping on session terminal state.
type ContainerReapConfig struct {
	// Disabled turns off container reaping for every session in this project.
	// Per-container sparing (ao.spare=true) is unaffected either way.
	Disabled bool `json:"disabled,omitempty"`
}

// ReviewerConfig names one reviewer agent by harness. The harness is drawn from
// the reviewer vocabulary (ReviewerHarness), which is distinct from the worker
// AgentHarness set.
type ReviewerConfig struct {
	Harness     ReviewerHarness `json:"harness"`
	AgentConfig AgentConfig     `json:"agentConfig,omitempty"`
}

// FallbackReviewerHarness is the reviewer used when a project configures none
// and the worker's family has no defined cross-family default.
const FallbackReviewerHarness = ReviewerClaudeCode

// ResolveReviewerHarness picks the reviewer harness for a worker. A configured
// reviewer wins. Otherwise the default is a reviewer of a different family than
// the worker, so an unconfigured project still gets an independent review.
func (c ProjectConfig) ResolveReviewerHarness(worker AgentHarness) ReviewerHarness {
	if len(c.Reviewers) > 0 {
		return c.Reviewers[0].Harness
	}
	return crossFamilyReviewer(worker)
}

func crossFamilyReviewer(worker AgentHarness) ReviewerHarness {
	switch worker {
	case HarnessMuse:
		return ReviewerMuse
	case HarnessKimchi:
		return ReviewerKimchi
	}
	switch worker.Family() {
	case AgentFamilyClaude:
		return ReviewerCodex
	case AgentFamilyCodex, AgentFamilyFugu:
		return ReviewerClaudeCode
	case AgentFamilyOpenCode:
		return ReviewerClaudeCode
	default:
		return FallbackReviewerHarness
	}
}

// RoleOverride overrides the harness and/or agent config for a session role.
type RoleOverride struct {
	Harness     AgentHarness `json:"agent,omitempty"`
	AgentConfig AgentConfig  `json:"agentConfig,omitempty"`
	// WakeInterval controls how long prime may sit idle or at waiting_input
	// before the daemon sends a supervision-loop nudge. It is consumed for the
	// prime role only; empty means use the daemon default.
	WakeInterval string `json:"wakeInterval,omitempty" description:"Prime role only. Positive Go duration string such as 15m; empty uses the daemon default."`
	// WakeBackoff controls exponential prime idle wake spacing. When unset,
	// backoff is enabled with WakeInterval as its base and a 60m max.
	WakeBackoff *WakeBackoffConfig `json:"wakeBackoff,omitempty"`
}

// WakeBackoffConfig is the JSON config for prime idle wake backoff. Base and
// Max are positive Go duration strings. Empty Base inherits WakeInterval; empty
// Max uses DefaultWakeBackoffMaxInterval.
type WakeBackoffConfig struct {
	Enabled *bool  `json:"enabled,omitempty" description:"When false, keep fixed-interval wake behavior at the base interval instead of exponential idle backoff. Defaults to true."`
	Base    string `json:"base,omitempty" description:"Positive Go duration for the reset/base wake interval. Empty inherits wakeInterval."`
	Max     string `json:"max,omitempty" description:"Positive Go duration cap for exponential idle wake backoff. Empty uses the daemon default."`
}

// WakeBackoffPolicy is the parsed scheduler policy.
type WakeBackoffPolicy struct {
	Enabled bool
	Base    time.Duration
	Max     time.Duration
}

// DefaultBranchName is the base branch used when a project configures none.
const DefaultBranchName = "main"

// DefaultPrimeWakeInterval is the daemon fallback when a project leaves
// prime.wakeInterval unset.
const DefaultPrimeWakeInterval = 15 * time.Minute

// DefaultWakeBackoffMaxInterval is the cap for prime idle wake backoff when
// wakeBackoff.max is unset.
const DefaultWakeBackoffMaxInterval = time.Hour

const defaultPrimeWakeIntervalConfig = "15m"

// DefaultProjectConfig returns the config a project has when it sets nothing:
// branch "main". Every other field defaults to its zero value (no
// env/symlinks/post-create, agent + role defaults).
func DefaultProjectConfig() ProjectConfig {
	return ProjectConfig{
		DefaultBranch: DefaultBranchName,
	}
}

// WithDefaults overlays DefaultProjectConfig onto c, filling only fields the
// project left unset. A set field is always preserved.
func (c ProjectConfig) WithDefaults() ProjectConfig {
	def := DefaultProjectConfig()
	if c.DefaultBranch == "" {
		c.DefaultBranch = def.DefaultBranch
	}
	if c.Prime.WakeInterval == "" {
		c.Prime.WakeInterval = defaultPrimeWakeIntervalConfig
	}
	c.TrackerIntake = c.TrackerIntake.WithDefaults()
	// WorkerMix deliberately gets no default: IsZero compares against the zero
	// ProjectConfig, so any non-nil default here would make every unset config
	// non-zero and stop storage persisting SQL NULL.
	return c
}

// IsZero reports whether the config carries no settings, so storage can persist
// SQL NULL and resolution can skip an empty config.
func (c ProjectConfig) IsZero() bool {
	return reflect.DeepEqual(c, ProjectConfig{})
}

// Validate rejects values outside the typed vocabulary so a bad config is
// refused when it is set (CLI/API) rather than surfacing at spawn.
func (c ProjectConfig) Validate() error {
	if err := c.AgentConfig.Validate(); err != nil {
		return err
	}
	if err := validateNameComponent("sessionPrefix", c.SessionPrefix); err != nil {
		return err
	}
	for role, ro := range map[string]RoleOverride{"worker": c.Worker, "orchestrator": c.Orchestrator, "prime": c.Prime} {
		if ro.Harness != "" && !ro.Harness.IsKnown() {
			return fmt.Errorf("%s.agent: unknown harness %q", role, ro.Harness)
		}
		if err := ro.AgentConfig.Validate(); err != nil {
			return fmt.Errorf("%s.%w", role, err)
		}
	}
	if c.Worker.WakeInterval != "" {
		return fmt.Errorf("worker.wakeInterval: not supported")
	}
	if c.Worker.WakeBackoff != nil {
		return fmt.Errorf("worker.wakeBackoff: not supported")
	}
	if c.Orchestrator.WakeInterval != "" {
		return fmt.Errorf("orchestrator.wakeInterval: not supported")
	}
	if c.Orchestrator.WakeBackoff != nil {
		return fmt.Errorf("orchestrator.wakeBackoff: not supported")
	}
	if _, err := c.Prime.WakeIntervalDuration(); err != nil {
		return fmt.Errorf("prime.wakeInterval: %w", err)
	}
	if _, err := c.Prime.WakeBackoffPolicy(); err != nil {
		return fmt.Errorf("prime.wakeBackoff: %w", err)
	}
	for _, s := range c.Symlinks {
		if err := validateRepoRelative(s); err != nil {
			return fmt.Errorf("symlink %q: %w", s, err)
		}
	}
	if err := validateRoleRulesFilePath(c.AgentRulesFile); err != nil {
		return fmt.Errorf("agentRulesFile %q: %w", c.AgentRulesFile, err)
	}
	if err := validateRoleRulesFilePath(c.OrchestratorRulesFile); err != nil {
		return fmt.Errorf("orchestratorRulesFile %q: %w", c.OrchestratorRulesFile, err)
	}
	if err := validateRoleRulesFilePath(c.ReviewerRulesFile); err != nil {
		return fmt.Errorf("reviewerRulesFile %q: %w", c.ReviewerRulesFile, err)
	}
	if err := validateRoleRulesFilePath(c.PrimeRulesFile); err != nil {
		return fmt.Errorf("primeRulesFile %q: %w", c.PrimeRulesFile, err)
	}
	for i, rv := range c.Reviewers {
		if !rv.Harness.IsKnown() {
			return fmt.Errorf("reviewers[%d].harness: unknown harness %q", i, rv.Harness)
		}
		if err := rv.AgentConfig.Validate(); err != nil {
			return fmt.Errorf("reviewers[%d]: %w", i, err)
		}
		if model := rv.AgentConfig.Model; model != "" {
			if hp := rv.Harness.AgentHarness().ModelProvider(); !ClassifyModelProvider(model).CompatibleWith(hp) {
				return fmt.Errorf("reviewers[%d].agentConfig.model: %q is not a %s model", i, model, hp)
			}
		}
	}
	if err := c.TrackerIntake.Validate(); err != nil {
		return err
	}
	// The mix owns its own rules (known harness, weight range, bucket
	// uniqueness, sum of 100); rejecting here keeps a mix that could not be
	// apportioned deterministically out of storage entirely.
	if err := c.WorkerMix.Validate(); err != nil {
		return err
	}
	// A negative cap is meaningless; zero means unbounded and is the default.
	if c.MaxLiveWorkers < 0 {
		return fmt.Errorf("maxLiveWorkers: must not be negative, got %d", c.MaxLiveWorkers)
	}
	return nil
}

// ResolveWorkerTaskPrompt returns the effective configured worker task
// template and its precedence source. A non-empty project value wins over the
// daemon-wide typed default. Whitespace values deliberately count as active so
// a bad higher-precedence setting fails closed at render time instead of
// silently falling back.
func ResolveWorkerTaskPrompt(project, defaults ProjectConfig) (template, source string) {
	if project.WorkerTaskPrompt != "" {
		return project.WorkerTaskPrompt, "project"
	}
	if defaults.WorkerTaskPrompt != "" {
		return defaults.WorkerTaskPrompt, "global"
	}
	return "", ""
}

// WakeIntervalDuration parses the configured prime wake interval. An empty
// value resolves to DefaultPrimeWakeInterval.
func (r RoleOverride) WakeIntervalDuration() (time.Duration, error) {
	if r.WakeInterval == "" {
		return DefaultPrimeWakeInterval, nil
	}
	d, err := time.ParseDuration(r.WakeInterval)
	if err != nil {
		return 0, err
	}
	if d <= 0 {
		return 0, fmt.Errorf("must be positive")
	}
	return d, nil
}

// WakeBackoffPolicy parses the prime wake backoff config. An unset wakeBackoff
// block means enabled backoff using WakeInterval as the base and a one-hour
// cap. A disabled block keeps fixed-interval wake behavior.
func (r RoleOverride) WakeBackoffPolicy() (WakeBackoffPolicy, error) {
	base, err := r.WakeIntervalDuration()
	if err != nil {
		return WakeBackoffPolicy{}, err
	}
	maxInterval := DefaultWakeBackoffMaxInterval
	enabled := true
	maxSet := false
	if r.WakeBackoff != nil {
		if r.WakeBackoff.Enabled != nil {
			enabled = *r.WakeBackoff.Enabled
		}
		if r.WakeBackoff.Base != "" {
			base, err = parsePositiveDuration("base", r.WakeBackoff.Base)
			if err != nil {
				return WakeBackoffPolicy{}, err
			}
		}
		if r.WakeBackoff.Max != "" {
			maxInterval, err = parsePositiveDuration("max", r.WakeBackoff.Max)
			if err != nil {
				return WakeBackoffPolicy{}, err
			}
			maxSet = true
		}
	}
	if !maxSet && maxInterval < base {
		maxInterval = base
	}
	if maxSet && maxInterval < base {
		return WakeBackoffPolicy{}, fmt.Errorf("max must be greater than or equal to base")
	}
	return WakeBackoffPolicy{Enabled: enabled, Base: base, Max: maxInterval}, nil
}

func parsePositiveDuration(field, value string) (time.Duration, error) {
	d, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", field, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("%s: must be positive", field)
	}
	return d, nil
}

func validateNoWhitespaceField(name, value string) error {
	if value == "" {
		return nil
	}
	if strings.TrimSpace(value) != value {
		return fmt.Errorf("%s: must not have leading or trailing whitespace", name)
	}
	return nil
}

func validateNameComponent(name, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	if strings.ContainsAny(trimmed, `/\`) || trimmed == "." || trimmed == ".." {
		return fmt.Errorf("%s: must not contain path separators or traversal components", name)
	}
	return nil
}

// validateRepoRelative refuses paths that would let a project config escape
// its repo root: absolute paths and any ".." segment (before or after Clean).
// The same guard runs at spawn time as defense-in-depth, but enforcing it here
// rejects bad config when it is set rather than at every later spawn.
func validateRepoRelative(p string) error {
	trimmed := strings.TrimSpace(p)
	if trimmed == "" {
		return nil
	}
	if filepath.IsAbs(trimmed) || strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, `\`) {
		return fmt.Errorf("path must be repo-relative and must not escape the project root")
	}
	clean := filepath.Clean(trimmed)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path must be repo-relative and must not escape the project root")
	}
	for _, seg := range strings.Split(filepath.ToSlash(clean), "/") {
		if seg == ".." {
			return fmt.Errorf("path must be repo-relative and must not escape the project root")
		}
	}
	return nil
}

func validateRoleRulesFilePath(p string) error {
	trimmed := strings.TrimSpace(p)
	if trimmed == "" {
		return nil
	}
	if filepath.IsAbs(trimmed) || strings.HasPrefix(trimmed, "/") {
		return nil
	}
	return validateRepoRelative(trimmed)
}
