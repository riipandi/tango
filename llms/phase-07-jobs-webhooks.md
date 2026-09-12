---
status: planned
updated: 2026-09-12
---

# Phase 7 — Async Jobs & Webhooks

Queue consumers and outbound webhooks. Pocket ID reference: `backend/internal/job/`,
`backend/internal/service/webhook_service.go` (in v2: `webhook_events`, `webhook_logs` tables).

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

- [ ] Task type registry: `Register` named queues on `deps.Queue` at build time (email, webhook).
- [ ] `EmailTask` consumer — render via `internal/mailer` templates, send, retry on transient.
- [ ] Move all inline mailer calls (Phase 1/5) onto the queue.
- [ ] Recurring job helper — self re-enqueueing task with `At(now+interval)`; jittered.
- [ ] `webhook` store/service/handler — CRUD, secret per endpoint, event type filter.
- [ ] Signed delivery — canonical JSON body, `X-Signature: t=...,v1=...` (HMAC), 30s timeout,
      backoff retry via queue, log per attempt.
- [ ] Webhook endpoint DTOs validated via `pkg/validate`.
- [ ] Tests: signing vectors, outbox-to-delivery flow on testcontainers, retry on failure.

## Validation

- Webhook receiver test asserts signature validity and retry schedule.
- Three standard suites + lint + gofmt clean.

## Progress Log

- 2026-09-12 Phase created (planned).
- 2026-09-12 Added `pkg/validate` request-validation task per plan update.
