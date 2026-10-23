// Package storagetest gives tests a store with every migration applied
// without running all of them again for each test.
//
// Running every migration on a fresh database costs a few hundred
// milliseconds, several times that under -race, and a package with hundreds
// of tests that each open a store spent most of its time there. The first
// call migrates one database for the whole test binary; every call copies it.
// RunMigrations on the copy still runs for real: it finds every migration
// applied and does the rest of its work (names, staging recovery, profile
// scope) exactly as it does for an existing install.
//
// The migrations themselves stay covered by storage's own migration and
// upgrade tests, which open plain databases.
package storagetest

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/peltonapp/Pelton/internal/storage"
)

var (
	once    sync.Once
	tplDir  string
	tplPath string
	tplErr  error
)

// Open opens the database at path like storage.Open. When there is no
// database at path yet it first copies one with every migration applied
// there, so the RunMigrations that follows has nothing left to apply.
func Open(path string) (*storage.DB, error) {
	if err := Seed(path); err != nil {
		return nil, err
	}
	return storage.Open(path)
}

// Seed copies a database with every migration applied to path, creating its
// directory. It leaves an existing database at path alone.
func Seed(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("storagetest: stat %q: %w", path, err)
	}
	src, err := template()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("storagetest: read template: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("storagetest: create %q: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("storagetest: write %q: %w", path, err)
	}
	return nil
}

// Main runs the tests and then removes the template. Call it from TestMain:
//
//	func TestMain(m *testing.M) { os.Exit(storagetest.Main(m)) }
func Main(m *testing.M) int {
	code := m.Run()
	if tplDir != "" {
		_ = os.RemoveAll(tplDir)
	}
	return code
}

// template migrates one database for the test binary and returns the path of
// a single-file copy of it.
func template() (string, error) {
	once.Do(func() {
		tplErr = build()
	})
	return tplPath, tplErr
}

func build() error {
	dir, err := os.MkdirTemp("", "pelton-storagetest-")
	if err != nil {
		return fmt.Errorf("storagetest: template dir: %w", err)
	}
	tplDir = dir
	ctx := context.Background()
	db, err := storage.Open(filepath.Join(dir, "migrate", "pelton.db"))
	if err != nil {
		return fmt.Errorf("storagetest: open template: %w", err)
	}
	defer db.Close()
	if err := db.RunMigrations(ctx); err != nil {
		return fmt.Errorf("storagetest: migrate template: %w", err)
	}
	// VACUUM INTO writes one self-contained file: no -wal or -shm to copy.
	path := filepath.Join(dir, "template.db")
	if err := db.Snapshot(ctx, path); err != nil {
		return fmt.Errorf("storagetest: snapshot template: %w", err)
	}
	tplPath = path
	return nil
}
