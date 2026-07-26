package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/config"
	"github.com/aoagents/agent-orchestrator/backend/internal/runfile"
)

// commandTimeout bounds a mutating daemon call. Spawns do real work (git
// worktree add, tmux launch, hook install), so it is generous compared to the
// status probe timeout.
const commandTimeout = 2 * time.Minute

// apiError is the subset of the daemon's JSON error envelope the CLI surfaces.
// RequestID is surfaced so a failed command can be correlated with daemon logs.
type apiError struct {
	Message   string `json:"message"`
	Code      string `json:"code"`
	RequestID string `json:"requestId"`
}

type apiResponseError struct {
	StatusCode int
	ErrorBody  apiError
}

func (e apiResponseError) Error() string {
	if e.ErrorBody.Message == "" {
		return fmt.Sprintf("daemon returned HTTP %d", e.StatusCode)
	}
	return e.ErrorBody.String()
}

// String renders the envelope for the user: "<message> (<code>) [request <id>]",
// omitting whichever parts the daemon left empty.
func (e apiError) String() string {
	msg := e.Message
	if e.Code != "" {
		msg = fmt.Sprintf("%s (%s)", msg, e.Code)
	}
	if e.RequestID != "" {
		msg = fmt.Sprintf("%s [request %s]", msg, e.RequestID)
	}
	return msg
}

// getJSON sends GET /api/v1/<path> to the running daemon and decodes a 2xx
// response into out. A missing daemon or non-2xx API envelope is rendered the
// same way as mutating calls.
func (c *commandContext) getJSON(ctx context.Context, path string, out any) error {
	return c.doJSON(ctx, http.MethodGet, path, nil, out)
}

// postJSON sends body as JSON to POST /api/v1/<path> on the running daemon and
// decodes a 2xx response into out (out may be nil). A non-2xx response becomes
// an error built from the API error envelope. A missing run-file or a stale one
// (dead PID) yields a clear "not running" message rather than a
// connection-refused dump.
func (c *commandContext) postJSON(ctx context.Context, path string, body, out any) error {
	return c.doJSON(ctx, http.MethodPost, path, body, out)
}

// patchJSON sends body as JSON to PATCH /api/v1/<path> on the running daemon
// and decodes a 2xx response into out.
func (c *commandContext) patchJSON(ctx context.Context, path string, body, out any) error {
	return c.doJSON(ctx, http.MethodPatch, path, body, out)
}

// putJSON sends body as JSON to PUT /api/v1/<path> on the running daemon and
// decodes a 2xx response into out.
func (c *commandContext) putJSON(ctx context.Context, path string, body, out any) error {
	return c.doJSON(ctx, http.MethodPut, path, body, out)
}

// putJSONIfMatch is putJSON carrying an If-Match precondition.
func (c *commandContext) putJSONIfMatch(ctx context.Context, path, ifMatch string, body, out any) error {
	return c.doJSONPathHeaders(ctx, http.MethodPut, "/api/v1/"+path, map[string]string{"If-Match": ifMatch}, body, out)
}

// deleteJSON sends DELETE /api/v1/<path> to the running daemon and decodes a
// 2xx response into out.
func (c *commandContext) deleteJSON(ctx context.Context, path string, out any) error {
	return c.doJSON(ctx, http.MethodDelete, path, nil, out)
}

func (c *commandContext) doJSON(ctx context.Context, method, path string, body, out any) error {
	return c.doJSONPath(ctx, method, "/api/v1/"+path, body, out)
}

func (c *commandContext) postLoopbackJSON(ctx context.Context, path string, body any) error {
	return c.doJSONPath(ctx, http.MethodPost, path, body, nil)
}

// daemonDownMarker identifies the daemon-down class of CLI failure. It is the
// single copy of that sentence: `ao doctor`'s restart-window suppression
// matches this constant instead of holding its own transcription of the
// message, so the two cannot drift apart the way they did when the hint text
// changed.
const daemonDownMarker = "AO daemon is not running"

// daemonStartHint names actions that actually start a daemon.
//
// It deliberately does NOT name `ao start`, which despite its name starts no
// daemon: it fetches and opens the Electron desktop app (see start.go). Both
// listed actions are always accurate, which is why the hint is a constant
// rather than something probed per environment — a probe would run a
// subprocess on the hottest error path in the product to say something the
// operator can already tell apart at a glance.
const daemonStartHint = "start it with `ao daemon`, or `systemctl --user start ao.service` on a systemd deployment"

// daemonDownError builds the daemon-down failure. detail, when non-empty, is
// parenthesised between the marker and the hint.
func daemonDownError(detail string) error {
	if detail == "" {
		return fmt.Errorf("%s — %s", daemonDownMarker, daemonStartHint)
	}
	return fmt.Errorf("%s (%s) — %s", daemonDownMarker, detail, daemonStartHint)
}

// isDaemonDownMessage reports whether text carries a daemon-down failure
// produced by daemonDownError. It requires the marker in the position the
// message actually puts it — immediately followed by the detail parenthesis or
// the hint separator — so a line that merely quotes the phrase, such as a
// user-controlled path or an agent's own output, is not mistaken for one.
//
// It lives beside the builder deliberately: the recogniser and the message are
// the same fact, and TestDaemonDownMessageIsRecognisedByItsOwnRecogniser pins
// them together.
func isDaemonDownMessage(text string) bool {
	return strings.Contains(text, daemonDownMarker+" (") ||
		strings.Contains(text, daemonDownMarker+" — ")
}

// daemonURL resolves the loopback URL for a daemon route, failing with a clear
// "not running" message rather than a connection-refused dump when the run-file
// is missing or stale. Every caller that talks to the daemon goes through here,
// so run-file discovery lives in exactly one place.
func (c *commandContext) daemonURL(path string) (string, error) {
	cfg, err := config.Load()
	if err != nil {
		return "", err
	}
	info, err := runfile.Read(cfg.RunFilePath)
	if err != nil {
		return "", err
	}
	if info == nil {
		return "", daemonDownError("")
	}
	if !c.deps.ProcessAlive(info.PID) {
		return "", daemonDownError("stale run-file at " + cfg.RunFilePath)
	}
	return fmt.Sprintf("http://%s:%d%s", config.LoopbackHost, info.Port, path), nil
}

func (c *commandContext) doJSONPath(ctx context.Context, method, path string, body, out any) error {
	return c.doJSONPathHeaders(ctx, method, path, nil, body, out)
}

func (c *commandContext) doJSONPathHeaders(ctx context.Context, method, path string, headers map[string]string, body, out any) error {
	url, err := c.daemonURL(path)
	if err != nil {
		return err
	}

	var reader io.Reader = http.NoBody
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader) // #nosec G704 -- daemon host is fixed loopback; path is an internal API route.
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		if value != "" {
			req.Header.Set(key, value)
		}
	}

	// Reuse the injected client's transport (keeps it stubbable in tests) but
	// give daemon API calls far more headroom than the 2s status-probe timeout.
	client := *c.deps.HTTPClient
	client.Timeout = commandTimeout
	resp, err := client.Do(req) // #nosec G704 -- request target is the fixed loopback daemon URL above.
	if err != nil {
		return fmt.Errorf("call daemon: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var e apiError
		_ = json.NewDecoder(resp.Body).Decode(&e)
		return apiResponseError{StatusCode: resp.StatusCode, ErrorBody: e}
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}
