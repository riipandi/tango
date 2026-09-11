// Package scheduler is a periodic background job scheduler for
// housekeeping (expired token cleanup, one-time access pruning, sync
// schedules) integrated with kernel.Startable.
//
// Planned files: scheduler.go (Scheduler contract + registration),
// runner.go (ticker-based runner). Modules register named jobs instead
// of owning goroutines.
package scheduler
