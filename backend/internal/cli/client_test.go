package cli

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAPIErrorString covers how the CLI renders the daemon's error envelope,
// including the requestId it now surfaces for log correlation.
func TestAPIErrorString(t *testing.T) {
	cases := []struct {
		name string
		in   apiError
		want string
	}{
		{"message only", apiError{Message: "boom"}, "boom"},
		{"message and code", apiError{Message: "boom", Code: "X"}, "boom (X)"},
		{"with request id", apiError{Message: "boom", Code: "X", RequestID: "req-1"}, "boom (X) [request req-1]"},
		{"message and request id", apiError{Message: "boom", RequestID: "req-1"}, "boom [request req-1]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.in.String(); got != tc.want {
				t.Fatalf("String() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestOpenStreamReturnsUndecodedBody(t *testing.T) {
	cfg := setConfigEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/notifications/stream" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("event: notification_created\ndata: {\"id\":\"ntf_1\"}\n\n"))
	}))
	defer srv.Close()
	writeRunFileFor(t, cfg, srv)

	ctx := &commandContext{deps: Deps{HTTPClient: srv.Client(), ProcessAlive: func(int) bool { return true }}}
	resp, err := ctx.openStream(context.Background(), "notifications/stream")
	if err != nil {
		t.Fatalf("openStream: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(raw), "notification_created") {
		t.Fatalf("expected the raw SSE frame, got %q", raw)
	}
}

func TestOpenStreamReportsDaemonNotRunning(t *testing.T) {
	setConfigEnv(t)
	ctx := &commandContext{deps: Deps{HTTPClient: http.DefaultClient, ProcessAlive: func(int) bool { return true }}}
	_, err := ctx.openStream(context.Background(), "notifications/stream")
	if err == nil {
		t.Fatal("expected an error when no run file exists")
	}
	if !strings.Contains(err.Error(), "not running") {
		t.Fatalf("expected a not-running message, got %q", err.Error())
	}
}

func TestOpenStreamSurfacesAPIError(t *testing.T) {
	cfg := setConfigEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotImplemented)
		_, _ = w.Write([]byte(`{"message":"not implemented","code":"NOT_IMPLEMENTED"}`))
	}))
	defer srv.Close()
	writeRunFileFor(t, cfg, srv)

	ctx := &commandContext{deps: Deps{HTTPClient: srv.Client(), ProcessAlive: func(int) bool { return true }}}
	_, err := ctx.openStream(context.Background(), "notifications/stream")
	if err == nil {
		t.Fatal("expected an error for a non-2xx stream response")
	}
	if !strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("expected the API envelope message, got %q", err.Error())
	}
}
