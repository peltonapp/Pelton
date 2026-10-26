package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// ErrFolderNotFound is returned when a folder id has no row.
var ErrFolderNotFound = errors.New("storage: folder not found")

// Well known mailbox names. Servers may differ, but these are the common
// special use folders callers reach for when one is not reported explicitly.
const (
	FolderInbox   = "INBOX"
	FolderSent    = "Sent"
	FolderDrafts  = "Drafts"
	FolderTrash   = "Trash"
	FolderJunk    = "Junk"
	FolderArchive = "Archive"
)

// attributeSeparator joins folder attributes into the single text column.
const attributeSeparator = " "

// folderColumns is the select list every folder query shares, in the order
// scanFolder reads them. The two ranks come from the layout join below rather
// than from the folder row: they belong to a profile, not to the install
// (#325). A folder the profile never arranged has no layout row, and coalesce
// turns that into 0, which is what "never reordered" has always meant.
const folderColumns = `f.id, f.account_id, f.name, f.imap_path, f.delimiter, f.parent_id,
       f.attributes, f.uid_validity,
       coalesce(l.position, 0), coalesce(l.pinned_position, 0),
       f.role_override, f.sync_excluded,
       f.remote_id, f.state_token, f.sync_floor_id, f.sync_initialized, f.last_full_sync_at`

// folderFrom joins a folder to the active profile's layout. Every query using
// folderColumns takes the layout profile id as its first argument.
const folderFrom = `
FROM folders f
LEFT JOIN profile_sidebar_layout l ON l.folder_id = f.id AND l.profile_id = ?`

// folderOrder sorts folders for display: reordered groups first in the order the
// user chose, then everything untouched in discovery (id) order. In sqlite
// `position = 0` is 1 for the untouched rows, which is why they sort last.
const folderOrder = `ORDER BY coalesce(l.position, 0) = 0, l.position, f.id`

// Folder is one mailbox in an account's hierarchy.
type Folder struct {
	ID        int64
	AccountID int64
	Name      string
	// IMAPPath is the raw mailbox name the server returned. Hierarchy is encoded
	// with Delimiter, which varies per server, so neither is ever assumed.
	IMAPPath    string
	Delimiter   string
	ParentID    *int64
	Attributes  []string
	UIDValidity uint32
	// Position is the folder's rank among its siblings in the sidebar, or 0 when
	// the user has never reordered that group. Unpositioned folders sort after
	// positioned ones by id, so discovery order survives and a new folder lands
	// at the end rather than the front.
	Position int
	// PinnedPosition is the folder's rank in the sidebar's Pinned group, or 0
	// when it is not pinned. Pinning mirrors the folder into that group; it stays
	// in its account's tree either way.
	PinnedPosition int
	// RoleOverride is the role the user assigned by hand, or empty to detect it
	// from the server's special-use attribute and the folder name. It wins over
	// both, because no amount of detection covers every server's naming.
	RoleOverride string
	// SyncExcluded means the user unchecked this folder, so sync skips it and
	// nothing in it is fetched. A 30k-message archive nobody reads is the case
	// this exists for. It stays in the sidebar, since hiding it would make the
	// setting impossible to find again.
	SyncExcluded bool
	// RemoteID is the stable server id for the mailbox: the imap path for IMAP.
	// StateToken is the delta cursor after a successful snapshot: the IMAP
	// CONDSTORE cursor (empty without CONDSTORE). SyncFloorID is an opaque
	// newest-window boundary for an adapter without numeric ids (empty when
	// nothing older remains). SyncInitialized
	// distinguishes a first sync from an already-synced empty folder.
	RemoteID        string
	StateToken      string
	SyncFloorID     string
	SyncInitialized bool
	// LastFullSyncAt is when the folder last finished a full reconcile (a
	// complete server listing rather than a delta). Zero means never.
	LastFullSyncAt time.Time
}

// CreateFolder inserts a folder and returns its new id.
func (d *DB) CreateFolder(ctx context.Context, f *Folder) (int64, error) {
	if f.RemoteID == "" {
		f.RemoteID = f.IMAPPath
	}
	const query = `
INSERT INTO folders (account_id, name, imap_path, delimiter, parent_id, attributes, uid_validity, remote_id, state_token, sync_floor_id)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	res, err := d.sql.ExecContext(ctx, query,
		f.AccountID, f.Name, f.IMAPPath, f.Delimiter, nullableID(f.ParentID),
		joinAttributes(f.Attributes), f.UIDValidity, f.RemoteID, f.StateToken, f.SyncFloorID)
	if err != nil {
		return 0, fmt.Errorf("storage: insert folder %q: %w", f.IMAPPath, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("storage: folder insert id: %w", err)
	}
	f.ID = id
	return id, nil
}

// UpsertFolderByRemoteID inserts a folder or updates name, imap_path, delimiter,
// attributes, uid_validity and remote_id matched on (account_id, remote_id). It
// does not touch parent_id, sync_excluded or role_override.
func (d *DB) UpsertFolderByRemoteID(ctx context.Context, f *Folder) error {
	if f.RemoteID == "" {
		f.RemoteID = f.IMAPPath
	}
	var existingID int64
	err := d.sql.QueryRowContext(ctx,
		`SELECT id FROM folders WHERE account_id = ? AND remote_id = ?`,
		f.AccountID, f.RemoteID).Scan(&existingID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("storage: lookup folder by remote id %q: %w", f.RemoteID, err)
	}
	if errors.Is(err, sql.ErrNoRows) {
		_, err := d.CreateFolder(ctx, f)
		return err
	}
	const query = `
UPDATE folders
SET name = ?, imap_path = ?, delimiter = ?, attributes = ?, uid_validity = ?, remote_id = ?
WHERE id = ?`
	res, err := d.sql.ExecContext(ctx, query,
		f.Name, f.IMAPPath, f.Delimiter, joinAttributes(f.Attributes), f.UIDValidity, f.RemoteID, existingID)
	if err != nil {
		return fmt.Errorf("storage: update folder by remote id %q: %w", f.RemoteID, err)
	}
	if err := requireOneRow(res, ErrFolderNotFound); err != nil {
		return err
	}
	f.ID = existingID
	return nil
}

// SetFolderStateToken stores the adapter's delta cursor for a folder after a
// successful snapshot.
func (d *DB) SetFolderStateToken(ctx context.Context, id int64, token string) error {
	res, err := d.sql.ExecContext(ctx,
		`UPDATE folders SET state_token = ? WHERE id = ?`, token, id)
	if err != nil {
		return fmt.Errorf("storage: set state token for folder %d: %w", id, err)
	}
	return requireOneRow(res, ErrFolderNotFound)
}

// SetFolderFullSyncAt records when the folder last finished a full reconcile.
func (d *DB) SetFolderFullSyncAt(ctx context.Context, id int64, at time.Time) error {
	res, err := d.sql.ExecContext(ctx,
		`UPDATE folders SET last_full_sync_at = ? WHERE id = ?`, at.Unix(), id)
	if err != nil {
		return fmt.Errorf("storage: set full sync time for folder %d: %w", id, err)
	}
	return requireOneRow(res, ErrFolderNotFound)
}

// SetFolderSyncWindow writes the opaque sync floor and whether the folder
// has completed an initial sync. It does not touch sync_floor_uid.
func (d *DB) SetFolderSyncWindow(ctx context.Context, id int64, floorID string, initialized bool) error {
	res, err := d.sql.ExecContext(ctx,
		`UPDATE folders SET sync_floor_id = ?, sync_initialized = ? WHERE id = ?`,
		floorID, boolToInt(initialized), id)
	if err != nil {
		return fmt.Errorf("storage: set sync window for folder %d: %w", id, err)
	}
	return requireOneRow(res, ErrFolderNotFound)
}

// SetFolderParent records a folder's parent, or clears it when parent is nil.
func (d *DB) SetFolderParent(ctx context.Context, id int64, parent *int64) error {
	res, err := d.sql.ExecContext(ctx,
		`UPDATE folders SET parent_id = ? WHERE id = ?`, nullableID(parent), id)
	if err != nil {
		return fmt.Errorf("storage: set parent for folder %d: %w", id, err)
	}
	return requireOneRow(res, ErrFolderNotFound)
}

// DeleteAccountFolders removes every folder for an account, staging attachment
// files so a transaction failure can restore them. Address books are untouched.
func (d *DB) DeleteAccountFolders(ctx context.Context, accountID int64) error {
	return d.withStagedAccountCache(ctx, accountID, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM folders WHERE account_id = ?`, accountID); err != nil {
			return fmt.Errorf("storage: delete folders for account %d: %w", accountID, err)
		}
		return nil
	})
}

// GetFolder returns one folder by id, or ErrFolderNotFound.
func (d *DB) GetFolder(ctx context.Context, id int64) (*Folder, error) {
	const query = `
SELECT ` + folderColumns + folderFrom + `
WHERE f.id = ?`
	f, err := scanFolder(d.sql.QueryRowContext(ctx, query, d.layoutProfile(), id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrFolderNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: get folder %d: %w", id, err)
	}
	return f, nil
}

// ListFolders returns every folder for an account in sidebar order: groups the
// user has reordered first, in that order, then the rest by id.
func (d *DB) ListFolders(ctx context.Context, accountID int64) ([]Folder, error) {
	const query = `
SELECT ` + folderColumns + folderFrom + `
WHERE f.account_id = ? ` + folderOrder
	rows, err := d.sql.QueryContext(ctx, query, d.layoutProfile(), accountID)
	if err != nil {
		return nil, fmt.Errorf("storage: list folders for account %d: %w", accountID, err)
	}
	defer rows.Close()

	var folders []Folder
	for rows.Next() {
		f, err := scanFolder(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan folder: %w", err)
		}
		folders = append(folders, *f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate folders: %w", err)
	}
	return folders, nil
}

// SetFolderUIDValidity updates the stored UIDVALIDITY for a folder. A change
// means the server reset the mailbox and the cache for it is stale.
func (d *DB) SetFolderUIDValidity(ctx context.Context, id int64, uidValidity uint32) error {
	res, err := d.sql.ExecContext(ctx,
		`UPDATE folders SET uid_validity = ? WHERE id = ?`, uidValidity, id)
	if err != nil {
		return fmt.Errorf("storage: update uid_validity for folder %d: %w", id, err)
	}
	return requireOneRow(res, ErrFolderNotFound)
}

// RenameFolder updates a folder's display name and imap path. A remote id that
// is the imap path (IMAP) follows the new path; any other remote id is kept. It
// does not touch the folder's children: the caller renames the subtree, since
// only it knows the server's delimiter (see RenameFolderSubtree).
func (d *DB) RenameFolder(ctx context.Context, id int64, name, imapPath string) error {
	const query = `
UPDATE folders
SET name = ?, imap_path = ?,
    remote_id = CASE WHEN remote_id = imap_path THEN ? ELSE remote_id END
WHERE id = ?`
	res, err := d.sql.ExecContext(ctx, query, name, imapPath, imapPath, id)
	if err != nil {
		return fmt.Errorf("storage: rename folder %d: %w", id, err)
	}
	return requireOneRow(res, ErrFolderNotFound)
}

// RenameFolderSubtree rewrites the imap path prefix of every descendant of a
// renamed folder, which an imap RENAME moves along with their parent. A remote
// id that is the imap path follows it, as in RenameFolder. oldPrefix and
// newPrefix are the parent paths with the server's delimiter already appended,
// so the match cannot catch a sibling whose name merely starts with the same
// text. Returns the number of rows rewritten.
func (d *DB) RenameFolderSubtree(ctx context.Context, accountID int64, oldPrefix, newPrefix string) (int, error) {
	// escape the like wildcards in the stored path so a folder legitimately
	// containing % or _ does not match half the account.
	const query = `
UPDATE folders
SET imap_path = ? || substr(imap_path, ?),
    remote_id = CASE WHEN remote_id = imap_path
                     THEN ? || substr(imap_path, ?) ELSE remote_id END
WHERE account_id = ? AND imap_path LIKE ? ESCAPE '\'`
	// substr counts characters, so the offset must too: a byte count would cut
	// into a child's path under a non-ascii parent.
	from := utf8.RuneCountInString(oldPrefix) + 1
	res, err := d.sql.ExecContext(ctx, query,
		newPrefix, from, newPrefix, from, accountID, escapeLike(oldPrefix)+"%")
	if err != nil {
		return 0, fmt.Errorf("storage: rename folder subtree %q: %w", oldPrefix, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("storage: rename subtree rows affected: %w", err)
	}
	return int(n), nil
}

// FolderDescendants returns every folder nested under a folder, deepest last,
// so a caller can tear a subtree down parent-last or build it parent-first. The
// folder itself is not included.
func (d *DB) FolderDescendants(ctx context.Context, accountID int64, prefix string) ([]Folder, error) {
	const query = `SELECT ` + folderColumns + folderFrom + `
WHERE f.account_id = ? AND f.imap_path LIKE ? ESCAPE '\'
ORDER BY length(f.imap_path)`
	rows, err := d.sql.QueryContext(ctx, query, d.layoutProfile(), accountID, escapeLike(prefix)+"%")
	if err != nil {
		return nil, fmt.Errorf("storage: list folder descendants of %q: %w", prefix, err)
	}
	defer rows.Close()

	var folders []Folder
	for rows.Next() {
		f, err := scanFolder(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan folder descendant: %w", err)
		}
		folders = append(folders, *f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate folder descendants: %w", err)
	}
	return folders, nil
}

// ListPinnedFolders returns every pinned folder across all accounts in the order
// the user arranged the Pinned group, shallowest rank first.
func (d *DB) ListPinnedFolders(ctx context.Context) ([]Folder, error) {
	const query = `SELECT ` + folderColumns + folderFrom + `
WHERE l.pinned_position > 0 ORDER BY l.pinned_position, f.id`
	rows, err := d.sql.QueryContext(ctx, query, d.layoutProfile())
	if err != nil {
		return nil, fmt.Errorf("storage: list pinned folders: %w", err)
	}
	defer rows.Close()

	var folders []Folder
	for rows.Next() {
		f, err := scanFolder(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan pinned folder: %w", err)
		}
		folders = append(folders, *f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate pinned folders: %w", err)
	}
	return folders, nil
}

// SetFolderPositions rewrites the sidebar order of a folder group in one
// transaction, so a drag reorder persists atomically. Callers pass the ids of
// one sibling group; folders not listed keep their current position. Positions
// start at 1, since 0 means "never reordered" (see folderOrder).
func (d *DB) SetFolderPositions(ctx context.Context, orderedIDs []int64) error {
	return d.setLayoutPositions(ctx, `
INSERT INTO profile_sidebar_layout (profile_id, folder_id, position)
VALUES (?1, ?2, ?3)
ON CONFLICT(profile_id, folder_id) DO UPDATE SET position = excluded.position`,
		orderedIDs, "folders")
}

// SetPinnedFolderPositions rewrites the order of the Pinned group in one
// transaction. Every id passed must already be pinned; this only reorders, it
// does not pin (see SetFolderPinned).
func (d *DB) SetPinnedFolderPositions(ctx context.Context, orderedIDs []int64) error {
	// no insert here: a folder with no layout row is not pinned, and reordering
	// must not be a way to pin one.
	return d.setLayoutPositions(ctx, `
UPDATE profile_sidebar_layout SET pinned_position = ?3
WHERE profile_id = ?1 AND folder_id = ?2 AND pinned_position > 0`,
		orderedIDs, "pinned folders")
}

// SetFolderPinned pins or unpins a folder. Pinning appends it to the end of the
// Pinned group; unpinning clears its rank and leaves the gap, which the next
// reorder closes. Pinning an already-pinned folder (or unpinning an unpinned
// one) is a no-op rather than an error.
func (d *DB) SetFolderPinned(ctx context.Context, id int64, pinned bool) error {
	// a folder the profile never arranged has no layout row at all, so neither
	// branch below can tell a missing folder from an untouched one. Asking first
	// keeps a pin against a deleted folder an error rather than a silent no-op.
	if _, err := d.GetFolder(ctx, id); err != nil {
		return err
	}
	profileID := d.layoutProfile()
	if !pinned {
		_, err := d.sql.ExecContext(ctx,
			`UPDATE profile_sidebar_layout SET pinned_position = 0 WHERE profile_id = ? AND folder_id = ?`,
			profileID, id)
		if err != nil {
			return fmt.Errorf("storage: unpin folder %d: %w", id, err)
		}
		return nil
	}
	// coalesce covers the empty-group case, where max() over no rows is null.
	const query = `
INSERT INTO profile_sidebar_layout (profile_id, folder_id, pinned_position)
VALUES (?1, ?2, (SELECT coalesce(max(pinned_position), 0) + 1 FROM profile_sidebar_layout WHERE profile_id = ?1))
ON CONFLICT(profile_id, folder_id) DO UPDATE
SET pinned_position = (SELECT coalesce(max(pinned_position), 0) + 1 FROM profile_sidebar_layout WHERE profile_id = ?1)
WHERE pinned_position = 0`
	// zero rows means it was already pinned here, which the conflict clause
	// leaves alone rather than moving it to the end of the group.
	if _, err := d.sql.ExecContext(ctx, query, profileID, id); err != nil {
		return fmt.Errorf("storage: pin folder %d: %w", id, err)
	}
	return nil
}

// SetFolderRoleOverride records the role the user assigned to a folder by hand.
// An empty role clears the override and hands the folder back to automatic
// detection. Validating the role itself is the caller's job.
func (d *DB) SetFolderRoleOverride(ctx context.Context, id int64, role string) error {
	res, err := d.sql.ExecContext(ctx,
		`UPDATE folders SET role_override = ? WHERE id = ?`, role, id)
	if err != nil {
		return fmt.Errorf("storage: set folder %d role override: %w", id, err)
	}
	return requireOneRow(res, ErrFolderNotFound)
}

// SetFolderSyncExcluded records whether sync skips a folder. Excluding one does
// not delete what is already cached: the messages stay readable offline and
// simply stop being updated, so unchecking a folder by mistake costs nothing.
func (d *DB) SetFolderSyncExcluded(ctx context.Context, id int64, excluded bool) error {
	res, err := d.sql.ExecContext(ctx,
		`UPDATE folders SET sync_excluded = ? WHERE id = ?`, boolToInt(excluded), id)
	if err != nil {
		return fmt.Errorf("storage: set folder %d sync excluded: %w", id, err)
	}
	return requireOneRow(res, ErrFolderNotFound)
}

// setLayoutPositions runs one position-rewriting statement per id inside a
// single transaction, so a drag persists all at once or not at all. The
// statement takes the layout profile as ?1, the row id as ?2 and the new
// position as ?3.
func (d *DB) setLayoutPositions(ctx context.Context, stmt string, orderedIDs []int64, what string) error {
	profileID := d.layoutProfile()
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("storage: begin reorder %s: %w", what, err)
	}
	defer tx.Rollback()

	for pos, id := range orderedIDs {
		if _, err := tx.ExecContext(ctx, stmt, profileID, id, pos+1); err != nil {
			return fmt.Errorf("storage: reorder %s, id %d: %w", what, id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("storage: commit reorder %s: %w", what, err)
	}
	return nil
}

// DeleteFolder removes a folder; its messages and their attachment rows cascade.
func (d *DB) DeleteFolder(ctx context.Context, id int64) error {
	res, err := d.sql.ExecContext(ctx, `DELETE FROM folders WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("storage: delete folder %d: %w", id, err)
	}
	return requireOneRow(res, ErrFolderNotFound)
}

func scanFolder(row rowScanner) (*Folder, error) {
	var (
		f           Folder
		parent      sql.NullInt64
		attrs       string
		excluded    int
		initialized int
		fullSync    sql.NullInt64
	)
	if err := row.Scan(&f.ID, &f.AccountID, &f.Name, &f.IMAPPath, &f.Delimiter,
		&parent, &attrs, &f.UIDValidity, &f.Position, &f.PinnedPosition,
		&f.RoleOverride, &excluded,
		&f.RemoteID, &f.StateToken, &f.SyncFloorID, &initialized, &fullSync); err != nil {
		return nil, err
	}
	f.SyncExcluded = excluded != 0
	f.SyncInitialized = initialized != 0
	if fullSync.Valid {
		f.LastFullSyncAt = time.Unix(fullSync.Int64, 0).UTC()
	}
	if parent.Valid {
		f.ParentID = &parent.Int64
	}
	f.Attributes = splitAttributes(attrs)
	return &f, nil
}

func joinAttributes(attrs []string) string {
	return strings.Join(attrs, attributeSeparator)
}

func splitAttributes(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, attributeSeparator)
}

// nullableID maps an optional id to a value sql can store as NULL.
func nullableID(id *int64) any {
	if id == nil {
		return nil
	}
	return *id
}
