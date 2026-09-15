---
status: planned
updated: 2026-09-15
---

# Encrypted Value Prefix

Goal: make every new recoverable encrypted string use the canonical `enc:` prefix provided by
`pkg/crypto`, without encrypting values that should only be hashed.

Prerequisites: read `pkg/crypto/`, the app settings migration, and every current cipher consumer
before changing the wire format. The current AES-256-GCM payload is base64 raw-standard encoded;
the prefix is metadata and is not part of the plaintext or ciphertext.

## Canonical contract

- `Cipher.Encrypt(plaintext)` always returns `enc:<base64.RawStdEncoding(ciphertext)>`.
- `enc:` is case-sensitive, has no whitespace, and must appear exactly once at the beginning.
- The ciphertext retains AES-256-GCM authentication and a fresh random nonce for every write.
- `Cipher.Decrypt` verifies the prefix before decoding and returns a stable package error for a
  missing or malformed prefix. It must never treat a plaintext value as encrypted data.
- The legacy path may be an explicitly named temporary cipher method or an owner-specific decoder,
  but normal `Decrypt` must not silently reinterpret arbitrary unprefixed input.
- Existing unprefixed ciphertext is handled only by an explicit, temporary legacy-read path for
  rows that predate this contract. A successful legacy read is rewritten in canonical format at
  the owning store boundary when safe; new code must not write unprefixed ciphertext.
- The prefix is not a password-hash marker. Passwords, reset tokens, session tokens, API keys,
  and recovery codes remain one-way hashed when the application only needs verification.

## Encrypted-value inventory

Before implementation, record the column, owner, write path, read path, key source, and migration
behavior for each recoverable value. At minimum inspect:

- application settings whose catalog marks the value sensitive;
- webhook signing secrets;
- SCIM provider tokens;
- OIDC/JWKS private material and client secrets that must be recovered;
- TOTP seeds;
- any remaining recoverable secret found by repository search.

Do not include LDAP or Application Images data in this inventory. Do not add encryption to public
keys, opaque identifiers, ordinary configuration, or values that are already hashes.

## Atomic tasks

1. Add the prefix/error contract to `pkg/crypto`, update round-trip, malformed-prefix, tampering,
   and fresh-nonce tests, and document the exact format. Keep callers compiling.
   Commit: `feat: add canonical encrypted value prefix`.
2. Inventory every current `Encrypt`/`Decrypt` caller and classify its value as hash-only,
   recoverable encrypted, or non-secret. Add focused tests for each recoverable owner and record
   the result in the contract matrix. Commit: `docs: inventory encrypted value consumers`.
3. Update webhook, SCIM, JWKS/OIDC, app settings, and identity MFA storage to write through the
   prefixed cipher. Keep secret responses redacted or one-time-only and map decrypt failures to
   safe internal errors. Commit: `fix: persist recoverable secrets with enc prefix`.
4. Add the compatibility read/rewrite path for existing unprefixed ciphertext. It must be scoped
   to known columns, preserve the original value on failed rewrite, and expose no plaintext in
   logs. Add Postgres tests for prefixed rows, legacy rows, malformed rows, and key failures.
   Commit: `fix: migrate legacy encrypted values safely`.
5. Update migrations/comments, `llms/database-reference.sql` notes if required, Yaak fixtures,
   and redaction checks. Confirm no encrypted database value is returned by admin, webhook, MFA,
   SCIM, or OIDC responses. Commit: `test: verify encrypted value boundaries`.

## Security and compatibility rules

- Use the existing key derivation and configuration source unless a separate approved task changes
  key management. Never derive a per-row key or introduce a second cipher implementation.
- Never log the plaintext, ciphertext, key, nonce, TOTP seed, webhook secret, provider token, or
  private key. Error messages may identify the owning field without including its value.
- A key rotation is a separate migration concern. Do not silently try multiple keys or accept an
  unbounded list of formats in this task.
- `pkg/crypto` remains independent of `internal/` and HTTP packages. Handlers use `pkg/responder`
  for failures but stores/services do not import responder.

## Validation

- `go test ./pkg/crypto/...` covers the format and error contract.
- Real-Postgres tests cover each recoverable secret owner and legacy rewrite behavior.
- `rg` confirms all new encryption writes use `Cipher.Encrypt` and no caller strips or invents a
  different prefix.
- Yaak requests are updated and re-sent whenever a response, header, or redaction behavior changes;
  no secret or ciphertext is saved in Yaak examples.
- Run focused tests with fail-fast behavior and explicit timeouts. Stop on the first failure; do
  not wait for a default integration or container timeout before reporting the blocker.
- Run `task test`, `task lint`, and `task check` before marking this plan complete, using the
  repository's fail-fast/timeout options where available.

## Uncertainty rule

If upstream Pocket ID behavior, a database meaning, or an endpoint contract is ambiguous, do not
guess from a nearby implementation. Record the evidence and ask the project owner for confirmation
before changing code, the matrix, or Yaak requests. Continue only with independent tasks that do
not depend on the unresolved decision.

## Acceptance criteria

- Every newly encrypted stored string begins with exactly `enc:`.
- Every recoverable secret has one owning package and one tested read/write path.
- Legacy data remains readable only through the bounded compatibility path and is rewritten when
  safe.
- Hash-only values remain hashes and are not made reversibly encrypted.
- No secret appears in an API response, Yaak request, log, audit record, or error message.
