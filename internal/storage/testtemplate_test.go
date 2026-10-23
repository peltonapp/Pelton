package storage

// One migrated database per test binary, copied for each test that needs a
// migrated store. Running every migration costs tens of milliseconds, and
// over a second under -race, for each test that opens a store.
// storagetest does the same for other packages; it cannot be imported here
// (it imports storage).
//
// The migration and upgrade tests open plain databases and keep running the
// migrations for real.

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestMain(m *testing.M) {
	code := m.Run()
	if migratedTemplateDir != "" {
		_ = os.RemoveAll(migratedTemplateDir)
	}
	os.Exit(code)
}

var (
	migratedTemplateOnce sync.Once
	migratedTemplateDir  string
	migratedTemplate     string
	migratedTemplateErr  error
)

// openMigrated opens a new database at path that already has every migration
// applied, copied from the template. The caller still runs RunMigrations,
// which finds nothing left to apply and does the rest of its work.
func openMigrated(path string) (*DB, error) {
	migratedTemplateOnce.Do(func() {
		migratedTemplateErr = buildMigratedTemplate()
	})
	if migratedTemplateErr != nil {
		return nil, migratedTemplateErr
	}
	data, err := os.ReadFile(migratedTemplate)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return nil, err
	}
	return Open(path)
}

func buildMigratedTemplate() error {
	dir, err := os.MkdirTemp("", "pelton-storage-test-")
	if err != nil {
		return err
	}
	migratedTemplateDir = dir
	ctx := context.Background()
	db, err := Open(filepath.Join(dir, "migrate", "pelton.db"))
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.RunMigrations(ctx); err != nil {
		return err
	}
	path := filepath.Join(dir, "template.db")
	if err := db.Snapshot(ctx, path); err != nil {
		return err
	}
	migratedTemplate = path
	return nil
}
