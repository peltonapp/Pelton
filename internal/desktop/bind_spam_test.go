package desktop

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"

	"github.com/peltonapp/Pelton/internal/storage"
)

// spamTestAccount builds an account with INBOX, Work and a \Junk folder, plus
// one message in the folder named by home. withJunk false leaves the junk
// folder out.
func spamTestAccount(t *testing.T, db *storage.DB, ctx context.Context, home string, withJunk bool) (map[string]storage.Folder, int64) {
	t.Helper()
	accountID, err := db.CreateAccount(ctx, &storage.Account{Email: "me@example.com"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	folders := []*storage.Folder{
		{AccountID: accountID, Name: "INBOX", IMAPPath: "INBOX"},
		{AccountID: accountID, Name: "Work", IMAPPath: "Work"},
	}
	if withJunk {
		folders = append(folders, &storage.Folder{AccountID: accountID, Name: "Junk Mail", IMAPPath: "Junk Mail", Attributes: []string{`\Junk`}})
	}
	byName := make(map[string]storage.Folder)
	for _, f := range folders {
		if _, err := db.CreateFolder(ctx, f); err != nil {
			t.Fatalf("create folder: %v", err)
		}
		byName[f.Name] = *f
	}
	id, err := db.InsertMessage(ctx, &storage.Message{
		AccountID: accountID,
		FolderID:  byName[home].ID,
		UID:       1,
		MessageID: "<one@example.com>",
		Subject:   "hello",
		Date:      time.Now(),
	})
	if err != nil {
		t.Fatalf("insert message: %v", err)
	}
	return byName, id
}

// reinsert puts the message back into the cache in folder, as the next sync
// would after a move.
func reinsert(t *testing.T, db *storage.DB, ctx context.Context, folder storage.Folder) int64 {
	t.Helper()
	id, err := db.InsertMessage(ctx, &storage.Message{
		AccountID: folder.AccountID,
		FolderID:  folder.ID,
		UID:       9,
		MessageID: "<one@example.com>",
		Subject:   "hello",
		Date:      time.Now(),
	})
	if err != nil {
		t.Fatalf("reinsert: %v", err)
	}
	return id
}

func TestMarkSpamMovesTagsAndRemembersTheSource(t *testing.T) {
	a, db, ctx := moveTestApp(t)
	folders, id := spamTestAccount(t, db, ctx, "Work", true)
	client := &fakeIMAP{permanentFlags: []imap.Flag{imap.FlagSeen, imap.FlagWildcard}}
	useFake(t, a, folders["Work"].AccountID, client)

	undo, err := a.MarkSpam(id)
	if err != nil {
		t.Fatalf("MarkSpam: %v", err)
	}
	if len(client.moved) != 1 || client.moved[0].dest != "Junk Mail" {
		t.Fatalf("moved %+v, want one move to Junk Mail", client.moved)
	}
	if want := []string{"-$NotJunk", "+$Junk"}; !slices.Equal(client.flagOps, want) {
		t.Errorf("flag ops %v, want %v", client.flagOps, want)
	}
	if undo.OriginalFolderID != folders["Work"].ID || undo.DestFolderID != folders["Junk Mail"].ID {
		t.Errorf("undo %+v, want Work -> Junk Mail", undo)
	}
	origin, ok, err := db.SpamOrigin(ctx, folders["Work"].AccountID, "<one@example.com>")
	if err != nil || !ok || origin != folders["Work"].ID {
		t.Errorf("origin = %d, %v, %v; want Work", origin, ok, err)
	}
}

// A server that does not keep keywords still gets the move; sending a STORE it
// would drop (or refuse) gains nothing.
func TestMarkSpamSkipsKeywordsTheServerWouldNotKeep(t *testing.T) {
	a, db, ctx := moveTestApp(t)
	folders, id := spamTestAccount(t, db, ctx, "INBOX", true)
	client := &fakeIMAP{permanentFlags: []imap.Flag{imap.FlagSeen, imap.FlagDeleted}}
	useFake(t, a, folders["INBOX"].AccountID, client)

	if _, err := a.MarkSpam(id); err != nil {
		t.Fatalf("MarkSpam: %v", err)
	}
	if len(client.flagOps) != 0 {
		t.Errorf("flag ops %v, want none", client.flagOps)
	}
	if len(client.moved) != 1 {
		t.Errorf("moved %d messages, want 1", len(client.moved))
	}
}

func TestMarkSpamWithoutJunkFolderFails(t *testing.T) {
	a, db, ctx := moveTestApp(t)
	folders, id := spamTestAccount(t, db, ctx, "INBOX", false)
	client := &fakeIMAP{}
	useFake(t, a, folders["INBOX"].AccountID, client)

	if _, err := a.MarkSpam(id); err == nil {
		t.Fatal("MarkSpam returned no error for an account with no spam folder")
	}
	if len(client.moved) != 0 {
		t.Errorf("moved %+v", client.moved)
	}
	if _, err := db.GetMessage(ctx, id); err != nil {
		t.Errorf("the row was dropped: %v", err)
	}
}

func TestMarkNotSpamDestination(t *testing.T) {
	tests := []struct {
		name string
		// origin is the folder recorded by an earlier MarkSpam, "" for none.
		origin string
		// dropOrigin deletes the origin folder before the rescue.
		dropOrigin bool
		want       string
	}{
		{name: "back to where it was marked from", origin: "Work", want: "Work"},
		{name: "filtered by the server goes to the inbox", want: "INBOX"},
		{name: "origin folder gone goes to the inbox", origin: "Work", dropOrigin: true, want: "INBOX"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, db, ctx := moveTestApp(t)
			folders, id := spamTestAccount(t, db, ctx, "Junk Mail", true)
			accountID := folders["INBOX"].AccountID
			if tt.origin != "" {
				if err := db.RecordSpamOrigin(ctx, accountID, "<one@example.com>", folders[tt.origin].ID); err != nil {
					t.Fatalf("record: %v", err)
				}
			}
			if tt.dropOrigin {
				if err := db.DeleteFolder(ctx, folders[tt.origin].ID); err != nil {
					t.Fatalf("delete folder: %v", err)
				}
			}
			client := &fakeIMAP{permanentFlags: []imap.Flag{imap.FlagWildcard}}
			useFake(t, a, accountID, client)

			undo, err := a.MarkNotSpam(id)
			if err != nil {
				t.Fatalf("MarkNotSpam: %v", err)
			}
			if len(client.moved) != 1 || client.moved[0].dest != tt.want {
				t.Fatalf("moved %+v, want one move to %s", client.moved, tt.want)
			}
			if want := []string{"-$Junk", "+$NotJunk"}; !slices.Equal(client.flagOps, want) {
				t.Errorf("flag ops %v, want %v", client.flagOps, want)
			}
			if undo.DestFolderID != folders[tt.want].ID {
				t.Errorf("undo dest %d, want %s", undo.DestFolderID, tt.want)
			}
			if _, ok, _ := db.SpamOrigin(ctx, accountID, "<one@example.com>"); ok {
				t.Error("origin record kept after the rescue")
			}
		})
	}
}

// Undoing a spam mark has to look in the junk folder, not in Archive, and take
// the $Junk tag back off so the filter does not keep training on it.
func TestRestoreMessageUndoesASpamMark(t *testing.T) {
	a, db, ctx := moveTestApp(t)
	folders, id := spamTestAccount(t, db, ctx, "INBOX", true)
	client := &fakeIMAP{permanentFlags: []imap.Flag{imap.FlagWildcard}, searchResult: []imap.UID{9}}
	useFake(t, a, folders["INBOX"].AccountID, client)

	undo, err := a.MarkSpam(id)
	if err != nil {
		t.Fatalf("MarkSpam: %v", err)
	}
	reinsert(t, db, ctx, folders["Junk Mail"])
	client.selected, client.moved, client.flagOps = nil, nil, nil

	if err := a.RestoreMessage(undo.MessageID, undo.DestFolderID, undo.OriginalFolderID); err != nil {
		t.Fatalf("RestoreMessage: %v", err)
	}
	if !slices.Equal(client.selected, []string{"Junk Mail"}) {
		t.Errorf("selected %v, want [Junk Mail]", client.selected)
	}
	if len(client.moved) != 1 || client.moved[0].uid != 9 || client.moved[0].dest != "INBOX" {
		t.Errorf("moved %+v, want uid 9 to INBOX", client.moved)
	}
	if want := []string{"-$Junk"}; !slices.Equal(client.flagOps, want) {
		t.Errorf("flag ops %v, want %v", client.flagOps, want)
	}
	if _, ok, _ := db.SpamOrigin(ctx, folders["INBOX"].AccountID, "<one@example.com>"); ok {
		t.Error("origin record kept after the undo")
	}
}

// The bug: undo after "Move to..." searched Archive, so undoing a move to any
// other folder failed with "not found".
func TestRestoreMessageLooksInTheFolderTheMessageWentTo(t *testing.T) {
	a, db, ctx := moveTestApp(t)
	folders, _ := spamTestAccount(t, db, ctx, "Work", true)
	client := &fakeIMAP{searchResult: []imap.UID{4}}
	useFake(t, a, folders["Work"].AccountID, client)

	if err := a.RestoreMessage("<one@example.com>", folders["Work"].ID, folders["INBOX"].ID); err != nil {
		t.Fatalf("RestoreMessage: %v", err)
	}
	if !slices.Equal(client.selected, []string{"Work"}) {
		t.Errorf("selected %v, want [Work]", client.selected)
	}
	if len(client.flagOps) != 0 {
		t.Errorf("flag ops %v on a plain move, want none", client.flagOps)
	}
}
