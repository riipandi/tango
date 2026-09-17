---
status: draft
updated: 2026-09-17
owner: tango-remediation
---

# Remediation Plan

Plan ini menutup temuan audit terhadap PRD, porting plan, endpoint reference, schema final,
artefak Yaak, dan implementasi runtime tango.

Tujuan akhirnya adalah:

- tidak ada legacy reader, fallback reader, dual write, compatibility adapter, atau transitional
  schema;
- schema Postgres hanya berisi bentuk final yang dipakai runtime;
- Application Images dan LDAP tetap excluded tanpa residue yang menyesatkan;
- pemisahan handler, service, store, dan boundary module sesuai architecture requirements;
- error publik stabil dan tidak membocorkan detail internal;
- seluruh endpoint memiliki test, matrix, dan Yaak evidence yang mutakhir;
- full test, lint, format, vet, race, dan live verification dapat dibuktikan.

## Source of truth

Implementasi harus mengikuti, dalam urutan prioritas:

1. `AGENTS.md`;
2. `llms/prd/`;
3. `llms/porting-plan/`;
4. `llms/endpoint-reference.md`;
5. `llms/tango-deviations.md`;
6. source upstream Pocket ID checkout lokal.

`llms/archived/` hanya konteks historis dan bukan bukti completion.

## Atomic commit rule

Setiap task bernomor adalah satu atomic commit. Agen wajib:

- mengerjakan hanya satu task per commit;
- menyertakan perubahan source, migration, test, matrix, dan Yaak yang menjadi acceptance task
  tersebut dalam commit yang sama;
- menjalankan validasi task sebelum commit;
- tidak menggabungkan dua task atau dua fase ke satu commit;
- tidak membuat commit kosong atau commit progress;
- menggunakan commit message yang tercantum di task;
- tidak melakukan push.

Jika task menemukan perilaku upstream yang ambigu, agen harus berhenti pada task tersebut,
mencatat evidence, dan meminta keputusan owner sebelum mengubah implementation atau contract.

## Urutan fase

1. [Baseline dan inventory](./00-baseline.md)
2. [Final schema dan penghapusan compatibility](./01-final-schema.md)
3. [Cleanup excluded feature dan artefak Yaak](./02-excluded-and-contracts.md)
4. [Pemisahan architecture boundary](./03-architecture-boundaries.md)
5. [Validation dan error safety](./04-validation-and-errors.md)
6. [Verification runtime dan live contract](./05-verification-gates.md)
7. [Final acceptance dan closeout](./06-closeout.md)

Fase berikutnya hanya dimulai setelah acceptance criteria fase sebelumnya terpenuhi dan seluruh
task fase sebelumnya sudah memiliki atomic commit.

