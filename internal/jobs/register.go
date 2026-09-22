package jobs

import (
	"context"
	"log/slog"
	"time"

	"github.com/riipandi/tango/internal/queue"
	"github.com/riipandi/tango/internal/scheduler"
	"github.com/riipandi/tango/internal/storage"
)

// Register registers every job the application runs and seeds the recurring
// ones. It is the one place the job list is spelled out, and the one call the
// composition root makes.
//
// Seeding is guarded by the pending count, so every process start that finds
// no task of a recurring queue pending adds exactly one: the schedule
// survives restarts without multiplying. Two processes starting together may
// seed twice, which is harmless — the jobs are idempotent and each run
// re-enqueues one successor, so the population stays at what the race left.
//
// uploader is the storage engine the chunk upload and the garbage collection
// run through; the composition root hands it over with everything else.
func Register(ctx context.Context, client *queue.Client, cleanupInterval time.Duration, uploader *storage.Manager) error {
	client.Register(queue.NewQueue[CleanupTask](cleanupProcessor))
	// A run without the storage engine registers none of its jobs: a queue
	// that cannot answer its tasks is not a schedule, it is a failure.
	if uploader != nil {
		client.Register(queue.NewQueue[ChunkUploadTask](func(ctx context.Context, task ChunkUploadTask) error {
			return uploadProcessor(ctx, task, uploader)
		}))
		client.Register(queue.NewQueue[StorageGCTask](func(ctx context.Context, task StorageGCTask) error {
			return gcProcessor(ctx, task, uploader)
		}))
	}

	if err := seedOnce(ctx, client, CleanupName, cleanupSeed(cleanupInterval), cleanupInterval); err != nil {
		return err
	}
	if uploader == nil {
		return nil
	}
	return seedOnce(ctx, client, StorageGCName, gcSeed(DefaultStorageGCInterval), DefaultStorageGCInterval)
}

// seedOnce seeds one recurring job while none of its tasks is pending. The
// interval is both the first run's delay and the schedule the task carries
// in its payload, so the run a restart seeds keeps the schedule it was
// seeded with.
func seedOnce(ctx context.Context, client *queue.Client, name string, task queue.Task, interval time.Duration) error {
	pending, err := client.Pending(ctx, name)
	if err != nil {
		return err
	}
	if pending > 0 {
		return nil
	}
	if _, err := client.Add(task).Ctx(ctx).Wait(interval).Save(); err != nil {
		return err
	}

	slog.InfoContext(ctx, "queue: recurring job seeded",
		"queue", name, "interval", interval.String())
	return nil
}

// Scheduled lists the jobs the cron scheduler enqueues at their times. The
// list is empty until a feature asks for a cron schedule — a recurring job
// that runs on a fixed interval belongs with Register, whose self-enqueued
// successor is durable without a second mechanism.
func Scheduled() []scheduler.Job {
	return nil
}
