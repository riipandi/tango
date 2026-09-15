---
status: draft
updated: 2026-09-15
owner: tango-auth-porting
---

# Tango Authentication Porting PRD

This PRD defines the product and engineering requirements for porting the in-scope Pocket ID
authentication API into tango. It is split into focused documents so an implementation agent can
load only the context needed for a task.

## Source of truth

- Product scope and delivery order: [`llms/porting-plan/README.md`](../porting-plan/README.md)
- Endpoint inventory: [`llms/endpoint-reference.md`](../endpoint-reference.md)
- Upstream schema reference: [`llms/database-reference.sql`](../database-reference.sql)
- Tango response contract: [`pkg/responder/`](../../pkg/responder/)
- Tango encryption contract: [`pkg/crypto/`](../../pkg/crypto/)
- Tango transport contract: [`internal/transport/`](../../internal/transport/)
- Historical work: [`llms/archived/`](../archived/), context only
- Upstream API reference: <https://pocket-id.org/docs/api>

## PRD sections

1. [Product context and outcomes](./01-context.md)
2. [Scope and exclusions](./02-scope.md)
3. [Actors and user journeys](./03-journeys.md)
4. [Upstream API parity requirements](./04-api-parity.md)
5. [Password and TOTP MFA requirements](./05-authentication.md)
6. [Webhook requirements](./06-webhooks.md)
7. [Data, security, and maintainability](./07-data-security.md)
8. [Verification and release acceptance](./08-verification.md)
9. [Delivery, dependencies, and risks](./09-delivery.md)
10. [Architecture requirements](./10-architecture.md)
11. [Recoverable secret encryption](./11-encryption.md)

## Decision status

This PRD is ready for implementation planning. Route-level details must be confirmed against the
endpoint matrix and the local Pocket ID v2.14.0 source before code changes are made.
