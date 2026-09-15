---
status: active
updated: 2026-09-16
---

# Tango Deviations from Upstream Pocket ID

Deliberate contract differences from upstream Pocket ID v2.14.0. Anything not listed here must
match the upstream endpoint contract.

## Excluded upstream features

- **LDAP directory synchronization** — no LDAP config surface, no sync endpoint, no runtime
  wiring, and no directory-key columns. Do not reintroduce LDAP settings or clients.
- **Application Images management** — the `/api/application-images/*` endpoints are not mounted
  and their Yaak requests were removed. The bundled default profile picture is still served
  through the user profile-picture fallback; OIDC client logos use the shared blob store
  directly.
- **SQLite and MySQL** — Postgres is the only supported database.
