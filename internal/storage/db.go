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
	"database/sql/driver"
	"embed"
	"errors"
	"fmt"
	"log/slog"
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
	// bodyFillLookupHook runs after a body fill looked up its row, before
	// the write transaction. Tests set it to change the target in between;
	// production leaves it nil.
	bodyFillLookupHook func()
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

// OthersActive reports whether any connection is in use: a query running,
// a transaction open, or a writer waiting for the lock. A bulk writer that
// calls it between its own transactions, holding none, learns whether
// anyone could be queued behind it.
func (d *DB) OthersActive() bool {
	return d.sql.Stats().InUse > 0
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
// on every startup. It also recovers attachment directories left staged by a run
// that died mid-change, so call it before any other use.
func (d *DB) RunMigrations(ctx context.Context) error {
	if err := d.ensureMigrationsTable(ctx); err != nil {
		return err
	}

	migrations, err := loadMigrations()
	if err != nil {
		return err
	}
	applied, err := d.appliedMigrations(ctx)
	if err != nil {
		return err
	}
	if err := d.checkMigrationNames(ctx, migrations, applied); err != nil {
		return err
	}

	rebuilt := false
	for _, m := range migrations {
		if _, ok := applied[m.version]; ok {
			continue
		}
		if isRebuild(m) {
			if err := d.checkRebuildSpace(ctx, m); err != nil {
				return err
			}
		}
		if err := d.applyMigration(ctx, m); err != nil {
			return err
		}
		rebuilt = rebuilt || isRebuild(m)
	}
	if rebuilt {
		d.reclaimSpace(ctx)
	}
	d.recoverCacheStaging(ctx)
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

// ensureMigrationsTable creates schema_migrations and gives it the name
// column. The column is added here rather than by a migration because the
// names are checked before any migration runs. NULL is a row recorded before
// names were.
//
// This runs before checkMigrationNames can refuse the database, and that is
// harmless: the nullable column changes nothing for a build that does not
// know it, which still records its migrations by id and applied_at, so a
// refused database stays usable by the build it belongs to.
func (d *DB) ensureMigrationsTable(ctx context.Context) error {
	if _, err := d.sql.ExecContext(ctx, createMigrationsTable); err != nil {
		return fmt.Errorf("storage: create schema_migrations: %w", err)
	}
	var n int
	if err := d.sql.QueryRowContext(ctx,
		`SELECT count(*) FROM pragma_table_info('schema_migrations') WHERE name = 'name'`).Scan(&n); err != nil {
		return fmt.Errorf("storage: inspect schema_migrations: %w", err)
	}
	if n == 0 {
		if _, err := d.sql.ExecContext(ctx, `ALTER TABLE schema_migrations ADD COLUMN name TEXT`); err != nil {
			return fmt.Errorf("storage: add schema_migrations.name: %w", err)
		}
	}
	return nil
}

// appliedMigrations maps each applied version to its recorded file name, ""
// when none was recorded.
func (d *DB) appliedMigrations(ctx context.Context) (map[int]string, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT id, coalesce(name, '') FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("storage: read schema_migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[int]string)
	for rows.Next() {
		var id int
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, fmt.Errorf("storage: scan migration id: %w", err)
		}
		applied[id] = name
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate migrations: %w", err)
	}
	return applied, nil
}

// rebuildMarker heads a migration that rebuilds tables with foreign-key
// dependents.
const rebuildMarker = "-- pelton:foreign-keys-off"

// isRebuild reports a migration that rebuilds tables with foreign-key
// dependents: its first line is rebuildMarker. Such a rebuild leaves the old
// copy behind as free pages.
func isRebuild(m migration) bool {
	first, _, _ := strings.Cut(m.sql, "\n")
	return strings.TrimSpace(first) == rebuildMarker
}

// reclaimMinFree is the free-page total below which reclaimSpace does not
// VACUUM: rewriting the whole file to give back a few pages is not worth it.
// A var for tests.
var reclaimMinFree int64 = 64 << 20

// diskFree reads free space for the directory holding the database. A var
// for tests.
var diskFree = freeDiskBytes

// usedBytes is the database's size without its free pages.
func (d *DB) usedBytes(ctx context.Context) (used, free int64, err error) {
	var pages, freePages, pageSize int64
	if err := d.sql.QueryRowContext(ctx,
		`SELECT c.page_count, f.freelist_count, p.page_size FROM pragma_page_count c, pragma_freelist_count f, pragma_page_size p`,
	).Scan(&pages, &freePages, &pageSize); err != nil {
		return 0, 0, err
	}
	return (pages - freePages) * pageSize, freePages * pageSize, nil
}

// checkRebuildSpace refuses to start a table rebuild the disk cannot hold.
// The rebuild writes a second copy of the table into the file and a third
// into the WAL before the checkpoint, so it needs about twice the database's
// used size free. Failing midway would roll back after minutes of work and
// fail the same way at every start with a bare "disk full"; this says why.
// Where free space cannot be read the rebuild just runs.
func (d *DB) checkRebuildSpace(ctx context.Context, m migration) error {
	used, _, err := d.usedBytes(ctx)
	if err != nil {
		return fmt.Errorf("storage: read database size before migration %d: %w", m.version, err)
	}
	dir := filepath.Dir(d.path)
	free, ok := diskFree(dir)
	if !ok {
		return nil
	}
	if need := uint64(2 * used); free < need {
		return fmt.Errorf("storage: not enough free disk space to upgrade the database: "+
			"migration %d (%s) needs about %d MB free in %s, but only %d MB is; "+
			"free up disk space and start Pelton again",
			m.version, m.name, need>>20, dir, free>>20)
	}
	return nil
}

// reclaimSpace runs after a table rebuild. The old copy of the table is left
// as free pages (rebuilding messages doubles the file) and the WAL holds a
// copy of the new one. VACUUM gives the pages back (about 6 s for 150k
// messages). In WAL mode it builds a fresh copy of the database in a temp
// file and then writes all of it through the WAL, so it needs about twice
// the used size free; it runs only when the disk has that and there are at
// least reclaimMinFree bytes of free pages. The temp copy goes to SQLite's
// temp directory (TMPDIR), which may be another filesystem: only the
// database's own is checked, and a full TMPDIR just fails the VACUUM. Failing is only logged: the
// upgrade is committed and SQLite reuses free pages anyway. The checkpoint
// then truncates the WAL. Peak disk use of the rebuild itself is about the
// messages table three times over (old, new and WAL).
func (d *DB) reclaimSpace(ctx context.Context) {
	used, free, err := d.usedBytes(ctx)
	switch {
	case err != nil:
		slog.Warn("storage: read free pages", "err", err)
	case free < reclaimMinFree:
	default:
		if disk, ok := diskFree(filepath.Dir(d.path)); ok && disk < uint64(2*used) {
			slog.Warn("storage: skip vacuum after migration, not enough free disk",
				"free_disk_mb", disk>>20, "need_mb", (2*used)>>20)
			break
		}
		start := time.Now()
		if _, err := d.sql.ExecContext(ctx, `VACUUM`); err != nil {
			slog.Warn("storage: vacuum after migration", "err", err)
		} else {
			slog.Info("storage: vacuum after migration", "freed_mb", free>>20, "took", time.Since(start))
		}
	}
	if _, err := d.sql.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		slog.Warn("storage: wal checkpoint after migration", "err", err)
	}
}

// lastUnnamedMigration is the newest version a build shipped without
// recording names. A row above it without a name was written by a build we do
// not know (a branch that reused our number before names existed), so it is
// trusted only when the schema shows our migration ran.
const lastUnnamedMigration = 36

// migrationFingerprints proves, for a version above lastUnnamedMigration, that
// the schema has the current version of our migration: the query returns a
// positive count. A NULL row for a version without a fingerprint is refused.
// Named rows are checked too, so a dev database that ran an earlier draft of
// the file under the same name is refused rather than left half-migrated.
// A fingerprint must hold for the newest schema: a later migration that
// drops what it checks must change it.
//
// 37 checks search_state and text_rev, which earlier drafts of 0037 did not
// have.
var migrationFingerprints = map[int]string{
	37: `SELECT count(*) = 2 FROM pragma_table_info('messages') WHERE name IN ('search_state', 'text_rev')`,
	38: `SELECT count(*) FROM pragma_table_info('messages') WHERE name = 'body_complete'`,
	39: `SELECT count(*) FROM pragma_table_info('accounts') WHERE name = 'sync_max_parallel'`,
	40: `SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_messages_folder_body_date'`,
	41: `SELECT count(*) FROM pragma_table_info('folders') WHERE name = 'last_full_sync_at'`,
	42: `SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_messages_search_stale_stub'`,
}

// checkMigrationNames refuses a database whose applied migration carries
// another file's name: two branches used the same number, and skipping ours
// would leave the schema without it. Rows without a name are trusted up to
// lastUnnamedMigration, and above it only when migrationFingerprints shows
// our schema. A named row with a fingerprint is checked as well: a database
// that ran an earlier draft of the file is refused, since there is no repair
// in place. Every row is checked before any is named, so a refusal changes
// nothing.
func (d *DB) checkMigrationNames(ctx context.Context, migrations []migration, applied map[int]string) error {
	var unnamed []migration
	for _, m := range migrations {
		name, ok := applied[m.version]
		switch {
		case !ok:
		case name == m.name:
			if _, checked := migrationFingerprints[m.version]; !checked {
				continue
			}
			ours, err := d.hasFingerprint(ctx, m.version)
			if err != nil {
				return err
			}
			if !ours {
				return fmt.Errorf("storage: the database ran an earlier draft of migration %d (%s) and lacks its current schema; "+
					"it is a development database that cannot be upgraded in place: delete it and let Pelton create a new one",
					m.version, m.name)
			}
		case name != "":
			return fmt.Errorf("storage: the database has migration %d as %q but this build has %q; "+
				"it was opened by a build with a different migration %d and cannot be used by this one",
				m.version, name, m.name, m.version)
		case m.version <= lastUnnamedMigration:
			unnamed = append(unnamed, m)
		default:
			ours, err := d.hasFingerprint(ctx, m.version)
			if err != nil {
				return err
			}
			if !ours {
				return fmt.Errorf("storage: the database has migration %d without a name and without the schema of %q; "+
					"it was opened by a build with a different migration %d and cannot be used by this one",
					m.version, m.name, m.version)
			}
			unnamed = append(unnamed, m)
		}
	}
	if len(unnamed) == 0 {
		return nil
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("storage: begin naming migrations: %w", err)
	}
	defer tx.Rollback()
	for _, m := range unnamed {
		if _, err := tx.ExecContext(ctx,
			`UPDATE schema_migrations SET name = ? WHERE id = ? AND name IS NULL`, m.name, m.version); err != nil {
			return fmt.Errorf("storage: record name of migration %d: %w", m.version, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("storage: commit migration names: %w", err)
	}
	return nil
}

// hasFingerprint reports whether the schema shows our migration version ran.
func (d *DB) hasFingerprint(ctx context.Context, version int) (bool, error) {
	q, ok := migrationFingerprints[version]
	if !ok {
		return false, nil
	}
	var n int
	if err := d.sql.QueryRowContext(ctx, q).Scan(&n); err != nil {
		return false, fmt.Errorf("storage: inspect schema for migration %d: %w", version, err)
	}
	return n > 0, nil
}

// recordMigration marks m applied inside its transaction.
func recordMigration(ctx context.Context, tx *sql.Tx, m migration) error {
	const insert = `INSERT INTO schema_migrations (id, applied_at, name) VALUES (?, ?, ?)`
	if _, err := tx.ExecContext(ctx, insert, m.version, nowText(), m.name); err != nil {
		return fmt.Errorf("storage: record migration %d: %w", m.version, err)
	}
	return nil
}

func (d *DB) applyMigration(ctx context.Context, m migration) error {
	if isRebuild(m) {
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
	if err := recordMigration(ctx, tx, m); err != nil {
		return err
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
		if fkErr := enableForeignKeys(conn); fkErr != nil {
			// Never hand a connection with foreign keys off back to the pool:
			// ErrBadConn from Raw makes database/sql close it instead.
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
			if err == nil {
				err = fmt.Errorf("storage: foreign keys after migration %d: %w", m.version, fkErr)
			}
		}
	}()

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("storage: begin migration %d: %w", m.version, err)
	}
	defer tx.Rollback()

	// Orphans that were already there (a database edited with foreign keys
	// off) are not this migration's doing and must not block the upgrade.
	// Only violations the migration introduced fail it.
	before, err := foreignKeyViolations(ctx, tx)
	if err != nil {
		return fmt.Errorf("storage: foreign_key_check before migration %d: %w", m.version, err)
	}
	if _, err := tx.ExecContext(ctx, m.sql); err != nil {
		return fmt.Errorf("storage: run migration %d (%s): %w", m.version, m.name, err)
	}
	after, err := foreignKeyViolations(ctx, tx)
	if err != nil {
		return fmt.Errorf("storage: foreign_key_check after migration %d: %w", m.version, err)
	}
	known := make(map[fkViolation]bool, len(before))
	for _, v := range before {
		known[v] = true
	}
	for _, v := range after {
		if !known[v] {
			return fmt.Errorf("storage: foreign key violation after migration %d: table=%s rowid=%d parent=%s",
				m.version, v.table, v.rowid, v.parent)
		}
	}

	if err := recordMigration(ctx, tx, m); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("storage: commit migration %d: %w", m.version, err)
	}
	return nil
}

// enableForeignKeys turns foreign keys back on and checks they are. A var so
// tests can make it fail.
var enableForeignKeys = func(conn *sql.Conn) error {
	ctx := context.Background()
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
		return err
	}
	var on int
	if err := conn.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&on); err != nil {
		return err
	}
	if on != 1 {
		return errors.New("still off")
	}
	return nil
}

// fkViolation is one PRAGMA foreign_key_check row. The fkid column is left
// out on purpose: it is a position in the table's constraint list, which a
// rebuilt table may number differently.
type fkViolation struct {
	table  string
	rowid  int64
	parent string
}

func foreignKeyViolations(ctx context.Context, tx *sql.Tx) ([]fkViolation, error) {
	rows, err := tx.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []fkViolation
	for rows.Next() {
		var v fkViolation
		var rowid sql.NullInt64
		var fkid int
		if err := rows.Scan(&v.table, &rowid, &v.parent, &fkid); err != nil {
			return nil, err
		}
		v.rowid = rowid.Int64
		out = append(out, v)
	}
	return out, rows.Err()
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
