package jobs

import (
	"context"
	"log/slog"
	"time"

	"github.com/riipandi/tango/internal/queue"
	"github.com/riipandi/tango/internal/scheduler"
)

// Register registers every job the application runs and seeds the recurring
// ones. It is the one place the job list is spelled out, and the one call the
// composition root makes.
//
// Seeding is guarded by the pending count, so every process start that finds
// no cleanup task pending adds exactly one: the schedule survives restarts
// without multiplying. Two processes starting together may seed twice, which
// is harmless — the job is idempotent and each run re-enqueues one successor,
// so the population stays at what the race left.
func Register(ctx context.Context, client *queue.Client, cleanupInterval time.Duration) error {
	client.Register(queue.NewQueue[CleanupTask](cleanupProcessor))

	pending, err := client.Pending(ctx, CleanupName)
	if err != nil {
		return err
	}
	if pending > 0 {
		return nil
	}

	_, err = client.Add(cleanupSeed(cleanupInterval)).Ctx(ctx).Wait(cleanupInterval).Save()
	if err != nil {
		return err
	}

	slog.InfoContext(ctx, "queue: maintenance job seeded",
		"queue", CleanupName, "interval", cleanupInterval.String())
	return nil
}

// Scheduled lists the jobs the cron scheduler enqueues at their times. The
// list is empty until a feature asks for a cron schedule — a recurring job
// that runs on a fixed interval belongs with Register, whose self-enqueued
// successor is durable without a second mechanism.
func Scheduled() []scheduler.Job {
	return nil
}
