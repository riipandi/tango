---
status: planned
updated: 2026-09-12
---

# Phase 8 — Directory Sync & Storage

LDAP sync, SCIM provisioning, S3 storage, app images. Optional tier of the port. Pocket ID
reference: `backend/internal/ldapsync/`, `scimsync/`, `storage/`, `app_images_service.go`,
`geolite/`.

## Goal

Enterprise directory integration and blob storage parity with Pocket ID.

## Deliverables

- `modules/identity/ldapsync` — periodic LDAP import into `users`/`user_groups` (attribute map,
  filter, sync schedule), driven by the recurring job from Phase 7; config from env
  (`LDAP_*` keys like Pocket ID).
- `modules/federation/scimsync` — SCIM 2.0 server for OIDC clients: `/api/scim/v2/Users` +
  `Groups` (filtered by client grants), bearer token = client credentials; tables
  `scim_service_providers`.
- `internal/storage` — S3 backend via `aws-sdk-go-v2` (MinIO-compatible endpoints, path-style);
  FS backend already exists; picker in registry (`STORAGE_*` config).
- `modules/appimage` — logo/background/favicon/email images with `file_stores` rows, served from
  `/api/application-images/*` with cache headers; resize deferred.
- Optional: geolite mirror downloads + audit log IP enrichment.

## Tasks

- [ ] S3 `Store` implementation — Put/Open/Delete + `ErrNotFound` mapping; integration test
      against MinIO testcontainer (or skipped without docker socket).
- [ ] `appimage` store/handler — upload (size/type limits), serve with `Cache-Control`, default
      assets fallback.
- [ ] `ldapsync` — connector via `github.com/go-ldap/ldap/v3`, attribute mapping config, dry-run
      mode, sync job wiring; create/disable users + group membership.
- [ ] `scimsync` — user/group provisioning endpoints, grant-scoped auth, pagination; mirror
      changes into identity stores (event → auditlog).
- [ ] Config keys: `LDAP_*`, `SCIM_*`, `STORAGE_*` verified against `.env.example`.
- [ ] Tests: sync idempotence (re-run = no-op), SCIM filter minimal support, storage round trip.

## Validation

- Three standard suites + lint + gofmt clean; LDAP/SCIM flows covered by service-level tests
  (in-memory LDAP mock or interface fakes — stores still real Postgres).

## Progress Log

- 2026-09-12 Phase created (planned).
