// Package sessionmanager drives internal session command operations over runtime,
// agent, workspace, storage, messenger, and lifecycle dependencies.
package sessionmanager

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/agentconfig"
	"github.com/aoagents/agent-orchestrator/backend/internal/candidatehealth"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	aoprocess "github.com/aoagents/agent-orchestrator/backend/internal/process"
	"github.com/aoagents/agent-orchestrator/backend/internal/sessionguard"
	"github.com/aoagents/agent-orchestrator/backend/internal/skillassets"
)

// Sentinel errors returned by the Session Manager; callers match them with
// errors.Is.
var (
	ErrNotFound         = errors.New("session: not found")
	ErrNotRestorable    = errors.New("session: not restorable (not terminal)")
	ErrTerminated       = errors.New("session: terminated")
	ErrIncompleteHandle = errors.New("session: incomplete teardown handle")
	// ErrProjectNotResolvable means the spawn's project has no usable repo
	// (unregistered, archived, or missing a path). The API maps it to a 400.
	ErrProjectNotResolvable = errors.New("session: project repo not resolvable")
	// ErrUnknownHarness means the requested agent harness has no registered
	// adapter. The API maps it to a 400 so a typo'd `--harness` is a validation
	// error, not an opaque 500.
	ErrUnknownHarness = errors.New("session: unknown agent harness")
	// ErrMissingHarness means neither the spawn request nor the project's role
	// config selected an agent. Worker/orchestrator spawns must be explicit.
	ErrMissingHarness = errors.New("session: agent harness required")
	// ErrWorkerMixExhausted means every bucket in the project's worker mix was
	// unavailable, so no bucket could be selected. The mix never substitutes a
	// harness it does not list, so an exhausted mix fails loudly instead.
	ErrWorkerMixExhausted = errors.New("session: no worker mix bucket available")
	// ErrWorkerMixBucketDown means the weighted worker mix selected an exact
	// bucket that candidate health currently marks down. The spawn refuses that
	// bucket instead of silently substituting another one, and candidate health
	// debits the skipped slot so the bucket's share is preserved in later census.
	ErrWorkerMixBucketDown = errors.New("session: worker mix bucket is down")
	// ErrWorkerConcurrencyCap means the project already has as many live workers
	// as its configured cap allows, so a further worker spawn is refused. It is a
	// capacity condition, not a launch failure: Spawn returns it before creating
	// any durable state, so a refusal leaves nothing to roll back and marks no
	// candidate down — being busy is not evidence a harness is broken. Tracker
	// intake matches it with errors.Is to defer an issue rather than fail it.
	ErrWorkerConcurrencyCap = errors.New("session: worker concurrency cap reached")
	// ErrProjectPaused means the project — or the fleet — is paused, so a new
	// worker spawn is refused. Like the concurrency cap it is returned before any
	// durable state is created, so a refusal leaves nothing to roll back and marks
	// no candidate down. Orchestrator spawns and forced spawns are exempt. Tracker
	// intake matches it with errors.Is to skip quietly rather than fail the issue.
	ErrProjectPaused = errors.New("session: project paused")
	// ErrScratchBranchUnsupported means a caller tried to force git branch
	// semantics onto a scratch project.
	ErrScratchBranchUnsupported = errors.New("session: scratch projects do not support branches")
	// ErrNotResumable means a terminated session cannot be relaunched: its adapter
	// cannot natively resume it AND it has no prompt to fresh-launch from, and it is
	// not an orchestrator (orchestrators are promptless by design and relaunch fresh
	// with the system prompt only). Workers without a task and without a native
	// session id have nothing meaningful to restore.
	ErrNotResumable = errors.New("session: nothing to resume from")
	// ErrSwitchInProgress means an agent switch is already running for this
	// session. The API maps it to a 409 so a double-submit does not race two
	// teardown/relaunch cycles over one worktree.
	ErrSwitchInProgress = errors.New("session: switch already in progress")
	// ErrAwaitingDecision means the session is paused on a pending
	// permission/approval dialog. Send refuses to paste into it: the runtime
	// appends Enter after every paste, and an Enter into a decision dialog
	// would answer it on the user's behalf. The API maps it to a 409; the
	// caller retries once the user has answered in the terminal.
	ErrAwaitingDecision = errors.New("session: awaiting a user decision")
	// ErrModelHarnessMismatch means a requested model is known to belong to a
	// different provider than the resolved harness.
	ErrModelHarnessMismatch = agentconfig.ErrModelHarnessMismatch
	// ErrModelUnreachable means a fresh cached verdict definitively rejected the
	// resolved model or effort. Spawn returns it before creating durable state.
	ErrModelUnreachable = errors.New("session: resolved model selection is unreachable")
	// ErrPreflightWorkerUnsupported means Preflight was asked about a worker
	// spawn. Resolving a worker's mix bucket debits candidate health, so the
	// speculative entry point refuses the kind rather than mutating fleet state
	// for a spawn that may never happen.
	ErrPreflightWorkerUnsupported = errors.New("session: preflight does not support worker spawns")
	// ErrWorkspaceOwnedByLiveSession means a terminated session's recorded
	// workspace is currently held by a different LIVE session, so relaunching it
	// would put two runtimes on one worktree and one branch. Kill and Cleanup
	// already spare such a workspace; Restore refuses it. The API maps it to a
	// 409 — the row becomes restorable again the moment the live owner goes away.
	ErrWorkspaceOwnedByLiveSession = errors.New("session: workspace is owned by a live session")
)

// SpawnModelSelectionValidator supplies cache-only model and effort verdicts
// for the resolved launch pair. Implementations must never perform discovery or
// provider calls on this path.
type SpawnModelSelectionValidator interface {
	ValidateSpawnSelection(ctx context.Context, harness domain.AgentHarness, model string, effort domain.Effort) (ports.ModelValidationResult, error)
}

// Env vars a spawned process reads to learn who it is.
const (
	EnvSessionID = "AO_SESSION_ID"
	EnvProjectID = "AO_PROJECT_ID"
	EnvIssueID   = "AO_ISSUE_ID"
	// EnvRuntimeToken scopes activity hooks to the runtime generation that
	// launched them so stale hooks cannot mutate a replacement session.
	EnvRuntimeToken = "AO_RUNTIME_TOKEN" // #nosec G101 -- env var name, not a credential value.
	// EnvDataDir tells a spawned agent's AO hook commands where the store lives.
	EnvDataDir = "AO_DATA_DIR"
)

// candidateSurfaceWorkerMix names the worker-mix selection surface for candidate
// health. It keeps this surface's candidates from colliding with any other
// pooled-agent surface that shares the candidate-health vocabulary.
const candidateSurfaceWorkerMix = "worker_mix"

// hookBinaryName is the executable name the workspace hook commands invoke:
// every agent adapter installs a bare `ao hooks <agent> <event>`. The session
// PATH pin (hookPATH) only works when the daemon's own executable carries this
// name, since prepending its directory must change what `ao` resolves to.
const hookBinaryName = "ao"

type lifecycleRecorder interface {
	MarkSpawned(ctx context.Context, id domain.SessionID, metadata domain.SessionMetadata) error
	MarkTerminated(ctx context.Context, id domain.SessionID) error
}

type runtimeController interface {
	Create(ctx context.Context, cfg ports.RuntimeConfig) (ports.RuntimeHandle, error)
	Destroy(ctx context.Context, handle ports.RuntimeHandle) error
	GetOutput(ctx context.Context, handle ports.RuntimeHandle, lines int) (string, error)
	// IsAlive reports whether the handle's runtime session still exists. Used by
	// Reconcile on boot to adopt crash-surviving sessions and reap leaked ones.
	IsAlive(ctx context.Context, handle ports.RuntimeHandle) (bool, error)
}

type launchProcessProber interface {
	IsRunningCommand(context.Context, ports.RuntimeHandle, string) (bool, error)
}

// RestoreMode reports whether a restore continued an agent-native transcript or
// relaunched from AO's saved task prompt.
type RestoreMode string

const (
	// RestoreModeNative means AO relaunched through the agent's native transcript resume command.
	RestoreModeNative RestoreMode = "native"
	// RestoreModeSavedPrompt means AO relaunched a new conversation from the saved task prompt.
	RestoreModeSavedPrompt RestoreMode = "saved_prompt"
	// RestoreModeFresh means AO relaunched without a saved task prompt.
	RestoreModeFresh RestoreMode = "fresh"
)

// RestoreResult is the command result for a restored session.
type RestoreResult struct {
	Session domain.SessionRecord
	Mode    RestoreMode
}

// Store is the persistence surface needed by the internal session Manager.
type Store interface {
	// GetProject loads a project row so spawn can resolve its per-project agent
	// config into the launch command. ok=false means the project is unknown.
	GetProject(ctx context.Context, id string) (domain.ProjectRecord, bool, error)
	// GetFleetPaused reads the daemon-global fleet pause flag so the spawn guard
	// can refuse new worker sessions fleet-wide, independent of any project bit.
	GetFleetPaused(ctx context.Context) (bool, error)
	GetPrimeSettings(ctx context.Context) (domain.PrimeSettings, error)
	ListWorkspaceRepos(ctx context.Context, projectID string) ([]domain.WorkspaceRepoRecord, error)
	CreateSession(ctx context.Context, rec domain.SessionRecord) (domain.SessionRecord, error)
	UpdateSession(ctx context.Context, rec domain.SessionRecord) error
	GetSession(ctx context.Context, id domain.SessionID) (domain.SessionRecord, bool, error)
	ListSessions(ctx context.Context, project domain.ProjectID) ([]domain.SessionRecord, error)
	ListAllSessions(ctx context.Context) ([]domain.SessionRecord, error)
	// DeleteSession removes a session row only if it is still in seed state
	// (no workspace, runtime handle, agent session id, or prompt; not
	// terminated). Returns deleted=true when removal happened; deleted=false
	// when the row had already progressed past seed state — preserving the
	// no-resurrection guarantee for live sessions.
	DeleteSession(ctx context.Context, id domain.SessionID) (bool, error)
	// UpsertSessionWorktree records or updates the worktree row for a session.
	// SaveAndTeardownAll writes the preserved_ref here (even when empty) as the
	// "shutdown-saved" marker before ForceDestroying the worktree.
	UpsertSessionWorktree(ctx context.Context, row domain.SessionWorktreeRecord) error
	// ListSessionWorktrees returns every worktree row for a session. RestoreAll
	// uses this to identify sessions saved by the last SaveAndTeardownAll: the
	// presence of any row is the marker; preserved_ref may be empty for clean
	// worktrees.
	ListSessionWorktrees(ctx context.Context, id domain.SessionID) ([]domain.SessionWorktreeRecord, error)
	// DeleteSessionWorktrees consumes stale shutdown-restore markers. Explicit
	// Kill and successful RestoreAll must remove these rows to prevent
	// resurrecting sessions the user intentionally terminated.
	DeleteSessionWorktrees(ctx context.Context, id domain.SessionID) error
}

// Manager coordinates internal session spawn, restore, kill, and cleanup over
// the outbound ports. User-facing read-model assembly lives in the service package.
type Manager struct {
	runtime   runtimeController
	agents    ports.AgentResolver
	workspace ports.Workspace
	store     Store
	// messenger is a sessionguard.Guard wrapping the raw messenger, so every
	// pane write is guarded (re-read state, refuse a blocked session) without
	// each call site re-deriving the check. Send/confirmActive use Deliver for
	// its Outcome; Spawn/Restore use the interface-level Send for
	// initial-prompt delivery, where a blocked session is impossible.
	messenger *sessionguard.Guard
	lcm       lifecycleRecorder
	dataDir   string
	clock     func() time.Time
	// lookPath is exec.LookPath in production; tests substitute a stub so
	// they don't need real binaries on PATH. Returns ports.ErrAgentBinaryNotFound
	// when the binary is missing so the sentinel propagates through toAPIError.
	lookPath func(string) (string, error)
	// executable resolves the daemon's own binary (os.Executable in
	// production); its directory is prepended to spawned sessions' PATH so the
	// workspace hook commands resolve back to this daemon. Tests inject a stub.
	executable func() (string, error)
	// sendConfirm bounds the best-effort post-send confirmation that the session
	// actually became active (the agent accepted the prompt). New fills in the
	// sendConfirm* defaults; tests in this package shrink the timings directly.
	sendConfirm sendConfirmConfig
	// launchProbe bounds the process-health probe that rejects a spawn whose
	// agent exited immediately. New fills in the defaults; tests can shrink them.
	launchProbe launchProbeConfig
	logger      *slog.Logger
	// health is the worker-mix candidate-health circuit breaker. Selection calls
	// it to skip down buckets; Spawn marks a mix-selected bucket down on a
	// launch-attributable failure and recovers a bucket on a successful spawn of
	// its exact identity. The Tracker owns telemetry emission (it holds the sink
	// wired at daemon assembly), so the manager itself has no telemetry
	// dependency — it only calls health-policy methods.
	health *candidatehealth.Tracker
	// modelValidator consumes only previously cached model/catalog verdicts.
	modelValidator SpawnModelSelectionValidator
	// spawnLocks serializes spawn admission per project: live cap check,
	// worker-mix census and selection, resolved model validation, and seed-row
	// creation. A lock is released once the seed row exists so runtime launch
	// and workspace work do not serialize unnecessarily, and unrelated projects
	// can admit workers independently.
	spawnLocksMu sync.Mutex
	spawnLocks   map[domain.ProjectID]*sync.Mutex
}

// sendConfirmConfig bounds the best-effort activity-confirmation loop run after
// Send. AO has no delivery ack: ao send returns 200 the moment tmux send-keys
// exits 0, and for a large multiline paste the single Enter may not submit the
// prompt — so UserPromptSubmit never fires and the orchestrator cannot tell the
// worker started. confirmActive observes the durable Activity.State (written by
// the user-prompt-submit hook) and re-sends Enter until the session is active or
// the budget is exhausted. It never fails the send.
type sendConfirmConfig struct {
	// pollInterval is the gap between activity reads.
	pollInterval time.Duration
	// attemptDeadline is how long to wait for active after each Enter.
	attemptDeadline time.Duration
	// maxAttempts bounds how many times Enter is (re)sent, counting the initial
	// Enter from Send itself.
	maxAttempts int
}

// Production sendConfirm bounds: 3 Enters total (1 from Send + 2 re-sends),
// each given 2s to flip the session active, polled every 300ms.
const (
	sendConfirmPollInterval    = 300 * time.Millisecond
	sendConfirmAttemptDeadline = 2 * time.Second
	sendConfirmMaxAttempts     = 3

	launchCommandProbeRetryDelay = 200 * time.Millisecond
	launchCommandProbeAttempts   = 3
)

// launchProbeConfig bounds the post-launch process-health probe. A slow start
// is an infra condition, not agent death, so the probe retries over a bounded
// grace window before rejecting a spawn.
type launchProbeConfig struct {
	retryDelay time.Duration
	attempts   int
}

// Deps are the collaborators a Session Manager needs; New wires them together.
type Deps struct {
	Runtime   runtimeController
	Agents    ports.AgentResolver
	Workspace ports.Workspace
	Store     Store
	Messenger ports.AgentMessenger
	Lifecycle lifecycleRecorder
	// DataDir is exported to spawned agents as AO_DATA_DIR so their hook
	// commands can open the same store.
	DataDir string
	Clock   func() time.Time
	// LookPath overrides exec.LookPath for the pre-launch agent-binary check.
	// Production wiring leaves this nil and the manager defaults to
	// exec.LookPath; tests inject a stub so they need not seed real binaries.
	LookPath func(string) (string, error)
	// Executable overrides os.Executable for the session PATH pin (see
	// hookPATH). Production wiring leaves this nil; tests inject a stub so they
	// control what the test binary appears to be.
	Executable func() (string, error)
	// Logger receives spawn-time diagnostics (e.g. when the session PATH
	// cannot be pinned to the daemon binary). Nil defaults to slog.Default().
	Logger *slog.Logger
	// Health is the worker-mix candidate-health tracker. Daemon wiring
	// constructs it once with the telemetry sink so its down state persists
	// across spawns and its candidate_{down,recovered} events reach telemetry.
	// Nil defaults to a sink-less Tracker: candidate health still narrows and
	// recovers buckets, only the structured events are dropped.
	Health *candidatehealth.Tracker
	// ModelValidator is the unified agent model service's cache-only spawn view.
	ModelValidator SpawnModelSelectionValidator
}

// New builds a Session Manager from its dependencies, defaulting the clock to
// time.Now when Deps.Clock is nil.
func New(d Deps) *Manager {
	m := &Manager{
		runtime:    d.Runtime,
		agents:     d.Agents,
		workspace:  d.Workspace,
		store:      d.Store,
		lcm:        d.Lifecycle,
		dataDir:    d.DataDir,
		clock:      d.Clock,
		lookPath:   d.LookPath,
		executable: d.Executable,
		sendConfirm: sendConfirmConfig{
			pollInterval:    sendConfirmPollInterval,
			attemptDeadline: sendConfirmAttemptDeadline,
			maxAttempts:     sendConfirmMaxAttempts,
		},
		launchProbe: launchProbeConfig{
			retryDelay: launchCommandProbeRetryDelay,
			attempts:   launchCommandProbeAttempts,
		},
		logger:         d.Logger,
		health:         d.Health,
		modelValidator: d.ModelValidator,
		spawnLocks:     map[domain.ProjectID]*sync.Mutex{},
	}
	if m.health == nil {
		// A sink-less Tracker keeps selection narrowing and recovery working in
		// tests and any caller that does not wire telemetry; only the structured
		// candidate-health events are disabled.
		m.health = candidatehealth.New(candidatehealth.Config{Source: "session_manager"})
	}
	if m.clock == nil {
		// UTC so spawn-stamped CreatedAt/UpdatedAt match every other session
		// write (rename, activity) — all of which use time.Now().UTC(). A local
		// default produced mixed-timezone timestamps in `ao session get`.
		m.clock = func() time.Time { return time.Now().UTC() }
	}
	if m.lookPath == nil {
		m.lookPath = exec.LookPath
	}
	if m.executable == nil {
		m.executable = os.Executable
	}
	if m.logger == nil {
		m.logger = slog.Default()
	}
	// messenger is the raw d.Messenger wrapped in a Guard (needs m.logger, so it
	// is built after the logger default).
	m.messenger = sessionguard.New(d.Store, d.Messenger, m.logger)
	return m
}

// spawnPlan is what preflightSpawn resolves: the checked, normalized spawn
// config plus every derived value the durable half of Spawn needs. Returning
// them keeps the checks and the launch reading one resolution instead of two.
type spawnPlan struct {
	cfg              ports.SpawnConfig
	project          domain.ProjectRecord
	projectKind      domain.ProjectKind
	projectlessPrime bool
	agentConfig      domain.AgentConfig
	target           resolvedSpawnTarget
	prompt           string
	systemPrompt     string
	requestedHarness domain.AgentHarness
	requestedModel   string
}

// spawnPass distinguishes the speculative Preflight pass over the spawn
// preconditions from the real Spawn pass. Both run the identical checklist and
// reach the identical verdict; the pass only gates the once-per-spawn
// operator-facing logs, which would otherwise be emitted twice for a single
// role reconcile (which preflights, then spawns).
type spawnPass bool

const (
	realSpawn        spawnPass = false
	speculativeSpawn spawnPass = true
)

// preflightSpawn runs every spawn precondition that can be decided before any
// durable state exists, and resolves the values the rest of Spawn launches
// with. It is the ONLY place those checks live: Spawn calls it, and Preflight
// calls it, so a caller asking "could this spawn succeed?" is asking the exact
// code that will run the spawn. Duplicating the checklist is what let a
// reconciler retire a live role session for a replacement that could never be
// created.
//
// Nothing here writes durable state, and for the role kinds (prime,
// orchestrator) nothing here mutates in-memory state either: the two mutating
// steps on the spawn path — the per-project admission lock and the worker-mix
// candidate-health skip debit inside selectMixBucket — are both reached only
// under `cfg.Kind == domain.KindWorker`. The admission lock is therefore left
// to Spawn (see below), and mix selection simply does not run for a role
// preflight. A worker preflight is NOT side-effect free for that reason, which
// is why exported Preflight refuses the worker kind outright.
//
// Residual gap: preconditions that only become reachable after the seed row
// exists are still not covered — ErrProjectNotResolvable, the
// ports.ErrWorkspaceBranch* sentinels raised inside createSessionWorkspace, and
// ports.ErrAgentBinaryNotFound from the adapter's launch command. A caller that
// retires before spawning can still be caught by those.
func (m *Manager) preflightSpawn(ctx context.Context, cfg ports.SpawnConfig, pass spawnPass) (spawnPlan, error) {
	projectlessPrime := cfg.Kind == domain.KindPrime && cfg.ProjectID == ""
	project := domain.ProjectRecord{}
	var err error
	if !projectlessPrime {
		project, err = m.loadProject(ctx, cfg.ProjectID)
		if err != nil {
			return spawnPlan{}, fmt.Errorf("spawn: %w", err)
		}
	}
	// Refuse new work when the project or the fleet is paused, before any durable
	// state is created. This is the authoritative spawn-side gate that also covers
	// direct HTTP/CLI spawns bypassing tracker intake. Orchestrators and forced
	// spawns are exempt.
	if err := m.guardPaused(ctx, project, cfg); err != nil {
		return spawnPlan{}, err
	}
	requestedHarness := cfg.Harness
	requestedModel := strings.TrimSpace(cfg.Model)
	explicitSpawnModel := requestedModel != ""
	// Enforce the per-project live-worker cap before anything else: no durable
	// row, no workspace, no mix or candidate-health consultation. Placed ahead
	// of resolveSpawnTarget so a refusal at capacity leaves nothing to roll back
	// and never perturbs the mix census or a candidate's health. The cap is a
	// project-level ceiling on workers, so it applies to every worker spawn —
	// pinned or mix-selected alike — and an orchestrator is neither counted nor
	// limited by it.
	if cfg.Kind == domain.KindWorker && project.Config.MaxLiveWorkers > 0 {
		live, err := m.liveWorkerCount(ctx, cfg.ProjectID)
		if err != nil {
			return spawnPlan{}, fmt.Errorf("spawn: %w", err)
		}
		if live >= project.Config.MaxLiveWorkers {
			return spawnPlan{}, fmt.Errorf("spawn: %w: project %s at %d of %d live worker(s)", ErrWorkerConcurrencyCap, cfg.ProjectID, live, project.Config.MaxLiveWorkers)
		}
	}
	// Harness, model, and any worker-mix effort are resolved together up front so
	// the same native tuple is launched with and recorded on the session row.
	// Trimming and effort normalization on this path let the census match the
	// tuple stored on live sessions.
	target, err := m.resolveSpawnTarget(ctx, cfg, project)
	if err != nil {
		return spawnPlan{}, fmt.Errorf("spawn: %w", err)
	}
	projectKind := project.Kind.WithDefault()
	if projectKind == domain.ProjectKindScratch && strings.TrimSpace(cfg.Branch) != "" {
		return spawnPlan{}, fmt.Errorf("spawn: %w", ErrScratchBranchUnsupported)
	}
	cfg.Harness = target.harness
	cfg.Model = target.model

	// Reject an unknown harness before any durable state is created. Doing this
	// after CreateSession would leave a terminated orphan row and waste a
	// worktree on a spawn that can never launch.
	if _, ok := m.agents.Agent(cfg.Harness); !ok {
		return spawnPlan{}, fmt.Errorf("spawn: %w: %q", ErrUnknownHarness, cfg.Harness)
	}
	// Resolve the remaining launch config before the model gate so effort is
	// validated as part of the exact native selection. This is the same resolver
	// result used later for adapter launch; only the already-resolved model is
	// overlaid for explicit pins and worker-mix buckets.
	agentConfig := domain.AgentConfig{}
	if projectlessPrime {
		settings, err := m.store.GetPrimeSettings(ctx)
		if err != nil {
			return spawnPlan{}, fmt.Errorf("spawn: prime settings: %w", err)
		}
		// Prime is projectless: these settings are the only source its launch
		// config has, and WithDefaults is what fills an unset field. The
		// effective mode is reported by GET /prime/settings, which applies the
		// same defaulting, so it needs no separate spawn-time disclosure.
		agentConfig = settings.WithDefaults().AgentConfig
		if agentConfig.Effort == "" {
			agentConfig.Effort = target.effort
		}
	} else {
		agentConfig, err = agentconfig.Effective(cfg.Kind, project.Config, "", cfg.Harness)
		if err != nil {
			return spawnPlan{}, fmt.Errorf("spawn: agent config: %w", err)
		}
	}
	if target.mixSelected {
		agentConfig.Effort = target.effort
	}
	if err := m.validateSpawnSelection(ctx, cfg.Harness, cfg.Model, agentConfig.Effort, spawnModelSource(explicitSpawnModel, target.mixSelected), pass); err != nil {
		return spawnPlan{}, fmt.Errorf("spawn: %w", err)
	}

	if err := m.validateRuntimePrerequisites(); err != nil {
		return spawnPlan{}, fmt.Errorf("spawn: %w", err)
	}

	prompt, systemPrompt, err := m.buildSpawnTexts(ctx, cfg)
	if err != nil {
		return spawnPlan{}, fmt.Errorf("spawn: prompt: %w", err)
	}

	// The daemon owns the name, and it is settled here — before the row exists —
	// so the persisted name, the launch command, and any harness write all read
	// the same single value.
	cfg.DisplayName = m.resolveDisplayName(cfg, project)

	return spawnPlan{
		cfg:              cfg,
		project:          project,
		projectKind:      projectKind,
		projectlessPrime: projectlessPrime,
		agentConfig:      agentConfig,
		target:           target,
		prompt:           prompt,
		systemPrompt:     systemPrompt,
		requestedHarness: requestedHarness,
		requestedModel:   requestedModel,
	}, nil
}

// Preflight reports whether a spawn's preconditions hold right now, creating
// nothing. It runs preflightSpawn and discards the resolved plan, so it can
// never drift from what Spawn actually enforces.
//
// This exists for callers that must tear something down to make room for the
// spawn — role reconciliation retiring the live orchestrator or Prime — and so
// must learn about a deterministic refusal before, not after, the teardown.
//
// Workers are refused with ErrPreflightWorkerUnsupported: resolving a worker's
// mix bucket debits the candidate-health skip ledger, so a speculative worker
// preflight would create nothing but still change fleet state. The refusal is
// here rather than in a doc note because "creates nothing" is the whole value
// of this method, and a contract only callers can keep is one a caller will
// eventually break. Workers have no use for it anyway — nothing is retired to
// make room for a worker.
func (m *Manager) Preflight(ctx context.Context, cfg ports.SpawnConfig) error {
	if cfg.Kind == domain.KindWorker {
		return fmt.Errorf("preflight: %w", ErrPreflightWorkerUnsupported)
	}
	_, err := m.preflightSpawn(ctx, cfg, speculativeSpawn)
	return err
}

// Spawn creates the session row (which assigns the "{project}-{n}" id), then the
// workspace and runtime, then reports completion to the LCM. If workspace
// materialization fails the still-seed row is deleted outright; a later failure
// parks the row as terminated and rolls back what was built.
func (m *Manager) Spawn(ctx context.Context, cfg ports.SpawnConfig) (domain.SessionRecord, int, int, error) {
	// Worker admission is serialized across the whole preflight-and-create
	// window: the live-worker cap and the worker-mix census both read state that
	// CreateSession then changes, so a competing spawn for the same project must
	// not land between the check and the row. Held here rather than inside
	// preflightSpawn so the preflight itself stays free of side effects.
	var unlockSpawnAdmission func()
	if cfg.Kind == domain.KindWorker {
		unlockSpawnAdmission = m.lockSpawnAdmission(cfg.ProjectID)
		defer func() {
			if unlockSpawnAdmission != nil {
				unlockSpawnAdmission()
			}
		}()
	}

	plan, err := m.preflightSpawn(ctx, cfg, realSpawn)
	if err != nil {
		return domain.SessionRecord{}, 0, 0, err
	}
	cfg = plan.cfg
	project := plan.project
	projectKind := plan.projectKind
	projectlessPrime := plan.projectlessPrime
	agentConfig := plan.agentConfig
	target := plan.target
	requestedHarness := plan.requestedHarness
	requestedModel := plan.requestedModel
	prompt := plan.prompt
	systemPrompt := plan.systemPrompt
	promptBytes := len(prompt)
	systemPromptBytes := len(systemPrompt)

	rec, err := m.store.CreateSession(ctx, seedRecord(cfg, agentConfig.Effort, target.mixSelected, target.mixBucketModel, m.clock()))
	if err != nil {
		return domain.SessionRecord{}, 0, 0, fmt.Errorf("spawn: create: %w", err)
	}
	if unlockSpawnAdmission != nil {
		unlockSpawnAdmission()
		unlockSpawnAdmission = nil
	}
	id := rec.ID
	systemPromptFile, err := m.prepareSystemPromptFile(id, cfg.Harness, systemPrompt)
	if err != nil {
		m.rollbackSpawnSeedRow(ctx, id)
		return domain.SessionRecord{}, 0, 0, fmt.Errorf("spawn %s: system prompt file: %w", id, err)
	}

	branch := cfg.Branch
	if branch == "" {
		branch = DefaultSpawnBranch(id, cfg.Kind, sessionPrefix(project), projectKind, m.dataDir)
	}
	ws, workspaceProject, err := m.createSessionWorkspace(ctx, project, cfg, id, branch)
	if err != nil {
		// Nothing observable exists yet — no worktree, no runtime — so the seed
		// row is deleted outright instead of accumulating as a terminated orphan
		// in session lists (e.g. when gitworktree refuses the branch).
		m.rollbackSpawnSeedRow(ctx, id)
		return domain.SessionRecord{}, 0, 0, fmt.Errorf("spawn %s: workspace: %w", id, err)
	}

	// Per-project workspace provisioning: symlink shared files, then run any
	// post-create commands (e.g. `pnpm install`) before the agent launches.
	if !projectlessPrime {
		if err := m.provisionWorkspace(ctx, project, ws.Path); err != nil {
			m.destroySpawnWorkspace(ctx, ws, workspaceProject)
			m.rollbackSpawnSeedRow(ctx, id)
			return domain.SessionRecord{}, 0, 0, fmt.Errorf("spawn %s: provision: %w", id, err)
		}
	}

	// CLI agents receive the prompt as text and cannot consume inline binary
	// data, so any pasted/dropped images are written into the worktree and
	// referenced by path in the prompt. Done after provisioning (so the worktree
	// exists) and before the launch command is built (so the references reach
	// the agent).
	if len(cfg.Attachments) > 0 {
		refs, err := writeSpawnAttachments(ws.Path, cfg.Attachments)
		if err != nil {
			m.destroySpawnWorkspace(ctx, ws, workspaceProject)
			m.rollbackSpawnSeedRow(ctx, id)
			return domain.SessionRecord{}, 0, 0, fmt.Errorf("spawn %s: attachments: %w", id, err)
		}
		// Keep the attachments dir out of git status. Best-effort: the images are
		// already written and usable, so an exclude failure must not fail the spawn.
		if err := m.workspace.AddExclude(ctx, ws, "/"+attachmentsDir+"/"); err != nil {
			m.logger.Warn("spawn: exclude attachments dir", "sessionID", id, "error", err)
		}
		prompt = appendAttachmentReferences(prompt, refs)
	}

	agent, ok := m.agents.Agent(cfg.Harness)
	if !ok {
		m.destroySpawnWorkspace(ctx, ws, workspaceProject)
		m.rollbackSpawnSeedRow(ctx, id)
		return domain.SessionRecord{}, 0, 0, fmt.Errorf("spawn %s: no agent adapter for harness %q", id, cfg.Harness)
	}
	agentConfig.Model = launchModelForBucket(cfg.Harness, cfg.Model, target.mixSelected)
	runtimeToken, err := newRuntimeToken()
	if err != nil {
		m.rollbackSpawnSeedRow(ctx, id)
		return domain.SessionRecord{}, 0, 0, fmt.Errorf("spawn %s: runtime token: %w", id, err)
	}
	env := m.runtimeEnv(id, cfg.ProjectID, cfg.IssueID, runtimeToken, project.Config.Env)
	m.augmentAgentRuntimeEnv(agent, env)
	if err := m.prepareWorkspace(ctx, agent, id, ws.Path, systemPrompt, systemPromptFile, agentConfig, env); err != nil {
		m.markMixCandidateDown(ctx, target.mixSelected, cfg, agentConfig.Effort, err)
		m.destroySpawnWorkspace(ctx, ws, workspaceProject)
		m.rollbackSpawnSeedRow(ctx, id)
		return domain.SessionRecord{}, 0, 0, fmt.Errorf("spawn %s: %w", id, err)
	}
	launchCfg := ports.LaunchConfig{
		DataDir:          m.dataDir,
		DisplayName:      cfg.DisplayName,
		SessionID:        string(id),
		WorkspacePath:    ws.Path,
		Kind:             cfg.Kind,
		Prompt:           prompt,
		SystemPrompt:     systemPrompt,
		SystemPromptFile: systemPromptFile,
		IssueID:          string(cfg.IssueID),
		Config:           agentConfig,
		Permissions:      agentConfig.Permissions,
	}
	delivery, err := agent.GetPromptDeliveryStrategy(ctx, launchCfg)
	if err != nil {
		m.rollbackPreparedSpawnWorkspace(ctx, rec, ws, workspaceProject)
		m.rollbackSpawnSeedRow(ctx, id)
		return domain.SessionRecord{}, 0, 0, fmt.Errorf("spawn %s: prompt delivery: %w", id, err)
	}
	if delivery == ports.PromptDeliveryAfterStart {
		launchCfg.Prompt = ""
	}
	argv, err := agent.GetLaunchCommand(ctx, launchCfg)
	if err != nil {
		// An adapter that cannot resolve its binary reports ErrAgentBinaryNotFound
		// from GetLaunchCommand itself (e.g. copilot), which is the same
		// candidate fault validateAgentBinary catches below — mark the bucket
		// down here too, or a broken bucket stays healthy and is reselected
		// forever. Other launch-command errors (prompt/config) are not candidate
		// faults. Mark down before rollback so caller-cancellation is evaluated at
		// failure time, not after cleanup may have let the context expire.
		if errors.Is(err, ports.ErrAgentBinaryNotFound) {
			m.markMixCandidateDown(ctx, target.mixSelected, cfg, agentConfig.Effort, err)
		}
		m.rollbackPreparedSpawnWorkspace(ctx, rec, ws, workspaceProject)
		m.rollbackSpawnSeedRow(ctx, id)
		return domain.SessionRecord{}, 0, 0, fmt.Errorf("spawn %s: launch command: %w", id, err)
	}
	// Pre-flight: confirm argv[0] actually exists on PATH (or as an absolute
	// path the adapter returned) BEFORE handing the launch to the runtime.
	// tmux happily creates a session+pane around a missing command, so an
	// unresolved binary would leak through as a "live" session that never ran.
	if err := m.validateAgentBinary(argv); err != nil {
		// Mark down before rollback: MarkDownForAttempt reads the caller context,
		// and rollback (workspace destroy + seed delete) can run long enough for a
		// live-at-failure context to expire, which would wrongly suppress the fault.
		m.markMixCandidateDown(ctx, target.mixSelected, cfg, agentConfig.Effort, err)
		m.rollbackPreparedSpawnWorkspace(ctx, rec, ws, workspaceProject)
		m.rollbackSpawnSeedRow(ctx, id)
		return domain.SessionRecord{}, 0, 0, fmt.Errorf("spawn %s: %w", id, err)
	}
	m.augmentRuntimePATHForLaunchBinary(ctx, env, argv)
	handle, err := m.runtime.Create(ctx, ports.RuntimeConfig{
		SessionID:     id,
		WorkspacePath: ws.Path,
		Argv:          argv,
		Env:           env,
	})
	if err != nil {
		m.markMixCandidateDown(ctx, target.mixSelected, cfg, agentConfig.Effort, err)
		m.rollbackPreparedSpawnWorkspace(ctx, rec, ws, workspaceProject)
		m.rollbackSpawnSeedRow(ctx, id)
		return domain.SessionRecord{}, 0, 0, fmt.Errorf("spawn %s: runtime: %w", id, err)
	}

	if err := m.verifyLaunchCommandRunning(ctx, handle, launchProcessCommand(argv)); err != nil {
		// Mark down before rollback: attribution is decided at failure time, and
		// MarkDownForAttempt reads the caller context. Rollback runs detached, so
		// it legitimately outlives a caller that has gone away — marking down
		// afterwards would let that departure erase the evidence.
		m.markMixCandidateDown(ctx, target.mixSelected, cfg, agentConfig.Effort, err)
		m.destroyFailedLaunchRuntime(ctx, handle)
		m.rollbackPreparedSpawnWorkspace(ctx, rec, ws, workspaceProject)
		m.rollbackSpawnSeedRow(ctx, id)
		return domain.SessionRecord{}, 0, 0, fmt.Errorf("spawn %s: launch process: %w", id, err)
	}
	metadata := domain.SessionMetadata{
		Branch:            ws.Branch,
		WorkspacePath:     ws.Path,
		WorkspaceRepoPath: ws.RepoPath,
		RuntimeHandleID:   handle.ID,
		RuntimeToken:      runtimeToken,
		LaunchCommand:     launchProcessCommand(argv),
		Prompt:            prompt,
		PromptPolicyHash:  promptPolicyHash(systemPrompt),
	}
	if err := m.lcm.MarkSpawned(ctx, id, metadata); err != nil {
		m.destroyFailedLaunchRuntime(ctx, handle)
		m.rollbackPreparedSpawnWorkspace(ctx, rec, ws, workspaceProject)
		m.markSpawnFailedTerminated(ctx, id)
		return domain.SessionRecord{}, 0, 0, fmt.Errorf("spawn %s: completed: %w", id, err)
	}
	// Name before prompt: the rename is instant, and leaving the prompt as the
	// last thing written means the session ends up working on its task rather
	// than with a rename typed into a turn already in flight.
	if err := m.deliverNameForSpawn(ctx, agent, launchCfg, handle, id, cfg.DisplayName); err != nil {
		if !m.forgiveSpawnNameFailure(ctx, handle, id, err) {
			m.destroyFailedLaunchRuntime(ctx, handle)
			m.rollbackPreparedSpawnWorkspace(ctx, rec, ws, workspaceProject)
			m.markSpawnFailedTerminatedWithoutWorkspace(ctx, id)
			return domain.SessionRecord{}, 0, 0, fmt.Errorf("spawn %s: deliver name: %w", id, err)
		}
	}
	if delivery == ports.PromptDeliveryAfterStart && prompt != "" {
		if err := m.deliverAfterStartPrompt(ctx, agent, launchCfg, handle, id, prompt); err != nil {
			m.markMixCandidateDown(ctx, target.mixSelected, cfg, agentConfig.Effort, err)
			m.destroyFailedLaunchRuntime(ctx, handle)
			m.rollbackPreparedSpawnWorkspace(ctx, rec, ws, workspaceProject)
			m.markSpawnFailedTerminatedWithoutWorkspace(ctx, id)
			return domain.SessionRecord{}, 0, 0, fmt.Errorf("spawn %s: deliver prompt: %w", id, err)
		}
	}
	if err := m.verifyLaunchCommandRunning(ctx, handle, launchProcessCommand(argv)); err != nil {
		// Mark down before rollback, for the reason on the probe branch above.
		m.markMixCandidateDown(ctx, target.mixSelected, cfg, agentConfig.Effort, err)
		m.destroyFailedLaunchRuntime(ctx, handle)
		m.rollbackPreparedSpawnWorkspace(ctx, rec, ws, workspaceProject)
		m.markSpawnFailedTerminatedWithoutWorkspace(ctx, id)
		return domain.SessionRecord{}, 0, 0, fmt.Errorf("spawn %s: launch process: %w", id, err)
	}
	m.recoverMixCandidate(project.Config, cfg, requestedHarness, requestedModel, agentConfig.Effort, target.mixSelected)
	rec, err = m.getRecord(ctx, id)
	if err != nil {
		return domain.SessionRecord{}, 0, 0, err
	}
	return rec, promptBytes, systemPromptBytes, nil
}

// loadProject loads the project record so spawn can resolve its per-project
// config (harness/agent overrides, env, branch, rules, provisioning). A missing
// project yields a zero record rather than an error: the project may be
// unregistered yet still have live sessions, and an empty config simply means
// every field falls back to its default.
// guardPaused refuses a worker spawn when the project or the fleet is paused.
// Orchestrator spawns and forced spawns are exempt so supervision keeps running
// and an operator retains a manual override. As an authoritative safety gate the
// fleet-flag read fails CLOSED: if the persisted flag cannot be read the spawn is
// refused (surfacing the error) rather than proceeding as though the fleet were
// running. Only the read-only display paths fail open, so a storage blip cannot
// silently un-pause work.
func (m *Manager) guardPaused(ctx context.Context, project domain.ProjectRecord, cfg ports.SpawnConfig) error {
	if cfg.Force || cfg.Kind == domain.KindOrchestrator || cfg.Kind == domain.KindPrime {
		return nil
	}
	scope := ""
	if project.Paused {
		scope = "project"
	} else {
		fleetPaused, err := m.store.GetFleetPaused(ctx)
		if err != nil {
			return fmt.Errorf("get fleet paused: %w", err)
		}
		if fleetPaused {
			scope = "fleet"
		}
	}
	if scope == "" {
		return nil
	}
	return fmt.Errorf("spawn: %w: %s scope; resume it or pass Force to override", ErrProjectPaused, scope)
}

func (m *Manager) loadProject(ctx context.Context, projectID domain.ProjectID) (domain.ProjectRecord, error) {
	row, ok, err := m.store.GetProject(ctx, string(projectID))
	if err != nil {
		return domain.ProjectRecord{}, fmt.Errorf("load project: %w", err)
	}
	if !ok {
		return domain.ProjectRecord{}, nil
	}
	return row, nil
}

func (m *Manager) createSessionWorkspace(ctx context.Context, project domain.ProjectRecord, cfg ports.SpawnConfig, id domain.SessionID, branch string) (ports.WorkspaceInfo, *ports.WorkspaceProjectInfo, error) {
	if cfg.Kind == domain.KindPrime && cfg.ProjectID == "" {
		ws, err := m.workspace.Create(ctx, ports.WorkspaceConfig{
			ProjectID:     "",
			SessionID:     id,
			Kind:          cfg.Kind,
			SessionPrefix: "prime",
			Branch:        branch,
			BaseBranch:    domain.DefaultBranchName,
			RepoPath:      fleetPrimeRepoPath(m.dataDir),
		})
		return ws, nil, err
	}
	projectKind := project.Kind.WithDefault()
	if projectKind != domain.ProjectKindWorkspace {
		baseBranch := project.Config.WithDefaults().DefaultBranch
		if projectKind == domain.ProjectKindScratch {
			baseBranch = ""
		}
		ws, err := m.workspace.Create(ctx, ports.WorkspaceConfig{
			ProjectID:     cfg.ProjectID,
			SessionID:     id,
			Kind:          cfg.Kind,
			SessionPrefix: sessionPrefix(project),
			Branch:        branch,
			BaseBranch:    baseBranch,
		})
		return ws, nil, err
	}
	workspaceProject, ok := m.workspace.(ports.WorkspaceProject)
	if !ok {
		return ports.WorkspaceInfo{}, nil, errors.New("workspace project materialization is not supported by workspace adapter")
	}
	repos, err := m.store.ListWorkspaceRepos(ctx, project.ID)
	if err != nil {
		return ports.WorkspaceInfo{}, nil, err
	}
	childRepos := make([]ports.WorkspaceProjectRepoConfig, 0, len(repos))
	for _, repo := range repos {
		childRepos = append(childRepos, ports.WorkspaceProjectRepoConfig{
			Name:         repo.Name,
			RelativePath: repo.RelativePath,
			RepoPath:     filepath.Join(project.Path, filepath.FromSlash(repo.RelativePath)),
		})
	}
	info, err := workspaceProject.CreateWorkspaceProject(ctx, ports.WorkspaceProjectConfig{
		ProjectID:     cfg.ProjectID,
		SessionID:     id,
		Kind:          cfg.Kind,
		SessionPrefix: sessionPrefix(project),
		Branch:        branch,
		RootRepoPath:  project.Path,
		BaseBranch:    project.Config.WithDefaults().DefaultBranch,
		Repos:         childRepos,
	})
	if err != nil {
		return ports.WorkspaceInfo{}, nil, err
	}
	for _, wt := range info.Worktrees {
		if err := m.store.UpsertSessionWorktree(ctx, domain.SessionWorktreeRecord{
			SessionID:    id,
			RepoName:     wt.RepoName,
			RepoPath:     wt.RepoPath,
			Branch:       wt.Branch,
			BaseSHA:      wt.BaseSHA,
			WorktreePath: wt.Path,
			State:        "active",
		}); err != nil {
			m.destroyPartialWorkspaceProject(ctx, workspaceProject, info, id)
			return ports.WorkspaceInfo{}, nil, fmt.Errorf("record workspace worktree %q: %w", wt.RepoName, err)
		}
	}
	return info.Root, &info, nil
}

// spawnCleanupTimeout bounds one compensating teardown step — a runtime
// destroy, a workspace teardown, a terminal-state write. Long enough for a tmux
// destroy and a git worktree removal, short enough that a wedged step cannot
// pin a request goroutine open indefinitely. A rollback branch runs a few steps
// in sequence, so its total is a small multiple of this; the guarantee is that
// no single step hangs forever, not that a branch finishes within 30s.
const spawnCleanupTimeout = 30 * time.Second

// cleanupContextKey marks a context already produced by cleanupContext.
type cleanupContextKey struct{}

// cleanupContext derives the context that compensating teardown runs on.
//
// Rollback must outlive the caller's cancellation. An HTTP client that
// disconnects mid-spawn cancels the request context, the in-flight step then
// fails with that same ctx.Err(), and teardown performed on the dead context
// would skip the very runtime session, worktree, and terminal-state write the
// rollback exists to perform — leaving exactly the orphan it was meant to
// prevent. Detached from cancellation, but bounded, so teardown still cannot
// run forever.
//
// Re-deriving from a context that is already a cleanup context returns it
// unchanged. Teardown helpers call each other, and context.WithoutCancel drops
// the parent's deadline along with its cancellation — so a fresh derive per
// nesting level would restart the clock and multiply the bound by the nesting
// depth. Sharing the outermost budget keeps spawnCleanupTimeout meaning what it
// says: one bound for the chain rooted at whichever helper derived it.
func cleanupContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx.Value(cleanupContextKey{}) != nil {
		return ctx, func() {}
	}
	detached := context.WithValue(context.WithoutCancel(ctx), cleanupContextKey{}, struct{}{})
	return context.WithTimeout(detached, spawnCleanupTimeout)
}

// destroyPartialWorkspaceProject compensates a workspace project whose worktree
// bookkeeping failed partway through: the on-disk project goes away together
// with the session_worktree rows already recorded for earlier repos.
//
// The rows usually cascade off the seed row (session_worktrees.session_id is
// ON DELETE CASCADE), so the explicit delete is what covers any path where the
// seed row is not deleted — rollbackSpawnSeedRow parks the row terminated when
// the delete fails or the row has already progressed past seed state, and the
// cascade then never fires. Those rows are 'active', so they are not restorable
// and no boot restore resurrects them; the cost of leaving them is a store that
// disagrees with the disk, not a failed restore.
//
// The rows are dropped only once the disk teardown has actually succeeded. If
// DestroyWorkspaceProject failed, the worktrees are still there and these rows
// are the only record of where — deleting them would strand directories that
// nothing can later find.
func (m *Manager) destroyPartialWorkspaceProject(ctx context.Context, adapter ports.WorkspaceProject, info ports.WorkspaceProjectInfo, id domain.SessionID) {
	cleanupCtx, cancel := cleanupContext(ctx)
	defer cancel()
	if err := adapter.DestroyWorkspaceProject(cleanupCtx, info); err != nil {
		m.logger.Warn("rollback: destroy partial workspace project; keeping worktree rows so cleanup can still find it",
			"sessionID", id, "error", err)
		return
	}
	_ = m.store.DeleteSessionWorktrees(cleanupCtx, id)
}

// destroyFailedLaunchRuntime tears down the runtime session a failed spawn or
// relaunch already created. Detached from the caller's cancellation for the
// reason in cleanupContext: the common trigger for these rollbacks is that the
// caller went away, and a skipped destroy leaks a live agent session.
func (m *Manager) destroyFailedLaunchRuntime(ctx context.Context, handle ports.RuntimeHandle) {
	cleanupCtx, cancel := cleanupContext(ctx)
	defer cancel()
	if err := m.runtime.Destroy(cleanupCtx, handle); err != nil {
		m.logger.Warn("rollback: destroy runtime session", "handle", handle.ID, "error", err)
	}
}

func (m *Manager) destroySpawnWorkspace(ctx context.Context, ws ports.WorkspaceInfo, workspaceProject *ports.WorkspaceProjectInfo) bool {
	cleanupCtx, cancel := cleanupContext(ctx)
	defer cancel()
	if workspaceProject != nil {
		if adapter, ok := m.workspace.(ports.WorkspaceProject); ok {
			err := adapter.DestroyWorkspaceProject(cleanupCtx, *workspaceProject)
			_ = m.store.DeleteSessionWorktrees(cleanupCtx, ws.SessionID)
			return err == nil
		}
	}
	err := m.workspace.Destroy(cleanupCtx, ws)
	_ = m.store.DeleteSessionWorktrees(cleanupCtx, ws.SessionID)
	return err == nil
}

func (m *Manager) rollbackPreparedSpawnWorkspace(ctx context.Context, rec domain.SessionRecord, ws ports.WorkspaceInfo, workspaceProject *ports.WorkspaceProjectInfo) {
	// Derived here rather than left to the two callees: it makes this pair of
	// teardowns share one cleanup budget instead of restarting the clock each.
	cleanupCtx, cancel := cleanupContext(ctx)
	defer cancel()
	if m.destroySpawnWorkspace(cleanupCtx, ws, workspaceProject) {
		m.cleanupAgentWorkspace(cleanupCtx, rec, ws.Path)
	}
}

type resolvedSpawnTarget struct {
	harness        domain.AgentHarness
	model          string
	effort         domain.Effort
	mixBucketModel string
	mixSelected    bool
}

// effectiveHarness resolves the harness for a spawn: an explicit harness wins;
// otherwise the project's role override for the session kind applies. Empty is
// invalid for new worker/orchestrator launches and is rejected by Spawn.
// resolveSpawnTarget resolves the (harness, model) pair a spawn launches with,
// and reports whether the worker mix is what chose it.
//
// A request that pins a harness is honored as given and the worker mix is never
// consulted for it. A request that pins only a model still lets the mix choose
// the harness, then overlays the explicit model onto that selected bucket. When
// no harness is pinned and a configured mix owns the choice for worker spawns,
// the selected bucket supplies the harness/effort and, unless an explicit spawn
// model overrides it, the model. With no mix configured the resolution is the
// pre-existing role/project fallback, unchanged.
//
// The mixSelected result is returned rather than re-derived downstream because
// this is the only point where it is knowable: a pin naming exactly a configured
// bucket yields the same pair as a selection, so nothing on the resulting row
// distinguishes the two after the fact.
func (m *Manager) resolveSpawnTarget(ctx context.Context, cfg ports.SpawnConfig, project domain.ProjectRecord) (resolvedSpawnTarget, error) {
	if cfg.Kind == domain.KindPrime && cfg.ProjectID == "" {
		settings, err := m.store.GetPrimeSettings(ctx)
		if err != nil {
			return resolvedSpawnTarget{}, err
		}
		settings = settings.WithDefaults()
		harness := cfg.Harness
		if harness == "" {
			harness = settings.Harness
		}
		if harness == "" {
			return resolvedSpawnTarget{}, fmt.Errorf("%w: configure fleet prime agent or pass --harness", ErrMissingHarness)
		}
		model := strings.TrimSpace(cfg.Model)
		if model == "" {
			model = strings.TrimSpace(settings.AgentConfig.Model)
		}
		effort := cfg.Effort
		if effort == "" {
			effort = settings.AgentConfig.Effort
		}
		return resolvedSpawnTarget{harness: harness, model: model, effort: effort}, nil
	}
	pinnedHarness := cfg.Harness != ""
	if !pinnedHarness && cfg.Kind == domain.KindWorker && len(project.Config.WorkerMix) > 0 {
		mix, err := effectiveWorkerMix(project.Config)
		if err != nil {
			return resolvedSpawnTarget{}, err
		}
		entry, err := m.selectMixBucket(ctx, cfg.ProjectID, mix, strings.TrimSpace(cfg.Model))
		if err != nil {
			return resolvedSpawnTarget{}, err
		}
		bucket := entry.BucketKey()
		model := bucket.Model
		if explicitModel := strings.TrimSpace(cfg.Model); explicitModel != "" {
			model = explicitModel
		}
		return resolvedSpawnTarget{
			harness:        bucket.Harness,
			model:          model,
			effort:         bucket.Effort,
			mixBucketModel: bucket.Model,
			mixSelected:    true,
		}, nil
	}
	// A per-project role override picks the harness when the spawn names none,
	// so a project can default workers to one agent and orchestrators to another.
	harness := effectiveHarness(cfg.Harness, cfg.Kind, project.Config)
	if harness == "" {
		return resolvedSpawnTarget{}, fmt.Errorf("%w: configure project %s.agent or pass --harness", ErrMissingHarness, roleConfigName(cfg.Kind))
	}
	resolved, err := agentconfig.Effective(cfg.Kind, project.Config, cfg.Model, harness)
	if err != nil {
		return resolvedSpawnTarget{}, err
	}
	return resolvedSpawnTarget{harness: harness, model: strings.TrimSpace(resolved.Model)}, nil
}

// effectiveWorkerMix resolves blank effort through the same worker/project
// configuration path used by launch, while explicit effort wins and is
// normalized to the harness's native vocabulary. Selection and census then
// operate on the exact effort persisted on the session rather than on an
// inheritance marker whose meaning can change with project configuration.
func effectiveWorkerMix(cfg domain.ProjectConfig) (domain.WorkerMix, error) {
	mix := make(domain.WorkerMix, len(cfg.WorkerMix))
	copy(mix, cfg.WorkerMix)
	for i := range mix {
		entry := &mix[i]
		if entry.Effort != "" {
			entry.Effort = domain.NormalizeEffortForHarness(entry.Harness, entry.Effort)
			continue
		}
		resolved, err := agentconfig.Effective(domain.KindWorker, cfg, entry.Model, entry.Harness)
		if err != nil {
			return nil, fmt.Errorf("workerMix[%d]: resolve inherited effort: %w", i, err)
		}
		entry.Effort = resolved.Effort
	}
	if err := mix.Validate(); err != nil {
		return nil, fmt.Errorf("resolved worker mix: %w", err)
	}
	return mix, nil
}

// validateSpawnSelection gates the resolved native selection. The verdict is
// identical on both passes; only the "unavailable" warning is suppressed on the
// speculative one, so a role reconcile — which preflights and then spawns —
// logs it once, from the pass that actually launches.
func (m *Manager) validateSpawnSelection(ctx context.Context, harness domain.AgentHarness, model string, effort domain.Effort, source string, pass spawnPass) error {
	if m.modelValidator == nil {
		return nil
	}
	result, err := m.modelValidator.ValidateSpawnSelection(ctx, harness, model, effort)
	if err != nil {
		if pass == realSpawn {
			m.warnSpawnSelectionUnavailable(harness, model, effort, source, err.Error())
		}
		return nil
	}
	if result.Status == ports.ModelValidationUnreachable {
		reason := strings.TrimSpace(result.Message)
		if reason == "" {
			reason = "the provider rejected this model selection"
		}
		return fmt.Errorf("%w: harness %q model %q effort %q from %s: %s", ErrModelUnreachable, harness, model, effort, source, reason)
	}
	if result.Status != ports.ModelValidationReachable {
		reason := strings.TrimSpace(result.Message)
		if reason == "" {
			reason = "no fresh cached model selection verdict"
		}
		if pass == realSpawn {
			m.warnSpawnSelectionUnavailable(harness, model, effort, source, reason)
		}
	}
	return nil
}

func (m *Manager) warnSpawnSelectionUnavailable(harness domain.AgentHarness, model string, effort domain.Effort, source, reason string) {
	m.logger.Warn("model validation unavailable; continuing spawn",
		"harness", harness,
		"model", model,
		"effort", effort,
		"source", source,
		"reason", reason,
	)
}

func spawnModelSource(explicitModel, mixSelected bool) string {
	if explicitModel {
		return "explicit spawn model"
	}
	if mixSelected {
		return "worker-mix bucket"
	}
	return "project/role config"
}

// selectMixBucket picks the bucket the next unpinned worker spawn launches on.
//
// The candidate set is the configured mix. Candidate health does not narrow it
// before selection: skip debit is folded into the census so a down bucket's share
// is preserved instead of silently reallocated to healthy buckets.
func (m *Manager) selectMixBucket(ctx context.Context, project domain.ProjectID, mix domain.WorkerMix, explicitModel string) (domain.WorkerMixEntry, error) {
	census, err := m.mixCensus(ctx, project, mix)
	if err != nil {
		return domain.WorkerMixEntry{}, err
	}
	m.applyWorkerMixSkipped(census, mix, explicitModel)
	entry, ok := mix.Select(census)
	if !ok {
		return domain.WorkerMixEntry{}, fmt.Errorf("%w: project %s configures %d bucket(s), none selectable", ErrWorkerMixExhausted, project, len(mix))
	}
	bk := selectedWorkerMixKey(entry, explicitModel)
	if m.health.RecordSkipIfDown(workerMixCandidate(bk.Harness, bk.Model, bk.Effort)) {
		if m.allWorkerMixCandidatesDown(mix, explicitModel) {
			return domain.WorkerMixEntry{}, fmt.Errorf("%w: project %s has every worker mix bucket down", ErrWorkerMixExhausted, project)
		}
		return domain.WorkerMixEntry{}, fmt.Errorf("worker mix selected %s: %w", bucketKeyString(bk), ErrWorkerMixBucketDown)
	}
	return entry, nil
}

func selectedWorkerMixKey(entry domain.WorkerMixEntry, explicitModel string) domain.BucketKey {
	bk := entry.BucketKey()
	if explicitModel != "" {
		bk.Model = explicitModel
	}
	return bk
}

func (m *Manager) allWorkerMixCandidatesDown(mix domain.WorkerMix, explicitModel string) bool {
	if len(mix) == 0 {
		return false
	}
	for _, entry := range mix {
		bk := selectedWorkerMixKey(entry, explicitModel)
		if !m.health.IsDown(workerMixCandidate(bk.Harness, bk.Model, bk.Effort)) {
			return false
		}
	}
	return true
}

// mixCensus counts the project's live workers per mix bucket, the input the
// apportionment step consumes. It reuses the existing list-and-filter scan (as
// activeOrchestratorSessionID does) rather than a dedicated aggregate query.
//
// Every live worker in a configured bucket is counted. Selection balances the
// actual live fleet, not only rows that the mix itself selected; a pinned worker
// occupying a bucket consumes that bucket's share too.
func (m *Manager) mixCensus(ctx context.Context, project domain.ProjectID, mix domain.WorkerMix) (map[domain.BucketKey]int, error) {
	recs, err := m.store.ListSessions(ctx, project)
	if err != nil {
		return nil, fmt.Errorf("worker mix census for %s: %w", project, err)
	}
	counts := make(map[domain.BucketKey]int, len(mix))
	for _, e := range mix {
		counts[e.BucketKey()] = 0
	}
	for _, rec := range recs {
		if rec.IsTerminated || rec.Kind != domain.KindWorker {
			continue
		}
		model := rec.Model
		if rec.MixSelected {
			model = rec.MixBucketModel
		}
		key := domain.BucketKey{Harness: rec.Harness, Model: model, Effort: rec.Effort}
		if _, inMix := counts[key]; !inMix {
			continue
		}
		counts[key]++
	}
	return counts, nil
}

func (m *Manager) applyWorkerMixSkipped(counts map[domain.BucketKey]int, mix domain.WorkerMix, explicitModel string) {
	explicitModel = strings.TrimSpace(explicitModel)
	m.health.ForEachSkipped(func(c candidatehealth.Candidate, skipped int) {
		if c.Surface != candidateSurfaceWorkerMix {
			return
		}
		key := domain.BucketKey{
			Harness: domain.AgentHarness(c.Harness),
			Model:   strings.TrimSpace(c.Model),
			Effort:  domain.NormalizeEffortForHarness(domain.AgentHarness(c.Harness), domain.Effort(c.Effort)),
		}
		if _, inMix := counts[key]; inMix {
			counts[key] += skipped
			return
		}
		if explicitModel == "" || key.Model != explicitModel {
			return
		}
		if bucket, ok := workerMixBucketForOverlaySkip(mix, key); ok {
			counts[bucket] += skipped
		}
	})
}

func workerMixBucketForOverlaySkip(mix domain.WorkerMix, overlay domain.BucketKey) (domain.BucketKey, bool) {
	var match domain.BucketKey
	matched := false
	for _, entry := range mix {
		bucket := entry.BucketKey()
		if bucket.Harness != overlay.Harness || bucket.Effort != overlay.Effort {
			continue
		}
		if matched {
			return domain.BucketKey{}, false
		}
		match = bucket
		matched = true
	}
	return match, matched
}

func bucketKeyString(key domain.BucketKey) string {
	parts := []string{string(key.Harness)}
	if key.Model != "" {
		parts = append(parts, key.Model)
	}
	if key.Effort != "" {
		parts = append(parts, "effort="+string(key.Effort))
	}
	return strings.Join(parts, ":")
}

// liveWorkerCount counts the project's non-terminated worker sessions — the
// input to the concurrency-cap check. It reuses the same list-and-filter scan
// mixCensus and activeOrchestratorSessionID already run rather than a dedicated
// aggregate query. Unlike mixCensus it tallies every live worker regardless of
// bucket or mix attribution: the cap is a project-level ceiling on workers, so
// a pinned worker counts exactly as a mix-selected one does. Orchestrators are
// excluded because the cap governs workers only.
func (m *Manager) liveWorkerCount(ctx context.Context, project domain.ProjectID) (int, error) {
	recs, err := m.store.ListSessions(ctx, project)
	if err != nil {
		return 0, fmt.Errorf("live worker count for %s: %w", project, err)
	}
	n := 0
	for _, rec := range recs {
		if !rec.IsTerminated && rec.Kind == domain.KindWorker {
			n++
		}
	}
	return n, nil
}

// workerMixCandidate is the candidate-health identity of one mix bucket: the
// worker-mix surface plus the bucket's normalized native tuple. The Tracker
// trims each string axis defensively; effort is normalized here because its
// compatibility aliases are harness-specific.
func workerMixCandidate(harness domain.AgentHarness, model string, effort domain.Effort) candidatehealth.Candidate {
	return candidatehealth.Candidate{
		Surface: candidateSurfaceWorkerMix,
		Harness: string(harness),
		Model:   model,
		Effort:  string(domain.NormalizeEffortForHarness(harness, effort)),
	}
}

// markMixCandidateDown marks the mix-selected bucket down after a
// launch-attributable spawn failure — the agent binary missing from PATH or the
// runtime refusing to create. Only a mix-selected spawn participates: a pinned
// spawn failing is not evidence the candidate AO would have chosen is broken.
// The attempt context is passed through so a caller-cancelled attempt is treated
// as a non-fault (the Tracker no-ops when ctx is already done), while a
// candidate-side error wrapping a deadline under a live caller still marks down.
func (m *Manager) markMixCandidateDown(ctx context.Context, mixSelected bool, cfg ports.SpawnConfig, effort domain.Effort, err error) {
	if !mixSelected {
		return
	}
	m.health.MarkDownForAttempt(ctx, workerMixCandidate(cfg.Harness, cfg.Model, effort), err)
}

func (m *Manager) recoverMixCandidate(projectConfig domain.ProjectConfig, cfg ports.SpawnConfig, requestedHarness domain.AgentHarness, requestedModel string, effort domain.Effort, mixSelected bool) {
	if mixSelected || requestedHarness != "" {
		m.health.MarkRecovered(workerMixCandidate(cfg.Harness, cfg.Model, effort))
	}
	mix, err := effectiveWorkerMix(projectConfig)
	if err != nil {
		return
	}
	if mixHasBucket(mix, cfg.Harness, cfg.Model, effort) {
		m.health.MarkRecovered(workerMixCandidate(cfg.Harness, cfg.Model, effort))
	} else if requestedHarness != "" && mixHasBucket(mix, cfg.Harness, requestedModel, effort) {
		m.health.MarkRecovered(workerMixCandidate(cfg.Harness, requestedModel, effort))
	}
}

// mixHasBucket reports whether the exact normalized native tuple is a
// configured bucket in the
// mix. A successful spawn on such a bucket — mix-selected or pinned onto the
// bucket's exact identity — is the "successful attempt on that exact candidate"
// that clears a stale down state. Mark-down is narrower (mix-selected only): a
// pin's failure is not evidence the candidate is broken, but a pin's success is
// proof it works, and because a down bucket is excluded from selection this is
// the reachable recovery path.
func mixHasBucket(mix domain.WorkerMix, harness domain.AgentHarness, model string, effort domain.Effort) bool {
	target := domain.WorkerMixEntry{Harness: harness, Model: model, Effort: effort}.BucketKey()
	for _, e := range mix {
		if e.BucketKey() == target {
			return true
		}
	}
	return false
}

func effectiveHarness(explicit domain.AgentHarness, kind domain.SessionKind, cfg domain.ProjectConfig) domain.AgentHarness {
	if explicit != "" {
		return explicit
	}
	if role := roleOverride(kind, cfg).Harness; role != "" {
		return role
	}
	return ""
}

func roleConfigName(kind domain.SessionKind) string {
	switch kind {
	case domain.KindOrchestrator:
		return "orchestrator"
	case domain.KindPrime:
		return "prime"
	default:
		return "worker"
	}
}

func roleOverride(kind domain.SessionKind, cfg domain.ProjectConfig) domain.RoleOverride {
	switch kind {
	case domain.KindOrchestrator:
		return cfg.Orchestrator
	case domain.KindPrime:
		return cfg.Prime
	default:
		return cfg.Worker
	}
}

func launchModelForBucket(harness domain.AgentHarness, model string, mixSelected bool) string {
	model = strings.TrimSpace(model)
	if model != "" {
		return model
	}
	if mixSelected {
		return domain.DefaultModelForHarness(harness)
	}
	return ""
}

// sessionPrefix returns the display prefix for a project: the explicit
// SessionPrefix when set, otherwise the first 12 characters of the project ID.
func sessionPrefix(project domain.ProjectRecord) string {
	if p := strings.TrimSpace(project.Config.SessionPrefix); p != "" {
		return p
	}
	if len(project.ID) <= 12 {
		return project.ID
	}
	return project.ID[:12]
}

// markSpawnFailedTerminated best-effort parks an orphaned spawn as terminated.
// A phantom half-spawned row is worse than a terminal one; we only delete the
// row when nothing observable has landed yet (seed state) via rollbackSpawn or
// rollbackSpawnSeedRow.
func (m *Manager) markSpawnFailedTerminated(ctx context.Context, id domain.SessionID) {
	cleanupCtx, cancel := cleanupContext(ctx)
	defer cancel()
	_ = m.lcm.MarkTerminated(cleanupCtx, id)
	m.cleanupSystemPromptDir(id)
}

// markSpawnFailedTerminatedWithoutWorkspace parks a spawn failure after the
// runtime row had become observable, but clears launch handles for resources
// that were destroyed during rollback. This keeps later restore/cleanup paths
// from treating a removed worktree as reusable state.
func (m *Manager) markSpawnFailedTerminatedWithoutWorkspace(ctx context.Context, id domain.SessionID) {
	cleanupCtx, cancel := cleanupContext(ctx)
	defer cancel()
	m.markSpawnFailedTerminated(cleanupCtx, id)
	rec, ok, err := m.store.GetSession(cleanupCtx, id)
	if err != nil || !ok {
		return
	}
	rec.Metadata.Branch = ""
	rec.Metadata.WorkspacePath = ""
	rec.Metadata.RuntimeHandleID = ""
	rec.Metadata.AgentSessionID = ""
	_ = m.store.UpdateSession(cleanupCtx, rec)
}

// rollbackSpawnSeedRow best-effort removes the row of a spawn that failed
// before anything observable (worktree, runtime) was built, so failed spawns
// don't accumulate terminated rows in session lists. DeleteSession only removes
// rows still in seed state; if the row has progressed or the delete itself
// fails, fall back to parking it terminated so a phantom row never looks live.
func (m *Manager) rollbackSpawnSeedRow(ctx context.Context, id domain.SessionID) {
	cleanupCtx, cancel := cleanupContext(ctx)
	defer cancel()
	if deleted, err := m.store.DeleteSession(cleanupCtx, id); err == nil && deleted {
		m.cleanupSystemPromptDir(id)
		return
	}
	m.markSpawnFailedTerminated(cleanupCtx, id)
}

// rollbackSpawn deletes a session row when it is still in seed state — used
// when an out-of-band step that happens AFTER `Spawn` returns (e.g. PR claim
// over HTTP) has failed and the caller wants the partially-spawned session
// gone without leaving a terminated orphan visible under `--include-terminated`.
//
// If the row has progressed past seed state (workspace exists, runtime created,
// etc.), DeleteSession is a no-op and rollbackSpawn falls back to a Kill so the
// runtime/workspace are torn down. Returns (deleted, killed):
//   - deleted=true: the row was a seed row and has been removed
//   - killed=true:  the row had spawn output and was torn down + terminated
//   - both false:   the row was already terminated or absent — benign no-op
func (m *Manager) rollbackSpawn(ctx context.Context, id domain.SessionID) (deleted, killed bool, err error) {
	deleted, err = m.store.DeleteSession(ctx, id)
	if err != nil {
		return false, false, fmt.Errorf("rollback %s: %w", id, err)
	}
	if deleted {
		m.cleanupSystemPromptDir(id)
		return true, false, nil
	}
	killed, err = m.Kill(ctx, id)
	if err != nil {
		return false, false, err
	}
	return false, killed, nil
}

// RollbackSpawn is the public surface of rollbackSpawn for service-layer callers.
func (m *Manager) RollbackSpawn(ctx context.Context, id domain.SessionID) (deleted, killed bool, err error) {
	return m.rollbackSpawn(ctx, id)
}

// Kill tears down the runtime and workspace, then records terminal intent with
// the LCM. A workspace teardown refused by the worktree-remove safety
// (uncommitted work) is never forced: Kill succeeds with freed=false,
// signalling the workspace was preserved for later inspection/cleanup while
// the session itself is still marked terminated.
//
// A session whose runtime handle or workspace path is missing (e.g. spawn
// failed partway, handle lost after a crash) is still terminated after the
// available destroy steps are skipped so it can be cleaned up from the
// dashboard.
func (m *Manager) Kill(ctx context.Context, id domain.SessionID) (bool, error) {
	rec, ok, err := m.store.GetSession(ctx, id)
	if err != nil {
		return false, fmt.Errorf("kill %s: %w", id, err)
	}
	if !ok {
		return false, nil // already gone: benign race
	}
	handle := runtimeHandle(rec.Metadata)
	ws := workspaceInfoForTeardown(rec, m.dataDir)

	// Same ownership invariant cleanup enforces. Role workspaces are canonical
	// and therefore shared across successive role rows, so killing an older row
	// whose path a live replacement already occupies would destroy the live
	// worktree. Its own runtime is still destroyed and the row still terminates;
	// only the shared workspace is spared.
	killWorkspaceOwnedElsewhere := false
	if ws.Path != "" || rec.Metadata.Branch != "" {
		live, liveErr := m.liveSessions(ctx)
		if liveErr != nil {
			return false, fmt.Errorf("kill %s: %w", id, liveErr)
		}
		if owner, inUse := workspaceOwnedByLiveSession(rec, live); inUse {
			m.logger.Info("kill: workspace still owned by a live session; preserving",
				"sessionID", rec.ID, "path", ws.Path, "owner", owner)
			killWorkspaceOwnedElsewhere = true
		}
	}

	var workspaceProjectRows []ports.WorkspaceRepoInfo
	workspaceProject := false
	// Skip row resolution entirely when the workspace belongs to a live session:
	// we are not going to tear it down, and a stale/unregistered child row could
	// error out here before the runtime is destroyed and the row terminated —
	// leaving the old process alive, which is the opposite of what Kill promises.
	if killWorkspaceOwnedElsewhere {
		// The shared root and every child the live owner occupies are left
		// alone; uncovered children are reclaimed below. All restore markers are
		// still cleared below, because keeping any would let RestoreAll
		// resurrect this killed session into the worktree the live replacement
		// is using (#2319).
		m.logger.Warn("kill: workspace preserved for a live owner; restore markers cleared",
			"sessionID", rec.ID, "path", ws.Path, "branch", rec.Metadata.Branch)
		workspaceProject = false
	} else if rows, ok, rowErr := m.workspaceProjectRows(ctx, rec); rowErr != nil {
		return false, fmt.Errorf("kill %s: workspace rows: %w", id, rowErr)
	} else if ok {
		workspaceProjectRows = rows
		workspaceProject = true
	}

	if handle.ID != "" {
		if err := m.runtime.Destroy(ctx, handle); err != nil {
			return false, fmt.Errorf("kill %s: runtime: %w", id, err)
		}
	}
	teardownCtx := ctx
	if handle.ID != "" {
		// Past this point the runtime is gone. The request remains entitled to
		// abandon agent-side cleanup below, but durable teardown must complete
		// on a bounded cleanup context so the row cannot stay live with a dead
		// runtime.
		cleanupCtx, cancelTeardown := cleanupContext(ctx)
		defer cancelTeardown()
		teardownCtx = cleanupCtx
	}
	if killWorkspaceOwnedElsewhere {
		// The root is owned, but a multi-repo CHILD the live owner does not
		// occupy is held by nobody. Reclaim it here, while its row still names
		// it: every marker is cleared below, and nothing else in the system
		// records a per-repo path, so a child left behind now is orphaned for
		// good (#144). Ordered after the runtime destroy above so the agent
		// whose worktree this is has already been torn down — a runtime destroy
		// that fails returns before this point and reclaims nothing.
		m.destroyUncoveredChildWorktrees(teardownCtx, rec)
	}
	freed := false
	if workspaceProject {
		cleaned, err := m.destroyWorkspaceProjectRows(teardownCtx, workspaceProjectRows)
		if err != nil {
			if errors.Is(err, ports.ErrWorkspaceDirty) {
				if err := m.lcm.MarkTerminated(teardownCtx, id); err != nil {
					return false, fmt.Errorf("kill %s: %w", id, err)
				}
				m.cleanupSystemPromptDir(id)
				return false, nil
			}
			return false, fmt.Errorf("kill %s: workspace: %w", id, err)
		}
		freed = cleaned
		if cleaned {
			m.cleanupAgentWorkspace(ctx, rec, ws.Path)
		}
	} else if ws.Path != "" && !killWorkspaceOwnedElsewhere {
		if err := m.workspace.Destroy(teardownCtx, ws); err != nil {
			if errors.Is(err, ports.ErrWorkspaceDirty) {
				if err := m.store.DeleteSessionWorktrees(teardownCtx, id); err != nil {
					m.logger.Warn("kill: delete restore marker failed", "sessionID", id, "error", err)
				}
				if err := m.lcm.MarkTerminated(teardownCtx, id); err != nil {
					return false, fmt.Errorf("kill %s: %w", id, err)
				}
				m.cleanupSystemPromptDir(id)
				return false, nil
			}
			return false, fmt.Errorf("kill %s: workspace: %w", id, err)
		}
		freed = true
		m.cleanupAgentWorkspace(ctx, rec, ws.Path)
	}
	// Clear the restore marker so the next boot's RestoreAll cannot resurrect a
	// killed session (#2319). For workspace projects this must happen after
	// teardown reads the rows; dirty-preserved rows return above and are left as
	// non-restorable inventory.
	if err := m.store.DeleteSessionWorktrees(teardownCtx, id); err != nil {
		m.logger.Warn("kill: delete restore marker failed", "sessionID", id, "error", err)
	}
	if err := m.lcm.MarkTerminated(teardownCtx, id); err != nil {
		return false, fmt.Errorf("kill %s: %w", id, err)
	}
	m.cleanupSystemPromptDir(id)
	return freed, nil
}

// RetireForReplacement terminates a live orchestrator and releases its branch
// for a replacement session. Unlike Kill, this captures uncommitted work before
// force-removing the worktree, so a dirty canonical orchestrator worktree does
// not block the replacement from claiming the canonical branch.
//
// This deliberately does not write a session_worktrees row: those rows are
// boot-restore markers, and a replaced orchestrator must stay terminated.
func (m *Manager) RetireForReplacement(ctx context.Context, id domain.SessionID) error {
	rec, ok, err := m.store.GetSession(ctx, id)
	if err != nil {
		return fmt.Errorf("retire replacement %s: %w", id, err)
	}
	if !ok || rec.IsTerminated {
		return nil
	}
	// A workspace is destroyed only by the session that still owns it. A role
	// row can share its canonical path/branch with a live replacement, and
	// retiring the older row must not tear down the worktree the newer one is
	// running in. Its own runtime is still reaped and the row still terminates —
	// only the SHARED workspace is spared; a multi-repo child the live owner
	// does not occupy is still reclaimed below.
	workspaceOwnedElsewhere := false
	if rec.Metadata.WorkspacePath != "" || rec.Metadata.Branch != "" {
		live, liveErr := m.liveSessions(ctx)
		if liveErr != nil {
			return fmt.Errorf("retire replacement %s: %w", id, liveErr)
		}
		if owner, inUse := workspaceOwnedByLiveSession(rec, live); inUse {
			m.logger.Info("retire replacement: workspace still owned by a live session; preserving",
				"sessionID", rec.ID, "path", rec.Metadata.WorkspacePath, "owner", owner)
			workspaceOwnedElsewhere = true
		}
	}
	if rec.Metadata.WorkspacePath == "" || rec.Metadata.Branch == "" || workspaceOwnedElsewhere {
		// A row with no workspace at all has no inventory to reclaim, so its
		// markers are cleared up front, exactly as before.
		if !workspaceOwnedElsewhere {
			if err := m.store.DeleteSessionWorktrees(ctx, rec.ID); err != nil {
				return fmt.Errorf("retire replacement %s: clear restore markers: %w", id, err)
			}
		}
		handle := runtimeHandle(rec.Metadata)
		if handle.ID != "" {
			if err := m.runtime.Destroy(ctx, handle); err != nil {
				return fmt.Errorf("retire replacement %s: runtime: %w", id, err)
			}
		}
		// Everything below is post-teardown bookkeeping for a runtime that is
		// already gone, so it runs detached from the caller (bounded) on the
		// package's one cleanup context. Up to here the caller may still abort:
		// a cancelled request that never reaps the runtime leaves the row live
		// and consistent, which is a retry. Once the runtime IS reaped the row
		// is committed to terminating, and running these steps on a dead caller
		// context fails all three at once — child reclaim, marker clear, and the
		// terminal write — which is precisely the live-row-with-a-dead-runtime
		// state the joined error below exists to prevent, reported faithfully
		// and left on disk anyway.
		cleanupCtx, cancelCleanup := cleanupContext(ctx)
		defer cancelCleanup()
		var clearErr error
		if workspaceOwnedElsewhere {
			// Same defect, same remedy as Kill (#144): the live owner holds the
			// root, but a multi-repo CHILD it does not occupy is held by nobody,
			// and session_worktrees is the only record of where that child is.
			// Retiring on a clean replacement is the common path, so clearing
			// the markers without reclaiming orphans it for good.
			//
			// Ordered between the runtime destroy and the marker clear because
			// both sides are load-bearing: the retiring agent is still running
			// in that tree until its runtime is destroyed (and a runtime destroy
			// that fails returns above, reclaiming nothing), and the reclaim
			// reads the very rows the clear below deletes. This is the ordering
			// the function's own workspace-teardown path already uses.
			m.destroyUncoveredChildWorktrees(cleanupCtx, rec)
			if err := m.store.DeleteSessionWorktrees(cleanupCtx, rec.ID); err != nil {
				clearErr = fmt.Errorf("retire replacement %s: clear restore markers: %w", id, err)
			}
		}
		// Past the runtime destroy the row MUST end terminated, whatever else
		// failed: a live row whose runtime is dead is a lie, and on the next boot
		// reconcileLive reads it as crash recovery and stashes and force-destroys
		// this row's shared root and covered children — underneath the live
		// replacement that legitimately owns them. Returning on the marker-clear
		// failure alone would leave exactly that row, so both failures are
		// reported instead and neither is swallowed.
		if err := m.lcm.MarkTerminated(cleanupCtx, id); err != nil {
			return errors.Join(clearErr, fmt.Errorf("retire replacement %s: mark terminated: %w", id, err))
		}
		return clearErr
	}
	if rows, ok, rowErr := m.workspaceProjectRows(ctx, rec); rowErr != nil {
		return fmt.Errorf("retire replacement %s: workspace rows: %w", id, rowErr)
	} else if ok {
		return m.retireWorkspaceProjectForReplacement(ctx, rec, rows)
	}

	ws := workspaceInfoForTeardown(rec, m.dataDir)
	staleWorkspace := false
	if _, err := m.workspace.StashUncommitted(ctx, ws); err != nil {
		if !errors.Is(err, ports.ErrWorkspaceStale) {
			return fmt.Errorf("retire replacement %s: stash: %w", id, err)
		}
		staleWorkspace = true
		m.logger.Warn("retire replacement: stale workspace; skipping preserve", "sessionID", id, "path", ws.Path, "error", err)
	}
	handle := runtimeHandle(rec.Metadata)
	cleanupCtx, cancelCleanup := cleanupContext(ctx)
	defer cancelCleanup()
	if handle.ID != "" {
		if err := m.runtime.Destroy(cleanupCtx, handle); err != nil {
			return fmt.Errorf("retire replacement %s: runtime: %w", id, err)
		}
	}
	if err := m.workspace.ForceDestroy(cleanupCtx, ws); err != nil {
		if staleWorkspace {
			m.logger.Warn("retire replacement: stale workspace cleanup failed", "sessionID", id, "path", ws.Path, "error", err)
		}
		return fmt.Errorf("retire replacement %s: force destroy: %w", id, err)
	}
	m.cleanupAgentWorkspace(cleanupCtx, rec, ws.Path)
	if err := m.store.DeleteSessionWorktrees(cleanupCtx, rec.ID); err != nil {
		return fmt.Errorf("retire replacement %s: clear restore markers: %w", id, err)
	}
	if err := m.lcm.MarkTerminated(cleanupCtx, rec.ID); err != nil {
		return fmt.Errorf("retire replacement %s: mark terminated: %w", id, err)
	}
	return nil
}

func (m *Manager) retireWorkspaceProjectForReplacement(ctx context.Context, rec domain.SessionRecord, rows []ports.WorkspaceRepoInfo) error {
	staleRepos := make(map[string]bool)
	for _, row := range rows {
		if _, err := m.workspace.StashUncommitted(ctx, workspaceInfoFromRepoInfo(row)); err != nil {
			if !errors.Is(err, ports.ErrWorkspaceStale) {
				return fmt.Errorf("retire replacement %s repo %s: stash: %w", rec.ID, row.RepoName, err)
			}
			staleRepos[row.RepoName] = true
			m.logger.Warn("retire replacement: stale workspace repo; skipping preserve", "sessionID", rec.ID, "repo", row.RepoName, "path", row.Path, "error", err)
		}
	}
	handle := runtimeHandle(rec.Metadata)
	cleanupCtx, cancelCleanup := cleanupContext(ctx)
	defer cancelCleanup()
	if handle.ID != "" {
		if err := m.runtime.Destroy(cleanupCtx, handle); err != nil {
			return fmt.Errorf("retire replacement %s: runtime: %w", rec.ID, err)
		}
	}
	for i := len(rows) - 1; i >= 0; i-- {
		if err := m.workspace.ForceDestroy(cleanupCtx, workspaceInfoFromRepoInfo(rows[i])); err != nil {
			if staleRepos[rows[i].RepoName] {
				m.logger.Warn("retire replacement: stale workspace repo cleanup failed", "sessionID", rec.ID, "repo", rows[i].RepoName, "path", rows[i].Path, "error", err)
			}
			return fmt.Errorf("retire replacement %s repo %s: force destroy: %w", rec.ID, rows[i].RepoName, err)
		}
	}
	m.cleanupAgentWorkspace(cleanupCtx, rec, rec.Metadata.WorkspacePath)
	if err := m.store.DeleteSessionWorktrees(cleanupCtx, rec.ID); err != nil {
		return fmt.Errorf("retire replacement %s: clear restore markers: %w", rec.ID, err)
	}
	if err := m.lcm.MarkTerminated(cleanupCtx, rec.ID); err != nil {
		return fmt.Errorf("retire replacement %s: mark terminated: %w", rec.ID, err)
	}
	return nil
}

// RestoreWithMode relaunches a torn-down session and reports whether AO used
// native resume, a saved-prompt fallback, or a fresh launch. The fallible I/O
// runs before any durable session write, so a failure never resurrects the row
// or destroys the worktree (it may hold the agent's prior work).
func (m *Manager) RestoreWithMode(ctx context.Context, id domain.SessionID) (RestoreResult, error) {
	rec, ok, err := m.store.GetSession(ctx, id)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("restore %s: %w", id, err)
	}
	if !ok {
		return RestoreResult{}, fmt.Errorf("restore %s: %w", id, ErrNotFound)
	}
	if !rec.IsTerminated {
		return RestoreResult{}, fmt.Errorf("restore %s: %w", id, ErrNotRestorable)
	}
	// The same ownership invariant Kill and Cleanup enforce, on the third
	// lifecycle operation over the same workspace. Role workspaces are canonical,
	// so a terminated role row records the very worktree its live replacement is
	// running in; relaunching it would put two runtimes on one worktree and one
	// branch. Refusing HERE — before the project load, the workspace restore, and
	// the runtime create — is what keeps the refusal from creating, restoring, or
	// launching anything.
	if rec.Metadata.WorkspacePath != "" || rec.Metadata.Branch != "" {
		live, liveErr := m.liveSessions(ctx)
		if liveErr != nil {
			return RestoreResult{}, fmt.Errorf("restore %s: %w", id, liveErr)
		}
		if owner, inUse := workspaceOwnedByLiveSession(rec, live); inUse {
			return RestoreResult{}, fmt.Errorf("restore %s: workspace %q is held by live session %s: %w",
				id, rec.Metadata.WorkspacePath, owner, ErrWorkspaceOwnedByLiveSession)
		}
	}
	meta := rec.Metadata
	project, err := m.loadProject(ctx, rec.ProjectID)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("restore %s: %w", id, err)
	}
	// Mirror Kill's incomplete-handle guard: a session whose spawn failed before
	// the workspace landed has neither WorkspacePath nor Branch, and there is
	// nothing meaningful to restore from. Surface this as a typed 409 instead of
	// letting workspace.Restore fail with an opaque wrapped error.
	if meta.WorkspacePath == "" || (meta.Branch == "" && project.Kind.WithDefault() != domain.ProjectKindScratch) {
		return RestoreResult{}, fmt.Errorf("restore %s: %w", id, ErrIncompleteHandle)
	}
	// Resumability is decided inside restoreArgv, not here. A promptless session
	// can still be fully resumable when the harness pins a deterministic session id
	// (Claude Code). restoreArgv returns ErrNotResumable only for a promptless,
	// unresumable non-orchestrator (a worker with no task and no native id to resume).
	// Orchestrators always relaunch fresh with the system prompt only.

	ws, err := m.restoreSessionWorkspace(ctx, project, rec)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("restore %s: workspace: %w", id, err)
	}
	return m.relaunchRestoredSession(ctx, rec, project, ws)
}

func (m *Manager) relaunchRestoredSession(ctx context.Context, rec domain.SessionRecord, project domain.ProjectRecord, ws ports.WorkspaceInfo) (RestoreResult, error) {
	agent, ok := m.agents.Agent(rec.Harness)
	if !ok {
		return RestoreResult{}, fmt.Errorf("restore %s: no agent adapter for harness %q", rec.ID, rec.Harness)
	}
	// The system prompt is derived, not persisted: recompute it so a restored
	// session keeps its standing instructions across the relaunch.
	systemPrompt, err := m.buildSystemPrompt(ctx, rec.Kind, rec.ProjectID)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("restore %s: system prompt: %w", rec.ID, err)
	}
	systemPromptFile, err := m.prepareSystemPromptFile(rec.ID, rec.Harness, systemPrompt)
	if err != nil {
		m.cleanupSystemPromptDir(rec.ID)
		return RestoreResult{}, fmt.Errorf("restore %s: system prompt file: %w", rec.ID, err)
	}

	// Restore re-applies the project's resolved agent config so a configured
	// model/permissions carry across a restore, matching fresh spawn. The model
	// comes from the row rather than config: the session is already counted in
	// its (harness, model) bucket, so relaunching it on a different model would
	// put the census and the running agent out of step.
	agentConfig, err := agentconfig.Effective(rec.Kind, project.Config, "", rec.Harness)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("restore %s: agent config: %w", rec.ID, err)
	}
	// A mix-selected session carries its bucket's model authoritatively. The
	// empty string remains the persisted bucket identity, but the adapter still
	// receives that harness's default model rather than a later project scalar.
	if rec.MixSelected {
		agentConfig.Model = launchModelForBucket(rec.Harness, rec.Model, true)
	} else if rec.Model != "" {
		agentConfig.Model = rec.Model
	}
	if rec.Effort != "" {
		agentConfig.Effort = rec.Effort
	}
	runtimeToken, err := newRuntimeToken()
	if err != nil {
		return RestoreResult{}, fmt.Errorf("restore %s: runtime token: %w", rec.ID, err)
	}
	env := m.runtimeEnv(rec.ID, rec.ProjectID, rec.IssueID, runtimeToken, project.Config.Env)
	m.augmentAgentRuntimeEnv(agent, env)
	if err := m.prepareWorkspace(ctx, agent, rec.ID, ws.Path, systemPrompt, systemPromptFile, agentConfig, env); err != nil {
		return RestoreResult{}, fmt.Errorf("restore %s: %w", rec.ID, err)
	}
	argv, delivery, mode, err := restoreArgv(ctx, agent, rec.ID, ws.Path, rec.Metadata, systemPrompt, systemPromptFile, agentConfig, rec.Kind, rec.Harness, m.dataDir)
	if err != nil {
		m.cleanupSystemPromptDir(rec.ID)
		return RestoreResult{}, fmt.Errorf("restore %s: %w", rec.ID, err)
	}
	m.augmentRuntimePATHForLaunchBinary(ctx, env, argv)
	handle, err := m.runtime.Create(ctx, ports.RuntimeConfig{
		SessionID:     rec.ID,
		WorkspacePath: ws.Path,
		Argv:          argv,
		Env:           env,
	})
	if err != nil {
		m.cleanupSystemPromptDir(rec.ID)
		return RestoreResult{}, fmt.Errorf("restore %s: runtime: %w", rec.ID, err)
	}
	if err := m.verifyLaunchCommandRunning(ctx, handle, launchProcessCommand(argv)); err != nil {
		m.destroyFailedLaunchRuntime(ctx, handle)
		m.cleanupSystemPromptDir(rec.ID)
		return RestoreResult{}, fmt.Errorf("restore %s: launch process: %w", rec.ID, err)
	}
	metadata := domain.SessionMetadata{
		Branch:            ws.Branch,
		WorkspacePath:     ws.Path,
		WorkspaceRepoPath: ws.RepoPath,
		RuntimeHandleID:   handle.ID,
		RuntimeToken:      runtimeToken,
		LaunchCommand:     launchProcessCommand(argv),
		AgentSessionID:    rec.Metadata.AgentSessionID,
		Prompt:            rec.Metadata.Prompt,
		PromptPolicyHash:  promptPolicyHash(systemPrompt),
	}
	if err := m.lcm.MarkSpawned(ctx, rec.ID, metadata); err != nil {
		m.destroyFailedLaunchRuntime(ctx, handle)
		m.cleanupSystemPromptDir(rec.ID)
		return RestoreResult{}, fmt.Errorf("restore %s: completed: %w", rec.ID, err)
	}
	restoreLaunchCfg := ports.LaunchConfig{
		DataDir:          m.dataDir,
		DisplayName:      rec.DisplayName,
		SessionID:        string(rec.ID),
		WorkspacePath:    ws.Path,
		Kind:             rec.Kind,
		Prompt:           rec.Metadata.Prompt,
		SystemPrompt:     systemPrompt,
		SystemPromptFile: systemPromptFile,
		Config:           agentConfig,
		Permissions:      agentConfig.Permissions,
	}
	// A restore relaunches the harness, so the harness's name resets to whatever
	// it derives for itself while AO keeps the name it owns. Without this, a
	// session renamed before it was torn down comes back with the old harness
	// name — a divergence reintroduced by the very lifecycle event meant to
	// preserve the session. The resume command carries no launch-time name flag,
	// so this always takes the in-harness path.
	if err := m.deliverNameAfterStart(ctx, agent, restoreLaunchCfg, handle, rec.ID, rec.DisplayName); err != nil {
		m.logger.Warn("restore: session name not delivered to the harness; AO's name stands",
			"sessionID", rec.ID, "error", err)
	}
	if delivery == ports.PromptDeliveryAfterStart && rec.Metadata.Prompt != "" {
		launchCfg := restoreLaunchCfg
		if err := m.deliverAfterStartPrompt(ctx, agent, launchCfg, handle, rec.ID, rec.Metadata.Prompt); err != nil {
			m.destroyFailedLaunchRuntime(ctx, handle)
			m.markSpawnFailedTerminated(ctx, rec.ID)
			return RestoreResult{}, fmt.Errorf("restore %s: deliver prompt: %w", rec.ID, err)
		}
	}
	if err := m.verifyLaunchCommandRunning(ctx, handle, launchProcessCommand(argv)); err != nil {
		// MarkSpawned above already flipped the restored row live, so parking it
		// terminated is what keeps a destroyed runtime from leaving a live-looking
		// row behind. The pre-MarkSpawned branch has nothing to undo.
		m.destroyFailedLaunchRuntime(ctx, handle)
		m.markSpawnFailedTerminated(ctx, rec.ID)
		return RestoreResult{}, fmt.Errorf("restore %s: launch process: %w", rec.ID, err)
	}
	updated, err := m.getRecord(ctx, rec.ID)
	if err != nil {
		return RestoreResult{}, err
	}
	return RestoreResult{Session: updated, Mode: mode}, nil
}

func (m *Manager) getRecord(ctx context.Context, id domain.SessionID) (domain.SessionRecord, error) {
	rec, ok, err := m.store.GetSession(ctx, id)
	if err != nil {
		return domain.SessionRecord{}, fmt.Errorf("get %s: %w", id, err)
	}
	if !ok {
		return domain.SessionRecord{}, fmt.Errorf("get %s: %w", id, ErrNotFound)
	}
	return rec, nil
}

func (m *Manager) lockSpawnAdmission(project domain.ProjectID) func() {
	m.spawnLocksMu.Lock()
	lock := m.spawnLocks[project]
	if lock == nil {
		lock = &sync.Mutex{}
		m.spawnLocks[project] = lock
	}
	m.spawnLocksMu.Unlock()
	lock.Lock()
	return lock.Unlock
}

// SaveAndTeardownAll captures uncommitted work and tears down every live
// session that has a workspace path. It is the shutdown path for the daemon:
// each session's uncommitted work is stashed into a preserve ref, the ref is
// written to session_worktrees (the "shutdown-saved" marker) BEFORE the
// worktree is force-removed. The DB write is committed before the worktree is
// destroyed so a crash between the two leaves the ref in place and the row
// present; RestoreAll will replay both.
//
// Failures on individual sessions are logged and do not abort the loop.
// ForceDestroy is never called if capture or the DB write did not succeed.
func (m *Manager) SaveAndTeardownAll(ctx context.Context) error {
	recs, err := m.store.ListAllSessions(ctx)
	if err != nil {
		return fmt.Errorf("save-teardown-all: list sessions: %w", err)
	}
	for _, rec := range recs {
		if rec.IsTerminated {
			continue
		}
		if rec.Metadata.WorkspacePath == "" || rec.Metadata.Branch == "" {
			continue
		}
		if err := m.saveAndTeardownOne(ctx, rec, true); err != nil {
			m.logger.Error("save-teardown-all: session failed, skipping", "sessionID", rec.ID, "error", err)
		}
	}
	return nil
}

// saveAndTeardownOne runs the capture-then-destroy sequence for a single
// session. The DB write (UpsertSessionWorktree) is committed before
// ForceDestroy; if either capture or the DB write fails, ForceDestroy is
// not called.
func (m *Manager) saveAndTeardownOne(ctx context.Context, rec domain.SessionRecord, destroyRuntime bool) error {
	if rows, ok, err := m.workspaceProjectRows(ctx, rec); err != nil {
		return fmt.Errorf("save %s: workspace rows: %w", rec.ID, err)
	} else if ok {
		return m.saveAndTeardownWorkspaceProject(ctx, rec, rows, destroyRuntime)
	}

	// 1. Capture uncommitted work (ref may be "" for clean worktrees).
	ws := workspaceInfoForTeardown(rec, m.dataDir)
	ref, err := m.workspace.StashUncommitted(ctx, ws)
	if err != nil {
		return fmt.Errorf("save %s: stash: %w", rec.ID, err)
	}

	// 2. Write the shutdown-saved marker to the DB. The row's presence (even
	// with an empty preserved_ref) is what RestoreAll uses to identify sessions
	// saved by this run. This MUST be committed before ForceDestroy.
	row := domain.SessionWorktreeRecord{
		SessionID:    rec.ID,
		RepoName:     domain.RootWorkspaceRepoName,
		RepoPath:     rec.Metadata.WorkspaceRepoPath,
		Branch:       rec.Metadata.Branch,
		WorktreePath: rec.Metadata.WorkspacePath,
		PreservedRef: ref,
		State:        "removed",
	}
	if err := m.store.UpsertSessionWorktree(ctx, row); err != nil {
		return fmt.Errorf("save %s: upsert worktree row: %w", rec.ID, err)
	}

	// 3. Mark terminal via the LCM (same path Kill uses).
	if err := m.lcm.MarkTerminated(ctx, rec.ID); err != nil {
		return fmt.Errorf("save %s: mark terminated: %w", rec.ID, err)
	}

	// 4. Runtime teardown (best-effort; same pattern as Kill).
	handle := runtimeHandle(rec.Metadata)
	if destroyRuntime && handle.ID != "" {
		if err := m.runtime.Destroy(ctx, handle); err != nil {
			m.logger.Warn("save-teardown-all: runtime destroy failed", "sessionID", rec.ID, "error", err)
		}
	}

	// 5. Force-remove the worktree (safe: work is captured in step 1 and the
	// DB write in step 2 is already committed).
	if err := m.workspace.ForceDestroy(ctx, ws); err != nil {
		m.logger.Warn("save-teardown-all: force destroy failed", "sessionID", rec.ID, "error", err)
	} else {
		m.cleanupAgentWorkspace(ctx, rec, ws.Path)
	}
	return nil
}

// reconcileLive handles a single non-terminated session on boot. If its runtime
// session is still alive (tmux is the persistence layer, so it survives a daemon
// crash) we adopt it: a no-op, the agent keeps running. If the runtime is gone,
// the agent died with the daemon, so we save-and-tear-down to the SAME end state
// a graceful shutdown produces: capture uncommitted work into a preserve ref,
// record the session_worktrees restore marker, mark terminated, and remove the
// worktree. RestoreAll (which Reconcile runs immediately after) then relaunches
// it on this same boot, resuming history. Crash recovery thus matches graceful
// restart instead of silently abandoning the session.
//
// If the work capture fails we mark terminated WITHOUT a marker and leave the
// worktree intact: better to skip the relaunch than to tear down un-preserved
// work or relaunch onto an inconsistent worktree.
func (m *Manager) reconcileLive(ctx context.Context, rec domain.SessionRecord) error {
	project, err := m.loadProject(ctx, rec.ProjectID)
	if err != nil {
		return err
	}
	projectKind := project.Kind.WithDefault()
	if rec.Metadata.WorkspacePath == "" || (rec.Metadata.Branch == "" && projectKind != domain.ProjectKindScratch) {
		return nil
	}
	handle := runtimeHandle(rec.Metadata)
	if handle.ID != "" {
		alive, err := m.runtime.IsAlive(ctx, handle)
		if err != nil {
			// A failed probe is not proof of death: leave the session as-is.
			return fmt.Errorf("reconcile %s: probe: %w", rec.ID, err)
		}
		if alive {
			return nil // adopt: the session survived the crash.
		}
	}
	if projectKind == domain.ProjectKindScratch {
		return m.lcm.MarkTerminated(ctx, rec.ID)
	}
	if err := m.saveAndTeardownOne(ctx, rec, false); err != nil {
		m.logger.Warn("reconcile: save-and-teardown failed; terminating without restore marker", "sessionID", rec.ID, "error", err)
		if mErr := m.lcm.MarkTerminated(ctx, rec.ID); mErr != nil {
			return fmt.Errorf("reconcile %s: mark terminated: %w", rec.ID, mErr)
		}
	}
	return nil
}

// reconcileReap kills the leaked tmux session of a session the DB already marks
// terminated. This covers the teardown that marked the row terminated but failed
// to kill the runtime (e.g. ForceDestroy/Destroy errored after MarkTerminated).
// Destroy is idempotent, so an already-gone session is a no-op.
func (m *Manager) reconcileReap(ctx context.Context, rec domain.SessionRecord) error {
	handle := runtimeHandle(rec.Metadata)
	if handle.ID == "" {
		return nil
	}
	alive, err := m.runtime.IsAlive(ctx, handle)
	if err != nil {
		return fmt.Errorf("reconcile reap %s: probe: %w", rec.ID, err)
	}
	if !alive {
		return nil
	}
	if err := m.runtime.Destroy(ctx, handle); err != nil {
		return fmt.Errorf("reconcile reap %s: destroy: %w", rec.ID, err)
	}
	return nil
}

// Reconcile is the boot-time consistency pass. It replaces the bare RestoreAll
// call so that however the previous daemon died (clean shutdown, SIGKILL, or
// crash), live reality matches the DB:
//
//  1. Live pass: for each non-terminated session, adopt it if its runtime
//     survived, else capture work and mark terminated (reconcileLive).
//  2. Reap pass: for each terminated session whose runtime leaked, kill it
//     (reconcileReap). Runs before restore so a restored session does not
//     collide with a leaked tmux of the same name.
//  3. Restore pass: relaunch shutdown-saved sessions (existing RestoreAll).
//
// Best-effort throughout: a per-session failure is logged and never aborts the
// pass or blocks boot.
func (m *Manager) Reconcile(ctx context.Context) error {
	recs, err := m.store.ListAllSessions(ctx)
	if err != nil {
		return fmt.Errorf("reconcile: list sessions: %w", err)
	}
	for _, rec := range recs {
		if rec.IsTerminated {
			continue
		}
		if err := m.reconcileLive(ctx, rec); err != nil {
			m.logger.Error("reconcile: live pass failed, skipping", "sessionID", rec.ID, "error", err)
		}
	}
	for _, rec := range recs {
		if !rec.IsTerminated {
			continue
		}
		if err := m.reconcileReap(ctx, rec); err != nil {
			m.logger.Error("reconcile: reap pass failed, skipping", "sessionID", rec.ID, "error", err)
		}
	}
	return m.RestoreAll(ctx)
}

// RestoreAll relaunches every terminated session that was saved by the last
// SaveAndTeardownAll. The "shutdown-saved" marker is the presence of a
// session_worktrees row for the session; sessions the user killed before
// shutdown have no such row and are left terminated.
//
// For each saved session:
//  1. Ensure the worktree exists via workspace.Restore.
//  2. If a preserve ref is recorded, replay it via ApplyPreserved; on conflict
//     log and continue (still relaunch the agent, never delete the ref).
//  3. Relaunch via the existing Restore method.
//
// Failures on individual sessions are logged and do not abort the loop.
func (m *Manager) RestoreAll(ctx context.Context) error {
	recs, err := m.store.ListAllSessions(ctx)
	if err != nil {
		return fmt.Errorf("restore-all: list sessions: %w", err)
	}
	for _, rec := range recs {
		if !rec.IsTerminated {
			continue
		}
		// Check the shutdown-saved marker: is there a session_worktrees row?
		rows, err := m.store.ListSessionWorktrees(ctx, rec.ID)
		if err != nil {
			m.logger.Error("restore-all: list worktrees failed", "sessionID", rec.ID, "error", err)
			continue
		}
		if len(rows) == 0 {
			// No marker: this session was killed by the user before shutdown.
			continue
		}
		rows = restorableWorktreeRows(rows)
		if len(rows) == 0 {
			continue
		}

		// Step 1: ensure the worktree exists. workspace.Restore re-creates it
		// if it was removed by SaveAndTeardownAll.
		project, err := m.loadProject(ctx, rec.ProjectID)
		if err != nil {
			m.logger.Error("restore-all: load project failed", "sessionID", rec.ID, "error", err)
			continue
		}
		var ws ports.WorkspaceInfo
		restoredWorkspaceProject := project.Kind.WithDefault() == domain.ProjectKindWorkspace
		var projectRows []ports.WorkspaceRepoInfo
		if restoredWorkspaceProject {
			var rowErr error
			projectRows, rowErr = m.workspaceProjectRestoreRowsFromMarkers(ctx, project, rec, rows)
			if rowErr != nil {
				m.logger.Error("restore-all: workspace rows failed", "sessionID", rec.ID, "error", rowErr)
				continue
			}
			root, restoreErr := m.restoreWorkspaceProjectRows(ctx, projectRows)
			if restoreErr != nil {
				m.logger.Error("restore-all: workspace project restore failed", "sessionID", rec.ID, "error", restoreErr)
				continue
			}
			ws = workspaceInfoFromRepoInfo(root)
		} else {
			var restoreErr error
			ws, restoreErr = m.workspace.Restore(ctx, ports.WorkspaceConfig{
				ProjectID:     rec.ProjectID,
				SessionID:     rec.ID,
				Kind:          rec.Kind,
				SessionPrefix: sessionPrefix(project),
				Branch:        rec.Metadata.Branch,
				RepoPath:      singleRepoOverridePath(rec, m.dataDir),
				Path:          rec.Metadata.WorkspacePath,
			})
			if restoreErr != nil {
				m.logger.Error("restore-all: workspace restore failed", "sessionID", rec.ID, "error", restoreErr)
				continue
			}
		}
		if ws.Path == "" {
			m.logger.Error("restore-all: workspace restore failed", "sessionID", rec.ID, "error", "empty restored root path")
			continue
		}

		// Step 2: replay preserve ref when one was recorded.
		if restoredWorkspaceProject {
			m.applyWorkspaceProjectPreserved(ctx, projectRows)
		} else {
			var preserveRef string
			for _, r := range rows {
				if r.PreservedRef != "" {
					preserveRef = r.PreservedRef
					break
				}
			}
			if preserveRef != "" {
				if applyErr := m.workspace.ApplyPreserved(ctx, ws, preserveRef); applyErr != nil {
					if errors.Is(applyErr, ports.ErrPreservedConflict) {
						m.logger.Warn("restore-all: apply preserved produced conflicts; agent relaunched with conflict markers in place",
							"sessionID", rec.ID, "ref", preserveRef, "error", applyErr)
					} else {
						m.logger.Error("restore-all: apply preserved failed", "sessionID", rec.ID, "error", applyErr)
					}
					// Continue: always relaunch even on conflict (never delete the ref here).
				}
			}
		}

		// Step 3: relaunch the agent in the restored workspace.
		if _, err := m.relaunchRestoredSession(ctx, rec, project, ws); err != nil {
			switch {
			case errors.Is(err, ErrNotResumable):
				// A promptless, unresumable worker is intentionally left terminated:
				// expected, not an operational failure, so log it quietly.
				m.logger.Warn("restore-all: session left terminated (nothing to resume)", "sessionID", rec.ID)
			case errors.Is(err, ErrNotFound):
				// The row was reaped between listing and relaunch (a stale id during
				// reconciliation): skip it and keep restoring the rest.
				m.logger.Warn("restore-all: session vanished before relaunch, skipping", "sessionID", rec.ID)
			default:
				m.logger.Error("restore-all: relaunch failed", "sessionID", rec.ID, "error", err)
			}
			continue
		}

		// One-shot: drop the consumed marker so it never outlives one restart
		// (#2319). A still-live session re-acquires it at the next quit.
		if restoredWorkspaceProject {
			for _, row := range projectRows {
				if err := m.upsertWorkspaceProjectRowState(ctx, row, "active"); err != nil {
					m.logger.Warn("restore-all: marking workspace repo active failed", "sessionID", rec.ID, "repo", row.RepoName, "error", err)
				}
			}
		} else {
			if err := m.markSessionWorktreesActive(ctx, rows); err != nil {
				m.logger.Warn("restore-all: marking worktrees active failed", "sessionID", rec.ID, "error", err)
			}
			if err := m.store.DeleteSessionWorktrees(ctx, rec.ID); err != nil {
				m.logger.Warn("restore-all: delete restore marker failed", "sessionID", rec.ID, "error", err)
			}
		}
	}
	return nil
}

func restorableWorktreeRows(rows []domain.SessionWorktreeRecord) []domain.SessionWorktreeRecord {
	out := make([]domain.SessionWorktreeRecord, 0, len(rows))
	for _, row := range rows {
		if row.State == "removed" || legacyRestorableWorktreeRow(row) {
			out = append(out, row)
		}
	}
	return out
}

func legacyRestorableWorktreeRow(row domain.SessionWorktreeRecord) bool {
	return row.State == "" && (row.PreservedRef != "" || row.RepoName == domain.RootWorkspaceRepoName)
}

func (m *Manager) markSessionWorktreesActive(ctx context.Context, rows []domain.SessionWorktreeRecord) error {
	for _, row := range rows {
		row.State = "active"
		row.PreservedRef = ""
		if err := m.store.UpsertSessionWorktree(ctx, row); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) restoreSessionWorkspace(ctx context.Context, project domain.ProjectRecord, rec domain.SessionRecord) (ports.WorkspaceInfo, error) {
	if project.Kind.WithDefault() != domain.ProjectKindWorkspace {
		return m.workspace.Restore(ctx, ports.WorkspaceConfig{
			ProjectID:     rec.ProjectID,
			SessionID:     rec.ID,
			Kind:          rec.Kind,
			SessionPrefix: sessionPrefix(project),
			Branch:        rec.Metadata.Branch,
			RepoPath:      singleRepoOverridePath(rec, m.dataDir),
			Path:          rec.Metadata.WorkspacePath,
		})
	}
	rows, err := m.workspaceProjectRestoreRows(ctx, project, rec)
	if err != nil {
		return ports.WorkspaceInfo{}, err
	}
	root, err := m.restoreWorkspaceProjectRows(ctx, rows)
	if err != nil {
		return ports.WorkspaceInfo{}, err
	}
	for _, row := range rows {
		if err := m.upsertWorkspaceProjectRowState(ctx, row, "active"); err != nil {
			return ports.WorkspaceInfo{}, fmt.Errorf("mark repo %s active: %w", row.RepoName, err)
		}
	}
	return workspaceInfoFromRepoInfo(root), nil
}

func (m *Manager) workspaceProjectRestoreRows(ctx context.Context, project domain.ProjectRecord, rec domain.SessionRecord) ([]ports.WorkspaceRepoInfo, error) {
	rows, err := m.store.ListSessionWorktrees(ctx, rec.ID)
	if err != nil {
		return nil, err
	}
	return m.workspaceProjectRestoreRowsFromMarkers(ctx, project, rec, rows)
}

func (m *Manager) workspaceProjectRestoreRowsFromMarkers(ctx context.Context, project domain.ProjectRecord, rec domain.SessionRecord, rows []domain.SessionWorktreeRecord) ([]ports.WorkspaceRepoInfo, error) {
	if len(rows) > 1 {
		return m.sessionWorktreeRowsToRepoInfos(ctx, project, rec, rows)
	}
	childRepos, err := m.store.ListWorkspaceRepos(ctx, project.ID)
	if err != nil {
		return nil, err
	}
	rootPath := rec.Metadata.WorkspacePath
	rootBranch := rec.Metadata.Branch
	var rootBaseSHA string
	if len(rows) == 1 && (rows[0].RepoName == "" || rows[0].RepoName == domain.RootWorkspaceRepoName) {
		rootPath = firstNonEmptyString(rows[0].WorktreePath, rootPath)
		rootBranch = firstNonEmptyString(rows[0].Branch, rootBranch)
		rootBaseSHA = rows[0].BaseSHA
	}
	out := []ports.WorkspaceRepoInfo{{
		RepoName:  domain.RootWorkspaceRepoName,
		RepoPath:  project.Path,
		Path:      rootPath,
		Branch:    rootBranch,
		BaseSHA:   rootBaseSHA,
		SessionID: rec.ID,
		ProjectID: rec.ProjectID,
	}}
	for _, repo := range childRepos {
		out = append(out, ports.WorkspaceRepoInfo{
			RepoName:     repo.Name,
			RepoPath:     filepath.Join(project.Path, filepath.FromSlash(repo.RelativePath)),
			Path:         filepath.Join(rootPath, filepath.FromSlash(repo.RelativePath)),
			Branch:       rootBranch,
			SessionID:    rec.ID,
			ProjectID:    rec.ProjectID,
			RelativePath: repo.RelativePath,
		})
	}
	return out, nil
}

func (m *Manager) workspaceProjectRows(ctx context.Context, rec domain.SessionRecord) ([]ports.WorkspaceRepoInfo, bool, error) {
	rows, err := m.store.ListSessionWorktrees(ctx, rec.ID)
	if err != nil {
		return nil, false, err
	}
	if len(rows) <= 1 {
		return nil, false, nil
	}
	project, err := m.loadProject(ctx, rec.ProjectID)
	if err != nil {
		return nil, false, err
	}
	if project.Kind.WithDefault() != domain.ProjectKindWorkspace {
		return nil, false, nil
	}
	infos, err := m.sessionWorktreeRowsToRepoInfos(ctx, project, rec, rows)
	if err != nil {
		return nil, false, err
	}
	return infos, true, nil
}

func (m *Manager) sessionWorktreeRowsToRepoInfos(ctx context.Context, project domain.ProjectRecord, rec domain.SessionRecord, rows []domain.SessionWorktreeRecord) ([]ports.WorkspaceRepoInfo, error) {
	childRepos, err := m.store.ListWorkspaceRepos(ctx, project.ID)
	if err != nil {
		return nil, err
	}
	repoPaths := map[string]string{domain.RootWorkspaceRepoName: project.Path}
	relPaths := map[string]string{}
	for _, repo := range childRepos {
		repoPaths[repo.Name] = filepath.Join(project.Path, filepath.FromSlash(repo.RelativePath))
		relPaths[repo.Name] = repo.RelativePath
	}
	out := make([]ports.WorkspaceRepoInfo, 0, len(rows))
	for _, row := range rows {
		repoPath := row.RepoPath
		if repoPath == "" {
			repoPath = repoPaths[row.RepoName]
		}
		if repoPath == "" {
			return nil, fmt.Errorf("session worktree row %q no longer matches workspace registry", row.RepoName)
		}
		out = append(out, ports.WorkspaceRepoInfo{
			RepoName:     row.RepoName,
			RepoPath:     repoPath,
			Path:         row.WorktreePath,
			Branch:       firstNonEmptyString(row.Branch, rec.Metadata.Branch),
			BaseSHA:      row.BaseSHA,
			SessionID:    rec.ID,
			ProjectID:    rec.ProjectID,
			RelativePath: relPaths[row.RepoName],
		})
	}
	return out, nil
}

func (m *Manager) saveAndTeardownWorkspaceProject(ctx context.Context, rec domain.SessionRecord, rows []ports.WorkspaceRepoInfo, destroyRuntime bool) error {
	for _, row := range rows {
		ref, err := m.workspace.StashUncommitted(ctx, workspaceInfoFromRepoInfo(row))
		if err != nil {
			return fmt.Errorf("save %s repo %s: stash: %w", rec.ID, row.RepoName, err)
		}
		if err := m.store.UpsertSessionWorktree(ctx, domain.SessionWorktreeRecord{
			SessionID:    rec.ID,
			RepoName:     row.RepoName,
			RepoPath:     row.RepoPath,
			Branch:       row.Branch,
			BaseSHA:      row.BaseSHA,
			WorktreePath: row.Path,
			PreservedRef: ref,
			State:        "removed",
		}); err != nil {
			return fmt.Errorf("save %s repo %s: upsert worktree row: %w", rec.ID, row.RepoName, err)
		}
	}
	if err := m.lcm.MarkTerminated(ctx, rec.ID); err != nil {
		return fmt.Errorf("save %s: mark terminated: %w", rec.ID, err)
	}
	handle := runtimeHandle(rec.Metadata)
	if destroyRuntime && handle.ID != "" {
		if err := m.runtime.Destroy(ctx, handle); err != nil {
			m.logger.Warn("save-teardown-all: runtime destroy failed", "sessionID", rec.ID, "error", err)
		}
	}
	rootDestroyed := false
	for i := len(rows) - 1; i >= 0; i-- {
		info := workspaceInfoFromRepoInfo(rows[i])
		if err := m.workspace.ForceDestroy(ctx, info); err != nil {
			m.logger.Warn("save-teardown-all: force destroy failed", "sessionID", rec.ID, "repo", rows[i].RepoName, "error", err)
		} else if info.Path == rec.Metadata.WorkspacePath {
			rootDestroyed = true
		}
	}
	if rootDestroyed {
		m.cleanupAgentWorkspace(ctx, rec, rec.Metadata.WorkspacePath)
	}
	return nil
}

func (m *Manager) destroyWorkspaceProjectRows(ctx context.Context, rows []ports.WorkspaceRepoInfo) (bool, error) {
	cleaned := false
	var firstErr error
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].Path == "" {
			continue
		}
		info := workspaceInfoFromRepoInfo(rows[i])
		if err := m.workspace.Destroy(ctx, info); err != nil {
			if errors.Is(err, ports.ErrWorkspaceDirty) {
				return cleaned, err
			}
			if stateErr := m.upsertWorkspaceProjectRowState(ctx, rows[i], "retry_remove"); stateErr != nil && firstErr == nil {
				firstErr = stateErr
			}
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if err := m.upsertWorkspaceProjectRowState(ctx, rows[i], "unavailable"); err != nil && firstErr == nil {
			firstErr = err
		}
		cleaned = true
	}
	return cleaned, firstErr
}

func (m *Manager) upsertWorkspaceProjectRowState(ctx context.Context, row ports.WorkspaceRepoInfo, state string) error {
	return m.store.UpsertSessionWorktree(ctx, domain.SessionWorktreeRecord{
		SessionID:    row.SessionID,
		RepoName:     row.RepoName,
		RepoPath:     row.RepoPath,
		Branch:       row.Branch,
		BaseSHA:      row.BaseSHA,
		WorktreePath: row.Path,
		State:        state,
	})
}

func (m *Manager) restoreWorkspaceProjectRows(ctx context.Context, rows []ports.WorkspaceRepoInfo) (ports.WorkspaceRepoInfo, error) {
	var root ports.WorkspaceRepoInfo
	for _, row := range rows {
		restored, err := m.workspace.Restore(ctx, ports.WorkspaceConfig{
			ProjectID: row.ProjectID,
			SessionID: row.SessionID,
			Branch:    row.Branch,
			RepoPath:  row.RepoPath,
			Path:      row.Path,
		})
		if err != nil {
			return ports.WorkspaceRepoInfo{}, fmt.Errorf("repo %s: %w", row.RepoName, err)
		}
		row.Path = restored.Path
		row.Branch = restored.Branch
		if row.RepoName == domain.RootWorkspaceRepoName {
			root = row
		}
	}
	if root.Path == "" {
		return ports.WorkspaceRepoInfo{}, errors.New("workspace project root worktree row missing")
	}
	return root, nil
}

func (m *Manager) applyWorkspaceProjectPreserved(ctx context.Context, rows []ports.WorkspaceRepoInfo) {
	for _, row := range rows {
		var preserveRef string
		sessionRows, err := m.store.ListSessionWorktrees(ctx, row.SessionID)
		if err != nil {
			m.logger.Error("restore-all: list worktrees failed", "sessionID", row.SessionID, "error", err)
			continue
		}
		for _, sessionRow := range sessionRows {
			if sessionRow.RepoName == row.RepoName {
				preserveRef = sessionRow.PreservedRef
				break
			}
		}
		if preserveRef == "" {
			continue
		}
		if applyErr := m.workspace.ApplyPreserved(ctx, workspaceInfoFromRepoInfo(row), preserveRef); applyErr != nil {
			if errors.Is(applyErr, ports.ErrPreservedConflict) {
				m.logger.Warn("restore-all: apply preserved produced conflicts; agent relaunched with conflict markers in place",
					"sessionID", row.SessionID, "repo", row.RepoName, "ref", preserveRef, "error", applyErr)
			} else {
				m.logger.Error("restore-all: apply preserved failed", "sessionID", row.SessionID, "repo", row.RepoName, "error", applyErr)
			}
		}
	}
}

// Send delivers a message to a running session's agent through the guarded
// pane-write primitive, then best-effort confirms the agent actually accepted
// it. The guard refuses delivery into a session that is gone, terminated, or
// paused on a permission decision (pasting there could answer the dialog);
// those refusals surface as typed sentinels so the API reports why instead of
// silently dropping the message. AO has no delivery ack: the messenger returns
// nil the moment the runtime paste + Enter commands exit 0, and for a large
// multiline prompt a single Enter may not submit (claude-code leaves it as an
// unsubmitted draft). confirmActive observes the durable Activity.State
// (flipped to active by the user-prompt-submit hook) and re-sends Enter until
// the session is active or the budget is exhausted. Confirmation never fails
// the send: it only decides whether to nudge again.
func (m *Manager) Send(ctx context.Context, id domain.SessionID, message string) error {
	message, err := m.prepareOutboundMessage(ctx, id, message)
	if err != nil {
		return err
	}
	outcome, err := m.messenger.Deliver(ctx, id, message)
	if err != nil {
		return fmt.Errorf("send %s: %w", id, err)
	}
	switch outcome {
	case sessionguard.SuppressedNotFound:
		return fmt.Errorf("send %s: %w", id, ErrNotFound)
	case sessionguard.SuppressedTerminated:
		return fmt.Errorf("send %s: %w", id, ErrTerminated)
	case sessionguard.SuppressedAwaitingUser:
		return fmt.Errorf("send %s: %w", id, ErrAwaitingDecision)
	}
	// confirmActive only helps — and is only SAFE — when the harness reports
	// both a prompt-submit signal (so the loop can observe active) and a
	// blocked signal it can clear mid-turn (so it can tell an unsubmitted
	// draft from a pending permission dialog and never Enter into the latter).
	// Only claude-code and its hook-delegators (grok/continueagent/devin)
	// satisfy both; every other harness opts out via EmitsBlockedActivity —
	// see ports.ActivitySignaler.
	rec, ok, err := m.store.GetSession(ctx, id)
	if err != nil {
		// Confirmation is best-effort and never fails the send (the message
		// was already delivered above); log so a store error is not swallowed
		// silently.
		m.logger.Warn("send: confirm skipped, session lookup failed", "sessionID", id, "error", err)
		return nil
	}
	if !ok {
		return nil
	}
	if m.harnessNudgeSafe(rec.Harness) {
		m.confirmActive(ctx, m.messenger, id)
	}
	return nil
}

func (m *Manager) prepareOutboundMessage(ctx context.Context, id domain.SessionID, message string) (string, error) {
	rec, ok, err := m.store.GetSession(ctx, id)
	if err != nil {
		return "", fmt.Errorf("send %s: session: %w", id, err)
	}
	if !ok {
		return message, nil
	}
	if rec.Harness != domain.HarnessCopilot || rec.Kind != domain.KindOrchestrator {
		return message, nil
	}
	return copilotOrchestratorMessage(rec.ProjectID, message), nil
}

func copilotOrchestratorMessage(projectID domain.ProjectID, message string) string {
	project := strings.TrimSpace(string(projectID))
	if project == "" {
		project = "<project>"
	}
	return fmt.Sprintf(`AO ORCHESTRATOR DIRECTIVE

You are acting as the AO orchestrator for project %s. Do not implement code changes, edit files, run implementation tests, or complete the user's task yourself.

Your next action for any implementation, fix, UI change, test, PR, or code-review task must be to spawn or redirect a worker session. Use:

ao spawn --project %s --issue <issue-id> --prompt "<clear worker task>"

If a suitable worker already exists, use ao send to redirect that worker instead. After spawning or redirecting, report the worker session id and stop. Do not do the worker's task in this orchestrator session.

USER MESSAGE:
%s`, project, project, message)
}

// harnessNudgeSafe reports whether the session's harness is safe to nudge with
// an Enter-only re-send (see ports.ActivitySignaler): it must emit BOTH a
// prompt-submit signal (else the loop wastes its budget never observing active)
// and a blocked signal (else an Enter meant to resubmit a draft could answer a
// permission dialog the harness cannot report).
func (m *Manager) harnessNudgeSafe(harness domain.AgentHarness) bool {
	if m.agents == nil {
		return false
	}
	agent, ok := m.agents.Agent(harness)
	if !ok {
		return false
	}
	s, ok := agent.(ports.ActivitySignaler)
	return ok && s.EmitsSubmitActivity() && s.EmitsBlockedActivity()
}

// waitOutcome is one poll round's verdict on whether confirmActive should
// nudge again.
type waitOutcome int

const (
	// waitTimedOut: the deadline elapsed without the session going active —
	// the previous Enter likely did not land, another may help.
	waitTimedOut waitOutcome = iota
	// waitActive: the session went active — the prompt was accepted, done.
	waitActive
	// waitBlocked: the session is paused on a user decision (a pending
	// permission/approval dialog) — an automated Enter could answer the dialog
	// on the user's behalf, so confirmation must stop and never nudge.
	waitBlocked
)

// confirmActive re-sends Enter until the session reports ActivityActive or the
// attempt budget is exhausted. The initial Send already submitted one Enter;
// each additional attempt sends Enter again (an empty message is an Enter-only
// nudge, see ports.AgentMessenger) after waiting for Activity.State to flip. It
// is best-effort: on context cancellation, store failure, or budget exhaustion
// it returns silently (the message was already delivered; the agent may yet
// pick it up). Harnesses without a user-prompt-submit hook never flip to
// active, so the loop simply times out — Send remains successful for them.
//
// Decision safety: a session observed in ActivityBlocked stops confirmation
// immediately with no nudge — an Enter into a pending permission dialog would
// answer it for the user. Sticky ActivityWaitingInput does NOT stop the loop:
// an idle-prompt session with an unsubmitted pasted draft is exactly the case
// the nudge exists for.
func (m *Manager) confirmActive(ctx context.Context, guard *sessionguard.Guard, id domain.SessionID) {
	for attempt := 1; ; attempt++ {
		outcome, err := m.waitForActive(ctx, id)
		if err != nil || outcome == waitActive {
			return
		}
		if outcome == waitBlocked {
			m.logger.Info("send: session awaiting a decision; skipping Enter nudge", "sessionID", id, "attempt", attempt)
			return
		}
		if attempt >= m.sendConfirm.maxAttempts {
			return
		}
		// Timed out with budget remaining: the previous Enter did not land.
		// Nudge again with an Enter-only send. Deliver re-reads state
		// immediately before pasting — a permission dialog can appear in the
		// gap between waitForActive's final poll and this send, and an Enter
		// into it would answer the decision. This closes the TOCTOU the
		// per-poll check inside waitForActive cannot cover; a store failure
		// inside the guard fails closed (no Enter on an unknown state).
		nudge, nudgeErr := guard.Deliver(ctx, id, "")
		if nudgeErr != nil {
			m.logger.Warn("send: confirm re-send failed", "sessionID", id, "attempt", attempt, "error", nudgeErr)
			return
		}
		if nudge != sessionguard.Sent {
			// Not necessarily blocked: the session may also have terminated or
			// vanished since the poll — the outcome says which.
			m.logger.Info("send: session unavailable before nudge; skipping Enter nudge", "sessionID", id, "attempt", attempt, "outcome", nudge.String())
			return
		}
	}
}

// waitForActive polls Activity.State for up to attemptDeadline and reports
// whether another nudge could help (see waitOutcome). Blocked is checked every
// poll so a permission dialog appearing mid-wait aborts immediately instead of
// burning the deadline. A non-nil error means polling cannot continue (ctx
// cancelled, store failure, session gone).
func (m *Manager) waitForActive(ctx context.Context, id domain.SessionID) (waitOutcome, error) {
	deadlineAt := m.clock().Add(m.sendConfirm.attemptDeadline)
	ticker := time.NewTicker(m.sendConfirm.pollInterval)
	defer ticker.Stop()
	for {
		rec, ok, err := m.store.GetSession(ctx, id)
		if err != nil {
			return waitTimedOut, err
		}
		if !ok {
			return waitTimedOut, fmt.Errorf("session %s not found", id)
		}
		switch rec.Activity.State {
		case domain.ActivityActive:
			return waitActive, nil
		case domain.ActivityBlocked:
			return waitBlocked, nil
		}
		if !m.clock().Before(deadlineAt) {
			return waitTimedOut, nil
		}
		// The tick select respects ctx cancellation so a request timeout
		// unblocks promptly.
		select {
		case <-ctx.Done():
			return waitTimedOut, ctx.Err()
		case <-ticker.C:
		}
	}
}

// CleanupSkip reports one terminal session whose workspace was preserved
// rather than reclaimed, and why.
type CleanupSkip struct {
	SessionID domain.SessionID
	Reason    string
}

// CleanupResult reports what Cleanup reclaimed and what it preserved.
type CleanupResult struct {
	Cleaned []domain.SessionID
	Skipped []CleanupSkip
}

// Cleanup reclaims the workspaces of terminal sessions in a project. A workspace
// whose teardown is refused (uncommitted work) is never forced; it is reported
// in Skipped with the reason so the refusal is visible instead of silent.
func (m *Manager) Cleanup(ctx context.Context, project domain.ProjectID) (CleanupResult, error) {
	recs, err := m.cleanupRecords(ctx, project)
	if err != nil {
		return CleanupResult{}, fmt.Errorf("cleanup %s: %w", project, err)
	}
	// Resolved once for the whole pass: a workspace is destroyed only by the
	// session that still owns it. Role workspaces and role branches are
	// canonical and therefore shared across successive role rows, so a
	// terminated row routinely points at the live replacement's worktree.
	live, err := m.liveSessions(ctx)
	if err != nil {
		return CleanupResult{}, fmt.Errorf("cleanup %s: %w", project, err)
	}
	result := CleanupResult{Cleaned: make([]domain.SessionID, 0, len(recs)), Skipped: []CleanupSkip{}}
	for _, rec := range recs {
		if !rec.IsTerminated {
			continue
		}
		ws := workspaceInfoForTeardown(rec, m.dataDir)
		if ws.Path == "" {
			m.cleanupSystemPromptDir(rec.ID)
			continue
		}
		if owner, inUse := workspaceOwnedByLiveSession(rec, live); inUse {
			m.logger.Info("cleanup: workspace still owned by a live session; preserving",
				"sessionID", rec.ID, "path", ws.Path, "owner", owner)
			result.Skipped = append(result.Skipped, CleanupSkip{SessionID: rec.ID, Reason: skipReasonWorkspaceInUse})
			continue
		}
		if h := runtimeHandle(rec.Metadata); h.ID != "" {
			_ = m.runtime.Destroy(ctx, h) // best effort; usually already gone
		}
		if rows, ok, rowErr := m.workspaceProjectRows(ctx, rec); rowErr != nil {
			m.logger.Warn("cleanup: workspace rows failed", "sessionID", rec.ID, "error", rowErr)
			result.Skipped = append(result.Skipped, CleanupSkip{SessionID: rec.ID, Reason: "workspace teardown failed"})
			continue
		} else if ok {
			if _, err := m.destroyWorkspaceProjectRows(ctx, rows); err != nil {
				if !errors.Is(err, ports.ErrWorkspaceDirty) {
					m.logger.Warn("cleanup: workspace teardown failed", "sessionID", rec.ID, "path", ws.Path, "error", err)
				}
				result.Skipped = append(result.Skipped, CleanupSkip{SessionID: rec.ID, Reason: cleanupSkipReason(err)})
				continue
			}
			m.cleanupAgentWorkspace(ctx, rec, ws.Path)
		} else if err := m.workspace.Destroy(ctx, ws); err != nil {
			if !errors.Is(err, ports.ErrWorkspaceDirty) {
				// The public reason stays a fixed string (the raw error carries
				// internal filesystem paths); the full cause lands here.
				m.logger.Warn("cleanup: workspace teardown failed", "sessionID", rec.ID, "path", ws.Path, "error", err)
			}
			result.Skipped = append(result.Skipped, CleanupSkip{SessionID: rec.ID, Reason: cleanupSkipReason(err)})
			continue
		} else {
			m.cleanupAgentWorkspace(ctx, rec, ws.Path)
		}
		m.cleanupSystemPromptDir(rec.ID)
		result.Cleaned = append(result.Cleaned, rec.ID)
	}
	return result, nil
}

// cleanupSkipReason renders a workspace teardown refusal as a short
// user-facing reason for the cleanup report. Deliberately not the raw error:
// it flows to the API response and CLI output, and teardown errors embed
// internal filesystem paths.
func cleanupSkipReason(err error) string {
	if errors.Is(err, ports.ErrWorkspaceDirty) {
		return "workspace has uncommitted changes"
	}
	if errors.Is(err, ErrProjectNotResolvable) {
		return "project is archived or unregistered — remove worktree manually"
	}
	if errors.Is(err, ports.ErrWorkspaceRepoMismatch) {
		return "workspace belongs to a different repo — remove worktree manually"
	}
	return "workspace teardown failed"
}

func (m *Manager) cleanupRecords(ctx context.Context, project domain.ProjectID) ([]domain.SessionRecord, error) {
	if project == "" {
		return m.store.ListAllSessions(ctx)
	}
	return m.store.ListSessions(ctx, project)
}

// ---- helpers ----

// seedRecord builds the initial session row. mixSelected comes from
// resolveSpawnTarget — the one place that knows whether the worker mix, rather
// than an explicit pin, chose this (harness, model) — and is recorded so the
// census counts only the mix's own selections.
func seedRecord(cfg ports.SpawnConfig, effort domain.Effort, mixSelected bool, mixBucketModel string, now time.Time) domain.SessionRecord {
	return domain.SessionRecord{
		ProjectID:      cfg.ProjectID,
		IssueID:        cfg.IssueID,
		Kind:           cfg.Kind,
		CreatedAt:      now,
		UpdatedAt:      now,
		Harness:        cfg.Harness,
		Model:          cfg.Model,
		Effort:         effort,
		MixSelected:    mixSelected,
		MixBucketModel: mixBucketModel,
		DisplayName:    cfg.DisplayName,
		Activity:       domain.Activity{State: domain.ActivityIdle, LastActivityAt: now},
	}
}

func defaultSessionBranch(id domain.SessionID, kind domain.SessionKind, prefix, branchNamespace string) string {
	switch kind {
	case domain.KindOrchestrator:
		return aoBranch(branchNamespace, prefix+"-orchestrator")
	case domain.KindPrime:
		if strings.TrimSpace(prefix) == "" {
			return aoBranch(branchNamespace, "prime")
		}
		return aoBranch(branchNamespace, prefix+"-prime")
	}
	// A fresh, unique branch per worker session: gitworktree can't add a worktree
	// on a branch already checked out elsewhere (e.g. main). Put the root work
	// branch under a session namespace so sibling PR branches such as
	// ao/<session>/<topic> remain valid Git refs.
	return aoBranch(branchNamespace, string(id), "root")
}

// DefaultSpawnBranch returns AO's generated work branch for a spawn. Explicit
// user-provided branches bypass this helper.
func DefaultSpawnBranch(id domain.SessionID, kind domain.SessionKind, prefix string, projectKind domain.ProjectKind, dataDir string) string {
	if projectKind == domain.ProjectKindScratch {
		return ""
	}
	branchNamespace := generatedBranchNamespace(dataDir)
	if projectKind == domain.ProjectKindWorkspace {
		return aoBranch(branchNamespace, string(id))
	}
	return defaultSessionBranch(id, kind, prefix, branchNamespace)
}

// DefaultOrchestratorBranch returns the generated canonical orchestrator branch
// for a project in the current data-dir namespace.
func DefaultOrchestratorBranch(prefix, dataDir string) string {
	return defaultSessionBranch("", domain.KindOrchestrator, prefix, generatedBranchNamespace(dataDir))
}

// DefaultPrimeBranch returns the generated canonical branch for the projectless
// fleet Prime singleton in the current data-dir namespace.
func DefaultPrimeBranch(dataDir string) string {
	return defaultSessionBranch("", domain.KindPrime, "", generatedBranchNamespace(dataDir))
}

func fleetPrimeRepoPath(dataDir string) string {
	return filepath.Join(dataDir, "prime", "repo")
}

func singleRepoOverridePath(rec domain.SessionRecord, dataDir string) string {
	if rec.Kind == domain.KindPrime && rec.ProjectID == "" {
		return fleetPrimeRepoPath(dataDir)
	}
	return ""
}

func aoBranch(namespace string, parts ...string) string {
	all := []string{"ao"}
	if namespace != "" {
		all = append(all, namespace)
	}
	all = append(all, parts...)
	return strings.Join(all, "/")
}

func generatedBranchNamespace(dataDir string) string {
	if isDefaultDevDataDir(dataDir) {
		return "dev"
	}
	return ""
}

func isDefaultDevDataDir(dataDir string) bool {
	if strings.TrimSpace(dataDir) == "" {
		return false
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	want, err := filepath.Abs(filepath.Join(home, ".ao", "dev", "data"))
	if err != nil {
		return false
	}
	got, err := filepath.Abs(dataDir)
	if err != nil {
		return false
	}
	return filepath.Clean(got) == filepath.Clean(want)
}

func buildPrompt(cfg ports.SpawnConfig) string {
	return buildTaskPrompt(taskPromptConfig{
		Role:         promptRoleForKind(cfg.Kind),
		Prompt:       cfg.Prompt,
		IssueID:      string(cfg.IssueID),
		IssueContext: cfg.IssueContext,
	})
}

func promptRoleForKind(kind domain.SessionKind) sessionPromptRole {
	switch kind {
	case domain.KindOrchestrator:
		return sessionPromptRoleOrchestrator
	case domain.KindPrime:
		return sessionPromptRolePrime
	case domain.KindWorker:
		return sessionPromptRoleWorker
	default:
		return ""
	}
}

func promptProjectContext(projectID domain.ProjectID, project domain.ProjectRecord) promptProject {
	cfg := project.Config.WithDefaults()
	if project.Kind.WithDefault() == domain.ProjectKindScratch {
		cfg.DefaultBranch = ""
	}
	id := project.ID
	if strings.TrimSpace(id) == "" {
		id = string(projectID)
	}
	return promptProject{
		ID:            id,
		Name:          project.DisplayName,
		Repo:          project.RepoOriginURL,
		DefaultBranch: cfg.DefaultBranch,
		Path:          project.Path,
	}
}

// attachmentsDir is the worktree-relative directory where spawn image
// attachments are written.
const attachmentsDir = ".ao/attachments"

// writeSpawnAttachments writes each attachment into the worktree under
// attachmentsDir as image-1<ext>, image-2<ext>, ... and returns the
// worktree-relative paths in order. The files are excluded from git via the
// worktree's info/exclude so they do not dirty the working tree.
func writeSpawnAttachments(workspacePath string, attachments []ports.SpawnAttachment) ([]string, error) {
	dir := filepath.Join(workspacePath, filepath.FromSlash(attachmentsDir))
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("create attachments dir: %w", err)
	}
	refs := make([]string, 0, len(attachments))
	for i, a := range attachments {
		ext := a.Ext
		if ext == "" {
			ext = ".bin"
		}
		name := fmt.Sprintf("image-%d%s", i+1, ext)
		if err := os.WriteFile(filepath.Join(dir, name), a.Data, 0o600); err != nil {
			return nil, fmt.Errorf("write attachment %d: %w", i+1, err)
		}
		// Worktree-relative reference, always forward-slashed for the prompt.
		refs = append(refs, attachmentsDir+"/"+name)
	}
	return refs, nil
}

// appendAttachmentReferences appends a block listing the attached image paths so
// the agent knows to read them. Placed after the human's brief.
func appendAttachmentReferences(prompt string, refs []string) string {
	if len(refs) == 0 {
		return prompt
	}
	var b strings.Builder
	b.WriteString(prompt)
	if strings.TrimSpace(prompt) != "" {
		b.WriteString("\n\n")
	}
	b.WriteString("Attached images (read these files in the workspace for visual context):")
	for _, ref := range refs {
		b.WriteString("\n- ")
		b.WriteString(ref)
	}
	return b.String()
}

// buildSpawnTexts returns the user-facing prompt and the system prompt to
// deliver separately to the agent. Orchestrator role instructions and worker
// coordination hints are placed in the system prompt so they are treated as
// standing instructions rather than part of the human's task request. A
// promptless spawn delivers no user prompt at all: the agent simply lands at an
// empty input box rather than receiving an auto-generated kickoff turn.
func (m *Manager) buildSpawnTexts(ctx context.Context, cfg ports.SpawnConfig) (prompt, systemPrompt string, err error) {
	prompt = buildPrompt(cfg)
	systemPrompt, err = m.buildSystemPrompt(ctx, cfg.Kind, cfg.ProjectID)
	if err != nil {
		return "", "", err
	}
	return prompt, systemPrompt, nil
}

// RoleSystemPrompt assembles the exact system prompt a worker or orchestrator
// session receives for a project, for operator inspection. It reuses the same
// assembly path as spawn (buildSystemPrompt), so what the operator sees matches
// what an agent would get if spawned right now — including any operator rules
// override, and the same fail-closed error when that override is misconfigured.
func (m *Manager) RoleSystemPrompt(ctx context.Context, kind domain.SessionKind, projectID domain.ProjectID) (string, error) {
	return m.buildSystemPrompt(ctx, kind, projectID)
}

// buildSystemPrompt derives the standing instructions for a session of the
// given kind from current store state. Restore recomputes them through here
// rather than persisting them, so a restored worker points at the orchestrator
// that is active now, not the one from its original spawn.
func (m *Manager) buildSystemPrompt(ctx context.Context, kind domain.SessionKind, projectID domain.ProjectID) (string, error) {
	if kind == domain.KindPrime && projectID == "" {
		settings, err := m.store.GetPrimeSettings(ctx)
		if err != nil {
			return "", err
		}
		cfg := systemPromptConfig{
			Role:    sessionPromptRolePrime,
			Project: promptProject{ID: "fleet", Name: "AO Fleet"},
		}
		rules, err := LoadRoleRules(RoleRulesConfig{
			Role:        "prime",
			ProjectID:   "",
			ProjectPath: "",
			InlineRules: settings.Rules,
			RulesFile:   settings.RulesFile,
		})
		if err != nil {
			return "", err
		}
		cfg.PrimeRules = rules
		return buildSystemPromptText(cfg), nil
	}
	project, err := m.loadProject(ctx, projectID)
	if err != nil {
		return "", err
	}
	cfg := systemPromptConfig{
		Role:    promptRoleForKind(kind),
		Project: promptProjectContext(projectID, project),
	}

	switch kind {
	case domain.KindOrchestrator:
		rules, err := LoadRoleRules(RoleRulesConfig{
			Role:        "orchestrator",
			ProjectID:   string(projectID),
			ProjectPath: project.Path,
			InlineRules: project.Config.OrchestratorRules,
			RulesFile:   project.Config.OrchestratorRulesFile,
		})
		if err != nil {
			return "", err
		}
		cfg.OrchestratorRules = rules
	case domain.KindPrime:
		rules, err := LoadRoleRules(RoleRulesConfig{
			Role:        "prime",
			ProjectID:   string(projectID),
			ProjectPath: project.Path,
			InlineRules: project.Config.PrimeRules,
			RulesFile:   project.Config.PrimeRulesFile,
		})
		if err != nil {
			return "", err
		}
		cfg.PrimeRules = rules
	case domain.KindWorker:
		orchestratorID, ok, err := m.activeOrchestratorSessionID(ctx, projectID)
		if err != nil {
			return "", err
		}
		if ok {
			cfg.OrchestratorSessionID = string(orchestratorID)
		}
		rules, err := LoadRoleRules(RoleRulesConfig{
			Role:        "worker",
			ProjectID:   string(projectID),
			ProjectPath: project.Path,
			InlineRules: project.Config.AgentRules,
			RulesFile:   project.Config.AgentRulesFile,
		})
		if err != nil {
			return "", err
		}
		cfg.ProjectRules = rules
	default:
		return "", nil
	}

	workspacePrompt, err := m.workspaceProjectPrompt(ctx, kind, projectID)
	if err != nil {
		return "", err
	}
	if workspacePrompt != "" {
		cfg.AdditionalSections = append(cfg.AdditionalSections, workspacePrompt)
	}
	if pointer := strings.TrimSpace(m.aoSkillPointer()); pointer != "" {
		cfg.AdditionalSections = append(cfg.AdditionalSections, pointer)
	}
	return buildSystemPromptText(cfg), nil
}

// aoSkillPointer is appended to every agent system prompt. It points the agent
// at the using-ao skill the daemon installs under the data dir, rather than
// inlining the whole CLI catalog. The path is absolute so it resolves from any
// project's worktree, not just the AO repo (the only place a repo-relative
// skills/ path would exist). The skill file carries exact flags and examples,
// so the standing prompt stays a short pointer rather than a command dump.
func (m *Manager) aoSkillPointer() string {
	dir := skillassets.Dir(m.dataDir)
	skillFile := filepath.ToSlash(filepath.Join(dir, "SKILL.md"))
	commandsGlob := filepath.ToSlash(filepath.Join(dir, "commands", "*.md"))
	return "\n\n" + "## Using the ao CLI\n\n" +
		"When you need to use the `ao` CLI, read `" + skillFile + "` first (and the relevant `" + commandsGlob + "`) for the full command catalog, flags, and examples."
}

func (m *Manager) workspaceProjectPrompt(ctx context.Context, kind domain.SessionKind, projectID domain.ProjectID) (string, error) {
	project, err := m.loadProject(ctx, projectID)
	if err != nil {
		return "", err
	}
	if project.Kind.WithDefault() != domain.ProjectKindWorkspace {
		return "", nil
	}
	repos, err := m.store.ListWorkspaceRepos(ctx, string(projectID))
	if err != nil {
		return "", fmt.Errorf("list workspace repos for prompt: %w", err)
	}
	switch kind {
	case domain.KindOrchestrator:
		return workspaceOrchestratorPrompt(repos), nil
	case domain.KindWorker:
		return workspaceWorkerPrompt(repos), nil
	default:
		return "", nil
	}
}

func (m *Manager) activeOrchestratorSessionID(ctx context.Context, project domain.ProjectID) (domain.SessionID, bool, error) {
	recs, err := m.store.ListSessions(ctx, project)
	if err != nil {
		return "", false, fmt.Errorf("list sessions for %s: %w", project, err)
	}
	for _, rec := range recs {
		if rec.Kind == domain.KindOrchestrator && !rec.IsTerminated {
			return rec.ID, true, nil
		}
	}
	return "", false, nil
}

func (m *Manager) writeSystemPromptFile(id domain.SessionID, systemPrompt string) (string, error) {
	if systemPrompt == "" || strings.TrimSpace(m.dataDir) == "" {
		return "", nil
	}
	path := filepath.Join(m.systemPromptDir(id), "system.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(strings.TrimRight(systemPrompt, "\n")+"\n"), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func (m *Manager) prepareSystemPromptFile(id domain.SessionID, harness domain.AgentHarness, systemPrompt string) (string, error) {
	path, err := m.writeSystemPromptFile(id, systemPrompt)
	if err == nil || path != "" {
		return path, err
	}
	if systemPromptFileRequired(harness) {
		return "", err
	}
	m.logger.Warn("system prompt file unavailable; falling back to inline system prompt", "session", id, "harness", harness, "err", err)
	return "", nil
}

func systemPromptFileRequired(harness domain.AgentHarness) bool {
	switch harness {
	case domain.HarnessAider,
		domain.HarnessAgy,
		domain.HarnessAuggie,
		domain.HarnessKiro,
		domain.HarnessOpenCode,
		domain.HarnessCopilot,
		domain.HarnessVibe:
		return true
	default:
		return false
	}
}

func (m *Manager) systemPromptDir(id domain.SessionID) string {
	if strings.TrimSpace(m.dataDir) == "" {
		return ""
	}
	return filepath.Join(m.dataDir, "prompts", string(id))
}

func (m *Manager) cleanupSystemPromptDir(id domain.SessionID) {
	dir := m.systemPromptDir(id)
	if dir == "" {
		return
	}
	if err := os.RemoveAll(dir); err != nil {
		m.logger.Warn("system prompt cleanup failed", "session", id, "path", dir, "err", err)
	}
}

func workspaceOrchestratorPrompt(repos []domain.WorkspaceRepoRecord) string {
	return fmt.Sprintf(`## Workspace project

This project is a multi-repository workspace. Sessions start at the workspace root. The root repository is %s at path `+"`.`"+`; child repositories are nested below it.

Repositories:
%s

When spawning workers, name the repository path or paths they should work in. Work can span multiple repositories, so track deliverables, pull requests, and checks by repository.`, domain.RootWorkspaceRepoName, workspaceRepoList(repos))
}

func workspaceWorkerPrompt(repos []domain.WorkspaceRepoRecord) string {
	return fmt.Sprintf(`## Workspace project

This session is a multi-repository workspace. You start at the workspace root. The root repository is %s at path `+"`.`"+`; child repositories are nested below it.

Repositories:
%s

Before editing, identify which repository owns the task and keep changes scoped to the requested repository or repositories. If you touch root files, call that out explicitly because root changes are separate from child-repository changes.`, domain.RootWorkspaceRepoName, workspaceRepoList(repos))
}

func workspaceRepoList(repos []domain.WorkspaceRepoRecord) string {
	lines := make([]string, 0, 1+len(repos))
	lines = append(lines, fmt.Sprintf("- %s: .", domain.RootWorkspaceRepoName))
	for _, repo := range repos {
		lines = append(lines, fmt.Sprintf("- %s: %s", repo.Name, repo.RelativePath))
	}
	return strings.Join(lines, "\n")
}

// spawnEnv builds the runtime environment: the per-project env vars first, then
// the AO-internal vars last so they always win (a project cannot override
// AO_SESSION_ID and friends).
func spawnEnv(id domain.SessionID, project domain.ProjectID, issue domain.IssueID, dataDir, runtimeToken string, projectEnv map[string]string) map[string]string {
	env := make(map[string]string, len(projectEnv)+5)
	for k, v := range projectEnv {
		env[k] = v
	}
	env[EnvSessionID] = string(id)
	env[EnvProjectID] = string(project)
	env[EnvIssueID] = string(issue)
	env[EnvRuntimeToken] = runtimeToken
	env[EnvDataDir] = dataDir
	return env
}

// runtimeEnv is spawnEnv plus the hook PATH pin: the session's PATH puts the
// running daemon's own directory first, so the bare `ao` in workspace hook
// commands resolves to the daemon that installed them rather than whatever
// `ao` is first on the inherited PATH (e.g. a legacy CLI without the hooks
// command, which fails every callback and silently kills activity tracking).
// When the pin cannot be applied the inherited PATH is kept and a warning is
// logged so the degradation isn't silent.
func (m *Manager) runtimeEnv(id domain.SessionID, project domain.ProjectID, issue domain.IssueID, runtimeToken string, projectEnv map[string]string) map[string]string {
	env := spawnEnv(id, project, issue, m.dataDir, runtimeToken, projectEnv)
	path, err := HookPATH(m.executable, os.Getenv, projectEnv)
	if err != nil {
		m.logger.Warn("session PATH not pinned to the daemon binary; `ao hooks` callbacks may resolve to a different ao and activity tracking will stall",
			"session", id, "error", err)
		return env
	}
	env["PATH"] = path
	return env
}

func newRuntimeToken() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func (m *Manager) verifyLaunchCommandRunning(ctx context.Context, handle ports.RuntimeHandle, command string) error {
	prober, ok := m.runtime.(launchProcessProber)
	if !ok {
		return nil
	}
	attempts := m.launchProbe.attempts
	if attempts < 1 {
		attempts = 1
	}
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		running, err := prober.IsRunningCommand(ctx, handle, command)
		if err != nil {
			alive, aliveErr := m.runtime.IsAlive(ctx, handle)
			if alive && aliveErr == nil {
				m.logger.Warn("spawn: launch-process probe failed but runtime session is alive; keeping session",
					"handle", handle.ID, "command", command, "error", err)
				return nil
			}
			if aliveErr != nil {
				return errors.Join(err, fmt.Errorf("session liveness probe: %w", aliveErr))
			}
			return err
		}
		if running {
			return nil
		}
		lastErr = fmt.Errorf("%q was not running after %d launch-process probe attempts", command, attempts)
		if attempt < attempts {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(m.launchProbe.retryDelay):
			}
		}
	}
	return lastErr
}

func launchProcessCommand(argv []string) string {
	bin, ok := launchBinary(argv)
	if ok {
		return bin
	}
	if len(argv) == 0 {
		return ""
	}
	return argv[0]
}

// HookPATH builds the PATH value pinned into a spawned session: the daemon
// executable's directory prepended to the base PATH (the project's PATH
// override when set, else the daemon's inherited PATH — matching what the
// runtime would have exported anyway). An error means the pin cannot be
// applied: the executable is unresolvable, or is not named "ao", in which case
// prepending its directory would not change what `ao` resolves to. Exported so
// the reviewer launcher can pin its pane's PATH the same way.
func HookPATH(executable func() (string, error), getenv func(string) string, projectEnv map[string]string) (string, error) {
	exe, err := executable()
	if err != nil {
		return "", fmt.Errorf("resolve daemon executable: %w", err)
	}
	name := filepath.Base(exe)
	if runtime.GOOS == "windows" {
		name = strings.TrimSuffix(strings.ToLower(name), ".exe")
	}
	if name != hookBinaryName {
		return "", fmt.Errorf("daemon executable %s is not named %q", exe, hookBinaryName)
	}
	base := projectEnv["PATH"]
	if base == "" {
		base = getenv("PATH")
	}
	dir := filepath.Dir(exe)
	if base == "" {
		return dir, nil
	}
	return dir + string(os.PathListSeparator) + base, nil
}

// provisionWorkspace applies the project's per-workspace setup after the
// worktree exists: symlink shared files from the project repo, then run any
// post-create commands. Either failing aborts the spawn so a half-provisioned
// workspace never launches an agent.
func (m *Manager) provisionWorkspace(ctx context.Context, project domain.ProjectRecord, workspacePath string) error {
	if err := applySymlinks(project.Path, workspacePath, project.Config.Symlinks); err != nil {
		return err
	}
	return runPostCreate(ctx, workspacePath, project.Config.PostCreate)
}

// applySymlinks links each repo-relative path into the workspace. A source that
// does not exist is skipped (symlinks are a convenience for optional files like
// .env); a real link failure aborts. Paths must be repo-relative with no
// parent traversal (no leading "/", no ".." segment) — a bad path is refused
// up front so a project config cannot escape the project or workspace tree.
func applySymlinks(projectPath, workspacePath string, symlinks []string) error {
	for _, rel := range symlinks {
		rel = strings.TrimSpace(rel)
		if rel == "" {
			continue
		}
		clean, err := safeRelPath(rel)
		if err != nil {
			return fmt.Errorf("symlink %q: %w", rel, err)
		}
		source := filepath.Join(projectPath, clean)
		if _, err := os.Stat(source); err != nil {
			continue
		}
		target := filepath.Join(workspacePath, clean)
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			return fmt.Errorf("symlink %q: %w", rel, err)
		}
		if _, err := os.Lstat(target); err == nil {
			continue
		}
		if err := os.Symlink(source, target); err != nil {
			return fmt.Errorf("symlink %q: %w", rel, err)
		}
	}
	return nil
}

// safeRelPath confines rel to a repo-relative path: no absolute paths and no
// ".." segments (before or after Clean). The cleaned form is returned so
// callers join it against project/workspace roots safely.
func safeRelPath(rel string) (string, error) {
	if filepath.IsAbs(rel) || strings.HasPrefix(rel, "/") || strings.HasPrefix(rel, `\`) {
		return "", fmt.Errorf("path must be repo-relative")
	}
	clean := filepath.Clean(rel)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == "." || clean == "" {
		return "", fmt.Errorf("path must be repo-relative")
	}
	for _, seg := range strings.Split(filepath.ToSlash(clean), "/") {
		if seg == ".." {
			return "", fmt.Errorf("path must be repo-relative")
		}
	}
	return clean, nil
}

// runPostCreate runs each post-create command in the workspace via the platform
// shell, so OS-agnostic commands like "pnpm install" work. A non-zero exit
// aborts the spawn with the command output.
func runPostCreate(ctx context.Context, workspacePath string, commands []string) error {
	for _, command := range commands {
		command = strings.TrimSpace(command)
		if command == "" {
			continue
		}
		var cmd *exec.Cmd
		if runtime.GOOS == "windows" {
			cmd = aoprocess.CommandContext(ctx, "cmd", "/c", command)
		} else {
			cmd = aoprocess.CommandContext(ctx, "sh", "-c", command)
		}
		cmd.Dir = workspacePath
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("postCreate %q: %w: %s", command, err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

// preLauncher is an optional Agent capability: a step the manager runs before
// launch. Claude Code implements it to record workspace trust in ~/.claude.json
// so its interactive "do you trust this folder?" dialog can't block the headless
// pane. Adapters that don't need it simply omit the method.
type preLauncher interface {
	PreLaunch(ctx context.Context, cfg ports.LaunchConfig) error
}

// workspaceCleaner is an optional Agent capability for durable agent-side state
// that should be released only after AO has actually removed the workspace.
type workspaceCleaner interface {
	CleanupWorkspace(ctx context.Context, cfg ports.WorkspaceHookConfig) error
}

type runtimeEnvAugmenter interface {
	AugmentRuntimeEnv(env map[string]string, dataDir string)
}

func (m *Manager) augmentAgentRuntimeEnv(agent ports.Agent, env map[string]string) {
	if augmenter, ok := agent.(runtimeEnvAugmenter); ok {
		augmenter.AugmentRuntimeEnv(env, m.dataDir)
	}
}

// prepareWorkspace runs the per-session pre-launch steps before the runtime
// starts the agent: installing the workspace-local activity hooks (so early
// startup hooks can update the already-created session row), then any optional
// PreLaunch step. Shared by Spawn and Restore.
func (m *Manager) prepareWorkspace(ctx context.Context, agent ports.Agent, id domain.SessionID, workspacePath, systemPrompt, systemPromptFile string, agentConfig ports.AgentConfig, env map[string]string) error {
	if err := agent.GetAgentHooks(ctx, ports.WorkspaceHookConfig{
		SessionID:        string(id),
		WorkspacePath:    workspacePath,
		DataDir:          m.dataDir,
		Env:              env,
		SystemPrompt:     systemPrompt,
		SystemPromptFile: systemPromptFile,
		Config:           agentConfig,
	}); err != nil {
		m.cleanupPreparedAgentWorkspace(ctx, agent, id, workspacePath, env)
		return fmt.Errorf("install hooks: %w", err)
	}
	if pl, ok := agent.(preLauncher); ok {
		if err := pl.PreLaunch(ctx, ports.LaunchConfig{DataDir: m.dataDir, SessionID: string(id), WorkspacePath: workspacePath}); err != nil {
			m.cleanupPreparedAgentWorkspace(ctx, agent, id, workspacePath, env)
			return fmt.Errorf("pre-launch: %w", err)
		}
	}
	return nil
}

func (m *Manager) cleanupPreparedAgentWorkspace(ctx context.Context, agent ports.Agent, id domain.SessionID, workspacePath string, env map[string]string) {
	cleaner, ok := agent.(workspaceCleaner)
	if !ok {
		return
	}
	cleanupCtx, cancel := cleanupContext(ctx)
	defer cancel()
	if err := cleaner.CleanupWorkspace(cleanupCtx, ports.WorkspaceHookConfig{
		SessionID:     string(id),
		WorkspacePath: workspacePath,
		DataDir:       m.dataDir,
		Env:           env,
	}); err != nil {
		m.logger.Warn("session prepare rollback: failed to clean agent workspace state",
			"session", id, "workspacePath", workspacePath, "error", err)
	}
}

func (m *Manager) cleanupAgentWorkspace(ctx context.Context, rec domain.SessionRecord, workspacePath string) {
	if strings.TrimSpace(workspacePath) == "" {
		return
	}
	agent, ok := m.agents.Agent(rec.Harness)
	if !ok {
		return
	}
	cleaner, ok := agent.(workspaceCleaner)
	if !ok {
		return
	}
	// Deliberately runs on the context it is given rather than deriving its own
	// cleanup context. Unlike the other teardown helpers this one is not
	// compensation-only: Kill, Cleanup, RetireForReplacement and stale-role
	// release all call it as part of the teardown the caller actually asked for,
	// and those callers are entitled to abandon it. On the rollback path it is
	// already detached, because rollbackPreparedSpawnWorkspace hands it that
	// cleanup context — which also keeps the pair on one budget instead of two.
	env := spawnEnv(rec.ID, rec.ProjectID, rec.IssueID, m.dataDir, rec.Metadata.RuntimeToken, nil)
	if project, err := m.loadProject(ctx, rec.ProjectID); err == nil {
		env = m.runtimeEnv(rec.ID, rec.ProjectID, rec.IssueID, rec.Metadata.RuntimeToken, project.Config.Env)
	} else {
		m.logger.Warn("workspace cleanup: project env unavailable; agent cleanup using AO env only",
			"sessionID", rec.ID, "projectID", rec.ProjectID, "error", err)
	}
	if err := cleaner.CleanupWorkspace(ctx, ports.WorkspaceHookConfig{
		DataDir:       m.dataDir,
		Env:           env,
		SessionID:     string(rec.ID),
		WorkspacePath: workspacePath,
	}); err != nil {
		m.logger.Warn("workspace cleanup: agent cleanup failed", "sessionID", rec.ID, "workspacePath", workspacePath, "error", err)
	}
}

func (m *Manager) deliverAfterStartPrompt(ctx context.Context, agent ports.Agent, cfg ports.LaunchConfig, handle ports.RuntimeHandle, id domain.SessionID, prompt string) error {
	if err := m.waitForPromptReadiness(ctx, agent, cfg, handle); err != nil {
		return err
	}
	// Call Deliver directly (not the Guard.Send wrapper, which folds a suppressed
	// outcome into nil): a freshly-spawned session can terminate or hit a
	// permission dialog between readiness and prompt injection, and folding that
	// into success would report a spawn/restore that never delivered its prompt.
	outcome, err := m.messenger.Deliver(ctx, id, prompt)
	if err != nil {
		return fmt.Errorf("send %s: %w", id, err)
	}
	switch outcome {
	case sessionguard.SuppressedNotFound:
		return fmt.Errorf("send %s: %w", id, ErrNotFound)
	case sessionguard.SuppressedTerminated:
		return fmt.Errorf("send %s: %w", id, ErrTerminated)
	case sessionguard.SuppressedAwaitingUser:
		return fmt.Errorf("send %s: %w", id, ErrAwaitingDecision)
	case sessionguard.SuppressedUnknown:
		return fmt.Errorf("send %s: pre-write session read failed", id)
	default:
		return nil
	}
}

func (m *Manager) waitForPromptReadiness(ctx context.Context, agent ports.Agent, cfg ports.LaunchConfig, handle ports.RuntimeHandle) error {
	provider, ok := agent.(ports.AgentPromptReadinessProvider)
	if !ok {
		return nil
	}
	hints, err := provider.PromptReadinessHints(ctx, cfg)
	if err != nil {
		return fmt.Errorf("prompt readiness: %w", err)
	}
	if hints.InitialDelay > 0 {
		if err := sleepContext(ctx, hints.InitialDelay); err != nil {
			return err
		}
	}
	if len(hints.Patterns) == 0 || hints.Timeout <= 0 {
		return nil
	}
	poll := hints.PollInterval
	if poll <= 0 {
		poll = 200 * time.Millisecond
	}
	lines := hints.Lines
	if lines <= 0 {
		lines = 80
	}

	deadline := time.NewTimer(hints.Timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(poll)
	defer ticker.Stop()

	for {
		output, err := m.runtime.GetOutput(ctx, handle, lines)
		if err == nil && promptOutputContains(output, hints.Patterns) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			// Prompt readiness is best-effort: a missing terminal marker must not
			// block spawn forever or be treated as confirmed readiness. Fall back
			// to delivering the prompt and make the degraded path observable.
			m.logger.Warn("prompt readiness timed out; falling back to after-start prompt delivery",
				"sessionID", cfg.SessionID,
				"kind", string(cfg.Kind),
				"timeout", hints.Timeout.String(),
				"pollInterval", poll.String(),
				"lines", lines,
			)
			return nil
		case <-ticker.C:
		}
	}
}

func promptOutputContains(output string, patterns []string) bool {
	for _, pattern := range patterns {
		if pattern != "" && strings.Contains(output, pattern) {
			return true
		}
	}
	return false
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// restoreArgv builds the argv to relaunch a torn-down session: the agent's
// native resume command when it can continue the session, else a fresh launch
// for harnesses where replaying the saved prompt is acceptable. The agent
// signals via ok=false (e.g. no native session id captured yet). Returns
// ErrNotResumable when transcript-preserving restore is required but unavailable,
// or when a promptless, unresumable worker has nothing to restore from.
func restoreArgv(ctx context.Context, agent ports.Agent, id domain.SessionID, workspacePath string, meta domain.SessionMetadata, systemPrompt, systemPromptFile string, agentConfig ports.AgentConfig, kind domain.SessionKind, _ domain.AgentHarness, dataDir string) ([]string, ports.PromptDeliveryStrategy, RestoreMode, error) {
	ref := ports.SessionRef{
		ID:            string(id),
		WorkspacePath: workspacePath,
		Metadata:      map[string]string{ports.MetadataKeyAgentSessionID: meta.AgentSessionID},
	}
	cmd, ok, err := agent.GetRestoreCommand(ctx, ports.RestoreConfig{Session: ref, Kind: kind, DataDir: dataDir, SystemPrompt: systemPrompt, SystemPromptFile: systemPromptFile, Config: agentConfig, Permissions: agentConfig.Permissions})
	if err != nil {
		return nil, "", "", fmt.Errorf("restore command: %w", err)
	}
	if ok {
		return cmd, ports.PromptDeliveryInCommand, RestoreModeNative, nil
	}
	// A saved prompt is replayed fresh. Terminal-only sessions are promptless by
	// design and relaunch with the system prompt only. A promptless WORKER has no
	// task and no session id to restore from: do not blank-relaunch it.
	if meta.Prompt == "" && kind != domain.KindOrchestrator && kind != domain.KindPrime {
		return nil, "", "", ErrNotResumable
	}
	// Fall through to a fresh launch. Command-delivered agents receive
	// meta.Prompt in argv; after-start agents receive it via the messenger once
	// the runtime is live.
	launchCfg := ports.LaunchConfig{
		DataDir:          dataDir,
		SessionID:        string(id),
		WorkspacePath:    workspacePath,
		Kind:             kind,
		Prompt:           meta.Prompt,
		SystemPrompt:     systemPrompt,
		SystemPromptFile: systemPromptFile,
		Config:           agentConfig,
		Permissions:      agentConfig.Permissions,
	}
	delivery, err := agent.GetPromptDeliveryStrategy(ctx, launchCfg)
	if err != nil {
		return nil, "", "", fmt.Errorf("prompt delivery: %w", err)
	}
	if delivery == ports.PromptDeliveryAfterStart {
		launchCfg.Prompt = ""
	}
	argv, err := agent.GetLaunchCommand(ctx, launchCfg)
	if err != nil {
		return nil, "", "", fmt.Errorf("launch command: %w", err)
	}
	mode := RestoreModeFresh
	if meta.Prompt != "" {
		mode = RestoreModeSavedPrompt
	}
	return argv, delivery, mode, nil
}

// validateAgentBinary checks that argv[0] resolves via the manager's
// lookPath (exec.LookPath in prod) before any runtime work happens. Adapters
// that can't resolve their binary now return ports.ErrAgentBinaryNotFound from
// GetLaunchCommand directly; this guard is a defense-in-depth for adapters
// that return an argv[0] like "claude" without verifying. Some adapters prefix
// their command with `env KEY=value`; in that case validate the first real
// executable after the environment assignments.
func (m *Manager) validateAgentBinary(argv []string) error {
	if len(argv) == 0 {
		return fmt.Errorf("agent: empty launch argv: %w", ports.ErrAgentBinaryNotFound)
	}
	bin, ok := launchBinary(argv)
	if !ok {
		return fmt.Errorf("agent: launch argv missing binary: %w", ports.ErrAgentBinaryNotFound)
	}
	if _, err := m.lookPath(bin); err != nil {
		return fmt.Errorf("agent binary %q: %w", bin, ports.ErrAgentBinaryNotFound)
	}
	return nil
}

func launchBinary(argv []string) (string, bool) {
	if len(argv) == 0 {
		return "", false
	}
	if filepath.Base(argv[0]) != "env" {
		return argv[0], true
	}
	for _, arg := range argv[1:] {
		if strings.Contains(arg, "=") {
			continue
		}
		return arg, true
	}
	return "", false
}

func (m *Manager) augmentRuntimePATHForLaunchBinary(ctx context.Context, env map[string]string, argv []string) {
	bin, ok := launchBinary(argv)
	if !ok || !filepath.IsAbs(bin) {
		return
	}
	launchDir := filepath.Dir(bin)
	if launchDir == "." || launchDir == string(filepath.Separator) {
		return
	}
	dirs := []string{launchDir}
	if isNodeLaunchBinary(bin) {
		if nodeDir := m.nodeRuntimeDir(ctx); nodeDir != "" && nodeDir != launchDir {
			dirs = append(dirs, nodeDir)
		}
	}
	var parts []string
	if path := env["PATH"]; path != "" {
		parts = strings.Split(path, string(os.PathListSeparator))
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		if !containsPathDir(parts, dirs[i]) {
			parts = append([]string{dirs[i]}, parts...)
		}
	}
	env["PATH"] = strings.Join(parts, string(os.PathListSeparator))
}

func isNodeLaunchBinary(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	const maxShebangBytes = 4096
	buf := make([]byte, maxShebangBytes)
	n, _ := f.Read(buf)
	line := string(buf[:n])
	if newline := strings.IndexByte(line, '\n'); newline >= 0 {
		line = line[:newline]
	}
	if !strings.HasPrefix(line, "#!") {
		return false
	}
	for _, field := range strings.Fields(strings.TrimPrefix(line, "#!")) {
		if filepath.Base(field) == "node" {
			return true
		}
	}
	return false
}

func containsPathDir(parts []string, dir string) bool {
	for _, part := range parts {
		if part == dir {
			return true
		}
	}
	return false
}

func (m *Manager) nodeRuntimeDir(ctx context.Context) string {
	if err := ctx.Err(); err != nil || runtime.GOOS == "windows" {
		return ""
	}
	if node, err := m.lookPath("node"); err == nil && node != "" {
		return filepath.Dir(node)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	fnmDir := os.Getenv("FNM_DIR")
	if fnmDir == "" {
		if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
			fnmDir = filepath.Join(xdg, "fnm")
		} else if runtime.GOOS == "darwin" {
			fnmDir = filepath.Join(home, "Library", "Application Support", "fnm")
		} else {
			fnmDir = filepath.Join(home, ".local", "share", "fnm")
		}
	}
	voltaHome := os.Getenv("VOLTA_HOME")
	if voltaHome == "" {
		voltaHome = filepath.Join(home, ".volta")
	}
	nvm := versionedNodeMatches(filepath.Join(home, ".nvm", "versions", "node", "*", "bin", "node"))
	if data, err := os.ReadFile(filepath.Join(home, ".nvm", "alias", "default")); err == nil {
		fields := strings.Fields(string(data))
		if len(fields) > 0 {
			nvm = preferNodeVersion(nvm, fields[0])
		}
	}
	fnmMatches := versionedNodeMatches(filepath.Join(fnmDir, "node-versions", "*", "installation", "bin", "node"))
	candidates := make([]string, 0, len(nvm)+len(fnmMatches)+3)
	candidates = append(candidates, nvm...)
	candidates = append(candidates, fnmMatches...)
	// Prefer explicitly selected/versioned runtimes over manager and package-
	// manager shims. A dormant ~/.volta installation must not override the NVM
	// default or newest fnm runtime merely because the GUI omitted shell setup.
	candidates = append(candidates, filepath.Join(voltaHome, "bin", "node"), "/opt/homebrew/bin/node", "/usr/local/bin/node")
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return ""
		}
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return filepath.Dir(candidate)
		}
	}
	return ""
}

func versionedNodeMatches(pattern string) []string {
	matches, _ := filepath.Glob(pattern)
	sort.SliceStable(matches, func(i, j int) bool {
		return compareNodeVersion(nodeVersionFromPath(matches[i]), nodeVersionFromPath(matches[j])) > 0
	})
	return matches
}

func nodeVersionFromPath(path string) string {
	dir := filepath.Dir(path)
	if filepath.Base(dir) == "bin" {
		dir = filepath.Dir(dir)
	}
	if filepath.Base(dir) == "installation" {
		dir = filepath.Dir(dir)
	}
	return filepath.Base(dir)
}

func preferNodeVersion(paths []string, version string) []string {
	version = normalizeNodeVersion(version)
	for i, path := range paths {
		if normalizeNodeVersion(nodeVersionFromPath(path)) != version {
			continue
		}
		out := make([]string, 0, len(paths))
		out = append(out, path)
		out = append(out, paths[:i]...)
		out = append(out, paths[i+1:]...)
		return out
	}
	return paths
}

func compareNodeVersion(a, b string) int {
	av, aok := parseNodeVersion(a)
	bv, bok := parseNodeVersion(b)
	for i := range av {
		if av[i] != bv[i] {
			if av[i] > bv[i] {
				return 1
			}
			return -1
		}
	}
	if aok != bok {
		if aok {
			return 1
		}
		return -1
	}
	return strings.Compare(a, b)
}

func parseNodeVersion(version string) ([3]int, bool) {
	var parsed [3]int
	fields := strings.Split(normalizeNodeVersion(version), ".")
	if len(fields) == 0 || fields[0] == "" {
		return parsed, false
	}
	for i := 0; i < len(fields) && i < len(parsed); i++ {
		n, err := strconv.Atoi(fields[i])
		if err != nil {
			return [3]int{}, false
		}
		parsed[i] = n
	}
	return parsed, true
}

func normalizeNodeVersion(version string) string {
	return strings.TrimPrefix(strings.TrimSpace(version), "v")
}

func (m *Manager) validateRuntimePrerequisites() error {
	if runtime.GOOS == "windows" {
		return nil
	}
	if path, err := m.lookPath("tmux"); err != nil || path == "" {
		return fmt.Errorf("%w: tmux required on macOS/Linux but not in PATH", ports.ErrRuntimePrerequisite)
	}
	return nil
}

func runtimeHandle(meta domain.SessionMetadata) ports.RuntimeHandle {
	return ports.RuntimeHandle{ID: meta.RuntimeHandleID}
}

func workspaceInfo(rec domain.SessionRecord) ports.WorkspaceInfo {
	return ports.WorkspaceInfo{
		Path:      rec.Metadata.WorkspacePath,
		Branch:    rec.Metadata.Branch,
		SessionID: rec.ID,
		ProjectID: rec.ProjectID,
		RepoPath:  rec.Metadata.WorkspaceRepoPath,
	}
}

// workspaceInfoForTeardown is workspaceInfo with the repo path derived from the
// role identity when the row does not carry one.
//
// A projectless Prime has no project id to resolve a repo through, so if
// WorkspaceRepoPath is empty the workspace layer fails with "project id is
// required". Cleanup renders that as the generic "workspace teardown failed"
// and skips the row — leaving the stale worktree holding the canonical
// ao/prime branch, which makes every replacement spawn fail with "branch is
// already checked out in another worktree" until the restart budget is spent.
//
// The derivation already exists (singleRepoOverridePath) and is used by the
// restore paths; teardown simply was not using it. The persisted path still
// wins — this is a fallback, not an override.
func workspaceInfoForTeardown(rec domain.SessionRecord, dataDir string) ports.WorkspaceInfo {
	info := workspaceInfo(rec)
	if info.RepoPath == "" {
		info.RepoPath = singleRepoOverridePath(rec, dataDir)
	}
	return info
}

func workspaceInfoFromRepoInfo(info ports.WorkspaceRepoInfo) ports.WorkspaceInfo {
	return ports.WorkspaceInfo{
		Path:      info.Path,
		Branch:    info.Branch,
		SessionID: info.SessionID,
		ProjectID: info.ProjectID,
		RepoPath:  info.RepoPath,
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
