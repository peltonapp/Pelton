package desktop

import (
	"context"
	"log/slog"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/peltonapp/Pelton/internal/search"
	"github.com/peltonapp/Pelton/internal/storage"
	"github.com/peltonapp/Pelton/internal/storage/storagetest"
)

func newSearchTestApp(t *testing.T) (*App, *storage.DB, storage.Folder) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	db, err := storagetest.Open(filepath.Join(dir, "pelton.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.RunMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	accountID, err := db.CreateAccount(ctx, &storage.Account{Email: "a@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	folder := storage.Folder{AccountID: accountID, Name: "INBOX", IMAPPath: "INBOX", RemoteID: "INBOX"}
	if _, err := db.CreateFolder(ctx, &folder); err != nil {
		t.Fatal(err)
	}
	idx, err := openSearchIndex(dir)
	if err != nil {
		t.Fatal(err)
	}
	a := &App{ctx: ctx, store: db, log: slog.New(slog.DiscardHandler), dataDir: dir, index: idx}
	t.Cleanup(func() { a.index.Close() })
	return a, db, folder
}

func searchIDs(t *testing.T, a *App, text string) []int64 {
	t.Helper()
	res, err := a.index.Search(search.Query{Text: text, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, h := range res.Hits {
		ids = append(ids, h.ID)
	}
	slices.Sort(ids)
	return ids
}

// A stub is searchable by its headers as soon as it is listed, and by its body
// once the body lands, even after the watermark has moved past it, in any
// fill order, and after a failed index write.
func TestSearchIndexStubThenBody(t *testing.T) {
	ctx := context.Background()
	a, db, folder := newSearchTestApp(t)
	stub := func(uid uint32, subject string) int64 {
		id, err := db.UpsertMessageListMeta(ctx, &storage.Message{AccountID: folder.AccountID, FolderID: folder.ID,
			UID: uid, RemoteID: "", Subject: subject, FromAddress: "x@example.com"})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	fill := func(uid uint32, subject, body string) {
		if _, err := db.FillMessageBody(ctx, storage.BodyTarget{}, &storage.Message{AccountID: folder.AccountID, FolderID: folder.ID,
			UID: uid, Subject: subject, FromAddress: "x@example.com", BodyPlain: body}, nil); err != nil {
			t.Fatal(err)
		}
	}
	older := stub(100, "kwartalny raport")
	newer := stub(101, "faktura lipiec")
	if err := a.indexNewMessages(); err != nil {
		t.Fatal(err)
	}
	if got := searchIDs(t, a, "kwartalny"); !slices.Equal(got, []int64{older}) {
		t.Fatalf("stub by subject: %v, want [%d]", got, older)
	}

	// Out of order: the newer stub is filled first.
	fill(101, "faktura lipiec", "zebra pasiasta")
	if err := a.indexNewMessages(); err != nil {
		t.Fatal(err)
	}
	if got := searchIDs(t, a, "zebra"); !slices.Equal(got, []int64{newer}) {
		t.Fatalf("filled newer by body: %v, want [%d]", got, newer)
	}

	// The older one's index write fails, then the next pass heals it.
	fill(100, "kwartalny raport", "żyrafa cętkowana")
	if err := a.index.Close(); err != nil {
		t.Fatal(err)
	}
	if err := a.indexNewMessages(); err == nil {
		t.Fatal("index write on a closed index succeeded")
	}
	idx, err := openSearchIndex(a.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	a.index = idx
	if got := searchIDs(t, a, "żyrafa"); len(got) != 0 {
		t.Fatalf("body indexed despite the failure: %v", got)
	}
	if err := a.indexNewMessages(); err != nil {
		t.Fatal(err)
	}
	if got := searchIDs(t, a, "żyrafa"); !slices.Equal(got, []int64{older}) {
		t.Fatalf("filled older by body after a failed write: %v, want [%d]", got, older)
	}
	if got := searchIDs(t, a, "zebra"); !slices.Equal(got, []int64{newer}) {
		t.Fatalf("newer lost: %v", got)
	}
}

// A rebuild indexes stubs and bodies again from scratch.
func TestSearchIndexRebuildResetsState(t *testing.T) {
	ctx := context.Background()
	a, db, folder := newSearchTestApp(t)
	if _, err := db.InsertMessage(ctx, &storage.Message{AccountID: folder.AccountID, FolderID: folder.ID,
		UID: 5, Subject: "s", BodyPlain: "pelikan", BodyComplete: true}); err != nil {
		t.Fatal(err)
	}
	if err := a.indexNewMessages(); err != nil {
		t.Fatal(err)
	}
	if err := a.rebuildSearchIndex(); err != nil {
		t.Fatal(err)
	}
	if got := searchIDs(t, a, "pelikan"); len(got) != 1 {
		t.Fatalf("after rebuild: %v", got)
	}
}

// A charset repair that lands while an index pass holds the old, mangled
// text must not let the pass mark the row indexed, whatever state the pass
// read it at; otherwise only the explicit reindex after the repair heals it,
// and a run that exits first keeps the mangled text in the index for good.
// The next incremental pass must pick the repaired text up from the data.
func TestSearchIndexRepairDuringPassIsIndexedAgain(t *testing.T) {
	for _, readAt := range []storage.SearchState{storage.SearchNotIndexed, storage.SearchHeaders, storage.SearchFull} {
		t.Run(strconv.Itoa(int(readAt)), func(t *testing.T) {
			ctx := context.Background()
			a, db, folder := newSearchTestApp(t)
			msg := storage.Message{AccountID: folder.AccountID, FolderID: folder.ID,
				UID: 9, Subject: "zepsuty", FromAddress: "x@example.com", BodyPlain: "tekst"}
			var id int64
			var err error
			switch readAt {
			case storage.SearchNotIndexed:
				msg.BodyComplete = true
				id, err = db.InsertMessage(ctx, &msg)
			case storage.SearchHeaders:
				if id, err = db.UpsertMessageListMeta(ctx, &storage.Message{AccountID: folder.AccountID,
					FolderID: folder.ID, UID: 9, Subject: "zepsuty", FromAddress: "x@example.com"}); err != nil {
					t.Fatal(err)
				}
				if err := a.indexNewMessages(); err != nil {
					t.Fatal(err)
				}
				_, err = db.FillMessageBody(ctx, storage.BodyTarget{}, &msg, nil)
			case storage.SearchFull:
				msg.BodyComplete = true
				if id, err = db.InsertMessage(ctx, &msg); err != nil {
					t.Fatal(err)
				}
				err = a.indexNewMessages()
			}
			if err != nil {
				t.Fatal(err)
			}

			// The pass reads the mangled row; the repair lands before it
			// records its result.
			old, err := db.GetMessage(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if old.SearchState != readAt || !old.BodyComplete {
				t.Fatalf("read state %d complete %v, want %d complete", old.SearchState, old.BodyComplete, readAt)
			}
			if err := db.RepairMessageText(ctx, id, "naprawiony", "tekst", "", "windows-1250"); err != nil {
				t.Fatal(err)
			}
			if err := a.indexBatch([]storage.Message{*old}); err != nil {
				t.Fatal(err)
			}
			now, err := db.GetMessage(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if now.SearchState == storage.SearchFull {
				t.Fatal("the pass holding the mangled text recorded the repaired row as fully indexed")
			}

			// The explicit reindex never runs (the app exited); the next
			// incremental pass, past the watermark, heals the row.
			if err := db.Set(ctx, settingSearchWatermark, strconv.FormatInt(id, 10)); err != nil {
				t.Fatal(err)
			}
			if err := a.indexNewMessages(); err != nil {
				t.Fatal(err)
			}
			if got := searchIDs(t, a, "naprawiony"); !slices.Equal(got, []int64{id}) {
				t.Errorf("repaired text: %v, want [%d]", got, id)
			}
			if got := searchIDs(t, a, "zepsuty"); len(got) != 0 {
				t.Errorf("mangled text still found: %v", got)
			}
			if now, err = db.GetMessage(ctx, id); err != nil || now.SearchState != storage.SearchFull {
				t.Errorf("state after the next pass = %v, %v, want full", now.SearchState, err)
			}
		})
	}
}
