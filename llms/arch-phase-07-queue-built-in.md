---
status: planned
updated: 2026-09-15
---

# Phase 7 — Promote the Queue to a Built-In Subsystem

Ownership decision (2026-09-15): the backlite upstream is no longer tracked. `internal/antree`
is the project's own queue foundation for task execution (and, later, scheduling — deferred,
see Non-Goals). This phase removes the "ported library" framing and the last integration
rough edges, making the queue a first-class built-in subsystem with the same conventions as
the rest of the stack.

## Current State (verified 2026-09-15)

- `internal/antree/` (13 files, ~1.3k LOC incl. tests + README): engine — `Client`,
  dispatcher, queue registry, task types, Postgres store. Self-contained; only external
  deps are `pgx/v5`, `go-sqlbuilder`, `encoding/json/v2`.
- `internal/queue/queue.go` (+test): 30-LOC kernel lifecycle adapter (`Module` with
  `Start`/`Stop`) wrapping `*antree.Client`.
- Consumers: `internal/jobs` (email + recurring maintenance + payload types),
  `internal/registry` (construction site, `deps.Queue`), `internal/logger` (`QueueLogger`),
  `modules/webhook` (registers + enqueues `jobs.WebhookDeliveryTask`).
- Schema: owned by `database/migrations/00010_create_queue_tables.sql`
  (`public.queue_tasks`, `public.queue_tasks_completed`); the client never mutates the
  schema — convention already satisfied.
- Config: `QUEUE_WORKERS`, `QUEUE_RELEASE_AFTER`, `QUEUE_CLEANUP_INTERVAL` exist in
  `internal/config` and `.env.example`; wired in `internal/registry/registry.go`.

## Integration Gaps Being Fixed

1. `ClientConfig.DB` takes `*pgxpool.Pool` directly — every other store goes through
   `internal/datastore` (`Executor`/`Store`, `WithTx`).
2. The engine defines its own local `Executor` interface duplicating `datastore.Executor`.
3. Store errors bypass the `datastore.Wrap`/`MapErr` error-hygiene convention.
4. `WebhookDeliveryTask` lives in `internal/jobs/schema.go` but is constructed, processed,
   and tested only by `modules/webhook` — payload ownership is inverted.
5. Package name is a port codename (`antree`), not a subsystem name; the kernel adapter
   lives one package away from the engine it starts.

## Decisions (recorded before moving)

- **Target layout**: `internal/queue/` = engine + lifecycle adapter in one package.
  `internal/antree/` is deleted; `internal/queue/queue.go` (the old adapter) is superseded
  by `internal/queue/module.go` (phase-4 `module.go` convention). Engine and its kernel
  wiring together: wiring is ~30 LOC and has no reason to be a separate package.
- **Naming**: keep the engine's type names — `Queue` (stdlib precedent: `url.URL`,
  `time.Time`), `QueueConfig`, `NewQueue`, `Client`, `ClientConfig`, `Task`, `TaskStatus`,
  `Retention`. Only the package path changes. No gratuitous API churn; larger API redesign
  waits for a real driver (cron, admin surface).
- **Dependency direction**: engine depends on `internal/datastore`, never on a concrete
  pool. `ClientConfig.DB *pgxpool.Pool` → `ClientConfig.Store datastore.Store`; the
  transactional save path uses `Store.WithTx` instead of manual `Begin`/`Commit`/`Rollback`.
- **Attribution**: backlite is MIT. The rename is not a relicensing — the package doc keeps
  "based on backlite" wording and `README.md` keeps the Credits section.
- **Cron/scheduler**: explicitly out of scope for this phase (owner decision deferred).
  When it lands, it should be a scheduler layer that emits delayed tasks into this engine,
  not engine semantics.

## Target Architecture

```
internal/queue/            engine + lifecycle (was internal/antree + internal/queue adapter)
  antree.go    → queue.go     Client, ClientConfig{Store, Logger, NumWorkers, ...}
  queue.go                    Queue/QueueConfig/Retention, NewQueue[T]
  task.go                     Task interface, TaskStatus
  dispatcher.go               workers, claim/release, cleanup, Notify
  store.go                    queue_tasks/queue_tasks_completed SQL (datastore.Executor)
  logger.go                   Logger/LoggerFunc
  module.go                   kernel Module: Name "queue", Start/Stop
  README.md                   built-in subsystem doc, Credits kept
internal/jobs/              domain layer: EmailTask, RecurringTask, Job registry
modules/webhook/            owns WebhookDeliveryTask (moved from internal/jobs/schema.go)
internal/registry/          single construction site: NewClient(deps.DB, ...) → deps.Queue,
                            registers queue module first (its Stop drains last)
```

Consumer direction stays one-way: `modules/*` and `internal/jobs` may depend on
`internal/queue`; `internal/queue` depends only on `internal/datastore`, `internal/kernel`
(module.go), and stdlib.

## Tasks

- [ ] Move `internal/antree/*` → `internal/queue/` with package rename `antree` → `queue`;
      fold the kernel adapter in as `module.go`; delete the old `internal/queue/queue.go`
      adapter and its test; update the package doc (built-in subsystem, backlite credit kept).
- [ ] Swap `ClientConfig.DB *pgxpool.Pool` → `Store datastore.Store`; delete the local
      `Executor` interface in favor of `datastore.Executor`; make the transactional save
      path use `WithTx`; keep the public `Add`/`Status`/`Flush*` signatures unchanged.
- [ ] Apply `datastore.Wrap("queue", op, err)` to store error returns; keep the
      `Status()` `ErrNoRows → TaskStatusNotFound` mapping.
- [ ] Move `WebhookDeliveryTask` from `internal/jobs/schema.go` to `modules/webhook/schema.go`
      and update its consumers (webhook service/tests, jobs registry test).
- [ ] Update all consumers to the `queue` import: `internal/jobs`, `internal/logger`,
      `internal/registry`, `modules/webhook`, plus tests.
- [ ] Sync docs: rewrite `internal/queue/README.md` header as built-in subsystem (Credits
      section stays), update root README + porting-guide references from `pkg/antree` /
      `internal/antree`, and record the ownership decision in `AGENTS.md` architecture notes.
- [ ] Full gate: `task test` (all three suites), `task lint`, `task check`; queue
      integration tests (testcontainers) green; no new migration, migrator count tests
      untouched.

## Validation

Behavior-preserving: no endpoint, envelope, or schema change; task rows are compatible
(`queue_tasks`/`queue_tasks_completed` untouched). Gates per the task list; integration
tests in `internal/jobs` + `modules/webhook` cover enqueue → claim → process → complete
round-trips against Postgres. A CLI smoke run (`serve` with `QUEUE_*` set) confirms the
dispatcher starts and drains on shutdown.

## Risks

- `WithTx` swap touches the save path — covered by `antree_test`/`store_test` +
  `internal/jobs/integration_test.go` (transactional enqueue case).
- Rename is wide but mechanical (import path + package clause); gofmt/lint catches strays.
- Downtime-free by construction: no schema change, task rows readable by both old and new
  package (identical SQL).

## Non-Goals

- Cron/scheduler semantics (owner decision deferred to a later phase).
- Admin API/UI for queue inspection; metrics export; multi-broker transports.

## Progress Log

- 2026-09-15 Phase planned after the ownership decision (upstream no longer tracked;
  queue is the project's own foundation). Cron explicitly deferred.
