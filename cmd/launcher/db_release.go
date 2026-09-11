//go:build !debug

package launcher

// DBCmd is the release command surface: backup operations join the
// migration commands that apply, roll back, and inspect the
// compiled-in migrations. Creating migration files and resetting
// the schema are development operations.
type DBCmd struct {
	Dump           DBDumpCmd         `cmd:"" help:"Dump schema & data or data only (custom format)"`
	Export         DBExportCmd       `cmd:"" help:"Export schema & data or data only (SQL format)"`
	Import         DBImportCmd       `cmd:"" help:"Import from a SQL file"`
	MigrateUp      MigrateUpCmd      `cmd:"" name:"migrate:up" help:"Run database migrations"`
	MigrateDown    MigrateDownCmd    `cmd:"" name:"migrate:down" help:"Rollback the most recent migration"`
	MigrateStatus  MigrateStatusCmd  `cmd:"" name:"migrate:status" help:"Check database migration status"`
	MigrateVersion MigrateVersionCmd `cmd:"" name:"migrate:version" help:"Print the current migration version"`
	Restore        DBRestoreCmd      `cmd:"" help:"Restore from a dump file (custom format)"`
}
