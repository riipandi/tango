package storage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChunkerSplitsIntoFixedChunksWithAShortLastOne(t *testing.T) {
	const chunkSize = 64
	chunker, err := NewChunker(chunkSize)
	require.NoError(t, err)

	// 64 + 64 + 10 bytes: two full chunks and one short tail.
	data := bytes.Repeat([]byte{0xA5}, 2*chunkSize+10)
	chunks, err := chunker.Split(bytes.NewReader(data))
	require.NoError(t, err)
	require.Len(t, chunks, 3)

	assert.Equal(t, chunkSize, chunks[0].Size)
	assert.Equal(t, chunkSize, chunks[1].Size)
	assert.Equal(t, 10, chunks[2].Size, "the last chunk carries only what is left")
	for i, chunk := range chunks {
		assert.Equal(t, i, chunk.Index)
	}

	// The hash is the SHA-256 of the chunk's bytes, the value the backends
	// address the chunk by.
	hash := sha256.Sum256(data[:chunkSize])
	assert.Equal(t, hex.EncodeToString(hash[:]), chunks[0].Hash)
}

func TestChunkerEmptyFileIsNoChunks(t *testing.T) {
	chunker, err := NewChunker(64)
	require.NoError(t, err)

	chunks, err := chunker.Split(strings.NewReader(""))
	require.NoError(t, err)
	assert.Empty(t, chunks)
}

func TestChunkerRefusesANonPositiveChunkSize(t *testing.T) {
	_, err := NewChunker(0)
	assert.Error(t, err)
}

func TestRootHashChangesWhenOneChunkChanges(t *testing.T) {
	const chunkSize = 32
	chunker, err := NewChunker(chunkSize)
	require.NoError(t, err)

	data := bytes.Repeat([]byte("x"), 3*chunkSize)
	chunks, err := chunker.Split(bytes.NewReader(data))
	require.NoError(t, err)
	before := RootHash(chunks)

	// One byte changed deep in the file: one chunk hash moves, the root
	// hash moves with it, and the diff knows exactly which chunk changed.
	data[2*chunkSize] ^= 0xFF
	chunks, err = chunker.Split(bytes.NewReader(data))
	require.NoError(t, err)
	after := RootHash(chunks)

	assert.NotEqual(t, before, after)
}

func TestChunkerReadAtReadsOnlyTheNamedChunk(t *testing.T) {
	const chunkSize = 16
	chunker, err := NewChunker(chunkSize)
	require.NoError(t, err)

	data := []byte("0123456789ABCDEFghijklmnop")
	chunks, err := chunker.Split(bytes.NewReader(data))
	require.NoError(t, err)
	require.Len(t, chunks, 2)

	// Reading a chunk by index returns exactly its bytes: the upload pass
	// re-reads only the chunks the diff named.
	f := bytes.NewReader(data)
	got, err := chunker.ReadAt(f, chunks[1], make([]byte, chunkSize))
	require.NoError(t, err)
	assert.Equal(t, "ghijklmnop", string(got))
}

func TestParallelSplitProducesTheManifestSplitProduces(t *testing.T) {
	// The two passes are interchangeable behind Sync: a file hashed in
	// parallel must carry the identical chunk list — same order, same
	// sizes, same hashes — as one hashed straight through, whatever the
	// worker count.
	const chunkSize = 64
	chunker, err := NewChunker(chunkSize)
	require.NoError(t, err)

	// 64 + 64 + 64 + 13: three full chunks and a short tail, more chunks
	// than any worker count here, so the slots really interleave.
	data := bytes.Repeat([]byte("parallel"), 4*chunkSize/8+1)
	data = data[:3*chunkSize+13]
	sequential, err := chunker.Split(bytes.NewReader(data))
	require.NoError(t, err)

	staging := t.TempDir()
	f, err := os.Create(filepath.Join(staging, "k"))
	require.NoError(t, err)
	_, err = f.Write(data)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	f, err = os.Open(f.Name())
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })

	for _, workers := range []int{1, 2, 4, 8} {
		parallel, err := chunker.ParallelSplit(f, int64(len(data)), workers)
		require.NoError(t, err)
		assert.Equal(t, sequential, parallel, "workers=%d", workers)
	}
}

func TestParallelSplitOfAnEmptyFileIsNoChunks(t *testing.T) {
	chunker, err := NewChunker(64)
	require.NoError(t, err)

	f := bytes.NewReader(nil)
	chunks, err := chunker.ParallelSplit(f, 0, 4)
	require.NoError(t, err)
	assert.Empty(t, chunks)
}
