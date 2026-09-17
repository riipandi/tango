---
status: draft
updated: 2026-09-17
owner: tango-remediation
---

# Phase 2 — Excluded Feature dan Contract Artifact Cleanup

Prasyarat: Phase 0 selesai. Task schema yang berdampak pada contract boleh dilakukan setelah
Phase 1 selesai.

## Task 2.1 — Remove stale Application Images Yaak artifacts

Hapus folder dan request Application Images dari exported Yaak specs, atau tandai excluded bila
workspace memang memerlukan record tersebut. Preferensi target fresh implementation adalah
menghapus request yang tidak didukung.

Pastikan tidak ada saved body, expected response, atau folder description yang menyiratkan route
Application Images supported.

Commit: `chore: remove excluded application image requests`

## Task 2.2 — Remove LDAP artifact wording

Hapus referensi LDAP dari active code/config/spec descriptions yang bukan penjelasan exclusion.
Pertahankan hanya reference yang diperlukan untuk membuktikan route tidak mounted atau exclusion
di dokumen scope/deviation.

Pastikan tidak ada LDAP dependency, config key, compose service, fixture, schema column, atau
active Yaak request.

Commit: `chore: clean excluded ldap artifacts`

## Task 2.3 — Refresh endpoint reference and deviations

Sinkronkan endpoint reference dan deviations dengan runtime final:

- status `done`, `partial`, dan `excluded` harus akurat;
- request CIMD tidak boleh lagi berdeskripsi `planned` bila endpoint sudah selesai;
- setiap intentional deviation memiliki alasan, status, dan evidence;
- excluded endpoints tidak dihitung sebagai parity failure atau supported surface.

Commit: `docs: synchronize endpoint contract artifacts`

## Acceptance criteria fase

- Tidak ada Application Images request aktif atau stale.
- LDAP hanya muncul dalam dokumen exclusion/route-negative test yang diperlukan.
- Tidak ada endpoint `planned` yang sebenarnya sudah implemented.
- Setiap in-scope row memiliki test reference dan Yaak reference yang mutakhir.

