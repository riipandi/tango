---
status: done
updated: 2026-09-17
---

# Contract Baseline

Goal: replace optimistic historical status with a verified implementation inventory.

Prerequisites: complete `01-excluded-cleanup.md`, `architecture.md`, and `database.md`. Use the local Pocket ID
v2.14.0 checkout, `llms/endpoint-reference.md`, `llms/database-reference.sql`, `pkg/responder/`,
`pkg/crypto/`, and current route mounts as inputs. Capture the current `cmd/launcher` →
`internal/registry` → `internal/transport` bootstrap path before changing it.

## Tasks

1. Update `llms/endpoint-reference.md` and `llms/tango-deviations.md` so LDAP and Application
   Images remain `excluded` after cleanup. Output: excluded routes are not presented as parity work.
   Commit: `docs: define porting scope and exclusions`.
2. Build one matrix row per in-scope method/path. Each row must record exact request encoding,
   parameters, headers, auth, success/error statuses, response fields, responder or bare format,
   current owner, and test evidence. Output: no unverified `done` rows. Commit:
   `docs: add verified endpoint matrix`.
3. Add reusable Postgres, session, API-key, envelope, and header test helpers. Add Yaak requests for
   missing or incorrect routes without changing production behavior. Output: one shared parity
   harness for later agents. Commit: `test: add endpoint parity harness`.
4. Inventory every recoverable encrypted value and current cipher consumer. Record whether it must
   be hashed or encrypted and include the strict `enc:` format in the contract matrix. Reject
   unprefixed values; do not record a compatibility path. Commit:
   `docs: baseline encrypted value consumers`.

## Validation

Confirm the matrix covers every in-scope route, excludes LDAP and Application Images, and has no
route marked complete without a test reference. Run `git diff --check`.

## Yaak output

Identify the Yaak folder and request file for every in-scope route. Add missing requests and remove
or mark excluded requests for removed LDAP or Application Images routes.
