---
status: draft
updated: 2026-09-17
owner: tango-remediation
---

# Phase 4 — Validation dan Error Safety

Prasyarat: Phase 3 selesai.

## Task 4.1 — Normalize request validation

Inventarisasi pemeriksaan manual seperti `missing code`, `session_id is required`, multipart
field checks, dan invalid body parsing. Untuk endpoint JSON/form biasa, buat request DTO dengan
`Validate()` melalui `pkg/validate` dan petakan error ke 422 envelope.

Protocol-required parsing boleh tetap manual, tetapi harus memiliki status/error contract yang
terdokumentasi dan test invalid-input.

Commit: `refactor: normalize request validation`

## Task 4.2 — Stabilize public error mapping

Ganti pengiriman langsung `err.Error()` pada response publik dengan sentinel/typed error mapping.
Detail provider, database, crypto, dan implementation tidak boleh keluar melalui response.

Pertahankan error code/message yang memang bagian dari protocol contract, khususnya OAuth/OIDC.
Tambahkan tests yang memastikan internal wrapped error tidak terlihat oleh client.

Commit: `fix: prevent internal error disclosure`

## Task 4.3 — Review security and redaction boundaries

Audit response, logger, audit payload, Yaak body, dan test fixture untuk password, reset token,
TOTP seed/code, recovery code, API key, webhook secret, provider token, private key, dan
ciphertext.

Tambahkan regression tests untuk one-time secret response, redacted list/get response, log
redaction, dan audit payload.

Commit: `test: harden secret redaction boundaries`

## Acceptance criteria fase

- Request validation menggunakan `pkg/validate` pada endpoint non-protocol.
- Tidak ada internal error detail pada public response.
- Protocol errors tetap sesuai RFC dan endpoint contract.
- Tidak ada secret atau ciphertext pada log, audit, Yaak, atau response terlarang.

