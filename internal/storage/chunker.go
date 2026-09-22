package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"

	"golang.org/x/sync/errgroup"
)

// Chunk is one piece of a file: its position in the file, the SHA-256 of its
// bytes — the address the backends store it under — and its length. The last
// chunk of a file may be short; every other one is Chunker.Size long.
type Chunk struct {
	Index int
	Hash  string
	Size  int
}

// Chunker cuts a file into the fixed-size chunks the manifest tracks.
type Chunker struct {
	size int
}

// NewChunker builds a chunker of the given chunk size. A size below one byte
// cannot cut anything, so it is refused here rather than at split time.
func NewChunker(size int) (*Chunker, error) {
	if size <= 0 {
		return nil, fmt.Errorf("storage: chunk size must be positive, got %d", size)
	}
	return &Chunker{size: size}, nil
}

// Split reads r once, hashing each chunk as it goes. The data is not kept:
// the pass exists to learn the manifest, and a chunk that needs uploading is
// read again from the file it came from. Memory stays at one read buffer no
// matter how large the file is.
func (c *Chunker) Split(r io.Reader) ([]Chunk, error) {
	var chunks []Chunk
	buf := make([]byte, c.size)
	hasher := sha256.New()

	for index := 0; ; index++ {
		n, err := io.ReadFull(r, buf)
		if n == 0 && err == io.EOF {
			return chunks, nil
		}
		if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
			return nil, fmt.Errorf("storage: read chunk %d: %w", index, err)
		}

		hasher.Reset()
		hasher.Write(buf[:n])
		chunks = append(chunks, Chunk{
			Index: index,
			Hash:  hex.EncodeToString(hasher.Sum(nil)),
			Size:  n,
		})

		if err != nil {
			return chunks, nil
		}
	}
}

// parallelHashThreshold is the staging size where hashing in parallel beats
// hashing one stream: below it the file fits a few reads anyway, and the
// concurrency would spend more on coordination than it wins back. A var so
// the sync test can force the parallel path without staging 16 MiB.
var parallelHashThreshold = int64(16 << 20) // 16 MiB

// ParallelSplit computes the same manifest Split produces, but hashes the
// chunks concurrently: each worker reads its chunk at its own offset, so no
// seek races exist and no lock does — every worker writes only its own
// slot. The buffers are one pool of `workers` slots handed out and
// returned, so memory stays at workers × chunk size however large the file
// is. The caller chooses this over Split when the file is big enough for
// the concurrency to pay for itself.
func (c *Chunker) ParallelSplit(f io.ReaderAt, size int64, workers int) ([]Chunk, error) {
	if workers <= 0 {
		workers = 1
	}
	count := (size + int64(c.size) - 1) / int64(c.size)
	if count <= 0 {
		return nil, nil
	}

	chunks := make([]Chunk, count)
	buffers := make(chan []byte, workers)
	for range workers {
		buffers <- make([]byte, c.size)
	}

	var group errgroup.Group
	group.SetLimit(workers)
	for index := range count {
		group.Go(func() error {
			buf := <-buffers
			defer func() { buffers <- buf }()

			n, err := f.ReadAt(buf, index*int64(c.size))
			if err != nil && err != io.EOF {
				return fmt.Errorf("storage: read chunk %d: %w", index, err)
			}
			hasher := sha256.New()
			hasher.Write(buf[:n])
			chunks[index] = Chunk{
				Index: int(index),
				Hash:  hex.EncodeToString(hasher.Sum(nil)),
				Size:  n,
			}
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}
	return chunks, nil
}

// SplitAt hashes the chunk the offset names out of an already-open file, so
// an upload pass reads only the bytes of the chunks it must upload. The read
// writes straight into the caller's buffer, so the parallel uploads cost
// workers × chunk size, never the file size.
func (c *Chunker) ReadAt(f io.ReaderAt, chunk Chunk, buf []byte) ([]byte, error) {
	if len(buf) < chunk.Size {
		return nil, fmt.Errorf("storage: chunk %d buffer shorter than chunk", chunk.Index)
	}
	n, err := f.ReadAt(buf[:chunk.Size], int64(chunk.Index)*int64(c.size))
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("storage: read chunk %d: %w", chunk.Index, err)
	}
	return buf[:n], nil
}

// RootHash hashes the chunk hashes in order. It is the one value that says
// whether a file changed: two files with the same root hash are the same
// bytes, and a manifest whose root hash matches the freshly computed one
// needs no chunk comparison at all.
func RootHash(chunks []Chunk) string {
	root := sha256.New()
	for _, chunk := range chunks {
		_, _ = root.Write([]byte(chunk.Hash))
	}
	return hex.EncodeToString(root.Sum(nil))
}
