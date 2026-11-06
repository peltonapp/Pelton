package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
)

// ErrSettingNotFound is returned by Get and the typed getters when a key has
// never been set, so the caller decides between a default and an error rather
// than receiving a silent empty string.
var ErrSettingNotFound = errors.New("storage: setting not found")

// Known ui setting keys. Callers should use these rather than raw strings.
const (
	SettingTheme               = "theme"
	SettingEditorMode          = "editor_mode"
	SettingWindowSize          = "window_size"
	SettingLastSelectedAccount = "last_selected_account"
	SettingLastSelectedFolder  = "last_selected_folder"
	// SettingProxy holds the outbound proxy preference as json (everything but
	// the password, which lives in the keyring).
	SettingProxy = "proxy"
)

// Every function here reads and writes the settings of the profile the app is
// in, which the store resolves once per switch: a profile that shares settings
// with main reads and writes main's rows, so neither the callers nor the keys
// change. See profilescope.go.

// Get returns the raw stored string for key, or ErrSettingNotFound.
func (d *DB) Get(ctx context.Context, key string) (string, error) {
	var value string
	err := d.sql.QueryRowContext(ctx,
		`SELECT value FROM settings WHERE key = ? AND profile_id = ?`,
		key, d.settingsProfile()).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrSettingNotFound
	}
	if err != nil {
		return "", fmt.Errorf("storage: get setting %q: %w", key, err)
	}
	return value, nil
}

// upsertSetting writes one setting, inserting or updating, with its write time.
const upsertSetting = `
INSERT INTO settings (key, profile_id, value, updated_at) VALUES (?, ?, ?, ?)
ON CONFLICT(key, profile_id) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`

// Set writes key to value, inserting or updating, and bumps updated_at.
func (d *DB) Set(ctx context.Context, key, value string) error {
	if _, err := d.sql.ExecContext(ctx, upsertSetting, key, d.settingsProfile(), value, nowText()); err != nil {
		return fmt.Errorf("storage: set setting %q: %w", key, err)
	}
	return nil
}

// GetInt reads a setting as an int.
func (d *DB) GetInt(ctx context.Context, key string) (int, error) {
	raw, err := d.Get(ctx, key)
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("storage: setting %q is not an int: %w", key, err)
	}
	return n, nil
}

// SetInt writes an int setting.
func (d *DB) SetInt(ctx context.Context, key string, value int) error {
	return d.Set(ctx, key, strconv.Itoa(value))
}

// GetBool reads a setting as a bool.
func (d *DB) GetBool(ctx context.Context, key string) (bool, error) {
	raw, err := d.Get(ctx, key)
	if err != nil {
		return false, err
	}
	b, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("storage: setting %q is not a bool: %w", key, err)
	}
	return b, nil
}

// SetBool writes a bool setting.
func (d *DB) SetBool(ctx context.Context, key string, value bool) error {
	return d.Set(ctx, key, strconv.FormatBool(value))
}

// GetJSON reads a setting and unmarshals it into target, which must be a
// non nil pointer.
func (d *DB) GetJSON(ctx context.Context, key string, target any) error {
	raw, err := d.Get(ctx, key)
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(raw), target); err != nil {
		return fmt.Errorf("storage: decode json setting %q: %w", key, err)
	}
	return nil
}

// SetJSON marshals value to json and writes it as a setting.
func (d *DB) SetJSON(ctx context.Context, key string, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("storage: encode json setting %q: %w", key, err)
	}
	return d.Set(ctx, key, string(encoded))
}

// SettingsScope is the profile whose settings rows the store reads and writes
// right now. Work that must stay with the profile it started in, across a
// switch, keeps it and reads and writes with GetJSONIn and SetJSONsIn.
func (d *DB) SettingsScope() int64 { return d.settingsProfile() }

// GetJSONIn is GetJSON on the settings of profile scope.
func (d *DB) GetJSONIn(ctx context.Context, scope int64, key string, target any) error {
	var raw string
	err := d.sql.QueryRowContext(ctx,
		`SELECT value FROM settings WHERE key = ? AND profile_id = ?`, key, scope).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrSettingNotFound
	}
	if err != nil {
		return fmt.Errorf("storage: get setting %q: %w", key, err)
	}
	if err := json.Unmarshal([]byte(raw), target); err != nil {
		return fmt.Errorf("storage: decode json setting %q: %w", key, err)
	}
	return nil
}

// ScopedSetting names one setting of one profile.
type ScopedSetting struct {
	Scope int64
	Key   string
}

// SetJSONsIn marshals each value to json and writes them all in one
// transaction, each to its profile: either every one is written or none is.
func (d *DB) SetJSONsIn(ctx context.Context, values map[ScopedSetting]any) error {
	encoded := make(map[ScopedSetting]string, len(values))
	for at, value := range values {
		raw, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("storage: encode json setting %q: %w", at.Key, err)
		}
		encoded[at] = string(raw)
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("storage: begin settings write: %w", err)
	}
	defer tx.Rollback()
	now := nowText()
	for at, value := range encoded {
		if _, err := tx.ExecContext(ctx, upsertSetting, at.Key, at.Scope, value, now); err != nil {
			return fmt.Errorf("storage: set setting %q: %w", at.Key, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("storage: commit settings write: %w", err)
	}
	return nil
}

// Setting pairs a key/value with its last write time, used by config sync to
// compare local state against an exported snapshot.
type Setting struct {
	Key       string
	Value     string
	UpdatedAt string
}

// AllSettings returns every stored setting, for exporting a full snapshot.
func (d *DB) AllSettings(ctx context.Context) ([]Setting, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT key, value, updated_at FROM settings WHERE profile_id = ?`, d.settingsProfile())
	if err != nil {
		return nil, fmt.Errorf("storage: list settings: %w", err)
	}
	defer rows.Close()

	var out []Setting
	for rows.Next() {
		var s Setting
		if err := rows.Scan(&s.Key, &s.Value, &s.UpdatedAt); err != nil {
			return nil, fmt.Errorf("storage: scan setting: %w", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate settings: %w", err)
	}
	return out, nil
}

// SetIfNewer writes key to value only if there is no existing row or the
// existing row's updated_at is not newer than updatedAt, so importing a
// snapshot never clobbers a more recent local change (last write wins).
func (d *DB) SetIfNewer(ctx context.Context, key, value, updatedAt string) error {
	const query = `
INSERT INTO settings (key, profile_id, value, updated_at) VALUES (?, ?, ?, ?)
ON CONFLICT(key, profile_id) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
WHERE excluded.updated_at >= settings.updated_at`
	if _, err := d.sql.ExecContext(ctx, query, key, d.settingsProfile(), value, updatedAt); err != nil {
		return fmt.Errorf("storage: set setting if newer %q: %w", key, err)
	}
	return nil
}
