package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"

	"golang.org/x/sync/errgroup"

	"github.com/riipandi/tango/internal/datastore"
)

// DB is the persistence surface the manager needs: plain reads on the shared
// Querier, writes inside the transaction WithTx owns. datastore.Postgres
// satisfies it; the indirection keeps the manager testable against a
// transaction the test owns.
type DB interface {
	datastore.Querier
	WithTx(ctx context.Context, fn func(ctx context.Context, tx datastore.Querier) error) error
}

// Manager is the file-level engine over a Store: staging, the chunked
// upload, the assembled read, and the deletion. Features hold this, not a
// backend; which backend answers is the configuration's business.
type Manager struct {
	store     Store
	db        DB
	manifests *Manifests
	chunker   *Chunker
	staging   string
	// uploads caps the chunk uploads in flight. Each holds one chunk buffer,
	// so this — not the file size — is what a sync costs in memory.
	uploads int
}

// NewManager builds the engine. staging is the directory a caller writes the
// next file into; uploads is the parallel chunk upload budget. A non-positive
// budget falls back to four, so a misconfigured run cannot serialize uploads
// by accident or spend its memory on a hundred of them.
func NewManager(store Store, db DB, chunkSize int, staging string, uploads int) (*Manager, error) {
	chunker, err := NewChunker(chunkSize)
	if err != nil {
		return nil, err
	}
	if uploads <= 0 {
		uploads = 4
	}
	return &Manager{
		store:     store,
		db:        db,
		manifests: NewManifests(),
		chunker:   chunker,
		staging:   staging,
		uploads:   uploads,
	}, nil
}

// Manifest reads the stored manifest of a key, the version a feature reads
// to know what the backend holds. ErrNotFound for a key nothing stored yet.
func (m *Manager) Manifest(ctx context.Context, key string) (Manifest, error) {
	manifest, err := m.manifests.Load(ctx, m.db, key)
	if errors.Is(err, ErrNoManifest) {
		return Manifest{}, ErrNotFound
	}
	return manifest, err
}

// Staging is the directory the next file is written into. The watcher owns
// it; a caller that does not run the watcher names the same directory.
func (m *Manager) Staging() string { return m.staging }

// Stage writes a file into the staging directory — the one write on the
// request path, local and buffered, so an API handler pays a disk write and
// nothing else. The bytes move to the backend later, on the queue.
func (m *Manager) Stage(ctx context.Context, key string, r io.Reader) error {
	if err := os.MkdirAll(m.staging, 0o755); err != nil {
		return fmt.Errorf("storage: staging directory: %w", err)
	}
	// The temp file makes a half-written staging file invisible: the watcher
	// only ever sees the final name, complete or absent. A key may name a
	// subdirectory, so the target directory exists before the rename.
	target := m.stagingPath(key)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("storage: staging %q: %w", key, err)
	}
	tmp, err := os.CreateTemp(m.staging, "."+filepath.Base(key)+".*")
	if err != nil {
		return fmt.Errorf("storage: staging %q: %w", key, err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	if _, err := io.Copy(tmp, r); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("storage: staging %q: %w", key, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("storage: staging %q: %w", key, err)
	}
	if err := os.Rename(tmp.Name(), target); err != nil {
		return fmt.Errorf("storage: staging %q: %w", key, err)
	}
	return nil
}

// Sync uploads a staging file: it computes the chunk manifest, uploads only
// the chunks that differ from the stored manifest — in parallel, bounded by
// the upload budget — and commits the new manifest in one transaction. It is
// the body of the upload job; idempotent, so the queue's retries replay it.
func (m *Manager) Sync(ctx context.Context, key string) error {
	path := m.stagingPath(key)
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		// A key with no staging file has nothing to sync: either another
		// attempt already finished, or the file was staged by a caller
		// that manages its own lifecycle.
		return nil
	}
	if err != nil {
		return fmt.Errorf("storage: open staging %q: %w", key, err)
	}
	defer func() { _ = f.Close() }()

	// Pass one learns the manifest. The file is opened once and read
	// through ReaderAt in both passes, so the OS page cache serves the
	// second one.
	size, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return fmt.Errorf("storage: size staging %q: %w", key, err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("storage: rewind staging %q: %w", key, err)
	}
	chunks, err := m.chunker.Split(f)
	if err != nil {
		return err
	}
	contentHash := RootHash(chunks)

	prior, err := m.manifests.Load(ctx, m.db, key)
	if err != nil && !errors.Is(err, ErrNoManifest) {
		return err
	}

	// The root hash is the fast path: same content, nothing to upload. The
	// manifest is rewritten anyway, so the stored row carries the chunk
	// sizes this run computed even if the chunking changed.
	if len(prior.Chunks) > 0 && prior.File.ContentHash == contentHash {
		prior.File.Size = size
		prior.File.ChunkSize = m.chunker.size
		prior.File.ChunkCount = len(chunks)
		prior.File.Status = StatusReady
		if err := m.db.WithTx(ctx, func(ctx context.Context, tx datastore.Querier) error {
			return m.manifests.Save(ctx, tx, prior)
		}); err != nil {
			return err
		}
		return os.Remove(path)
	}

	if err := m.uploadMissing(ctx, key, chunks); err != nil {
		return err
	}

	manifest := Manifest{
		File: File{
			Key:         key,
			Size:        size,
			ChunkSize:   m.chunker.size,
			ChunkCount:  len(chunks),
			ContentHash: contentHash,
			Status:      StatusReady,
		},
		Chunks: chunks,
	}
	if err := m.db.WithTx(ctx, func(ctx context.Context, tx datastore.Querier) error {
		return m.manifests.Save(ctx, tx, manifest)
	}); err != nil {
		return err
	}

	// The manifest is committed: the staging copy's bytes live in the
	// backend, chunk for chunk, so the local file has served its purpose.
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("storage: clear staging %q: %w", key, err)
	}
	slog.InfoContext(ctx, "storage: file synced",
		"key", key, "chunks", len(chunks), "size", size)
	return nil
}

// uploadMissing puts every chunk the backend does not hold yet, several at a
// time. The worker count is the budget; each worker borrows one buffer from
// the pool, so a sync's memory is budget × chunk size, never the file size.
func (m *Manager) uploadMissing(ctx context.Context, key string, chunks []Chunk) error {
	pending := make([]Chunk, 0, len(chunks))
	for _, chunk := range chunks {
		ok, err := m.store.HasChunk(ctx, chunk.Hash)
		if err != nil {
			return fmt.Errorf("storage: probe chunk %s: %w", chunk.Hash, err)
		}
		if !ok {
			pending = append(pending, chunk)
		}
	}
	if len(pending) == 0 {
		return nil
	}

	f, err := os.Open(m.stagingPath(key))
	if err != nil {
		return fmt.Errorf("storage: open staging %q: %w", key, err)
	}
	defer func() { _ = f.Close() }()

	// One buffer per worker slot, handed out and returned: the allocations
	// happen once, the uploads reuse them for the whole file.
	buffers := make(chan []byte, m.uploads)
	for range m.uploads {
		buffers <- make([]byte, m.chunker.size)
	}

	group, ctx := errgroup.WithContext(ctx)
	group.SetLimit(m.uploads)
	for _, chunk := range pending {
		group.Go(func() error {
			buf := <-buffers
			defer func() { buffers <- buf }()

			data, err := m.chunker.ReadAt(f, chunk, buf)
			if err != nil {
				return err
			}
			return m.store.PutChunk(ctx, chunk.Hash, data)
		})
	}
	if err := group.Wait(); err != nil {
		return fmt.Errorf("storage: upload %q: %w", key, err)
	}
	return nil
}

// Open reads a stored file back as one stream: chunks are fetched in order
// behind an io.Reader, so a caller never sees the chunking.
func (m *Manager) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	manifest, err := m.manifests.Load(ctx, m.db, key)
	if errors.Is(err, ErrNoManifest) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &chunkReader{ctx: ctx, manager: m, manifest: manifest}, nil
}

// Delete removes a file's manifest and the chunks nothing else references —
// a chunk shared with another file survives, that is what content addressing
// bought. The backend deletion happens after the rows are gone, so a crash
// between the two leaves an unreferenced chunk for the garbage collection,
// never a manifest that names a missing chunk.
func (m *Manager) Delete(ctx context.Context, key string) error {
	var hashes []string
	err := m.db.WithTx(ctx, func(ctx context.Context, tx datastore.Querier) (err error) {
		if hashes, err = m.manifests.Delete(ctx, tx, key); err != nil {
			return err
		}
		// The keep-set is read inside the transaction, after the rows are
		// gone: a hash that survives here is referenced by another file
		// and stays.
		keep, err := m.manifests.ReferencedHashes(ctx, tx)
		if err != nil {
			return err
		}
		hashes = slices.DeleteFunc(hashes, func(hash string) bool {
			_, ok := keep[hash]
			return ok
		})
		return nil
	})
	if err != nil {
		return err
	}

	for _, hash := range hashes {
		if err := m.store.DeleteChunk(ctx, hash); err != nil {
			return fmt.Errorf("storage: delete chunk %s: %w", hash, err)
		}
	}
	return nil
}

// CollectGarbage removes every chunk the backend holds that no manifest
// references. It is the drain of the paths a crash can leave: an upload that
// died after PutChunk but before its manifest commit, and the chunks a
// Delete released. Returns the number of chunks removed.
func (m *Manager) CollectGarbage(ctx context.Context) (int, error) {
	keep, err := m.manifests.ReferencedHashes(ctx, m.db)
	if err != nil {
		return 0, err
	}

	var removed int
	err = m.store.ListChunks(ctx, func(hash string) error {
		if _, ok := keep[hash]; ok {
			return nil
		}
		if err := m.store.DeleteChunk(ctx, hash); err != nil {
			return err
		}
		removed++
		return nil
	})
	if err != nil {
		return removed, fmt.Errorf("storage: garbage collection: %w", err)
	}
	return removed, nil
}

// stagingPath is where a key's staging file waits.
func (m *Manager) stagingPath(key string) string {
	return filepath.Join(m.staging, key)
}

// chunkReader assembles a stored file from its chunks, fetching the next one
// only when the reader reaches it. A read is a sequence of backend reads at
// manifest order; no whole-file buffer ever exists.
type chunkReader struct {
	ctx      context.Context
	manager  *Manager
	manifest Manifest
	index    int // chunk the read is inside
	offset   int // bytes of the current chunk already consumed
	current  []byte
	err      error
}

// Read fills p from the current chunk, fetching the next when it ends, until
// p is full or the file ends.
func (r *chunkReader) Read(p []byte) (int, error) {
	if r.err != nil {
		return 0, r.err
	}
	total := 0
	for len(p) > 0 {
		if r.index >= len(r.manifest.Chunks) {
			r.err = io.EOF
			if total == 0 {
				return 0, io.EOF
			}
			return total, nil
		}
		if r.current == nil {
			chunk := r.manifest.Chunks[r.index]
			data, err := r.manager.store.GetChunk(r.ctx, nil, chunk.Hash)
			if err != nil {
				r.err = fmt.Errorf("storage: read chunk %d: %w", chunk.Index, err)
				return total, r.err
			}
			r.current = data
			r.offset = 0
		}
		n := copy(p, r.current[r.offset:])
		r.offset += n
		p = p[n:]
		total += n
		if r.offset >= len(r.current) {
			r.current = nil
			r.index++
		}
	}
	return total, nil
}

// Close satisfies io.ReadCloser. The chunks were read on demand; nothing
// holds a backend resource between reads.
func (r *chunkReader) Close() error {
	r.current = nil
	r.err = io.EOF
	return nil
}
