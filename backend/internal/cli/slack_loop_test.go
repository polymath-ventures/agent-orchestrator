package cli

import (
	"bytes"
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
// fastReconnect shrinks the loop's pacing so tests exercise real cancellable
// waits and the periodic reconcile without spending seconds in them.
func fastReconnect(t *testing.T) {
	t.Helper()
	origDelay, origMax, origRecon := slackReconnectDelay, slackMaxReconnectDelay, slackReconcileInterval
	slackReconnectDelay = 5 * time.Millisecond
	slackMaxReconnectDelay = 20 * time.Millisecond
	slackReconcileInterval = 15 * time.Millisecond
	t.Cleanup(func() {
		slackReconnectDelay, slackMaxReconnectDelay, slackReconcileInterval = origDelay, origMax, origRecon
	})
}

// settle gives any in-flight duplicate delivery a chance to land, so an
// exactly-once assertion fails loudly instead of passing by racing ahead of the
// delivery it should have caught.
func settle() { time.Sleep(150 * time.Millisecond) }

func runLoop(t *testing.T, daemon *fakeDaemon, sink *slackSink) (context.CancelFunc, <-chan error) {
	t.Helper()
	fastReconnect(t)
	cfg := setConfigEnv(t)
	writeRunFileFor(t, cfg, daemon.srv)

	ctx := &commandContext{deps: Deps{
		HTTPClient:   &http.Client{},
		ProcessAlive: func(int) bool { return true },
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

// The current unread backlog is delivered on startup, before anything arrives
// on the stream — reconciliation runs immediately, not only on reconnect.
func TestSlackNotifyDeliversUnreadBacklogOnStartup(t *testing.T) {
	sink := newSlackSink(t)
	daemon := newFakeDaemon(t, &fakeDaemon{
		unread: func(int) []slackNotification {
			return []slackNotification{{ID: "ntf_missed", Type: "needs_input", Title: "Missed while down"}}
		},
		stream: func(_ *testing.T, _ int, _ http.ResponseWriter, _ func(), ctx context.Context) {
			<-ctx.Done() // stream stays open but silent
		},
	})

	cancel, errCh := runLoop(t, daemon, sink)
	got := sink.waitFor(t, 1)
	if !strings.Contains(strings.Join(got, "|"), "Missed while down") {
		t.Fatalf("expected the unread backlog delivered on startup, got %v", got)
	}
	// Both routes are exercised: the stream is subscribed and the listing read.
	paths := daemon.requestPaths()
	if !containsPath(paths, "/api/v1/notifications/stream") || !containsPath(paths, "/api/v1/notifications") {
		t.Fatalf("expected both the stream and the listing to be called, got %v", paths)
	}
	expectCleanShutdown(t, cancel, errCh)
}

func containsPath(paths []string, want string) bool {
	for _, p := range paths {
		if p == want {
			return true
		}
	}
	return false
}

// A notification whose live post fails while the stream STAYS connected must
// still be retried — the periodic reconcile is the safety net, not reconnect.
// This is the case the reconnect-only retry missed.
func TestSlackNotifyRetriesFailedPostWithoutReconnect(t *testing.T) {
	var mu sync.Mutex
	var attempts int
	accepted := make(chan string, 4)
	slackSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		attempts++
		first := attempts == 1
		mu.Unlock()
		if first {
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

	// The stream connects once and stays open — no reconnect will ever happen.
	daemon := newFakeDaemon(t, &fakeDaemon{
		unread: func(int) []slackNotification {
			return []slackNotification{{ID: "ntf_flaky", Type: "needs_input", Title: "Flaky once"}}
		},
		stream: func(_ *testing.T, _ int, _ http.ResponseWriter, _ func(), ctx context.Context) {
			<-ctx.Done()
		},
	})

	fastReconnect(t)
	cfg := setConfigEnv(t)
	writeRunFileFor(t, cfg, daemon.srv)
	ctx := &commandContext{deps: Deps{
		HTTPClient:   &http.Client{},
		ProcessAlive: func(int) bool { return true },
	}.withDefaults()}

	runCtx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- ctx.runSlackNotify(runCtx, slackNotifyOptions{webhookURL: slackSrv.URL}) }()

	select {
	case text := <-accepted:
		if !strings.Contains(text, "Flaky once") {
			t.Fatalf("unexpected delivery %q", text)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a notification whose first post failed was never retried on a live stream")
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
	sink.waitFor(t, 2)
	settle() // let a duplicate land if the dedupe is broken
	got := sink.delivered()

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
	sink.waitFor(t, 2)
	settle()
	got := sink.delivered()

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
	settle() // both reconciliations have now run

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
	fastReconnect(t)
	ctx := &commandContext{deps: Deps{
		HTTPClient:   &http.Client{},
		ProcessAlive: func(int) bool { return true },
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

// Regression for a review finding: an id was recorded as delivered BEFORE Slack
// acknowledged it, so a failed post silently suppressed that notification for
// the rest of the process's life. It must stay undelivered and be retried.
func TestSlackNotifyRetriesAfterAFailedPost(t *testing.T) {
	var mu sync.Mutex
	var attempts int
	accepted := make(chan string, 8)
	slackSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		attempts++
		first := attempts == 1
		mu.Unlock()
		if first {
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

	// The same single notification is offered on every reconciliation.
	daemon := newFakeDaemon(t, &fakeDaemon{
		unread: func(int) []slackNotification {
			return []slackNotification{{ID: "ntf_retry", Type: "needs_input", Title: "Must be retried"}}
		},
		stream: func(_ *testing.T, connection int, _ http.ResponseWriter, _ func(), ctx context.Context) {
			if connection <= 2 {
				return // drop, forcing another reconciliation
			}
			<-ctx.Done()
		},
	})

	fastReconnect(t)
	cfg := setConfigEnv(t)
	writeRunFileFor(t, cfg, daemon.srv)
	ctx := &commandContext{deps: Deps{
		HTTPClient:   &http.Client{},
		ProcessAlive: func(int) bool { return true },
	}.withDefaults()}

	runCtx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- ctx.runSlackNotify(runCtx, slackNotifyOptions{webhookURL: slackSrv.URL}) }()

	select {
	case text := <-accepted:
		if !strings.Contains(text, "Must be retried") {
			t.Fatalf("unexpected delivery %q", text)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a notification whose first post failed was never retried")
	}
	expectCleanShutdown(t, cancel, errCh)
}

// Regression: reconciliation must ask for the daemon's maximum unread page, not
// silently accept the smaller default, or a backlog is truncated.
func TestSlackNotifyRequestsTheMaximumUnreadPage(t *testing.T) {
	sink := newSlackSink(t)
	queries := make(chan string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/notifications/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		case "/api/v1/notifications":
			select {
			case queries <- r.URL.RawQuery:
			default:
			}
			_ = json.NewEncoder(w).Encode(listNotificationsAPIResponse{})
		}
	}))
	defer srv.Close()

	fastReconnect(t)
	cfg := setConfigEnv(t)
	writeRunFileFor(t, cfg, srv)
	ctx := &commandContext{deps: Deps{
		HTTPClient:   &http.Client{},
		ProcessAlive: func(int) bool { return true },
	}.withDefaults()}

	runCtx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- ctx.runSlackNotify(runCtx, slackNotifyOptions{webhookURL: sink.srv.URL}) }()

	select {
	case q := <-queries:
		if !strings.Contains(q, "limit=100") {
			t.Fatalf("expected the maximum unread page to be requested, got %q", q)
		}
		if !strings.Contains(q, "status=unread") {
			t.Fatalf("expected an unread filter, got %q", q)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reconciliation never queried the notifications list")
	}
	expectCleanShutdown(t, cancel, errCh)
}

// Regression: a long backoff must not delay shutdown. With the daemon gone the
// loop is in a reconnect wait; cancelling must return promptly.
func TestSlackNotifyShutsDownPromptlyDuringBackoff(t *testing.T) {
	sink := newSlackSink(t)
	daemon := newFakeDaemon(t, &fakeDaemon{
		unread: func(int) []slackNotification { return nil },
		// Always drop the connection, so the loop keeps backing off.
		stream: func(_ *testing.T, _ int, _ http.ResponseWriter, _ func(), _ context.Context) {},
	})

	// A long backoff that only a cancellable wait can escape quickly.
	origDelay, origMax := slackReconnectDelay, slackMaxReconnectDelay
	slackReconnectDelay = 30 * time.Second
	slackMaxReconnectDelay = 30 * time.Second
	t.Cleanup(func() { slackReconnectDelay, slackMaxReconnectDelay = origDelay, origMax })

	cfg := setConfigEnv(t)
	writeRunFileFor(t, cfg, daemon.srv)
	ctx := &commandContext{deps: Deps{
		HTTPClient:   &http.Client{},
		ProcessAlive: func(int) bool { return true },
	}.withDefaults()}

	runCtx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- ctx.runSlackNotify(runCtx, slackNotifyOptions{webhookURL: sink.srv.URL}) }()

	time.Sleep(100 * time.Millisecond) // let it enter the wait
	start := time.Now()
	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("expected clean shutdown, got %v", err)
		}
		if elapsed := time.Since(start); elapsed > 5*time.Second {
			t.Fatalf("shutdown took %s — the backoff wait ignored cancellation", elapsed)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("shutdown blocked on the backoff wait")
	}
}

// The parser must handle what the daemon's writeNotificationSSE emits, plus the
// SSE constructs a well-behaved server may add: comments/keep-alives, CRLF line
// endings, and multi-line data fields (joined with a newline, per the spec).
func TestSlackNotifyParsesSSEVariants(t *testing.T) {
	sink := newSlackSink(t)
	daemon := newFakeDaemon(t, &fakeDaemon{
		unread: func(int) []slackNotification { return nil },
		stream: func(_ *testing.T, _ int, w http.ResponseWriter, flush func(), ctx context.Context) {
			// Keep-alive comment, then a CRLF-framed event.
			_, _ = w.Write([]byte(": keep-alive\r\n\r\n"))
			_, _ = w.Write([]byte("event: notification_created\r\ndata: " +
				`{"id":"ntf_crlf","type":"pr_merged","title":"CRLF ok"}` + "\r\n\r\n"))
			flush()
			// A multi-line data field: the two lines join with "\n" to form
			// valid JSON. Concatenating them without the newline still parses
			// here, so the payload is split mid-token to prove the join.
			_, _ = w.Write([]byte("data: {\"id\":\"ntf_multi\",\"type\":\"needs_input\",\n"))
			_, _ = w.Write([]byte("data: \"title\":\"Multi ok\"}\n\n"))
			flush()
			<-ctx.Done()
		},
	})

	cancel, errCh := runLoop(t, daemon, sink)
	got := sink.waitFor(t, 2)

	joined := strings.Join(got, "|")
	if !strings.Contains(joined, "CRLF ok") {
		t.Fatalf("CRLF-framed event was not delivered: %v", got)
	}
	if !strings.Contains(joined, "Multi ok") {
		t.Fatalf("multi-line data field was not delivered: %v", got)
	}
	expectCleanShutdown(t, cancel, errCh)
}

// A read error on the stream (here: a line beyond the scanner cap) must be
// reported and drive a reconnect, not be silently swallowed as a clean EOF.
func TestSlackNotifyReportsStreamReadError(t *testing.T) {
	var errBuf bytes.Buffer
	var errMu sync.Mutex
	safeErr := &lockedWriter{w: &errBuf, mu: &errMu}

	connects := make(chan struct{}, 8)
	daemon := newFakeDaemon(t, &fakeDaemon{
		unread: func(int) []slackNotification { return nil },
		stream: func(_ *testing.T, connection int, w http.ResponseWriter, flush func(), ctx context.Context) {
			connects <- struct{}{}
			if connection == 1 {
				// A single data line longer than slackStreamLineCap: bufio.Scanner
				// returns ErrTooLong rather than EOF.
				_, _ = w.Write([]byte("data: "))
				_, _ = w.Write([]byte(strings.Repeat("x", slackStreamLineCap+16)))
				_, _ = w.Write([]byte("\n"))
				flush()
				return
			}
			<-ctx.Done()
		},
	})

	origCap := slackStreamLineCap
	_ = origCap // documents intent; cap is const, the oversized line exceeds it
	fastReconnect(t)
	cfg := setConfigEnv(t)
	writeRunFileFor(t, cfg, daemon.srv)
	ctx := &commandContext{deps: Deps{
		HTTPClient:   &http.Client{},
		ProcessAlive: func(int) bool { return true },
		Err:          safeErr,
	}.withDefaults()}

	runCtx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- ctx.runSlackNotify(runCtx, slackNotifyOptions{webhookURL: "http://127.0.0.1:1/unused"})
	}()

	<-connects // first connection with the oversized line
	<-connects // reconnected — proving the read error did not stall the loop

	cancel()
	select {
	case <-errCh:
	case <-time.After(5 * time.Second):
		t.Fatal("did not shut down")
	}

	errMu.Lock()
	out := errBuf.String()
	errMu.Unlock()
	if !strings.Contains(out, "stream read error") {
		t.Fatalf("expected a stream read error to be reported, got %q", out)
	}
}

// lockedWriter guards a writer so the test can read it while the command's
// goroutines write to it.
type lockedWriter struct {
	w  *bytes.Buffer
	mu *sync.Mutex
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// On reconnect, a notification published during the outage must be reconciled
// PROMPTLY — driven by the on-connect poke, not by having to wait out a full
// periodic interval. Set the periodic interval huge so only the on-connect
// reconcile can satisfy the test.
func TestSlackNotifyReconcilesOnConnectNotOnlyOnTick(t *testing.T) {
	sink := newSlackSink(t)
	daemon := newFakeDaemon(t, &fakeDaemon{
		unread: func(connection int) []slackNotification {
			if connection <= 1 {
				return nil // nothing before the drop
			}
			return []slackNotification{{ID: "ntf_gap", Type: "pr_merged", Title: "During outage"}}
		},
		stream: func(_ *testing.T, connection int, _ http.ResponseWriter, _ func(), ctx context.Context) {
			if connection == 1 {
				return // drop immediately, forcing a reconnect
			}
			<-ctx.Done()
		},
	})

	// Fast reconnect, but a periodic interval far longer than the test: if the
	// gap notification arrives, it can only be via the on-connect reconcile.
	origDelay, origMax, origRecon := slackReconnectDelay, slackMaxReconnectDelay, slackReconcileInterval
	slackReconnectDelay = 5 * time.Millisecond
	slackMaxReconnectDelay = 20 * time.Millisecond
	slackReconcileInterval = time.Hour
	t.Cleanup(func() {
		slackReconnectDelay, slackMaxReconnectDelay, slackReconcileInterval = origDelay, origMax, origRecon
	})

	cfg := setConfigEnv(t)
	writeRunFileFor(t, cfg, daemon.srv)
	ctx := &commandContext{deps: Deps{
		HTTPClient:   &http.Client{},
		ProcessAlive: func(int) bool { return true },
	}.withDefaults()}
	runCtx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- ctx.runSlackNotify(runCtx, slackNotifyOptions{webhookURL: sink.srv.URL}) }()

	got := sink.waitFor(t, 1)
	if !strings.Contains(strings.Join(got, "|"), "During outage") {
		t.Fatalf("expected the gap notification reconciled on reconnect, got %v", got)
	}
	expectCleanShutdown(t, cancel, errCh)
}
