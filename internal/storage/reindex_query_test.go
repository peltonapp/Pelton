package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"modernc.org/sqlite"
)

// reindexIDsUnbounded is the candidate query ListMessagesForReindex used
// before each arm stopped at the limit: an IN over both arms, limited only
// outside, which collects every eligible id up to the watermark per batch.
// Kept as the reference the bounded query must match row for row.
const reindexIDsUnbounded = `
  SELECT id FROM messages WHERE body_complete = 1 AND search_state < 2 AND id > ?1 AND id <= ?2
  UNION ALL
  SELECT id FROM messages WHERE body_complete = 0 AND search_state = 0 AND id > ?1 AND id <= ?2`

const (
	reindexQueryBounded   = `SELECT id FROM messages WHERE id IN (` + reindexIDs + `) ORDER BY id ASC`
	reindexQueryUnbounded = `SELECT id FROM messages WHERE id IN (` + reindexIDsUnbounded + `) ORDER BY id ASC LIMIT ?3`
)

// reindexProbeVisits counts the index entries the reindex arms read: the
// probe sits in each arm's WHERE, so SQLite calls it once per row the arm
// walks, before any limit outside the arm applies.
var (
	reindexProbeOnce   sync.Once
	reindexProbeVisits atomic.Int64
)

func registerReindexProbe(t *testing.T) {
	t.Helper()
	reindexProbeOnce.Do(func() {
		sqlite.MustRegisterScalarFunction("reindex_probe", 1, func(*sqlite.FunctionContext, []driver.Value) (driver.Value, error) {
			reindexProbeVisits.Add(1)
			return int64(1), nil
		})
	})
}

// withReindexProbe adds the probe to both arms of a reindex candidate query.
func withReindexProbe(t *testing.T, query string) string {
	t.Helper()
	const bound = "id <= ?2"
	if n := strings.Count(query, bound); n != 2 {
		t.Fatalf("reindex query has %d arms bounded by %q, want 2", n, bound)
	}
	return strings.ReplaceAll(query, bound, bound+" AND reindex_probe(id)")
}

// newReindexBacklog stores n messages mixing complete rows and stubs in
// search states 0, 1 and 2, the two flags varying independently, and returns
// their ids in order.
func newReindexBacklog(t *testing.T, n int) (*DB, []int64) {
	t.Helper()
	registerReindexProbe(t)
	ctx := context.Background()
	db, accountID, folder := newBodyTestFolder(t)
	ids := make([]int64, 0, n)
	for i := range n {
		id, err := db.InsertMessage(ctx, &Message{AccountID: accountID, FolderID: folder.ID, RemoteID: fmt.Sprint(i + 1), BodyComplete: true, Subject: "s"})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if _, err := db.sql.ExecContext(ctx, `UPDATE messages SET body_complete = (id % 5 < 3), search_state = (id * 7 / 3) % 3`); err != nil {
		t.Fatal(err)
	}
	return db, ids
}

// queryIDStmts holds the statements queryIDs prepared, per test, store and
// query. The walks run a query hundreds of times, and preparing it again each
// time was most of their cost (far more under -race).
var queryIDStmts sync.Map

type queryIDStmtKey struct {
	t     *testing.T
	db    *DB
	query string
}

func queryIDs(t *testing.T, db *DB, query string, afterID, upToID int64, limit int) []int64 {
	t.Helper()
	key := queryIDStmtKey{t, db, query}
	var stmt *sql.Stmt
	if v, ok := queryIDStmts.Load(key); ok {
		stmt = v.(*sql.Stmt)
	} else {
		var err error
		if stmt, err = db.sql.PrepareContext(context.Background(), query); err != nil {
			t.Fatal(err)
		}
		queryIDStmts.Store(key, stmt)
		t.Cleanup(func() {
			queryIDStmts.Delete(key)
			_ = stmt.Close()
		})
	}
	rows, err := stmt.QueryContext(context.Background(), afterID, upToID, normalizeLimit(limit))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return ids
}

// The bounded query lists the same rows in the same order as the unbounded
// one, batch by batch, over complete rows and stubs in every search state,
// for any batch size and watermark, and with no limit at all.
func TestListMessagesForReindexMatchesUnboundedQuery(t *testing.T) {
	ctx := context.Background()
	db, ids := newReindexBacklog(t, 240)
	var stats [2][3]int
	rows, err := db.sql.QueryContext(ctx, `SELECT body_complete, search_state FROM messages`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var complete, state int
		if err := rows.Scan(&complete, &state); err != nil {
			t.Fatal(err)
		}
		stats[complete][state]++
	}
	rows.Close()
	for complete := range 2 {
		for state := range 3 {
			if stats[complete][state] == 0 {
				t.Fatalf("dataset has no row with body_complete %d, search_state %d: %v", complete, state, stats)
			}
		}
	}

	for _, upTo := range []int64{ids[0] - 1, ids[0], ids[len(ids)/2], ids[len(ids)-1], ids[len(ids)-1] + 10} {
		for _, limit := range []int{1, 2, 7, 50, 500, 0} {
			t.Run(fmt.Sprintf("up to %d limit %d", upTo, limit), func(t *testing.T) {
				want := queryIDs(t, db, reindexQueryUnbounded, 0, upTo, -1)
				var got, gotStorage []int64
				for afterID := int64(0); ; {
					batch := queryIDs(t, db, reindexQueryBounded, afterID, upTo, limit)
					old := queryIDs(t, db, reindexQueryUnbounded, afterID, upTo, limit)
					if !slices.Equal(batch, old) {
						t.Fatalf("batch after %d = %v, unbounded query gives %v", afterID, batch, old)
					}
					messages, err := db.ListMessagesForReindex(ctx, afterID, upTo, limit)
					if err != nil {
						t.Fatal(err)
					}
					for _, m := range messages {
						gotStorage = append(gotStorage, m.ID)
					}
					if len(batch) == 0 {
						break
					}
					got = append(got, batch...)
					afterID = batch[len(batch)-1]
				}
				if !slices.Equal(got, want) {
					t.Fatalf("paged ids = %v, want %v", got, want)
				}
				if !slices.Equal(gotStorage, want) {
					t.Fatalf("ListMessagesForReindex paged ids = %v, want %v", gotStorage, want)
				}
			})
		}
	}
}

// A batch walks at most one limit of entries per partial index, however big
// the backlog behind the watermark: the unbounded query read every eligible
// id for each batch. Counted with a probe SQLite calls per row an arm reads,
// so the measure does not depend on timing.
func TestListMessagesForReindexWalksOneBatch(t *testing.T) {
	const limit = 10
	db, ids := newReindexBacklog(t, 600)
	upTo := ids[len(ids)-1]
	backlog := len(queryIDs(t, db, reindexQueryUnbounded, 0, upTo, -1))
	if backlog < 20*limit {
		t.Fatalf("backlog = %d rows, want well over one batch of %d", backlog, limit)
	}

	visits := func(query string, afterID int64) (int64, int) {
		t.Helper()
		reindexProbeVisits.Store(0)
		rows, err := db.sql.QueryContext(context.Background(), withReindexProbe(t, query), afterID, upTo, limit)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		n := 0
		for rows.Next() {
			n++
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return reindexProbeVisits.Load(), n
	}

	for _, afterID := range []int64{0, ids[len(ids)/3]} {
		n, got := visits(listForReindexQuery, afterID)
		if got != limit {
			t.Fatalf("batch after %d = %d rows, want %d", afterID, got, limit)
		}
		if n > 2*limit {
			t.Errorf("batch after %d read %d index entries, want at most %d (backlog %d)", afterID, n, 2*limit, backlog)
		}
		// The probe sees the old query's full walk, so it measures what the
		// bound saves.
		if old, _ := visits(reindexQueryUnbounded, afterID); old <= 2*limit {
			t.Errorf("unbounded query read %d index entries after %d; the probe does not count the backlog", old, afterID)
		}
	}

	// The probe leaves the plan as it is: each arm still walks its index.
	for _, query := range []string{listForReindexQuery, withReindexProbe(t, listForReindexQuery)} {
		plan := reindexPlan(t, db, query)
		for _, idx := range []string{"idx_messages_search_pending", "idx_messages_search_stale_stub"} {
			if !strings.Contains(plan, "SEARCH messages USING INDEX "+idx) && !strings.Contains(plan, "SEARCH messages USING COVERING INDEX "+idx) {
				t.Errorf("plan does not search %s:\n%s", idx, plan)
			}
		}
		if strings.Contains(plan, "SCAN messages") {
			t.Errorf("plan scans the messages table:\n%s", plan)
		}
	}
}

func reindexPlan(t *testing.T, db *DB, query string) string {
	t.Helper()
	rows, err := db.sql.QueryContext(context.Background(), `EXPLAIN QUERY PLAN `+query, 0, 10, 10)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return strings.Join(plan, "\n")
}
