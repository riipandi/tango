---
status: draft
updated: 2026-09-17
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
