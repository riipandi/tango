---
status: draft
updated: 2026-09-17
owner: tango-remediation
---

# Phase 2 — Excluded Feature and Contract Artifact Cleanup

Prerequisite: Phase 0 done. Schema tasks that impact contracts may run after Phase 1 finishes.

## Task 2.1 — Remove stale Application Images Yaak artifacts

Remove the Application Images folder and requests from the exported Yaak specs, or mark them
excluded if the workspace really needs the record. The fresh-implementation preference is to
delete requests that are not supported.

Make sure no saved body, expected response, or folder description implies the Application Images
routes are supported.

Commit: `chore: remove excluded application image requests`

## Task 2.2 — Remove LDAP artifact wording

Remove LDAP references from active code/config/spec descriptions that are not exclusion
explanations. Keep only the references needed to prove the route is not mounted or the exclusion
in the scope/deviation documents.

Make sure there is no LDAP dependency, config key, compose service, fixture, schema column, or
active Yaak request.

Commit: `chore: clean excluded ldap artifacts`

## Task 2.3 — Refresh endpoint reference and deviations

Synchronize the endpoint reference and the deviations doc with the final runtime:

- the `done`, `partial`, and `excluded` statuses must be accurate;
- CIMD requests must no longer be described as `planned` once the endpoint is complete;
- every intentional deviation carries a reason, a status, and evidence;
- excluded endpoints do not count as parity failures or supported surfaces.

Commit: `docs: synchronize endpoint contract artifacts`

## Phase acceptance criteria

- No active or stale Application Images request remains.
- LDAP appears only in the required exclusion documents/route-negative tests.
- No `planned` endpoint is actually implemented.
- Every in-scope row has an up-to-date test reference and Yaak reference.
