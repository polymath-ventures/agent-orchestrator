package domain

import (
	"testing"
	"time"
)

func TestProjectConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     ProjectConfig
		wantErr bool
	}{
		{"empty ok", ProjectConfig{}, false},
		{"good agent config", ProjectConfig{AgentConfig: AgentConfig{Model: "m", Permissions: PermissionModeAuto}}, false},
		{"good agent mode", ProjectConfig{AgentConfig: AgentConfig{Mode: "ultra"}}, false},
		{"bad agent mode", ProjectConfig{AgentConfig: AgentConfig{Mode: "turbo"}}, true},
		{"bad permission", ProjectConfig{AgentConfig: AgentConfig{Permissions: "yolo"}}, true},
		{"good per-harness model", ProjectConfig{AgentConfig: AgentConfig{ModelByHarness: map[AgentHarness]HarnessModel{HarnessCodex: {Model: "gpt-5-codex", Effort: EffortHigh}}}}, false},
		{"per-harness unknown harness", ProjectConfig{AgentConfig: AgentConfig{ModelByHarness: map[AgentHarness]HarnessModel{"nope": {Model: "gpt-5-codex"}}}}, true},
		{"per-harness invalid effort", ProjectConfig{AgentConfig: AgentConfig{ModelByHarness: map[AgentHarness]HarnessModel{HarnessCodex: {Model: "gpt-5-codex", Effort: "giant"}}}}, true},
		{"per-harness cross-provider model", ProjectConfig{AgentConfig: AgentConfig{ModelByHarness: map[AgentHarness]HarnessModel{HarnessCodex: {Model: "claude-opus-4-5"}}}}, true},
		{"per-harness model whitespace", ProjectConfig{AgentConfig: AgentConfig{ModelByHarness: map[AgentHarness]HarnessModel{HarnessCodex: {Model: " gpt-5-codex"}}}}, true},
		{"good session prefix", ProjectConfig{SessionPrefix: "ao"}, false},
		{"session prefix with slash", ProjectConfig{SessionPrefix: "ao/project"}, true},
		{"session prefix with backslash", ProjectConfig{SessionPrefix: `ao\project`}, true},
		{"session prefix traversal component", ProjectConfig{SessionPrefix: ".."}, true},
		{"good role override", ProjectConfig{Worker: RoleOverride{Harness: HarnessCodex}}, false},
		{"unknown role harness", ProjectConfig{Orchestrator: RoleOverride{Harness: "nope"}}, true},
		{"bad role agent config", ProjectConfig{Worker: RoleOverride{AgentConfig: AgentConfig{Permissions: "nope"}}}, true},
		{"orchestrator wake interval unsupported", ProjectConfig{Orchestrator: RoleOverride{WakeInterval: "15m"}}, true},
		{"orchestrator wake backoff unsupported", ProjectConfig{Orchestrator: RoleOverride{WakeBackoff: &WakeBackoffConfig{Enabled: boolPtr(true)}}}, true},
		{"good prime wake interval", ProjectConfig{Prime: RoleOverride{WakeInterval: "30m"}}, false},
		{"negative prime wake interval", ProjectConfig{Prime: RoleOverride{WakeInterval: "-1m"}}, true},
		{"good prime wake backoff", ProjectConfig{Prime: RoleOverride{WakeBackoff: &WakeBackoffConfig{Enabled: boolPtr(true), Base: "15m", Max: "1h"}}}, false},
		{"prime wake backoff max below base", ProjectConfig{Prime: RoleOverride{WakeBackoff: &WakeBackoffConfig{Base: "30m", Max: "15m"}}}, true},
		{"worker wake interval unsupported", ProjectConfig{Worker: RoleOverride{WakeInterval: "15m"}}, true},
		{"worker wake backoff unsupported", ProjectConfig{Worker: RoleOverride{WakeBackoff: &WakeBackoffConfig{Enabled: boolPtr(true)}}}, true},
		{"good symlinks", ProjectConfig{Symlinks: []string{".env", "configs/dev.toml"}}, false},
		{"symlink absolute path", ProjectConfig{Symlinks: []string{"/etc/passwd"}}, true},
		{"symlink parent escape", ProjectConfig{Symlinks: []string{"../escape"}}, true},
		{"symlink embedded parent", ProjectConfig{Symlinks: []string{"a/../../b"}}, true},
		{"symlink bare ..", ProjectConfig{Symlinks: []string{".."}}, true},
		{"good prompt rules", ProjectConfig{AgentRules: "Run tests.", AgentRulesFile: "docs/agent-rules.md", OrchestratorRules: "Delegate work."}, false},
		{"good worker task prompt", ProjectConfig{WorkerTaskPrompt: "/address-issue {issue}"}, false},
		{"whitespace worker task prompt persists until spawn validation", ProjectConfig{WorkerTaskPrompt: " \n\t"}, false},
		{"agent rules file absolute path", ProjectConfig{AgentRulesFile: "/etc/passwd"}, false},
		{"agent rules file leading backslash", ProjectConfig{AgentRulesFile: `\policy.md`}, true},
		{"agent rules file parent escape", ProjectConfig{AgentRulesFile: "../rules.md"}, true},
		{"agent rules file cleans to dot", ProjectConfig{AgentRulesFile: "docs/.."}, true},
		{"agent rules file bare dot", ProjectConfig{AgentRulesFile: "."}, true},
		{"good orchestrator rules file", ProjectConfig{OrchestratorRulesFile: "docs/orch-rules.md"}, false},
		{"orchestrator rules file absolute path", ProjectConfig{OrchestratorRulesFile: "/etc/passwd"}, false},
		{"orchestrator rules file parent escape", ProjectConfig{OrchestratorRulesFile: "../rules.md"}, true},
		{"good reviewer rules", ProjectConfig{ReviewerRules: "Focus on data isolation.", ReviewerRulesFile: "docs/review-rules.md"}, false},
		{"reviewer rules file absolute path", ProjectConfig{ReviewerRulesFile: "/etc/passwd"}, false},
		{"reviewer rules file parent escape", ProjectConfig{ReviewerRulesFile: "../rules.md"}, true},
		{"good reviewers", ProjectConfig{Reviewers: []ReviewerConfig{{Harness: ReviewerClaudeCode}}}, false},
		{"good codex reviewer", ProjectConfig{Reviewers: []ReviewerConfig{{Harness: ReviewerCodex}}}, false},
		{"good codex reviewer model", ProjectConfig{Reviewers: []ReviewerConfig{{Harness: ReviewerCodex, AgentConfig: AgentConfig{Model: "gpt-5-codex"}}}}, false},
		{"bad reviewer permission", ProjectConfig{Reviewers: []ReviewerConfig{{Harness: ReviewerCodex, AgentConfig: AgentConfig{Permissions: "yolo"}}}}, true},
		{"reviewer cross-provider model rejected", ProjectConfig{Reviewers: []ReviewerConfig{{Harness: ReviewerCodex, AgentConfig: AgentConfig{Model: "claude-opus-4-5"}}}}, true},
		{"reviewer unknown model allowed", ProjectConfig{Reviewers: []ReviewerConfig{{Harness: ReviewerCodex, AgentConfig: AgentConfig{Model: "some-internal-model"}}}}, false},
		{"good opencode reviewer", ProjectConfig{Reviewers: []ReviewerConfig{{Harness: ReviewerOpenCode}}}, false},
		{"good kiro reviewer", ProjectConfig{Reviewers: []ReviewerConfig{{Harness: ReviewerKiro}}}, false},
		{"good pi reviewer", ProjectConfig{Reviewers: []ReviewerConfig{{Harness: ReviewerPi}}}, false},
		{"good experimental qwen reviewer", ProjectConfig{Reviewers: []ReviewerConfig{{Harness: ReviewerQwen}}}, false},
		{"good experimental agy reviewer", ProjectConfig{Reviewers: []ReviewerConfig{{Harness: ReviewerAgy}}}, false},
		{"good experimental continue reviewer", ProjectConfig{Reviewers: []ReviewerConfig{{Harness: ReviewerContinue}}}, false},
		{"good experimental goose reviewer", ProjectConfig{Reviewers: []ReviewerConfig{{Harness: ReviewerGoose}}}, false},
		{"good experimental vibe reviewer", ProjectConfig{Reviewers: []ReviewerConfig{{Harness: ReviewerVibe}}}, false},
		{"good experimental Devin reviewer", ProjectConfig{Reviewers: []ReviewerConfig{{Harness: ReviewerDevin}}}, false},
		{"good experimental Droid reviewer", ProjectConfig{Reviewers: []ReviewerConfig{{Harness: ReviewerDroid}}}, false},
		{"good experimental Kimi reviewer", ProjectConfig{Reviewers: []ReviewerConfig{{Harness: ReviewerKimi}}}, false},
		{"good Muse reviewer", ProjectConfig{Reviewers: []ReviewerConfig{{Harness: ReviewerMuse}}}, false},
		{"unknown reviewer harness", ProjectConfig{Reviewers: []ReviewerConfig{{Harness: "nope"}}}, true},
		{"good interactive Amp reviewer", ProjectConfig{Reviewers: []ReviewerConfig{{Harness: ReviewerAmp}}}, false},
		{"good interactive Aider reviewer", ProjectConfig{Reviewers: []ReviewerConfig{{Harness: ReviewerAider}}}, false},
		{"good experimental Grok reviewer", ProjectConfig{Reviewers: []ReviewerConfig{{Harness: ReviewerGrok}}}, false},
		{"good experimental Crush reviewer", ProjectConfig{Reviewers: []ReviewerConfig{{Harness: ReviewerCrush}}}, false},
		{"good experimental Auggie reviewer", ProjectConfig{Reviewers: []ReviewerConfig{{Harness: ReviewerAuggie}}}, false},
		{"good experimental Cline reviewer", ProjectConfig{Reviewers: []ReviewerConfig{{Harness: ReviewerCline}}}, false},
		{"good experimental Autohand reviewer", ProjectConfig{Reviewers: []ReviewerConfig{{Harness: ReviewerAutohand}}}, false},
		{"empty reviewer harness", ProjectConfig{Reviewers: []ReviewerConfig{{Harness: ""}}}, true},
		{"tracker intake assignee rule", ProjectConfig{TrackerIntake: TrackerIntakeConfig{Enabled: true, Assignee: "alice"}}, false},
		{"tracker intake explicit github", ProjectConfig{TrackerIntake: TrackerIntakeConfig{Enabled: true, Provider: TrackerProviderGitHub, Assignee: "alice"}}, false},
		{"tracker intake no rule", ProjectConfig{TrackerIntake: TrackerIntakeConfig{Enabled: true}}, true},
		{"tracker intake unknown provider", ProjectConfig{TrackerIntake: TrackerIntakeConfig{Enabled: true, Provider: "linear", Assignee: "alice"}}, true},
		{"tracker intake repo with whitespace", ProjectConfig{TrackerIntake: TrackerIntakeConfig{Enabled: true, Repo: " acme/demo", Assignee: "alice"}}, true},
		{"tracker intake assignee with whitespace", ProjectConfig{TrackerIntake: TrackerIntakeConfig{Enabled: true, Assignee: " alice"}}, true},
		{"empty worker mix", ProjectConfig{WorkerMix: WorkerMix{}}, false},
		{"good worker mix", ProjectConfig{WorkerMix: WorkerMix{
			{Harness: HarnessClaudeCode, Weight: 60},
			{Harness: HarnessCodex, Weight: 40},
		}}, false},
		{"worker mix weights do not sum to 100", ProjectConfig{WorkerMix: WorkerMix{
			{Harness: HarnessClaudeCode, Weight: 60},
			{Harness: HarnessCodex, Weight: 30},
		}}, true},
		{"worker mix unknown harness", ProjectConfig{WorkerMix: WorkerMix{{Harness: "nope", Weight: 100}}}, true},
		{"worker mix duplicate bucket", ProjectConfig{WorkerMix: WorkerMix{
			{Harness: HarnessClaudeCode, Weight: 50},
			{Harness: HarnessClaudeCode, Weight: 50},
		}}, true},
		{"zero max live workers is unset", ProjectConfig{MaxLiveWorkers: 0}, false},
		{"positive max live workers", ProjectConfig{MaxLiveWorkers: 3}, false},
		{"negative max live workers", ProjectConfig{MaxLiveWorkers: -1}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.cfg.Validate(); (err != nil) != tt.wantErr {
				t.Fatalf("Validate() err = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

func TestDefaultProjectConfig(t *testing.T) {
	def := DefaultProjectConfig()

	// The one documented non-empty default.
	if def.DefaultBranch != "main" {
		t.Fatalf("default DefaultBranch = %q, want main", def.DefaultBranch)
	}

	// Every other field defaults to its zero value: clearing the documented
	// default must leave the config completely empty.
	def.DefaultBranch = ""
	if !def.IsZero() {
		t.Fatalf("default config has unexpected non-zero fields: %#v", def)
	}
}

func TestProjectConfigWithDefaults(t *testing.T) {
	// An unset config gets the documented defaults.
	got := (ProjectConfig{}).WithDefaults()
	if got.DefaultBranch != DefaultBranchName {
		t.Fatalf("WithDefaults = %#v, want branch=main", got)
	}

	// Set fields are preserved, not overwritten.
	got = (ProjectConfig{
		DefaultBranch: "develop",
		AgentConfig:   AgentConfig{Model: "m"},
	}).WithDefaults()
	if got.DefaultBranch != "develop" {
		t.Fatalf("WithDefaults overwrote set fields: %#v", got)
	}
	if got.AgentConfig.Model != "m" {
		t.Fatalf("WithDefaults dropped a set field: %#v", got.AgentConfig)
	}

	got = (ProjectConfig{TrackerIntake: TrackerIntakeConfig{Enabled: true, Assignee: "alice"}}).WithDefaults()
	if got.TrackerIntake.Provider != TrackerProviderGitHub {
		t.Fatalf("TrackerIntake.Provider = %q, want %q", got.TrackerIntake.Provider, TrackerProviderGitHub)
	}

	got = (ProjectConfig{}).WithDefaults()
	if got.TrackerIntake.Provider != "" {
		t.Fatalf("disabled TrackerIntake.Provider = %q, want empty", got.TrackerIntake.Provider)
	}

	// The worker mix has no default: a non-nil one here would make every unset
	// config non-zero, and IsZero (reflect.DeepEqual) is what lets storage
	// persist SQL NULL for a project that configures nothing.
	if got.WorkerMix != nil {
		t.Fatalf("WithDefaults populated WorkerMix = %#v, want nil", got.WorkerMix)
	}

	got = (ProjectConfig{}).WithDefaults()
	if got.Prime.WakeInterval != "15m" {
		t.Fatalf("default prime wake interval = %s, want 15m", got.Prime.WakeInterval)
	}
	got = (ProjectConfig{Prime: RoleOverride{WakeInterval: "45m"}}).WithDefaults()
	if got.Prime.WakeInterval != "45m" {
		t.Fatalf("explicit prime wake interval = %s, want 45m", got.Prime.WakeInterval)
	}
	if d, err := got.Prime.WakeIntervalDuration(); err != nil || d != 45*time.Minute {
		t.Fatalf("parsed prime wake interval = %s, %v; want 45m", d, err)
	}
}

func TestResolveWorkerTaskPrompt(t *testing.T) {
	tests := []struct {
		name       string
		project    ProjectConfig
		defaults   ProjectConfig
		wantPrompt string
		wantSource string
	}{
		{name: "none"},
		{name: "global", defaults: ProjectConfig{WorkerTaskPrompt: "/global {issue}"}, wantPrompt: "/global {issue}", wantSource: "global"},
		{name: "project wins", project: ProjectConfig{WorkerTaskPrompt: "/project {issue}"}, defaults: ProjectConfig{WorkerTaskPrompt: "/global {issue}"}, wantPrompt: "/project {issue}", wantSource: "project"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prompt, source := ResolveWorkerTaskPrompt(tt.project, tt.defaults)
			if prompt != tt.wantPrompt || source != tt.wantSource {
				t.Fatalf("ResolveWorkerTaskPrompt() = %q, %q; want %q, %q", prompt, source, tt.wantPrompt, tt.wantSource)
			}
		})
	}
}

func boolPtr(v bool) *bool {
	return &v
}

func TestResolveReviewerHarness(t *testing.T) {
	// A configured reviewer always wins, regardless of the worker harness.
	cfg := ProjectConfig{Reviewers: []ReviewerConfig{{Harness: ReviewerClaudeCode}}}
	if got := cfg.ResolveReviewerHarness(HarnessAider); got != ReviewerClaudeCode {
		t.Fatalf("configured reviewer = %q, want claude-code", got)
	}

	// No reviewer configured: default to a reviewer of a DIFFERENT family than
	// the worker, so an unconfigured project still gets an independent review.
	if got := (ProjectConfig{}).ResolveReviewerHarness(HarnessClaudeCode); got != ReviewerCodex {
		t.Fatalf("claude-code worker = %q, want cross-family reviewer codex", got)
	}
	if got := (ProjectConfig{}).ResolveReviewerHarness(HarnessCodex); got != ReviewerClaudeCode {
		t.Fatalf("codex worker = %q, want cross-family reviewer claude-code", got)
	}
	if got := (ProjectConfig{}).ResolveReviewerHarness(HarnessCodexFugu); got != ReviewerClaudeCode {
		t.Fatalf("codex-fugu worker = %q, want cross-family reviewer claude-code", got)
	}
	if got := (ProjectConfig{}).ResolveReviewerHarness(HarnessOpenCode); got != ReviewerClaudeCode {
		t.Fatalf("opencode worker = %q, want cross-family reviewer claude-code", got)
	}
	for _, worker := range []AgentHarness{HarnessClaudeCode, HarnessCodex, HarnessCodexFugu, HarnessOpenCode} {
		reviewer := (ProjectConfig{}).ResolveReviewerHarness(worker)
		if reviewer.AgentHarness().Family() == worker.Family() {
			t.Fatalf("worker %q got same-family reviewer %q", worker, reviewer)
		}
	}
	if got := (ProjectConfig{}).ResolveReviewerHarness(HarnessMuse); got != ReviewerMuse {
		t.Fatalf("muse worker = %q, want reviewer muse", got)
	}
	if got := (ProjectConfig{}).ResolveReviewerHarness(HarnessKimchi); got != ReviewerKimchi {
		t.Fatalf("kimchi worker = %q, want reviewer kimchi", got)
	}

	// A worker harness with no established family (e.g. crush, aider) falls
	// back to claude-code.
	if got := (ProjectConfig{}).ResolveReviewerHarness(HarnessCrush); got != FallbackReviewerHarness {
		t.Fatalf("crush worker = %q, want %q", got, FallbackReviewerHarness)
	}
	for _, worker := range []AgentHarness{
		HarnessCopilot, HarnessCursor, HarnessKilocode, HarnessKiro, HarnessPi,
		HarnessAider, HarnessAmp, HarnessQwen, HarnessAgy, HarnessContinue,
		HarnessGoose, HarnessVibe, HarnessDevin, HarnessDroid, HarnessKimi,
		HarnessGrok, HarnessCrush, HarnessAuggie, HarnessCline, HarnessAutohand,
	} {
		if got := (ProjectConfig{}).ResolveReviewerHarness(worker); got != FallbackReviewerHarness {
			t.Errorf("%s worker = %q, want explicit-selection fallback %q", worker, got, FallbackReviewerHarness)
		}
	}
}

func TestProjectConfigIsZero(t *testing.T) {
	if !(ProjectConfig{}).IsZero() {
		t.Fatal("empty config should be zero")
	}
	if (ProjectConfig{DefaultBranch: "main"}).IsZero() {
		t.Fatal("populated config should not be zero")
	}
	if (ProjectConfig{Env: map[string]string{"A": "b"}}).IsZero() {
		t.Fatal("config with env should not be zero")
	}
	if (ProjectConfig{MaxLiveWorkers: 2}).IsZero() {
		t.Fatal("config with a worker cap should not be zero")
	}
}
