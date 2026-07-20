package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

const (
	// slackWebhookEnv is the documented way to supply the webhook URL. It is
	// preferred over the flag because the URL is a secret and this command is
	// long-lived, so a flag value would sit in the process table.
	slackWebhookEnv = "AO_SLACK_WEBHOOK_URL"

	// deliveredIDCap bounds the set of already-delivered notification IDs so a
	// long-lived process does not grow without limit. Reconciliation only ever
	// lists *unread* notifications, which the daemon caps at 100 per response,
	// so an ID evicted from a set this size can no longer appear in any future
	// listing and cannot cause a duplicate.
	deliveredIDCap = 512

	// slackReconnectDelay is the pause before re-subscribing after the stream
	// ends, and the base for backoff when the daemon is temporarily unreachable.
	slackReconnectDelay = 2 * time.Second
	// slackMaxReconnectDelay caps the backoff so a daemon restart is picked up
	// promptly rather than after an ever-growing wait.
	slackMaxReconnectDelay = 30 * time.Second

	// slackStreamLineCap bounds a single SSE line. Notification bodies are short
	// enough that the default scanner buffer would do; this leaves headroom
	// without letting a malformed stream consume unbounded memory.
	slackStreamLineCap = 1 << 20
)

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
		return fmt.Errorf("post to Slack: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("slack webhook returned HTTP %d", resp.StatusCode)
	}
	return nil
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
		// Slack link syntax: <url|label>. The URL is not escaped as text; a
		// notification's PR URL comes from the daemon, not from user input.
		parts = append(parts, "<"+prURL+"|View PR>")
	}
	// Collapse newlines so one notification is always one Slack line.
	return strings.Join(strings.Fields(strings.Join(parts, " ")), " ")
}

// deliveredSet remembers which notification IDs have already been sent to
// Slack, evicting the oldest once it is full. Bounded on purpose: see
// deliveredIDCap.
type deliveredSet struct {
	ids   map[string]struct{}
	order []string
	cap   int
}

func newDeliveredSet(capacity int) *deliveredSet {
	return &deliveredSet{ids: make(map[string]struct{}, capacity), cap: capacity}
}

// add records an ID and reports whether it was newly added — i.e. whether the
// caller should deliver it.
func (s *deliveredSet) add(id string) bool {
	if _, seen := s.ids[id]; seen {
		return false
	}
	s.ids[id] = struct{}{}
	s.order = append(s.order, id)
	if len(s.order) > s.cap {
		oldest := s.order[0]
		s.order = s.order[1:]
		delete(s.ids, oldest)
	}
	return true
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
	poster := slackPoster{client: &client, webhookURL: webhookURL}
	delivered := newDeliveredSet(deliveredIDCap)

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
			c.deps.Sleep(backoff)
			if backoff *= 2; backoff > slackMaxReconnectDelay {
				backoff = slackMaxReconnectDelay
			}
			continue
		}
		connected = true
		backoff = slackReconnectDelay

		c.reconcileSlack(ctx, poster, delivered)
		c.consumeSlackStream(ctx, resp, poster, delivered)
		_ = resp.Body.Close()

		c.deps.Sleep(slackReconnectDelay)
	}
	return nil
}

// openNotificationStream subscribes to the daemon's notification SSE endpoint
// and hands back the live response, undecoded, for the caller to consume. It
// clears the HTTP client timeout because a long-lived stream must be bounded by
// the caller's context rather than by a deadline that would sever it
// mid-consumption. The caller owns closing Body.
func (c *commandContext) openNotificationStream(ctx context.Context) (*http.Response, error) {
	url, err := c.daemonURL("/api/v1/notifications/stream")
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody) // #nosec G704 -- daemon host is fixed loopback; path is an internal API route.
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
func (c *commandContext) reconcileSlack(ctx context.Context, poster slackPoster, delivered *deliveredSet) {
	var listed listNotificationsAPIResponse
	if err := c.getJSON(ctx, "notifications?status=unread", &listed); err != nil {
		if ctx.Err() == nil {
			_, _ = fmt.Fprintf(c.deps.Err, "ao notify slack: could not reconcile unread notifications: %v\n", err)
		}
		return
	}
	for _, n := range listed.Notifications {
		c.deliverSlack(ctx, poster, delivered, n)
	}
}

// consumeSlackStream reads SSE frames until the stream ends or ctx is done.
// The daemon emits a single event type with one `data:` line per frame.
func (c *commandContext) consumeSlackStream(ctx context.Context, resp *http.Response, poster slackPoster, delivered *deliveredSet) {
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), slackStreamLineCap)

	var data strings.Builder
	for scanner.Scan() {
		if ctx.Err() != nil {
			return
		}
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "data:"):
			data.WriteString(strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		case line == "":
			payload := data.String()
			data.Reset()
			if payload == "" {
				continue
			}
			var n slackNotification
			if err := json.Unmarshal([]byte(payload), &n); err != nil {
				_, _ = fmt.Fprintf(c.deps.Err, "ao notify slack: skipping unparsable notification frame: %v\n", err)
				continue
			}
			c.deliverSlack(ctx, poster, delivered, n)
		}
		// Other SSE fields (event:, id:, retry:, comments) need no handling:
		// the daemon emits exactly one event type on this stream.
	}
}

// deliverSlack posts a notification unless it has already been delivered. A
// Slack failure is reported and skipped: a notification channel that exits
// because Slack hiccuped is worse than one that misses a line.
func (c *commandContext) deliverSlack(ctx context.Context, poster slackPoster, delivered *deliveredSet, n slackNotification) {
	if strings.TrimSpace(n.ID) == "" || !delivered.add(n.ID) {
		return
	}
	if err := poster.post(ctx, renderNotification(n)); err != nil {
		if ctx.Err() == nil {
			_, _ = fmt.Fprintf(c.deps.Err, "ao notify slack: delivery failed for %s: %v\n", n.ID, err)
		}
	}
}
