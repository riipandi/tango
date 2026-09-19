---
status: planned
updated: 2026-09-19
owner: tango-connectrpc-remediation
---

# Phase 01 — Authorization and Contract Correctness

Prerequisite: none. This phase is first because F1 is the only security-relevant finding.

## Task 01.1 — Restrict the machine credential to the documented surface

**Finding F1 (P0).** `internal/registry/registry.go` wraps 15 of the 18 module mounts with
`machine := middleware.RPCAPIKeyAuth(rt.apiKeys.Verify)` (line 180). The three mounts without it
are the auth lifecycle, MFA, and device approval. The documents state that
`X-API-KEY` authenticates "the admin application API" and that "API key create/renew stay
session-only so a leaked key cannot extend itself" (`docs/api-endpoint.md:15-18`,
`llms/connectrpc-plan/endpoint-reference.md:51-57`, `llms/endpoint-reference.md:21-23`).

Verified behavior contradicts that. A probe with one admin-owned key sent `X-API-KEY` to each
procedure through the real mount:

| Procedure | Result | Documented auth |
| --- | --- | --- |
| `AccountService/UpdateAccount` | 200, profile changed | `bearer` |
| `AccountService/ChangePassword` | 200, password rotated | `bearer` |
| `AccountService/GetAccount` | 200 | `bearer` |
| `AccountService/ListSessions` | 200 | `bearer` |
| `UserService/UpdateMe` | 200, profile changed | `bearer` |
| `SignupService/CreateSignupToken` | 200, token minted | `bearer` (admin) |
| `EmailVerificationService/SendEmail` | 200 | `bearer` |
| `AuditLogService/ListAll` | 200 | `bearer` (admin) |
| `ApplicationConfigurationService/GetAll` | 200 | `bearer` (admin) |
| `OidcConsentService/ListAllAuthorizedClients` | 200 | `bearer` (admin) |
| `MfaService/GetTotpStatus` | 401 | `bearer` (correct) |
| `DeviceApprovalService/GetPendingRequest` | 401 | `bearer` (correct) |

`AccountService.ChangePassword` is the worst case: a leaked key rotates the owner's password,
which is exactly the escalation the "a leaked key cannot extend itself" rule exists to prevent.

### Required final shape

Decide the intended boundary, then encode it once. Two options:

- **Option A (narrow).** `X-API-KEY` reaches only the admin application API: `ApiService`,
  `ApplicationConfigurationService`, `AuditLogService`, `OidcClientService`,
  `OidcConsentService`, `ScimProviderService`, `WebhookService`, `UserGroupService`,
  `CustomClaimService`, `UserService` admin procedures, `SignupService` token administration, and
  `ApiKeyService` list/delete. Remove the `machine()` wrapper from the account, email
  verification, MFA, device approval, auth lifecycle, and one-time access mounts.
- **Option B (broad, documented).** Keep the current reach, remove the session-only rule for
  `ChangePassword` and `UpdateAccount`, and rewrite the three credential paragraphs to state that
  a machine credential is a full principal for its owner, with password change and profile update
  included.

**Option A is the recommended default.** It matches the existing documents, it matches the
`ApiKeyService` session-only precedent, and it does not weaken a security invariant. Do not pick
Option B without an explicit owner decision, because it turns a leaked key into a full account
takeover.

The task must also add a regression test that pins the boundary, so the wrapper list cannot
silently widen again.

### Steps

1. Record the owner decision (Option A or B) in this file under "Decision".
2. Apply the decision to `internal/registry/registry.go` `MountRPC`.
3. Add `internal/registry/rpc_machine_boundary_test.go` (or extend
   `internal/registry/rpc_inventory_test.go`) that seeds one admin-owned API key and asserts the
   status for each procedure in the table above. For Option A the account, email verification,
   one-time access, MFA, device approval, and auth-lifecycle procedures must answer 401; for
   Option B they must answer 200 and the session-only exception must be limited to
   `ApiKeyService.Create`/`Renew`.
4. If Option B was chosen, update `docs/api-endpoint.md`, `llms/endpoint-reference.md`, and
   `llms/connectrpc-plan/endpoint-reference.md` in this same commit.
5. Update the auth column of the affected matrix rows so the record and the code agree.

Validation: the new boundary test passes; `task test:go -- ./internal/registry/...` passes.

Commit: `fix(rpc): restrict the machine credential to the documented surface`

## Task 01.2 — Make the application-configuration bootstrap anonymous

**Finding F2 (P1).** `modules/admin/appconfig/handler_rpc.go:33-35` puts
`ApplicationConfigurationServiceGetProcedure` in the `self` map of `RPCPrincipalGuard`. The guard
resolves a principal for every procedure in `admin` or `self`, so `Get` demands a bearer.

Three documents state the opposite:

- `docs/api-endpoint.md:101` — "Public bootstrap configuration";
- `llms/endpoint-reference.md:139` — "done — anonymous view the SPA reads before sign-in";
- `llms/connectrpc-plan/endpoint-reference.md:375` — "Public bootstrap view; anonymous."

Probe result through the real mount: anonymous `POST /tango.admin.v1.ApplicationConfigurationService/Get`
answers `401 {"code":"unauthenticated","message":"bearer token required"}`. The only anonymous
bootstrap today is the retained REST route `GET /api/application-configuration`
(`modules/admin/appconfig/module.go:74-77`), which returns a different shape.

This is a live break for the SPA: the documented pre-sign-in configuration read is unreachable
over `/rpc`.

### Required final shape

`Get` is anonymous on `/rpc`. `GetAll`, `Update`, and `TestEmail` stay admin-only. The `self` map
is removed and the anonymous procedure is omitted from both maps, which is the pass-through case
of `RPCPrincipalGuard`.

### Steps

1. Drop `ApplicationConfigurationServiceGetProcedure` from `self` and the `self` map itself.
2. Add a test in `modules/admin/appconfig/handler_rpc_test.go` that calls the mounted handler
   without any credential and asserts 200 plus a public-only payload. Assert `GetAll` still
   answers 401 anonymously.
3. Confirm the Yaak request `Public bootstrap configuration` (`rq_F5VSGPSv6T`) carries no
   authentication block, send it against the running server, and record the observed status in
   the task evidence.

Validation: the new anonymous test passes; the Yaak request answers 200 without credentials.

Commit: `fix(rpc): serve the public bootstrap configuration anonymously`

## Decision (Task 01.1)

**Option A — narrow.** Taken by the owner on 2026-09-19.

`X-API-KEY` reaches the admin application API only. The `machine()` wrapper was removed from the
account, email verification, one-time access, and auth-lifecycle mounts; the one-time access
service no longer resolves a machine principal at all. Because the user mount keeps `machine()` for
its admin procedures, the self procedures gained `middleware.RPCMachineDenied`, so a machine
principal cannot reach `UpdateMe`, `UpdateMyProfilePicture`, or `DeleteMyProfilePicture`.

Shipped in the same change:

- `internal/registry/registry.go` — the mount list above.
- `modules/identity/user/handler_rpc.go` — `RPCMachineDenied(self)` on the user service.
- `internal/registry/rpc_machine_boundary_test.go` — pins both halves: the allowed procedures
  accept a machine credential, the denied procedures answer unauthenticated or permission_denied,
  and the same self-service procedure still resolves a bearer principal.
- `docs/api-endpoint.md`, `llms/endpoint-reference.md`,
  `llms/connectrpc-plan/endpoint-reference.md` — the credential paragraph and the affected rows.

`SignupService` token administration stays machine-reachable because Option A lists it as part of
the admin application API. It is admin-only and mints signup tokens, not account changes; revisit
if the owner wants it narrowed too.

## Status (Task 01.2)

**Done.** `ApplicationConfigurationService.Get` is anonymous again. The `self` map was removed from
`modules/admin/appconfig/handler_rpc.go`, which is the pass-through case of `RPCPrincipalGuard`;
`GetAll`, `Update`, and `TestEmail` keep the `admin` map.

Shipped in the same change:

- `modules/admin/appconfig/handler_rpc_test.go` — `TestRPCConfigBootstrapIsAnonymous` sends an
  unauthenticated request through the mounted handler: `Get` answers 200 with the public payload,
  `GetAll` answers 401. The endpoint row now cites this test.
- Verified live against the running server and through Yaak: `Public bootstrap configuration`
  (`rq_F5VSGPSv6T`, environment `Development`) answers `200` with `requestHeaders` carrying no
  credential; the same request against the stale container on `:3443` answered `401`.

Note: `internal/registry/rpc_machine_boundary_test.go` (task 01.1) was committed on its own in
`d6ea484` without the implementation it pins, so `HEAD` fails that test until task 01.1's source
changes land.

## Phase 01 gate

Run at `HEAD` + the working-tree changes above:

| Command | Result |
| --- | --- |
| `go test ./...` (debug tags) | pass |
| `go test -tags release ./...` | pass |
| `go test -tags debug ./cmd/... ./database/...` | pass |
| `pnpm exec vitest run` | 43 tests pass, 7 files |
| `task lint` | 0 issues |
| `task check` | clean |
| `task typecheck` | clean |
| `task rpc:lint`, `task rpc:breaking`, `task rpc:stale` | clean |
