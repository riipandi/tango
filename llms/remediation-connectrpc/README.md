---
status: planned
updated: 2026-09-19
owner: tango-connectrpc-remediation
---

# ConnectRPC Remediation Plan

This plan closes the findings of the post-implementation audit of
[`llms/connectrpc-plan/`](../connectrpc-plan/README.md) and
[`docs/api-endpoint.md`](../../docs/api-endpoint.md). The refactor shipped and every gate is green;
what remains is contract drift between the documents and the wire, one authorization gap wider than
documented, one false transport claim, and stale residue.

The audit ran at commit `fe5baca` on branch `refactor-connectrpc`; commit `4a095a6` landed during
the audit and is the current `HEAD`. Each finding records the method used to verify it, so a
reviewer can re-check it without repeating the audit.

## End state

- The documented `/rpc` surface, the protobuf contract, and the mounted handlers agree exactly.
- The HTTP method column in every endpoint document matches the transport.
- The `X-API-KEY` credential reaches only the surfaces the documents claim, in code and in the Yaak
  harness.
- Anonymous procedures are anonymous in code, not only in the documents.
- No credential literal is tracked under `api/specs/`.
- The Yaak collection provisions its own tokens and identifiers, so its evidence is reproducible.
- Every transport claim in the plan is either verified by a test or deleted.
- No unused exported middleware, no empty package file, no comment referring to a deleted route.
- `AGENTS.md` describes the ConnectRPC era instead of the REST-only era.

## Source of truth

In priority order:

1. `AGENTS.md`;
2. `llms/connectrpc-plan/endpoint-reference.md` (transport decision record and service matrix);
3. `docs/api-endpoint.md` and `llms/endpoint-reference.md` (row-by-row contract);
4. `api/connect/*.proto` (wire contract);
5. `internal/registry/rpc_inventory_test.go` (what the composition root actually mounts).

`llms/connectrpc-plan/*.md` phase notes are historical records. Where a phase note contradicts
the code, the code wins and the note is corrected in phase 04 of this plan.

## Findings index

Severity: **P0** = security or contract violation (including a committed secret), **P1** =
documentation contradicts behavior, **P2** = residue or hygiene.

| ID | Severity | Finding | Task |
| --- | --- | --- | --- |
| F1 | P0 | `X-API-KEY` reaches self-service and credential-lifecycle procedures far beyond the documented "admin application API" — including `AccountService.ChangePassword`, which rotates the key owner's password | 01.1 — **resolved** (Option A, 2026-09-19) |
| F2 | P1 | `ApplicationConfigurationService.Get` is documented anonymous in three files but guarded as a `self` procedure; anonymous callers get 401 | 01.2 — **resolved** (2026-09-19) |
| F3 | P1 | `docs/api-endpoint.md` and `llms/endpoint-reference.md` list `GET`/`PUT`/`DELETE` for 10 `/rpc` procedures; the transport is POST-only and answers 405 | 02.1 — **resolved** (2026-09-19) |
| F4 | P1 | `llms/connectrpc-plan/03-server.md` claims gRPC and gRPC-Web "are not mounted and answer not_found"; the generated handlers serve both | 02.2 — **resolved** (Option B, 2026-09-19) |
| F5 | P1 | 3 procedures exist in the proto with no document row and no Yaak request: `AccountService.UpdateAccount`, `AuthService.ForgotPassword`, `AuthService.ResetPassword` | 02.3 — **resolved** (2026-09-19) |
| F6 | P1 | The `429` response on `/rpc` is the REST envelope, not a Connect error document; `rpcerr.ResourceExhausted` has no call site | 02.4 — **resolved** (2026-09-19) |
| F7 | P1 | Cross-origin `/rpc` preflight cannot send `Authorization`, `X-API-KEY`, or `Connect-Timeout-Ms` | 02.5 — **resolved** (both layers, 2026-09-19) |
| F8 | P2 | `middleware.ResolveRPCPrincipal` and `middleware.RPCAdminProcedureGuard` have no caller | 03.1 — **resolved** (`6c2c049`) |
| F9 | P2 | `modules/identity/account/store.go` and `modules/identity/password/handler.go` contain only a package clause | 03.2 — **resolved** (`9405f80`) |
| F10 | P2 | Comments cite the deleted `/api/version/latest` route | 03.3 — **resolved** (`14f3e8b`) |
| F11 | P2 | Plan documents carry stale claims: generated Go "committed", `protoc-gen-connect-es`, the removed Yaak folder `[ConnectRPC] System (smoke)`, and the unresolved A1–A5 block | 04.1 — **resolved** (`1371953`) |
| F12 | P2 | `AGENTS.md` has zero ConnectRPC coverage and still points at `llms/phase-*.md`, which does not exist | 04.2 — **resolved** (`6bcc4af`) |
| F13 | P2 | Pagination shape is inconsistent: some list RPCs take `common.v1.PageRequest` directly, others embed it | 05.1 — **resolved** (`6ca011c`); scope widened to the REST/RPC metadata parity below |
| F14 | P1 | Tracked Yaak requests carry a plaintext password at `HEAD` | 05.2 — **resolved** (`a0fea7c` env references, `723c1d3` guard) |
| F15 | P2 | `llms/connectrpc-plan/` is marked `status: done` while its own completion criteria are unmet (no SPA, so the caller-migration criterion cannot pass) | 04.1 — **resolved**: criteria rewritten, the two out-of-scope ones named |
| F16 | P1 | The rate limiter was silently disabled on **every** policy: `rateKey` embedded the policy name verbatim (`forgot-password`), and the `rate_limits` key check only accepts `[a-z0-9_:]`, so each insert raised `23514` and the middleware failed open | 02.6 — **resolved** (`f5eb5ee`) |
| Y1 | P0 | The Yaak workspace header `X-API-KEY: ${[ apiKey ]}` was inherited by every request, so "anonymous" evidence carried a machine credential | 06.1 — **resolved**: the owner removed the workspace header during the audit; the credential is now scoped to its two requests |
| Y2 | P1 | No Yaak request obtains a real token; `accessToken` is `dummy`, so all 110 protected requests answer 401 | 06.2 |
| Y3 | P1 | No Yaak auth type is configured: 130 requests `null`, 23 `none`, zero folders or workspaces set one | 06.3 |
| Y4 | P1 | Neither CORS layer (nginx `compose.yaml:209`, Go `middleware/cors.go`) allows `X-API-KEY` or `Connect-Timeout-Ms` | 06.4 — **resolved** by 02.5 (`40e4ae6`, `23b2bcb`) |
| Y5 | P2 | `refreshToken` is defined in every Yaak environment and referenced by no request | 06.5 — **resolved** (`a0fea7c`) |
| Y6 | P2 | ~40 `REPLACE_*` placeholders are typed by hand where `uuid.v7()`, `faker.*`, and `response.body.path()` apply | 06.5 |
| Y7 | P2 | Zero Yaak template functions are used anywhere in the collection | 06.5 |

## Finding F17 — the RPC response shape did not mirror the REST envelope

Added during phase 05, at the owner's request. The RPC surface answered a `common.v1.PageMetadata`
block that shared nothing with the REST envelope, and the pagination rules lived in two places with
different behaviour. Three defects came out of that review:

- **`limit: 0` returned every row.** Stores guard with `if params.Limit > 0 { Limit(...) }`, so a
  proto3 default of 0 meant "no LIMIT". REST treats a zero limit as invalid and falls back to 25.
- **`rpcerr.PageMetadata` was one-based** while `responder.NewPagination` is zero-based, and both
  sides had a test pinning its own convention.
- **`sort_by`/`sort_order` do not work on either transport.** `PaginationParams.SortBy`/`SortOrder`
  have no production caller and no store builds an `ORDER BY` from input, so the fields were not
  copied into the proto.

Resolved in `6ca011c` by defining the rules once in `pkg/responder`, giving
`common.v1.ResponseMetadata` the same fields as the REST metadata block, and applying the list-shape
rule from task 05.1.

## Baseline state

Audited at commit `fe5baca` on branch `refactor-connectrpc`. Two state changes landed while the
audit ran:

- `4a095a6` committed the Yaak export churn (131 files) and the plaintext sign-in credential, so
  finding F14 is now a `HEAD` state rather than a working-tree state.
- The Yaak workspace header `X-API-KEY` was removed, resolving finding Y1's root cause. Task 06.1
  now keeps that state rather than producing it.

The gates passed at `fe5baca`:

| Command | Result |
| --- | --- |
| `task test:go` | 589 tests pass |
| `go test -tags release ./...` | 587 tests pass |
| `pnpm exec vitest run` | 43 tests pass, 7 files |
| `task lint` | 0 issues |
| `task check` | clean |
| `task typecheck` | clean |
| `task rpc:lint`, `task rpc:breaking`, `task rpc:stale` | clean |

Nothing in this plan is a build or test failure; every finding is contract drift, residue, or a
documentation mismatch.

The audit verified findings F1, F2, F3, F4, F6, F7, Y1, Y2, and Y4 by temporary probe tests and
live Yaak sends against the running stack. The probes were deleted after the audit and must be
recreated as the permanent assertions named in each task.

## Scope note

This plan covers the audit findings only. It does not re-open the ConnectRPC refactor's design
decisions, and it does not add features. Two known non-goals stay out of scope:

- The SPA does not exist yet (`index.html:97` still comments out the app entry), so the
  "first-party callers use the Connect client" criterion cannot be verified here.
- `api/client` is REST-only by decision; this plan does not extend it to cover RPCs.

## Working rules

- One task per atomic commit, using the commit message stated in the task.
- Every task ships its implementation, tests, matrix rows, and Yaak changes in the same commit.
- Yaak requests are created and updated through the Yaak MCP integration only. Never edit
  `api/specs/*.yaml` by hand; the export is one-way.
- The Yaak workspace is live state, not branch state: requests created or deleted through MCP
  persist across `git checkout`. Re-export before relying on the files on disk.
- Workspace-level Yaak headers and auth are inherited by every request. Set a credential at the
  narrowest scope that needs it, and use an explicit **No Auth** on rows that must stay anonymous.
- A task that changes a proto message runs `task rpc:generate` before the test gate.
- A task that changes a handler response or status updates the matching `api/client` schema and
  test in the same commit when the surface is a retained REST route.
- Do not push. Do not commit files this plan did not change.
- When a task needs a product decision rather than a code fix, stop, record the evidence, and ask
  the owner.

## Blocking decisions

One task cannot start until the owner decides:

1. **Task 02.2** — whether to close the gRPC and gRPC-Web transports or document them (keep option
   recommended).

Task 01.1 was decided on 2026-09-19: **Option A (narrow)**, recorded in
[`01-authorization.md`](./01-authorization.md).

## Progress

| Task | Finding | State |
| --- | --- | --- |
| 01.1 | F1 | done — committed `a0e633b` |
| 01.2 | F2 | done — committed `19112f3` |
| 02.1 | F3 | done — committed `7393b1e` |
| 02.2 | F4 | done — committed `f125111` |
| 02.3 | F5 | done — committed `6635cb0` |
| 02.4 | F6 | done — committed `689b7cd` |
| 02.5 | F7 | done |
| 02.6 | F16 | done — committed `f5eb5ee` |
| 02.7 | Y4 | done with 02.5 |
| 03.1 | F8 | done — committed `6c2c049` |
| 03.2 | F9 | done — committed `9405f80` |
| 03.3 | F10 | done — committed `14f3e8b` |
| 04.1 | F11, F15 | done — committed `1371953` |
| 04.2 | F12 | done — committed `6bcc4af` |
| 05.1–06.5 | F13, F14, Y1–Y3, Y5–Y7 | not started |

## Phase index

1. [Authorization and contract correctness](./01-authorization.md) — F1, F2
2. [Transport and error contract](./02-transport-contract.md) — F3, F4, F5, F6, F7
3. [Dead code and residue](./03-residue.md) — F8, F9, F10
4. [Documentation truth](./04-documentation.md) — F11, F12, F15
5. [Consistency and closeout](./05-consistency.md) — F13, F14
6. [Yaak test harness](./06-yaak-testing.md) — Y1–Y7

Ordering notes:

- **Phase 06 first.** The harness is the evidence source for every other phase. Until Y2 is fixed,
  no protected Yaak result in this plan is trustworthy: every protected request sends the literal
  `dummy` token. Y1's root cause is already resolved.
- Task 05.2 (the committed credential) has no dependency on phases 01–04 and overlaps task 06.2
  step 4. Do them together.
- Task 02.5 and task 06.4 edit the same Go file. Do them as one commit.
- Phases 01–05 are a reading order, not a strict scheduling constraint.

## Gate

`task test`, `task lint`, `task check`, `task typecheck`, `task rpc:lint`, `task rpc:breaking`, and
`task rpc:stale` pass after every task. Phase 01 additionally requires a probe test proving the
machine-credential boundary, and phase 02 requires a probe test proving the method and transport
claims.
