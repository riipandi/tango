//go:build debug

package launcher

// DBCmd is the debug command surface: backup operations join the
// migration commands, including the development-only ones
// (scaffolding new migrations, reordering, validating, rolling the
// schema back to the initial state).
type DBCmd struct {
	Dump           DBDumpCmd          `cmd:"" help:"Dump schema & data or data only (custom format)"`
	Export         DBExportCmd        `cmd:"" help:"Export schema & data or data only (SQL format)"`
	Import         DBImportCmd        `cmd:"" help:"Import from a SQL file"`
	MigrateUp      MigrateUpCmd       `cmd:"" name:"migrate:up" help:"Run database migrations"`
	MigrateDown    MigrateDownCmd     `cmd:"" name:"migrate:down" help:"Rollback the most recent migration"`
	MigrateStatus  MigrateStatusCmd   `cmd:"" name:"migrate:status" help:"Check database migration status"`
	MigrateVersion MigrateVersionCmd  `cmd:"" name:"migrate:version" help:"Print the current migration version"`
	MigrateCreate  MigrateCreateCmd   `cmd:"" name:"migrate:create" help:"Create a new sequential migration file"`
	MigrateFix     MigrateFixCmd      `cmd:"" name:"migrate:fix" help:"Reorder migration files"`
	MigrateVal     MigrateValidateCmd `cmd:"" name:"migrate:validate" help:"Check the migration files"`
	MigrateReset   MigrateResetCmd    `cmd:"" name:"migrate:reset" help:"Rollback all migrations"`
	Restore        DBRestoreCmd       `cmd:"" help:"Restore from a dump file (custom format)"`
}
