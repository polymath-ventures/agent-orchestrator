package controllers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/controllers"
)

type fakePrimeRelauncher struct {
	calls int
	sess  domain.Session
	err   error
}

func (f *fakePrimeRelauncher) RelaunchPrime(context.Context) (domain.Session, error) {
	f.calls++
	return f.sess, f.err
}

func primeRelaunchRouter(rl controllers.PrimeRelauncher) http.Handler {
	r := chi.NewRouter()
	(&controllers.PrimeController{Relaunch: rl}).Register(r)
	return r
}

func TestPrimeControllerRelaunch(t *testing.T) {
	rl := &fakePrimeRelauncher{sess: domain.Session{SessionRecord: domain.SessionRecord{
		ID: "prime-2", Kind: domain.KindPrime, DisplayName: "Prime",
	}}}
	rr := httptest.NewRecorder()

	primeRelaunchRouter(rl).ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/prime/relaunch", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("POST /prime/relaunch = %d body=%s", rr.Code, rr.Body.String())
	}
	if rl.calls != 1 {
		t.Fatalf("relaunch calls = %d, want 1", rl.calls)
	}
	var got controllers.SessionResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Session.ID != "prime-2" {
		t.Fatalf("session = %q, want prime-2", got.Session.ID)
	}
}

func TestPrimeControllerRelaunchSurfacesFailure(t *testing.T) {
	rr := httptest.NewRecorder()
	primeRelaunchRouter(&fakePrimeRelauncher{err: errors.New("boom")}).
		ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/prime/relaunch", nil))

	if rr.Code == http.StatusOK {
		t.Fatalf("POST /prime/relaunch = %d, want a failure status", rr.Code)
	}
}

// Without a relauncher wired the route reports not-implemented rather than
// panicking, matching the other Prime routes' contract.
func TestPrimeControllerRelaunchNotImplemented(t *testing.T) {
	rr := httptest.NewRecorder()
	primeRelaunchRouter(nil).ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/prime/relaunch", nil))

	if rr.Code == http.StatusOK {
		t.Fatalf("POST /prime/relaunch = %d, want not-implemented", rr.Code)
	}
}
