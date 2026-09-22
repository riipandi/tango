package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/huandu/go-sqlbuilder"

	"github.com/riipandi/tango/internal/datastore"
)

// File status values. A file is pending while its chunks travel, ready once
// the manifest is committed, failed when a run gave up on it — the queue's
// own retry schedule decides when a failed one is tried again.
const (
	StatusPending = "pending"
	StatusReady   = "ready"
	StatusFailed  = "failed"
)

// The manifest tables the builders name, the same one-constant-per-table
// rule the queue's store follows.
const (
	storageFilesTable  = "storage_files"
	storageChunksTable = "storage_chunks"
)

// File is one stored file and the settings its chunks were cut with. The
// content hash is the root hash of the chunk list: one read tells whether
// the bytes on the staging side still match what the backend holds.
//
// Metadata is the feature's own free-form record — content type, original
// file name, owner — carried and rewritten but never interpreted here.
// StagingSize and StagingMtime fingerprint the staging file the chunk list
// was computed from: a retry that finds both unchanged reuses the stored
// chunk list instead of hashing the file again.
type File struct {
	ID           string
	Key          string
	Size         int64
	ChunkSize    int
	ChunkCount   int
	ContentHash  string
	Status       string
	Metadata     map[string]any
	ChunksDone   int
	StagingSize  int64
	StagingMtime time.Time
}

// Manifest is one file and the chunks it is made of, in file order.
type Manifest struct {
	File   File
	Chunks []Chunk
}

// ErrNoManifest is what Load answers for a key nothing stored yet.
var ErrNoManifest = errors.New("storage: no manifest for key")

// Manifests reads and writes the manifest tables. Every method takes the
// Querier to run on, so a caller that must be transactional passes its
// transaction and one that must not passes the pool — the repository never
// opens a transaction of its own, the same rule the seeders follow.
type Manifests struct{}

// NewManifests builds the manifest repository.
func NewManifests() *Manifests { return &Manifests{} }

// Load reads the manifest a key holds. ErrNoManifest for a key nothing
// stored yet — the answer that makes an upload the file's first.
func (Manifests) Load(ctx context.Context, q datastore.Querier, key string) (Manifest, error) {
	fb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	fb.Select(
		"id", "key", "size", "chunk_size", "chunk_count", "content_hash", "status",
		"metadata", "chunks_done", "staging_size", "staging_mtime",
	)
	fb.From(storageFilesTable)
	fb.Where(fb.Equal("key", key))

	var file File
	var metadata []byte
	query, args := fb.Build()
	err := q.QueryRow(ctx, query, args...).Scan(
		&file.ID, &file.Key, &file.Size, &file.ChunkSize, &file.ChunkCount, &file.ContentHash, &file.Status,
		&metadata, &file.ChunksDone, &file.StagingSize, &file.StagingMtime,
	)
	if errors.Is(err, datastore.ErrNoRows) {
		return Manifest{}, ErrNoManifest
	}
	if err != nil {
		return Manifest{}, fmt.Errorf("storage: load file %q: %w", key, err)
	}
	if err := json.Unmarshal(metadata, &file.Metadata); err != nil {
		return Manifest{}, fmt.Errorf("storage: decode metadata of %q: %w", key, err)
	}

	cb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	cb.Select("chunk_index", "hash", "size")
	cb.From(storageChunksTable)
	cb.Where(cb.Equal("file_id", file.ID))
	cb.OrderBy("chunk_index")

	query, args = cb.Build()
	rows, err := q.Query(ctx, query, args...)
	if err != nil {
		return Manifest{}, fmt.Errorf("storage: load chunks of %q: %w", key, err)
	}
	defer rows.Close()

	var manifest Manifest
	manifest.File = file
	for rows.Next() {
		var chunk Chunk
		if err := rows.Scan(&chunk.Index, &chunk.Hash, &chunk.Size); err != nil {
			return Manifest{}, fmt.Errorf("storage: scan chunk of %q: %w", key, err)
		}
		manifest.Chunks = append(manifest.Chunks, chunk)
	}
	return manifest, rows.Err()
}

// Save commits one manifest: the file row upserted, the chunk rows replaced.
// It runs inside the caller's transaction, so a manifest is visible whole or
// not at all — a read never sees a file half of whose chunks belong to the
// previous version. A pending save is the checkpoint a retry resumes from;
// a ready save is the finished one.
func (Manifests) Save(ctx context.Context, q datastore.Querier, manifest Manifest) error {
	file := manifest.File
	metadata, err := json.Marshal(file.Metadata)
	if err != nil {
		return fmt.Errorf("storage: encode metadata of %q: %w", file.Key, err)
	}

	// The trailing clause rides on the builder the flavor documents for
	// exactly this shape: an upsert that hands back the row it wrote.
	ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
	ib.InsertInto(storageFilesTable)
	ib.Cols(
		"key", "size", "chunk_size", "chunk_count", "content_hash", "status",
		"metadata", "chunks_done", "staging_size", "staging_mtime",
	)
	ib.Values(
		file.Key, file.Size, file.ChunkSize, file.ChunkCount, file.ContentHash, file.Status,
		metadata, file.ChunksDone, file.StagingSize, nullableTime(file.StagingMtime),
	)
	ib.SQL("ON CONFLICT (key) DO UPDATE SET " +
		"size = EXCLUDED.size, " +
		"chunk_size = EXCLUDED.chunk_size, " +
		"chunk_count = EXCLUDED.chunk_count, " +
		"content_hash = EXCLUDED.content_hash, " +
		"status = EXCLUDED.status, " +
		"metadata = EXCLUDED.metadata, " +
		"chunks_done = EXCLUDED.chunks_done, " +
		"staging_size = EXCLUDED.staging_size, " +
		"staging_mtime = EXCLUDED.staging_mtime " +
		"RETURNING id")

	var id string
	query, args := ib.Build()
	if err := q.QueryRow(ctx, query, args...).Scan(&id); err != nil {
		return fmt.Errorf("storage: save file %q: %w", file.Key, err)
	}

	// The chunk rows are rewritten whole: a diff against the old rows is
	// what decided the upload list, and the diff was computed before this
	// write, so the table only ever needs to describe the newest version.
	cb := sqlbuilder.PostgreSQL.NewDeleteBuilder()
	cb.DeleteFrom(storageChunksTable)
	cb.Where(cb.Equal("file_id", id))

	query, args = cb.Build()
	if _, err := q.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("storage: clear chunks of %q: %w", file.Key, err)
	}

	// One multi-row insert for the whole manifest: a row per chunk is what
	// a large file needs, and the batch is all-or-nothing with the delete
	// above under the caller's transaction.
	if len(manifest.Chunks) > 0 {
		ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
		ib.InsertInto(storageChunksTable)
		ib.Cols("file_id", "chunk_index", "hash", "size")
		for _, chunk := range manifest.Chunks {
			ib.Values(id, chunk.Index, chunk.Hash, chunk.Size)
		}

		query, args = ib.Build()
		if _, err := q.Exec(ctx, query, args...); err != nil {
			return fmt.Errorf("storage: save chunks of %q: %w", file.Key, err)
		}
	}
	return nil
}

// Stage records the intent to store a file: the row exists before the first
// byte travels, carrying the feature's metadata and the staging fingerprint.
// An upload that never arrives leaves a pending row the next Stage or Sync
// of the same key overwrites, not a half-stored file.
func (Manifests) Stage(ctx context.Context, q datastore.Querier, key string, size int64, mtime time.Time, metadata map[string]any) error {
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("storage: encode metadata of %q: %w", key, err)
	}

	ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
	ib.InsertInto(storageFilesTable)
	ib.Cols("key", "size", "chunk_size", "chunk_count", "content_hash", "status", "metadata", "staging_size", "staging_mtime")
	ib.Values(key, size, 1, 0, "", StatusPending, encoded, size, nullableTime(mtime))
	ib.SQL("ON CONFLICT (key) DO UPDATE SET " +
		"metadata = EXCLUDED.metadata, " +
		"staging_size = EXCLUDED.staging_size, " +
		"staging_mtime = EXCLUDED.staging_mtime, " +
		"status = EXCLUDED.status, " +
		"chunks_done = 0")

	query, args := ib.Build()
	if _, err := q.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("storage: stage file %q: %w", key, err)
	}
	return nil
}

// UpdateMetadata replaces the metadata a key carries, keeping everything
// else — the manifest, the status, the chunks — exactly as it is. A key
// nothing stored yet gets a pending row, so metadata can be set before or
// after the bytes travel.
func (Manifests) UpdateMetadata(ctx context.Context, q datastore.Querier, key string, metadata map[string]any) error {
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("storage: encode metadata of %q: %w", key, err)
	}

	ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
	ib.InsertInto(storageFilesTable)
	ib.Cols("key", "chunk_size", "content_hash", "status", "metadata")
	ib.Values(key, 1, "", StatusPending, encoded)
	ib.SQL("ON CONFLICT (key) DO UPDATE SET metadata = EXCLUDED.metadata")

	query, args := ib.Build()
	if _, err := q.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("storage: update metadata of %q: %w", key, err)
	}
	return nil
}

// SetProgress pins the progress counter to an absolute value, the reset a
// resumed round starts from: the chunks the backend already holds are the
// distance the counter opens with.
func (Manifests) SetProgress(ctx context.Context, q datastore.Querier, key string, done int) error {
	ub := sqlbuilder.PostgreSQL.NewUpdateBuilder()
	ub.Update(storageFilesTable)
	ub.Set(ub.Assign("chunks_done", done))
	ub.Where(ub.Equal("key", key))

	query, args := ub.Build()
	if _, err := q.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("storage: set progress of %q: %w", key, err)
	}
	return nil
}

// BumpProgress counts chunks the current upload round has finished. The
// increment is one UPDATE per landed chunk — a raw assignment, since the
// builder's Incr cannot add more than one — so parallel workers add their
// own counts without a read-modify-write race.
func (Manifests) BumpProgress(ctx context.Context, q datastore.Querier, key string, done int) error {
	ub := sqlbuilder.PostgreSQL.NewUpdateBuilder()
	ub.Update(storageFilesTable)
	ub.Set(fmt.Sprintf("chunks_done = chunks_done + %d", done))
	ub.Where(ub.Equal("key", key))

	query, args := ub.Build()
	if _, err := q.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("storage: bump progress of %q: %w", key, err)
	}
	return nil
}

// Delete removes a file and its chunk rows. It returns the hashes the file
// referenced: the caller diffs them against the references the other files
// still hold before removing any bytes from the backend.
func (Manifests) Delete(ctx context.Context, q datastore.Querier, key string) ([]string, error) {
	// The hashes are read before the rows go; the subquery names the file
	// by its key, so the read and the delete below answer the same rows.
	fb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	fb.Select("id")
	fb.From(storageFilesTable)
	fb.Where(fb.Equal("key", key))

	cb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	cb.Select("DISTINCT hash")
	cb.From(storageChunksTable)
	cb.Where(cb.In("file_id", fb))

	query, args := cb.Build()
	rows, err := q.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: list chunks of %q: %w", key, err)
	}
	var hashes []string
	for rows.Next() {
		var hash string
		if err := rows.Scan(&hash); err != nil {
			rows.Close()
			return nil, fmt.Errorf("storage: scan chunk of %q: %w", key, err)
		}
		hashes = append(hashes, hash)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("storage: list chunks of %q: %w", key, err)
	}
	rows.Close()

	db := sqlbuilder.PostgreSQL.NewDeleteBuilder()
	db.DeleteFrom(storageFilesTable)
	db.Where(db.Equal("key", key))

	query, args = db.Build()
	tag, err := q.Exec(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: delete file %q: %w", key, err)
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return hashes, nil
}

// ReferencedHashes lists every chunk hash at least one file still points at.
// It is the keep-set the garbage collection diffs the backend's listing
// against: a chunk the backend holds and this set does not is unreferenced.
func (Manifests) ReferencedHashes(ctx context.Context, q datastore.Querier) (map[string]struct{}, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("DISTINCT hash")
	sb.From(storageChunksTable)

	query, args := sb.Build()
	rows, err := q.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: list referenced chunks: %w", err)
	}
	defer rows.Close()

	hashes := make(map[string]struct{})
	for rows.Next() {
		var hash string
		if err := rows.Scan(&hash); err != nil {
			return nil, fmt.Errorf("storage: scan referenced chunk: %w", err)
		}
		hashes[hash] = struct{}{}
	}
	return hashes, rows.Err()
}

// nullableTime hands a zero time to the driver as NULL, the value an absent
// staging fingerprint is.
func nullableTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}
