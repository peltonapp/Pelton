package scripting

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrNotFound means the script or run is gone.
var ErrNotFound = errors.New("scripting: not found")

// Store keeps scripts, their run log and their key value storage in SQLite.
// The tables come from the storage migrations 0101 to 0103.
type Store struct {
	db *sql.DB
}

// maxKVKeys caps the keys one script stores.
const maxKVKeys = 1000

// NewStore wraps an open database.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "null"
	}
	return string(b)
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

const scriptColumns = `id, name, description, kind, source, blocks, trigger, permissions, approved,
enabled, origin, settings_schema, settings, catch_up, catch_up_cap, allow_unasked,
created_at, updated_at, enabled_at, alive_at, last_run_at`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanScript(row rowScanner) (*Script, error) {
	var (
		s                                             Script
		kind, origin, catchUp, blocks, trigger        string
		perms, approved, schema, settings, allow      string
		created, updated, enabledAt, aliveAt, lastRun string
		enabled                                       int
	)
	err := row.Scan(&s.ID, &s.Name, &s.Description, &kind, &s.Source, &blocks, &trigger, &perms, &approved,
		&enabled, &origin, &schema, &settings, &catchUp, &s.CatchUpCap, &allow,
		&created, &updated, &enabledAt, &aliveAt, &lastRun)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scripting: read script: %w", err)
	}
	s.Kind, s.Origin, s.CatchUp, s.Enabled = Kind(kind), Origin(origin), CatchUpMode(catchUp), enabled != 0
	if blocks != "" {
		t, err := ParseTree([]byte(blocks))
		if err != nil {
			return nil, err
		}
		s.Blocks = &t
	}
	for _, part := range []struct {
		raw string
		dst any
	}{
		{trigger, &s.Trigger}, {perms, &s.Permissions}, {approved, &s.Approved},
		{schema, &s.SettingsSchema}, {settings, &s.Settings}, {allow, &s.AllowUnasked},
	} {
		if err := json.Unmarshal([]byte(part.raw), part.dst); err != nil {
			return nil, fmt.Errorf("scripting: read script %d: %w", s.ID, err)
		}
	}
	s.CreatedAt, s.UpdatedAt = parseTime(created), parseTime(updated)
	s.EnabledAt, s.AliveAt, s.LastRunAt = parseTime(enabledAt), parseTime(aliveAt), parseTime(lastRun)
	return &s, nil
}

func blocksJSON(t *Tree) string {
	if t == nil {
		return ""
	}
	return mustJSON(t)
}

// Create stores a new script and returns its id.
func (st *Store) Create(ctx context.Context, s *Script) (int64, error) {
	now := time.Now()
	res, err := st.db.ExecContext(ctx, `
INSERT INTO scripts (name, description, kind, source, blocks, trigger, permissions, approved,
  enabled, origin, settings_schema, settings, catch_up, catch_up_cap, allow_unasked,
  created_at, updated_at, enabled_at, alive_at, last_run_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '')`,
		s.Name, s.Description, string(s.Kind), s.Source, blocksJSON(s.Blocks), mustJSON(s.Trigger),
		mustJSON(nonNil(s.Permissions)), mustJSON(nonNil(s.Approved)), boolInt(s.Enabled), string(s.Origin),
		mustJSON(nonNil(s.SettingsSchema)), mustJSON(nonNilMap(s.Settings)), string(s.CatchUp), s.CatchUpCap,
		mustJSON(nonNil(s.AllowUnasked)), fmtTime(now), fmtTime(now), fmtTime(s.EnabledAt), fmtTime(s.AliveAt))
	if err != nil {
		return 0, fmt.Errorf("scripting: create script: %w", err)
	}
	return res.LastInsertId()
}

func nonNilMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Update writes every field of a stored script but its id, origin and
// creation time.
func (st *Store) Update(ctx context.Context, s *Script) error {
	res, err := st.db.ExecContext(ctx, `
UPDATE scripts SET name = ?, description = ?, kind = ?, source = ?, blocks = ?, trigger = ?,
  permissions = ?, approved = ?, enabled = ?, settings_schema = ?, settings = ?, catch_up = ?,
  catch_up_cap = ?, allow_unasked = ?, updated_at = ?, enabled_at = ?, alive_at = ?
WHERE id = ?`,
		s.Name, s.Description, string(s.Kind), s.Source, blocksJSON(s.Blocks), mustJSON(s.Trigger),
		mustJSON(nonNil(s.Permissions)), mustJSON(nonNil(s.Approved)), boolInt(s.Enabled),
		mustJSON(nonNil(s.SettingsSchema)), mustJSON(nonNilMap(s.Settings)), string(s.CatchUp),
		s.CatchUpCap, mustJSON(nonNil(s.AllowUnasked)), fmtTime(time.Now()), fmtTime(s.EnabledAt),
		fmtTime(s.AliveAt), s.ID)
	if err != nil {
		return fmt.Errorf("scripting: update script %d: %w", s.ID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Get returns one script.
func (st *Store) Get(ctx context.Context, id int64) (*Script, error) {
	return scanScript(st.db.QueryRowContext(ctx, `SELECT `+scriptColumns+` FROM scripts WHERE id = ?`, id))
}

// List returns every script by name.
func (st *Store) List(ctx context.Context) ([]*Script, error) {
	rows, err := st.db.QueryContext(ctx, `SELECT `+scriptColumns+` FROM scripts ORDER BY name COLLATE NOCASE, id`)
	if err != nil {
		return nil, fmt.Errorf("scripting: list scripts: %w", err)
	}
	defer rows.Close()
	var out []*Script
	for rows.Next() {
		s, err := scanScript(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Delete removes a script with its runs and storage.
func (st *Store) Delete(ctx context.Context, id int64) error {
	if _, err := st.db.ExecContext(ctx, `DELETE FROM scripts WHERE id = ?`, id); err != nil {
		return fmt.Errorf("scripting: delete script %d: %w", id, err)
	}
	return nil
}

// MarkAlive stamps every enabled script as running at t.
func (st *Store) MarkAlive(ctx context.Context, t time.Time) error {
	if _, err := st.db.ExecContext(ctx, `UPDATE scripts SET alive_at = ? WHERE enabled = 1`, fmtTime(t)); err != nil {
		return fmt.Errorf("scripting: mark scripts alive: %w", err)
	}
	return nil
}

// StartRun logs the start of a run and returns its id.
func (st *Store) StartRun(ctx context.Context, r *Run) (int64, error) {
	res, err := st.db.ExecContext(ctx, `
INSERT INTO script_runs (script_id, trigger, started_at, status, depth) VALUES (?, ?, ?, ?, ?)`,
		r.ScriptID, r.Trigger, fmtTime(r.Started), string(StatusRunning), r.Depth)
	if err != nil {
		return 0, fmt.Errorf("scripting: log run: %w", err)
	}
	return res.LastInsertId()
}

// FinishRun records how a run ended. A run that was never started (a
// skipped one) is inserted whole.
func (st *Store) FinishRun(ctx context.Context, r *Run) error {
	steps := mustJSON(nonNil(r.Steps))
	if r.ID == 0 {
		id, err := st.StartRun(ctx, r)
		if err != nil {
			return err
		}
		r.ID = id
	}
	_, err := st.db.ExecContext(ctx, `
UPDATE script_runs SET duration_ms = ?, status = ?, error = ?, steps = ?, changes = ? WHERE id = ?`,
		r.Duration.Milliseconds(), string(r.Status), r.Error, steps, r.Changes(), r.ID)
	if err != nil {
		return fmt.Errorf("scripting: log run %d: %w", r.ID, err)
	}
	if r.Status != StatusSkipped {
		if _, err := st.db.ExecContext(ctx, `UPDATE scripts SET last_run_at = ? WHERE id = ?`, fmtTime(r.Started), r.ScriptID); err != nil {
			return fmt.Errorf("scripting: stamp last run: %w", err)
		}
	}
	return nil
}

// Runs returns a script's latest runs, newest first; scriptID 0 lists the
// runs of every script.
func (st *Store) Runs(ctx context.Context, scriptID int64, limit int) ([]*Run, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	query := `SELECT id, script_id, trigger, started_at, duration_ms, status, error, steps, depth FROM script_runs`
	args := []any{}
	if scriptID != 0 {
		query += ` WHERE script_id = ?`
		args = append(args, scriptID)
	}
	query += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := st.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("scripting: list runs: %w", err)
	}
	defer rows.Close()
	var out []*Run
	for rows.Next() {
		var (
			r                      Run
			started, status, steps string
			ms                     int64
		)
		if err := rows.Scan(&r.ID, &r.ScriptID, &r.Trigger, &started, &ms, &status, &r.Error, &steps, &r.Depth); err != nil {
			return nil, fmt.Errorf("scripting: read run: %w", err)
		}
		r.Started, r.Duration, r.Status = parseTime(started), time.Duration(ms)*time.Millisecond, Status(status)
		if err := json.Unmarshal([]byte(steps), &r.Steps); err != nil {
			r.Steps = nil
		}
		out = append(out, &r)
	}
	return out, rows.Err()
}

// ClearRuns empties a script's log; scriptID 0 empties every log.
func (st *Store) ClearRuns(ctx context.Context, scriptID int64) error {
	query, args := `DELETE FROM script_runs`, []any{}
	if scriptID != 0 {
		query += ` WHERE script_id = ?`
		args = append(args, scriptID)
	}
	if _, err := st.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("scripting: clear runs: %w", err)
	}
	return nil
}

// PruneRuns drops runs started before cutoff, and all but the newest keep
// runs of each script.
func (st *Store) PruneRuns(ctx context.Context, cutoff time.Time, keep int) error {
	if _, err := st.db.ExecContext(ctx, `DELETE FROM script_runs WHERE started_at < ?`, fmtTime(cutoff)); err != nil {
		return fmt.Errorf("scripting: prune runs: %w", err)
	}
	_, err := st.db.ExecContext(ctx, `
DELETE FROM script_runs WHERE id IN (
  SELECT id FROM (
    SELECT id, ROW_NUMBER() OVER (PARTITION BY script_id ORDER BY id DESC) AS n FROM script_runs
  ) WHERE n > ?
)`, keep)
	if err != nil {
		return fmt.Errorf("scripting: prune runs: %w", err)
	}
	return nil
}

// KVGet reads one stored value.
func (st *Store) KVGet(ctx context.Context, scriptID int64, key string) (string, bool, error) {
	var v string
	err := st.db.QueryRowContext(ctx, `SELECT value FROM script_kv WHERE script_id = ? AND key = ?`, scriptID, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("scripting: read storage: %w", err)
	}
	return v, true, nil
}

// KVSet writes one value, refusing a new key past the per script cap.
func (st *Store) KVSet(ctx context.Context, scriptID int64, key, value string) error {
	var exists, count int
	if err := st.db.QueryRowContext(ctx, `
SELECT (SELECT COUNT(*) FROM script_kv WHERE script_id = ? AND key = ?),
       (SELECT COUNT(*) FROM script_kv WHERE script_id = ?)`, scriptID, key, scriptID).Scan(&exists, &count); err != nil {
		return fmt.Errorf("scripting: write storage: %w", err)
	}
	if exists == 0 && count >= maxKVKeys {
		return fmt.Errorf("scripting: a script stores at most %d keys", maxKVKeys)
	}
	_, err := st.db.ExecContext(ctx, `
INSERT INTO script_kv (script_id, key, value) VALUES (?, ?, ?)
ON CONFLICT (script_id, key) DO UPDATE SET value = excluded.value`, scriptID, key, value)
	if err != nil {
		return fmt.Errorf("scripting: write storage: %w", err)
	}
	return nil
}

// KVDelete removes one value.
func (st *Store) KVDelete(ctx context.Context, scriptID int64, key string) error {
	if _, err := st.db.ExecContext(ctx, `DELETE FROM script_kv WHERE script_id = ? AND key = ?`, scriptID, key); err != nil {
		return fmt.Errorf("scripting: delete storage: %w", err)
	}
	return nil
}

// KVKeys lists a script's keys in order.
func (st *Store) KVKeys(ctx context.Context, scriptID int64) ([]string, error) {
	rows, err := st.db.QueryContext(ctx, `SELECT key FROM script_kv WHERE script_id = ? ORDER BY key`, scriptID)
	if err != nil {
		return nil, fmt.Errorf("scripting: list storage: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}
