package modelhealth

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	agentsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/agent"
)

type validationReply struct {
	result ports.ModelValidationResult
	err    error
}

type stubValidator struct {
	mu      sync.Mutex
	replies map[string]validationReply
	calls   []string
}

func validationKey(harness domain.AgentHarness, model string) string {
	return string(harness) + "|" + model
}

func (s *stubValidator) ValidateModel(_ context.Context, harness domain.AgentHarness, model string) (ports.ModelValidationResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := validationKey(harness, model)
	s.calls = append(s.calls, key)
	reply, ok := s.replies[key]
	if !ok {
		return ports.ModelValidationResult{Status: ports.ModelValidationProbeUnavailable, Message: "no verdict"}, nil
	}
	return reply.result, reply.err
}

func (s *stubValidator) set(harness domain.AgentHarness, model string, result ports.ModelValidationResult, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.replies[validationKey(harness, model)] = validationReply{result: result, err: err}
}

func (s *stubValidator) callKeys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...)
}

func testPin(project, scope, model string) Pin {
	return Pin{ProjectID: domain.ProjectID(project), Scope: scope, Harness: domain.HarnessCodex, Model: model}
}

func TestRunOnceEmitsTypedUnreachableAndRecoveryIntentsExactlyOnce(t *testing.T) {
	initiallyDown := testPin("ao", "worker", "down")
	laterDown := testPin("ao", "orchestrator", "later-down")
	validator := &stubValidator{replies: map[string]validationReply{
		validationKey(initiallyDown.Harness, initiallyDown.Model): {result: ports.ModelValidationResult{Status: ports.ModelValidationUnreachable, Message: "400 rejected"}},
		validationKey(laterDown.Harness, laterDown.Model):         {result: ports.ModelValidationResult{Status: ports.ModelValidationReachable}},
	}}
	var intents []Intent
	monitor := New(Deps{
		Pins:      func(context.Context) ([]Pin, error) { return []Pin{initiallyDown, laterDown}, nil },
		Validator: validator,
		OnIntent:  func(intent Intent) { intents = append(intents, intent) },
	})
	ctx := context.Background()
	if err := monitor.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if err := monitor.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(intents) != 1 || intents[0].Kind != IntentUnreachable || intents[0].Pin.Key() != initiallyDown.Key() || intents[0].Reason != "400 rejected" {
		t.Fatalf("initial intents = %+v, want one typed initial-unreachable intent", intents)
	}

	validator.set(laterDown.Harness, laterDown.Model, ports.ModelValidationResult{Status: ports.ModelValidationUnreachable, Message: "404 missing"}, nil)
	if err := monitor.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if err := monitor.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(intents) != 2 || intents[1].Kind != IntentUnreachable || intents[1].Previous != agentsvc.ModelStatusReachable || intents[1].Current != agentsvc.ModelStatusUnreachable || intents[1].Pin.Scope != "orchestrator" {
		t.Fatalf("reachable-to-unreachable intents = %+v", intents)
	}

	validator.set(initiallyDown.Harness, initiallyDown.Model, ports.ModelValidationResult{Status: ports.ModelValidationReachable}, nil)
	if err := monitor.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if err := monitor.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(intents) != 3 || intents[2].Kind != IntentRecovered || intents[2].Previous != agentsvc.ModelStatusUnreachable || intents[2].Current != agentsvc.ModelStatusReachable || intents[2].Pin.ProjectID != "ao" {
		t.Fatalf("recovery intents = %+v", intents)
	}
}

func TestRunOncePreservesDefinitiveStateAcrossUnavailableUnknownAndError(t *testing.T) {
	pin := testPin("ao", "workerMix[0]", "gpt-5.5-codex")
	validator := &stubValidator{replies: map[string]validationReply{
		validationKey(pin.Harness, pin.Model): {result: ports.ModelValidationResult{Status: ports.ModelValidationUnreachable, Message: "400 rejected"}},
	}}
	var intents []Intent
	monitor := New(Deps{
		Pins:      func(context.Context) ([]Pin, error) { return []Pin{pin}, nil },
		Validator: validator,
		OnIntent:  func(intent Intent) { intents = append(intents, intent) },
	})
	ctx := context.Background()
	if err := monitor.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	for _, reply := range []validationReply{
		{result: ports.ModelValidationResult{Status: ports.ModelValidationProbeUnavailable, Message: "rate limited"}},
		{result: ports.ModelValidationResult{Status: ports.ModelValidationStatus("unknown"), Message: "inconclusive"}},
		{err: errors.New("transport failed")},
	} {
		validator.set(pin.Harness, pin.Model, reply.result, reply.err)
		if err := monitor.RunOnce(ctx); err != nil {
			t.Fatal(err)
		}
		verdict := monitor.Snapshot().Verdicts[0]
		if verdict.Status != agentsvc.ModelStatusUnreachable || verdict.Reason != "400 rejected" {
			t.Fatalf("uncertain result overwrote definitive verdict: %+v", verdict)
		}
	}
	if len(intents) != 1 {
		t.Fatalf("uncertain checks emitted false transitions: %+v", intents)
	}

	validator.set(pin.Harness, pin.Model, ports.ModelValidationResult{Status: ports.ModelValidationReachable}, nil)
	if err := monitor.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(intents) != 2 || intents[1].Kind != IntentRecovered {
		t.Fatalf("recovery did not retain prior unreachable state: %+v", intents)
	}
}

func TestFirstUnavailableIsCachedAsUnknownWithoutIntent(t *testing.T) {
	pin := testPin("ao", "worker", "gpt-unknown")
	validator := &stubValidator{replies: map[string]validationReply{
		validationKey(pin.Harness, pin.Model): {result: ports.ModelValidationResult{Status: ports.ModelValidationProbeUnavailable, Message: "auth unavailable"}},
	}}
	var intents []Intent
	monitor := New(Deps{
		Pins:      func(context.Context) ([]Pin, error) { return []Pin{pin}, nil },
		Validator: validator,
		OnIntent:  func(intent Intent) { intents = append(intents, intent) },
	})
	if err := monitor.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	verdict := monitor.Snapshot().Verdicts[0]
	if verdict.Status != agentsvc.ModelStatusUnknown || verdict.ReasonCode != agentsvc.ModelReasonProbeUnavailable || verdict.Reason != "auth unavailable" {
		t.Fatalf("verdict = %+v, want cached advisory unknown", verdict)
	}
	if len(intents) != 0 {
		t.Fatalf("unknown emitted intent: %+v", intents)
	}
}

func TestRunOnceNormalizesPinsAndValidatesEachHarnessModelOnce(t *testing.T) {
	one := testPin("b", "worker", "same")
	two := testPin("a", "reviewer[0]", "same")
	invalid := testPin("", "worker", "blank-project")
	validator := &stubValidator{replies: map[string]validationReply{
		validationKey(one.Harness, one.Model): {result: ports.ModelValidationResult{Status: ports.ModelValidationReachable}},
	}}
	monitor := New(Deps{
		Pins: func(context.Context) ([]Pin, error) {
			return []Pin{one, two, one, invalid, {ProjectID: "a", Scope: "blank", Harness: domain.HarnessCodex, Model: "  "}}, nil
		},
		Validator: validator,
	})
	if err := monitor.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls := validator.callKeys(); len(calls) != 1 || calls[0] != validationKey(domain.HarnessCodex, "same") {
		t.Fatalf("validator calls = %#v, want one shared harness/model probe", calls)
	}
	verdicts := monitor.Snapshot().Verdicts
	if len(verdicts) != 2 || verdicts[0].Pin.ProjectID != "a" || verdicts[1].Pin.ProjectID != "b" {
		t.Fatalf("normalized snapshot = %+v, want two key-sorted pins", verdicts)
	}
}

func TestRunOnceRemovesUnconfiguredPinsAndListerErrorsRetainState(t *testing.T) {
	pin := testPin("ao", "worker", "gpt")
	pins := []Pin{pin}
	listErr := false
	validator := &stubValidator{replies: map[string]validationReply{
		validationKey(pin.Harness, pin.Model): {result: ports.ModelValidationResult{Status: ports.ModelValidationReachable}},
	}}
	monitor := New(Deps{
		Pins: func(context.Context) ([]Pin, error) {
			if listErr {
				return nil, errors.New("project store unavailable")
			}
			return append([]Pin(nil), pins...), nil
		},
		Validator: validator,
	})
	if err := monitor.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	listErr = true
	if err := monitor.RunOnce(context.Background()); err == nil {
		t.Fatal("lister error = nil")
	}
	if len(monitor.Snapshot().Verdicts) != 1 {
		t.Fatal("lister error pruned prior state")
	}
	listErr = false
	pins = nil
	if err := monitor.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(monitor.Snapshot().Verdicts) != 0 {
		t.Fatalf("removed pin remained cached: %+v", monitor.Snapshot())
	}
}

func TestRunPerformsImmediateAndIntervalChecksAndZeroDisables(t *testing.T) {
	pin := testPin("ao", "worker", "gpt")
	validator := &stubValidator{replies: map[string]validationReply{
		validationKey(pin.Harness, pin.Model): {result: ports.ModelValidationResult{Status: ports.ModelValidationReachable}},
	}}
	monitor := New(Deps{Pins: func(context.Context) ([]Pin, error) { return []Pin{pin}, nil }, Validator: validator})
	if err := monitor.Run(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if len(validator.callKeys()) != 0 {
		t.Fatal("disabled interval performed a validation")
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- monitor.Run(ctx, 5*time.Millisecond) }()
	deadline := time.Now().Add(time.Second)
	for len(validator.callKeys()) < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not stop after cancellation")
	}
	if len(validator.callKeys()) < 2 {
		t.Fatalf("calls = %#v, want immediate and interval validations", validator.callKeys())
	}
}
