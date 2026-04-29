package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtractUpMigrationSQLReturnsUpSectionOnly(t *testing.T) {
	t.Parallel()

	sql, err := extractUpMigrationSQL([]byte(`-- +migrate Up
CREATE TABLE users (id INT);
CREATE TABLE peers (id INT);

-- +migrate Down
DROP TABLE peers;
DROP TABLE users;
`))
	if err != nil {
		t.Fatalf("extractUpMigrationSQL returned error: %v", err)
	}

	want := "CREATE TABLE users (id INT);\nCREATE TABLE peers (id INT);"
	if sql != want {
		t.Fatalf("expected %q, got %q", want, sql)
	}
}

func TestResolveMigrationsDirFindsExplicitDirectory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	migrationsDir := filepath.Join(dir, "migrations")
	if err := os.Mkdir(migrationsDir, 0o755); err != nil {
		t.Fatalf("mkdir migrations dir: %v", err)
	}

	got, err := ResolveMigrationsDir(migrationsDir)
	if err != nil {
		t.Fatalf("ResolveMigrationsDir returned error: %v", err)
	}
	if got != migrationsDir {
		t.Fatalf("expected %q, got %q", migrationsDir, got)
	}
}
