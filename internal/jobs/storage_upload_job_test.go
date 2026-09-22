package jobs

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/internal/storage"
	"github.com/riipandi/tango/pkg/testutils"
)

func TestChunkUploadJobSyncsAStagedFile(t *testing.T) {
	container := testutils.StartPostgres(t.Context(), t)
	dsn := container.NewDatabase(t)

	pool, client := migratedClient(t, dsn)
	store := storage.NewFS(t.TempDir())
	manager, err := storage.NewManager(store, pool, 32, t.TempDir(), 2)
	require.NoError(t, err)
	require.NoError(t, Register(t.Context(), client, time.Hour, manager))

	data := bytes.Repeat([]byte("queued"), 40)
	require.NoError(t, manager.Stage(t.Context(), "uploads/report.bin", bytes.NewReader(data), nil))

	// The watcher's job: one task enqueued onto the durable queue, executed
	// by the dispatcher this test starts.
	if _, err := client.Add(ChunkUploadTask{Key: "uploads/report.bin"}).Save(); err != nil {
		t.Fatal(err)
	}
	client.Start(t.Context())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 5*time.Second)
		defer cancel()
		client.Stop(ctx)
	})

	// The sync's effect: the manifest is ready and the staging file is gone.
	require.Eventually(t, func() bool {
		manifest, err := manager.Manifest(t.Context(), "uploads/report.bin")
		return err == nil && manifest.File.Status == storage.StatusReady
	}, 5*time.Second, 20*time.Millisecond, "the upload job must commit the manifest")

	// The staging file has served its purpose: the bytes live in the
	// backend, chunk for chunk.
	_, statErr := os.Stat(filepath.Join(manager.Staging(), "uploads/report.bin"))
	assert.ErrorIs(t, statErr, os.ErrNotExist, "the staging file must be gone after the sync")
}
