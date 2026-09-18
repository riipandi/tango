---
status: done
updated: 2026-09-16
---

# Target Modular Monolith Architecture

## Decision

Keep the current command and infrastructure layout. Simplify the application boundary and module
graph instead of introducing new infrastructure layers.

The target has four application modules:

```text
modules/identity     users, groups, credentials, sessions, MFA, WebAuthn, signup, verification
modules/federation   OIDC, JWKS, discovery, SCIM
modules/admin        app config, audit, API keys, APIs, permissions, custom-claim administration
modules/webhook      webhook endpoints, outbox delivery, signatures, logs
```

The following remain in their current locations:

```text
cmd/launcher         CLI and server entrypoint
internal/config      process configuration
internal/datastore   Postgres pool and transaction primitives
internal/fetcher     outbound HTTP client
internal/jobs        email and recurring job coordination
internal/kernel      only small shared auth/runtime contracts
internal/logger      logging
internal/mailer      email delivery
internal/queue       Postgres-backed task queue
internal/registry    explicit composition root
internal/storage     shared blob adapter only when an in-scope media feature needs it
internal/transport   HTTP server and middleware
```

Do not create `internal/app`, `internal/platform`, `internal/domain`, or another nested platform
tree. Do not introduce a DI framework, generic plugin registry, service locator, generic event bus,
message broker, or microservice boundary.

## Current problems this architecture fixes

- `internal/kernel.Registry` discovers capabilities through runtime type assertions.
- `identity.Feature` and `federation.Feature` turn ordinary use cases into plugins.
- `registry.Deps` contains mutable fields that are populated after construction.
- `appconfigRef`, `eventFanout.Attach`, and `SetVersionFeed` create temporal coupling.
- Service packages retain HTTP guards and transport concerns.
- Domain errors directly carry `pkg/responder` HTTP status behavior.
- Identity, federation, appconfig, and webhook modules depend on concrete sibling modules.

## Target composition root

Keep `internal/registry`, but replace the generic registry with a concrete runtime:

```go
type Runtime struct {
    Identity   *identity.Module
    Federation *federation.Module
    Admin      *admin.Module
    Webhook    *webhook.Module
    Queue      *queue.Client
    Jobs       *jobs.Registry
}

func New(deps Dependencies) (*Runtime, error)
func (r *Runtime) MountRoot(router chi.Router)
func (r *Runtime) MountAPI(router chi.Router)
func (r *Runtime) Start(ctx context.Context) error
func (r *Runtime) Stop(ctx context.Context) error
```

`cmd/launcher/serve.go` keeps loading configuration, opening Postgres, creating logger/fetcher/
mailer, calling `registry.New`, starting the runtime, and shutting it down. Only the type returned
by `registry.New` changes from a generic module registry to the concrete runtime.

`internal/transport` receives explicit route callbacks rather than a generic module registry:

```go
type RouteSet struct {
    MountRoot func(chi.Router)
    MountAPI  func(chi.Router)
}
```

This avoids an import cycle while making route ownership visible at the server boundary.

## Module boundaries

### Identity

Owns users, groups, credentials, sessions, password auth, TOTP MFA, WebAuthn, signup, email
verification, device login, one-time access, and identity-owned claims.

Internal files/packages may remain separated for readability, but they must not be independently
registered in a runtime registry. Identity exposes narrow ports for consumers:

```go
type UserReader interface { UserByID(...) ... }
type GroupReader interface { GroupsForUser(...) ... }
type SessionReader interface { Resolve(...) ... }
type ClaimsReader interface { ClaimsForUser(...) ... }
```

The exact signatures follow existing types. Consumers must not receive the concrete identity
module, session service, or user store.

### Federation

Owns OIDC provider flows, client management, token issuance, discovery, JWKS, and SCIM. It consumes
identity and admin behavior through small consumer-side interfaces. It must not import concrete
identity feature packages for ordinary use cases.

OIDC may stay one top-level package with files per use case. Split only when a real data or behavior
boundary appears; do not create packages solely to reduce file size.

### Admin

Owns admin-facing configuration, audit reads/writes, API keys, resource APIs, permissions, and
custom-claim administration. It can consume identity management ports, but identity must not import
admin HTTP types.

### Webhook

Owns endpoint configuration, event subscriptions, signing, delivery attempts, retries, and logs.
It consumes a narrow event/outbox port and does not import identity, federation, or admin concrete
services.

## Routing and authorization

Modules expose explicit mounting methods. They do not mutate themselves through `UseGuard`,
`MountAdminAPI`, `MountSelfAPI`, or late setters.

The transport/application boundary creates route groups for:

- public routes;
- session-authenticated routes;
- admin-authenticated routes;
- API-key routes;
- OAuth/OIDC protocol routes.

Services receive a principal/actor or a narrow authorization result when business rules need it. A
service must not store `http.Handler`, `kernel.Guard`, or router state.

## Events and durability

Do not add a generic event bus. Use a small durable outbox boundary for events that must reach audit
or webhook delivery.

For a domain transaction:

```text
write domain state
write audit/outbox record with immutable delivery bytes
commit
enqueue or notify after commit
worker delivers webhook and records attempts
```

The identity service should not call the webhook module directly. Event delivery failure must not
roll back a successful user operation, and a committed user operation must not silently lose its
audit/webhook event.

The existing Postgres queue remains the worker mechanism. Do not add Kafka, Redis streams, or a
second queue implementation.

## Data and error boundaries

- Keep Postgres stores inside owning modules.
- Keep `internal/datastore` focused on executor, transaction, health, and shutdown primitives.
- Narrow broad `datastore.Store` usage over time; do not expose `Pool()` to application modules.
- Use `pkg/crypto` as the only encryption implementation. Recoverable stored values use the
  canonical `enc:` prefix; hash-only values remain one-way hashes.
- PostgreSQL uses one `public` schema with logical table ownership by module. Do not introduce
  schema-per-module, RLS, partitioning, or database-per-module without a measured requirement.
- Use relational tables for credentials, sessions, MFA recovery codes, permissions, webhook
  subscriptions, deliveries, and attempts. JSONB is limited to flexible metadata and event payloads.
- Fresh migrations define the final schema. Do not add legacy tables, compatibility columns, fallback
  readers, dual writes, compatibility views, or transitional adapters.
- Domain errors are plain errors owned by the module.
- HTTP status and responder envelopes are mapped in transport/handlers.
- `pkg/responder` must not be imported by domain services or stores.

## Atomic migration tasks

1. **Record the architecture decision** — add route/module ownership tests and dependency rules.
   Commit: `docs: define target modular monolith architecture`.
2. **Introduce concrete runtime** — add `registry.Runtime` and explicit route/lifecycle methods
   while keeping `cmd/launcher` behavior unchanged. Commit: `refactor: add explicit application runtime`.
3. **Remove generic feature registration** — make identity, federation, admin, and webhook mount
   their own routes through explicit methods. Commit: `refactor: remove feature plugin wiring`.
4. **Remove late binding** — delete `appconfigRef`, module attach methods, mutable dependency outputs,
   and late version-feed wiring. Commit: `refactor: remove late-bound dependencies`.
5. **Move transport concerns outward** — remove guards and responder status errors from services;
   preserve endpoint behavior with handler tests. Commit: `refactor: isolate transport concerns`.
6. **Group modules** — move appconfig/audit/API/key/claim administration under `modules/admin`, and
   keep identity/federation public ports stable during the move. Commit:
   `refactor: consolidate application modules`.
7. **Add durable event boundary** — write audit/webhook outbox records in the domain transaction
   and remove synchronous registry fan-out. Commit: `feat: add transactional event delivery`.
8. **Narrow infrastructure interfaces** — remove application reliance on datastore pool access and
   remove unused kernel capability interfaces. Commit: `refactor: narrow infrastructure boundaries`.
9. **Apply the encryption contract** — keep recoverable secret handling inside owning modules,
   route it through `pkg/crypto`, and enforce the strict `enc:` format in code and database tests.
   Commit:
   `fix: standardize encrypted value storage`.
10. **Remove obsolete scaffolding** — remove the old generic registry, adapters, excluded feature
   tables, routes, and dead packages. Do not replace them with compatibility wrappers. Commit:
   `chore: remove obsolete module scaffolding`.

## Architecture acceptance criteria

- `cmd/launcher` remains the only CLI/server entrypoint.
- No new nested `internal/` architecture is introduced.
- `internal/registry` is a concrete composition root, not a plugin registry.
- Only identity, federation, admin, and webhook are application module boundaries.
- No service stores an HTTP guard or router.
- No domain service imports `pkg/responder`.
- No module imports another module's concrete implementation for ordinary reads.
- No application module implements a second encryption format or writes recoverable ciphertext
  without the `enc:` prefix.
- No compatibility branch, legacy reader, dual-write path, or transitional schema artifact exists.
- Event delivery has a durable transaction boundary.
- Existing endpoint behavior, response envelope, Yaak requests, and migrations remain valid after
  each structural change.
