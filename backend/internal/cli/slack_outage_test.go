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

// A successful reconnect re-arms the outage latch, while each continuous
// outage still produces exactly one alert.
func TestSlackNotifyLatchesAndRearmsDaemonUnreachableAlert(t *testing.T) {
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
			success := streamConnections == 1 || streamConnections == 5
			mu.Unlock()
			if success {
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
	var alerts []string
	for len(alerts) < 2 {
		select {
		case msg := <-sink.ch:
			if strings.Contains(msg, "daemon_unreachable") {
				alerts = append(alerts, msg)
			}
		case <-deadline:
			t.Fatal("no daemon-unreachable alert after repeated post-connect failures")
		}
	}
	for _, alert := range alerts {
		if !strings.HasPrefix(alert, "<@UOPS> ") {
			t.Fatalf("outage alert must mention the configured member, got %q", alert)
		}
	}

	// Give the loop time to keep failing; the latch must suppress repeats.
	time.Sleep(200 * time.Millisecond)
	sink.mu.Lock()
	alertCount := 0
	for _, m := range sink.got {
		if strings.Contains(m, "daemon_unreachable") {
			alertCount++
		}
	}
	sink.mu.Unlock()
	if alertCount != 2 {
		t.Fatalf("expected one alert per outage, got %d", alertCount)
	}
	expectCleanShutdown(t, cancel, errCh)
}
