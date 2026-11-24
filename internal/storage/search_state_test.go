package storage

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// A charset repair that lands while an index pass has the row in hand must
// keep the pass from recording its result, whatever state the row was read
// at, or the index keeps the mangled text for good. A row read at 0 or 1 is
// the hard case: the stale mark leaves its state as it was. The next body
// pass must pick the repaired row up; an untouched row is recorded.
func TestRecordSearchStateKeepsStaleMarkFromRepair(t *testing.T) {
	t.Parallel()
	for _, readAt := range []SearchState{SearchNotIndexed, SearchHeaders, SearchFull} {
		t.Run(fmt.Sprint("read at ", int(readAt)), func(t *testing.T) {
			ctx := context.Background()
			db, accountID, folder := newBodyTestFolder(t)
			var ids []int64
			for _, remote := range []string{"1", "2"} {
				id, err := db.InsertMessage(ctx, &Message{AccountID: accountID, FolderID: folder.ID, RemoteID: remote, BodyComplete: true, Subject: "mangled"})
				if err != nil {
					t.Fatal(err)
				}
				ids = append(ids, id)
			}
			if _, err := db.sql.ExecContext(ctx, `UPDATE messages SET search_state = ?`, int(readAt)); err != nil {
				t.Fatal(err)
			}

			// The pass reads both rows.
			var read []IndexRead
			for _, id := range ids {
				m, err := db.GetMessage(ctx, id)
				if err != nil {
					t.Fatal(err)
				}
				if m.SearchState != readAt {
					t.Fatalf("message %d read with state %d, want %d", id, m.SearchState, readAt)
				}
				read = append(read, m.IndexRead())
			}
			// The repair lands before the pass records its result.
			if err := db.RepairMessageText(ctx, ids[0], "repaired", "", "", "windows-1250"); err != nil {
				t.Fatal(err)
			}
			if err := db.RecordSearchState(ctx, read, SearchFull); err != nil {
				t.Fatal(err)
			}

			for i, want := range []SearchState{min(readAt, SearchHeaders), SearchFull} {
				m, err := db.GetMessage(ctx, ids[i])
				if err != nil {
					t.Fatal(err)
				}
				if m.SearchState != want {
					t.Errorf("message %d state = %d, want %d", ids[i], m.SearchState, want)
				}
			}
			pending, err := db.ListMessagesForReindex(ctx, 0, ids[1], 10)
			if err != nil {
				t.Fatal(err)
			}
			if len(pending) != 1 || pending[0].ID != ids[0] || pending[0].Subject != "repaired" {
				t.Fatalf("pending body index = %+v, want only the repaired message", pending)
			}
			// The next pass reads the repaired text and records it.
			if err := db.RecordSearchState(ctx, []IndexRead{pending[0].IndexRead()}, SearchFull); err != nil {
				t.Fatal(err)
			}
			if pending, err := db.ListMessagesForReindex(ctx, 0, ids[1], 10); err != nil || len(pending) != 0 {
				t.Fatalf("pending body index after the next pass = %+v, %v, want none", pending, err)
			}
		})
	}
}

// Every write that changes indexed text bumps text_rev; a list refresh with
// the same columns does not.
func TestTextRevBumpedByTextRewrites(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, accountID, folder := newBodyTestFolder(t)
	rev := func(id int64) int64 {
		t.Helper()
		m, err := db.GetMessage(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return m.TextRev
	}
	stub := &Message{AccountID: accountID, FolderID: folder.ID, RemoteID: "7", Subject: "s", FromAddress: "x@example.com"}
	id, err := db.UpsertMessageListMeta(ctx, stub)
	if err != nil {
		t.Fatal(err)
	}
	want := rev(id)
	step := func(name string, bump bool, write func() error) {
		t.Helper()
		if err := write(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if bump {
			want++
		}
		if got := rev(id); got != want {
			t.Fatalf("%s: text_rev = %d, want %d", name, got, want)
		}
	}
	step("same list refresh", false, func() error {
		_, err := db.UpsertMessageListMeta(ctx, &Message{AccountID: accountID, FolderID: folder.ID, RemoteID: "7", Subject: "s", FromAddress: "x@example.com"})
		return err
	})
	step("changed list refresh", true, func() error {
		_, err := db.UpsertMessageListMeta(ctx, &Message{AccountID: accountID, FolderID: folder.ID, RemoteID: "7", Subject: "s2", FromAddress: "x@example.com"})
		return err
	})
	step("body fill", true, func() error {
		_, err := db.FillMessageBody(ctx, BodyTarget{}, &Message{AccountID: accountID, FolderID: folder.ID, RemoteID: "7", Subject: "s2", BodyPlain: "body"}, nil)
		return err
	})
	step("charset repair", true, func() error {
		return db.RepairMessageText(ctx, id, "s3", "body", "", "windows-1250")
	})
	step("flag change", false, func() error {
		return db.SetFlagColor(ctx, id, 2)
	})
}

// A list refresh that changes indexed metadata marks the row stale for the
// reindex pass: a complete row drops from full to headers, a stub from
// headers to not indexed, and both are listed for reindex behind the
// watermark. A refresh that changes nothing indexed, though the row's date
// was stored by an older build in another offset, marks nothing.
func TestListMetaRefreshMarksSearchStale(t *testing.T) {
	t.Parallel()
	date := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	cases := []struct {
		name     string
		complete bool
		state    SearchState
		subject  string
		want     SearchState
		bump     bool
	}{
		{"stub changed", false, SearchHeaders, "nowy", SearchNotIndexed, true},
		{"complete changed", true, SearchFull, "nowy", SearchHeaders, true},
		{"complete headers changed", true, SearchHeaders, "nowy", SearchHeaders, true},
		{"stub unchanged", false, SearchHeaders, "stary", SearchHeaders, false},
		{"complete unchanged", true, SearchFull, "stary", SearchFull, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			db, accountID, folder := newBodyTestFolder(t)
			id, err := db.InsertMessage(ctx, &Message{AccountID: accountID, FolderID: folder.ID, RemoteID: "5",
				Subject: "stary", FromName: "Ala", FromAddress: "x@example.com", Date: date,
				BodyPlain: "treść"})
			if err != nil {
				t.Fatal(err)
			}
			// The date is the same instant as an older build may have
			// stored it.
			if _, err := db.sql.ExecContext(ctx, `UPDATE messages SET body_complete = ?, search_state = ?, date = ? WHERE id = ?`,
				boolToInt(tc.complete), int(tc.state), "2026-03-04T06:06:07+01:00", id); err != nil {
				t.Fatal(err)
			}
			before, err := db.GetMessage(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.UpsertMessageListMeta(ctx, &Message{AccountID: accountID, FolderID: folder.ID, RemoteID: "5",
				Subject: tc.subject, FromName: "Ala", FromAddress: "x@example.com", Date: date, BodyPlain: "treść"}); err != nil {
				t.Fatal(err)
			}
			after, err := db.GetMessage(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if after.SearchState != tc.want {
				t.Errorf("search state = %d, want %d", after.SearchState, tc.want)
			}
			if bumped := after.TextRev != before.TextRev; bumped != tc.bump {
				t.Errorf("text_rev %d -> %d, want bumped %v", before.TextRev, after.TextRev, tc.bump)
			}
			pending, err := db.ListMessagesForReindex(ctx, 0, id, 10)
			if err != nil {
				t.Fatal(err)
			}
			wantPending := tc.want != SearchFull && (tc.complete || tc.want == SearchNotIndexed)
			if (len(pending) == 1) != wantPending || len(pending) > 1 {
				t.Errorf("listed for reindex: %d rows, want %v", len(pending), wantPending)
			}
		})
	}
}

// The reindex query reaches each arm through its partial index, not a scan
// of the messages table.
func TestListMessagesForReindexUsesIndexes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, _, _ := newBodyTestFolder(t)
	rows, err := db.sql.QueryContext(ctx, `EXPLAIN QUERY PLAN SELECT id FROM messages WHERE id IN (`+reindexIDs+`) ORDER BY id LIMIT ?3`, 0, 10, 10)
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
	joined := strings.Join(plan, "\n")
	for _, idx := range []string{"idx_messages_search_pending", "idx_messages_search_stale_stub"} {
		if !strings.Contains(joined, idx) {
			t.Errorf("plan does not use %s:\n%s", idx, joined)
		}
	}
}
