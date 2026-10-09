// Package storage is the local SQLite cache and settings store for Pelton.
//
// It persists everything the imap layer fetches (folders, message metadata,
// bodies and attachments) so the app can render without hitting the server on
// every read, plus a key value store for ui preferences. It never holds
// credentials: those live in the os keyring, referenced only by account id.
//
// The driver is the pure go modernc.org/sqlite so cross compiling needs no cgo.
package storage

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const (
	driverName = "sqlite"

	appDirName         = "Pelton"
	dbFileName         = "pelton.db"
	attachmentsDirName = "attachments"

	// dirPerm is used for every directory we create under the config dir.
	dirPerm = 0o755

	migrationsDir   = "migrations"
	migrationSuffix = ".sql"
)

// timestampLayout is the single format used for every text timestamp column.
const timestampLayout = time.RFC3339

//go:embed migrations/*.sql
var migrationFiles embed.FS

// execer is satisfied by both *sql.DB and *sql.Tx so query helpers can run
// either standalone or inside a transaction.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// DB is a handle to the Pelton store. It owns the sql connection pool and the
// on disk attachments root.
type DB struct {
	sql            *sql.DB
	path           string
	attachmentsDir string
	// scope is the profile the store reads and writes profile-owned rows for.
	// See profilescope.go.
	scope scope
	// deleteTxHook runs after a staged cache mutation (delete or stub body
	// fill) has executed its statements, before commit. Tests set it to
	// inject a failure; production leaves it nil.
	deleteTxHook func(ctx context.Context) error
}

// ChannelNightly is the build channel of the automated dev-branch builds. It
// gets its own data directory so a nightly can never damage a real install's
// accounts, cache or settings.
const ChannelNightly = "nightly"

// DefaultPath returns the database path for a normal build, inside the user
// config directory: os.UserConfigDir()/Pelton/pelton.db. When the PELTON_DEV
// environment variable is set (the `make run`/`wails dev` loop sets it), it
// uses Pelton-dev instead, so a local dev/test run never touches a real
// install's accounts, cache or settings.
func DefaultPath() (string, error) {
	return DefaultPathForChannel("")
}

// DefaultPathForChannel is DefaultPath for a specific build channel. Any
// channel other than "" (stable) gets its own directory, e.g. Pelton-nightly.
// PELTON_DEV still wins, so a dev run of a nightly build stays on throwaway
// data rather than the nightly's own.
func DefaultPathForChannel(channel string) (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("storage: locate user config dir: %w", err)
	}
	return filepath.Join(dir, DataDirName(channel), dbFileName), nil
}

// DataDirName names the data directory of a build channel: Pelton, Pelton-dev
// under PELTON_DEV, or Pelton-<channel>. The keyring service is named the same,
// so each data directory has its own secrets as well as its own database.
func DataDirName(channel string) string {
	switch {
	case os.Getenv("PELTON_DEV") != "":
		return appDirName + "-dev"
	case channel != "":
		return appDirName + "-" + channel
	default:
		return appDirName
	}
}

// Open opens (creating it and its parent directory if needed) the database at
// path and configures the connection pool. Attachments are stored alongside it
// under an "attachments" directory in the same folder. Call RunMigrations next.
func Open(path string) (*DB, error) {
	baseDir := filepath.Dir(path)
	if err := os.MkdirAll(baseDir, dirPerm); err != nil {
		return nil, fmt.Errorf("storage: create db dir %q: %w", baseDir, err)
	}

	sqlDB, err := sql.Open(driverName, dataSourceName(path))
	if err != nil {
		return nil, fmt.Errorf("storage: open db %q: %w", path, err)
	}
	// the pool was never actually bounded, so every concurrent caller opened a
	// connection of its own and they all queued for sqlite's single write lock.
	// A sync holds that lock in bursts, and the app has a handful of readers, so
	// a small ceiling is enough to keep a sync, the ui and a background job off
	// each other without the pool growing to whatever the moment demanded.
	sqlDB.SetMaxOpenConns(maxOpenConns)
	sqlDB.SetMaxIdleConns(maxOpenConns)
	// a connection is cheap here, and an idle one costs a file handle rather
	// than a server session, so they are kept rather than recycled on a timer.
	sqlDB.SetConnMaxLifetime(0)
	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("storage: ping db %q: %w", path, err)
	}

	return &DB{
		sql:            sqlDB,
		path:           path,
		attachmentsDir: filepath.Join(baseDir, attachmentsDirName),
	}, nil
}

// Path returns the database file this DB was opened on.
func (d *DB) Path() string {
	return d.path
}

// maxOpenConns caps the connection pool. One writer at a time is all sqlite
// allows whatever this is set to; the rest of the ceiling is for readers, and
// keeping it small keeps the queue for the write lock short and predictable.
const maxOpenConns = 8

// dataSourceName builds the dsn. the pragmas are set as query params so they
// apply to every connection the pool opens, not just the first one. wal
// improves read and write concurrency, foreign_keys enforces the cascades.
//
// _txlock=immediate is what keeps a busy database from turning into an error.
// Without it a transaction begins deferred and only asks for the write lock at
// its first write, and by then another connection may have written since the
// transaction's snapshot: sqlite fails that outright rather than waiting, and
// busy_timeout does not cover it. Taking the lock at BEGIN instead turns the
// same contention into a wait that busy_timeout governs, which is the
// difference between a sidebar drag during a sync waiting its turn and the
// same drag failing with "database is locked".
//
// busy_timeout has to cover the longest a writer can hold the lock, not the
// average. Caching a message with attachments writes their bytes to disk
// inside its transaction, so on a slow disk with a large attachment that is
// well past the 5s this used to allow.
func dataSourceName(path string) string {
	const params = "_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(15000)&_txlock=immediate"
	return fmt.Sprintf("file:%s?%s", path, params)
}

// Close closes the underlying connection pool.
func (d *DB) Close() error {
	if err := d.sql.Close(); err != nil {
		return fmt.Errorf("storage: close db: %w", err)
	}
	return nil
}

// AttachmentsDir returns the on disk attachments root, for callers (like
// config sync) that mirror it alongside the database file.
func (d *DB) AttachmentsDir() string {
	return d.attachmentsDir
}

// Snapshot writes a consistent, point-in-time copy of the database to destPath
// using SQLite's VACUUM INTO, which is safe to run against a live database
// (unlike a raw file copy, which can catch a writer mid transaction). destPath
// must not already exist.
func (d *DB) Snapshot(ctx context.Context, destPath string) error {
	if _, err := d.sql.ExecContext(ctx, `VACUUM INTO ?`, destPath); err != nil {
		return fmt.Errorf("storage: snapshot db to %q: %w", destPath, err)
	}
	return nil
}

// migration is one embedded sql file paired with its numeric version.
type migration struct {
	version int
	name    string
	sql     string
}

// RunMigrations applies every embedded migration that has not run yet, in
// version order, each in its own transaction. It is idempotent and safe to call
// on every startup.
func (d *DB) RunMigrations(ctx context.Context) error {
	if err := d.ensureMigrationsTable(ctx); err != nil {
		return err
	}

	applied, err := d.appliedMigrations(ctx)
	if err != nil {
		return err
	}

	migrations, err := loadMigrations()
	if err != nil {
		return err
	}

	for _, m := range migrations {
		if applied[m.version] {
			continue
		}
		if err := d.applyMigration(ctx, m); err != nil {
			return err
		}
	}
	// settings, signatures and saved views belong to a profile, so the store has
	// to know which one before anything reads them. Doing it here means every
	// path that opens a database gets a usable scope, tests included.
	return d.UseActiveProfile(ctx)
}

const createMigrationsTable = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    id         INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL
)`

func (d *DB) ensureMigrationsTable(ctx context.Context) error {
	if _, err := d.sql.ExecContext(ctx, createMigrationsTable); err != nil {
		return fmt.Errorf("storage: create schema_migrations: %w", err)
	}
	return nil
}

func (d *DB) appliedMigrations(ctx context.Context) (map[int]bool, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT id FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("storage: read schema_migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[int]bool)
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("storage: scan migration id: %w", err)
		}
		applied[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate migrations: %w", err)
	}
	return applied, nil
}

func (d *DB) applyMigration(ctx context.Context, m migration) error {
	if strings.Contains(m.sql, "-- pelton:foreign-keys-off") {
		return d.applyMigrationForeignKeysOff(ctx, m)
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("storage: begin migration %d: %w", m.version, err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, m.sql); err != nil {
		return fmt.Errorf("storage: run migration %d (%s): %w", m.version, m.name, err)
	}
	const insert = `INSERT INTO schema_migrations (id, applied_at) VALUES (?, ?)`
	if _, err := tx.ExecContext(ctx, insert, m.version, nowText()); err != nil {
		return fmt.Errorf("storage: record migration %d: %w", m.version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("storage: commit migration %d: %w", m.version, err)
	}
	return nil
}

// applyMigrationForeignKeysOff runs a migration that rebuilds a table with
// foreign-key dependents. PRAGMA foreign_keys cannot change inside a
// transaction, and the pool must not switch connections mid-migration, so the
// work is pinned to one Conn with foreign keys off for the duration.
func (d *DB) applyMigrationForeignKeysOff(ctx context.Context, m migration) (err error) {
	conn, err := d.sql.Conn(ctx)
	if err != nil {
		return fmt.Errorf("storage: conn for migration %d: %w", m.version, err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return fmt.Errorf("storage: disable foreign keys for migration %d: %w", m.version, err)
	}
	defer func() {
		if _, onErr := conn.ExecContext(context.Background(), `PRAGMA foreign_keys = ON`); onErr != nil && err == nil {
			err = fmt.Errorf("storage: re-enable foreign keys after migration %d: %w", m.version, onErr)
			return
		}
		var on int
		if qerr := conn.QueryRowContext(context.Background(), `PRAGMA foreign_keys`).Scan(&on); qerr != nil {
			if err == nil {
				err = fmt.Errorf("storage: verify foreign_keys after migration %d: %w", m.version, qerr)
			}
			return
		}
		if on != 1 && err == nil {
			err = fmt.Errorf("storage: foreign keys not enabled after migration %d", m.version)
		}
	}()

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("storage: begin migration %d: %w", m.version, err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, m.sql); err != nil {
		return fmt.Errorf("storage: run migration %d (%s): %w", m.version, m.name, err)
	}

	rows, err := tx.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return fmt.Errorf("storage: foreign_key_check after migration %d: %w", m.version, err)
	}
	defer rows.Close()
	if rows.Next() {
		var table string
		var rowid int64
		var parent string
		var fkid int
		_ = rows.Scan(&table, &rowid, &parent, &fkid)
		return fmt.Errorf("storage: foreign key violation after migration %d: table=%s rowid=%d parent=%s",
			m.version, table, rowid, parent)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("storage: iterate foreign_key_check after migration %d: %w", m.version, err)
	}

	const insert = `INSERT INTO schema_migrations (id, applied_at) VALUES (?, ?)`
	if _, err := tx.ExecContext(ctx, insert, m.version, nowText()); err != nil {
		return fmt.Errorf("storage: record migration %d: %w", m.version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("storage: commit migration %d: %w", m.version, err)
	}
	return nil
}

// loadMigrations reads the embedded sql files and sorts them by the numeric
// prefix in their filename, for example 0001_init.sql.
func loadMigrations() ([]migration, error) {
	entries, err := migrationFiles.ReadDir(migrationsDir)
	if err != nil {
		return nil, fmt.Errorf("storage: read migrations dir: %w", err)
	}

	migrations := make([]migration, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), migrationSuffix) {
			continue
		}
		version, err := versionFromName(e.Name())
		if err != nil {
			return nil, err
		}
		content, err := migrationFiles.ReadFile(migrationsDir + "/" + e.Name())
		if err != nil {
			return nil, fmt.Errorf("storage: read migration %q: %w", e.Name(), err)
		}
		migrations = append(migrations, migration{version: version, name: e.Name(), sql: string(content)})
	}

	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].version < migrations[j].version
	})
	if err := checkUniqueVersions(migrations); err != nil {
		return nil, err
	}
	return migrations, nil
}

// checkUniqueVersions fails when two migrations share a version number. The
// applied set is keyed by version, so a duplicate would otherwise be skipped
// silently on any database that already ran its twin. migrations must be
// sorted by version.
func checkUniqueVersions(migrations []migration) error {
	for i := 1; i < len(migrations); i++ {
		if migrations[i].version == migrations[i-1].version {
			return fmt.Errorf("storage: duplicate migration version %d: %q and %q",
				migrations[i].version, migrations[i-1].name, migrations[i].name)
		}
	}
	return nil
}

// versionFromName parses the leading digits of a migration filename.
func versionFromName(name string) (int, error) {
	prefix := name
	if before, _, ok := strings.Cut(name, "_"); ok {
		prefix = before
	}
	version, err := strconv.Atoi(prefix)
	if err != nil {
		return 0, fmt.Errorf("storage: bad migration name %q: %w", name, err)
	}
	return version, nil
}

// nowText returns the current utc time in the shared timestamp layout.
func nowText() string {
	return time.Now().UTC().Format(timestampLayout)
}

// parseTime parses a stored timestamp, tolerating an empty string as the zero
// time so optional date columns do not error.
func parseTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(timestampLayout, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("storage: parse time %q: %w", s, err)
	}
	return t, nil
}

// formatTime renders a time for storage, leaving the zero time as an empty
// string rather than a misleading year zero timestamp.
func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(timestampLayout)
}
