# Notification reliability: epic and tasks (draft)

Goal: a user always receives a notification. The `notifications` row is the source of truth; realtime, email and push are repairable accelerators.

## Epic: Reliable notification delivery

### Phase 0: Client catch-up (web only)

**T0.1 Refetch notifications on socket reconnect, focus and visibility**
- Files: `apps/web/src/routes/_authenticated.tsx`, `apps/web/src/lib/socket-client.ts`
- Invalidate `["notifications"]` on socket `connect` (including reconnect), `window` focus and `visibilitychange` to visible.
- AC: after a simulated socket drop and a notification created meanwhile, the badge updates on reconnect without a reload. Unit test covers each trigger.

**T0.2 Safety-net unread-count poll**
- Poll the unread count about every 60s while the tab is visible; stop when hidden.
- AC: with realtime disabled, the badge updates within about 60s. No polling while the tab is hidden.

**T0.3 Stop swallowing notification errors on producers**
- Files: `service/notification/service.go`, `service/task/activity_service.go` (`handleMentions`)
- Log (slog, with notification type and recipient) every discarded error in `publishCreated`, `publishNotificationEvent` and `NotifyMentioned`'s caller.
- AC: forced failures in tests produce a warn or error log; behavior otherwise unchanged.

### Phase 1: Consumer hardening (Go)

**T1.1 Reclaim stale pending messages with XAUTOCLAIM**
- Files: `worker/notification_consumer.go`, `worker/question_notification_consumer.go`, `worker/plugin_event_consumer.go`
- Periodically `XAUTOCLAIM` entries idle longer than a threshold, including those owned by consumers from dead pods.
- AC: a message left pending by a consumer name that no longer exists is processed by a live consumer.

**T1.2 Max-delivery cap and dead-letter stream**
- After N failed deliveries (via `XPENDING` delivery count), move the message to a `*.dead` stream, ack it, and log. Add a metric.
- AC: a poison message stops retrying after N attempts and appears in the dead-letter stream.

**T1.3 Retry `repo.Create` in NotifyMentioned and NotifyDocCommentMentioned**
- Short bounded retry with backoff for transient DB errors; return the error to the caller where today it is dropped.
- AC: a transient create failure is retried and succeeds; a permanent one is logged.

**T1.4 Review NOGROUP and `$` start decisions**
- Document, and where safe change, the cases where the backlog is skipped (assignment NOGROUP recovery, question consumer start). Prefer idempotency over skipping.
- AC: written decision per consumer; tests for any behavior change.

#### T1.4 decisions (recorded)

- **Assignment consumer (`api.notification_writer`) and plugin dispatcher (`api.plugin_dispatcher`):** keep recreating a lost group at `$` on NOGROUP. Replaying the retained history from `0` would send every past assignment and email again, because neither handler dedups by event. Skipped backlog is accepted until Phase 2 gives deliveries an idempotency key; revisit then.
- **Question consumer (`api.question_notifier`):** keeps starting at `$`. A question asked while the API was down is not worth notifying about later, and replaying the whole agent event stream is expensive.
- **Consumer names** stay hostname-based. The reclaimer (T1.1) is what recovers entries owned by a name that no longer exists. Idle consumer names are not yet deleted from the groups (`XGROUP DELCONSUMER`); harmless but they accumulate per pod rename.
- **Delivery is now at-least-once** for these three consumers: an entry whose side effect ran but whose ack was lost is handled again. For `NotifyAssigned` that can mean a duplicate inbox row; Phase 2's `(notification_id, channel)` key is the fix.

### Phase 2: Delivery outbox

**T2.1 Migration: `notification_deliveries`**
- Columns: `id`, `notification_id` (FK), `channel` (`realtime`/`email`/`push`), `status`, `attempts`, `next_attempt_at`, `last_error`, timestamps. Unique on `(notification_id, channel)`. Index on due rows.
- AC: migration up and down; repository methods with tests.

**T2.2 Create notification and delivery rows in one transaction**
- Extend `notificationdom.Repository.Create` (or add a method) to insert delivery rows atomically.
- AC: no notification without its delivery rows; rollback test.

**T2.3 Delivery dispatcher worker**
- Claim due rows with `FOR UPDATE SKIP LOCKED`; exponential backoff; cap attempts; mark `sent` or `failed`. Safe across multiple API replicas.
- AC: two dispatchers never double-send; failed sends retry with backoff; exceeding the cap marks `failed`.

**T2.4 Move realtime publish behind the dispatcher**
- `publishCreated` becomes a delivery channel. Keep the immediate publish as a fast path; the dispatcher repairs misses.
- AC: killing realtime during creation still results in a successful delivery once it is back, or a client catch-up (T0.1) covers it.

**T2.5 Optional per-user notification sequence**
- Add a monotonically increasing per-user `seq` to realtime notification events so the client detects gaps and refetches.
- AC: a skipped `seq` triggers a refetch. Decide against folding into the `sync:*` protocol, or in favor, in the task.

#### Phase 2 as built

- T2.1–T2.4 done: migration `000097` (`notification_deliveries`, one migration with the Web Push and Do Not Disturb tables below), notification + realtime delivery inserted in one transaction, `NotificationDeliveryDispatcher` (leases due rows with `FOR UPDATE SKIP LOCKED` in a short transaction, sends outside it, then records the outcomes; backoff 15s doubling to 5min, 8 attempts, expires after 15min, prunes sent after 3d and failed after 30d). The creating request still publishes immediately and marks the delivery sent; the first dispatcher look is 30s later.
- T2.5 (per-user `seq`) **not built, by decision**: the outbox repairs "API never handed it to Valkey"; Pub/Sub loss after Valkey accepts it is covered by Phase 0's refetch on reconnect and the 60s poll. A `seq` would only shave latency off that case. Revisit if the sync protocol grows a per-user feed.
- Only the `realtime` channel is dispatched. `email` and `push` are accepted by the schema but no dispatcher sends them (Phase 3 / 5).

### Phase 3: Reliable email

**T3.1 Design: plugin delivery-result contract**
- `HandleEvent` returns nothing today. Decide: change the wasm contract to return success or failure, or move email sending into core. Write a short ADR. Check `minCoreVersion` and the compatibility story for existing plugin installs.
- AC: ADR approved.

**T3.2 Implement result reporting and idempotency**
- Per the ADR. Idempotency key `notification_id + channel` so retries never double-send.
- AC: a forced SMTP failure is retried; a retry after a success never sends twice.

**T3.3 Route email through the outbox**
- Dispatcher emits the email delivery; stop acking `StreamPluginEvents` notification topics regardless of outcome.
- AC: SMTP down for N minutes, then up: the email arrives once.

**T3.4 Admin-visible delivery log and failure count**
- Surface in the SMTP plugin admin page: recent deliveries, failures, last error.
- AC: an admin can see why an email failed.

#### Phase 3 decision and as built

**T3.1 decision: observe failures in the host; do not change the plugin ABI, and do not route email through the Phase 2 outbox.**

- `HandleEvent` returns nothing, but the email is sent by the core's own `paca.send_email` host function, so the host already sees every send fail. It now records those failures against the event being handled (`platform/plugin/event_delivery.go`) and `Runtime.DeliverEvent` returns a per-plugin result. No plugin needs rebuilding or re-releasing, and old and new plugins behave the same.
- A failure is **retryable** (connection refused, timeout, SMTP 4xx) or **permanent** (SMTP 5xx such as bad credentials, an invalid address, a missing permission, an oversized payload).
- `PluginEventConsumer` leaves an entry unacked on a retryable failure, so the Phase 1 reclaimer redelivers it (after 60s, up to 5 times, then dead-letters it). The failed plugin names are kept in a Valkey set per entry, so a redelivery runs only those plugins; a plugin that already sent its email or called its webhook is not run again. A permanent failure is recorded on `paca.plugin_events.dead` with a reason and not retried.
- Why not the outbox: two of the four email topics (doc and task-description mentions) have no notification row to hang a delivery on until Phase 4, and the stream path already gives spacing, a cap and a dead-letter record. The outbox stays for the realtime channel.

**Not covered, by design**
- A plugin that fails before it tries to send (SMTP not configured, the recipient opted out, a config decrypt error) looks like success. For the first two that is correct. An explicit status from `HandleEvent` would close the rest; it can be added later without breaking existing plugins (the host would treat no result as success).
- A trap or timeout of the plugin's own code is not retried, as before: re-running arbitrary plugin code (webhooks, GitHub calls) is not safe to do implicitly.
- At-least-once: if a send succeeds but the process dies before the ack, the email can be sent twice.

**T3.4 (admin-visible delivery log) not built here.** The page belongs to the SMTP plugin's own UI (`paca-plugin-smtp`, a separate repo). The core now logs every failure (warn for retries, error for permanent ones) and keeps permanent failures and exhausted retries on `paca.plugin_events.dead`, which is the durable record a log page can read.

### Phase 4: Coverage gaps

**T4.1 Inbox rows for doc-content and task-description mentions**
- First verify whether the `task_id` non-null constraint still blocks this (migration `000075` added `document_id`). Add task-description rows; add doc-content rows with `document_id`.
- Keep the membership re-check from the current code.
- AC: a mention in a doc body or a task description shows in the bell, with correct navigation, without SMTP configured. Frontend text and i18n added in all locales (parity is enforced).

### Phase 5: Out-of-app reach and user control

**T5.1 Core `notification_preferences` model and API**
- Per-type and per-channel toggles; router-level permission gates only.
- AC: preferences respected by the dispatcher; migrate existing SMTP `user_email_preferences`.

**T5.2 Preferences UI**
- AC: user can toggle each type and channel; i18n in all locales.

**T5.3 Web Push (service worker, VAPID, subscription storage)**
- New `push` delivery channel; subscription management; expired subscription cleanup.
- AC: a notification arrives with the tab closed; unsubscribed or expired endpoints are pruned.

**T5.4 Optional: unread escalation or digest email**
- AC: unread for N minutes triggers one email; respects preferences.

#### Phase 5 as built (scope chosen with the user: Web Push with push-only preferences, VAPID keys auto-generated)

- **Preferences (T5.1/T5.2), push only.** `notification_preferences` (sparse; a missing row means enabled; channel `email` reserved) and an account Preferences card with a per-type switch. Email opt-out stays in the SMTP plugin, so the core matrix does not duplicate it; the SMTP preference migration was not done for that reason.
- **Web Push (T5.3).** Part 2 of migration `000097` (`push_subscriptions`, `notification_preferences`, `push_vapid_keys`). The VAPID pair is generated on first use and stored in the database, encrypted when `ENCRYPTION_KEY` is set (plaintext otherwise, like the other secrets). `Create` inserts a `push` delivery row in the same transaction only when the recipient has a subscription and has not switched the type off. A second `NotificationDeliveryDispatcher` (channel `push`) sends it with the same backoff, attempt cap and expiry as realtime. Dead subscriptions (404/410) are deleted. Sends go through the SSRF-guarded HTTP client and only to `https` endpoints. `public/sw.js` shows the notification and opens the right page.
- **Not built:** T5.4 (unread escalation / digest). **Known limits:** push text is rendered on the server in English (no per-user locale there); the service worker always shows the notification and closes it after 4s when the app is focused, because Safari revokes subscriptions whose pushes show nothing; not exercised against a real browser or push service.

#### Do Not Disturb (added after Phase 5)

Part 3 of migration `000097` (`user_notification_settings.dnd_until`). One switch for the whole account, with an end time or "until I turn it off" (stored as 9999-12-31). It is enforced in `pushsvc.Service.Send`, the single place every push passes through, and evaluated when a push is due, not when it was queued. A push dropped for DND is not retried, so nothing buzzes an hour after DND ends. The web client also silences the in-app chime while it is on (it reads the state from the cached push settings and re-checks the end time). It silences, it does not hide: the inbox and the bell badge keep counting. API: `GET /users/me/push` carries `do_not_disturb`; `PUT /users/me/push/do-not-disturb` sets it (`{until}`, `{indefinite: true}` or `{until: null}`); an end time must be in the future and at most 366 days away. It does not silence email (the SMTP plugin has its own opt-out) and has no schedule.

#### Poll placement

The 60s safety-net poll lives in `useNotificationCatchUp` and invalidates the whole `["notifications"]` prefix (not only the bell's query), so every list under it heals together.

#### Chat and meetings are not on master

This work was built on this branch from `master`, so it covers the notification system master has. The chat, calls and meeting-assistant features exist only on `release/v1.0.0`, and the pushes for them (direct messages, chat mentions, incoming calls, assistant proposals; a `push_messages` outbox, per-category preferences, and the read/call/proposal relevance checks) depend on that code, so they are **not** in this change. They were built and tested on `release/v1.0.0`; the full diff is saved as `notification-work-on-release-v1.0.0.patch` next to the repository, and `release/v1.0.0` has its own notification changes (mention context, in-app rows for doc and description mentions), which is where Phase 4 above is already done upstream. Port them when chat lands on master: expect to renumber the migrations and re-merge the notification service there.

### Phase 6: Observability

**T6.1 Metrics and alerting**
- Deliveries by channel and status, retries, dead-letter depth, creation-to-delivery latency.
- AC: dashboard panels and an alert on rising `failed` or dead-letter counts.

**T6.2 End-to-end tests**
- Playwright (`apps/e2e`): notification arrives after a socket drop; arrives after a realtime restart.
- AC: specs pass against the dedicated e2e stack.

#### Phase 6 as built (scope chosen with the user: an admin health endpoint and logs, not Prometheus)

The API has no metrics system (no Prometheus client, no `/metrics`), so Phase 6 adds no new infrastructure.

- **Health endpoint.** `GET /api/v1/admin/notifications/health`, gated on `settings:write` like the other instance diagnostics. It reports, per delivery channel: pending, retrying, overdue (due for more than 2 minutes, which the 5-second dispatcher should never allow), sent and failed in the last hour, creation-to-delivery latency (median and 95th percentile over the last hour), and the three commonest failure reasons; and, per notification stream and consumer group: unacknowledged entries, the longest wait, and dead-letter entries (last hour and total). It folds this into a status: **degraded** for failures or late deliveries in the last hour, an unacknowledged entry older than 5 minutes, recent dead letters, or a 95th percentile over a minute; **failing** for deliveries overdue by more than 10 minutes (a stalled dispatcher) or an entry unacknowledged for more than 15 minutes (a stuck consumer). Each reason is written out in plain words.
- **Admin page.** Admin → Settings → Notification delivery shows the status, the reasons and both tables, refreshed every 30 seconds.
- **Logs.** Each delivery dispatcher logs one summary line per 5 minutes in which it did anything (sent, retried, failed, expired) at Info, and at Warn when anything was given up on or expired, so a log-based alert can key on that line. The reclaimer already logs each dead-lettered entry at Error.
- **Alerting.** Not wired to a system, since the repo has none: poll the endpoint (status other than `ok`) or alert on the Warn summary line. A Prometheus exporter can be added later on top of the same queries.
- **E2E** (`apps/e2e/tests/realtime/notification-delivery.spec.ts`, feature `notification-delivery.feature`): a notification assigned while the browser is offline, while the announcement is swallowed with the socket still up (found by the 1-minute re-read), and, opt-in with `E2E_ALLOW_STACK_RESTART=1` because it stops the stack's realtime service, while the realtime service is down.
- **Limits.** The latency and failure figures cover what has not been pruned (sent deliveries are kept 3 days, failed 30). The dispatcher counters in the log summary can slightly over-count if a claim transaction rolls back.

## Dependencies

- T0.x and T1.x are independent and can run in parallel.
- T2.x depends on nothing in 0–1, but T2.4 and T2.5 build on T0.1.
- T3.x depends on T2.1–T2.3; T3.1 can start now.
- T4.1 is independent. T5.1 precedes T5.2–T5.4 and needs T2.3. T6.x follows 2–3.

## Open questions

- Scope of "always received": in-app only, plus email, or plus push?
- Is realtime still a single replica?
- Plugin contract change (T3.1) versus a core-owned email sender.

#### Review follow-ups

- A push dropped for Do Not Disturb marks its delivery `sent`, so the health page counts it as delivered, not as suppressed.
- `POST /users/me/push/subscriptions` re-points an existing endpoint to the caller. This is intended (a browser that signs in as another user keeps one row); endpoints are unguessable URLs, so knowing one is the proof of holding it.
- An account keeps at most 20 browser subscriptions; registering another deletes the oldest. Pushes to one notification's subscriptions are sent 4 at a time.
- A 403 from a push service deletes the subscription (VAPID key mismatch) only when a https public URL is configured for the VAPID contact; with the `mailto:admin@paca.local` placeholder it is only logged, since the service may be rejecting the placeholder.
- A stored VAPID private key that cannot be decrypted (`ENCRYPTION_KEY` rotated or removed) is an error, not a fallback to the ciphertext.
- The stream reclaimer counts deliveries locally when `XPENDING` cannot be read, so the dead-letter cap still applies.
