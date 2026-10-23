package desktop

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/storage"
)

// A list refresh that changes a message's subject must reach the search
// index, for a complete row and for a stub whose body stays outside the sync
// window, whether the refresh lands after an index pass recorded the row or
// while a pass holds the old text. The watermark is already past the row, so
// only the reindex pass can heal it: search finds the new subject, not the
// old one.
func TestSearchIndexListRefreshReindexes(t *testing.T) {
	for _, complete := range []bool{false, true} {
		for _, when := range []string{"before pass", "after pass", "during pass"} {
			name := "stub/" + when
			if complete {
				name = "complete/" + when
			}
			t.Run(name, func(t *testing.T) {
				ctx := context.Background()
				a, db, folder := newSearchTestApp(t)
				date := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
				meta := func(subject string) *storage.Message {
					return &storage.Message{AccountID: folder.AccountID, FolderID: folder.ID, UID: 7,
						Subject: subject, FromAddress: "x@example.com", Date: date}
				}
				var id int64
				var err error
				if complete {
					id, err = db.InsertMessage(ctx, &storage.Message{AccountID: folder.AccountID, FolderID: folder.ID,
						UID: 7, Subject: "pomarańczowy", FromAddress: "x@example.com", Date: date,
						BodyPlain: "treść", BodyComplete: true})
				} else {
					id, err = db.UpsertMessageListMeta(ctx, meta("pomarańczowy"))
				}
				if err != nil {
					t.Fatal(err)
				}
				// A later row keeps the watermark past the refreshed one.
				if _, err := db.UpsertMessageListMeta(ctx, &storage.Message{AccountID: folder.AccountID,
					FolderID: folder.ID, UID: 8, Subject: "inny", FromAddress: "y@example.com", Date: date}); err != nil {
					t.Fatal(err)
				}

				refresh := func() {
					t.Helper()
					if _, err := db.UpsertMessageListMeta(ctx, meta("fioletowy")); err != nil {
						t.Fatal(err)
					}
				}
				switch when {
				case "before pass":
					refresh()
					if err := a.indexNewMessages(); err != nil {
						t.Fatal(err)
					}
				case "after pass":
					if err := a.indexNewMessages(); err != nil {
						t.Fatal(err)
					}
					refresh()
				case "during pass":
					if err := a.indexNewMessages(); err != nil {
						t.Fatal(err)
					}
					// A pass reads the row, the refresh lands, then the pass
					// writes the old text and records its read.
					old, err := db.GetMessage(ctx, id)
					if err != nil {
						t.Fatal(err)
					}
					refresh()
					if err := a.indexBatch([]storage.Message{*old}); err != nil {
						t.Fatal(err)
					}
				}
				if err := a.indexNewMessages(); err != nil {
					t.Fatal(err)
				}

				if got := searchIDs(t, a, "fioletowy"); !slices.Equal(got, []int64{id}) {
					t.Errorf("new subject hits = %v, want [%d]", got, id)
				}
				if got := searchIDs(t, a, "pomarańczowy"); len(got) != 0 {
					t.Errorf("old subject hits = %v, want none", got)
				}
				m, err := db.GetMessage(ctx, id)
				if err != nil {
					t.Fatal(err)
				}
				if m.BodyComplete != complete {
					t.Fatalf("body complete = %v, want %v", m.BodyComplete, complete)
				}
				want := storage.SearchHeaders
				if complete {
					want = storage.SearchFull
				}
				if m.SearchState != want {
					t.Errorf("search state = %d, want %d", m.SearchState, want)
				}
				// Healed: nothing left to reindex.
				if left, err := db.ListMessagesForReindex(ctx, 0, id+1, 10); err != nil || len(left) != 0 {
					t.Errorf("left to reindex = %d rows, %v; want none", len(left), err)
				}
			})
		}
	}
}

// A refresh that changes nothing the index holds, with the date in another
// zone and with sub-second noise, leaves the index state and text revision
// alone, so a list does not churn the index.
func TestSearchIndexUnchangedRefreshDoesNotReindex(t *testing.T) {
	ctx := context.Background()
	a, db, folder := newSearchTestApp(t)
	date := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	stubID, err := db.UpsertMessageListMeta(ctx, &storage.Message{AccountID: folder.AccountID, FolderID: folder.ID,
		UID: 7, Subject: "s", FromName: "Ala", FromAddress: "x@example.com", ToAddresses: "y@example.com",
		Date: date, BodyPlain: "podgląd"})
	if err != nil {
		t.Fatal(err)
	}
	fullID, err := db.InsertMessage(ctx, &storage.Message{AccountID: folder.AccountID, FolderID: folder.ID,
		UID: 8, Subject: "t", FromName: "Ala", FromAddress: "x@example.com", ToAddresses: "y@example.com",
		Date: date, BodyPlain: "treść", BodyComplete: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.indexNewMessages(); err != nil {
		t.Fatal(err)
	}
	before := map[int64]*storage.Message{}
	for _, id := range []int64{stubID, fullID} {
		if before[id], err = db.GetMessage(ctx, id); err != nil {
			t.Fatal(err)
		}
	}

	warsaw := time.FixedZone("CET", 3600)
	for uid, subject := range map[uint32]string{7: "s", 8: "t"} {
		if _, err := db.UpsertMessageListMeta(ctx, &storage.Message{AccountID: folder.AccountID, FolderID: folder.ID,
			UID: uid, Subject: subject, FromName: "Ala", FromAddress: "x@example.com", ToAddresses: "y@example.com",
			Date: date.In(warsaw).Add(300 * time.Millisecond), BodyPlain: "podgląd", Flags: storage.FlagSeen}); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []int64{stubID, fullID} {
		m, err := db.GetMessage(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if m.SearchState != before[id].SearchState || m.TextRev != before[id].TextRev {
			t.Errorf("message %d: state %d rev %d, want %d rev %d", id, m.SearchState, m.TextRev,
				before[id].SearchState, before[id].TextRev)
		}
	}
	if left, err := db.ListMessagesForReindex(ctx, 0, fullID, 10); err != nil || len(left) != 0 {
		t.Errorf("left to reindex = %d rows, %v; want none", len(left), err)
	}
}
