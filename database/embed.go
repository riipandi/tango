package database

import "embed"

//go:embed migrations/*.sql
var DatabaseMigrations embed.FS

//go:embed seeders/*.sql
var DatabaseSeeders embed.FS
