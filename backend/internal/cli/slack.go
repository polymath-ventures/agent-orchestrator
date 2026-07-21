package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

const (
	// slackWebhookEnv is the documented, preferred way to supply the webhook URL.
	// It is preferred over the flag because the URL is a secret and this command
	// is long-lived, so a flag value would sit in the process table. The AO_
	// prefix matches the repo's env convention.
	slackWebhookEnv = "AO_SLACK_WEBHOOK_URL"

	// slackWebhookEnvFallback is the un-prefixed name Slack tooling conventionally
	// uses. It is accepted as a fallback so an operator's existing SLACK_WEBHOOK_URL
	// works without duplicating it under the AO_ prefix.
	slackWebhookEnvFallback = "SLACK_WEBHOOK_URL"
	slackMemberEnv          = "AO_SLACK_MEMBER_ID"
	slackMemberEnvFallback  = "SLACK_MEMBER_ID"

	// slackUnreadLimit is the page size reconciliation asks for. The daemon caps
	// unread listings at 100 and defaults to 50, so asking explicitly is the
	// difference between reading 50 and 100 notifications per cursor page.
	// Reconciliation keeps requesting pages until the full unread backlog is
	// exhausted.
	slackUnreadLimit = 100

	// slackStreamLineCap bounds a single SSE line. Notification bodies are short
	// enough that the default scanner buffer would do; this leaves headroom
	// without letting a malformed stream consume unbounded memory.
	slackStreamLineCap = 1 << 20

	// slackQueueBuffer is the delivery worker's inbound queue depth. It absorbs a
	// burst while Slack catches up; once full, producers apply backpressure and
	// any daemon-side drop is recovered by the next periodic reconcile.
	slackQueueBuffer = 256
)

// Reconnect pacing. Vars rather than consts so tests can shrink them; the loop
// waits on these through waitOrDone, which stays responsive to cancellation.
var (
	// slackReconnectDelay is the pause before re-subscribing after the stream
	// ends, and the base for backoff when the daemon is temporarily unreachable.
	slackReconnectDelay = 2 * time.Second
	// slackMaxReconnectDelay caps the backoff so a daemon restart is picked up
	// promptly rather than after an ever-growing wait.
	slackMaxReconnectDelay = 30 * time.Second
	// slackReconcileInterval is how often the reconcile loop re-lists unread
	// notifications. It bounds the delay before a stream-missed or
	// transiently-failed notification is retried, independent of stream health.
	slackReconcileInterval = 30 * time.Second
)

// waitOrDone sleeps for d, returning early if ctx is cancelled, so a long
// backoff never delays shutdown. Callers re-test ctx via the loop condition.
func waitOrDone(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

// slackPoster delivers one rendered line to a Slack incoming webhook. Slack
// incoming webhooks are a plain JSON POST, so this needs no SDK.
type slackPoster struct {
	client     *http.Client
	webhookURL string
}

func (p slackPoster) post(ctx context.Context, text string) error {
	payload, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.webhookURL, bytes.NewReader(payload)) // #nosec G704 -- webhook URL is operator-supplied configuration.
	if err != nil {
		// A malformed webhook URL fails here, and the parse error embeds the
		// full URL — the credential. Scrub it, same as the transport error below.
		return fmt.Errorf("build Slack request: %s", scrubWebhookURL(err, p.webhookURL))
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req) // #nosec G704 -- request target is the operator-configured webhook above.
	if err != nil {
		// Never wrap the transport error directly: Go's *url.Error embeds the
		// full request URL, and the webhook URL IS the credential. This error is
		// printed to stderr on every delivery failure.
		return fmt.Errorf("post to Slack: %s", scrubWebhookURL(err, p.webhookURL))
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("slack webhook returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// scrubWebhookURL renders a delivery error without leaking the webhook secret.
// A *url.Error carries the full URL, so its embedded error is reported alone;
// anything else is redacted defensively by substring.
func scrubWebhookURL(err error, webhookURL string) string {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		err = urlErr.Err
	}
	msg := err.Error()
	if webhookURL != "" {
		msg = strings.ReplaceAll(msg, webhookURL, "[redacted webhook URL]")
	}
	return msg
}

// slackNotification mirrors the daemon's NotificationResponse fields the Slack
// channel renders. The CLI keeps its own copy so it need not import httpd.
type slackNotification struct {
	ID        string    `json:"id"`
	SessionID string    `json:"sessionId"`
	ProjectID string    `json:"projectId"`
	PRURL     string    `json:"prUrl"`
	Type      string    `json:"type"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
}

// listNotificationsAPIResponse mirrors the daemon's ListNotificationsResponse
// body for GET /api/v1/notifications.
type listNotificationsAPIResponse struct {
	Notifications []slackNotification `json:"notifications"`
}

// notificationIcon decorates a notification by type. An unrecognized type — a
// notification kind added to the daemon after this command was written — falls
// back to a generic icon rather than being dropped, so the payload's own title
// and body still reach Slack.
func notificationIcon(notificationType string) string {
	switch notificationType {
	case "needs_input":
		return ":speech_balloon:"
	case "ready_to_merge":
		return ":white_check_mark:"
	case "pr_merged":
		return ":tada:"
	case "pr_closed_unmerged":
		return ":wastebasket:"
	case "low_quota":
		return ":warning:"
	case "model_unreachable":
		return ":brain:"
	case "model_recovered":
		return ":green_heart:"
	case "prime_restart_capped":
		return ":rotating_light:"
	default:
		return ":bell:"
	}
}

// escapeSlackText escapes the three characters Slack treats as markup control
// characters in message text. Only these three, and in this order, per Slack's
// message-formatting rules.
func escapeSlackText(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

// renderNotification turns one notification into a single-line Slack message.
// It is pure: the whole presentation of this feature lives here, so it can be
// exercised without a daemon or a webhook.
func renderNotification(n slackNotification) string {
	return renderNotificationWithMember(n, "")
}

func renderNotificationWithMember(n slackNotification, memberID string) string {
	title := strings.TrimSpace(n.Title)
	if title == "" {
		title = n.Type
	}
	parts := []string{notificationIcon(n.Type), "*" + escapeSlackText(title) + "*"}
	if memberID != "" && isSlackAttentionType(n.Type) {
		parts = append([]string{"<@" + escapeSlackText(memberID) + ">"}, parts...)
	}
	if body := strings.TrimSpace(n.Body); body != "" {
		parts = append(parts, "— "+escapeSlackText(body))
	}
	if prURL := strings.TrimSpace(n.PRURL); prURL != "" {
		// Slack link syntax is <url|label>, so a URL containing the delimiters
		// could close the link early and inject markup. Render such a URL as
		// escaped plain text instead of trusting it in link position.
		if strings.ContainsAny(prURL, "<>|") {
			parts = append(parts, escapeSlackText(prURL))
		} else {
			parts = append(parts, "<"+prURL+"|View PR>")
		}
	}
	// Collapse newlines so one notification is always one Slack line.
	return strings.Join(strings.Fields(strings.Join(parts, " ")), " ")
}

func isSlackAttentionType(notificationType string) bool {
	switch notificationType {
	case "needs_input", "prime_restart_capped", "model_unreachable":
		return true
	default:
		return false
	}
}

// syncLogger serializes stderr writes. Multiple goroutines (the delivery
// worker, the stream reader, the reconcile loop) report diagnostics, and
// io.Writer makes no concurrency promise — an injected buffer would otherwise
// race. os.Stderr happens to be safe, but the dependency contract is broader.
type syncLogger struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *syncLogger) printf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = fmt.Fprintf(l.w, "ao notify slack: "+format+"\n", args...)
}

// slackDeliverer is a single-consumer delivery queue. Producers (the stream
// reader and the reconcile loop) enqueue notifications; one worker posts them
// serially and owns the delivered-ID ledger. Because the worker is the sole
// reader of that ledger, dedupe needs no lock, and — crucially — no lock is
// held across the network POST, so a slow webhook can never block a producer:
// the stream reader keeps draining the daemon's subscriber buffer while the
// worker catches up.
//
// The ledger is unbounded on purpose. An earlier version evicted the oldest
// entries, reasoning that a notification too old to appear in an unread listing
// could never be re-offered — but this command never marks anything read, so a
// delivered notification stays unread and can re-enter the listing once newer
// ones are read in the UI. Eviction therefore reintroduces duplicates. Entries
// are a notification id apiece, so the ledger stays small in any realistic run.
type slackDeliverer struct {
	poster   slackPoster
	log      func(format string, args ...any)
	queue    chan slackNotification
	state    *slackDeliveryState
	memberID string
}

func newSlackDeliverer(poster slackPoster, state *slackDeliveryState, memberID string, log func(string, ...any)) *slackDeliverer {
	return &slackDeliverer{poster: poster, state: state, memberID: memberID, log: log, queue: make(chan slackNotification, slackQueueBuffer)}
}

// enqueue offers a notification for delivery. It drops into the buffered queue,
// or returns immediately if ctx is cancelled — it never blocks shutdown. If the
// queue is full (Slack far behind), the producer waits, applying backpressure;
// any resulting daemon-side drop is recovered by the next periodic reconcile.
func (d *slackDeliverer) enqueue(ctx context.Context, n slackNotification) {
	if strings.TrimSpace(n.ID) == "" {
		return
	}
	select {
	case d.queue <- n:
	case <-ctx.Done():
	}
}

// run is the single delivery worker. An ID is recorded as delivered ONLY after
// Slack accepts it, so a failed post is left unrecorded and re-offered by the
// next reconcile pass — a Slack hiccup repeats a line at worst, never drops one.
func (d *slackDeliverer) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case n := <-d.queue:
			if d.state.contains(n.ID) {
				continue
			}
			if err := d.poster.post(ctx, renderNotificationWithMember(n, d.memberID)); err != nil {
				if ctx.Err() == nil {
					d.log("delivery failed for %s (will retry): %v", n.ID, err)
				}
				continue
			}
			if err := d.state.record(n.ID); err != nil {
				d.log("could not persist delivery for %s (will retry): %v", n.ID, err)
			}
		}
	}
}

type slackNotifyOptions struct {
	webhookURL string
}

func newNotifyCommand(ctx *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "notify",
		Short: "Mirror AO notifications to an external channel",
	}
	cmd.AddCommand(newNotifySlackCommand(ctx))
	return cmd
}

func newNotifySlackCommand(ctx *commandContext) *cobra.Command {
	var opts slackNotifyOptions
	cmd := &cobra.Command{
		Use:   "slack",
		Short: "Stream AO notifications to a Slack incoming webhook (one-way)",
		Long: "Subscribe to the running daemon's notification stream and post every notification " +
			"to a Slack incoming webhook. Runs until interrupted. Delivery is one-way: nothing is " +
			"read from Slack.\n\nThe webhook URL is resolved in order: --webhook-url, then " +
			slackWebhookEnv + ", then " + slackWebhookEnvFallback + ". Prefer an environment variable " +
			"over the flag, since a flag value is visible in the process table; " + slackWebhookEnv +
			" is the documented name and " + slackWebhookEnvFallback + " is accepted as a fallback.",
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// No CLI command is cancellable by default (the root runs with a
			// background context), so this long-lived one wraps its own rather
			// than changing the shared execution path for every other command.
			runCtx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return ctx.runSlackNotify(runCtx, opts)
		},
	}
	cmd.Flags().StringVar(&opts.webhookURL, "webhook-url", "",
		"Slack incoming webhook URL (prefer "+slackWebhookEnv+" or "+slackWebhookEnvFallback+"; a flag value is visible in the process table)")
	return cmd
}

// runSlackNotify wires up three concurrent parts and runs until cancelled:
//   - one delivery worker that posts to Slack and owns dedupe;
//   - a stream reader that enqueues live notifications, reconnecting on drop;
//   - a periodic reconcile loop that lists unread notifications and enqueues
//     any the worker has not delivered.
//
// Reconciliation is periodic, not just on connect, and that is what makes the
// design robust: the daemon's notification hub is in-process and best-effort
// with no replay, so a dropped stream event or a transiently-failed Slack post
// would otherwise be lost. The periodic pass re-offers anything still unread,
// so every missed notification is delivered within one interval. Dedupe by ID
// keeps that at-most-once.
func (c *commandContext) runSlackNotify(ctx context.Context, opts slackNotifyOptions) error {
	// Precedence: --webhook-url flag, then AO_SLACK_WEBHOOK_URL (preferred), then
	// the conventional un-prefixed SLACK_WEBHOOK_URL.
	webhookURL := strings.TrimSpace(opts.webhookURL)
	if webhookURL == "" {
		webhookURL = strings.TrimSpace(os.Getenv(slackWebhookEnv))
	}
	memberID := strings.TrimSpace(os.Getenv(slackMemberEnv))
	if memberID == "" {
		memberID = strings.TrimSpace(os.Getenv(slackMemberEnvFallback))
	}
	if webhookURL == "" {
		webhookURL = strings.TrimSpace(os.Getenv(slackWebhookEnvFallback))
	}
	if webhookURL == "" {
		return usageError{errors.New("usage: --webhook-url is required (or set " + slackWebhookEnv + " / " + slackWebhookEnvFallback + ")")}
	}

	client := *c.deps.HTTPClient
	client.Timeout = commandTimeout
	logger := &syncLogger{w: c.deps.Err}
	state, err := loadSlackDeliveryState()
	if err != nil {
		return err
	}
	if state.warning != "" {
		logger.printf("%s", state.warning)
	}
	if !state.Initialized {
		unread, err := c.listUnreadSlack(ctx)
		if err != nil {
			return fmt.Errorf("seed Slack delivery state: %w", err)
		}
		ids := slackSeedIDs(unread)
		if err := state.initialize(ids); err != nil {
			return err
		}
	}
	deliverer := newSlackDeliverer(slackPoster{client: &client, webhookURL: webhookURL}, state, memberID, logger.printf)

	// A local context so a fatal stream error (never-reachable daemon) tears
	// down the worker and reconcile loop too.
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// reconcileNow lets the stream loop trigger an immediate reconcile on every
	// successful (re)connect, on top of the periodic tick. Buffered so a poke is
	// never lost and never blocks the poker; coalesced if one is already pending.
	reconcileNow := make(chan struct{}, 1)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		deliverer.run(runCtx)
	}()
	go func() {
		defer wg.Done()
		c.reconcileLoop(runCtx, deliverer, logger, reconcileNow)
	}()

	// The stream loop runs in the foreground and returns an error only when the
	// FIRST connection fails — a daemon that was never reachable is an operator
	// error worth exiting on (exit 1). Once connected, drops are transient.
	err = c.streamLoop(runCtx, deliverer, logger, reconcileNow)
	cancel()
	wg.Wait()
	return err
}

// slackSeedIDs turns the daemon's newest-first unread listing into oldest-first
// insertion order, matching steady-state delivery order in the persisted file.
func slackSeedIDs(unread []slackNotification) []string {
	ids := make([]string, 0, len(unread))
	for i := len(unread) - 1; i >= 0; i-- {
		ids = append(ids, unread[i].ID)
	}
	return ids
}

// pokeReconcile requests a reconcile without blocking. If one is already queued,
// the poke coalesces into it — one extra reconcile is enough.
func pokeReconcile(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

// streamLoop consumes the notification stream, reconnecting with backoff on
// drop. It returns an error only if the very first connection fails; after that
// it runs until ctx is cancelled. Each successful (re)connect pokes a reconcile
// so anything the hub dropped while disconnected is recovered promptly, not
// only at the next periodic tick — the on-connect reconciliation the spec
// requires.
func (c *commandContext) streamLoop(ctx context.Context, deliverer *slackDeliverer, logger *syncLogger, reconcileNow chan struct{}) error {
	connected := false
	backoff := slackReconnectDelay
	consecutiveErrors := 0
	outageAlerted := false
	for ctx.Err() == nil {
		resp, err := c.openNotificationStream(ctx)
		if err != nil {
			if !connected && ctx.Err() == nil {
				return err
			}
			consecutiveErrors++
			if consecutiveErrors >= 3 && !outageAlerted && ctx.Err() == nil {
				prefix := ""
				if deliverer.memberID != "" {
					prefix = "<@" + escapeSlackText(deliverer.memberID) + "> "
				}
				text := prefix + ":adhesive_bandage: *daemon_unreachable* Slack notifier cannot reach AO notifications — alerts may be delayed until catch-up succeeds."
				if postErr := deliverer.poster.post(ctx, text); postErr != nil {
					logger.printf("daemon-unreachable alert failed: %v", postErr)
				} else {
					outageAlerted = true
				}
			}
			logger.printf("reconnecting after stream error: %v", err)
			waitOrDone(ctx, backoff)
			if backoff *= 2; backoff > slackMaxReconnectDelay {
				backoff = slackMaxReconnectDelay
			}
			continue
		}
		connected = true
		consecutiveErrors = 0
		outageAlerted = false
		backoff = slackReconnectDelay
		pokeReconcile(reconcileNow)

		c.consumeSlackStream(ctx, resp, deliverer, logger)
		_ = resp.Body.Close()
		waitOrDone(ctx, slackReconnectDelay)
	}
	return nil
}

// reconcileLoop delivers the current unread backlog immediately, then re-checks
// on every reconcile poke (a fresh stream connection) and on a fixed interval.
// It is the safety net for anything the stream missed: a hub-dropped event, or
// a notification whose live post failed.
func (c *commandContext) reconcileLoop(ctx context.Context, deliverer *slackDeliverer, logger *syncLogger, reconcileNow <-chan struct{}) {
	ticker := time.NewTicker(slackReconcileInterval)
	defer ticker.Stop()
	for {
		c.reconcileSlack(ctx, deliverer, logger)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-reconcileNow:
		}
	}
}

// openNotificationStream subscribes to the daemon's notification SSE endpoint
// and hands back the live response, undecoded, for the caller to consume. It
// clears the HTTP client timeout because a long-lived stream must be bounded by
// the caller's context rather than by a deadline that would sever it
// mid-consumption. The caller owns closing Body.
func (c *commandContext) openNotificationStream(ctx context.Context) (*http.Response, error) {
	streamURL, err := c.daemonURL("/api/v1/notifications/stream")
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, streamURL, http.NoBody) // #nosec G704 -- daemon host is fixed loopback; path is an internal API route.
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")

	client := *c.deps.HTTPClient
	client.Timeout = 0
	resp, err := client.Do(req) // #nosec G704 -- request target is the fixed loopback daemon URL above.
	if err != nil {
		return nil, fmt.Errorf("call daemon: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var e apiError
		_ = json.NewDecoder(resp.Body).Decode(&e)
		_ = resp.Body.Close()
		return nil, apiResponseError{StatusCode: resp.StatusCode, ErrorBody: e}
	}
	return resp, nil
}

// reconcileSlack enqueues every current unread notification; the worker's
// dedupe skips those already delivered. The hub is in-process and best-effort
// with no replay, so the unread listing is the only way to recover anything the
// stream missed.
func (c *commandContext) reconcileSlack(ctx context.Context, deliverer *slackDeliverer, logger *syncLogger) {
	all, err := c.listUnreadSlack(ctx)
	if err != nil {
		if ctx.Err() == nil {
			logger.printf("could not reconcile unread notifications: %v", err)
		}
		return
	}
	for i := len(all) - 1; i >= 0; i-- {
		if ctx.Err() != nil {
			return
		}
		deliverer.enqueue(ctx, all[i])
	}
}

func (c *commandContext) listUnreadSlack(ctx context.Context) ([]slackNotification, error) {
	var all []slackNotification
	var before time.Time
	var beforeID string
	for ctx.Err() == nil {
		values := url.Values{"status": {"unread"}, "limit": {strconv.Itoa(slackUnreadLimit)}}
		if !before.IsZero() {
			values.Set("before", before.Format(time.RFC3339Nano))
			values.Set("beforeId", beforeID)
		}
		var listed listNotificationsAPIResponse
		if err := c.getJSON(ctx, "notifications?"+values.Encode(), &listed); err != nil {
			return nil, err
		}
		all = append(all, listed.Notifications...)
		if len(listed.Notifications) < slackUnreadLimit {
			break
		}
		last := listed.Notifications[len(listed.Notifications)-1]
		if last.CreatedAt.IsZero() || last.ID == "" || (last.CreatedAt.Equal(before) && last.ID == beforeID) {
			return nil, errors.New("could not advance unread notification cursor")
		}
		before, beforeID = last.CreatedAt, last.ID
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return all, nil
}

// consumeSlackStream reads SSE frames until the stream ends or ctx is done,
// enqueueing each parsed notification. The daemon emits a single event type
// with one `data:` line per frame; the multi-line join below follows the SSE
// spec so a future multi-line payload would still parse rather than silently
// concatenating.
func (c *commandContext) consumeSlackStream(ctx context.Context, resp *http.Response, deliverer *slackDeliverer, logger *syncLogger) {
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), slackStreamLineCap)

	var data []string
	for scanner.Scan() {
		if ctx.Err() != nil {
			return
		}
		// Tolerate CRLF: a bare \r would otherwise ride into the JSON payload.
		line := strings.TrimSuffix(scanner.Text(), "\r")
		switch {
		case strings.HasPrefix(line, ":"):
			// SSE comment / keep-alive.
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		case line == "":
			payload := strings.Join(data, "\n")
			data = nil
			if payload == "" {
				continue
			}
			var n slackNotification
			if err := json.Unmarshal([]byte(payload), &n); err != nil {
				logger.printf("skipping unparsable notification frame: %v", err)
				continue
			}
			deliverer.enqueue(ctx, n)
		}
		// Other SSE fields (event:, id:, retry:) need no handling: the daemon
		// emits exactly one event type on this stream.
	}
	// Distinguish a real read failure from a clean end-of-stream, so an
	// oversized line or transport error is not misreported as a plain reconnect.
	if err := scanner.Err(); err != nil && ctx.Err() == nil {
		logger.printf("notification stream read error: %v", err)
	}
}
