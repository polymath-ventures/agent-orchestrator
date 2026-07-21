package opencode

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

var ansiEscapePattern = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

type verboseModel struct {
	ID         string                     `json:"id"`
	ProviderID string                     `json:"providerID"`
	Name       string                     `json:"name"`
	Variants   map[string]json.RawMessage `json:"variants"`
}

// AvailableModels asks OpenCode for the provider-aware catalog it has made
// available locally. Discovery failures remain errors so the model service can
// decide whether to surface cached or maintained fallback data.
func (p *Plugin) AvailableModels(ctx context.Context) ([]ports.ModelCatalogEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	binary, err := p.opencodeBinary(ctx)
	if err != nil {
		return nil, err
	}
	out, err := exec.CommandContext(ctx, binary, "models", "--refresh", "--verbose").CombinedOutput()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		message := strings.TrimSpace(string(ansiEscapePattern.ReplaceAll(out, nil)))
		if message == "" {
			return nil, fmt.Errorf("opencode: refresh model catalog: %w", err)
		}
		return nil, fmt.Errorf("opencode: refresh model catalog: %w: %s", err, message)
	}
	models, err := parseVerboseModels(out)
	if err != nil {
		return nil, fmt.Errorf("opencode: parse model catalog: %w", err)
	}
	return models, nil
}

func parseVerboseModels(output []byte) ([]ports.ModelCatalogEntry, error) {
	remaining := ansiEscapePattern.ReplaceAll(output, nil)
	models := make([]ports.ModelCatalogEntry, 0)
	for {
		objectStart := bytes.IndexByte(remaining, '{')
		if objectStart < 0 {
			break
		}

		decoder := json.NewDecoder(bytes.NewReader(remaining[objectStart:]))
		var native verboseModel
		if err := decoder.Decode(&native); err != nil {
			return nil, fmt.Errorf("decode verbose model entry %d: %w", len(models)+1, err)
		}
		remaining = remaining[objectStart+int(decoder.InputOffset()):]

		providerID := strings.TrimSpace(native.ProviderID)
		modelID := strings.TrimSpace(native.ID)
		if providerID == "" || modelID == "" {
			return nil, fmt.Errorf("verbose model entry %d is missing providerID or id", len(models)+1)
		}
		id := providerID + "/" + modelID
		label := strings.TrimSpace(native.Name)
		if label == "" {
			label = id
		}
		models = append(models, ports.ModelCatalogEntry{
			ID:      id,
			Label:   label,
			Efforts: opencodeVariantEfforts(native.Variants),
			Dynamic: true,
		})
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("verbose output contained no model entries")
	}
	return models, nil
}

func opencodeVariantEfforts(variants map[string]json.RawMessage) []domain.Effort {
	efforts := make([]domain.Effort, 0, len(variants))
	for variant := range variants {
		effort := domain.Effort(strings.TrimSpace(variant))
		if effort == "" {
			continue
		}
		efforts = append(efforts, effort)
	}
	sort.Slice(efforts, func(i, j int) bool {
		left, right := effortOrder(efforts[i]), effortOrder(efforts[j])
		if left != right {
			return left < right
		}
		return efforts[i] < efforts[j]
	})
	return efforts
}

func effortOrder(effort domain.Effort) int {
	switch effort {
	case domain.EffortMinimal:
		return 0
	case domain.EffortLow:
		return 1
	case domain.EffortMedium:
		return 2
	case domain.EffortHigh:
		return 3
	case domain.EffortXHigh:
		return 4
	case domain.EffortMax:
		return 5
	default:
		return 6
	}
}
