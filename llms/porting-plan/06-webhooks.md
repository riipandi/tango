---
status: planned
updated: 2026-09-15
---

# Webhooks

Goal: keep the tango-only webhook feature reliable and simple.

Prerequisites: inspect the existing webhook schema, queue, fetcher, audit event names, and responder
handlers. Preserve public route names unless the contract matrix identifies a defect.

## Acceptance and Yaak criteria

- Secrets never appear in list, get, log, error, or audit responses.
- The signed body is exactly the delivered body and can be verified independently.
- Timeout, non-2xx, retry, rotation, and duplicate-attempt behavior are tested.
- Update and re-send Yaak requests for event bodies, signing headers, rotation, filters, pagination,
  and expected delivery status. Remove requests for deleted routes.

## Tasks

1. Document CRUD, rotation, test delivery, event filters, logs, statuses, redaction, signing
   headers, canonical payload, and retry behavior. Commit: `docs: define webhook contracts`.
2. Keep endpoint configuration, event selection, delivery attempts, and pruning in focused stores
   and services. Ensure secrets never appear in list/get responses. Commit:
   `refactor: simplify webhook boundaries`.
3. Harden deterministic HMAC signing, timestamp handling, bounded timeouts, idempotent attempt
   records, queue delivery, retries, and pruning. Test tampering, timeout, non-2xx, duplicate
   delivery, and rotation. Commit: `fix: harden webhook delivery`.
4. Verify CRUD, authorization, redaction, rotation, test delivery, event delivery, and logs with
   Yaak and a controlled receiver. Commit: `test: verify webhook API and delivery`.
