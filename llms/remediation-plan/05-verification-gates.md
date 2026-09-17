---
status: draft
updated: 2026-09-17
owner: tango-remediation
---

# Phase 5 — Verification Runtime dan Live Contract

Prasyarat: semua code/schema/contract task fase sebelumnya selesai.

## Task 5.1 — Repair focused test execution environment

Pastikan test dapat dijalankan pada environment yang mendukung:

- Docker daemon untuk testcontainers Postgres/Mailpit;
- listener HTTP untuk `httptest`;
- explicit timeout dan fail-fast behavior.

Jangan mengubah test menjadi skip hanya untuk membuat gate hijau. Jika sandbox tidak mendukung,
jalankan pada host/devbox yang sesuai dan simpan command serta hasilnya.

Commit: `test: make remediation verification reproducible`

## Task 5.2 — Run database and race verification

Jalankan dengan timeout eksplisit:

- fresh migration up/down/up;
- schema contract dan migration tests;
- seluruh Go release/debug suites;
- race test untuk identity MFA, webhook, queue, dan store yang diubah;
- `go vet`, `gofmt`, `task lint`, `task check`, dan typecheck.

Perbaiki failure source satu per satu; setiap fix adalah atomic commit terpisah.

Commit: `test: verify database and race gates`

## Task 5.3 — Re-run endpoint matrix

Bandingkan tango dengan local upstream Pocket ID v2.14.0 untuk setiap in-scope endpoint:

- method/path;
- query/path parameters;
- request encoding;
- auth boundary dan headers;
- status, headers, envelope, bare response, dan error fields;
- pagination dan TypeID behavior.

Catat hanya intentional deviation di `llms/tango-deviations.md`. Jangan menyesuaikan matrix untuk
menutupi defect tanpa memperbaiki implementation atau mendapat keputusan owner.

Commit: `docs: refresh endpoint parity evidence`

## Task 5.4 — Re-send Yaak live verification

Gunakan clean cookie jar dan fresh Postgres. Re-send seluruh request yang berubah, minimum:

- password/recovery;
- TOTP enrollment/confirm/verify/recovery/disable;
- OIDC client secret lifecycle;
- device/PAR/token/userinfo;
- SCIM/JWKS;
- webhook CRUD/rotation/test/delivery/retry;
- excluded route negative checks.

Simpan nama request, observed status, header differences, dan tanggal verifikasi. Jangan menyimpan
secret, token, recovery code, seed, atau ciphertext.

Commit: `test: record live remediation verification`

## Acceptance criteria fase

- Full gates benar-benar berjalan pada environment yang sesuai.
- Fresh database hanya berisi final schema.
- Race test dan security cases lulus.
- Semua in-scope endpoint memiliki evidence matrix dan Yaak terbaru.
- Tidak ada stale request atau undocumented deviation.

