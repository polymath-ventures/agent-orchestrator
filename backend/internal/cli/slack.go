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
	// slackWebhookEnv is the documented way to supply the webhook URL. It is
	// preferred over the flag because the URL is a secret and this command is
	// long-lived, so a flag value would sit in the process table.
	slackWebhookEnv = "AO_SLACK_WEBHOOK_URL"

	// slackUnreadLimit is the page size reconciliation asks for. The daemon caps
	// unread listings at 100 and defaults to 50, so asking explicitly is the
	// difference between seeing the last 50 and the last 100 missed
	// notifications. A backlog deeper than this cannot be recovered from the
	// listing at all — documented in docs/cli/README.md.
	slackUnreadLimit = 100

	// slackStreamLineCap bounds a single SSE line. Notification bodies are short
	// enough that the default scanner buffer would do; this leaves headroom
	// without letting a malformed stream consume unbounded memory.
	slackStreamLineCap = 1 << 20
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
		return err
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
	ID        string `json:"id"`
	SessionID string `json:"sessionId"`
	ProjectID string `json:"projectId"`
	PRURL     string `json:"prUrl"`
	Type      string `json:"type"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	Status    string `json:"status"`
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
	title := strings.TrimSpace(n.Title)
	if title == "" {
		title = n.Type
	}
	parts := []string{notificationIcon(n.Type), "*" + escapeSlackText(title) + "*"}
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

// slackDeliverer serializes delivery and remembers what has already been sent.
// It is the single place dedupe and posting happen, so the two can never drift:
// an ID is recorded ONLY after Slack accepts the message, which means a failed
// post is retried by the next reconciliation rather than being silently lost.
//
// The ledger is unbounded on purpose. An earlier version evicted the oldest
// entries, reasoning that a notification too old to appear in an unread listing
// could never be re-offered — but this command never marks anything read, so a
// delivered notification stays unread and can re-enter the listing once newer
// ones are read in the UI. Eviction therefore reintroduces duplicates. Entries
// are a notification id apiece, so the ledger stays small in any realistic run.
type slackDeliverer struct {
	mu        sync.Mutex
	delivered map[string]struct{}
	poster    slackPoster
	errOut    io.Writer
}

func newSlackDeliverer(poster slackPoster, errOut io.Writer) *slackDeliverer {
	return &slackDeliverer{delivered: map[string]struct{}{}, poster: poster, errOut: errOut}
}

// deliver posts a notification unless it has already been delivered. A Slack
// failure is reported and left undelivered so reconciliation can retry it: a
// notification channel that exits because Slack hiccuped is worse than one that
// repeats a line.
func (d *slackDeliverer) deliver(ctx context.Context, n slackNotification) {
	if strings.TrimSpace(n.ID) == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, seen := d.delivered[n.ID]; seen {
		return
	}
	if err := d.poster.post(ctx, renderNotification(n)); err != nil {
		if ctx.Err() == nil {
			_, _ = fmt.Fprintf(d.errOut, "ao notify slack: delivery failed for %s (will retry on reconnect): %v\n", n.ID, err)
		}
		return
	}
	d.delivered[n.ID] = struct{}{}
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
			"read from Slack.\n\nSupply the webhook URL via " + slackWebhookEnv + " (preferred, so " +
			"it does not appear in the process table) or --webhook-url.",
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
		"Slack incoming webhook URL (prefer "+slackWebhookEnv+"; a flag value is visible in the process table)")
	return cmd
}

// runSlackNotify is the delivery loop. Each iteration subscribes to the stream
// FIRST and reconciles against the unread listing SECOND: any notification
// published between the two calls is caught by the listing, and any seen by
// both is collapsed by ID. That ordering is what makes at-most-once delivery a
// property of the loop rather than something a later check has to repair.
func (c *commandContext) runSlackNotify(ctx context.Context, opts slackNotifyOptions) error {
	webhookURL := strings.TrimSpace(opts.webhookURL)
	if webhookURL == "" {
		webhookURL = strings.TrimSpace(os.Getenv(slackWebhookEnv))
	}
	if webhookURL == "" {
		return usageError{errors.New("usage: --webhook-url is required (or set " + slackWebhookEnv + ")")}
	}

	client := *c.deps.HTTPClient
	client.Timeout = commandTimeout
	deliverer := newSlackDeliverer(slackPoster{client: &client, webhookURL: webhookURL}, c.deps.Err)

	connected := false
	backoff := slackReconnectDelay
	// Cancellation is how this command is meant to end, so it is the loop
	// condition rather than an error: every exit path below either returns a
	// real failure or falls out of the loop once the context is done.
	for ctx.Err() == nil {
		resp, err := c.openNotificationStream(ctx)
		if err != nil {
			// A daemon that was never reachable is an operator error worth
			// surfacing now; one that goes away mid-run is transient. A failure
			// caused by our own shutdown is neither — the loop condition ends it.
			if !connected && ctx.Err() == nil {
				return err
			}
			_, _ = fmt.Fprintf(c.deps.Err, "ao notify slack: reconnecting after stream error: %v\n", err)
			waitOrDone(ctx, backoff)
			if backoff *= 2; backoff > slackMaxReconnectDelay {
				backoff = slackMaxReconnectDelay
			}
			continue
		}
		connected = true
		backoff = slackReconnectDelay

		// Reconcile CONCURRENTLY with consuming, not before it. Subscribing
		// first is only half the guarantee: if the listing's deliveries had to
		// finish before the first stream read, a slow webhook could stall
		// consumption long enough for the daemon's 64-slot subscriber buffer to
		// overflow, and the hub drops rather than blocks — losing notifications
		// published after the listing snapshot. Consuming immediately keeps the
		// buffer draining. slackDeliverer serializes the two producers.
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.reconcileSlack(ctx, deliverer)
		}()

		c.consumeSlackStream(ctx, resp, deliverer)
		_ = resp.Body.Close()
		wg.Wait()

		waitOrDone(ctx, slackReconnectDelay)
	}
	return nil
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

// reconcileSlack delivers unread notifications the stream did not carry —
// those published while this process was disconnected. The hub is in-process
// and best-effort with no replay, so the listing is the only way to see them.
func (c *commandContext) reconcileSlack(ctx context.Context, deliverer *slackDeliverer) {
	var listed listNotificationsAPIResponse
	if err := c.getJSON(ctx, "notifications?status=unread&limit="+strconv.Itoa(slackUnreadLimit), &listed); err != nil {
		if ctx.Err() == nil {
			_, _ = fmt.Fprintf(c.deps.Err, "ao notify slack: could not reconcile unread notifications: %v\n", err)
		}
		return
	}
	for _, n := range listed.Notifications {
		if ctx.Err() != nil {
			return
		}
		deliverer.deliver(ctx, n)
	}
}

// consumeSlackStream reads SSE frames until the stream ends or ctx is done.
// The daemon emits a single event type with one `data:` line per frame; the
// multi-line join below follows the SSE spec so a future multi-line payload
// would still parse rather than silently concatenating.
func (c *commandContext) consumeSlackStream(ctx context.Context, resp *http.Response, deliverer *slackDeliverer) {
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
				_, _ = fmt.Fprintf(c.deps.Err, "ao notify slack: skipping unparsable notification frame: %v\n", err)
				continue
			}
			deliverer.deliver(ctx, n)
		}
		// Other SSE fields (event:, id:, retry:) need no handling: the daemon
		// emits exactly one event type on this stream.
	}
	// Distinguish a real read failure from a clean end-of-stream, so an
	// oversized line or transport error is not misreported as a plain reconnect.
	if err := scanner.Err(); err != nil && ctx.Err() == nil {
		_, _ = fmt.Fprintf(c.deps.Err, "ao notify slack: notification stream read error: %v\n", err)
	}
}
