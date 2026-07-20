package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
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
		return fmt.Errorf("Slack returned HTTP %d", resp.StatusCode)
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
