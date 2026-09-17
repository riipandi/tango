---
status: draft
updated: 2026-09-17
owner: tango-remediation
---

# Phase 3 — Pemisahan Architecture Boundary

Prasyarat: Phase 1 dan Phase 2 selesai.

## Task 3.1 — Separate recovery HTTP handlers from use cases

Pisahkan `modules/identity/recovery` menjadi request/response handler dan service/use-case layer.
Service tidak boleh mengimpor `net/http`, chi, middleware, atau responder. Handler menerjemahkan
sentinel/typed errors ke envelope.

Pertahankan generic forgot-password response, token single-use, session invalidation, cookie
rotation, dan email queue behavior.

Tambahkan unit tests service tanpa HTTP serta handler tests untuk body, status, envelope, dan
cookies.

Commit: `refactor: separate recovery transport from service`

## Task 3.2 — Normalize federation transport boundaries

Audit `authorize`, `token`, `device`, `PAR`, `userinfo`, discovery, dan metadata code. Pisahkan
protocol handler dari service/store ketika file tersebut mencampur use case dan HTTP response
mapping.

Protocol exceptions tetap bare sesuai OIDC/OAuth; endpoint JSON biasa tetap memakai responder.
Jangan memindahkan responder ke store atau domain service.

Commit: `refactor: isolate federation protocol handlers`

## Task 3.3 — Add enforceable architecture checks

Perluas architecture tests untuk mendeteksi:

- responder/net/http/chi import pada service dan store files;
- concrete sibling-module imports di luar composition root;
- store files yang mengakses `Pool()` langsung;
- route registration di luar module route methods;
- duplicate method+path.

Aturan harus memiliki false-positive exception yang eksplisit hanya untuk protocol handler files.

Commit: `test: enforce application architecture boundaries`

## Acceptance criteria fase

- Domain/service package dapat diuji tanpa HTTP transport.
- Handler adalah satu-satunya layer yang memetakan domain error ke HTTP response.
- Architecture tests menangkap regression secara otomatis.
- Tidak ada module baru atau nested generic internal architecture.

