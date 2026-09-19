---
status: planned
updated: 2026-09-19
owner: tango-connectrpc-remediation
---

# Phase 03 — Dead Code and Residue

Prerequisite: phase 02 done.

## Task 03.1 — Delete the unused RPC middleware

**Finding F8 (P2).** Two exported helpers in `internal/transport/middleware/rpc_guard.go` have no
production caller:

| Symbol | References |
| --- | --- |
| `ResolveRPCPrincipal` (line 108) | its own doc comment only |
| `RPCAdminProcedureGuard` (line 160) | its own doc comment only |

Neither is exercised by a test either, so `golangci-lint` does not flag them and they look
intentional. `RPCAdminProcedureGuard` is also functionally superseded: `RPCPrincipalGuard` already
covers the admin-per-procedure case for the services that mix visibility, and `RPCAdminGuard`
covers the whole-service case.

The refactor shipped several guard variants (`RPCPrincipalGuard`, `RPCPrincipalAuth`,
`RPCAdminGuard`, `RPCMachineDenied`, `RPCSessionAuth`, plus the two dead ones). Two of the seven
carry no traffic.

### Required final shape

No exported middleware without a caller. The surviving guard set is documented in one place so a
future reader does not invent a seventh.

### Steps

1. Delete `ResolveRPCPrincipal` (line 108) and its doc comment. The unexported `resolveRPCPrincipal`
   stays: `RPCPrincipalGuard` calls it at line 85 and `RPCPrincipalAuth` calls it at line 119.
2. Delete `RPCAdminProcedureGuard` (line 160) and the `adminProcedureGuard` type (line 164) with
   its three streaming-hook stubs. Nothing else references the type.
3. Add a short comment block above the surviving guards in `rpc_guard.go` naming when to use which:
   `RPCPrincipalGuard` for a mixed-visibility service, `RPCPrincipalAuth` for a fully protected
   non-admin service, `RPCAdminGuard` for a fully admin service, `RPCSessionAuth` for a service
   that must reject machine credentials at the mount, `RPCMachineDenied` for per-procedure session
   enforcement inside an otherwise machine-capable service.
4. Re-run `task lint` to confirm no new unused-symbol findings appear.

Validation: `task lint` and `task test:go -- ./internal/transport/...` pass; `go build ./...`
succeeds.

Commit: `refactor(rpc): drop the unused rpc middleware`

## Task 03.2 — Remove the empty package files

**Finding F9 (P2).** Two tracked files contain only a package clause:

| File | Content |
| --- | --- |
| `modules/identity/account/store.go` | `package account` |
| `modules/identity/password/handler.go` | `package password` |

`modules/identity/account/schema.go` holds the package doc comment and must stay. The other two
are leftovers: `store.go` survives because `AGENTS.md` mandates one store file per feature even
when the store lives elsewhere, and `handler.go` survives because a TODO comment was removed in
commit `e042a90` without deleting the file.

`modules/identity/account/store.go` is the interesting one. The account feature reads and writes
through `modules/identity/user` and `modules/identity/session` stores; it has no store of its own.
The file exists only to satisfy a naming convention that does not apply.

### Required final shape

No tracked file whose only content is a package clause. If `AGENTS.md` genuinely requires a
`store.go` in every feature directory, then either the rule is wrong or the account feature needs a
real store — decide, do not leave the empty file.

### Steps

1. Delete `modules/identity/password/handler.go`. Confirm nothing references it (the package is
   imported for `password.NewService` from `service.go`, which stays).
2. Decide `modules/identity/account/store.go`:
   - if the convention is a directory listing rather than a hard requirement, delete the file and
     adjust nothing else;
   - if the convention is enforced, move the account feature's persistence into it or amend the
     `AGENTS.md` rule to say "a `store.go` exists when the feature owns persistence".
3. Check for other package-only files while here:
   `modules/admin/appconfig/schema.go` and `modules/federation/schema.go` also carry only a package
   clause plus doc comment. Decide each with the same rule; a package doc comment alone is
   acceptable, a bare `package x` line is not.

Validation: `go build ./...` and `task test:go -- ./modules/identity/...` pass.

Commit: `refactor(identity): drop the empty package files`

## Task 03.3 — Fix the comments that cite a deleted route

**Finding F10 (P2).** The version metadata moved to ConnectRPC in commit `bc58675` and the REST
routes `/api/version/current` and `/api/version/latest` were deleted. Four comments still describe
the old route as the consumer of the version feed:

| File | Line | Comment |
| --- | --- | --- |
| `internal/registry/registry.go` | 100 | `// supplies /api/version/latest.` |
| `internal/jobs/registry.go` | 43 | `// feed supplies /api/version/latest.` |
| `internal/jobs/registry.go` | 51 | `// The version feed supplies /api/version/latest.` |
| `cmd/launcher/serve.go` | 60 | `// for /api/version/latest; a nil source keeps the deployed version.` |

`internal/transport/middleware/ratelimit_test.go:131` and `internal/transport/http_test.go:78-79`
reference the paths deliberately, as negative assertions, and must stay.

### Required final shape

Each comment names the live consumer: `VersionService.Latest` below `/rpc`.

### Steps

1. Rewrite the four comments to reference `VersionService.Latest` over `/rpc`.
2. Search for other comments that name a deleted internal REST route:
   `grep -rn "/api/" --include="*.go" internal/ modules/ cmd/ | grep -v _test` and classify each
   hit as live (retained REST route, protocol path) or stale.
3. Fix every stale hit in this commit. Do not touch comments about retained routes.

Validation: `task check` (go vet) and `task lint` pass.

Commit: `docs: point the version comments at the connect surface`
