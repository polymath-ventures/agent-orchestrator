package controllers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/observe/metrics"
)

type fakeQuotaProber struct {
	statuses    []domain.HarnessQuotaStatus
	probeAllHit int
	probedOne   domain.AgentHarness
	known       map[domain.AgentHarness]bool
}

func (f *fakeQuotaProber) Statuses() []domain.HarnessQuotaStatus { return f.statuses }

func (f *fakeQuotaProber) ProbeAll(context.Context) []domain.HarnessQuotaStatus {
	f.probeAllHit++
	return f.statuses
}

func (f *fakeQuotaProber) ProbeHarness(_ context.Context, h domain.AgentHarness) (domain.HarnessQuotaStatus, bool) {
	f.probedOne = h
	if f.known != nil && !f.known[h] {
		return domain.HarnessQuotaStatus{}, false
	}
	return domain.HarnessQuotaStatus{Harness: h, State: domain.QuotaProbeOK}, true
}

func mountMetricsWithProber(p MetricsProvider, prober QuotaProber) http.Handler {
	r := chi.NewRouter()
	(&MetricsController{Provider: p, Prober: prober}).Register(r)
	return r
}

func TestMetricsGetIncludesProbeStatuses(t *testing.T) {
	prober := &fakeQuotaProber{statuses: []domain.HarnessQuotaStatus{
		{Harness: domain.HarnessClaudeCode, State: domain.QuotaProbeOK, HasData: true},
	}}
	h := mountMetricsWithProber(fakeMetricsProvider{hasLatest: false}, prober)

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var resp MetricsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.ProbeStatuses) != 1 || resp.ProbeStatuses[0].Harness != domain.HarnessClaudeCode {
		t.Fatalf("ProbeStatuses = %+v, want claude-code", resp.ProbeStatuses)
	}
}

func TestMetricsGetProbeStatusesEmptyWhenNoProber(t *testing.T) {
	h := mountMetricsWithProber(fakeMetricsProvider{hasLatest: false}, nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	var resp MetricsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.ProbeStatuses) != 0 {
		t.Fatalf("ProbeStatuses = %+v, want empty when no prober", resp.ProbeStatuses)
	}
}

func TestProbeAllWithEmptyBody(t *testing.T) {
	prober := &fakeQuotaProber{statuses: []domain.HarnessQuotaStatus{
		{Harness: domain.HarnessClaudeCode, State: domain.QuotaProbeOK},
	}}
	h := mountMetricsWithProber(fakeMetricsProvider{}, prober)

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/metrics/probe", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if prober.probeAllHit != 1 {
		t.Fatalf("ProbeAll hit %d times, want 1", prober.probeAllHit)
	}
	var resp ProbeQuotaResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Statuses) != 1 {
		t.Fatalf("statuses len = %d, want 1", len(resp.Statuses))
	}
}

func TestProbeSpecificHarness(t *testing.T) {
	prober := &fakeQuotaProber{known: map[domain.AgentHarness]bool{domain.HarnessCodex: true}}
	h := mountMetricsWithProber(fakeMetricsProvider{}, prober)

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/metrics/probe", strings.NewReader(`{"harness":"codex"}`)))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rr.Code, rr.Body.String())
	}
	if prober.probedOne != domain.HarnessCodex {
		t.Fatalf("probed %q, want codex", prober.probedOne)
	}
	if prober.probeAllHit != 0 {
		t.Fatalf("ProbeAll should not run for a specific harness")
	}
}

func TestProbeUnknownHarnessNotFound(t *testing.T) {
	prober := &fakeQuotaProber{known: map[domain.AgentHarness]bool{domain.HarnessCodex: true}}
	h := mountMetricsWithProber(fakeMetricsProvider{}, prober)

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/metrics/probe", strings.NewReader(`{"harness":"nope"}`)))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
}

func TestProbeNotImplementedWhenNoProber(t *testing.T) {
	h := mountMetricsWithProber(fakeMetricsProvider{}, nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/metrics/probe", nil))
	if rr.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501", rr.Code)
	}
}

func TestProbeInvalidJSON(t *testing.T) {
	prober := &fakeQuotaProber{}
	h := mountMetricsWithProber(fakeMetricsProvider{}, prober)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/metrics/probe", strings.NewReader(`{bad`)))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
}

type fakeMetricsProvider struct {
	latest    metrics.Snapshot
	hasLatest bool
	history   []metrics.Snapshot
}

func (f fakeMetricsProvider) Snapshots() ([]metrics.Snapshot, metrics.Snapshot, bool) {
	return f.history, f.latest, f.hasLatest
}

func mountMetrics(p MetricsProvider) http.Handler {
	r := chi.NewRouter()
	(&MetricsController{Provider: p}).Register(r)
	return r
}

func TestMetricsControllerReturnsSnapshot(t *testing.T) {
	snap := metrics.Snapshot{
		CollectedAt: time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC),
		Cost:        metrics.Cost{CostTotals: metrics.CostTotals{TotalTokens: 42}},
	}
	h := mountMetrics(fakeMetricsProvider{latest: snap, hasLatest: true, history: []metrics.Snapshot{snap}})

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var resp MetricsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Latest == nil || resp.Latest.Cost.TotalTokens != 42 {
		t.Errorf("latest wrong: %+v", resp.Latest)
	}
	if len(resp.History) != 1 {
		t.Errorf("history len = %d, want 1", len(resp.History))
	}
}

func TestMetricsControllerOmitsLatestBeforeFirstTick(t *testing.T) {
	h := mountMetrics(fakeMetricsProvider{hasLatest: false, history: nil})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var resp MetricsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Latest != nil {
		t.Errorf("latest should be omitted before first tick, got %+v", resp.Latest)
	}
	if resp.History == nil {
		t.Errorf("history must serialize as [] not null")
	}
}

func TestMetricsControllerNotImplementedWhenDisabled(t *testing.T) {
	h := mountMetrics(nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rr.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501", rr.Code)
	}
}
