# Architecture Improvement Plan

Follow-up of the architecture deep analysis (2026-09-15): remove cross-module code duplication
and straighten out module placement/separation. The port is complete (112/113 parity + 1 recorded
non-goal), so these phases only re-shape existing behavior — no endpoint, envelope, or schema
changes. Read `AGENTS.md` and `llms/tango-deviations.md` first.

## Ground Rules (all arch phases)

- **Behavior-preserving refactors only.** No endpoint moves, no JSON shape changes, no DB schema
  changes (exception: none planned; token consolidation reuses the existing `auth_tokens` table).
- Validation gate for every task: `task test` (all three suites), `task lint`, `task check`.
  A checkbox may only be ticked when its validation command passes.
- No Yaak verification required (no HTTP behavior change) — unless a phase task touches route
  mounting, then spot-check the affected routes with curl against the running server.
- Update `status:` (`planned` → `in_progress` → `done`) and `updated:` in front matter; append one
  line to the Progress Log per task group; update the index below.
- Keep phases strictly sequential: each phase builds on the helpers/contracts of the previous one.

## Phase Index

| Phase | File                                                        | Scope                                                                  | Status  | Updated    |
| ----- | ----------------------------------------------------------- | ---------------------------------------------------------------------- | ------- | ---------- |
| 1     | [arch-phase-01-mechanical-dedup.md](./arch-phase-01-mechanical-dedup.md) | Shared pgx helpers, ErrNoRows mapping, constant fixes, antree relocation | done    | 2026-09-15 |
| 2     | [arch-phase-02-error-mapping.md](./arch-phase-02-error-mapping.md)       | Central error→HTTP mapping, remove per-handler `writeError` copies      | done    | 2026-09-15 |
| 3     | [arch-phase-03-kernel-guards.md](./arch-phase-03-kernel-guards.md)       | `kernel.Guard` + `kernel.Authenticator`, remove guard option duplicates | done    | 2026-09-15 |
| 4     | [arch-phase-04-module-layout.md](./arch-phase-04-module-layout.md)       | Standard module layout, settings mapping back into modules, slim registry | done    | 2026-09-15 |
| 5     | [arch-phase-05-token-store.md](./arch-phase-05-token-store.md)           | Consolidate duplicated `auth_tokens` stores into one token package      | planned | 2026-09-15 |
| 6     | [arch-phase-06-oidc-split.md](./arch-phase-06-oidc-split.md)             | Decompose the 4k-LOC `federation/oidc` package per bounded context      | planned | 2026-09-15 |

## Expected Outcome

- ~1.000–1.500 LOC of duplicated helpers, guards, and error switches removed.
- Modules depend only on `internal/kernel` contracts, not on `internal/transport/middleware`.
- `internal/registry` becomes pure assembly (no per-module settings mapping logic).
- One store for the shared `auth_tokens` domain; `federation/oidc` split into reviewable subpackages.
- Module files follow one fixed layout: `schema.go`, `store.go`, `service.go`, `handler.go`, `module.go`.

## Progress Log

- 2026-09-15 Plan created from architecture analysis findings (no code changes yet).
- 2026-09-15 Phase 1 done: shared pgx/error helpers in `internal/datastore` (conv.go), antree
  relocated to `internal/`. Go gates green (default 572 pass, debug 35 pass, release 0 fail,
  golangci-lint 0 issues); JS-side `test:ui`/oxlint failures are pre-existing environment gaps
  (no api-client test files; `oxlint-tsgolint` undeclared).
- 2026-09-15 Phase 2 done: `responder.StatusedError` + `WriteError`; 7 `writeError` copies
  removed; 21 sentinels across 8 modules now carry their HTTP status. Live curl checks pass.
  New finding for phase 3: the machine-auth mount on `/api/users` shadows the admin mount
  (pre-existing, identical on baseline).
- 2026-09-15 Phase 3 done: `kernel.Guard`/`Guarded`/`Authenticator`/`Principal` contracts;
  middleware aliases; 5 `RouteGuard` types removed. Two pre-existing guard bugs fixed and live-
  verified: `/api/users` machine mount shadowed the admin mount (admin sessions 401'd), and
  sync-ldap was anonymous-reachable (nil guard never wired, fail-open).
- 2026-09-15 Phase 4 done: registry settings mapping + SCIM snapshot SQL moved into owning
  packages (ldapsync/mailer/appconfig/scimsync); `Feature` strays out of schema.go; registry
  features.go 624 → 393 lines (pure assembly); AGENTS.md config-key convention synced.
