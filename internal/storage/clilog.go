package storage

import (
	"context"
	"fmt"
	"time"
)

// cliLogKept is how many requests the command line log keeps.
const cliLogKept = 1000

// CLIRequest is one request of the pelton command, for the log the user
// reads.
type CLIRequest struct {
	ID        int64
	Command   string
	Touched   string
	Result    string
	Switch    string
	CreatedAt time.Time
}

// RecordCLIRequest appends to the log and drops what falls past the newest
// cliLogKept.
func (d *DB) RecordCLIRequest(ctx context.Context, r CLIRequest) error {
	res, err := d.sql.ExecContext(ctx, `
INSERT INTO cli_log (command, touched, result, switch, created_at) VALUES (?, ?, ?, ?, ?)`,
		r.Command, r.Touched, r.Result, r.Switch, formatTime(nowOr(r.CreatedAt)))
	if err != nil {
		return fmt.Errorf("storage: record command line request: %w", err)
	}
	if id, err := res.LastInsertId(); err == nil && id > cliLogKept {
		if _, err := d.sql.ExecContext(ctx, `DELETE FROM cli_log WHERE id <= ?`, id-cliLogKept); err != nil {
			return fmt.Errorf("storage: trim command line log: %w", err)
		}
	}
	return nil
}

// ListCLIRequests returns the newest requests first.
func (d *DB) ListCLIRequests(ctx context.Context, limit int) ([]CLIRequest, error) {
	rows, err := d.sql.QueryContext(ctx, `
SELECT id, command, touched, result, switch, created_at FROM cli_log ORDER BY id DESC LIMIT ?`, normalizeLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("storage: list command line requests: %w", err)
	}
	defer rows.Close()
	var out []CLIRequest
	for rows.Next() {
		var (
			r       CLIRequest
			created string
		)
		if err := rows.Scan(&r.ID, &r.Command, &r.Touched, &r.Result, &r.Switch, &created); err != nil {
			return nil, fmt.Errorf("storage: scan command line request: %w", err)
		}
		r.CreatedAt, _ = parseTime(created)
		out = append(out, r)
	}
	return out, rows.Err()
}

// ClearCLIRequests empties the log.
func (d *DB) ClearCLIRequests(ctx context.Context) error {
	if _, err := d.sql.ExecContext(ctx, `DELETE FROM cli_log`); err != nil {
		return fmt.Errorf("storage: clear command line log: %w", err)
	}
	return nil
}
