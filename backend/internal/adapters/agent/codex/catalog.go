package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

const catalogTimeout = 10 * time.Second

var _ ports.AgentAvailableModels = (*Plugin)(nil)

// AvailableModels returns the model picker owned by the selected Codex-family
// harness. Plain Codex asks its offline app-server; Fugu reads the installed
// profile catalog because the wrapper does not support app-server profiles.
func (p *Plugin) AvailableModels(ctx context.Context) ([]ports.ModelCatalogEntry, error) {
	if p.adapterID() == fuguAdapterID {
		return fuguModels(ctx)
	}
	binary, err := p.agentBinary(ctx)
	if err != nil {
		return nil, err
	}
	return codexAppServerModels(ctx, binary)
}

type appServerEnvelope struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type appServerModelList struct {
	Data []struct {
		ID                       string `json:"id"`
		Model                    string `json:"model"`
		DisplayName              string `json:"displayName"`
		Hidden                   bool   `json:"hidden"`
		DefaultReasoningEffort   string `json:"defaultReasoningEffort"`
		SupportedReasoningEffort []struct {
			ReasoningEffort string `json:"reasoningEffort"`
		} `json:"supportedReasoningEfforts"`
	} `json:"data"`
}

func codexAppServerModels(ctx context.Context, binary string) ([]ports.ModelCatalogEntry, error) {
	probeCtx, cancel := context.WithTimeout(ctx, catalogTimeout)
	defer cancel()

	cmd := exec.CommandContext(probeCtx, binary, "app-server", "--listen", "stdio://")
	cmd.WaitDelay = probeWaitDelay
	configureProbeProcessGroup(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("codex app-server stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("codex app-server stdout: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start codex app-server: %w", err)
	}

	stop := func() {
		_ = stdin.Close()
		if cmd.Cancel != nil {
			_ = cmd.Cancel()
		} else if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}
	defer stop()

	encoder := json.NewEncoder(stdin)
	if err := encoder.Encode(map[string]any{
		"id": 1, "method": "initialize",
		"params": map[string]any{
			"clientInfo":   map[string]string{"name": "ao", "title": "Agent Orchestrator", "version": "0.0.1"},
			"capabilities": map[string]any{},
		},
	}); err != nil {
		return nil, fmt.Errorf("write codex initialize: %w", err)
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	if _, err := readAppServerResponse(scanner, 1); err != nil {
		return nil, fmt.Errorf("codex initialize: %w%s", err, formatProbeOutput(stderr.Bytes()))
	}
	if err := encoder.Encode(map[string]any{"method": "initialized", "params": map[string]any{}}); err != nil {
		return nil, fmt.Errorf("write codex initialized: %w", err)
	}
	if err := encoder.Encode(map[string]any{
		"id": 2, "method": "model/list", "params": map[string]any{"includeHidden": false},
	}); err != nil {
		return nil, fmt.Errorf("write codex model/list: %w", err)
	}
	raw, err := readAppServerResponse(scanner, 2)
	if err != nil {
		return nil, fmt.Errorf("codex model/list: %w%s", err, formatProbeOutput(stderr.Bytes()))
	}
	var response appServerModelList
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, fmt.Errorf("decode codex model/list: %w", err)
	}

	models := make([]ports.ModelCatalogEntry, 0, len(response.Data))
	for _, native := range response.Data {
		if native.Hidden {
			continue
		}
		id := strings.TrimSpace(native.ID)
		if id == "" {
			id = strings.TrimSpace(native.Model)
		}
		if id == "" {
			continue
		}
		label := strings.TrimSpace(native.DisplayName)
		if label == "" {
			label = id
		}
		efforts := make([]domain.Effort, 0, len(native.SupportedReasoningEffort))
		for _, option := range native.SupportedReasoningEffort {
			if effort, ok := parseEffort(option.ReasoningEffort); ok {
				efforts = appendUniqueEffort(efforts, effort)
			}
		}
		defaultEffort, hasDefault := parseEffort(native.DefaultReasoningEffort)
		if !hasDefault && len(efforts) > 0 {
			defaultEffort = efforts[0]
		}
		models = append(models, ports.ModelCatalogEntry{
			ID: id, Label: label, Efforts: efforts, DefaultEffort: defaultEffort, Dynamic: true,
		})
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("codex app-server returned no visible models")
	}
	return models, nil
}

func readAppServerResponse(scanner *bufio.Scanner, id int) (json.RawMessage, error) {
	for scanner.Scan() {
		var envelope appServerEnvelope
		if err := json.Unmarshal(scanner.Bytes(), &envelope); err != nil {
			return nil, fmt.Errorf("decode JSON-RPC envelope: %w", err)
		}
		if envelope.ID != id {
			continue
		}
		if envelope.Error != nil {
			return nil, fmt.Errorf("JSON-RPC error %d: %s", envelope.Error.Code, envelope.Error.Message)
		}
		if len(envelope.Result) == 0 {
			return nil, fmt.Errorf("JSON-RPC response %d has no result", id)
		}
		return envelope.Result, nil
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("app-server closed before response %d", id)
}

type fuguCatalog struct {
	Models []struct {
		Slug                     string `json:"slug"`
		DisplayName              string `json:"display_name"`
		DefaultReasoningLevel    string `json:"default_reasoning_level"`
		SupportedReasoningLevels []struct {
			Effort string `json:"effort"`
		} `json:"supported_reasoning_levels"`
	} `json:"models"`
}

func fuguModels(ctx context.Context) ([]ports.ModelCatalogEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	catalog, ok := readFuguCatalog()
	if !ok {
		return knownFuguModels(), nil
	}
	models := make([]ports.ModelCatalogEntry, 0, len(catalog.Models))
	for _, native := range catalog.Models {
		id := strings.TrimSpace(native.Slug)
		if id == "" {
			continue
		}
		label := strings.TrimSpace(native.DisplayName)
		if label == "" {
			label = id
		}
		efforts := make([]domain.Effort, 0, len(native.SupportedReasoningLevels))
		for _, option := range native.SupportedReasoningLevels {
			effort, ok := parseEffort(option.Effort)
			if !ok {
				continue
			}
			if strings.EqualFold(string(effort), string(domain.EffortMax)) {
				effort = domain.EffortXHigh
			}
			efforts = appendUniqueEffort(efforts, effort)
		}
		defaultEffort, ok := parseEffort(native.DefaultReasoningLevel)
		if strings.EqualFold(string(defaultEffort), string(domain.EffortMax)) {
			defaultEffort = domain.EffortXHigh
		}
		if !ok && len(efforts) > 0 {
			defaultEffort = efforts[0]
		}
		models = append(models, ports.ModelCatalogEntry{
			ID: id, Label: label, Efforts: efforts, DefaultEffort: defaultEffort, Dynamic: true,
		})
	}
	if len(models) == 0 {
		return knownFuguModels(), nil
	}
	return models, nil
}

func readFuguCatalog() (fuguCatalog, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return fuguCatalog{}, false
	}
	data, err := os.ReadFile(filepath.Join(home, ".codex", "fugu.json"))
	if err != nil {
		return fuguCatalog{}, false
	}
	var catalog fuguCatalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		return fuguCatalog{}, false
	}
	return catalog, true
}

func knownFuguModels() []ports.ModelCatalogEntry {
	efforts := []domain.Effort{domain.EffortHigh, domain.EffortXHigh}
	return []ports.ModelCatalogEntry{
		{ID: "fugu", Label: "Fugu", Efforts: append([]domain.Effort(nil), efforts...), DefaultEffort: domain.EffortHigh},
		{ID: "fugu-ultra", Label: "Fugu Ultra", Efforts: append([]domain.Effort(nil), efforts...), DefaultEffort: domain.EffortHigh},
	}
}

func parseEffort(value string) (domain.Effort, bool) {
	effort := domain.Effort(strings.TrimSpace(value))
	return effort, effort != ""
}

func appendUniqueEffort(efforts []domain.Effort, effort domain.Effort) []domain.Effort {
	for _, existing := range efforts {
		if existing == effort {
			return efforts
		}
	}
	return append(efforts, effort)
}
