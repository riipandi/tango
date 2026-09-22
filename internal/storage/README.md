# Storage

Storage is tango's file engine: chunked, content-addressed uploads over a local filesystem or
an S3-compatible object store, with the manifest in PostgreSQL. A file is never stored whole
and never held in memory whole — the request path is one local disk write, everything else
runs on the durable queue.

> **Relation to the queue:** storage is the payload; the queue (`internal/queue`) is the
> executor. A staged file is noticed by the watcher (or named by any caller) as a
> `ChunkUploadTask`, and the queue owns execution, retries, backoff, and the archive. A sync
> is idempotent, so a replayed task finishes the round the crash interrupted.
>
> **Relation to presigned multipart uploads:** S3's native multipart moves parts through the
> client, which ties the upload to one live session and one client. Storage's chunk-per-object
> shape keeps the manifest durable in Postgres, so a retry resumes from the checkpoint with no
> session to re-establish — and the same code serves the local driver, which has no multipart.

## Features

- **Chunked, content-addressed storage** — a file is split into `storage.chunk_size` chunks,
  each named by its SHA-256 (`chunks/<2 hex>/<hash>`); two files sharing a chunk share one
  object, a changed file re-uploads only its changed chunks, and a retried upload is
  idempotent by construction
- **Manifest in PostgreSQL** — `storage_files` + `storage_chunks` (migration `00009`) are the
  durable diff source; the root hash over the chunk hashes is the one-value "nothing to
  upload" check
- **Cheap request path** — `Stage` writes the file to the staging directory through a temp
  file and a rename and commits the intent row; the watcher never sees a half-written name
- **Resumable uploads** — a pending manifest whose staging fingerprint (size + mtime) matches
  is reused whole: a retry of a large file skips the hashing pass and resumes from the chunks
  the backend still lacks
- **Parallel hashing and uploading** — files of 16 MiB or more are hashed in parallel (each
  worker reads at its own chunk offset); uploads run through an errgroup bounded to the
  worker budget; a sync costs `workers × chunk_size` of memory, never the file size
- **Batched backend probe** — one probe per two-hex prefix (a handful of LISTs), never one
  HEAD per chunk
- **Flexible, validated keys** — `storage.Key("avatar", userID, "128.png")` composes the
  multi-purpose name a feature owns; every manager entry point refuses traversal, absolute,
  and hidden segments before a path is built
- **Per-file metadata** — a JSONB column the feature owns (content type, original file name,
  owner); written at stage time, rewritten by `UpdateMetadata`, carried but never read by the
  engine
- **Upload progress** — the workers bump `chunks_done` as chunks land; `Manager.Progress`
  reads it (the endpoint wiring is still a stub — see `llms/architecture.md`,
  TODO(notification))
- **Upload hooks** — `WithBeforeSync` (the gate: validate, preprocess) and `WithAfterSync`
  (the post-processing point: thumbnail, notification), nil by default, idempotence their
  contract
- **File watching** — `fsnotify` over every directory under staging (nested keys are
  first-class), per-path debounce, a start-up scan that picks up an upload that outlived its
  process
- **Garbage collection as a recurring job** — `storage_gc` sweeps the backend against the
  referenced hashes, draining whatever a crash left
- **Streamed reads** — `Manager.Open` returns an `io.ReadCloser` that fetches chunks on
  demand; no whole-file buffer exists on the read path either
- **Graceful shutdown** — the watcher stops through context cancellation; in-flight syncs are
  queue tasks and finish through the queue's own drain

## Architecture

```mermaid
flowchart TB
    subgraph Request path
        H[Feature handler] -->|Stage: write + intent row| ST[(staging dir)]
        H -->|metadata + fingerprint| SF[(storage_files)]
    end

    subgraph Watcher
        W[fsnotify + debounce] -->|ChunkUploadTask| Q[queue]
    end

    subgraph Sync (queue worker)
        Q -->|claim| S[Manager.Sync]
        S -->|reuse or hash| CH[Chunker]
        S -->|diff| MF[(storage_chunks)]
        S -->|missing chunks| BE[Backend]
        S -->|ready manifest| SF
        S -->|remove| ST
    end

    subgraph Backend
        FS[Local FS]
        S3[S3-compatible]
    end

    BE --> FS
    BE --> S3

    GC[storage_gc job] -->|sweep unreferenced| BE
```

**One sync, three fast paths.** The ready manifest with a matching root hash uploads nothing;
the pending manifest with a matching staging fingerprint skips the hashing pass; the diff
uploads only the chunks the probe did not find. Everything else in the round is the same code
a first upload runs.

**Crash safety by ordering.** The chunk list is checkpointed before the first chunk travels,
the ready manifest commits after the last, and the staging copy is removed only if its
fingerprint still matches what the sync read. A crash anywhere leaves either a staging file
to re-sync or unreferenced chunks to collect — never a manifest naming a missing chunk, and
never a deleted file whose re-stage was destroyed mid-round.

## Requirements

- Go >= 1.27
- PostgreSQL >= 18 (the manifest tables; shared `datastore` pool)
- One backend: the local filesystem, or any S3-compatible service (aws-sdk-go-v2; path style
  for MinIO/Silo)
- `internal/queue` — the upload and the garbage collection run as queue jobs
- Docker for the tests (testcontainers: Postgres, MinIO)

## Wiring

The package lives inside the `tango` module and is not published. The composition root wires
it in `internal/registry`: the manager is built from the shared `datastore.Postgres` pool,
the configured backend, and the `storage` config section; the watcher is built over the
manager's staging directory and enqueues `ChunkUploadTask` through the queue client. The
storage jobs are registered in `internal/jobs/register.go` (`Register` skips them when the
manager is absent, so a build without storage runs the rest unchanged).

Schema is owned by the migration (`database/migrations/00009_create_filestore_tables.sql`) —
run `task db:migrate`; the engine never creates tables itself.

The engine logs through `log/slog` — the process logger `serve` hands over — so storage
lines reach every configured sink and carry the trace context of the run.

## Quick Start

### 1. Stage a File

Staging is the whole request path: one buffered local write and one intent row.

```go
// Key composes the feature's own naming; each part becomes one safe
// path segment. ValidateKey refuses any key that could escape staging.
key, err := storage.Key("avatar", userID, "128.png")
if err != nil {
    return err
}

err = manager.Stage(ctx, key, r, map[string]any{
    "content_type": contentType,
    "original":     filename,
    "owner_id":     userID,
})
```

From here the watcher (or the feature itself, via `client.Add(jobs.ChunkUploadTask{Key: key})`)
sends the upload to the queue. The feature does not wait for it.

### 2. Read, Update, Delete

```go
// One stream that fetches chunks on demand.
rc, err := manager.Open(ctx, key)
defer func() { _ = rc.Close() }()

// The manifest a feature reads to know what the backend holds.
manifest, err := manager.Manifest(ctx, key)
progress, err := manager.Progress(ctx, key) // status, done/total, size

// Metadata is rewritten any time; chunks and manifest are untouched.
err = manager.UpdateMetadata(ctx, key, map[string]any{"original": "renamed.png"})

// Deletion keeps any chunk another file still references.
err = manager.Delete(ctx, key)
```

### 3. Hooks

Both are optional and wired in the registry, where the feature that owns the behavior lives:

```go
manager.
    WithBeforeSync(func(ctx context.Context, key, path string) error {
        // The gate: runs before the fingerprint is read, so a rewrite
        // here is what gets hashed. An error refuses the file.
        return validateImage(path)
    }).
    WithAfterSync(func(ctx context.Context, manifest storage.Manifest) error {
        // The post-processing point: every chunk is durable, the
        // manifest is ready, the staging copy still exists.
        return client.Add(ThumbnailTask{Key: manifest.File.Key}).Save()
    })
```

**Contracts** (documented on the types, enforced by review):

- **Idempotent** — the queue's retries replay both hooks; a failed after-hook is retried
  through the finished-manifest fast path, without re-hashing or re-uploading
- **Quick** — long work belongs in a job the hook enqueues, not in the upload worker's slot
- **Before** receives the staging path and may rewrite or replace the file; an error fails
  the round, and a hook that keeps refusing a file walks it to the dead letters

### 4. Garbage Collection

`storage_gc` runs on its own schedule (`DefaultStorageGCInterval`, six hours) and needs
nothing from a feature. To collect immediately — rarely needed outside tests:

```go
removed, err := manager.CollectGarbage(ctx)
```

## Configuration

### `storage` section

| Key                     | Default   | Description                                                                    |
| ----------------------- | --------- | ------------------------------------------------------------------------------ |
| `storage.driver`        | `local`   | `local` or `s3`; only the selected backend's client is built                    |
| `storage.local_path`    | `storage` | The one data directory: chunk files, `staging/`, and the log sink live under it |
| `storage.chunk_size`    | 8388608   | Chunk size in bytes (8 MiB); also the parallel passes' memory unit              |
| `storage.watch.enable`  | true      | The staging watcher enqueues uploads as staging paths settle                    |
| `storage.watch.debounce`| 2         | Seconds a path must stay quiet before its upload is enqueued                    |
| `storage.s3.*`          | —         | `access_key_id`, `access_key_secret` (both secrets), `bucket_name`, `endpoint_url`, `force_path_style`, `path_prefix`, `region` |

The local path needs no flag and no variable of its own — a deployment sets it in the config
file. Postgres plus local storage are enough; no backend is required.

### Manager construction

| Parameter | Meaning                                                                      |
| --------- | ---------------------------------------------------------------------------- |
| `store`   | The backend (`New` builds the one `storage.driver` selects)                   |
| `db`      | The shared Postgres pool (`Querier` + `WithTx`)                               |
| `chunkSize` | Bytes per chunk; refused at construction when not positive                  |
| `staging` | The staging directory (the registry joins `storage.local_path` + `staging`)   |
| `uploads` | Parallel chunk budget; non-positive falls back to four                        |

## API Reference

### `New(store Store, db DB, chunkSize int, staging string, uploads int) (*Manager, error)`

Builds the engine: the chunker, the manifest store, the staging directory. Nothing touches
the backend or the database yet.

### `(*Manager).Stage(ctx, key, r io.Reader, metadata map[string]any) error`

Writes the file into staging through a temp file + rename and commits the intent row —
metadata and the staging fingerprint — before the first byte travels. Re-staging a key
replaces its metadata and makes the stored manifest stale, so the next sync uploads the new
version.

### `(*Manager).Sync(ctx, key) error`

The upload round the `ChunkUploadTask` processor runs: hook gate → fingerprint → reuse or
hash → diff → parallel upload → ready commit → after hook → staging cleanup. Idempotent; the
queue's retries replay it. A key with no staging file returns nil — another attempt already
finished.

### `(*Manager).Open(ctx, key) (io.ReadCloser, error)`

A stream that assembles the file from its chunks, fetching each when the reader reaches it.
`ErrNotFound` for a key nothing stored.

### `(*Manager).Manifest(ctx, key) (Manifest, error)` / `(*Manager).Progress(ctx, key) (Progress, error)`

The stored state a feature reads: chunk list, status, content hash, metadata — and the
progress shape (`Status`, `Done`, `Total`, `Size`) a status endpoint would serve.

### `(*Manager).UpdateMetadata(ctx, key, metadata map[string]any) error`

Replaces the metadata; works before and after the upload (a key nothing stored yet gets a
pending row).

### `(*Manager).Delete(ctx, key) error`

Removes the manifest rows and then the chunks nothing else references; the staging copy goes
with it.

### `(*Manager).CollectGarbage(ctx) (int, error)`

Removes every backend chunk no manifest references; returns the count.

### `Key(parts ...string) (string, error)` / `ValidateKey(key string) error`

The naming helpers. `Key` sanitizes each part onto one path segment (letters, digits, dot,
dash, underscore stay; the rest becomes `_`); both refuse empty parts, `.`/`..`, hidden
segments, absolute paths, and separators where one part was asked for.

### `NewChunker(size int) (*Chunker, error)`

`Split` hashes one stream sequentially; `ParallelSplit` hashes by offset with a bounded
worker pool; `ReadAt` reads one chunk's bytes for the upload pass; `RootHash` folds the chunk
hashes into the one-value change check.

### `Store`

The backend contract: `PutChunk`, `GetChunk`, `HasChunks` (batched), `DeleteChunk`,
`ListChunks`. `storage.New(cfg)` builds the one the `storage.driver` section names (`FS` over
`storage.local_path`, `S3` over `storage.s3.*`); both translate their protocol's not-found
shape into `ErrNotFound` at the edge.

## Database Schema

Two tables, created by migration `database/migrations/00009_create_filestore_tables.sql`:

### `storage_files`

| Column          | Type          | Description                                                        |
| --------------- | ------------- | ------------------------------------------------------------------ |
| `id`            | `UUID`        | Primary key, `uuidv7()` default                                     |
| `key`           | `TEXT`        | The feature's naming, unique                                       |
| `size`          | `BIGINT`      | File size in bytes                                                 |
| `chunk_size`    | `INTEGER`     | Bytes per chunk this manifest was cut with (`> 0`)                 |
| `chunk_count`   | `INTEGER`     | Number of chunks                                                   |
| `content_hash`  | `TEXT`        | SHA-256 over the chunk hashes in order — the one-value diff        |
| `status`        | `TEXT`        | `pending` / `ready` / `failed` (`chk_storage_files_status`)        |
| `metadata`      | `JSONB`       | The feature's own record; the engine carries it, never reads it    |
| `chunks_done`   | `INTEGER`     | Chunks the current round has landed                                |
| `staging_size`  | `BIGINT`      | Fingerprint half one: the staging file's size                      |
| `staging_mtime` | `TIMESTAMPTZ` | Fingerprint half two: the staging file's modification time         |
| `created_at` / `updated_at` | `TIMESTAMPTZ` | `updated_at` maintained by trigger                    |

Indexes on `status` and `content_hash`; named checks guard the status vocabulary, the chunk
size, and the counters.

### `storage_chunks`

| Column        | Type          | Description                                          |
| ------------- | ------------- | ---------------------------------------------------- |
| `file_id`     | `UUID`        | The file this chunk belongs to (`ON DELETE CASCADE`) |
| `chunk_index` | `INTEGER`     | Position in the file; part of the primary key        |
| `hash`        | `TEXT`        | SHA-256 of the chunk's bytes — the backend address (`^[0-9a-f]{64}$`) |
| `size`        | `INTEGER`     | Chunk bytes (`> 0`)                                  |
| `uploaded_at` | `TIMESTAMPTZ` | When the chunk landed in the backend                 |

Indexed on `hash` — the keep-set read that deletion and garbage collection compare against.

## Error Handling

- **A refused key** — `ErrInvalidKey` before any path is built or any byte moves; traversal
  shapes never reach the filesystem
- **A failed sync** — the queue retries it; the checkpoint makes the retry cheap, and a round
  that exhausts its attempts rests in the archive with its error
- **A failed after-hook** — the data is already durable; the retry replays the hook through
  the fast path
- **A re-staged file under a running sync** — the TOCTOU guard leaves it for a fresh round
  and fails this one, so nothing is destroyed unrecorded
- **A lost staging file** — `Sync` returns nil: another attempt finished, or the caller owns
  the lifecycle
- **An orphaned chunk** — a crash between `PutChunk` and the manifest commit leaves one for
  `storage_gc`; it is never a manifest naming a missing chunk

## Testing

Tests run against real containers (testcontainers) using the shared `pkg/testutils` helpers:
`StartPostgres` per test database (migrations applied), `StartMinIO` for the S3 driver. Each
fixture uses salted data, so two tests — or two runs — sharing one bucket cannot answer each
other's probes.

```bash
go test ./internal/storage/
go test -race ./internal/storage/
```

## Design Decisions

| Decision | Rationale |
| -------- | --------- |
| Content-addressed chunks | Dedupe, precise diffs, and idempotent retries come from one naming rule |
| Manifest in Postgres, not sidecar files | Migrations are the schema truth; the manifest survives a lost disk and is queryable |
| Staging first, upload on the queue | The request path is one local write; a slow backend never sits inside a request |
| Chunk-per-object, not S3 multipart | A retry resumes from the durable checkpoint, and both drivers share the shape |
| Fingerprint (size + mtime) reuse | A retry skips the hashing pass — the one cost a large file cannot afford twice |
| Parallel passes bounded by a worker budget | `workers × chunk_size` memory, whatever the file size; one knob for hashing and uploading |
| `ReadAt` per chunk, never shared `Seek` | Concurrent reads of one file race on `Seek`/`Read`; offsets do not |
| Hooks at the round's edges, nil default | Features get validation and post-processing exactly once per finished upload, with zero cost when absent |
| Idempotence as the hooks' contract | The queue retries the round; a hook that cannot be replayed cannot be safe |
| Watcher with per-path debounce and a start-up scan | A burst of writes is one upload; an upload that outlived its process is not lost |
| Garbage collection as a recurring job | Crash leftovers are swept on a clock instead of a goroutine the process must babysit |
| Keys validated at every entry point | The staging path is built from the key; the door is the only place a traversal can be stopped |
| Schema owned by migrations | One source of schema truth; the engine never mutates the schema |
