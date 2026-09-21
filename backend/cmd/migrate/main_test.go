package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverMigrationsOrdersUpFilesAndCalculatesChecksums(t *testing.T) {
	directory := t.TempDir()
	for name, contents := range map[string]string{
		"000002_second.up.sql":  "CREATE TABLE second (id INT);",
		"000001_first.up.sql":   "CREATE TABLE first (id INT);",
		"000001_first.down.sql": "DROP TABLE first;",
		"README.md":             "ignored",
	} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(contents), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	items, err := discoverMigrations(directory)
	if err != nil {
		t.Fatalf("discoverMigrations() error = %v", err)
	}
	if len(items) != 2 || items[0].version != 1 || items[1].version != 2 ||
		len(items[0].checksum) != 64 {
		t.Fatalf("discoverMigrations() = %+v, want ordered up migrations with SHA-256", items)
	}
}

func TestDiscoverMigrationsRejectsDuplicateVersions(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{"000001_first.up.sql", "000001_again.up.sql"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("SELECT 1;"), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if _, err := discoverMigrations(directory); err == nil {
		t.Fatal("discoverMigrations() error = nil, want duplicate version error")
	}
}
