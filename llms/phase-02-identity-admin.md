---
status: planned
updated: 2026-09-12
---

# Phase 2 — Identity Admin Surface

User groups, custom claims, audit log API, and admin CRUD for users with pagination. Pocket ID
reference: `backend/internal/controller/{user,user_group,custom_claim,audit_log}_controller.go`.

## Goal

Admins manage users, group memberships, and custom claims in bulk; audit events are queryable
through the API.

## Deliverables

- `modules/identity/usergroup` — CRUD + membership (`user_groups`, `user_groups_users`); group
  checks used later by OIDC client access.
- `modules/identity/customclaim` (identity side) — user and group claims (`custom_claims`).
- `modules/auditlog` — paginated list API with filters (user, event, date range) on top of the
  existing store.
- `modules/identity/user` — admin CRUD completion: create/disable/delete (soft), group assignment,
  avatar reference; pagination via `pkg/responder`.

## Tasks

- [ ] `usergroup` store/service/handler — CRUD, add/remove members, list by user.
- [ ] `customclaim` store/service/handler — attach claims to users and groups, validation.
- [ ] `auditlog` list endpoint — filters + `pkg/responder` pagination contract.
- [ ] `user` admin endpoints — POST/PATCH/DELETE `/api/users`, group assignment, paginated list
      with search + sort.
- [ ] Soft delete integration: write `deleted_records` rows on user/group delete.
- [ ] Tests: store + service + handler per feature; pagination determinism on the shared test
      container (unique data per run).

## Validation

- Three standard suites + lint + gofmt clean; new endpoints covered by handler tests with real
  Postgres.

## Progress Log

- 2026-09-12 Phase created (planned).
