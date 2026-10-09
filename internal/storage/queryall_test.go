package storage

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func TestQueryAll(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "q.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.sql.ExecContext(ctx, `CREATE TABLE t(n INTEGER); INSERT INTO t VALUES (1),(2),(3)`); err != nil {
		t.Fatal(err)
	}
	scanInt := func(r *sql.Rows) (int, error) {
		var n int
		err := r.Scan(&n)
		return n, err
	}
	got, err := db.queryAll[int](ctx, scanInt, `SELECT n FROM t WHERE n > ? ORDER BY n`, 1)
	if err != nil || len(got) != 2 || got[0] != 2 || got[1] != 3 {
		t.Fatalf("queryAll = %v, %v; want [2 3]", got, err)
	}
	none, err := db.queryAll[int](ctx, scanInt, `SELECT n FROM t WHERE n > 9`)
	if err != nil || none != nil {
		t.Fatalf("no rows = %v, %v; want nil, nil", none, err)
	}
	boom := errors.New("boom")
	if _, err := db.queryAll[int](ctx, func(*sql.Rows) (int, error) { return 0, boom }, `SELECT n FROM t`); !errors.Is(err, boom) {
		t.Fatalf("scan error = %v, want boom", err)
	}
}
