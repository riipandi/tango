---
status: draft
updated: 2026-09-15
owner: tango-auth-porting
---

# Webhook Requirements

## Public capabilities

- Authenticated administrator can create, list, get, update, delete, and rotate webhook endpoints.
- Administrator can select subscribed event types and request a test delivery.
- Authorized users can view redacted endpoint and delivery logs according to the existing policy.
- Delivery workers send signed payloads and record every attempt.

## Delivery contract

- The signature covers the exact bytes delivered, not a re-serialized equivalent.
- Signature format, timestamp handling, and header names are documented and stable.
- Requests have bounded connect, response, and total timeouts.
- Non-2xx responses, timeouts, and transport failures are observable and retried within a bounded
  policy.
- Duplicate attempts are safe to inspect and do not corrupt endpoint state.
- Secret rotation affects new deliveries without exposing old or new secrets through the API.
- Queue and pruning behavior remain bounded and do not block the originating HTTP request.

## Data exposure rules

Never expose signing secrets in create/list/get/update responses, logs, error messages, audit data,
or Yaak saved bodies. Delivery logs may include status, timing, attempt number, and redacted response
metadata, but not credentials or sensitive request content.

## Verification

Use a controlled receiver for successful delivery, timeout, non-2xx, malformed response, retry,
signature verification, rotation, and duplicate-attempt tests. Update and re-send Yaak requests for
every route/body/header/status change.
