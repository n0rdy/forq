// Package testutil provides shared helpers for tests that need a real,
// migrated SQLite database. Forq's data layer is plain SQL against SQLite, so
// tests run against the real thing instead of mocks - it's fast enough and
// catches actual SQL/index/schema mistakes.
package testutil

import (
	"database/sql"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/n0rdy/forq/configs"
	"github.com/n0rdy/forq/db"

	_ "github.com/mattn/go-sqlite3"
)

// NewTestRepo creates a migrated SQLite database in a temp dir and returns the
// repo, the app configs it was built with, and a raw DB handle for tests that
// need to fiddle with rows directly (e.g. rewinding timestamps to simulate
// stale or expired messages). Everything is cleaned up with the test.
func NewTestRepo(t *testing.T) (*db.ForqRepo, *configs.AppConfigs, *sql.DB) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "forq_test.db")
	ApplyMigrations(t, dbPath)

	appConfigs := configs.NewAppConfig(false, 24, 168)
	repo, err := db.NewSQLiteRepo(dbPath, appConfigs)
	if err != nil {
		t.Fatalf("failed to create repo: %v", err)
	}
	t.Cleanup(func() { repo.Close() })

	rawDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("failed to open raw db handle: %v", err)
	}
	rawDB.SetMaxOpenConns(1)
	t.Cleanup(func() { rawDB.Close() })

	return repo, appConfigs, rawDB
}

// ApplyMigrations runs all embedded up-migrations, in order, against the
// SQLite file at dbPath.
func ApplyMigrations(t *testing.T, dbPath string) {
	t.Helper()

	sqlDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("failed to open db for migrations: %v", err)
	}
	defer sqlDB.Close()

	var upFiles []string
	err = fs.WalkDir(db.MigrationsFS, "migrations", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".up.sql") {
			upFiles = append(upFiles, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("failed to walk migrations: %v", err)
	}
	sort.Strings(upFiles)

	for _, file := range upFiles {
		content, err := fs.ReadFile(db.MigrationsFS, file)
		if err != nil {
			t.Fatalf("failed to read migration %s: %v", file, err)
		}
		if _, err := sqlDB.Exec(string(content)); err != nil {
			t.Fatalf("failed to apply migration %s: %v", file, err)
		}
	}
}
