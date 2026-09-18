package database

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// chdirRepoRoot moves the test working directory to the repository
// root — debug builds resolve the disk migration source relative
// to it, and the backup directory lands there — and restores it
// when the test ends.
func chdirRepoRoot(t *testing.T) {
	t.Helper()

	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}

	dir := orig
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "Taskfile.yml")); statErr == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repository root not found")
		}
		dir = parent
	}

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir to repository root: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

// freshMigratedDB brings the shared database to a deterministic
// fully-migrated state: roll everything back via MigrateDownTo (it
// exists in both build variants, unlike the debug-only reset),
// then apply everything.
func freshMigratedDB(ctx context.Context, t *testing.T, dsn string) []MigrationOutcome {
	t.Helper()

	if _, err := MigrateDownTo(ctx, dsn, 0); err != nil {
		t.Fatalf("reset before test: %v", err)
	}
	applied, err := MigrateUp(ctx, dsn)
	if err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	if len(applied) == 0 {
		t.Fatal("expected at least one migration to apply")
	}
	return applied
}
