## 1. Rendering (pure, no I/O)

- [ ] 1.1 Write failing table tests for `renderNotification` covering `needs_input`,
  `ready_to_merge`, `pr_merged`, `pr_closed_unmerged`, a notification with no PR URL, and an
  unrecognized type falling back to its own title and body
- [ ] 1.2 Implement `renderNotification(NotificationResponse) string` in
  `backend/internal/cli/slack.go` until 1.1 passes

## 2. Slack webhook delivery

- [ ] 2.1 Write failing tests for the webhook poster against an `httptest` server: asserts a JSON
  `{"text": ...}` POST body, and returns an error on a non-2xx response
- [ ] 2.2 Implement the minimal webhook poster (standard library only, `context.Context` first arg)

## 3. Streaming helper on the shared client

- [ ] 3.1 Write a failing test in `client_test.go` for the new stream helper: resolves the daemon
  loopback URL from the run file, surfaces the "daemon is not running" error when the run file is
  absent, and returns an undecoded response body
- [ ] 3.2 Add the stream helper next to `doJSONPath` in `backend/internal/cli/client.go`, copying
  the HTTP client by value and clearing `Timeout` so long-lived reads are not cut off

## 4. Command wiring and configuration

- [ ] 4.1 Write failing tests for configuration resolution: `AO_SLACK_WEBHOOK_URL` set → accepted;
  `--webhook-url` set → accepted; neither set → `usageError` (exit code 2 via `ExitCode`)
- [ ] 4.2 Add the `notify` parent command and its `slack` child in
  `backend/internal/cli/slack.go`, following the nesting idiom in `review.go`
- [ ] 4.3 Register the command in `root.go` and confirm the telemetry allow/deny switch classifies
  it as intended

## 5. Delivery loop, ordering, and dedupe

- [ ] 5.1 Write a failing test proving ordering: the stream is subscribed **before** the unread
  listing is fetched
- [ ] 5.2 Write a failing test proving at-most-once delivery when the same notification ID appears
  in both the unread listing and the live stream
- [ ] 5.3 Write a failing test proving reconnect behavior: a dropped stream reconnects, re-reconciles,
  and delivers only notifications not previously delivered; and that nothing is sent when nothing
  was missed
- [ ] 5.4 Implement the loop — subscribe, then reconcile, then consume — with the bounded delivered-ID
  set sized above the service's unread list cap
- [ ] 5.5 Implement reconnect with backoff driven through `deps.Now` / `deps.Sleep`, following
  `waitForStopped` in `stop.go`

## 6. Failure and shutdown behavior

- [ ] 6.1 Write failing tests for: daemon unreachable at startup → plain error (exit code 1); Slack
  post failure → error reported, loop continues consuming
- [ ] 6.2 Implement the failure policy from 6.1
- [ ] 6.3 Wrap the command context with `signal.NotifyContext` so interrupt/termination exits 0, and
  test the cancellation path by cancelling the context directly

## 7. Verification and documentation

- [ ] 7.1 Run the full local gate: `go build ./...`, `go test ./...`, `go test -race ./...`,
  `go vet ./...`, and `npm run lint`
- [ ] 7.2 Confirm zero daemon surface changed — no migration, no `internal/notify` edit, no
  regenerated OpenAPI or TypeScript schema in the diff
- [ ] 7.3 Exercise it for real against a running daemon and a live Slack webhook: trigger a
  notification, confirm the Slack message, kill and restart the stream, confirm reconciliation
  delivers the gap without duplicates
- [ ] 7.4 Document `AO_SLACK_WEBHOOK_URL` and the command in the appropriate `docs/` page
