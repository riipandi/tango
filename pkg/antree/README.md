# Antree

Antree is a task queue library for Go, built on PostgreSQL. It provides type-safe, persistent task
queues that run within your application — no external message broker required.

> **Origin:** a port of [Backlite](https://github.com/mikestefanello/backlite)
> (originally SQLite-based) to PostgreSQL with `pgx/v5`, UUIDv7 primary keys, `TIMESTAMPTZ`
> columns, `go-sqlbuilder` query construction, and `encoding/json/v2` payloads.

## Features

- **Embedded execution** — workers run as goroutines inside your process, no separate service needed
- **Type-safe queues** — generic `Queue[T Task]` with compile-time type checking for payloads
- **Persistent** — tasks survive process restarts via PostgreSQL
- **Automatic retries** — configurable max attempts with a fixed backoff delay
- **Atomic claims** — tasks are claimed with a conditional `UPDATE ... RETURNING`; two dispatcher
  instances never execute the same task twice
- **Delayed execution** — schedule tasks for later or set a wait duration
- **Graceful shutdown** — workers finish in-flight tasks before stopping
- **Completed task retention** — configurable policies for retaining success/failure records
- **Periodic cleanup** — automatic deletion of expired completed tasks
- **Transaction-aware** — add tasks inside existing database transactions
- **Status tracking** — query task status by ID (pending / running / success / failure / not found)

## Architecture

```mermaid
flowchart TB
    subgraph Client
        C[Client]
    end

    subgraph Dispatcher
        T[triggerer]
        F[fetcher]
        CL[cleaner]
        CH[(tasks channel)]
        W1[worker 1]
        WN[worker N]
    end

    subgraph PostgreSQL
        QT[(queue_tasks)]
        QTC[(queue_tasks_completed)]
    end

    subgraph Processors
        P1[Queue Processor]
    end

    C -->|Register queues| P1
    C -->|Add tasks| QT
    C -->|Start / Stop| T
    C -->|Start / Stop| F

    T -->|trigger| F
    F -->|claim & dispatch| CH
    CL -->|delete expired| QTC
    CH --> W1
    CH --> WN
    W1 -->|process| P1
    WN -->|process| P1
    P1 -->|success| QTC
    P1 -->|failed| QT
```

**Goroutine model:** each `Start` runs one generation of goroutines; a restart waits for the
previous generation to drain, so restarts never race a shutdown in progress.

| Goroutine     | Role                                                                   |
| ------------- | ---------------------------------------------------------------------- |
| `triggerer`   | Converts ready signals into single trigger events (debounce)           |
| `fetcher`     | Claims tasks from DB and dispatches to workers; handles backoff timers |
| `worker 1..N` | Executes task processor callbacks                                      |
| `cleaner`     | Periodically deletes expired completed tasks                           |

## Requirements

- Go >= 1.27 (generics, stdlib `uuid` for UUIDv7 generation, `encoding/json/v2`)
- PostgreSQL >= 18 (native `uuidv7()` default in the schema)

## Installation

This package lives inside the `tango` module and is not published. The app wires it in
`internal/registry`: the client is built from `deps.DB.Pool()` with `QUEUE_*` config, the
`internal/queue` kernel module starts/stops the dispatcher, and features register their queues
via `deps.Queue.Register(...)`.

Schema is owned by the migrations (`database/migrations/00010_create_queue_tables.sql`) — run
`task db:migrate`; the client never creates tables itself.

## Quick Start

### 1. Define a Task

A task is any struct that implements the `Task` interface — just provide a `Config()` method
that returns queue settings:

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/riipandi/tango/pkg/antree"
)

// EmailTask represents an email to send.
type EmailTask struct {
    To      string `json:"to"`
    Subject string `json:"subject"`
    Body    string `json:"body"`
}

func (e EmailTask) Config() antree.QueueConfig {
    return antree.QueueConfig{
        Name:        "email",
        MaxAttempts: 3,
        Timeout:     30 * time.Second,
        Backoff:     10 * time.Second, // retry after 10s on failure
        Retention: &antree.Retention{
            Duration:   7 * 24 * time.Hour, // keep completed records for 7 days
            OnlyFailed: true,               // only retain failures
            Data: &antree.RetainData{
                OnlyFailed: true,
            },
        },
    }
}
```

### 2. Create the Client

```go
pool, err := pgxpool.New(context.Background(), "postgresql://user:pass@localhost:5432/mydb")
if err != nil {
    panic(err)
}
defer pool.Close()

client, err := antree.NewClient(antree.ClientConfig{
    DB:              pool,
    NumWorkers:      5,
    ReleaseAfter:    5 * time.Minute,
    CleanupInterval: 1 * time.Hour,
})
if err != nil {
    panic(err)
}

// ... register queues, add tasks, start ...
```

### 3. Register Queues

```go
client.Register(antree.NewQueue[EmailTask](func(ctx context.Context, task EmailTask) error {
    fmt.Printf("Sending email to %s: %s\n", task.To, task.Subject)
    // ... send email ...
    return nil
}))
```

### 4. Add Tasks

```go
// Immediate
ids, err := client.Add(EmailTask{
    To: "user@example.com", Subject: "Welcome", Body: "Hello!",
}).Save()

// Delayed
ids, err = client.Add(EmailTask{
    To: "user@example.com", Subject: "Reminder", Body: "Don't forget!",
}).Wait(1 * time.Hour).Save()

// At a specific time
ids, err = client.Add(EmailTask{
    To: "user@example.com", Subject: "Scheduled", Body: "This is scheduled.",
}).At(time.Now().Add(2 * time.Hour)).Save()

// Multiple tasks in one operation
ids, err = client.Add(
    EmailTask{To: "a@example.com", Subject: "Hi A"},
    EmailTask{To: "b@example.com", Subject: "Hi B"},
).Save()
```

### 5. Start the Dispatcher

```go
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

client.Start(ctx)

// ... your application runs ...

// Graceful shutdown — waits for in-flight tasks to complete
client.Stop(context.Background())
```

## Configuration Reference

### `ClientConfig`

| Field             | Type            | Required | Description                                                                                                               |
| ----------------- | --------------- | -------- | ------------------------------------------------------------------------------------------------------------------------- |
| `DB`              | `*pgxpool.Pool` | Yes      | PostgreSQL connection pool                                                                                                |
| `Logger`          | `Logger`        | No       | Custom logger (defaults to no-op)                                                                                         |
| `NumWorkers`      | `int`           | Yes      | Number of concurrent worker goroutines (must be >= 1)                                                                     |
| `ReleaseAfter`    | `time.Duration` | Yes      | Duration after which a stuck task is released back to the queue (should exceed your longest expected task execution time) |
| `CleanupInterval` | `time.Duration` | No       | How often to delete expired completed tasks (no cleanup if zero)                                                          |

### `QueueConfig`

| Field         | Type            | Required | Description                                                     |
| ------------- | --------------- | -------- | --------------------------------------------------------------- |
| `Name`        | `string`        | Yes      | Unique queue name (duplicates or empty names panic)             |
| `MaxAttempts` | `int`           | Yes      | Maximum execution attempts before marking as completed (failed) |
| `Timeout`     | `time.Duration` | No       | Context deadline for each task execution (no timeout if zero)   |
| `Backoff`     | `time.Duration` | Yes      | Duration to wait before retrying a failed attempt               |
| `Retention`   | `*Retention`    | No       | Policy for retaining completed tasks (discarded if nil)         |

### `Retention`

| Field        | Type            | Description                                                     |
| ------------ | --------------- | --------------------------------------------------------------- |
| `Duration`   | `time.Duration` | How long to keep completed records. Zero = forever.             |
| `OnlyFailed` | `bool`          | If true, only failed tasks are retained.                        |
| `Data`       | `*RetainData`   | Policy for retaining task payload data. Nil = no data retained. |

### `RetainData`

| Field        | Type   | Description                                         |
| ------------ | ------ | --------------------------------------------------- |
| `OnlyFailed` | `bool` | If true, only retain payload data for failed tasks. |

## API Reference

### `NewClient(cfg ClientConfig) (*Client, error)`

Creates a new antree client. Validates config and initializes the internal dispatcher.

### `(*Client).Register(queue Queue)`

Registers a queue so tasks can be added to it. Panics if a queue with the same name is
registered twice, or if the queue name is empty. Must be called before `Start()`.

### `(*Client).Add(tasks ...Task) *TaskAddOp`

Starts an operation to add one or more tasks. Returns a fluent builder:

```go
// Immediate
ids, err := client.Add(myTask).Save()

// Delayed
ids, err := client.Add(myTask).Wait(5 * time.Minute).Save()

// Scheduled
ids, err := client.Add(myTask).At(futureTime).Save()

// With context
ids, err := client.Add(myTask).Ctx(requestCtx).Save()

// Inside a transaction
tx, _ := pool.Begin(ctx)
ids, err := client.Add(myTask).Tx(tx).Save()
tx.Commit(ctx)
client.Notify() // required when using Tx()
```

### `(*Client).Start(ctx context.Context)`

Starts the dispatcher background goroutines. The provided context controls the main lifecycle —
cancelling it triggers shutdown.

### `(*Client).Stop(ctx context.Context) bool`

Attempts graceful shutdown. Waits up to the context deadline. Returns `true` if all workers
finished their tasks, `false` if the context was cancelled first.

```go
stopCtx, stopCancel := context.WithTimeout(context.Background(), 30*time.Second)
defer stopCancel()
if !client.Stop(stopCtx) {
    log.Warn("some tasks did not finish gracefully")
}
```

### `(*Client).Status(ctx context.Context, taskID string) (TaskStatus, error)`

Returns the current status of a task by ID:

| Status               | Meaning                      |
| -------------------- | ---------------------------- |
| `TaskStatusPending`  | Queued but not yet executing |
| `TaskStatusRunning`  | Currently being processed    |
| `TaskStatusSuccess`  | Completed successfully       |
| `TaskStatusFailure`  | Completed with failure       |
| `TaskStatusNotFound` | No matching record found     |

### `(*Client).Notify()`

Notifies the dispatcher that a new task was added. **Only required when adding tasks inside an
external transaction** (via `TaskAddOp.Tx()`), because the dispatcher cannot observe transaction
commits.

### `(*Client).Flush(ctx context.Context) (int64, error)`

Deletes all pending (unclaimed) tasks and returns how many were removed. Claimed tasks — in
flight or awaiting release — are untouched and will finish their lifecycle normally.

```go
removed, err := client.Flush(ctx)
```

### `(*Client).FlushCompleted(ctx context.Context) (int64, error)`

Deletes all completed task records, bypassing retention expiry, and returns how many were
removed.

```go
removed, err := client.FlushCompleted(ctx)
```

### `FromContext(ctx context.Context) *Client`

Retrieves the client from a processor context, allowing processors to enqueue follow-up tasks:

```go
client.Register(antree.NewQueue[OrderTask](func(ctx context.Context, task OrderTask) error {
    antree.FromContext(ctx).Add(EmailTask{To: task.Email}).Save()
    return nil
}))
```

## Database Schema

Two tables, created by migration `database/migrations/00010_create_queue_tables.sql`:

### `queue_tasks`

| Column             | Type          | Description                                |
| ------------------ | ------------- | ------------------------------------------ |
| `id`               | `UUID`        | Primary key, `uuidv7()` default            |
| `queue`            | `TEXT`        | Queue name                                 |
| `task`             | `BYTEA`       | JSON-encoded task payload                  |
| `attempts`         | `INTEGER`     | Execution attempt counter                  |
| `wait_until`       | `TIMESTAMPTZ` | Earliest execution time (NULL = ready now) |
| `claimed_at`       | `TIMESTAMPTZ` | When a dispatcher claimed this task        |
| `last_executed_at` | `TIMESTAMPTZ` | Last execution timestamp                   |
| `created_at`       | `TIMESTAMPTZ` | Original creation time                     |

**Index:** `idx_queue_tasks_fetch` on `(wait_until ASC, id ASC)` WHERE `wait_until IS NOT NULL`

### `queue_tasks_completed`

| Column                | Type          | Description                              |
| --------------------- | ------------- | ---------------------------------------- |
| `id`                  | `UUID`        | Primary key, `uuidv7()` default          |
| `queue`               | `TEXT`        | Queue name                               |
| `attempts`            | `INTEGER`     | Total execution attempts                 |
| `last_duration_micro` | `BIGINT`      | Last execution duration in microseconds  |
| `succeeded`           | `BOOLEAN`     | Whether execution succeeded              |
| `task`                | `BYTEA`       | Retained task payload (optional)         |
| `error`               | `TEXT`        | Error message (if failed)                |
| `expires_at`          | `TIMESTAMPTZ` | Auto-deletion time (NULL = keep forever) |
| `last_executed_at`    | `TIMESTAMPTZ` | Last execution timestamp                 |
| `created_at`          | `TIMESTAMPTZ` | Original creation time                   |

**Index:** `idx_queue_tasks_completed_expires` on `(expires_at)` WHERE `expires_at IS NOT NULL`

## Transaction Support

Tasks can be added inside an existing database transaction. The task only becomes visible to the
dispatcher after the transaction commits. **You must call `Notify()` after committing**:

```go
tx, err := pool.Begin(ctx)
if err != nil {
    return err
}

if _, err = tx.Exec(ctx, "INSERT INTO orders (...) VALUES (...)"); err != nil {
    _ = tx.Rollback(ctx)
    return err
}

ids, err := client.Add(EmailTask{
    To:      "customer@example.com",
    Subject: "Order confirmed",
}).Tx(tx).Save()
if err != nil {
    _ = tx.Rollback(ctx)
    return err
}

if err := tx.Commit(ctx); err != nil {
    return err
}
client.Notify()
```

## Graceful Shutdown

```go
ctx, cancel := context.WithCancel(context.Background())
client.Start(ctx)

// ... application lifetime ...

cancel() // signal shutdown

stopCtx, stopCancel := context.WithTimeout(context.Background(), 30*time.Second)
defer stopCancel()
if client.Stop(stopCtx) {
    log.Println("All workers finished gracefully")
} else {
    log.Warn("Shutdown timed out — some tasks may be re-queued")
}
```

## Error Handling

- **Processor panics** are recovered and logged; the task is marked as failed
- **Max attempts exceeded**: task is moved to `queue_tasks_completed` with the error message
- **Backoff**: failed tasks are re-queued with `wait_until` set to `now + Backoff`
- **Stuck tasks**: `ReleaseAfter` reclaims tasks whose `claimed_at` expired; the claim wins
  atomically, so a task is never executed twice across dispatcher instances
- **Unregistered queue**: the task is discarded with an error log instead of crashing the worker

## Testing

Tests run against a real Postgres (testcontainers, Postgres 18) using the shared
`pkg/testutils.StartPostgres` helper; helpers in `helpers_test.go` manage per-test cleanup.

```bash
go test ./pkg/antree/
go test -race ./pkg/antree/
```

## Design Decisions

| Decision                              | Rationale                                                                                         |
| ------------------------------------- | ------------------------------------------------------------------------------------------------- |
| UUIDv7 primary keys                   | Time-sortable, globally unique, generated in the app so callers can reference the task pre-commit |
| Atomic claim (`UPDATE ... RETURNING`) | Contended tasks are never executed twice across dispatcher instances                              |
| TIMESTAMPTZ everywhere                | Consistent timezone handling, no ambiguity                                                        |
| go-sqlbuilder                         | Type-safe query construction, PostgreSQL flavor, no raw SQL strings                               |
| Channel-based task distribution       | Low-latency dispatch, no polling overhead                                                         |
| Non-blocking ready signal             | Prevents deadlock when triggerer exits before all producers                                       |
| Schema owned by migrations            | One source of schema truth; the client never mutates the schema                                   |
| No external dependencies for queuing  | Eliminates Redis/RabbitMQ as operational requirements                                             |

## Credits

Based on [Backlite](https://github.com/mikestefanello/backlite) by Mike Stefanello, adapted for
PostgreSQL with `pgx/v5`.
