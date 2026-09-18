//go:build !debug

package launcher

// DBCmd exposes the release manage surface; migration maintenance commands are debug-only.
type DBCmd struct {
	MigrateUp      MigrateUpCmd      `cmd:"" name:"migrate:up" help:"Run database migrations"`
	MigrateDown    MigrateDownCmd    `cmd:"" name:"migrate:down" help:"Rollback the most recent migration"`
	MigrateStatus  MigrateStatusCmd  `cmd:"" name:"migrate:status" help:"Check database migration status"`
	MigrateVersion MigrateVersionCmd `cmd:"" name:"migrate:version" help:"Print the current migration version"`
	Export         DBExportCmd       `cmd:"" help:"Export schema & data or data only (SQL format)"`
	Import         DBImportCmd       `cmd:"" help:"Import from a SQL file"`
	Dump           DBDumpCmd         `cmd:"" help:"Dump schema & data or data only (custom format)"`
	Restore        DBRestoreCmd      `cmd:"" help:"Restore from a dump file (custom format)"`
}
