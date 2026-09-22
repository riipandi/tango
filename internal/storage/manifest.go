package storage

import (
	"context"
	"errors"
	"fmt"

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
type File struct {
	ID          string
	Key         string
	Size        int64
	ChunkSize   int
	ChunkCount  int
	ContentHash string
	Status      string
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
	fb.Select("id", "key", "size", "chunk_size", "chunk_count", "content_hash", "status")
	fb.From(storageFilesTable)
	fb.Where(fb.Equal("key", key))

	var file File
	query, args := fb.Build()
	err := q.QueryRow(ctx, query, args...).
		Scan(&file.ID, &file.Key, &file.Size, &file.ChunkSize, &file.ChunkCount, &file.ContentHash, &file.Status)
	if errors.Is(err, datastore.ErrNoRows) {
		return Manifest{}, ErrNoManifest
	}
	if err != nil {
		return Manifest{}, fmt.Errorf("storage: load file %q: %w", key, err)
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
// previous version.
func (Manifests) Save(ctx context.Context, q datastore.Querier, manifest Manifest) error {
	file := manifest.File

	// The trailing clause rides on the builder the flavor documents for
	// exactly this shape: an upsert that hands back the row it wrote.
	ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
	ib.InsertInto(storageFilesTable)
	ib.Cols("key", "size", "chunk_size", "chunk_count", "content_hash", "status")
	ib.Values(file.Key, file.Size, file.ChunkSize, file.ChunkCount, file.ContentHash, file.Status)
	ib.SQL("ON CONFLICT (key) DO UPDATE SET " +
		"size = EXCLUDED.size, " +
		"chunk_size = EXCLUDED.chunk_size, " +
		"chunk_count = EXCLUDED.chunk_count, " +
		"content_hash = EXCLUDED.content_hash, " +
		"status = EXCLUDED.status " +
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
