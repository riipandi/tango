---
status: done
updated: 2026-09-16
---

# TOTP MFA

Goal: add a small, recoverable second factor that composes with password and existing sessions.

Prerequisites: complete password authentication and inspect session assurance, queue, crypto, and
audit APIs. TOTP belongs inside the identity module and may expose a small identity-owned port to
session handling; do not create a generic identity-provider or authentication-policy framework.

## Security and Yaak acceptance criteria

- TOTP seeds are encrypted at rest with `pkg/crypto` and the canonical `enc:` prefix; they are
  never returned after enrollment.
- Store MFA state separately from full sessions. Pending authentication, active TOTP state, and
  recovery codes must not be represented by flags or JSONB fields on `users` or `sessions`.
- Codes use constant-time verification, documented bounded skew, and replay protection.
- Recovery codes are stored as hashes, shown once, and invalidated after use or rotation.
- Pending authentication expires, cannot be upgraded by another user, and is cleared on sign-out.
- Update and re-send affected Yaak requests whenever route names, bodies, headers, cookies, status
  codes, or response fields change. Never save seeds or recovery codes in requests.

## Tasks

1. Define enrollment, confirm/enable, verify, disable, status, and recovery-code contracts. Choose
   route names from existing module conventions before writing handlers. Define issuer, digits,
   period, skew, recovery-code count, and session assurance. Commit:
   `docs: define TOTP MFA contracts`.
2. Add a Postgres migration for dedicated TOTP state, `enc:` encrypted seed, recovery-code hash
   rows, consumed timestamps, pending-auth state, and audit data. Never return the seed after
   enrollment. Commit:
   `feat: add Postgres TOTP MFA storage`.
3. Implement seed generation, provisioning data, constant-time verification, bounded clock skew,
   replay protection, recovery-code hashing, and recovery-code rotation without an HTTP dependency.
   Commit: `feat: add TOTP verification service`.
4. Make password sign-in create pending authentication when MFA is required and issue a normal
   session only after verification. Expire pending states and prevent unintended passkey bypass.
   Commit: `feat: enforce MFA during authentication`.
5. Add handlers, responder envelopes, validation, authorization, audit events, rate limits, and
   Yaak requests. Test enrollment, wrong code, skew, replay, recovery, disablement, and assurance.
   Commit: `test: verify TOTP MFA lifecycle`.
