package storage

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestUnifiedPageMatchesNaive(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	acc, _ := db.EnsureLocalAccount(ctx)
	var ids []int64
	base := time.Now().Add(-100 * time.Hour)
	uid := uint32(0)
	for _, name := range []string{"INBOX", "Sent", "Archive"} {
		f, _ := db.EnsureLocalFolder(ctx, acc.ID, name)
		ids = append(ids, f.ID)
		for range 40 {
			uid++
			m := &Message{AccountID: acc.ID, FolderID: f.ID, UID: uid,
				MessageID: fmt.Sprintf("<%d@x>", uid), Subject: fmt.Sprintf("s%d", uid),
				Date: base.Add(time.Duration(uid) * time.Minute)}
			if uid%4 == 0 {
				m.Flags = FlagFlagged
			}
			if _, err := db.InsertMessage(ctx, m); err != nil {
				t.Fatal(err)
			}
		}
	}

	for _, q := range []MessageQuery{
		{FolderIDs: ids, Limit: 50},
		{FolderIDs: ids, Limit: 50, Offset: 50},
		{FolderIDs: ids, Limit: 10, Offset: 95},
		{FolderIDs: ids, Limit: 0},
		{FolderIDs: ids, Limit: 20, RequireFlags: FlagFlagged},
		{FolderIDs: ids[:1], Limit: 20, Offset: 10},
	} {
		got, err := db.QueryMessages(ctx, q)
		if err != nil {
			t.Fatalf("%v: %v", q, err)
		}
		where, args := messageWhere(q)
		rows, err := db.sql.QueryContext(ctx, selectMessageColumns+`
FROM messages WHERE `+where+` ORDER BY date DESC, uid DESC LIMIT ? OFFSET ?`,
			append(args, normalizeLimit(q.Limit), q.Offset)...)
		if err != nil {
			t.Fatal(err)
		}
		var want []Message
		for rows.Next() {
			m, err := scanMessage(rows)
			if err != nil {
				t.Fatal(err)
			}
			want = append(want, *m)
		}
		rows.Close()
		if len(got) != len(want) {
			t.Fatalf("%+v: got %d rows, want %d", q, len(got), len(want))
		}
		for i := range got {
			if got[i].ID != want[i].ID {
				t.Fatalf("%+v: row %d = id %d, want id %d", q, i, got[i].ID, want[i].ID)
			}
		}
	}
}

// select-all reads ids straight from the query rather than paging through the
// list, so the set it returns has to be exactly what the pages would have held.
func TestQueryMessageIDsMatchesTheFullPage(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	acc, _ := db.EnsureLocalAccount(ctx)
	var folders []int64
	base := time.Now().Add(-100 * time.Hour)
	uid := uint32(0)
	for _, name := range []string{"INBOX", "Sent"} {
		f, _ := db.EnsureLocalFolder(ctx, acc.ID, name)
		folders = append(folders, f.ID)
		for range 30 {
			uid++
			m := &Message{AccountID: acc.ID, FolderID: f.ID, UID: uid,
				MessageID: fmt.Sprintf("<%d@x>", uid), Subject: fmt.Sprintf("s%d", uid),
				Date: base.Add(time.Duration(uid) * time.Minute)}
			if uid%3 == 0 {
				m.Flags = FlagFlagged
			}
			if _, err := db.InsertMessage(ctx, m); err != nil {
				t.Fatal(err)
			}
		}
	}

	for _, q := range []MessageQuery{
		{FolderIDs: folders},
		{FolderIDs: folders[:1]},
		{FolderIDs: folders, RequireFlags: FlagFlagged},
	} {
		ids, err := db.QueryMessageIDs(ctx, q)
		if err != nil {
			t.Fatalf("%v: %v", q, err)
		}
		// the same query with no limit, read as full rows.
		full := q
		full.Limit = 0
		msgs, err := db.QueryMessages(ctx, full)
		if err != nil {
			t.Fatalf("%v: %v", q, err)
		}
		if len(ids) != len(msgs) {
			t.Fatalf("%v: got %d ids for %d messages", q, len(ids), len(msgs))
		}
		for i, m := range msgs {
			if ids[i] != m.ID {
				t.Fatalf("%v: id %d = %d, want %d (order must match the list)", q, i, ids[i], m.ID)
			}
		}
	}

	// a query with no folders selects nothing rather than everything.
	ids, err := db.QueryMessageIDs(ctx, MessageQuery{})
	if err != nil {
		t.Fatalf("empty query: %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("an empty query returned %d ids", len(ids))
	}
}
