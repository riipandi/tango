---
status: draft
updated: 2026-09-15
owner: tango-auth-porting
---

# Recoverable Secret Encryption Requirements

## Product requirement

All new recoverable encrypted values must be recognizable by the `enc:` prefix and must use the
existing `pkg/crypto` AES-256-GCM implementation. This creates one storage contract for application
settings, webhook secrets, provider tokens, private key material, and TOTP seeds while preserving
hash-only storage for credentials and one-time verification values.

## Wire format

```text
enc:<base64.RawStdEncoding(AES-256-GCM nonce || ciphertext || authentication tag)>
```

The prefix is case-sensitive and exact. `Encrypt` always emits it. `Decrypt` validates it before
decoding and rejects missing or malformed prefixes. There is no unprefixed format, legacy reader,
fallback decoder, dual-write path, compatibility column, or rewrite migration.

The expected shape is `enc:THIS_IS_ENCRYPTED_STRING`, where the suffix is an actual authenticated
ciphertext, not the literal placeholder text.

## Classification rules

| Value class | Storage rule |
| --- | --- |
| Passwords | One-way password hash; never reversible encryption |
| Reset/session/auth/recovery tokens | SHA-256 or purpose-specific hash when only verification is needed |
| TOTP seed | `enc:` encrypted because it must be recovered for verification |
| Webhook signing secret | `enc:` encrypted because workers must recover it |
| SCIM/provider token | `enc:` encrypted when the server must send it |
| OIDC private key/client secret | `enc:` encrypted when the server must recover it |
| Public keys and identifiers | Plain or existing public representation; no encryption |

The implementation agent must inventory actual columns and callers before adding a new encrypted
field. LDAP and Application Images are excluded and must not be added to the inventory.

## Security requirements

- AES-GCM authentication and a fresh nonce are preserved for every write.
- Encryption keys come from the existing configured secret derivation. No second cipher or ad hoc
  base64 wrapper is allowed.
- Plaintext, ciphertext, keys, nonces, TOTP seeds, webhook secrets, provider tokens, and private
  keys never appear in logs, audit records, errors, API responses, or saved Yaak requests.
- Decrypt failures fail closed and identify only the owning field or operation.
- Key rotation and multi-key fallback are separate approved work; this requirement does not add
  unbounded format negotiation.
- Ciphertext without `enc:` is invalid and must not be interpreted as plaintext or as an older
  supported format.

## Fresh-schema requirements

- Every new database schema and fixture uses `enc:` from the first write.
- Known encrypted columns use a database marker constraint where practical.
- The application fails fast on invalid encrypted configuration or malformed stored ciphertext.
- There are no legacy migrations, compatibility views, fallback readers, or temporary formats.

## API and verification requirements

The encryption format is an at-rest contract and must not change normal public response shapes.
Secret-bearing endpoints still use `pkg/responder`, return redacted views, or return a generated
secret only once where explicitly required. Update and re-send affected Yaak requests whenever
redaction, status, headers, or one-time response behavior changes. Never save ciphertext as a
substitute for a secret fixture in Yaak.

Required tests cover:

- prefix emission and exact prefix validation;
- round-trip Unicode and empty plaintext;
- random nonce and tamper rejection;
- malformed, missing, and duplicate prefix handling;
- database rejection of unprefixed stored values;
- key-size/configuration errors;
- API redaction and one-time secret responses;
- no secret leakage in logs, audit data, errors, or Yaak artifacts.

## Uncertainty and test execution

If a Pocket ID endpoint or stored field has multiple plausible meanings, the agent must stop that
dependent task, show the upstream evidence, and ask the project owner for confirmation. It must not
silently choose a behavior based on intuition. Independent work may continue.

Tests must fail fast with explicit timeouts. A hanging test, unavailable Docker daemon, or stalled
Yaak request must be reported as the first failure rather than allowed to consume the default long
timeout. Use a narrower focused test before retrying a broader gate.

## Acceptance criteria

- `pkg/crypto` is the only encryption implementation used by application modules.
- Every new recoverable encrypted database value starts with `enc:`.
- Hash-only values remain one-way hashes.
- No excluded LDAP or Application Images code is reintroduced.
- Fresh Postgres databases pass the migration and storage-contract tests.
- No legacy code or backward-compatibility behavior exists.
- All affected endpoint matrix rows, Yaak requests, and deviations are current.
