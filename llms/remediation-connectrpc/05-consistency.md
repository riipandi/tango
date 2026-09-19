---
status: planned
updated: 2026-09-19
owner: tango-connectrpc-remediation
---

# Phase 05 — Consistency and Closeout

Prerequisite: phase 04 done.

## Task 05.1 — Decide the pagination shape

**Finding F13 (P2).** `llms/connectrpc-plan/02-protobuf.md` contract rule: "Pagination has one
shared message shape across internal list RPCs." The shipped protos use two:

**Group A — `common.v1.PageRequest` as the whole request** (10 procedures):

| Procedure | Request |
| --- | --- |
| `ApiKeyService.List` | `common.v1.PageRequest` |
| `ApiService.ListApis` | `common.v1.PageRequest` |
| `OidcClientService.ListClients` | `common.v1.PageRequest` |
| `OidcConsentService.ListMyAuthorizedClients` | `common.v1.PageRequest` |
| `OidcConsentService.ListAllAuthorizedClients` | `common.v1.PageRequest` |
| `SignupService.ListSignupTokens` | `common.v1.PageRequest` |
| `UserService.ListUsers` | `common.v1.PageRequest` |
| `UserGroupService.ListGroups` | `common.v1.PageRequest` |
| `WebhookService.List` | `common.v1.PageRequest` |
| `WebhookService.ListAllDeliveries` | `common.v1.PageRequest` |

**Group B — a dedicated request that embeds `common.v1.PageRequest page = N`** (5 procedures across
4 requests):

| Procedure | Request | Extra fields |
| --- | --- | --- |
| `AuditLogService.List`, `AuditLogService.ListAll` | `ListAuditLogsRequest` | `event`, `user_id`, `from`, `to` |
| `OidcConsentService.ListUserAuthorizedClients` | `ListUserAuthorizedClientsRequest` | `user_id` |
| `UserGroupService.ListGroupUsers` | `ListGroupUsersRequest` | `group_id` |
| `WebhookService.ListDeliveries` | `ListDeliveriesRequest` | `webhook_id` |

Group B is legitimate where a list needs a scope or a filter. The inconsistency is narrower and
more concrete:

- `query` (free-text filter) lives inside `common.v1.PageRequest`, so a scoped list cannot accept
  it without duplicating the field. `GetQuery()` is read by three handlers (`user`, `usergroup`,
  `apiaccess`).
- Six list procedures carry no pagination field at all: `ListSecrets`, `ListUserGroups`,
  `ListWebAuthnCredentials`, `ListUserClaims`, `ListGroupClaims`, and the client-grant lists
  (`ListAssignableClients`, `ListClients`, `ListApisForClient`, `ListAssignableApisForClient`).
  Two more take `google.protobuf.Empty` (`ListSessions`, `ListMyClients`). Some of these are
  genuinely bounded; none says so.

The response side is already consistent: 11 list responses carry
`common.v1.PageMetadata metadata = 2`, and the handlers build it through `rpcerr.PageMetadata`.

### Required final shape

One documented rule for list RPCs, applied everywhere:

1. `common.v1.PageRequest` is the request when the list has no scope and no filter;
2. a dedicated `List*Request` embeds `common.v1.PageRequest page = N` when the list is scoped or
   filtered;
3. a list that cannot paginate says so in a comment, rather than silently omitting the field.

### Steps

1. Apply the rule to the protos. The candidates that need a decision are the eight unpaginated
   lists named above. Either add the embedded `page` field with handler support, or document why
   the list is bounded. Do not add a field without wiring it in the handler — an ignored field is
   worse than an absent one.
2. Move `query` out of `common.v1.PageRequest` if the scoped lists need it, or document that
   free-text filtering applies only to the unscoped lists. `GetQuery()` is read by three handlers
   (`usergroup`, `user`, `apiaccess`).
3. Record the rule in `llms/connectrpc-plan/endpoint-reference.md` under the generated-code or
   contract section.
4. Run `task rpc:generate` and `task rpc:breaking`; a request-shape change is a breaking change for
   any consumer, so `rpc:breaking` must be run and its result recorded even if the check is
   expected to fail.
5. Update the affected handlers, the matrix rows, the Yaak requests, and the Go tests in the same
   commit.

If step 1 or 2 requires changing the wire shape of a shipped procedure, stop and ask the owner
first: the SPA does not exist yet, so the cost is low, but the plan's rule is that a contract
change is a decision, not a cleanup.

Validation: `task rpc:lint`, `task rpc:generate`, `task test:go -- ./modules/...` pass; every list
RPC either paginates or documents why it does not.

Commit: `refactor(rpc): unify the list request shape`

## Task 05.2 — Protect the Yaak workspace from credential and churn commits

**Finding F14 (P2).** `api/specs/yaak.rq_5SgzmJyWWh.yaml` (sign-in) stores a plaintext password at
`HEAD`:

```yaml
body:
  text: |-
    {
      "identity": "admin@example.com",
      "secret": "@admin123"
    }
```

`git grep -nE "admin123" HEAD -- api/specs` returns that one line.
`api/specs/yaak.rq_fziQd4bTFR.yaml` (forgot password) carries `{"identity":"admin@example.com"}`.
Neither is a real production credential, but both defeat the policy that commit `a23ddcd`
established: "the requests no longer carry credentials: 20 bearer JWTs, the bench password, the
SCIM provider token, and the OIDC client id now ride environment variables". That commit replaced
`REPLACE_IDENTITY`/`REPLACE_PASSWORD` with environment variables; commit `4a095a6` (the audit
window) reformatted 131 request bodies and wrote literal values back into these two.

The second problem is churn. `4a095a6` touched **131 files, 608 insertions, 228 deletions** for
JSON re-indentation (`'{"a":1}'` becoming a block scalar) plus `updatedAt` bumps. No contract
changed. A reviewer cannot separate a real edit from app-generated noise, and the sign-in literal
rode in on exactly that noise.

### Required final shape

No credential literal in a tracked `api/specs/*.yaml`, and a reviewer can separate a contract
change from export churn.

### Steps

1. Restore the sign-in request to placeholder or environment form through the Yaak MCP integration
   (`REPLACE_IDENTITY` / `${[ password ]}`, matching how the other credential-bearing requests
   were normalised in `a23ddcd`). Do the same for the forgot-password request's `identity`.
   Do not hand-edit the YAML; the export is one-way and the live workspace is the source.
2. Sweep the rest of the export for the same regression. The `4a095a6` commit rewrote 131 request
   bodies, so check every one that carries a credential-shaped field:
   `git show 4a095a6 | grep -E '^\+.*"(secret|password|current_password|new_password|token|identity)"'`.
   Classify each hit as a placeholder, an environment reference, or a literal that must be
   replaced. `admin@example.com` inside `yaak.rq_UBkffFzKH5.yaml` is a fixture email in a signup
   request, not a credential; leave it or normalise it to `REPLACE_ADMIN_EMAIL`.
3. Decide the churn policy and record it:
   - **Normalise the export.** Accept the reformatting as a single dedicated commit, so later
     contract diffs stay small. The reformatting is already committed as `4a095a6`, so this option
     is effectively taken; state it explicitly so the next agent does not re-litigate it.
   - **Stop tracking the export.** Remove `api/specs/` from git (194 tracked files) and treat the
     Yaak workspace as local tooling. This removes the audit trail that `a23ddcd` and `0e6d2d0`
     built, so do not take it without the owner. Note that `api/specs/` has been tracked since
     `dcad7a8`; the `/specs/` rule that briefly lived in `.gitignore` was root-anchored and never
     matched it (removed in `a518f23`).
4. Add a guard so the literal cannot return. `lefthook.yml` is the natural home; scope the rule to
   `api/specs/*.yaml` and to an explicit pattern set, not an entropy heuristic:
   - a `"secret"`, `"password"`, `"current_password"`, or `"new_password"` value that is neither a
     `REPLACE_*` placeholder nor a `${[...]}` environment reference;
   - a `Bearer eyJ` literal;
   - an API-key literal (`pik_`, `sk_`) outside a placeholder.
5. Verify the guard by reintroducing `@admin123` locally and confirming the check fails.

Validation: the guard fails on the literal and passes on the reverted tree.

Commit: `chore(yaak): keep credentials out of the tracked workspace`

## Task 05.3 — Close the plan

Prerequisite: tasks 01.1 through 05.2 complete.

### Steps

1. Re-run the full gate and record the command, date, environment, and result in this file:
   `task test` (release + debug + frontend), `task lint`, `task check`, `task format`,
   `task typecheck`, `task rpc:lint`, `task rpc:breaking`, `task rpc:stale`.
2. Re-verify each finding in the README table against the code and mark it closed with the
   resolving commit. A finding is closed only when its verification method now passes.
3. Re-run the audit probes that produced the findings, as temporary tests that are deleted
   afterwards:
   - the machine-credential boundary table from task 01.1;
   - the anonymous `ApplicationConfigurationService.Get` call from task 01.2;
   - the gRPC, gRPC-Web, and method assertions from task 02.2 and 02.3;
   - the `/rpc` 429 body from task 02.4;
   - the CORS preflight header list from task 02.5.
4. Set this plan's frontmatter to `status: done` and update `updated`.
5. Leave the `llms/connectrpc-plan/` status decision from task 04.1 as recorded there. Do not
   re-open it here.
6. Recommend a commit message for the owner. Do not push.

### Completion criteria

- Every finding in the README table is closed with a commit and a passing verification.
- The documents, the protos, and the mounted handlers agree on the `/rpc` surface.
- `AGENTS.md` describes the shipped architecture.
- No plaintext credential is tracked under `api/specs/`.
- The full gate passes and its evidence is recorded here.

Commit: `docs(rpc): close the connectrpc remediation`

## Final gate record

Pending.
