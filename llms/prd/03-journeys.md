---
status: draft
updated: 2026-09-15
owner: tango-auth-porting
---

# Actors and User Journeys

## Actors

- **End user**: signs in, manages password and MFA, views sessions, and authorizes OIDC clients.
- **Administrator**: manages users, groups, clients, APIs, claims, configuration, audit logs, and
  webhook endpoints.
- **OIDC client**: performs authorization-code and token flows using the documented protocol.
- **API consumer**: calls administrative/resource endpoints with session or API-key authentication.
- **Webhook receiver**: accepts signed event deliveries and returns HTTP success or failure.
- **Operations agent**: runs migrations, starts the service, inspects logs, and executes Yaak checks.

## Required journeys

### Password sign-in

1. User submits identity and password.
2. Server validates credentials without revealing whether the identity exists.
3. If MFA is disabled, server rotates and sets the authenticated session cookie.
4. If MFA is required, server creates a short-lived pending-auth state and does not issue a full
   session yet.
5. User completes TOTP or an approved second factor and receives a full session.

### Password recovery

1. User submits an email or username to request recovery.
2. Server returns the same public result for known and unknown identities.
3. Server stores only a hash of a short-lived, single-use token and queues the email.
4. User submits the token with a new password.
5. Server consumes the token atomically, changes the password, invalidates the intended sessions,
   and returns the standard response.

### TOTP enrollment

1. Authenticated user requests enrollment.
2. Server creates an encrypted pending seed and returns only enrollment data needed by the client.
3. User submits a valid code to confirm enrollment.
4. Server activates MFA, generates recovery codes, and displays recovery codes once.
5. Subsequent sign-ins require the second factor according to the configured policy.

### OIDC authorization

1. OIDC client sends the upstream-compatible authorization request.
2. Server validates client, redirect URI, response type, scope, state, and PKCE requirements.
3. User authenticates and approves if required.
4. Server returns the protocol-defined redirect or error without wrapping it in the JSON envelope.
5. Client exchanges the code and consumes tokens according to the OIDC contract.

### Webhook delivery

1. Administrator creates an endpoint and selects event types.
2. Server stores the endpoint and protects its signing secret.
3. An in-scope event writes an outbox/delivery record and queues delivery.
4. Worker sends the canonical body with signature and delivery metadata.
5. Server records attempts, retries bounded failures, and exposes redacted logs.
