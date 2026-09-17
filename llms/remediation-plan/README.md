---
status: done
updated: 2026-09-18
owner: tango-remediation
---

# Remediation Plan

This plan closes the findings of the audit against the PRD, porting plan, endpoint reference,
final schema, Yaak artifacts, and the tango runtime implementation.

The end state is:

- no legacy reader, fallback reader, dual write, compatibility adapter, or transitional schema;
- the Postgres schema only holds the final shapes used by the runtime;
- Application Images and LDAP stay excluded without misleading residue;
- handler, service, store, and module boundaries are separated per the architecture requirements;
- public errors are stable and do not disclose internal details;
- every endpoint has up-to-date tests, matrix rows, and Yaak evidence;
- full test, lint, format, vet, race, and live verification runs are provable.

## Source of truth

The implementation must follow, in priority order:

1. `AGENTS.md`;
2. `llms/prd/`;
3. `llms/porting-plan/`;
4. `llms/endpoint-reference.md`;
5. `llms/tango-deviations.md`;
6. the local upstream Pocket ID checkout.

`llms/archived/` is historical context only and is not evidence of completion.

## Atomic commit rule

Every numbered task is one atomic commit. Agents must:

- work on exactly one task per commit;
- include the source, migration, test, matrix, and Yaak changes that belong to that task's
  acceptance criteria in the same commit;
- run the task validation before committing;
- never merge two tasks or two phases into one commit;
- never create empty or progress commits;
- use the commit message stated in the task;
- never push.

If a task uncovers ambiguous upstream behavior, the agent must stop at that task, record the
evidence, and ask the project owner for a decision before changing the implementation or
contract.

## Phase order

1. [Baseline and inventory](./00-baseline.md) — done
2. [Final schema and compatibility removal](./01-final-schema.md) — done
3. [Excluded feature and contract artifact cleanup](./02-excluded-and-contracts.md) — done
4. [Architecture boundary separation](./03-architecture-boundaries.md) — done
5. [Validation and error safety](./04-validation-and-errors.md) — done
6. [Runtime verification and live contract](./05-verification-gates.md) — done
7. [Final acceptance and closeout](./06-closeout.md) — done

A phase starts only after the previous phase's acceptance criteria are met and every task of the
previous phase has its atomic commit.

## Open decision (blocker)

- **Setup endpoint session cookie** — `POST /api/signup/setup` issues a session token but does
  not set the cookie, and the first admin has no password row. The account is reachable only
  through `tango setup` (CLI) until this is decided: set the cookie (upstream parity) or
  document CLI-only bootstrap. Evidence and options: `05-verification-gates.md` § Findings.
