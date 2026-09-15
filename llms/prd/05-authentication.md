---
status: draft
updated: 2026-09-15
owner: tango-auth-porting
---

# Password and TOTP MFA Requirements

## Password requirements

- Verify identity using the existing password service and crypto package.
- Enforce one password policy in the service, not duplicated across handlers.
- Return indistinguishable public failures for invalid identity and invalid password.
- Support sign-in, sign-out, session inspection, password change, forgot-password, and
  reset-password.
- Hash reset tokens at rest; make them expiring, single-use, and atomically consumed.
- Do not encrypt passwords, reset tokens, session tokens, or recovery codes when verification is
  the only required operation.
- Queue recovery email delivery through the existing queue/mailer path.
- Invalidate affected sessions after reset and rotate authentication cookies.
- Audit credential changes and security-sensitive events without logging secrets.
- Apply transport rate limits to sign-in and recovery endpoints.

## TOTP requirements

- Support enrollment, confirmation, status, verification, disablement, and recovery codes.
- Encrypt seeds at rest with `pkg/crypto` using `enc:` and never return an active seed after
  enrollment.
- Use documented algorithm, digits, period, and bounded clock-skew behavior.
- Prevent code replay within the accepted time window.
- Hash recovery codes, show them only once, and invalidate used or rotated codes.
- Require authorization for management operations and audit all state changes.
- Use a short-lived pending-auth state between password success and full session creation.
- Expire and clear pending-auth state on timeout, failed completion, and sign-out.
- Do not introduce a generic identity provider or policy framework for this feature.
- Do not add compatibility handling for older password, session, token, or MFA storage formats.
  Invalid or unsupported stored data fails closed.

## Response and cookie requirements

Password and MFA JSON responses use `pkg/responder`. Cookie attributes must be explicit and tested:
Secure, HttpOnly, SameSite, Path, expiry, and rotation behavior. Secrets, tokens, seeds, and
recovery codes must not appear in response bodies, logs, or persisted Yaak examples.

## Required security tests

Cover invalid credentials, disabled accounts, enumeration resistance, reset expiry/replay,
concurrent reset use, MFA wrong code, skew boundaries, replay, recovery-code reuse, disablement,
pending-auth expiry, and session invalidation.
