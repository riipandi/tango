---
status: draft
updated: 2026-09-17
owner: tango-remediation
---

# Phase 0 — Baseline dan Inventory

## Tujuan

Membuat daftar final yang dapat diaudit sebelum perubahan source atau schema dilakukan.

## Task 0.1 — Capture baseline

Catat hasil repository saat mulai:

- `git status --short` dan commit dasar;
- jumlah endpoint in-scope, excluded, dan partial pada endpoint reference;
- seluruh `Encrypt`/`Decrypt` consumer;
- seluruh route mount;
- seluruh table dan kolom yang dimiliki migration;
- hasil `gofmt -l`, `go vet`, focused tests, frontend tests, dan full gate;
- status Docker, Postgres, server tango, upstream parity instance, dan Yaak workspace.

Simpan hasil sebagai evidence di dokumen remediation atau update matrix yang relevan. Jangan
mengubah perilaku runtime pada task ini.

Validasi: semua command memakai timeout eksplisit; kegagalan environment dicatat terpisah dari
failure source.

Commit: `docs: capture remediation baseline`

## Task 0.2 — Inventory final-shape violations

Buat tabel inventory dengan kolom `finding`, `owner`, `source`, `required final shape`, `migration
impact`, `test impact`, dan `Yaak impact`. Minimum mencakup:

- `oidc_clients.secret` dan `credentials`/multi-secret shape;
- `LegacySecretID` dan secret fallback;
- `oidc_clients.image_type` dan `dark_image_type`;
- `oidc_refresh_tokens` yang tidak digunakan runtime;
- Application Images Yaak folder/request;
- stale `planned` request description;
- responder imports pada service/module files;
- ad-hoc request validation;
- public `err.Error()` responses;
- missing live evidence.

Jangan menyelesaikan finding pada task ini; hasil inventory menjadi input fase berikutnya.

Commit: `docs: inventory final-shape violations`

