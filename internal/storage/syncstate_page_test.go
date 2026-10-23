package storage

import (
	"context"
	"slices"
	"strconv"
	"testing"
)

// MessageStatesByRemoteID reads only the asked rows of the asked folder,
// across chunks, and FolderHasMessages answers without reading any.
func TestMessageStatesByRemoteID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, accountID, folder := newBodyTestFolder(t)
	other := Folder{AccountID: accountID, Name: "Other", IMAPPath: "Other", RemoteID: "Other"}
	if _, err := db.CreateFolder(ctx, &other); err != nil {
		t.Fatal(err)
	}
	if has, err := db.FolderHasMessages(ctx, folder.ID); err != nil || has {
		t.Fatalf("empty folder has messages = %v, %v", has, err)
	}
	for i := 1; i <= 700; i++ {
		if _, err := db.UpsertMessageListMeta(ctx, &Message{AccountID: accountID, FolderID: folder.ID,
			RemoteID: strconv.Itoa(i), Flags: FlagSeen}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.UpsertMessageListMeta(ctx, &Message{AccountID: accountID, FolderID: other.ID, RemoteID: "3"}); err != nil {
		t.Fatal(err)
	}
	if has, err := db.FolderHasMessages(ctx, folder.ID); err != nil || !has {
		t.Fatalf("filled folder has messages = %v, %v", has, err)
	}

	// 600 asked ids span two chunks; "x" has no row.
	var ask []string
	for i := 100; i < 700; i++ {
		ask = append(ask, strconv.Itoa(i))
	}
	ask = append(ask, "x")
	states, err := db.MessageStatesByRemoteID(ctx, folder.ID, ask)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range states {
		if s.Flags != FlagSeen || s.ID == 0 {
			t.Fatalf("state %+v", s)
		}
		got = append(got, s.RemoteID)
	}
	slices.Sort(got)
	want := slices.Clone(ask[:600])
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("got %d states, want the 600 asked rows with a row", len(got))
	}
	if states, err := db.MessageStatesByRemoteID(ctx, folder.ID, nil); err != nil || len(states) != 0 {
		t.Fatalf("no ids: %v, %v", states, err)
	}
}
