package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// After at least one successful connect, three consecutive reconnect failures
// must produce exactly one daemon-unreachable alert, mentioning the configured
// member, and the alert must not repeat while the outage persists.
func TestSlackNotifyLatchesDaemonUnreachableAlert(t *testing.T) {
	fastReconnect(t)
	cfg := setConfigEnv(t)
	t.Setenv(slackMemberEnv, "UOPS")

	sink := newSlackSink(t)

	var mu sync.Mutex
	streamConnections := 0
	daemonSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/notifications/stream":
			mu.Lock()
			streamConnections++
			first := streamConnections == 1
			mu.Unlock()
			if first {
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				w.(http.Flusher).Flush() // connect, then end the stream to force reconnects
				return
			}
			w.WriteHeader(http.StatusInternalServerError)
		case "/api/v1/notifications":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(listNotificationsAPIResponse{})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer daemonSrv.Close()
	writeRunFileFor(t, cfg, daemonSrv)

	ctx := &commandContext{deps: Deps{HTTPClient: &http.Client{}, ProcessAlive: func(int) bool { return true }}.withDefaults()}
	runCtx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- ctx.runSlackNotify(runCtx, slackNotifyOptions{webhookURL: sink.srv.URL}) }()

	deadline := time.After(5 * time.Second)
	var alert string
	for alert == "" {
		select {
		case msg := <-sink.ch:
			if strings.Contains(msg, "daemon_unreachable") {
				alert = msg
			}
		case <-deadline:
			t.Fatal("no daemon-unreachable alert after repeated post-connect failures")
		}
	}
	if !strings.HasPrefix(alert, "<@UOPS> ") {
		t.Fatalf("outage alert must mention the configured member, got %q", alert)
	}

	// Give the loop time to keep failing; the latch must suppress repeats.
	time.Sleep(200 * time.Millisecond)
	sink.mu.Lock()
	alerts := 0
	for _, m := range sink.got {
		if strings.Contains(m, "daemon_unreachable") {
			alerts++
		}
	}
	sink.mu.Unlock()
	if alerts != 1 {
		t.Fatalf("expected exactly one latched outage alert, got %d", alerts)
	}
	expectCleanShutdown(t, cancel, errCh)
}
