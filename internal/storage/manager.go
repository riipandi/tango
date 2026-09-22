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
	"sync"
	"time"

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

// Progress is the upload state of one key, the data a status endpoint reads
// while the queue works. The transport that carries it to a frontend is not
// wired yet — see the TODO in llms/architecture.md, storage section.
type Progress struct {
	// Status is pending while chunks travel, ready once the manifest is
	// committed.
	Status string
	// Done and Total count the chunks of the current upload round: Done
	// grows as chunks land in the backend, Total is the file's chunk count.
	Done  int
	Total int
	// Size is the file's byte size, known from the moment it is staged.
	Size int64
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

// UpdateMetadata replaces the metadata a key carries — content type, the
// original file name, whatever the feature records — leaving the manifest
// and the chunks untouched. Works before and after the upload.
func (m *Manager) UpdateMetadata(ctx context.Context, key string, metadata map[string]any) error {
	if err := ValidateKey(key); err != nil {
		return err
	}
	return m.manifests.UpdateMetadata(ctx, m.db, key, metadata)
}

// Progress reports how far one key's upload has travelled, the shape a
// status endpoint would serve.
//
// TODO(notification): nothing serves this yet — the ConnectRPC and REST
// wiring does not exist. When it does, a handler maps this onto either a
// polling response or a server-streamed updates channel; the data is
// already current, the queue's workers bump it as chunks land.
func (m *Manager) Progress(ctx context.Context, key string) (Progress, error) {
	manifest, err := m.Manifest(ctx, key)
	if err != nil {
		return Progress{}, err
	}
	return Progress{
		Status: manifest.File.Status,
		Done:   manifest.File.ChunksDone,
		Total:  manifest.File.ChunkCount,
		Size:   manifest.File.Size,
	}, nil
}

// Staging is the directory the next file is written into. The watcher owns
// it; a caller that does not run the watcher names the same directory.
func (m *Manager) Staging() string { return m.staging }

// Stage writes a file into the staging directory and records the intent:
// the key's metadata and the staging fingerprint land in the manifest
// before the first byte travels, so a crash between the two leaves a
// pending row, never a half-stored file. The write is the one cost on the
// request path — local and buffered; the bytes move to the backend later,
// on the queue.
//
// metadata is the feature's own record (content type, original file name,
// owner); the engine carries it, never reads it. Re-staging a key replaces
// its metadata and makes the stored manifest stale, so the next sync
// uploads the new version.
func (m *Manager) Stage(ctx context.Context, key string, r io.Reader, metadata map[string]any) error {
	if err := ValidateKey(key); err != nil {
		return err
	}
	if err := os.MkdirAll(m.staging, 0o755); err != nil {
		return fmt.Errorf("storage: staging directory: %w", err)
	}
	// A key may name a subdirectory, so the target directory exists before
	// the rename. The temp file makes a half-written staging file
	// invisible: the watcher only ever sees the final name, complete or
	// absent.
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

	// The row is the durable half of the staging write: metadata and the
	// fingerprint the sync's checkpoint compares against, committed before
	// the upload is enqueued.
	info, err := os.Stat(target)
	if err != nil {
		return fmt.Errorf("storage: staging %q: %w", key, err)
	}
	return m.manifests.Stage(ctx, m.db, key, info.Size(), info.ModTime(), metadata)
}

// Sync uploads a staging file: it computes or reuses the chunk manifest,
// uploads only the chunks the backend does not hold — in parallel, bounded
// by the upload budget — and commits the ready manifest in one transaction.
// It is the body of the upload job; idempotent, so the queue's retries
// replay it.
func (m *Manager) Sync(ctx context.Context, key string) error {
	if err := ValidateKey(key); err != nil {
		return err
	}
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

	// The fingerprint is read once and trusted for the whole sync: the
	// file the chunks were computed from is the file the TOCTOU guard
	// compares against before the staging copy is removed.
	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("storage: stat staging %q: %w", key, err)
	}
	fingerprint := stagingFingerprint{size: info.Size(), mtime: info.ModTime()}

	prior, err := m.manifests.Load(ctx, m.db, key)
	if err != nil && !errors.Is(err, ErrNoManifest) {
		return err
	}

	// The checkpoint is the retry's fast path: a pending manifest whose
	// fingerprint matches this staging file was computed from the same
	// bytes, so the chunk list is reused and the whole hashing pass —
	// the one thing a retry of a large file cannot afford — is skipped.
	reuse := len(prior.Chunks) > 0 &&
		prior.File.Status == StatusPending &&
		prior.File.StagingSize == fingerprint.size &&
		prior.File.StagingMtime.Equal(fingerprint.mtime)

	var chunks []Chunk
	contentHash := ""
	switch {
	case reuse:
		chunks, contentHash = prior.Chunks, prior.File.ContentHash
	default:
		// A large file pays for hashing in parallel — the pass reads at
		// each chunk's own offset, the same shape as the upload pass —
		// while a small one is cheaper read straight through.
		if fingerprint.size >= parallelHashThreshold && m.uploads > 1 {
			chunks, err = m.chunker.ParallelSplit(f, fingerprint.size, m.uploads)
		} else {
			if _, err := f.Seek(0, io.SeekStart); err != nil {
				return fmt.Errorf("storage: rewind staging %q: %w", key, err)
			}
			chunks, err = m.chunker.Split(f)
		}
		if err != nil {
			return err
		}
		contentHash = RootHash(chunks)
	}

	// The finished-manifest fast path: the stored manifest is ready and
	// hashes to the same root, so the backend already holds every chunk
	// this file needs. The row is refreshed and the staging copy can go.
	if prior.File.Status == StatusReady && prior.File.ContentHash == contentHash {
		prior.File.Size = fingerprint.size
		prior.File.ChunkSize = m.chunker.size
		prior.File.ChunkCount = len(chunks)
		prior.File.StagingSize = fingerprint.size
		prior.File.StagingMtime = fingerprint.mtime
		if err := m.db.WithTx(ctx, func(ctx context.Context, tx datastore.Querier) error {
			return m.manifests.Save(ctx, tx, prior)
		}); err != nil {
			return err
		}
		return m.clearStaging(ctx, path, fingerprint)
	}

	// The checkpoint: the chunk list lands before the first chunk travels,
	// so a retry resumes from the stored hashes instead of recomputing
	// them. The progress counter restarts with the round.
	checkpoint := Manifest{
		File: File{
			Key:          key,
			Size:         fingerprint.size,
			ChunkSize:    m.chunker.size,
			ChunkCount:   len(chunks),
			ContentHash:  contentHash,
			Status:       StatusPending,
			Metadata:     prior.File.Metadata,
			ChunksDone:   0,
			StagingSize:  fingerprint.size,
			StagingMtime: fingerprint.mtime,
		},
		Chunks: chunks,
	}
	if !reuse {
		if err := m.db.WithTx(ctx, func(ctx context.Context, tx datastore.Querier) error {
			return m.manifests.Save(ctx, tx, checkpoint)
		}); err != nil {
			return err
		}
	}

	done, err := m.uploadMissing(ctx, key, chunks)
	if err != nil {
		return err
	}

	// The ready commit closes the round: every chunk is in the backend,
	// the manifest says so, and the staging copy has served its purpose —
	// unless it changed underneath the sync, which the guard catches.
	checkpoint.File.Status = StatusReady
	checkpoint.File.ChunksDone = len(chunks)
	if err := m.db.WithTx(ctx, func(ctx context.Context, tx datastore.Querier) error {
		return m.manifests.Save(ctx, tx, checkpoint)
	}); err != nil {
		return err
	}
	if !reuse && done > 0 {
		slog.InfoContext(ctx, "storage: file synced",
			"key", key, "chunks", len(chunks), "uploaded", done, "size", fingerprint.size)
	}
	return m.clearStaging(ctx, path, fingerprint)
}

// uploadMissing puts every chunk the backend does not hold yet, several at
// a time. The worker count is the budget; each worker borrows one buffer
// from the pool, so a sync's memory is budget × chunk size, never the file
// size. Returns how many chunks this round actually uploaded.
func (m *Manager) uploadMissing(ctx context.Context, key string, chunks []Chunk) (int, error) {
	hashes := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		hashes = append(hashes, chunk.Hash)
	}
	present, err := m.store.HasChunks(ctx, hashes)
	if err != nil {
		return 0, fmt.Errorf("storage: probe chunks of %q: %w", key, err)
	}

	pending := make([]Chunk, 0, len(chunks))
	for _, chunk := range chunks {
		if !present[chunk.Hash] {
			pending = append(pending, chunk)
		}
	}

	// The counter starts at what the backend already holds, so a resumed
	// round reports the true distance to the total, not the distance this
	// process happens to have walked.
	done := len(chunks) - len(pending)
	if err := m.manifests.SetProgress(ctx, m.db, key, done); err != nil {
		return 0, err
	}
	if len(pending) == 0 {
		return 0, nil
	}

	f, err := os.Open(m.stagingPath(key))
	if err != nil {
		return 0, fmt.Errorf("storage: open staging %q: %w", key, err)
	}
	defer func() { _ = f.Close() }()

	// One buffer per worker slot, handed out and returned: the allocations
	// happen once, the uploads reuse them for the whole file.
	buffers := make(chan []byte, m.uploads)
	for range m.uploads {
		buffers <- make([]byte, m.chunker.size)
	}

	// The progress counter grows one UPDATE per landed chunk — the data a
	// status reader watches while the round runs. The mutex keeps the
	// counter and the row in step without a read-modify-write race.
	var mu sync.Mutex
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
			if err := m.store.PutChunk(ctx, chunk.Hash, data); err != nil {
				return err
			}
			mu.Lock()
			done++
			err = m.manifests.BumpProgress(ctx, m.db, key, 1)
			mu.Unlock()
			return err
		})
	}
	if err := group.Wait(); err != nil {
		return done, fmt.Errorf("storage: upload %q: %w", key, err)
	}
	return done, nil
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
	if err := ValidateKey(key); err != nil {
		return err
	}

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

	// A staged-but-never-synced copy would outlive its manifest; the key
	// is gone, so its staging file goes with it.
	_ = os.Remove(m.stagingPath(key))
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

// clearStaging removes the staging copy, but only the copy this sync read:
// a file re-staged underneath a running sync has a different fingerprint,
// and removing it would destroy an upload nobody has recorded. The mismatch
// is an error, so the queue retries and the newer staging file gets its own
// round.
func (m *Manager) clearStaging(ctx context.Context, path string, fingerprint stagingFingerprint) error {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("storage: clear staging: %w", err)
	}
	if info.Size() != fingerprint.size || !info.ModTime().Equal(fingerprint.mtime) {
		return fmt.Errorf("storage: staging %s changed during sync, left for a fresh round", filepath.Base(path))
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("storage: clear staging: %w", err)
	}
	return nil
}

// stagingPath is where a key's staging file waits. The key is validated by
// every entry point before it reaches here, so the join cannot walk out of
// the staging directory.
func (m *Manager) stagingPath(key string) string {
	return filepath.Join(m.staging, key)
}

// stagingFingerprint is the identity of the staging file one sync read:
// size and modification time. Equal fingerprints mean the same bytes.
type stagingFingerprint struct {
	size  int64
	mtime time.Time
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
