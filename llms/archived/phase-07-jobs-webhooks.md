---
status: done
updated: 2026-09-14
---

# Phase 7 — Async Jobs & Webhooks

Queue consumers and outbound webhooks. Upstream reference: `backend/internal/job/`
(analytics + file cleanup cron actors) and `backend/internal/service/version_service.go`.
Note: the webhooks named in this phase are a tango extension — Pocket ID v2.14.0 has no
webhook service and its schema has no `webhook_events`/`webhook_logs` tables (the tables in
tango's migration 00008 predate this phase).

## Goal

Background work runs on antree (retries, retention included); app events fan out to registered
webhooks with signed payloads; recurring jobs have a home.

## Deliverables

- `modules/queue` (or `internal/jobs`) — task type registry + worker helpers: `EmailTask` (moves
  Phase 5 mail sends onto antree), cleanup tasks.
- Recurring jobs — ticker-backed long-lived task added on boot (delayed re-enqueue pattern), or a
  small `internal/jobs` scheduler on top of antree; used for webhook pruning and token cleanup.
- `modules/webhook` — endpoint CRUD, event subscriptions, HMAC-SHA256 signing (`X-Signature`),
  delivery via `internal/fetcher`, attempt logging into `webhook_events`/`webhook_logs`.
- Outbox pattern — webhook dispatch enqueued as antree task in the same tx as the event write.

## Tasks

- [x] Task type registry: `Register` named queues on `deps.Queue` at build time (email, webhook).
- [x] `EmailTask` consumer — render via `internal/mailer` templates, send, retry on transient.
- [x] Move all inline mailer calls (Phase 1/5) onto the queue.
- [x] Recurring job helper — self re-enqueueing task with `At(now+interval)`; jittered.
- [x] `webhook` store/service/handler — CRUD, secret per endpoint, event type filter.
- [x] Signed delivery — canonical JSON body, `X-Signature: t=...,v1=...` (HMAC), 30s timeout,
      backoff retry via queue, log per attempt.
- [x] Webhook endpoint DTOs validated via `pkg/validate`.
- [x] Tests: signing vectors, outbox-to-delivery flow on testcontainers, retry on failure.
- [x] Yaak: folder "Tango Extensions (webhooks)" — endpoint CRUD + delivery test against a local
      receiver; not part of the upstream spec.

## Validation

- Use <https://webhooktest.net> via MCP, bucket ID for test: `01a09d0c-2abc-70ca-979b-fff00307bd4f`
- Webhook receiver test asserts signature validity and retry schedule.
- Three standard suites + lint + gofmt clean.

## Progress Log

- 2026-09-12 Phase created (planned).
- 2026-09-12 Added `pkg/validate` request-validation task per plan update.
- 2026-09-14 Phase started. Survey finding: the plan's upstream reference is wrong —
  Pocket ID v2.14.0 ships no webhook code (`backend/internal/service/webhook_service.go` does
  not exist) and the v2.14.0 schema dump in `database-reference.sql` contains no
  `webhook_events`/`webhook_logs` tables. Those tables exist only in tango's migration 00008
  (pre-seeded by an earlier foundation pass), so the webhook surface is a tango extension
  built on top of them; upstream-parity work in this phase is limited to the mail queue,
  the version feed, and `POST /api/application-configuration/test-email`.
- 2026-09-14 Queue consumers landed: `internal/jobs` owns the task types (EmailTask,
  WebhookDeliveryTask, RecurringTask) and the registry that registers the email and
  maintenance queues at build time; the webhook module registers its own delivery queue.
  `pkg/antree.TaskAddOp` gains `Executor(exec)` so an outbox enqueue can join a
  `datastore.WithTx` transaction (same shape as `Tx(pgx.Tx)`).
- 2026-09-14 Mail queue wired: one-time access email (admin) and email verification now
  queue through `EmailTask` instead of returning tokens; `POST /users/me/send-email-verification`
  answers 204 with no body (upstream parity — the token travels by email only). API-key
  expiry reminders land as a recurring job using the existing `expiration_email_sent_at`
  marker (closes the phase 6 deferral).
- 2026-09-14 Webhook module complete: endpoint CRUD, encrypted-at-rest signing secret
  (AES-256-GCM under a SHA-256 of `auth.secret_key`), event subscription via `TEXT[]`
  (empty/`*` = all), HMAC-SHA256 delivery (`X-Signature: t=...,v1=...` over
  `<unix>.<canonical body>`), per-attempt logging into `webhook_logs` including the rendered
  request (method, headers with signature, body) so a receiver can re-verify offline.
- 2026-09-14 Outbox proven end to end: emit writes one pending log row per subscriber plus
  the delivery tasks in a single transaction, then notifies the dispatcher after commit.
  Domain events fan out through the identity audit recorder (`eventFanout`), so every
  `user.*`, `api_key.*`, ... audit action reaches subscribed endpoints.
- 2026-09-14 Recurring maintenance: expired token/session sweep (6h), webhook log pruning
  (12h, 30-day retention), API-key expiry reminders (12h, 7-day window), latest-release
  check (6h) feeding `/api/version/latest` (drops its "mirrors deployed" deviation).
  Job clock is a package seam (`now`) because `auth_tokens`/`sessions` carry
  `CHECK (expires_at > CURRENT_TIMESTAMP)` — expired rows can only exist once wall time
  moves on, so tests freeze the clock instead of fighting the constraint.
- 2026-09-14 `POST /api/application-configuration/test-email` implemented as the first slice
  of `modules/appconfig` (202 accepted; defaults to the signed-in admin, explicit recipient
  is a tango extension); the settings CRUD stays with the appconfig phase.
- 2026-09-14 Live verification on :3080 (compose Postgres + webhooktest.net bucket
  `01a09d0c-2abc-70ca-979b-fff00307bd4f`): endpoint create 201, list/get 200 without the
  secret, rotate 200, test delivery 202; the bucket captured `webhook.test` and a real
  `user.created` fan-out with `X-Signature`/`X-Webhook-Event` set, and the recorded
  signature verified locally against the endpoint secret. Mail queue: email verification
  204, admin one-time access 204, test-email 202 — Mailpit received all four messages.
  Failure path: 5 attempts recorded on the log row with the 500 preserved.
- Known deviations from upstream: webhook CRUD/signing/logs are tango-only (upstream has no
  webhook feature); retry backoff is the queue's fixed 30s rather than exponential (antree
  carries one backoff per queue); one delivery = one `webhook_logs` row updated per attempt
  rather than a row per attempt; delivery fan-out happens in its own transaction rather
  than the producer's (audit rows and outbox rows are not atomic across modules).
