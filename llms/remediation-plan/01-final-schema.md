---
status: draft
updated: 2026-09-17
owner: tango-remediation
---

# Phase 1 — Final Schema dan Penghapusan Compatibility

Prasyarat: Phase 0 selesai dan inventory disetujui.

## Task 1.1 — Pilih bentuk final client secret

Tetapkan satu bentuk final untuk client secrets. Bentuk final harus mendukung secret metadata,
hash-only comparison, expiry, active state, create-once response, rotation, dan deletion tanpa
membutuhkan kolom legacy.

Update schema types, store queries, create/verify/rotate/delete flows, dan tests agar hanya bentuk
final yang digunakan. Hapus `LegacySecretID`, synthetic legacy entry, fallback comparison, dan
komentar yang mengklaim kompatibilitas lama.

Tambahkan tests untuk create, multi-secret, expiry, inactive secret, rotation, deletion, dan
client authentication setelah kolom legacy tidak lagi dibaca.

Commit: `refactor: finalize oidc client secret storage`

## Task 1.2 — Hapus kolom client-secret dan image legacy

Buat migration baru; jangan mengedit migration applied. Hapus kolom yang tidak termasuk schema
final, minimal:

- `oidc_clients.secret` jika Task 1.1 sudah memindahkan seluruh pemakaian;
- `oidc_clients.image_type`;
- `oidc_clients.dark_image_type`.

Sesuaikan `SELECT`, `INSERT`, `UPDATE`, scanner, metadata view, API access view, dan tests. Logo
client tetap dipertahankan melalui `logo_path` bila itu memang fitur in-scope.

Migration harus aman pada fresh database dan pada database yang sudah memiliki bentuk sebelumnya.
Tambahkan schema contract test yang memastikan kolom obsolete tidak ada.

Commit: `db: remove obsolete client columns`

## Task 1.3 — Hapus tabel refresh-token yang tidak dipakai

Pastikan seluruh runtime OIDC menggunakan storage final yang dipilih. Jika `oidc_refresh_tokens`
tidak memiliki caller aktif, hapus tabel dan index-nya melalui migration baru, lalu perbarui
migrator count/assertions, schema contract, backup tests, dan database reference bila diperlukan.

Jika ditemukan caller aktif, dokumentasikan caller dan buktikan mengapa tabel tersebut adalah
bagian final schema sebelum mengubahnya.

Commit: `db: remove unused oidc refresh token table`

## Task 1.4 — Enforce final encrypted-value storage

Audit semua recoverable secret: TOTP, webhook, SCIM, JWKS/private material, dan settings sensitif.
Pastikan seluruh write memakai `pkg/crypto.Cipher.Encrypt`, seluruh read memakai `Decrypt`, dan
semua known encrypted columns memiliki marker check yang sesuai.

Tambahkan real-Postgres tests untuk malformed prefix, missing prefix, wrong key, tampering, dan
redaction. Hash-only values tetap hash dan tidak boleh dipindah ke encryption.

Commit: `test: enforce final encrypted value storage`

## Acceptance criteria fase

- Tidak ada `legacy`, fallback reader, dual write, atau compatibility adapter pada client secret.
- Fresh schema dan migration upgrade menghasilkan schema final yang sama.
- Tidak ada kolom atau tabel obsolete yang tidak memiliki caller final.
- Semua recoverable secrets memakai format `enc:`.
- Migration version/count assertions sudah diperbarui.

