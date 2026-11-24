package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// ErrImportJobNotFound is returned when an import job id has no row.
var ErrImportJobNotFound = errors.New("storage: import job not found")

// The states an import job moves through.
const (
	ImportRunning   = "running"
	ImportPaused    = "paused"
	ImportCancelled = "cancelled"
	ImportDone      = "done"
	ImportFailed    = "failed"
)

// ImportJob is one guided import. Plan, Progress and Report are JSON owned
// by the importer; storage only keeps them.
type ImportJob struct {
	ID         int64
	SourceKind string
	SourcePath string
	Plan       string
	State      string
	Progress   string
	Report     string
	Error      string
	CreatedAt  string
	UpdatedAt  string
}

const importJobColumns = `id, source_kind, source_path, plan, state, progress, report, error, created_at, updated_at`

func scanImportJob(row rowScanner) (*ImportJob, error) {
	var j ImportJob
	if err := row.Scan(&j.ID, &j.SourceKind, &j.SourcePath, &j.Plan, &j.State, &j.Progress,
		&j.Report, &j.Error, &j.CreatedAt, &j.UpdatedAt); err != nil {
		return nil, err
	}
	return &j, nil
}

// CreateImportJob stores a new job in the running state and returns its id.
func (d *DB) CreateImportJob(ctx context.Context, j *ImportJob) (int64, error) {
	now := nowText()
	if j.State == "" {
		j.State = ImportRunning
	}
	res, err := d.sql.ExecContext(ctx, `
INSERT INTO import_jobs (source_kind, source_path, plan, state, progress, report, created_at, updated_at)
VALUES (?, ?, ?, ?, '{}', '{}', ?, ?)`, j.SourceKind, j.SourcePath, j.Plan, j.State, now, now)
	if err != nil {
		return 0, fmt.Errorf("storage: insert import job: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("storage: import job id: %w", err)
	}
	j.ID, j.CreatedAt, j.UpdatedAt, j.Progress, j.Report = id, now, now, "{}", "{}"
	return id, nil
}

// GetImportJob returns one job, or ErrImportJobNotFound.
func (d *DB) GetImportJob(ctx context.Context, id int64) (*ImportJob, error) {
	j, err := scanImportJob(d.sql.QueryRowContext(ctx, `SELECT `+importJobColumns+` FROM import_jobs WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrImportJobNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: get import job %d: %w", id, err)
	}
	return j, nil
}

// ListImportJobs returns the most recent jobs first, at most limit of them.
func (d *DB) ListImportJobs(ctx context.Context, limit int) ([]ImportJob, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT `+importJobColumns+` FROM import_jobs ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("storage: list import jobs: %w", err)
	}
	defer rows.Close()
	var out []ImportJob
	for rows.Next() {
		j, err := scanImportJob(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan import job: %w", err)
		}
		out = append(out, *j)
	}
	return out, rows.Err()
}

// SaveImportJobState records a job's state, progress, report and error.
func (d *DB) SaveImportJobState(ctx context.Context, id int64, state, progress, report, failure string) error {
	res, err := d.sql.ExecContext(ctx, `
UPDATE import_jobs SET state = ?, progress = ?, report = ?, error = ?, updated_at = ? WHERE id = ?`,
		state, progress, report, failure, nowText(), id)
	if err != nil {
		return fmt.Errorf("storage: save import job %d: %w", id, err)
	}
	return requireOneRow(res, ErrImportJobNotFound)
}

// InterruptImportJobs marks every job still running as paused. It is called
// at start, when a job that was running belongs to a run of the app that is
// gone, and returns how many it changed.
func (d *DB) InterruptImportJobs(ctx context.Context) (int, error) {
	res, err := d.sql.ExecContext(ctx, `UPDATE import_jobs SET state = ?, updated_at = ? WHERE state = ?`,
		ImportPaused, nowText(), ImportRunning)
	if err != nil {
		return 0, fmt.Errorf("storage: interrupt import jobs: %w", err)
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// ImportPosition is how far a job got through one part of its source.
type ImportPosition struct {
	Position int64
	Done     bool
}

// ImportPositions returns every recorded part of a job, by part key.
func (d *DB) ImportPositions(ctx context.Context, jobID int64) (map[string]ImportPosition, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT part, position, done FROM import_positions WHERE job_id = ?`, jobID)
	if err != nil {
		return nil, fmt.Errorf("storage: import positions of job %d: %w", jobID, err)
	}
	defer rows.Close()
	out := map[string]ImportPosition{}
	for rows.Next() {
		var (
			part string
			p    ImportPosition
			done int
		)
		if err := rows.Scan(&part, &p.Position, &done); err != nil {
			return nil, fmt.Errorf("storage: scan import position: %w", err)
		}
		p.Done = done != 0
		out[part] = p
	}
	return out, rows.Err()
}

// SetImportPosition records how far a job got through one part.
func (d *DB) SetImportPosition(ctx context.Context, jobID int64, part string, p ImportPosition) error {
	_, err := d.sql.ExecContext(ctx, `
INSERT INTO import_positions (job_id, part, position, done) VALUES (?, ?, ?, ?)
ON CONFLICT(job_id, part) DO UPDATE SET position = excluded.position, done = excluded.done`,
		jobID, part, p.Position, boolToInt(p.Done))
	if err != nil {
		return fmt.Errorf("storage: set import position %q: %w", part, err)
	}
	return nil
}

// ImportDelivered reports whether a message with this fingerprint was already
// delivered to the destination.
func (d *DB) ImportDelivered(ctx context.Context, destination, fingerprint string) (bool, error) {
	var one int
	err := d.sql.QueryRowContext(ctx, `SELECT 1 FROM import_ledger WHERE destination = ? AND fingerprint = ?`,
		destination, fingerprint).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("storage: look up import ledger: %w", err)
	}
	return true, nil
}

// RecordImportDelivery notes that a message reached a destination. messageID
// is the local row it became, or 0 when it went to a server.
func (d *DB) RecordImportDelivery(ctx context.Context, destination, fingerprint string, messageID int64) error {
	var row any
	if messageID != 0 {
		row = messageID
	}
	_, err := d.sql.ExecContext(ctx, `
INSERT INTO import_ledger (destination, fingerprint, message_id, created_at) VALUES (?, ?, ?, ?)
ON CONFLICT(destination, fingerprint) DO NOTHING`, destination, fingerprint, row, nowText())
	if err != nil {
		return fmt.Errorf("storage: record import delivery: %w", err)
	}
	return nil
}

// EnsureLocalFolderPath returns the local folder at the end of segments,
// creating it and every missing parent as a nested folder joined with "/".
// A "/" inside one segment is replaced, since it would read as another level.
func (d *DB) EnsureLocalFolderPath(ctx context.Context, accountID int64, segments []string) (Folder, error) {
	var (
		parent *int64
		path   string
		folder Folder
	)
	if len(segments) == 0 {
		return Folder{}, errors.New("storage: empty local folder path")
	}
	for _, seg := range segments {
		seg = strings.TrimSpace(strings.ReplaceAll(seg, "/", "∕"))
		if seg == "" {
			seg = "_"
		}
		if path == "" {
			path = seg
		} else {
			path += "/" + seg
		}
		const query = `SELECT ` + folderColumns + folderFrom + ` WHERE f.account_id = ? AND f.imap_path = ?`
		f, err := scanFolder(d.sql.QueryRowContext(ctx, query, d.layoutProfile(), accountID, path))
		switch {
		case err == nil:
			folder = *f
		case errors.Is(err, sql.ErrNoRows):
			folder = Folder{AccountID: accountID, Name: seg, IMAPPath: path, Delimiter: "/", ParentID: parent}
			if _, err := d.CreateFolder(ctx, &folder); err != nil {
				return Folder{}, err
			}
		default:
			return Folder{}, fmt.Errorf("storage: look up local folder %q: %w", path, err)
		}
		id := folder.ID
		parent = &id
	}
	return folder, nil
}
