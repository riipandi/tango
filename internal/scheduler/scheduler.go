// Package scheduler runs periodic housekeeping jobs (token
// cleanup, pruning). Modules register named jobs; no own
// goroutines. Integrates with kernel.Startable.
package scheduler
