package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/config"
)

const (
	slackStateEnv   = "AO_SLACK_NOTIFIER_STATE"
	slackStateFile  = "slack-notifier-state.json"
	slackStateLimit = 2000
)

type slackDeliveryState struct {
	Version     int      `json:"version"`
	Initialized bool     `json:"initialized"`
	Delivered   []string `json:"delivered"`
	path        string
	seen        map[string]struct{}
	warning     string
}

func loadSlackDeliveryState() (*slackDeliveryState, error) {
	path := os.Getenv(slackStateEnv)
	if path == "" {
		cfg, err := config.Load()
		if err != nil {
			return nil, err
		}
		path = filepath.Join(cfg.DataDir, slackStateFile)
	}
	s := &slackDeliveryState{Version: 1, path: path, seen: map[string]struct{}{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read Slack delivery state: %w", err)
	}
	if err := json.Unmarshal(data, s); err != nil {
		backup := fmt.Sprintf("%s.corrupt-%d", path, time.Now().UnixNano())
		if renameErr := os.Rename(path, backup); renameErr != nil {
			return nil, errors.Join(
				fmt.Errorf("decode Slack delivery state: %w", err),
				fmt.Errorf("backup corrupt Slack delivery state: %w", renameErr),
			)
		}
		return &slackDeliveryState{
			Version: 1, path: path, seen: map[string]struct{}{},
			warning: fmt.Sprintf("corrupt Slack delivery state moved to %s; reseeding unread notifications", backup),
		}, nil
	}
	s.path = path
	s.seen = make(map[string]struct{}, len(s.Delivered))
	for _, id := range s.Delivered {
		s.seen[id] = struct{}{}
	}
	return s, nil
}

func (s *slackDeliveryState) contains(id string) bool {
	_, ok := s.seen[id]
	return ok
}

func (s *slackDeliveryState) record(ids ...string) error {
	candidate := append([]string(nil), s.Delivered...)
	seen := make(map[string]struct{}, len(s.seen)+len(ids))
	for id := range s.seen {
		seen[id] = struct{}{}
	}
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		candidate = append(candidate, id)
	}
	if len(candidate) > slackStateLimit {
		candidate = append([]string(nil), candidate[len(candidate)-slackStateLimit:]...)
		seen = make(map[string]struct{}, len(candidate))
		for _, id := range candidate {
			seen[id] = struct{}{}
		}
	}
	next := *s
	next.Delivered, next.seen = candidate, seen
	if err := next.save(); err != nil {
		return err
	}
	s.Delivered, s.seen = candidate, seen
	return nil
}

func (s *slackDeliveryState) initialize(ids []string) error {
	s.Initialized = true
	if err := s.record(ids...); err != nil {
		s.Initialized = false
		return err
	}
	return nil
}

func (s *slackDeliveryState) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o750); err != nil {
		return fmt.Errorf("create Slack state directory: %w", err)
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.tmp-%d", s.path, os.Getpid())
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write Slack delivery state: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace Slack delivery state: %w", err)
	}
	return nil
}
