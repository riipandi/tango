---
status: done
updated: 2026-09-19
---

# Phase 02: Protobuf and Code Generation

> Completed 2026-09-19. Contracts frozen flat in `api/connect/*.proto` (common, system, identity,
> admin, federation, webhook) with module-owning packages `tango.<module>.v1`; TypeIDs and
> timestamps are strings; show-once secrets are dedicated response fields documented never to
> return from reads. buf generation produces untracked build outputs — Go in `codegen/proto/go/` and
> TypeScript in `codegen/proto/ts/` — through pinned, locally resolved plugins; buf itself comes
> from the `go.mod` tool directive (`go tool buf`), so no global binary is required.
> `test`/`dev`/`build`/`typecheck` depend on generation, and every build path (Dockerfile,
> GoReleaser, CI) generates before it compiles; `.rpc-gen.stamp` detects stale
> contracts (`task rpc:stale`). Task targets: `rpc:generate`, `rpc:lint`, `rpc:breaking`,
> `rpc:stale`. Connect error mapping frozen in the endpoint reference with helper constructors in
> `internal/rpcerr`. Representative compatibility test:
> `internal/transport/rpc_contract_test.go` (User message vs REST DTO document, including the
> proto-JSON default-omission rule). Yaak evidence: `POST /rpc/tango.identity.v1.UserService/ListUsers`
> (`rq_o3cpcGiTWL`) answers `not_found` until the service implementation lands in phase 05 — it
> pins routing, procedure path, and metadata for the generated contract.

## Outcome

Create stable internal RPC contracts without copying transport-specific REST envelopes into RPC
messages.

All hand-written protobuf contracts in this phase are placed directly in `api/connect/*.proto`.
Use files such as `api/connect/identity.proto`, `api/connect/admin.proto`,
`api/connect/federation.proto`, and `api/connect/webhook.proto` unless the service split requires
more focused files. Do not create nested contract directories under `api/connect/`.

## Tasks

1. Add protobuf definitions grouped by owning module in `api/connect/*.proto`: identity, admin,
   federation, and webhook.
2. Define request, response, pagination, filtering, and validation messages in protobuf terms.
3. Represent show-once secrets explicitly and document that they are never returned by read RPCs.
4. Keep TypeID values as strings at the wire boundary; do not expose raw UUIDs in RPC paths.
5. Define a consistent Connect error mapping for validation, authentication, authorization,
   not-found, conflict, rate-limit, and internal errors.
6. Configure deterministic generation for Go and TypeScript from `api/connect/*.proto`. Generated
   output must be reproducible in CI and must not be edited manually or written back into the
   contract directory.
7. Add and pin `buf.yaml`/`buf.gen.yaml` (or the selected equivalent), generator versions, Go
   package options, TypeScript runtime/plugin, and generated output directories.
8. Add task targets for generation, formatting, linting, breaking checks, and stale-generated-file
   detection. Generation must work from a clean checkout without relying on globally installed
   binaries beyond the declared task setup.
9. Keep `api/client` schemas and methods limited to retained REST endpoints. Do not make the REST
   SDK wrap generated ConnectRPC clients or duplicate protobuf service contracts.
10. Decide whether generated TypeScript protobuf types are consumed directly by the Connect client
   or adapted by a thin generated transport layer; do not maintain hand-written RPC DTOs beside
   the protobuf messages.
11. Create or update Yaak Connect Protocol HTTP requests through Yaak MCP for representative
    generated services. Use the generated service and method names from the proto contract; never
    hand-edit exported Yaak request YAML.

## Contract rules

- RPC messages use protobuf naming and generated JSON mapping; they do not use the REST envelope.
- `api/connect/*.proto` contains contracts only: no generated code, server implementation, client
  wrappers, or transport-specific helper logic.
- Every Connect service has an explicit package and method name. The mapping is recorded in the
  endpoint reference before the generated code is produced (generated output is a gitignored build
  artifact, never committed).
- Pagination has one shared message shape across internal list RPCs: `common.v1.PageRequest` is the
  request when the list has no scope and no filter, a dedicated `List*Request` embeds
  `common.v1.PageRequest page = N` when the list is scoped or filtered, and a bounded list documents
  why it cannot paginate. The rules themselves live in `pkg/responder` and are shared with the REST
  transport.
- Empty success responses use `google.protobuf.Empty` or an explicit result only when metadata is
  needed; do not encode HTTP 204 semantics into every RPC.
- Protocol REST DTOs remain hand-written where RFC wire behavior requires it.

## Gate

Generation works from a clean checkout, generated Go and TypeScript compile, and one representative
RPC has a request/response compatibility test plus a Yaak MCP Connect Protocol request that uses
the generated service contract.

## Commit

Commit this phase as one atomic conventional commit containing `api/connect/*.proto`, generation
configuration, generated output, codegen tasks/checks, tests, and Yaak contract evidence.
