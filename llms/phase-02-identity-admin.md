---
status: done
updated: 2026-09-12
---

# Phase 2 — Identity Admin Surface

User groups, custom claims, audit log API, and admin CRUD for users with pagination. Pocket ID
reference: `backend/internal/controller/{user,user_group,custom_claim,audit_log}_controller.go`.

## Goal

Admins manage users, group memberships, and custom claims in bulk; audit events are queryable
through the API.

## Deliverables

- `modules/identity/usergroup` — CRUD + atomic membership (WithTx) + per-user group listing
  (`user_groups`, `user_groups_users`); group checks used later by OIDC client access.
- `modules/identity/customclaim` — user and group claims (`custom_claims`) with duplicate checks
  (DB UNIQUE treats NULLs as distinct) + suggestions; `CustomClaimID` ownership moved here from
  the federation stub (claims only belong to users/groups).
- `modules/auditlog` — admin-guarded list API: pagination + filters (user, event, date range) +
  filter-value endpoints (`/filters/users`, `/filters/client-names`).
- `modules/identity/user` — admin CRUD completion: POST/PUT/DELETE `/api/users`, disable, group
  assignment via usergroup, paginated search (ILIKE across username/email/display, OR-composed);
  soft delete stays DB-triggered (`fn_soft_delete` archives to `deleted_records` — no app code).

## Tasks

- [x] `usergroup` store/service/handler — CRUD, add/remove members (tx + member-existence check →
      422), list by user.
- [x] `customclaim` store/service/handler — attach claims to users and groups, validation.
- [x] Admin request bodies validated via `pkg/validate` (ozzo rules per DTO `Validate()`).
- [x] `auditlog` list endpoint — filters + `pkg/responder` pagination contract.
- [x] `user` admin endpoints — POST/PUT/DELETE `/api/users`, group assignment (via usergroup),
      paginated list with search + sort.
- [x] Soft delete integration: covered by the DB `fn_soft_delete` trigger (verified live —
      DELETE archives into `deleted_records` without app code).
- [x] Tests: store + service + handler per feature; pagination determinism on the shared test
      container (unique data per run).
- [x] Yaak: folders "User Groups", "Custom Claims", "Audit Logs" (+ "Users" additions) —
      live-tested on :3080 (see progress log).

## Validation

- Three standard suites + lint + gofmt clean; new endpoints covered by handler tests with real
  Postgres.

## Progress Log

- 2026-09-12 Phase created (planned).
- 2026-09-12 Added `pkg/validate` request-validation task per plan update.
- 2026-09-12 Phase started.
- 2026-09-12 Landed: user admin CRUD (PUT/DELETE + pagination/search, soft delete via DB trigger),
  usergroup (CRUD + atomic membership + per-user listing), customclaim (user/group scopes +
  suggestions, ID ownership moved from federation), auditlog list API (filters + pagination +
  admin guard). Search filters use OR across columns (AND composition matched nothing).
  Validated: 3 suites + `-race` 0 FAIL, golangci-lint 0 issues, gofmt clean; two email templates
  fixed (unterminated Go template string literals broke `TestRealEmbeddedTemplatesRender`).
- 2026-09-12 Yaak live verification on :3080 (admin session): `GET /api/user-groups` 200 (Yaak),
  `GET /api/audit-logs?limit=5` 200 (Yaak), `GET /api/audit-logs/filters/users` 200 (Yaak),
  `GET /api/custom-claims/suggestions` 200 (Yaak), `GET /api/audit-logs` anonymous 401;
  via curl: POST groups 201 / dup 409 / PUT 200 / members 200 / groups-of-user 200; claims
  create 201 / update 200 / dup 409 / delete 200; users POST 201 / PUT 200 / DELETE 200 +
  paginated search 200. POST/PUT/DELETE bodies left empty in Yaak (MCP harness double-escapes
  nested JSON strings) — fill them in the UI when replaying.
