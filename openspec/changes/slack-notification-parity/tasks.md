## 1. FIT — Add stable unread-notification pagination

- [x] 1.1 Write failing service/storage/controller tests for `before` + `beforeId` keyset pagination, including more than 100 unread rows, identical `createdAt` ties, invalid/incomplete cursor input, and unchanged no-cursor behavior. Reference: `ao-slack-notifier.mjs:746-780`; current gap: `controllers/dto.go:549-552`, `queries/notifications.sql:7-12`.
- [x] 1.2 Add the cursor-aware sqlc query and thread the cursor through `notification.ListFilter`, store interfaces/adapters, manager, controller DTO/parser, and OpenAPI generator; run `npm run sqlc` and `npm run api`. Reference algorithm: `ao-slack-notifier.mjs:750-778`.
- [ ] 1.3 Write failing Slack reconcile tests for loop-until-short-page, oldest-first enqueue, `(createdAt,id)` cursor advancement, and cancellation mid-pagination; implement paged reconciliation without changing the existing 100-row page size. Reference: `ao-slack-notifier.mjs:746-806`.

## 2. PORT — Persist delivery state and eliminate restart floods

- [ ] 2.1 Write failing tests for ledger load, missing/corrupt ledger recovery, AO-data-dir default path + override, parent-directory creation, atomic temp-file rename, and tail-bounded delivered IDs. Reference: `ao-slack-notifier.mjs:74,351-355,610-646`; state shape: `:619-634`.
- [x] 2.2 Implement the smallest Go ledger containing `version`, `initialized`, and delivered notification IDs; use code-owned path creation and atomic replacement under AO's configured data directory. Do not port reference-only attention/thread/digest fields. Reference port manifest: `ao-slack-notifier.mjs:417,610-646,619-634`.
- [x] 2.3 Write failing tests that first run seeds every paged unread ID without posting, persists `initialized`, and subsequent runs deliver only new IDs; implement bootstrap seed-before-stream. Reference: `ao-slack-notifier.mjs:717,725-745,784-790,808`.
- [x] 2.4 Write failing tests proving the in-memory delivered set is not mutated until Slack acceptance AND ledger persistence both succeed; implement that write-order invariant so persist failure causes safe re-offer. Reference lesson: PR #198 cycle 1; reference success-path state write: `ao-slack-notifier.mjs:794-806`.

## 3. PORT/FIT — Attention mentions and complete per-type rendering

- [x] 3.1 Write failing table tests covering distinct renderings for all eight `domain.NotificationType` values and the unknown-type fallback; implement intentional icons/one-liners. Reference informational icons: `ao-slack-notifier.mjs:166-172,379-388`; attention icons: `attention-core.mjs:32-45`.
- [x] 3.2 Write failing tests for Slack-member env/flag resolution and conditional mention behavior: mention `needs_input`, `prime_restart_capped`, and `model_unreachable`; never mention routine types; no member config still delivers. Reference conditional prefix: `ao-slack-notifier.mjs:154-165,891-915`, `attention-core.mjs:133-143`; fit mapping: `ao-slack-notifier.mjs:430-439`.
- [x] 3.3 Implement outbound mention rendering only; explicitly do not add bot-token posting, reply routing, thread bindings, or resolution edits. Reference code intentionally NOT ported: `ao-slack-notifier.mjs:901-913` and the two-way needs-response path.

## 4. IMPROVE/FIX — Outage alerting, heartbeats, and shutdown safety

- [ ] 4.1 Write failing stream-loop tests for N=3 consecutive post-connect failures, one alert per outage, latch reset on recovery, optional member mention, and unchanged first-connect runtime failure; implement the latched daemon-unreachable alert. Reference: `ao-slack-notifier.mjs:1017-1045,1179-1180,1211`.
- [x] 4.2 Preserve and regression-test this fork's superior cancellation-aware 2s→30s exponential reconnect backoff and periodic reconcile instead of porting the reference's flat 10s retry. Current improvement: `slack.go:56-78,398-419`; anti-reference: `ao-slack-notifier.mjs:80,1204-1216`.
- [ ] 4.3 Probe the daemon notification SSE handler for periodic comment heartbeats; if absent, write a failing controller test and add the minimal `: keepalive` tick + flush. Verify the Slack consumer ignores comments. History lesson #86; consumer: `slack.go:509-510`.
- [ ] 4.4 Add/extend SIGTERM cancellation tests covering backoff sleep, in-flight stream fetch, Slack POST, ledger persist boundary, and multi-page reconciliation. Reference lesson: #197/PR #200; current cancellation machinery: `slack.go:69-78,310-312`.

## 5. Verification checklist, docs, and generated contracts

- [ ] 5.1 Verify history lessons 1-10 line-by-line against the final code and record file:line evidence in the PR body: adopt heartbeat, persisted state/write ordering/shutdown/fleet-health coverage; record stream replay, lifecycle signatures, transition-aware emission, and pause gating as not-applicable at the Slack-channel layer with probe evidence.
- [x] 5.2 Update CLI docs for the ledger path/override, first-run seed behavior, Slack member configuration, paged recovery, and one-way/no-resolution boundary; remove any statement that backlog deeper than 100 is unrecoverable.
- [ ] 5.3 Run targeted Go tests, `go test ./...`, `go test -race ./...`, `go vet ./...`, `npm run sqlc`, `npm run api`, `npm run lint`, `npm run frontend:typecheck`, and `npx @redwoodjs/agent-ci run --all`; verify generated OpenAPI + TypeScript client changes are committed together.
- [ ] 5.4 Live-verify with a temporary daemon/webhook harness: first-run seed sends zero historical posts, new delivery persists, restart sends no duplicate, >100 backlog fully drains, attention types mention, routine types do not, third daemon failure alerts once, recovery re-arms, SIGTERM exits 0, and errors never expose the webhook URL.

## 6. Review and merge-readiness

- [ ] 6.1 Push each completed implementation phase and run the configured phase-review for the API/storage and persistence phases (sensitive state/data paths).
- [ ] 6.2 Run final-review with an independent cross-family reviewer plus the PR-integrated reviewer when available; resolve all current-head findings and record convergence/no-ping-pong.
- [ ] 6.3 Rebase onto the freshly fetched default branch, rerun full CI if rewritten, push with `--force-with-lease` only when required, and report the PR merge-ready without merging absent explicit authorization.
