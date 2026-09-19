---
status: planned
updated: 2026-09-19
owner: tango-connectrpc-remediation
---

# Phase 04 — Documentation Truth

Prerequisite: phase 03 done. Phase 02 corrected the endpoint documents; this phase corrects the
plan record and the repository instructions.

## Task 04.1 — Correct the stale claims in the plan record

**Finding F11 + F15 (P2).** `llms/connectrpc-plan/` is the decision record for the refactor.
Several statements in it no longer match the shipped state, and one block still reads as an open
question on a plan marked `done`. A reader who trusts the plan will make wrong decisions.

Verified mismatches:

| Location | Claim | Reality |
| --- | --- | --- |
| `01-transport.md:12` | buf-generated Go in `codegen/proto/go/`, "committed" | `/codegen/` is gitignored (`.gitignore:99`); `git ls-files codegen` is empty. Commit `9362786` untracked `gen/`, and `e6416a0` moved the ignore rule to `/codegen/` when the output path changed |
| `endpoint-reference.md:457` | plugins `protoc-gen-es` / `protoc-gen-connect-es` | `buf.gen.yaml` has no connect-es plugin; `package.json` has no `protoc-gen-connect-es`. connect-es v2 removed it, as `04-client.md` itself records |
| `01-transport.md:16`, `03-server.md:18`, `04-client.md:34` | Yaak folder `[ConnectRPC] System (smoke)` | no such folder exists; the workspace was reorganised in `b512a72` into `[Tango] Account`, `[Tango] Authentication`, `[Tango] Webhooks`, and topic folders |
| `endpoint-reference.md:492-507` | "Ambiguities to resolve before implementation", A1–A5 | A1 and A2 carry inline resolutions elsewhere in the file; A3 is still cited as open in the matrix rows; A4 is unanswered; A5 is answered only in the route tables |
| `03-server.md:21` | gRPC and gRPC-Web answer not_found | corrected by task 02.2 |
| `README.md:105-109` | completion criteria include "every endpoint in the reference has an explicit protocol decision" and "all first-party callers use the Connect client" | three procedures lacked a row (fixed by task 02.3); no SPA exists yet (`index.html:97` still comments out the app entry), so the caller criterion cannot be satisfied |

The plan also carries a `status: done` frontmatter while its own completion criteria are unmet. That
is the core problem: the marker hides the remaining work.

### Required final shape

The plan record is accurate: no claim contradicts the code, the ambiguity block reflects the
resolutions, and the status reflects the true state.

### Steps

1. Fix the generated-code claim in `01-transport.md`: state that `codegen/proto/go/` and
   `codegen/proto/ts/` are build outputs, gitignored, produced by `task rpc:generate`, and that
   every build path generates first.
2. Remove `protoc-gen-connect-es` from `endpoint-reference.md:457` and state that `protoc-gen-es`
   emits the service descriptors.
3. Replace the three `[ConnectRPC] System (smoke)` folder references with the current folder names.
   Confirm the current names through the Yaak MCP integration before writing them.
4. Rewrite the ambiguity block. For each of A1–A5: state the resolution, the commit that
   resolved it, and the evidence. A3 is answered by the fact that `/api/apis/{id}` was deleted in
   phase 05, so the trailing-slash question is historical; say so. A4 is answered by task 02.3.
   A5 is answered by `modules/federation/oidc/handler.go:24-25` (both `GET` and `POST` on
   `/authorize`). Rename the section to "Ambiguity resolutions".
5. Reconcile the completion criteria with reality. Two options:
   - keep `status: done` and rewrite the criteria to describe what was actually delivered
     (transport, contracts, server, auth transport, cutover, retirement, verification), moving the
     caller-migration criterion into this remediation plan;
   - or set the plan to `status: partial` and name the outstanding criterion.
   Choose one and record the reason. Do not leave `done` next to an unmet criterion.
6. Re-check `02-protobuf.md:66` ("before generated code is committed") and `07-verification.md:48`
   ("generated code is reproducible and checked in") — both repeat the untracked-code confusion.

Validation: no statement in `llms/connectrpc-plan/` contradicts `git ls-files`, `buf.gen.yaml`,
`package.json`, or the mounted routes. Re-read the whole folder after editing.

Commit: `docs(rpc): correct the connectrpc plan record`

## Task 04.2 — Teach AGENTS.md the ConnectRPC era

**Finding F12 (P2).** `AGENTS.md` is the single instruction source for every agent in this
repository. It has **zero** mentions of ConnectRPC, `/rpc`, `api/connect`, `buf`, or `codegen`:

```
grep -c -i "connectrpc\|/rpc\|api/connect\|buf\b\|codegen" AGENTS.md  ->  0
```

An agent that follows it today would:

- add a new internal endpoint as a REST handler, because nothing says first-party surfaces live
  under `/rpc`;
- never run `task rpc:generate`, because nothing mentions protobuf;
- believe `api/client` is "part of the API contract for every internal surface"
  (`AGENTS.md:53`), which contradicts the ConnectRPC plan's REST-only decision;
- look for `llms/phase-*.md` (`AGENTS.md:52`), which does not exist — the porting plan lives in
  `llms/porting-plan/`.

This is the highest-leverage documentation fix in the plan, because it is the file agents read
first.

### Required final shape

`AGENTS.md` describes the transport split, the proto-first workflow, the generated-code policy, and
the correct plan paths.

### Steps

1. Add a ConnectRPC section under "Architecture" stating:
   - first-party application API is ConnectRPC below `/rpc`, contract in `api/connect/*.proto`;
   - protocol and infrastructure surfaces stay REST below `/api` or a root path;
   - generated Go and TypeScript land in `codegen/proto/go/` and `codegen/proto/ts/`, are
     gitignored, and `task rpc:generate` must run before build, test, and typecheck (the task
     graph already wires this — say so);
   - handlers live in `modules/<area>/<feature>/handler_rpc.go` beside `handler.go`;
   - new endpoints start in the proto, then the handler, then the matrix rows, then the Yaak
     request.
2. Correct the plan path in the "Add an endpoint" bullet: `llms/porting-plan/` for upstream parity
   work, `llms/connectrpc-plan/endpoint-reference.md` for transport decisions, and this remediation
   plan for the open findings.
3. Rewrite the `api/client` bullet (`AGENTS.md:53`) to match the shipped decision: `api/client` is
   the **REST-only** SDK for retained HTTP and protocol endpoints; it does not wrap ConnectRPC
   services, and adding an RPC does not require an SDK change. Keep the existing rule that a change
   to a retained REST route does require the SDK sync.
4. Add the machine-credential rule: `X-API-KEY` reaches only the documented admin surface; password
   change, profile update, and API-key minting/renewal are session-only. Reference the boundary
   test from task 01.1.
5. Add the guard-selection rule from task 03.1 so a new service picks the right middleware.
6. Add the CORS rule: a new RPC header must be added to `internal/transport/middleware/cors.go`.
7. Keep the file within its existing style: short bullets, no history, no phase markers.

Validation: an agent reading only `AGENTS.md` can add a correct ConnectRPC endpoint. Verify by
walking the steps against a real procedure (for example `WebhookService.Get`) and confirming each
step in the instructions matches a file that exists.

Commit: `docs: describe the connectrpc architecture in AGENTS.md`

## Status

Both tasks are done and committed.

| Task | Finding | Commit |
| --- | --- | --- |
| 04.1 | F11, F15 | `1371953` |
| 04.2 | F12 | `6bcc4af` |

### Task 04.1 result

Corrections shipped in `llms/connectrpc-plan/`:

| Location | Correction |
| --- | --- |
| `01-transport.md` | generated Go is a gitignored build output, not committed; the Yaak evidence folder is `Health Check` |
| `02-protobuf.md` | the endpoint reference is recorded before generation, not before a commit |
| `03-server.md`, `04-client.md` | the `[ConnectRPC] System (smoke)` folder no longer exists; the workspace was reorganised |
| `07-verification.md` | generated code is reproducible by `task rpc:generate`, never committed |
| `endpoint-reference.md` | `protoc-gen-connect-es` removed; the ambiguity block became "Ambiguity resolutions" |
| `README.md` | completion criteria describe what was delivered |

The ambiguity block is resolved in full: A1 and A2 became `OidcConsentService` and
`OidcClientService` procedures, A3 is closed as historical because `/api/apis*` was retired
(`internal/registry/rpc_inventory_test.go` lists it under `retiredREST`), A4 is answered by task
02.3, and A5 is answered by `modules/federation/oidc/handler.go:24-25` (both `GET` and `POST`).

**Status decision (F15).** The plan keeps `status: done`, and the completion criteria were rewritten
to describe the transport refactor that shipped. Two criteria are named as out of scope instead of
sitting unmet:

- *first-party callers use the Connect client* — the SPA does not exist (`index.html:97` still
  comments out the app entry), so there is no caller to migrate; the SPA work owns it;
- *`api/client` wraps the RPC surface* — `api/client` is REST-only by decision.

`llms/connectrpc-plan/README.md` now links to this remediation plan as the open work. Rationale for
keeping `done`: the refactor is complete, and the unmet criterion belongs to a different piece of
work. `status: partial` would imply the refactor itself is unfinished.

### Task 04.2 result

`AGENTS.md` gained a "Transport split (ConnectRPC vs REST)" section and a "ConnectRPC
authorization" section covering the proto-first workflow, the gitignored generated outputs, the
`handler_rpc.go` placement, the `X-API-KEY` boundary with its test, guard selection, and the CORS
rule. The plan paths were corrected (`llms/porting-plan/` for parity, `llms/connectrpc-plan/` for
transport decisions, `llms/remediation-connectrpc/` for open findings) and the `api/client` bullet
now states the REST-only decision.

Verified by walking the instructions against `WebhookService.Get`: the proto declares it
(`api/connect/webhook.proto:48`), the handler is `modules/webhook/handler_rpc.go`, the mount is
`internal/registry/registry.go:264`, the matrix row is `llms/endpoint-reference.md:98`, and
`rpc:generate` exists with `test`, `dev`, `build`, `typecheck`, and `release` depending on it.

### Phase 04 gate

| Command | Result |
| --- | --- |
| `go test ./...` (debug tags) | pass |
| `go test -tags release ./...` | pass |
| `go test -tags debug ./cmd/... ./database/...` | pass |
| `pnpm exec vitest run` | 43 tests pass, 7 files |
| `task lint` | 0 issues |
| `task check` | clean |
| `task typecheck` | clean |
| `task rpc:lint`, `task rpc:breaking`, `task rpc:stale` | clean |
