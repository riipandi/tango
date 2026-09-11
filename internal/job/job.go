// Package job is a periodic background job scheduler for housekeeping
// (expired token cleanup, one-time access pruning, sync schedules)
// integrated with kernel.Startable.
//
// Planned files: job.go (Scheduler contract + registration),
// scheduler.go (ticker-based runner). Modules register named jobs
// instead of owning goroutines.
package job
