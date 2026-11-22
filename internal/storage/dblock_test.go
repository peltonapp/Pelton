package storage

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// The bug: a sidebar drag during a big sync failed with "database is locked".
// A transaction that began deferred only asked for the write lock at its first
// write, and if another connection had written since the transaction started,
// sqlite refused it outright rather than waiting. busy_timeout does not cover
// that case, so no amount of waiting would have helped.
//
// This is that shape: a transaction reads, another connection writes and
// commits, and only then does the first one write.
func TestTransactionThatReadsBeforeWritingIsNotRefused(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	if _, err := db.CreateAccount(ctx, &Account{Email: "first@example.com"}); err != nil {
		t.Fatalf("seed account: %v", err)
	}

	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback()

	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM accounts`).Scan(&count); err != nil {
		t.Fatalf("read inside the transaction: %v", err)
	}

	// the other connection, held off until the transaction has read, so the
	// ordering that used to fail is the one under test rather than a race.
	var wg sync.WaitGroup
	wg.Add(1)
	var otherErr error
	go func() {
		defer wg.Done()
		_, otherErr = db.CreateAccount(ctx, &Account{Email: "second@example.com"})
	}()

	// long enough for the other connection to be waiting on the write lock,
	// short enough that the test does not idle.
	time.Sleep(50 * time.Millisecond)

	if _, err := tx.ExecContext(ctx, `UPDATE accounts SET display_name = ? WHERE email = ?`,
		"written inside the transaction", "first@example.com"); err != nil {
		t.Fatalf("write after reading in the same transaction: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	wg.Wait()
	if otherErr != nil {
		t.Fatalf("the waiting connection failed instead of taking its turn: %v", otherErr)
	}
}

// Writers from several goroutines at once have to queue rather than fail: a
// sync, the ui and a background job all write, and none of them has a reason to
// know about the others.
func TestConcurrentWritersQueueRatherThanFail(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	const writers = 12
	errs := make(chan error, writers)
	var wg sync.WaitGroup
	for i := range writers {
		wg.Go(func() {
			_, err := db.CreateAccount(ctx, &Account{Email: string(rune('a'+i)) + "@example.com"})
			errs <- err
		})
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("a concurrent writer failed: %v", err)
		}
	}
}

// The pool has to stay bounded. Without a ceiling every concurrent caller opened
// a connection of its own, so a burst of work grew the pool to whatever the
// moment demanded and left every one of those connections queued for the same
// single write lock.
func TestPoolIsBounded(t *testing.T) {
	db := newTestDB(t)

	if got := db.sql.Stats().MaxOpenConnections; got != maxOpenConns {
		t.Errorf("MaxOpenConnections = %d, want %d", got, maxOpenConns)
	}
}

// busy_timeout and the immediate transaction lock are what turn contention into
// a wait. Both live in the dsn, where a missing one fails silently: everything
// still works until two writers meet.
func TestDataSourceNameSetsTheLockingParameters(t *testing.T) {
	dsn := dataSourceName("/tmp/pelton.db")

	for _, want := range []string{"journal_mode(WAL)", "foreign_keys(1)", "busy_timeout(", "_txlock=immediate"} {
		if !strings.Contains(dsn, want) {
			t.Errorf("dsn %q is missing %q", dsn, want)
		}
	}
}
