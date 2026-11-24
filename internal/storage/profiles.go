package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrProfileNotFound is returned when a profile id has no row.
var ErrProfileNotFound = errors.New("storage: profile not found")

// ErrMainProfileUndeletable is returned by DeleteProfile for the main profile.
// It owns the rows every sharing profile reads, and switching has to have
// somewhere to land, so it can be renamed but never removed.
var ErrMainProfileUndeletable = errors.New("storage: the main profile cannot be deleted")

// Profile is one workspace within the install: which accounts it shows, and
// whether its settings, signatures and saved views are its own or the main
// profile's. The mail itself is never per profile; a message cached for an
// account is the same message in every profile that shows it.
type Profile struct {
	ID   int64
	Name string
	// Icon is an emoji or short glyph shown next to the name.
	Icon     string
	Position int
	// Main marks the profile the install started as.
	Main bool
	// Active marks the profile the app is currently in. Exactly one row has it.
	Active bool
	// ShareSettings, ShareSignatures and ShareViews make the profile read and
	// write the main profile's rows for that area instead of its own, so a
	// change is visible in both.
	ShareSettings   bool
	ShareSignatures bool
	ShareViews      bool
	// ShareLayout does the same for the sidebar layout: which folders are
	// pinned, and the order of folders and account sections. A profile that
	// shares keeps its own layout rows, unused, so unticking it brings the
	// arrangement back rather than starting over.
	ShareLayout bool
	CreatedAt   time.Time
}

const profileColumns = `id, name, icon, position, is_main, is_active,
       share_settings, share_signatures, share_views, share_layout, created_at`

// ListProfiles returns every profile, main first and the rest by position then
// id, which is the order the switcher shows them in.
func (d *DB) ListProfiles(ctx context.Context) ([]Profile, error) {
	query := `SELECT ` + profileColumns + ` FROM profiles ORDER BY is_main DESC, position, id`
	rows, err := d.sql.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("storage: list profiles: %w", err)
	}
	defer rows.Close()

	var out []Profile
	for rows.Next() {
		p, err := scanProfile(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan profile: %w", err)
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// GetProfile returns one profile by id, or ErrProfileNotFound.
func (d *DB) GetProfile(ctx context.Context, id int64) (*Profile, error) {
	query := `SELECT ` + profileColumns + ` FROM profiles WHERE id = ?`
	p, err := scanProfile(d.sql.QueryRowContext(ctx, query, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrProfileNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: get profile %d: %w", id, err)
	}
	return p, nil
}

// ActiveProfile returns the profile the app is in. An install whose active flag
// somehow went missing falls back to main rather than failing: there is always
// a profile to be in.
func (d *DB) ActiveProfile(ctx context.Context) (*Profile, error) {
	query := `SELECT ` + profileColumns + ` FROM profiles ORDER BY is_active DESC, is_main DESC, id LIMIT 1`
	p, err := scanProfile(d.sql.QueryRowContext(ctx, query))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrProfileNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: active profile: %w", err)
	}
	return p, nil
}

// MainProfile returns the profile the install started as.
func (d *DB) MainProfile(ctx context.Context) (*Profile, error) {
	query := `SELECT ` + profileColumns + ` FROM profiles WHERE is_main = 1`
	p, err := scanProfile(d.sql.QueryRowContext(ctx, query))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrProfileNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: main profile: %w", err)
	}
	return p, nil
}

// CreateProfile inserts a profile and returns its new id. Main and Active are
// ignored: an install has exactly one main profile, and switching is what makes
// one active.
func (d *DB) CreateProfile(ctx context.Context, p *Profile) (int64, error) {
	const query = `
INSERT INTO profiles (name, icon, position, is_main, is_active,
                      share_settings, share_signatures, share_views, share_layout, created_at)
VALUES (?, ?, ?, 0, 0, ?, ?, ?, ?, ?)`
	res, err := d.sql.ExecContext(ctx, query,
		p.Name, p.Icon, p.Position,
		boolToInt(p.ShareSettings), boolToInt(p.ShareSignatures), boolToInt(p.ShareViews),
		boolToInt(p.ShareLayout), nowText())
	if err != nil {
		return 0, fmt.Errorf("storage: insert profile %q: %w", p.Name, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("storage: profile insert id: %w", err)
	}
	p.ID = id
	return id, nil
}

// UpdateProfile saves a profile's name, icon and sharing switches. Whether it is
// main or active is not editable here.
func (d *DB) UpdateProfile(ctx context.Context, p Profile) error {
	const query = `
UPDATE profiles
SET name = ?, icon = ?, share_settings = ?, share_signatures = ?, share_views = ?, share_layout = ?
WHERE id = ?`
	res, err := d.sql.ExecContext(ctx, query,
		p.Name, p.Icon,
		boolToInt(p.ShareSettings), boolToInt(p.ShareSignatures), boolToInt(p.ShareViews),
		boolToInt(p.ShareLayout), p.ID)
	if err != nil {
		return fmt.Errorf("storage: update profile %d: %w", p.ID, err)
	}
	return requireOneRow(res, ErrProfileNotFound)
}

// DeleteProfile removes a profile along with its own settings, signatures,
// saved views and account visibility. The accounts and their mail are the
// install's and stay. Deleting the main profile is refused.
//
// Deleting the active profile leaves the app on a profile that no longer
// exists, so the caller switches first; ActiveProfile falls back to main
// either way.
func (d *DB) DeleteProfile(ctx context.Context, id int64) error {
	profile, err := d.GetProfile(ctx, id)
	if err != nil {
		return err
	}
	if profile.Main {
		return ErrMainProfileUndeletable
	}

	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("storage: begin delete profile %d: %w", id, err)
	}
	defer tx.Rollback()

	// the rows a profile owns. profile_accounts cascades, but settings,
	// signatures and views are deleted explicitly so the intent is on the page
	// rather than in the schema.
	for _, stmt := range []string{
		`DELETE FROM settings WHERE profile_id = ?`,
		`DELETE FROM signatures WHERE profile_id = ?`,
		`DELETE FROM views WHERE profile_id = ?`,
		`DELETE FROM profile_accounts WHERE profile_id = ?`,
		`DELETE FROM profiles WHERE id = ?`,
	} {
		if _, err := tx.ExecContext(ctx, stmt, id); err != nil {
			return fmt.Errorf("storage: delete profile %d: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("storage: commit delete profile %d: %w", id, err)
	}
	return nil
}

// SetActiveProfile makes one profile the active one and clears the flag from
// every other, in a single transaction so there is never a moment with two.
func (d *DB) SetActiveProfile(ctx context.Context, id int64) error {
	if _, err := d.GetProfile(ctx, id); err != nil {
		return err
	}

	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("storage: begin activate profile %d: %w", id, err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `UPDATE profiles SET is_active = 0 WHERE is_active = 1`); err != nil {
		return fmt.Errorf("storage: clear active profile: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE profiles SET is_active = 1 WHERE id = ?`, id); err != nil {
		return fmt.Errorf("storage: activate profile %d: %w", id, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("storage: commit activate profile %d: %w", id, err)
	}
	return nil
}

// ProfileAccountIDs returns the accounts a profile shows.
func (d *DB) ProfileAccountIDs(ctx context.Context, profileID int64) ([]int64, error) {
	const query = `SELECT account_id FROM profile_accounts WHERE profile_id = ? ORDER BY account_id`
	rows, err := d.sql.QueryContext(ctx, query, profileID)
	if err != nil {
		return nil, fmt.Errorf("storage: list profile %d accounts: %w", profileID, err)
	}
	defer rows.Close()

	out := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("storage: scan profile account: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// SetProfileAccounts replaces the set of accounts a profile shows.
func (d *DB) SetProfileAccounts(ctx context.Context, profileID int64, accountIDs []int64) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("storage: begin set profile %d accounts: %w", profileID, err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM profile_accounts WHERE profile_id = ?`, profileID); err != nil {
		return fmt.Errorf("storage: clear profile %d accounts: %w", profileID, err)
	}
	const insert = `INSERT OR IGNORE INTO profile_accounts (profile_id, account_id) VALUES (?, ?)`
	for _, accountID := range accountIDs {
		if _, err := tx.ExecContext(ctx, insert, profileID, accountID); err != nil {
			return fmt.Errorf("storage: add account %d to profile %d: %w", accountID, profileID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("storage: commit profile %d accounts: %w", profileID, err)
	}
	return nil
}

// AddAccountToProfiles makes a new account visible in the given profiles. An
// account nobody can see would be invisible mail, so callers pass at least the
// profile it was created in.
func (d *DB) AddAccountToProfiles(ctx context.Context, accountID int64, profileIDs []int64) error {
	const insert = `INSERT OR IGNORE INTO profile_accounts (profile_id, account_id) VALUES (?, ?)`
	for _, profileID := range profileIDs {
		if _, err := d.sql.ExecContext(ctx, insert, profileID, accountID); err != nil {
			return fmt.Errorf("storage: add account %d to profile %d: %w", accountID, profileID, err)
		}
	}
	return nil
}

// CopyProfileSettings duplicates one profile's settings rows onto another,
// which is what "copy from" means at creation time: a snapshot, not a link.
// Existing rows on the destination are overwritten.
func (d *DB) CopyProfileSettings(ctx context.Context, from, to int64) error {
	const query = `
INSERT INTO settings (key, profile_id, value, updated_at)
SELECT key, ?, value, updated_at FROM settings WHERE profile_id = ?
ON CONFLICT(key, profile_id) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`
	if _, err := d.sql.ExecContext(ctx, query, to, from); err != nil {
		return fmt.Errorf("storage: copy settings %d to %d: %w", from, to, err)
	}
	return nil
}

// CopyProfileSignatures duplicates one profile's signatures onto another. The
// copies are new rows, so editing one afterwards leaves the original alone.
func (d *DB) CopyProfileSignatures(ctx context.Context, from, to int64) error {
	const query = `
INSERT INTO signatures (name, kind, format, content, created_at, updated_at, profile_id)
SELECT name, kind, format, content, created_at, updated_at, ?
FROM signatures WHERE profile_id = ?`
	if _, err := d.sql.ExecContext(ctx, query, to, from); err != nil {
		return fmt.Errorf("storage: copy signatures %d to %d: %w", from, to, err)
	}
	return nil
}

// CopyProfileViews duplicates one profile's saved views onto another.
func (d *DB) CopyProfileViews(ctx context.Context, from, to int64) error {
	const query = `
INSERT INTO views (name, icon, color, query_text, query_from, query_to, query_subject,
                   use_regex, within_days, unread_only, flagged_only, has_attachment,
                   account_id, position, created_at, updated_at, profile_id)
SELECT name, icon, color, query_text, query_from, query_to, query_subject,
       use_regex, within_days, unread_only, flagged_only, has_attachment,
       account_id, position, created_at, updated_at, ?
FROM views WHERE profile_id = ?`
	if _, err := d.sql.ExecContext(ctx, query, to, from); err != nil {
		return fmt.Errorf("storage: copy views %d to %d: %w", from, to, err)
	}
	return nil
}

// CopyProfileLayout duplicates one profile's sidebar layout onto another: its
// pinned folders, its folder order and its account section order. Like the
// other copies it is a snapshot, so rearranging one afterwards leaves the other
// alone.
func (d *DB) CopyProfileLayout(ctx context.Context, from, to int64) error {
	const folders = `
INSERT INTO profile_sidebar_layout (profile_id, folder_id, position, pinned_position)
SELECT ?, folder_id, position, pinned_position FROM profile_sidebar_layout WHERE profile_id = ?
ON CONFLICT(profile_id, folder_id) DO UPDATE
SET position = excluded.position, pinned_position = excluded.pinned_position`
	if _, err := d.sql.ExecContext(ctx, folders, to, from); err != nil {
		return fmt.Errorf("storage: copy folder layout %d to %d: %w", from, to, err)
	}
	const accounts = `
INSERT INTO profile_account_order (profile_id, account_id, position)
SELECT ?, account_id, position FROM profile_account_order WHERE profile_id = ?
ON CONFLICT(profile_id, account_id) DO UPDATE SET position = excluded.position`
	if _, err := d.sql.ExecContext(ctx, accounts, to, from); err != nil {
		return fmt.Errorf("storage: copy account order %d to %d: %w", from, to, err)
	}
	return nil
}

func scanProfile(row rowScanner) (*Profile, error) {
	var (
		p          Profile
		main       int
		active     int
		settings   int
		signatures int
		views      int
		layout     int
		created    string
	)
	if err := row.Scan(&p.ID, &p.Name, &p.Icon, &p.Position, &main, &active,
		&settings, &signatures, &views, &layout, &created); err != nil {
		return nil, err
	}
	p.Main = main != 0
	p.Active = active != 0
	p.ShareSettings = settings != 0
	p.ShareSignatures = signatures != 0
	p.ShareViews = views != 0
	p.ShareLayout = layout != 0
	t, err := parseTime(created)
	if err != nil {
		return nil, err
	}
	p.CreatedAt = t
	return &p, nil
}
