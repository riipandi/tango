---
status: draft
updated: 2026-09-15
owner: tango-auth-porting
---

# Architecture Requirements

## Product decision

Tango remains a modular monolith. Preserve the existing command and infrastructure layout, and
reduce complexity by making application boundaries explicit.

### Preserved paths

- `cmd/launcher` remains the CLI and server entrypoint.
- Existing `internal/` packages remain flat: `config`, `datastore`, `fetcher`, `jobs`, `kernel`,
  `logger`, `mailer`, `queue`, `registry`, `storage`, and `transport`.
- `internal/registry` remains the composition root, but becomes a concrete runtime builder.
- `internal/transport` remains the HTTP server and middleware boundary.
- `internal/queue` remains the Postgres-backed worker system.

Do not add `internal/app`, `internal/platform`, `internal/domain`, a DI framework, a generic plugin
registry, a generic event bus, a message broker, or a microservice boundary.

## Application modules

### Identity

Owns users, groups, credentials, sessions, password authentication, TOTP MFA, WebAuthn, signup,
verification, device login, and one-time access.

Identity exposes narrow consumer ports for user reads, groups, claims, and session resolution. Other
modules do not receive concrete identity services or stores.

### Federation

Owns OIDC clients and flows, authorization, tokens, userinfo, introspection, discovery, JWKS, and
SCIM. It consumes identity/admin behavior through small interfaces defined at the consumer side.

### Admin

Owns application settings, audit, API keys, APIs, permissions, and custom-claim administration.
It may call identity management ports, but identity never imports admin HTTP types.

### Webhook

Owns endpoint CRUD, event subscriptions, signing, delivery, retries, test delivery, and logs. It
consumes a narrow event/outbox contract and does not import concrete identity or federation modules.

## Runtime composition

Replace the generic `kernel.Registry` with a concrete `registry.Runtime` in `internal/registry`:

```go
type Runtime struct {
    Identity   *identity.Module
    Federation *federation.Module
    Admin      *admin.Module
    Webhook    *webhook.Module
    Queue      *queue.Client
    Jobs       *jobs.Registry
}
```

The runtime owns construction order, route mounting, startup, and shutdown explicitly. Constructors
must receive complete dependencies; no `Attach`, `UseGuard`, `SetVersionFeed`, or mutable output
fields are allowed after construction.

`cmd/launcher/serve.go` keeps its current responsibilities and calls the concrete runtime. The
transport package receives explicit root/API route callbacks so it does not need to import the
registry package.

## Route and auth requirements

Route mounting is explicit at module boundaries. Services do not store HTTP guards, routers, or
handlers. Transport creates public, session, admin, API-key, and OIDC route groups and passes the
authenticated actor into use cases where needed.

The architecture must not rely on nil guards or registration order to decide whether an endpoint is
available. A route is either mounted by an explicit module method or absent by design.

## Event requirements

Do not add a general event bus. Security/audit events and webhook delivery records must have a
durable transaction boundary:

```text
domain write -> audit/outbox write -> commit -> queue notification -> delivery worker
```

The queue remains the delivery mechanism. Delivery failures are retried and logged without rolling
back a successful domain operation.

## Error and persistence requirements

- Domain services own plain sentinel/typed errors.
- HTTP handlers map them to `pkg/responder` envelopes.
- `pkg/responder` is not imported by stores or domain services.
- Stores stay inside their owning module and use `internal/datastore` transactions.
- Application modules do not use `datastore.Store.Pool()` directly.
- Existing TypeID and Postgres boundaries remain unchanged unless a task explicitly documents them.
- Recoverable secrets cross module storage boundaries only through `pkg/crypto` and use the
  canonical `enc:` format. Hash-only values remain hashes.
- PostgreSQL uses one `public` schema with logical module ownership. Core credentials, sessions,
  MFA, permissions, webhook subscriptions, deliveries, and attempts use relational tables; JSONB is
  limited to flexible metadata and event payloads.
- Fresh migrations define the final schema. No legacy tables, compatibility columns, fallback
  readers, dual writes, compatibility views, or transitional adapters are allowed.

## Required architecture tests

- Route ownership test: each public path is mounted exactly once.
- Runtime test: startup and shutdown order is explicit and deterministic.
- Dependency test: no application module imports another module's concrete implementation except
  approved composition wiring.
- Boundary test: domain services compile without responder or HTTP middleware imports.
- Event test: a committed domain mutation creates the expected outbox/audit record; delivery failure
  remains observable and retryable.
- Regression test: `cmd/launcher` still starts the same server and all Yaak requests remain valid.

## Migration tasks

1. Document and test ownership boundaries.
2. Add concrete `registry.Runtime` while keeping launcher behavior unchanged.
3. Remove generic feature registration and module capability discovery.
4. Remove late-bound dependency holders and setters.
5. Move guards and HTTP error mapping to the transport/application layer.
6. Consolidate existing appconfig, audit, API, API-key, and claim packages under `modules/admin`.
7. Add the durable event/outbox boundary without adding another queue.
8. Narrow datastore and kernel contracts.
9. Remove obsolete generic registry, adapters, excluded feature paths, and dead packages without
   replacing them with compatibility wrappers.

Each step preserves the intended in-scope public behavior, has focused tests, updates affected Yaak
requests, and is one atomic commit. It must not preserve obsolete internal or database shapes.

## Architecture acceptance criteria

- `cmd/launcher` remains unchanged in role and remains the only entrypoint.
- No new nested `internal/` architecture exists.
- `internal/registry` is concrete and explicit, not a plugin registry.
- Exactly four application module boundaries are used: identity, federation, admin, webhook.
- No service owns an HTTP guard or router.
- No domain service imports `pkg/responder`.
- No concrete sibling-module dependency exists outside composition wiring.
- Events needed by audit/webhooks survive domain commit and remain retryable.
- Full API behavior, responder envelope, migrations, and Yaak evidence remain green.
- Encryption-format tests, unprefixed-value rejection tests, schema-contract tests, and
  secret-redaction checks remain green.
