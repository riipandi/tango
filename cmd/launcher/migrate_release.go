//go:build !debug

package launcher

// MigrateCmd is the release command surface: apply, roll back, and
// inspect the compiled-in migrations. Creating migration files and
// resetting the schema are development operations.
type MigrateCmd struct {
	Up      MigrateUpCmd      `cmd:"" help:"Run database migrations"`
	Down    MigrateDownCmd    `cmd:"" help:"Rollback the most recent migration"`
	Status  MigrateStatusCmd  `cmd:"" help:"Check database migration status"`
	Version MigrateVersionCmd `cmd:"" help:"Print the current migration version"`
	DB      DBCmd             `cmd:"" name:"db" help:"Database backup and restore"`
}
