---
status: done
updated: 2026-09-14
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
- `modules/federation/scimsync` — SCIM 2.0 **outbound provisioning client** (upstream v2.14
  semantics): `scim_service_providers` rows bind an OIDC client to a remote SCIM endpoint +
  bearer token; a sync pushes the client's allowed users/groups to the remote provider
  (create/update/delete via SCIM REST). NOT a SCIM server — `/api/scim/v2/*` never existed
  upstream.
- `internal/storage` — S3 backend via `aws-sdk-go-v2` (MinIO-compatible endpoints, path-style);
  FS backend already exists; picker in registry (`STORAGE_*` config).
- `modules/appimage` — logo/background/favicon/email images with `file_stores` rows, served from
  `/api/application-images/*` with cache headers; resize deferred.
- Optional: geolite mirror downloads + audit log IP enrichment.

## Tasks

- [x] `internal/storage` — S3 backend via `aws-sdk-go-v2` (MinIO-compatible endpoints,
      path-style, checksum mode relaxed), filesystem backend rewritten on `os.Root` (temp+rename
      atomic writes, recursive List matching the S3 contract), registry picker
      (`STORAGE_S3_ENDPOINT_URL` set → S3, else FS under `STORAGE_DATA_DIR`); contract tests +
      picker test.
- [x] `modules/appimage` — logo light/dark, email logo, background, favicon, default profile
      picture; upload (5 MiB cap, MIME allowlist), serve with `Cache-Control`
      (15 min / stale-while-revalidate 1 day, `skipCache=1` bypass), bundled defaults seeded
      from the Vite output dir (`web/output/images`), delete tombstones so re-seed does not
      resurrect.
- [x] `modules/identity/ldapsync` — desired-state reconciler via `go-ldap/ldap/v3` (fetch users
      + groups with member resolution: DN cache → DN property → bare uid → base-object lookup),
      one-tx create/update/disable(hard/soft)/delete, admin group flag, hourly ticker + manual
      trigger endpoint (`POST /application-configuration/sync-ldap`), env-backed
      `LDAPSettings` until appconfig lands.
- [x] `modules/federation/scimsync` — SCIM 2.0 outbound provisioning client: service-provider
      CRUD (`/api/scim/service-provider*`, `/api/oidc/clients/{id}/scim-service-provider`),
      tokens encrypted at rest (same cipher derivation as JWKS), unique index per client,
      outbound sync (list remote → create/update/delete, 429-aware retry with `Retry-After`),
      snapshot scoped by the client's group allowlist.
- [x] Config keys: `LDAP_*` section bound in config + documented in `.env.example`
      (env-example sync test enforces coverage); `STORAGE_DATA_DIR` added.
- [x] Tests: ldapsync lifecycle (create/idempotence/hard+soft delete/group membership),
      scimsync provider lifecycle (token round-trip, duplicate refusal, unknown client,
      MarkSynced), appimage lifecycle (upload/replace/delete-tombstone/re-seed skip),
      storage round trip + path-escape rejection.
- [x] Yaak: folders "Application Configuration", "Application Images", "SCIM", "Storage" —
      live-tested (sync-ldap 200 with stats, favicon 200 + cache headers, upload 204,
      unauthenticated upload 401, SCIM create 201 / by-client 200 / update 200 / delete 204 /
      sync 502 against unreachable remote).

## Validation

- Three standard suites + lint + gofmt clean; LDAP/SCIM flows covered by service-level tests
  (interface fakes for the LDAP client — stores still real Postgres).
- Live: LDAP sync reconciled 4 users + 3 groups from glauth, idempotent re-run (0 created,
  0 deleted); usernames sanitized to tango rules (`ada.wong` → `ada_wong`).

## Progress Log

- 2026-09-12 Phase created (planned).
- 2026-09-14 Phase started. Survey via local upstream clone (v2.14.0): ldapsync is a
  desired-state reconciler (fetch LDAP users+groups → diff against DB → create/update/
  disable/delete in one tx), driven by appconfig keys (LDAP_ENABLED, LDAP_URL, LDAP_BIND_DN,
  LDAP_BASE, LDAP__*ATTRIBUTE*_, LDAP_ADMIN_GROUP_NAME, LDAP_SOFT_DELETE_USERS). DB columns
  `users.ldap_id` / `user_groups.ldap_id` already exist in migration 00004. glauth LDAP dev
  server with postgres plugin backend landed separately (commit 456eb15) for live testing.
- 2026-09-14 Plan correction: upstream scimsync is an outbound push client (scim_service_
  providers + sync push), not a SCIM server — `/api/scim/v2/*` never existed upstream.
  Deliverables rewritten accordingly.
- 2026-09-14 internal/storage landed (FS on os.Root + S3 via aws-sdk-go-v2, picker by
  endpoint config); appimage landed (blob-store-backed images, Vite-output seeding, delete
  tombstones); /static/* now serves real files from the Vite output instead of a placeholder.
- 2026-09-14 ldapsync landed (reconciler + go-ldap fetch + hourly ticker + sync-ldap endpoint,
  env-backed settings); live-verified against glauth: 4 users + 3 groups reconciled,
  idempotent re-run. Username sanitization maps LDAP names onto tango's username rules.
- 2026-09-14 scimsync landed (provider CRUD + encrypted tokens + unique-per-client index in
  migration 00006 + outbound sync with 429 retry); live-verified via Yaak (create/by-client/
  update/delete; sync 502 against an unreachable remote as expected).
- 2026-09-14 Full gate: 3 suites 0 FAIL, -race clean on changed packages, golangci-lint
  0 issues, gofmt clean. Yaak folders created (10 requests).
