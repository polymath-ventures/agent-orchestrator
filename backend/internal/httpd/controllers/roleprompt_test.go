package controllers_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/controllers"
	"github.com/aoagents/agent-orchestrator/backend/internal/roleprompt"
	sessionmanager "github.com/aoagents/agent-orchestrator/backend/internal/session_manager"
)

type fakeRolePrompt struct {
	prompt string
	err    error
	gotID  domain.ProjectID
	gotRl  string
}

func (f *fakeRolePrompt) RolePrompt(_ context.Context, id domain.ProjectID, role string) (string, error) {
	f.gotID, f.gotRl = id, role
	return f.prompt, f.err
}

func rolePromptRouter(svc controllers.RolePromptService) http.Handler {
	r := chi.NewRouter()
	(&controllers.RolePromptController{Svc: svc}).Register(r)
	return r
}

func TestRolePromptController_OK(t *testing.T) {
	svc := &fakeRolePrompt{prompt: "ASSEMBLED PROMPT"}
	rr := httptest.NewRecorder()
	rolePromptRouter(svc).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/projects/mer/roles/worker/prompt", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	var body controllers.RolePromptResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Role != "worker" || body.Prompt != "ASSEMBLED PROMPT" {
		t.Fatalf("body = %+v", body)
	}
	if svc.gotID != "mer" || svc.gotRl != "worker" {
		t.Fatalf("service got id=%q role=%q", svc.gotID, svc.gotRl)
	}
}

func TestRolePromptController_UnknownRoleIsBadRequest(t *testing.T) {
	svc := &fakeRolePrompt{err: fmt.Errorf("%w: %q", roleprompt.ErrUnknownRole, "prime")}
	rr := httptest.NewRecorder()
	rolePromptRouter(svc).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/projects/mer/roles/prime/prompt", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
}

func TestRolePromptController_NotFound(t *testing.T) {
	svc := &fakeRolePrompt{err: fmt.Errorf("%w: %q", roleprompt.ErrProjectNotFound, "nope")}
	rr := httptest.NewRecorder()
	rolePromptRouter(svc).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/projects/nope/roles/worker/prompt", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rr.Code, rr.Body.String())
	}
}

func TestRolePromptController_MisconfiguredOverrideIsUnprocessable(t *testing.T) {
	svc := &fakeRolePrompt{err: &sessionmanager.RulesLoadError{
		ProjectID: "mer", Role: "reviewer", File: "docs/x.md", Err: errors.New("file is empty"),
	}}
	rr := httptest.NewRecorder()
	rolePromptRouter(svc).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/projects/mer/roles/reviewer/prompt", nil))
	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rr.Code, rr.Body.String())
	}
}

func TestRolePromptController_InternalErrorIsSanitized500(t *testing.T) {
	// A non-config failure (store/context/internal) must not be mislabeled a 422
	// config problem, and its raw detail must not leak.
	svc := &fakeRolePrompt{err: fmt.Errorf("database connection refused: secret-host:5432")}
	rr := httptest.NewRecorder()
	rolePromptRouter(svc).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/projects/mer/roles/worker/prompt", nil))
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "secret-host") {
		t.Fatalf("internal detail leaked in response: %s", rr.Body.String())
	}
}

func TestRolePromptController_NilServiceNotImplemented(t *testing.T) {
	rr := httptest.NewRecorder()
	rolePromptRouter(nil).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/projects/mer/roles/worker/prompt", nil))
	if rr.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501; body=%s", rr.Code, rr.Body.String())
	}
}
