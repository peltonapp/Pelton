package storage

import (
	"context"
	"database/sql"
)

// queryAll runs query and scans every row with scan, in order. A query with
// no rows returns nil. The first query, scan or iteration error is returned
// as is, for the caller to wrap.
func (d *DB) queryAll[T any](ctx context.Context, scan func(*sql.Rows) (T, error), query string, args ...any) ([]T, error) {
	rows, err := d.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []T
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
