package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// slackSink is a stand-in Slack incoming webhook that records every delivered
// message text and lets a test wait for a given number of deliveries.
type slackSink struct {
	srv *httptest.Server
	mu  sync.Mutex
	got []string
	ch  chan string
}

func newSlackSink(t *testing.T) *slackSink {
	t.Helper()
	sink := &slackSink{ch: make(chan string, 64)}
	sink.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Text string `json:"text"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		sink.mu.Lock()
		sink.got = append(sink.got, payload.Text)
		sink.mu.Unlock()
		sink.ch <- payload.Text
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(sink.srv.Close)
	return sink
}

func (s *slackSink) waitFor(t *testing.T, n int) []string {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for i := 0; i < n; i++ {
		select {
		case <-s.ch:
		case <-deadline:
			s.mu.Lock()
			got := append([]string(nil), s.got...)
			s.mu.Unlock()
			t.Fatalf("timed out waiting for %d Slack messages, got %d: %v", n, len(got), got)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.got...)
}

func (s *slackSink) delivered() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.got...)
}

func sseFrame(n slackNotification) string {
	data, _ := json.Marshal(n)
	return fmt.Sprintf("event: notification_created\ndata: %s\n\n", data)
}

// fakeDaemon serves the two notification routes the Slack channel consumes.
// streamFn is invoked per stream connection so a test can control framing and
// disconnection; unread is returned by the list route.
type fakeDaemon struct {
	srv     *httptest.Server
	mu      sync.Mutex
	paths   []string
	streams int
	unread  func(connection int) []slackNotification
	stream  func(t *testing.T, connection int, w http.ResponseWriter, flush func(), ctx context.Context)
}

func newFakeDaemon(t *testing.T, d *fakeDaemon) *fakeDaemon {
	t.Helper()
	d.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.mu.Lock()
		d.paths = append(d.paths, r.URL.Path)
		d.mu.Unlock()

		switch r.URL.Path {
		case "/api/v1/notifications/stream":
			d.mu.Lock()
			d.streams++
			connection := d.streams
			d.mu.Unlock()
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			flusher, ok := w.(http.Flusher)
			if !ok {
				t.Errorf("test response writer does not flush")
				return
			}
			flusher.Flush()
			d.stream(t, connection, w, flusher.Flush, r.Context())
		case "/api/v1/notifications":
			d.mu.Lock()
			connection := d.streams
			d.mu.Unlock()
			var list []slackNotification
			if d.unread != nil {
				list = d.unread(connection)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(listNotificationsAPIResponse{Notifications: list})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(d.srv.Close)
	return d
}

func (d *fakeDaemon) requestPaths() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.paths...)
}

// runLoop starts runSlackNotify in the background and returns a cancel func and
// a channel carrying its return value.
func runLoop(t *testing.T, daemon *fakeDaemon, sink *slackSink) (context.CancelFunc, <-chan error) {
	t.Helper()
	cfg := setConfigEnv(t)
	writeRunFileFor(t, cfg, daemon.srv)

	ctx := &commandContext{deps: Deps{
		HTTPClient:   &http.Client{},
		ProcessAlive: func(int) bool { return true },
		Sleep:        func(time.Duration) {},
		Now:          time.Now,
	}.withDefaults()}

	runCtx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- ctx.runSlackNotify(runCtx, slackNotifyOptions{webhookURL: sink.srv.URL})
	}()
	return cancel, errCh
}

func expectCleanShutdown(t *testing.T, cancel context.CancelFunc, errCh <-chan error) {
	t.Helper()
	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("expected a clean shutdown on cancellation, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runSlackNotify did not return after cancellation")
	}
}

// The stream must be subscribed BEFORE the unread listing is fetched. Reversing
// the order leaves a window in which a notification published between the two
// calls is delivered by neither.
func TestSlackNotifySubscribesBeforeReconciling(t *testing.T) {
	sink := newSlackSink(t)
	daemon := newFakeDaemon(t, &fakeDaemon{
		unread: func(int) []slackNotification {
			return []slackNotification{{ID: "ntf_missed", Type: "needs_input", Title: "Missed while down"}}
		},
		stream: func(_ *testing.T, _ int, _ http.ResponseWriter, _ func(), ctx context.Context) {
			<-ctx.Done()
		},
	})

	cancel, errCh := runLoop(t, daemon, sink)
	sink.waitFor(t, 1)

	paths := daemon.requestPaths()
	if len(paths) < 2 {
		t.Fatalf("expected both routes to be called, got %v", paths)
	}
	if paths[0] != "/api/v1/notifications/stream" {
		t.Fatalf("expected the stream to be subscribed first, got %v", paths)
	}
	if paths[1] != "/api/v1/notifications" {
		t.Fatalf("expected the unread listing second, got %v", paths)
	}
	expectCleanShutdown(t, cancel, errCh)
}

func TestSlackNotifyDeliversStreamAndReconciledNotificationsOnce(t *testing.T) {
	sink := newSlackSink(t)
	daemon := newFakeDaemon(t, &fakeDaemon{
		unread: func(int) []slackNotification {
			return []slackNotification{
				{ID: "ntf_1", Type: "needs_input", Title: "First"},
				{ID: "ntf_2", Type: "pr_merged", Title: "Second"},
			}
		},
		stream: func(_ *testing.T, _ int, w http.ResponseWriter, flush func(), ctx context.Context) {
			// ntf_1 also arrives live: it must not be delivered twice.
			_, _ = w.Write([]byte(sseFrame(slackNotification{ID: "ntf_1", Type: "needs_input", Title: "First"})))
			flush()
			<-ctx.Done()
		},
	})

	cancel, errCh := runLoop(t, daemon, sink)
	got := sink.waitFor(t, 2)

	if len(got) != 2 {
		t.Fatalf("expected exactly 2 deliveries, got %d: %v", len(got), got)
	}
	joined := strings.Join(got, "|")
	if !strings.Contains(joined, "First") || !strings.Contains(joined, "Second") {
		t.Fatalf("expected both notifications delivered, got %v", got)
	}
	if strings.Count(joined, "First") != 1 {
		t.Fatalf("ntf_1 delivered more than once: %v", got)
	}
	expectCleanShutdown(t, cancel, errCh)
}

func TestSlackNotifyReconnectsAndReconcilesTheGap(t *testing.T) {
	sink := newSlackSink(t)
	daemon := newFakeDaemon(t, &fakeDaemon{
		unread: func(connection int) []slackNotification {
			if connection <= 1 {
				return []slackNotification{{ID: "ntf_1", Type: "needs_input", Title: "Before drop"}}
			}
			// Published while the stream was down.
			return []slackNotification{
				{ID: "ntf_1", Type: "needs_input", Title: "Before drop"},
				{ID: "ntf_2", Type: "pr_merged", Title: "During outage"},
			}
		},
		stream: func(_ *testing.T, connection int, _ http.ResponseWriter, _ func(), ctx context.Context) {
			if connection == 1 {
				return // drop the connection immediately
			}
			<-ctx.Done()
		},
	})

	cancel, errCh := runLoop(t, daemon, sink)
	got := sink.waitFor(t, 2)

	joined := strings.Join(got, "|")
	if strings.Count(joined, "Before drop") != 1 {
		t.Fatalf("expected ntf_1 delivered exactly once across reconnect, got %v", got)
	}
	if !strings.Contains(joined, "During outage") {
		t.Fatalf("expected the gap notification delivered after reconnect, got %v", got)
	}
	expectCleanShutdown(t, cancel, errCh)
}

func TestSlackNotifySendsNothingWhenNothingWasMissed(t *testing.T) {
	sink := newSlackSink(t)
	reconnected := make(chan struct{}, 4)
	daemon := newFakeDaemon(t, &fakeDaemon{
		unread: func(int) []slackNotification {
			return []slackNotification{{ID: "ntf_1", Type: "needs_input", Title: "Only one"}}
		},
		stream: func(_ *testing.T, connection int, _ http.ResponseWriter, _ func(), ctx context.Context) {
			if connection <= 2 {
				reconnected <- struct{}{}
				return
			}
			<-ctx.Done()
		},
	})

	cancel, errCh := runLoop(t, daemon, sink)
	sink.waitFor(t, 1)
	// Wait for a reconnect so the second reconciliation has definitely run.
	<-reconnected
	<-reconnected

	if got := sink.delivered(); len(got) != 1 {
		t.Fatalf("expected no re-delivery when nothing was missed, got %v", got)
	}
	expectCleanShutdown(t, cancel, errCh)
}

func TestSlackNotifyContinuesWhenSlackRejectsAMessage(t *testing.T) {
	var attempts int
	var mu sync.Mutex
	accepted := make(chan string, 8)
	slackSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		attempts++
		n := attempts
		mu.Unlock()
		if n == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		var payload struct {
			Text string `json:"text"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		accepted <- payload.Text
		w.WriteHeader(http.StatusOK)
	}))
	defer slackSrv.Close()

	daemon := newFakeDaemon(t, &fakeDaemon{
		unread: func(int) []slackNotification { return nil },
		stream: func(_ *testing.T, _ int, w http.ResponseWriter, flush func(), ctx context.Context) {
			_, _ = w.Write([]byte(sseFrame(slackNotification{ID: "ntf_1", Type: "needs_input", Title: "Rejected"})))
			_, _ = w.Write([]byte(sseFrame(slackNotification{ID: "ntf_2", Type: "pr_merged", Title: "Accepted"})))
			flush()
			<-ctx.Done()
		},
	})

	cfg := setConfigEnv(t)
	writeRunFileFor(t, cfg, daemon.srv)
	ctx := &commandContext{deps: Deps{
		HTTPClient:   &http.Client{},
		ProcessAlive: func(int) bool { return true },
		Sleep:        func(time.Duration) {},
	}.withDefaults()}

	runCtx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- ctx.runSlackNotify(runCtx, slackNotifyOptions{webhookURL: slackSrv.URL}) }()

	select {
	case text := <-accepted:
		if !strings.Contains(text, "Accepted") {
			t.Fatalf("expected the loop to continue past the rejection, got %q", text)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("loop did not continue after a Slack rejection")
	}
	expectCleanShutdown(t, cancel, errCh)
}

func TestSlackNotifyFailsFastWhenDaemonUnreachableAtStartup(t *testing.T) {
	sink := newSlackSink(t)
	setConfigEnv(t)

	ctx := &commandContext{deps: Deps{
		HTTPClient:   &http.Client{},
		ProcessAlive: func(int) bool { return true },
		Sleep:        func(time.Duration) {},
	}.withDefaults()}

	err := ctx.runSlackNotify(context.Background(), slackNotifyOptions{webhookURL: sink.srv.URL})
	if err == nil {
		t.Fatal("expected an error when the daemon is unreachable at startup")
	}
	if ExitCode(err) != 1 {
		t.Fatalf("expected a runtime exit code 1, got %d for %v", ExitCode(err), err)
	}
}

func TestNotifySlackRequiresWebhookURL(t *testing.T) {
	setConfigEnv(t)
	t.Setenv(slackWebhookEnv, "")

	_, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "notify", "slack")
	if err == nil {
		t.Fatal("expected a usage error when no webhook URL is configured")
	}
	if ExitCode(err) != 2 {
		t.Fatalf("expected usage exit code 2, got %d for %v", ExitCode(err), err)
	}
}

func TestNotifySlackReadsWebhookFromEnv(t *testing.T) {
	setConfigEnv(t)
	t.Setenv(slackWebhookEnv, "https://hooks.slack.example/T/B/X")

	// No run file is written, so the command gets past configuration and fails
	// on the daemon instead — proving the env var satisfied the config gate.
	_, _, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "notify", "slack")
	if err == nil {
		t.Fatal("expected a daemon error")
	}
	if ExitCode(err) != 1 {
		t.Fatalf("expected runtime exit code 1, got %d for %v", ExitCode(err), err)
	}
	if !strings.Contains(err.Error(), "not running") {
		t.Fatalf("expected a daemon not-running error, got %v", err)
	}
}
