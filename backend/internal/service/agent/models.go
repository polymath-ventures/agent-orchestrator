package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	agentregistry "github.com/aoagents/agent-orchestrator/backend/internal/adapters/agent/registry"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

var agentModelProbeTimeout = 45 * time.Second

const (
	modelAvailabilityCacheTTL = 5 * time.Minute
	spawnPinVerdictTTL        = 48 * time.Hour
	likelyUnconfiguredPinAge  = 25 * time.Hour
	maxPinVerdicts            = 256
	maxConcurrentModelProbes  = 4
)

// ModelStatus is the API-facing availability state. Probe-unavailable remains
// unknown here and is distinguished by ReasonCode.
type ModelStatus string

const (
	// ModelStatusReachable means the provider accepted the configured model.
	ModelStatusReachable ModelStatus = "reachable"
	// ModelStatusUnreachable means the provider definitively rejected the model.
	ModelStatusUnreachable ModelStatus = "unreachable"
	// ModelStatusUnknown means no definitive provider verdict is available.
	ModelStatusUnknown ModelStatus = "unknown"
)

// ModelReasonCode explains why a row has unknown availability.
type ModelReasonCode string

const (
	// ModelReasonNotProbed marks a catalog row that has not been validated.
	ModelReasonNotProbed ModelReasonCode = "not-probed"
	// ModelReasonProbeUnavailable marks infrastructure or provider uncertainty.
	ModelReasonProbeUnavailable ModelReasonCode = "probe-unavailable"
	// ModelReasonNoCapability marks a harness without a validation capability.
	ModelReasonNoCapability ModelReasonCode = "no-capability"
)

const noModelValidationCapabilityReason = "This harness has no model validation capability; configured pins are accepted without live validation."

// ModelCatalogSource identifies the source currently rendered by clients.
type ModelCatalogSource string

const (
	// ModelCatalogAdapter identifies a fresh native adapter catalog.
	ModelCatalogAdapter ModelCatalogSource = "adapter"
	// ModelCatalogCachedAdapter identifies the last successful adapter catalog.
	ModelCatalogCachedAdapter ModelCatalogSource = "cached-adapter"
	// ModelCatalogKnownSet identifies a maintained, unverified fallback catalog.
	ModelCatalogKnownSet ModelCatalogSource = "known-set"
	// ModelCatalogPins identifies a catalog containing only configured pins.
	ModelCatalogPins ModelCatalogSource = "configured-pins"
	// ModelCatalogNone identifies a harness with no visible model rows.
	ModelCatalogNone ModelCatalogSource = "none"
)

// ModelPin is a configured model that must remain visible even when discovery
// does not return it.
type ModelPin struct {
	Harness domain.AgentHarness `json:"harness"`
	Model   string              `json:"model"`
}

// ModelAvailabilityRequest carries configured pins and request cache policy.
type ModelAvailabilityRequest struct {
	Pins  []ModelPin `json:"pins,omitempty"`
	Force bool       `json:"-"`
}

// ModelAvailability is one harness-native catalog row and its latest verdict.
type ModelAvailability struct {
	Model         string          `json:"model"`
	Label         string          `json:"label"`
	Efforts       []domain.Effort `json:"efforts,omitempty"`
	DefaultEffort domain.Effort   `json:"defaultEffort,omitempty"`
	Dynamic       bool            `json:"dynamic,omitempty"`
	Verified      bool            `json:"verified"`
	Status        ModelStatus     `json:"status" enum:"reachable,unreachable,unknown"`
	Reason        string          `json:"reason,omitempty"`
	ReasonCode    ModelReasonCode `json:"reasonCode,omitempty" enum:"not-probed,probe-unavailable,no-capability"`
}

// HarnessModels is the availability catalog for one registered harness.
type HarnessModels struct {
	ID              string              `json:"id"`
	Label           string              `json:"label"`
	ReviewerCapable bool                `json:"reviewerCapable"`
	CatalogSource   ModelCatalogSource  `json:"catalogSource" enum:"adapter,cached-adapter,known-set,configured-pins,none"`
	CatalogReason   string              `json:"catalogReason,omitempty"`
	CatalogVerified bool                `json:"catalogVerified"`
	Models          []ModelAvailability `json:"models"`
}

// ModelAvailabilityResponse is the force-refreshable model catalog read model.
type ModelAvailabilityResponse struct {
	Harnesses []HarnessModels `json:"harnesses"`
	CheckedAt time.Time       `json:"checkedAt"`
}

type modelAvailabilityCache struct {
	key       string
	checkedAt time.Time
	response  ModelAvailabilityResponse
}

type cachedModelCatalog struct {
	entries   []ports.ModelCatalogEntry
	checkedAt time.Time
}

type pinVerdict struct {
	result    ports.ModelValidationResult
	checkedAt time.Time
}

type modelCandidate struct {
	entry    ports.ModelCatalogEntry
	verified bool
}

type modelProbeTarget struct {
	harnessIndex int
	modelIndex   int
	item         agentregistry.HarnessAgent
	model        string
}

// ModelAvailability returns adapter-first catalogs merged with configured
// pins. The five-minute request cache is keyed by the normalized pin set;
// force bypasses it while preserving the last successful adapter catalog as a
// visible fallback for later discovery failures.
func (s *Service) ModelAvailability(ctx context.Context, req ModelAvailabilityRequest) (ModelAvailabilityResponse, error) {
	if err := ctx.Err(); err != nil {
		return ModelAvailabilityResponse{}, err
	}
	key := modelAvailabilityCacheKey(req.Pins)
	if !req.Force {
		if cached, ok := s.cachedModelAvailability(key); ok {
			return cached, nil
		}
		s.modelRefreshMu.Lock()
		defer s.modelRefreshMu.Unlock()
		if cached, ok := s.cachedModelAvailability(key); ok {
			return cached, nil
		}
	}

	response, err := s.freshModelAvailability(ctx, req)
	if err != nil {
		return ModelAvailabilityResponse{}, err
	}
	if ctx.Err() == nil {
		s.storeModelAvailability(key, response)
	}
	return cloneModelAvailabilityResponse(response), nil
}

func (s *Service) freshModelAvailability(ctx context.Context, req ModelAvailabilityRequest) (ModelAvailabilityResponse, error) {
	pins := pinsByHarness(req.Pins)
	pinned := modelPinSet(req.Pins)
	harnesses := make([]HarnessModels, 0, len(s.agents))
	probes := make([]modelProbeTarget, 0, len(req.Pins))

	for _, item := range s.agents {
		if err := ctx.Err(); err != nil {
			return ModelAvailabilityResponse{}, err
		}
		candidates, source, reason, catalogVerified, err := s.modelCandidates(ctx, item, pins[item.Harness])
		if err != nil {
			return ModelAvailabilityResponse{}, err
		}
		harnessIndex := len(harnesses)
		models := make([]ModelAvailability, 0, len(candidates))
		for _, candidate := range candidates {
			row := availabilityRow(candidate)
			if _, ok := pinned[modelPinKey(item.Harness, row.Model)]; ok {
				row.Reason = ""
				row.ReasonCode = ""
				if item.Harness == domain.HarnessClaudeCode {
					result := claudeCatalogVerdict(row.Model, candidate.verified)
					if _, ok := item.Agent.(ports.AgentModelValidator); !ok {
						result = ports.ModelValidationResult{Status: ports.ModelValidationProbeUnavailable, Message: noModelValidationCapabilityReason}
					}
					s.recordPinVerdict(item.Harness, row.Model, pinVerdict{result: result})
					row = applyProbeResult(row, result)
					if _, ok := item.Agent.(ports.AgentModelValidator); !ok {
						row.ReasonCode = ModelReasonNoCapability
					}
				} else {
					probes = append(probes, modelProbeTarget{
						harnessIndex: harnessIndex,
						modelIndex:   len(models),
						item:         item,
						model:        row.Model,
					})
				}
			}
			models = append(models, row)
		}
		label := strings.TrimSpace(item.Manifest.Name)
		if label == "" {
			label = string(item.Harness)
		}
		harnesses = append(harnesses, HarnessModels{
			ID:              string(item.Harness),
			Label:           label,
			ReviewerCapable: domain.ReviewerHarness(item.Harness).IsKnown(),
			CatalogSource:   source,
			CatalogReason:   reason,
			CatalogVerified: catalogVerified,
			Models:          models,
		})
	}

	s.classifyPinnedModels(ctx, harnesses, probes)
	if err := ctx.Err(); err != nil {
		return ModelAvailabilityResponse{}, err
	}
	sort.Slice(harnesses, func(i, j int) bool { return harnesses[i].ID < harnesses[j].ID })
	return ModelAvailabilityResponse{Harnesses: harnesses, CheckedAt: time.Now()}, nil
}

func (s *Service) modelCandidates(ctx context.Context, item agentregistry.HarnessAgent, pins []string) ([]modelCandidate, ModelCatalogSource, string, bool, error) {
	var (
		entries  []ports.ModelCatalogEntry
		source   ModelCatalogSource
		reason   string
		verified bool
	)

	if catalog, ok := item.Agent.(ports.AgentModelCatalog); ok {
		discovered, err := catalog.AvailableModels(ctx)
		if err == nil {
			discovered = normalizeCatalogEntries(discovered)
			if len(discovered) == 0 {
				err = fmt.Errorf("adapter returned no usable model entries")
			}
		}
		if err == nil {
			entries = discovered
			source = ModelCatalogAdapter
			verified = true
			s.storeSuccessfulCatalog(item.Harness, discovered)
		} else {
			reason = err.Error()
			if cached, ok := s.cachedSuccessfulCatalog(item.Harness); ok {
				entries = cached
				source = ModelCatalogCachedAdapter
			} else if known := knownModelsForHarness(item.Harness); len(known) > 0 {
				entries = known
				source = ModelCatalogKnownSet
			} else if len(normalizePinModels(pins)) > 0 {
				source = ModelCatalogPins
			} else {
				return nil, "", "", false, fmt.Errorf("model catalog for %s: %w", item.Harness, err)
			}
		}
	} else if known := knownModelsForHarness(item.Harness); len(known) > 0 {
		entries = known
		source = ModelCatalogKnownSet
		reason = "adapter exposes no native model catalog; using maintained fallback"
	} else if len(normalizePinModels(pins)) > 0 {
		source = ModelCatalogPins
		reason = "adapter exposes no native model catalog; showing configured pins"
	} else {
		source = ModelCatalogNone
		reason = "adapter exposes no native model catalog"
	}

	candidates := make([]modelCandidate, 0, len(entries)+len(pins))
	for _, entry := range normalizeCatalogEntries(entries) {
		candidates = append(candidates, modelCandidate{entry: entry, verified: verified})
	}
	for _, model := range normalizePinModels(pins) {
		candidates = append(candidates, modelCandidate{
			entry: ports.ModelCatalogEntry{ID: model, Label: model},
		})
	}
	return dedupeAndSortCandidates(candidates), source, reason, verified, nil
}

func (s *Service) classifyPinnedModels(ctx context.Context, out []HarnessModels, probes []modelProbeTarget) {
	sem := make(chan struct{}, maxConcurrentModelProbes)
	var wg sync.WaitGroup
	for _, target := range probes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				out[target.harnessIndex].Models[target.modelIndex] = applyProbeResult(
					out[target.harnessIndex].Models[target.modelIndex],
					ports.ModelValidationResult{Status: ports.ModelValidationProbeUnavailable, Message: ctx.Err().Error()},
				)
				return
			}
			result := s.probeModel(ctx, target.item, target.model)
			row := applyProbeResult(out[target.harnessIndex].Models[target.modelIndex], result)
			if _, ok := target.item.Agent.(ports.AgentModelValidator); !ok {
				row.ReasonCode = ModelReasonNoCapability
			}
			out[target.harnessIndex].Models[target.modelIndex] = row
		}()
	}
	wg.Wait()
}

// ValidateModel runs one fresh, independently bounded model probe. Adapter
// execution errors and invalid results are normalized to probe-unavailable;
// only an unsupported harness is returned as a hard error.
func (s *Service) ValidateModel(ctx context.Context, harness domain.AgentHarness, model string) (ports.ModelValidationResult, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return ports.ModelValidationResult{Status: ports.ModelValidationReachable}, nil
	}
	for _, item := range s.agents {
		if item.Harness != harness {
			continue
		}
		result := s.probeModel(ctx, item, model)
		return result, nil
	}
	return ports.ModelValidationResult{}, fmt.Errorf("unsupported agent %q", harness)
}

// ValidateModelSelection validates one explicit harness/model/effort selection
// for config writes. Model reachability uses the ordinary bounded validator;
// effort support comes from the harness's catalog. Claude's catalog capability
// is maintained local metadata, so this path never issues a paid Claude prompt.
func (s *Service) ValidateModelSelection(ctx context.Context, harness domain.AgentHarness, model string, effort domain.Effort) (ports.ModelValidationResult, error) {
	if effort != "" {
		effortResult := s.validateSelectionEffort(ctx, harness, model, effort)
		if effortResult.Status != ports.ModelValidationReachable {
			return effortResult, nil
		}
	}
	return s.ValidateModel(ctx, harness, model)
}

func (s *Service) probeModel(ctx context.Context, item agentregistry.HarnessAgent, model string) ports.ModelValidationResult {
	model = strings.TrimSpace(model)
	if item.Harness == domain.HarnessClaudeCode {
		return s.validateClaudeAlias(ctx, item, model)
	}
	validator, ok := item.Agent.(ports.AgentModelValidator)
	if !ok {
		result := ports.ModelValidationResult{Status: ports.ModelValidationProbeUnavailable, Message: noModelValidationCapabilityReason}
		s.recordPinVerdict(item.Harness, model, pinVerdict{result: result})
		return result
	}
	if err := ctx.Err(); err != nil {
		return ports.ModelValidationResult{Status: ports.ModelValidationProbeUnavailable, Message: err.Error()}
	}
	probeCtx, cancel := context.WithTimeout(ctx, agentModelProbeTimeout)
	result, err := validator.ValidateModel(probeCtx, model)
	probeErr := probeCtx.Err()
	cancel()
	if probeErr != nil {
		result = ports.ModelValidationResult{Status: ports.ModelValidationProbeUnavailable, Message: probeErr.Error()}
	} else if err != nil {
		result = ports.ModelValidationResult{Status: ports.ModelValidationProbeUnavailable, Message: err.Error()}
	} else if !result.Status.Valid() {
		result = ports.ModelValidationResult{Status: ports.ModelValidationProbeUnavailable, Message: "adapter returned an invalid model validation status"}
	}
	s.recordPinVerdict(item.Harness, model, pinVerdict{result: result})
	return result
}

func (s *Service) validateClaudeAlias(ctx context.Context, item agentregistry.HarnessAgent, model string) ports.ModelValidationResult {
	result := ports.ModelValidationResult{
		Status:  ports.ModelValidationProbeUnavailable,
		Message: "model is not in the installed maintained Claude alias catalog; runtime remains the final validator",
	}
	catalog, ok := item.Agent.(ports.AgentModelCatalog)
	if !ok {
		result.Message = "Claude adapter exposes no local alias catalog; runtime remains the final validator"
	} else if entries, err := catalog.AvailableModels(ctx); err != nil {
		result.Message = "Claude alias catalog unavailable: " + err.Error()
	} else {
		for _, entry := range entries {
			if strings.TrimSpace(entry.ID) == model {
				result = claudeCatalogVerdict(model, true)
				break
			}
		}
	}
	s.recordPinVerdict(item.Harness, model, pinVerdict{result: result})
	return result
}

func claudeCatalogVerdict(model string, verified bool) ports.ModelValidationResult {
	if verified {
		return ports.ModelValidationResult{
			Status:  ports.ModelValidationReachable,
			Message: fmt.Sprintf("model %q is present in the installed maintained Claude alias catalog", model),
		}
	}
	return ports.ModelValidationResult{
		Status:  ports.ModelValidationProbeUnavailable,
		Message: "model is not verified by the installed maintained Claude alias catalog; runtime remains the final validator",
	}
}

// ValidateSpawnModel reads only the verdict cache. Missing, stale, and unknown
// entries return probe-unavailable so callers can fail open without network I/O.
func (s *Service) ValidateSpawnModel(_ context.Context, harness domain.AgentHarness, model string) (ports.ModelValidationResult, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return ports.ModelValidationResult{Status: ports.ModelValidationReachable}, nil
	}
	verdict, ok := s.cachedPinVerdict(harness, model)
	if !ok {
		return ports.ModelValidationResult{
			Status:  ports.ModelValidationProbeUnavailable,
			Message: fmt.Sprintf("no fresh cached reachability verdict for model %q on agent %q", model, harness),
		}, nil
	}
	return verdict.result, nil
}

// ValidateSpawnSelection validates a resolved launch selection exclusively
// from fresh cached model and catalog verdicts. It must remain free of adapter
// discovery and provider calls because session creation invokes it before any
// durable state is created.
func (s *Service) ValidateSpawnSelection(ctx context.Context, harness domain.AgentHarness, model string, effort domain.Effort) (ports.ModelValidationResult, error) {
	modelResult, err := s.ValidateSpawnModel(ctx, harness, model)
	if err != nil || modelResult.Status == ports.ModelValidationUnreachable || effort == "" {
		return modelResult, err
	}
	entries, ok := s.cachedFreshSuccessfulCatalog(harness)
	if !ok {
		return ports.ModelValidationResult{
			Status:  ports.ModelValidationProbeUnavailable,
			Message: fmt.Sprintf("no fresh cached effort catalog for agent %q", harness),
		}, nil
	}
	effortResult := validateCatalogEffort(harness, model, effort, entries)
	if effortResult.Status != ports.ModelValidationReachable {
		return effortResult, nil
	}
	return modelResult, nil
}

func (s *Service) validateSelectionEffort(ctx context.Context, harness domain.AgentHarness, model string, effort domain.Effort) ports.ModelValidationResult {
	for _, item := range s.agents {
		if item.Harness != harness {
			continue
		}
		catalog, ok := item.Agent.(ports.AgentModelCatalog)
		if !ok {
			return ports.ModelValidationResult{
				Status:  ports.ModelValidationProbeUnavailable,
				Message: fmt.Sprintf("agent %q exposes no model effort catalog", harness),
			}
		}
		entries, err := catalog.AvailableModels(ctx)
		if err != nil {
			return ports.ModelValidationResult{Status: ports.ModelValidationProbeUnavailable, Message: err.Error()}
		}
		entries = normalizeCatalogEntries(entries)
		if len(entries) == 0 {
			return ports.ModelValidationResult{
				Status:  ports.ModelValidationProbeUnavailable,
				Message: fmt.Sprintf("agent %q returned no model effort catalog", harness),
			}
		}
		s.storeSuccessfulCatalog(harness, entries)
		return validateCatalogEffort(harness, model, effort, entries)
	}
	return ports.ModelValidationResult{
		Status:  ports.ModelValidationProbeUnavailable,
		Message: fmt.Sprintf("unsupported agent %q", harness),
	}
}

func validateCatalogEffort(harness domain.AgentHarness, model string, effort domain.Effort, entries []ports.ModelCatalogEntry) ports.ModelValidationResult {
	model = strings.TrimSpace(model)
	if model == "" {
		return ports.ModelValidationResult{
			Status:  ports.ModelValidationProbeUnavailable,
			Message: fmt.Sprintf("cannot verify effort %q for agent %q without a resolved model", effort, harness),
		}
	}
	for _, entry := range entries {
		if strings.TrimSpace(entry.ID) != model {
			continue
		}
		if len(entry.Efforts) == 0 {
			return ports.ModelValidationResult{
				Status:  ports.ModelValidationProbeUnavailable,
				Message: fmt.Sprintf("model %q on agent %q does not declare supported efforts", model, harness),
			}
		}
		for _, supported := range entry.Efforts {
			if supported == effort {
				return ports.ModelValidationResult{Status: ports.ModelValidationReachable}
			}
		}
		supported := make([]string, 0, len(entry.Efforts))
		for _, value := range entry.Efforts {
			supported = append(supported, string(value))
		}
		return ports.ModelValidationResult{
			Status: ports.ModelValidationUnreachable,
			Message: fmt.Sprintf(
				"effort %q is not supported by model %q on agent %q (supported: %s)",
				effort, model, harness, strings.Join(supported, ", "),
			),
		}
	}
	return ports.ModelValidationResult{
		Status:  ports.ModelValidationProbeUnavailable,
		Message: fmt.Sprintf("model %q is absent from the cached effort catalog for agent %q", model, harness),
	}
}

func applyProbeResult(row ModelAvailability, result ports.ModelValidationResult) ModelAvailability {
	row.Reason = strings.TrimSpace(result.Message)
	switch result.Status {
	case ports.ModelValidationReachable:
		row.Status = ModelStatusReachable
		row.ReasonCode = ""
	case ports.ModelValidationUnreachable:
		row.Status = ModelStatusUnreachable
		row.ReasonCode = ""
	default:
		row.Status = ModelStatusUnknown
		row.ReasonCode = ModelReasonProbeUnavailable
	}
	return row
}

func availabilityRow(candidate modelCandidate) ModelAvailability {
	entry := candidate.entry
	return ModelAvailability{
		Model:         entry.ID,
		Label:         entry.Label,
		Efforts:       append([]domain.Effort(nil), entry.Efforts...),
		DefaultEffort: entry.DefaultEffort,
		Dynamic:       entry.Dynamic,
		Verified:      candidate.verified,
		Status:        ModelStatusUnknown,
		Reason:        "not probed; only configured pins are live-validated",
		ReasonCode:    ModelReasonNotProbed,
	}
}

func (s *Service) cachedModelAvailability(key string) (ModelAvailabilityResponse, bool) {
	s.modelMu.Lock()
	defer s.modelMu.Unlock()
	if s.modelCache.key != key || s.modelCache.checkedAt.IsZero() || time.Since(s.modelCache.checkedAt) >= modelAvailabilityCacheTTL {
		return ModelAvailabilityResponse{}, false
	}
	return cloneModelAvailabilityResponse(s.modelCache.response), true
}

func (s *Service) storeModelAvailability(key string, response ModelAvailabilityResponse) {
	s.modelMu.Lock()
	defer s.modelMu.Unlock()
	s.modelCache = modelAvailabilityCache{key: key, checkedAt: time.Now(), response: cloneModelAvailabilityResponse(response)}
}

func (s *Service) storeSuccessfulCatalog(harness domain.AgentHarness, entries []ports.ModelCatalogEntry) {
	s.modelMu.Lock()
	defer s.modelMu.Unlock()
	if s.catalogCache == nil {
		s.catalogCache = make(map[domain.AgentHarness]cachedModelCatalog)
	}
	s.catalogCache[harness] = cachedModelCatalog{entries: cloneCatalogEntries(entries), checkedAt: time.Now()}
}

func (s *Service) cachedSuccessfulCatalog(harness domain.AgentHarness) ([]ports.ModelCatalogEntry, bool) {
	s.modelMu.Lock()
	defer s.modelMu.Unlock()
	catalog, ok := s.catalogCache[harness]
	if !ok || len(catalog.entries) == 0 {
		return nil, false
	}
	return cloneCatalogEntries(catalog.entries), true
}

func (s *Service) cachedFreshSuccessfulCatalog(harness domain.AgentHarness) ([]ports.ModelCatalogEntry, bool) {
	s.modelMu.Lock()
	defer s.modelMu.Unlock()
	catalog, ok := s.catalogCache[harness]
	if !ok || len(catalog.entries) == 0 || time.Since(catalog.checkedAt) >= spawnPinVerdictTTL {
		return nil, false
	}
	return cloneCatalogEntries(catalog.entries), true
}

func (s *Service) recordPinVerdict(harness domain.AgentHarness, model string, verdict pinVerdict) {
	model = strings.TrimSpace(model)
	if harness == "" || model == "" {
		return
	}
	if !verdict.result.Status.Valid() {
		verdict.result.Status = ports.ModelValidationProbeUnavailable
	}
	if verdict.checkedAt.IsZero() {
		verdict.checkedAt = time.Now()
	}
	now := time.Now()
	s.modelMu.Lock()
	defer s.modelMu.Unlock()
	if s.pinVerdicts == nil {
		s.pinVerdicts = make(map[string]pinVerdict)
	}
	for key, existing := range s.pinVerdicts {
		if now.Sub(existing.checkedAt) >= spawnPinVerdictTTL {
			delete(s.pinVerdicts, key)
		}
	}
	s.pinVerdicts[modelPinKey(harness, model)] = verdict
	for len(s.pinVerdicts) > maxPinVerdicts {
		s.evictOnePinVerdictLocked(now)
	}
}

func (s *Service) cachedPinVerdict(harness domain.AgentHarness, model string) (pinVerdict, bool) {
	s.modelMu.Lock()
	defer s.modelMu.Unlock()
	verdict, ok := s.pinVerdicts[modelPinKey(harness, model)]
	if !ok || time.Since(verdict.checkedAt) >= spawnPinVerdictTTL {
		return pinVerdict{}, false
	}
	return verdict, true
}

// evictOnePinVerdictLocked preserves current definitive rejections while a
// likely-unconfigured or non-blocking entry is available. Caller holds modelMu.
func (s *Service) evictOnePinVerdictLocked(now time.Time) {
	oldestMatching := func(match func(pinVerdict) bool) (string, bool) {
		var key string
		var checkedAt time.Time
		for candidate, verdict := range s.pinVerdicts {
			if !match(verdict) {
				continue
			}
			if key == "" || verdict.checkedAt.Before(checkedAt) || (verdict.checkedAt.Equal(checkedAt) && candidate < key) {
				key = candidate
				checkedAt = verdict.checkedAt
			}
		}
		return key, key != ""
	}
	if key, ok := oldestMatching(func(v pinVerdict) bool { return now.Sub(v.checkedAt) >= likelyUnconfiguredPinAge }); ok {
		delete(s.pinVerdicts, key)
		return
	}
	if key, ok := oldestMatching(func(v pinVerdict) bool { return v.result.Status != ports.ModelValidationUnreachable }); ok {
		delete(s.pinVerdicts, key)
		return
	}
	if key, ok := oldestMatching(func(pinVerdict) bool { return true }); ok {
		delete(s.pinVerdicts, key)
	}
}

func modelAvailabilityCacheKey(pins []ModelPin) string {
	set := modelPinSet(pins)
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return strings.Join(keys, "\x01")
}

func modelPinSet(pins []ModelPin) map[string]struct{} {
	out := make(map[string]struct{}, len(pins))
	for _, pin := range pins {
		model := strings.TrimSpace(pin.Model)
		if pin.Harness != "" && model != "" {
			out[modelPinKey(pin.Harness, model)] = struct{}{}
		}
	}
	return out
}

func pinsByHarness(pins []ModelPin) map[domain.AgentHarness][]string {
	out := make(map[domain.AgentHarness][]string)
	for _, pin := range pins {
		model := strings.TrimSpace(pin.Model)
		if pin.Harness != "" && model != "" {
			out[pin.Harness] = append(out[pin.Harness], model)
		}
	}
	return out
}

func modelPinKey(harness domain.AgentHarness, model string) string {
	return string(harness) + "\x00" + strings.TrimSpace(model)
}

func normalizePinModels(pins []string) []string {
	set := make(map[string]struct{}, len(pins))
	for _, pin := range pins {
		if pin = strings.TrimSpace(pin); pin != "" {
			set[pin] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for pin := range set {
		out = append(out, pin)
	}
	sort.Strings(out)
	return out
}

func normalizeCatalogEntries(entries []ports.ModelCatalogEntry) []ports.ModelCatalogEntry {
	out := make([]ports.ModelCatalogEntry, 0, len(entries))
	for _, entry := range entries {
		entry.ID = strings.TrimSpace(entry.ID)
		if entry.ID == "" {
			continue
		}
		entry.Label = strings.TrimSpace(entry.Label)
		if entry.Label == "" {
			entry.Label = entry.ID
		}
		entry.Efforts = append([]domain.Effort(nil), entry.Efforts...)
		out = append(out, entry)
	}
	return out
}

func dedupeAndSortCandidates(candidates []modelCandidate) []modelCandidate {
	byID := make(map[string]modelCandidate, len(candidates))
	for _, candidate := range candidates {
		if _, exists := byID[candidate.entry.ID]; !exists {
			byID[candidate.entry.ID] = candidate
		}
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]modelCandidate, 0, len(ids))
	for _, id := range ids {
		out = append(out, byID[id])
	}
	return out
}

func cloneCatalogEntries(entries []ports.ModelCatalogEntry) []ports.ModelCatalogEntry {
	out := make([]ports.ModelCatalogEntry, len(entries))
	for i, entry := range entries {
		out[i] = entry
		out[i].Efforts = append([]domain.Effort(nil), entry.Efforts...)
	}
	return out
}

func cloneModelAvailabilityResponse(response ModelAvailabilityResponse) ModelAvailabilityResponse {
	out := ModelAvailabilityResponse{CheckedAt: response.CheckedAt, Harnesses: make([]HarnessModels, len(response.Harnesses))}
	for i, harness := range response.Harnesses {
		out.Harnesses[i] = harness
		out.Harnesses[i].Models = make([]ModelAvailability, len(harness.Models))
		for j, model := range harness.Models {
			out.Harnesses[i].Models[j] = model
			out.Harnesses[i].Models[j].Efforts = append([]domain.Effort(nil), model.Efforts...)
		}
	}
	return out
}

func knownModelsForHarness(harness domain.AgentHarness) []ports.ModelCatalogEntry {
	allEfforts := []domain.Effort{domain.EffortMinimal, domain.EffortLow, domain.EffortMedium, domain.EffortHigh, domain.EffortXHigh, domain.EffortMax}
	switch harness {
	case domain.HarnessClaudeCode:
		claudeEfforts := []domain.Effort{domain.EffortLow, domain.EffortMedium, domain.EffortHigh, domain.EffortXHigh, domain.EffortMax}
		return []ports.ModelCatalogEntry{
			{ID: "fable", Label: "Fable", Efforts: append([]domain.Effort(nil), claudeEfforts...), DefaultEffort: domain.EffortHigh, Dynamic: true},
			{ID: "haiku", Label: "Haiku", Dynamic: true},
			{ID: "opus", Label: "Opus", Efforts: append([]domain.Effort(nil), claudeEfforts...), DefaultEffort: domain.EffortHigh, Dynamic: true},
			{ID: "sonnet", Label: "Sonnet", Efforts: append([]domain.Effort(nil), claudeEfforts...), DefaultEffort: domain.EffortHigh, Dynamic: true},
		}
	case domain.HarnessCodex:
		return []ports.ModelCatalogEntry{
			{ID: "gpt-5-codex", Label: "GPT-5 Codex", Efforts: append([]domain.Effort(nil), allEfforts...)},
			{ID: "gpt-5.4", Label: "GPT-5.4", Efforts: append([]domain.Effort(nil), allEfforts...)},
			{ID: "gpt-5.4-codex", Label: "GPT-5.4 Codex", Efforts: append([]domain.Effort(nil), allEfforts...)},
		}
	case domain.HarnessCodexFugu:
		return []ports.ModelCatalogEntry{
			{ID: "fugu", Label: "Fugu", Efforts: []domain.Effort{domain.EffortHigh, domain.EffortXHigh}, DefaultEffort: domain.EffortHigh},
			{ID: "fugu-ultra", Label: "Fugu Ultra", Efforts: []domain.Effort{domain.EffortHigh, domain.EffortXHigh}, DefaultEffort: domain.EffortHigh},
		}
	default:
		return nil
	}
}
