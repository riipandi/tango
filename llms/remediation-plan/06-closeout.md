---
status: done
updated: 2026-09-18
owner: tango-remediation
---

# Phase 6 — Final Acceptance and Closeout

Prerequisite: Phase 5 done and every failure fixed.

## Task 6.1 — Final repository audit

Run the final searches to prove there is no:

- legacy/fallback/dual-write reader;
- `LegacySecretID` or legacy client-secret comments;
- obsolete image column/table;
- Application Images request or supported-sounding description;
- LDAP runtime/config/dependency residue;
- responder import in services/stores;
- direct internal error disclosure;
- unverified `planned`/stale contract artifact.

Review the changes against `AGENTS.md`, every PRD acceptance criterion, and the porting-plan
final gate.

Commit: `docs: complete final remediation audit`

### Evidence (2026-09-18)

Final searches over `modules/`, `internal/`, `cmd/`, `database/`, `api/`:

- **Legacy/fallback/dual-write readers**: none. Remaining `fallback` matches are NULL-coalescing
  helpers (`orDefault`, `timeOf`) and test names, not legacy-format readers.
- **`LegacySecretID` / legacy client-secret comments**: zero matches.
- **Obsolete image columns**: no `image_type`/`dark_image`/`has_dark_logo` in Go code; `has_logo`
  survives only as a derived view field (from `logo_path`) in the OIDC client view and the
  apiaccess `ClientRef` projection — not a column.
- **Application Images artifacts**: only exclusion/deviation documentation and the bundled
  default avatar fallback (`internal/storage/bundled.go`, `web/embed.go`); no route, no Yaak
  request, no supported-sounding description.
- **LDAP residue**: zero matches in Go code, `api/specs/`, and `api/client/`.
- **Responder in services/stores**: enforced by the architecture tests from Task 3.3
  (`TestServiceAndStoreFilesStayTransportFree`, `TestUseCaseFilesStayTransportFree`).
- **Internal error disclosure**: two real leaks found and fixed in this audit —
  `modules/identity/devicelogin/handler.go` (inspect path appended `err.Error()`) and
  `modules/federation/oidc/cimd.go` (CIMD fetch/parse failures leaked transport detail into the
  422 message; now fixed generic messages, transport detail stays in logs). All other
  `err.Error()` sites are sentinel-guarded public-by-design messages; `nullableError` writes
  delivery-attempt logs (internal), the scimsync SQLSTATE check is internal.
- **Stale contract artifacts**: `api/specs/` (172 files) carries no LDAP/Application-Images
  requests; endpoint matrix stands at 129 done / 1 partial (documented deviation) / 0 planned /
  14 excluded.
- **AGENTS.md conformance review**: no legacy/compat branches added; migrations verbatim
  (owner-directed schema consolidation committed as its own change); typed IDs, `enc:` sealing,
  responder envelopes, and the SDK-sync rule held across all phase commits; comments follow the
  brevity rules.
- **PRD acceptance cross-check** (`llms/prd/08-verification.md` release criteria): matrix rows +
  test references + Yaak evidence current; deviations recorded; no LDAP/Application Images
  mounted; recovery/TOTP security cases tested; webhook signature/delivery observability
  verified; fresh-migration + contract tests green; full gates green (Task 5.2 evidence);
  fail-fast timeouts everywhere; no compatibility fixtures.

## Task 6.2 — Update plan and PRD status

Update the status metadata only when the evidence is complete:

- the remediation plan becomes `done`;
- a PRD whose acceptance criteria are all met may move from `draft` to `done`;
- the final gate records the command, date, environment, and actual results;
- unresolved items stay written as blockers/open decisions, never deleted.

Commit: `docs: close remediation acceptance`

## Completion criteria

- Every task in this plan has exactly one atomic commit.
- No agent-made change is pushed.
- Every PRD and porting-plan acceptance criterion is met or has a written owner decision.
- `task test`, `task lint`, `task check`, format, vet, race, fresh migration, the endpoint
  matrix, and Yaak verification all carry current evidence.
- The repository is ready for human review and the final commit recommendation goes to the owner.

## Final gate record (2026-09-18)

Environment: macOS (darwin/arm64), Go 1.27, Node >= 24, Docker 29.4.0 with the compose stack up
(pgsql, mailpit, redis, nginx, silo, upstream pocketid parity instance on :1411).

| Gate | Command | Result |
| --- | --- | --- |
| Go release suite | `go test -tags release -count=1 -failfast -timeout 1200s ./...` | 42 packages ok |
| Go debug suite | `go test -tags debug -count=1 -failfast -timeout 900s ./cmd/... ./database/...` | ok |
| Race suite | `go test -tags release -race -failfast` over webauthn, webhook, queue, recovery, scimsync, oidc, apikey, apiaccess | no races, ok |
| Fresh migration | `TestMigrationsLifecycle`, `TestFreshSchemaHasNoObsoleteTables`, contract suite | ok |
| Frontend | `pnpm exec vitest run` | 76/76 (9 files) |
| Lint | `task lint` | 0 issues |
| Check | `task check` | clean |
| Format | `gofmt -l`, oxfmt | clean |
| Vet | `go vet ./...` | clean |
| Typecheck | `task typecheck` (`tsc -b --noEmit`) | clean |
| Endpoint matrix | live diff tango (:3081, fresh DB) vs upstream (:1411) | statuses match; deviations recorded |
| Live Yaak contracts | curl re-send of every changed flow against the fresh DB | all pass; findings recorded |

## Commit inventory (this plan, branch `overhaul-stack`, unpushed)

Baseline `cacaa6355c11` → `e9d6f7a`, `c9c4efb` (phase 0) → `cd085ef`, `b52886c`, `e17a7e0`,
`e7fbfd7` (phase 1) → plan translation commit → `acfdeb4`, `bbc5182` (phase 2) → three phase-3
commits (recovery split, federation split, architecture tests) → three phase-4 commits
(validation, error disclosure, redaction) → `aa1ebe9` (contract test order fix),
`11988eb` (owner-directed schema consolidation) → `e2f2923`, `19c83b3`, `b3321df`, `d9c7760`
(phase 5) → `81cc019` (phase 6 audit) → this closeout commit.

## Open decision (stays open until the owner decides)

- **Setup endpoint session cookie** — `POST /api/signup/setup` issues a session token but does
  not set the cookie; the first admin has no password row, so API access requires the CLI
  bootstrap until decided. Options: set the cookie (upstream parity) or document CLI-only
  bootstrap. Evidence: `05-verification-gates.md` § Findings.
