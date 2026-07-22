package daemon

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/agentconfig"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	agentsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/agent"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/agenthealth"
	modelhealthsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/modelhealth"
)

type configuredProjectSource interface {
	ListProjects(context.Context) ([]domain.ProjectRecord, error)
}

// configuredProjectModels is the daemon-owned bridge from durable project
// configuration to the agent controller and health monitor. Model resolution
// stays in agentconfig.ConfiguredModelPins; the controller receives final pins
// and never re-derives launch precedence.
type configuredProjectModels struct {
	projects configuredProjectSource
	logger   *slog.Logger
}

func newConfiguredProjectModels(projects configuredProjectSource, logger *slog.Logger) *configuredProjectModels {
	if logger == nil {
		logger = slog.Default()
	}
	return &configuredProjectModels{projects: projects, logger: logger}
}

func (p *configuredProjectModels) ListModelPins(ctx context.Context) ([]agentsvc.ModelPin, error) {
	healthPins, err := p.ListModelHealthPins(ctx)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	pins := make([]agentsvc.ModelPin, 0, len(healthPins))
	for _, configured := range healthPins {
		key := string(configured.Harness) + "\x00" + configured.Model
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		pins = append(pins, agentsvc.ModelPin{Harness: configured.Harness, Model: configured.Model})
	}
	sort.Slice(pins, func(i, j int) bool {
		if pins[i].Harness != pins[j].Harness {
			return pins[i].Harness < pins[j].Harness
		}
		return pins[i].Model < pins[j].Model
	})
	return pins, nil
}

// ListModelHealthPins retains the project and source scope carried by
// agentconfig.ConfiguredModelPins for transition tracking and project reads.
func (p *configuredProjectModels) ListModelHealthPins(ctx context.Context) ([]modelhealthsvc.Pin, error) {
	projects, err := p.projects.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	pins := make([]modelhealthsvc.Pin, 0)
	for _, project := range projects {
		for _, configured := range agentconfig.ConfiguredModelPins(project.Config) {
			model := strings.TrimSpace(configured.Model)
			if configured.Harness == "" || model == "" {
				continue
			}
			pin := modelhealthsvc.Pin{
				ProjectID: domain.ProjectID(project.ID), Scope: configured.Scope, Harness: configured.Harness, Model: model,
			}
			key := pin.Key()
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			pins = append(pins, pin)
		}
	}
	sort.Slice(pins, func(i, j int) bool { return pins[i].Key() < pins[j].Key() })
	return pins, nil
}

// ConfiguredHarnesses returns every explicitly configured harness, including a
// model-less harness that cannot appear in ConfiguredModelPins but still needs
// installation/auth health monitoring.
func (p *configuredProjectModels) ConfiguredHarnesses(ctx context.Context) []string {
	projects, err := p.projects.ListProjects(ctx)
	if err != nil {
		p.logger.Warn("list configured agent harnesses failed", "err", err)
		return nil
	}
	set := make(map[domain.AgentHarness]struct{})
	add := func(harness domain.AgentHarness) {
		if harness != "" {
			set[harness] = struct{}{}
		}
	}
	addMap := func(models map[domain.AgentHarness]domain.HarnessModel) {
		for harness := range models {
			add(harness)
		}
	}
	for _, project := range projects {
		cfg := project.Config
		for _, pin := range agentconfig.ConfiguredModelPins(cfg) {
			add(pin.Harness)
		}
		add(cfg.Worker.Harness)
		add(cfg.Orchestrator.Harness)
		add(cfg.Prime.Harness)
		add(cfg.ResolveReviewerHarness(cfg.Worker.Harness).AgentHarness())
		for _, reviewer := range cfg.Reviewers {
			add(reviewer.Harness.AgentHarness())
		}
		for _, bucket := range cfg.WorkerMix {
			add(bucket.Harness)
		}
		addMap(cfg.AgentConfig.ModelByHarness)
		addMap(cfg.Worker.AgentConfig.ModelByHarness)
		addMap(cfg.Orchestrator.AgentConfig.ModelByHarness)
		addMap(cfg.Prime.AgentConfig.ModelByHarness)
	}
	ids := make([]string, 0, len(set))
	for harness := range set {
		ids = append(ids, string(harness))
	}
	sort.Strings(ids)
	return ids
}

type agentProbeService interface {
	Probe(ctx context.Context, agentID string) (agentsvc.ProbeResult, error)
}

type agentHealthProber struct {
	agents agentProbeService
}

func (p agentHealthProber) HarnessHealth(ctx context.Context, ids []string) ([]agenthealth.Probe, error) {
	probes := make([]agenthealth.Probe, 0, len(ids))
	for _, id := range ids {
		result, err := p.agents.Probe(ctx, id)
		if err != nil {
			return nil, err
		}
		probeID := strings.TrimSpace(result.Agent.ID)
		if probeID == "" {
			probeID = id
		}
		probes = append(probes, agenthealth.Probe{
			ID: probeID, Label: result.Agent.Label, Installed: result.Installed, AuthStatus: result.Agent.AuthStatus,
		})
	}
	return probes, nil
}

func newAgentHealthMonitor(agents agentProbeService, projects *configuredProjectModels, logger *slog.Logger) *agenthealth.Monitor {
	if logger == nil {
		logger = slog.Default()
	}
	return agenthealth.New(agenthealth.Deps{
		Prober:    agentHealthProber{agents: agents},
		Harnesses: projects.ConfiguredHarnesses,
		Logger:    logger,
		OnTransition: func(transition agenthealth.Transition) {
			current := transition.Current
			if current.Health.Actionable() {
				logger.Warn("configured agent health transition", "harness", current.ID, "health", current.Health, "prev", transition.Prev)
				return
			}
			logger.Info("configured agent health transition", "harness", current.ID, "health", current.Health, "prev", transition.Prev)
		},
	})
}

type agentHealthRunner interface {
	Run(context.Context, time.Duration) error
}

func startAgentHealthMonitor(ctx context.Context, monitor agentHealthRunner, interval time.Duration, logger *slog.Logger) <-chan struct{} {
	if logger == nil {
		logger = slog.Default()
	}
	if interval <= 0 {
		logger.Info("agent health monitor disabled (AO_AGENT_HEALTH_INTERVAL=0)")
		return closedDone()
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := monitor.Run(ctx, interval); err != nil && ctx.Err() == nil {
			logger.Error("agent health monitor stopped", "err", err)
		}
	}()
	return done
}
