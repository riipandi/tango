---
status: done
updated: 2026-09-18
owner: tango-remediation
---

# Phase 3 — Architecture Boundary Separation

Prerequisite: Phase 1 and Phase 2 done.

## Task 3.1 — Separate recovery HTTP handlers from use cases

Split `modules/identity/recovery` into request/response handlers and a service/use-case layer.
The service must not import `net/http`, chi, middleware, or responder. Handlers translate
sentinel/typed errors into the envelope.

Preserve the generic forgot-password response, token single-use, session invalidation, cookie
rotation, and email queue behavior.

Add service unit tests without HTTP plus handler tests for body, status, envelope, and cookies.

Commit: `refactor: separate recovery transport from service`

## Task 3.2 — Normalize federation transport boundaries

Audit the `authorize`, `token`, `device`, `PAR`, `userinfo`, discovery, and metadata code. Split
protocol handlers from service/store when a file mixes use cases with HTTP response mapping.

Protocol exceptions stay bare per OIDC/OAuth; ordinary JSON endpoints keep using responder. Do
not move responder into stores or domain services.

Commit: `refactor: isolate federation protocol handlers`

## Task 3.3 — Add enforceable architecture checks

Extend the architecture tests to detect:

- responder/net/http/chi imports in service and store files;
- concrete sibling-module imports outside the composition root;
- store files touching `Pool()` directly;
- route registration outside module route methods;
- duplicate method+path.

The rules must have explicit false-positive exceptions only for protocol handler files.

Commit: `test: enforce application architecture boundaries`

## Phase acceptance criteria

- Domain/service packages are testable without the HTTP transport.
- Handlers are the only layer mapping domain errors to HTTP responses.
- Architecture tests catch regressions automatically.
- No new module or nested generic internal architecture appears.
