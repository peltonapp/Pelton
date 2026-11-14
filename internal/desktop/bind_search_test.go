package desktop

import (
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/search"
	"github.com/peltonapp/Pelton/internal/storage"
)

func TestSearchRequestKeeps(t *testing.T) {
	read := &storage.Message{AccountID: 1, FolderID: 10, Flags: storage.FlagSeen}
	unread := &storage.Message{AccountID: 1, FolderID: 10}
	flagged := &storage.Message{AccountID: 2, FolderID: 20, Flags: storage.FlagFlagged | storage.FlagSeen, HasAttachments: true}

	tests := []struct {
		name string
		req  SearchRequestDTO
		msg  *storage.Message
		want bool
	}{
		{"no filter keeps everything", SearchRequestDTO{}, read, true},
		{"unread drops a read message", SearchRequestDTO{UnreadOnly: true}, read, false},
		{"unread keeps an unread message", SearchRequestDTO{UnreadOnly: true}, unread, true},
		{"flagged drops an unflagged message", SearchRequestDTO{FlaggedOnly: true}, unread, false},
		{"flagged keeps a flagged message", SearchRequestDTO{FlaggedOnly: true}, flagged, true},
		{"attachment drops a bare message", SearchRequestDTO{HasAttachment: true}, read, false},
		{"account drops another account", SearchRequestDTO{AccountID: 2}, read, false},
		{"account keeps its own", SearchRequestDTO{AccountID: 2}, flagged, true},
		{"folder drops another folder", SearchRequestDTO{FolderID: 20}, unread, false},
		{"folder keeps its own", SearchRequestDTO{FolderID: 10}, unread, true},
		{"filters combine", SearchRequestDTO{FlaggedOnly: true, FolderID: 10}, flagged, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.req.keeps(tt.msg); got != tt.want {
				t.Errorf("keeps = %v, want %v", got, tt.want)
			}
		})
	}
}

// a request made only of stored-field filters is still a search, not the
// normal list: "in:Archive is:flagged" has to return the flagged archive.
func TestSearchQueryTreatsStoredFiltersAsConstraints(t *testing.T) {
	for _, req := range []SearchRequestDTO{
		{FlaggedOnly: true},
		{AccountID: 3},
		{FolderID: 4},
	} {
		if _, ok := req.searchQuery(10); !ok {
			t.Errorf("searchQuery(%+v) reported an empty request", req)
		}
	}
	if _, ok := (SearchRequestDTO{Query: "  "}).searchQuery(10); ok {
		t.Error("a blank request was treated as a search")
	}
}

func TestSearchFiltersByFolderAccountAndFlag(t *testing.T) {
	a := newTrashTestApp(t)
	idx, err := search.Open(filepath.Join(t.TempDir(), "search.bleve"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = idx.Close() })
	a.index = idx

	first := seedAccount(t, a)
	second, err := a.store.CreateAccount(a.ctx, &storage.Account{Email: "b@example.test"})
	if err != nil {
		t.Fatal(err)
	}
	inbox := seedFolder(t, a, first, "INBOX", "INBOX", nil, 0)
	archive := seedFolder(t, a, first, "Archive", "Archive", nil, 0)
	other := seedFolder(t, a, second, "INBOX", "INBOX", nil, 0)

	insert := func(folder storage.Folder, uid uint32, flags storage.Flag) int64 {
		t.Helper()
		id, err := a.store.InsertMessage(a.ctx, &storage.Message{
			AccountID: folder.AccountID, FolderID: folder.ID, UID: uid,
			Subject: "quarterly report", Date: time.Now().UTC(), Flags: flags,
		})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	inInbox := insert(inbox, 1, 0)
	inArchive := insert(archive, 2, storage.FlagFlagged)
	inOther := insert(other, 3, storage.FlagFlagged)
	if err := a.indexNewMessages(); err != nil {
		t.Fatal(err)
	}

	ids := func(req SearchRequestDTO) []int64 {
		t.Helper()
		req.Query = "quarterly"
		req.Limit = 50
		res, err := a.Search(req)
		if err != nil {
			t.Fatal(err)
		}
		out := make([]int64, 0, len(res.Messages))
		for _, m := range res.Messages {
			out = append(out, m.ID)
		}
		slices.Sort(out)
		return out
	}

	tests := []struct {
		name string
		req  SearchRequestDTO
		want []int64
	}{
		{"everything", SearchRequestDTO{}, []int64{inInbox, inArchive, inOther}},
		{"one folder", SearchRequestDTO{FolderID: archive.ID}, []int64{inArchive}},
		{"one account", SearchRequestDTO{AccountID: second}, []int64{inOther}},
		{"flagged", SearchRequestDTO{FlaggedOnly: true}, []int64{inArchive, inOther}},
		{"flagged in the first account", SearchRequestDTO{FlaggedOnly: true, AccountID: first}, []int64{inArchive}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ids(tt.req)
			want := slices.Clone(tt.want)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Errorf("Search ids = %v, want %v", got, want)
			}
		})
	}

	sel, err := a.SearchMessageIDs(SearchRequestDTO{Query: "quarterly", FolderID: inbox.ID})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(sel.IDs, []int64{inInbox}) {
		t.Errorf("SearchMessageIDs ids = %v, want [%d]", sel.IDs, inInbox)
	}
}
