package desktop

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/storage"
)

// statsAccount is a synced account with the folders the statistics tell apart.
type statsAccount struct {
	id                                   int64
	inbox, sent, archive, trash, allMail storage.Folder
	uid                                  uint32
}

func newStatsAccount(t *testing.T, db *storage.DB, ctx context.Context) *statsAccount {
	t.Helper()
	id, err := db.CreateAccount(ctx, &storage.Account{Email: "me@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	acc := &statsAccount{id: id}
	for _, f := range []struct {
		dst   *storage.Folder
		path  string
		attrs []string
	}{
		{&acc.inbox, "INBOX", nil},
		{&acc.sent, "Sent", nil},
		{&acc.archive, "Archive", nil},
		{&acc.trash, "Trash", nil},
		{&acc.allMail, "[Gmail]/All Mail", []string{`\All`}},
	} {
		folder := storage.Folder{AccountID: id, Name: f.path, IMAPPath: f.path, Attributes: f.attrs}
		folderID, err := db.CreateFolder(ctx, &folder)
		if err != nil {
			t.Fatal(err)
		}
		folder.ID = folderID
		*f.dst = folder
	}
	return acc
}

func (s *statsAccount) add(t *testing.T, db *storage.DB, ctx context.Context, m storage.Message) int64 {
	t.Helper()
	s.uid++
	m.AccountID = s.id
	if m.FolderID == 0 {
		m.FolderID = s.inbox.ID
	}
	m.UID = s.uid
	if m.MessageID == "" {
		m.MessageID = fmt.Sprintf("<n%d@test>", s.uid)
	}
	id, err := db.InsertMessage(ctx, &m)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestListNewslettersOnEmptyMailbox(t *testing.T) {
	a, _, _ := moveTestApp(t)
	got, err := a.ListNewsletters()
	if err != nil || len(got) != 0 {
		t.Fatalf("ListNewsletters = %v, %v", got, err)
	}
}

func TestListNewsletters(t *testing.T) {
	a, db, ctx := moveTestApp(t)
	acc := newStatsAccount(t, db, ctx)
	now := time.Now()
	unsub := "<mailto:off@news.example>, <https://news.example/u>"
	var newest int64
	for i := range 6 {
		id := acc.add(t, db, ctx, storage.Message{FromAddress: "News <news@example.com>", ListUnsubscribe: unsub,
			ListUnsubscribePost: true, Date: now.Add(-time.Duration(i*7+1) * 24 * time.Hour), SizeBytes: 10})
		if i == 0 {
			newest = id
		}
	}
	// one-click is offered only when the newest mail's signature vouches for
	// the unsubscribe headers (RFC 8058); without that it is a web page.
	if got, err := a.ListNewsletters(); err != nil || len(got) != 1 || got[0].UnsubscribeKind != unsubKindLink {
		t.Fatalf("unverified = %+v, %v; want the link kind", got, err)
	}
	if err := db.SetMessageTrust(ctx, storage.MessageTrust{MessageID: newest, AuthState: "trusted", DKIM: "pass",
		DKIMDomains: []string{"example.com"}, UnsubSigners: []string{"example.com"}}); err != nil {
		t.Fatal(err)
	}
	// mail in trash and in Gmail's All Mail is not counted again.
	acc.add(t, db, ctx, storage.Message{FolderID: acc.trash.ID, FromAddress: "news@example.com", ListUnsubscribe: unsub, Date: now})
	acc.add(t, db, ctx, storage.Message{FolderID: acc.allMail.ID, FromAddress: "news@example.com", ListUnsubscribe: unsub, Date: now})
	// the existing unsubscribe flow keyed its memory by the raw sender.
	a.markUnsubscribed("News <News@Example.com>")

	got, err := a.ListNewsletters()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d senders, want 1: %+v", len(got), got)
	}
	n := got[0]
	if n.Email != "news@example.com" || n.Name != "News" || n.Count != 6 || n.Bytes != 60 || n.Unread != 6 {
		t.Errorf("sender = %+v", n)
	}
	if n.UnsubscribeKind != unsubKindOneClick {
		t.Errorf("kind = %q, want one-click", n.UnsubscribeKind)
	}
	if n.UnsubscribedAt == "" {
		t.Error("the earlier unsubscribe was not recognised")
	}
	// six mails, the first 36 days ago: measured from that first one.
	if math.Abs(n.PerWeek-7.0/6) > 0.05 {
		t.Errorf("per week = %v, want about 1.17", n.PerWeek)
	}
}

func TestPerWeek(t *testing.T) {
	now := time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)
	day := 24 * time.Hour
	cases := []struct {
		recent int
		first  time.Time
		want   float64
	}{
		{0, now.Add(-400 * day), 0},
		{90, now.Add(-400 * day), 7},
		{2, now.Add(-2 * day), 2},
		{6, now.Add(-14 * day), 3},
	}
	for _, c := range cases {
		if got := perWeek(c.recent, c.first, now); math.Abs(got-c.want) > 0.001 {
			t.Errorf("perWeek(%d, %v) = %v, want %v", c.recent, c.first, got, c.want)
		}
	}
}

func TestNewsletterMessageIDs(t *testing.T) {
	a, db, ctx := moveTestApp(t)
	acc := newStatsAccount(t, db, ctx)
	now := time.Now()
	unsub := "<https://news.example/u>"
	old := acc.add(t, db, ctx, storage.Message{FromAddress: "news@example.com", ListUnsubscribe: unsub, Date: now.Add(-60 * 24 * time.Hour)})
	recent := acc.add(t, db, ctx, storage.Message{FromAddress: "news@example.com", ListUnsubscribe: unsub, Date: now.Add(-2 * 24 * time.Hour)})
	archived := acc.add(t, db, ctx, storage.Message{FolderID: acc.archive.ID, FromAddress: "news@example.com", ListUnsubscribe: unsub, Date: now.Add(-90 * 24 * time.Hour)})

	cases := []struct {
		name string
		sel  NewsletterSelectionDTO
		want []int64
	}{
		{"everything", NewsletterSelectionDTO{Emails: []string{"news@example.com"}}, []int64{recent, old, archived}},
		{"older than 30 days", NewsletterSelectionDTO{Emails: []string{"news@example.com"}, OlderThanDays: 30}, []int64{old, archived}},
		{"keep the latest", NewsletterSelectionDTO{Emails: []string{"news@example.com"}, KeepLatest: true}, []int64{old, archived}},
		{"to archive", NewsletterSelectionDTO{Emails: []string{"news@example.com"}, ForArchive: true}, []int64{recent, old}},
		{"nobody", NewsletterSelectionDTO{}, []int64{}},
	}
	for _, c := range cases {
		got, err := a.NewsletterMessageIDs(c.sel)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if fmt.Sprint(got) != fmt.Sprint(c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestDeleteMessagesAndUndo(t *testing.T) {
	a, db, ctx := moveTestApp(t)
	acc := newStatsAccount(t, db, ctx)
	one := acc.add(t, db, ctx, storage.Message{FromAddress: "a@example.com", Date: time.Now()})
	two := acc.add(t, db, ctx, storage.Message{FromAddress: "a@example.com", Date: time.Now()})

	res, err := a.DeleteMessages([]int64{one, 9999, two})
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(res.Done) != fmt.Sprint([]int64{one, two}) || res.Failed != 1 || res.FirstError == "" {
		t.Errorf("delete result = %+v", res)
	}
	n, err := db.CountMessages(ctx, storage.MessageQuery{FolderIDs: []int64{acc.inbox.ID}})
	if err != nil || n != 0 {
		t.Fatalf("visible after delete = %d, %v", n, err)
	}

	undo, err := a.UndoDeletes(res.Done)
	if err != nil || len(undo.Done) != 2 || undo.Failed != 0 {
		t.Fatalf("undo = %+v, %v", undo, err)
	}
	if n, _ := db.CountMessages(ctx, storage.MessageQuery{FolderIDs: []int64{acc.inbox.ID}}); n != 2 {
		t.Errorf("visible after undo = %d, want 2", n)
	}
}

// One session per account and one MOVE per source folder, however many
// messages: a login per message is what made archiving a newsletter's
// backlog take minutes.
func TestArchiveMessagesMovesPerFolder(t *testing.T) {
	a, db, ctx := moveTestApp(t)
	inbox, archive, first := moveTestAccount(t, db, ctx, false)
	second, err := db.InsertMessage(ctx, &storage.Message{AccountID: inbox.AccountID, FolderID: inbox.ID, UID: 2, MessageID: "<two@example.com>", Date: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	already, err := db.InsertMessage(ctx, &storage.Message{AccountID: inbox.AccountID, FolderID: archive.ID, UID: 3, MessageID: "<three@example.com>", Date: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeIMAP{}
	useFake(t, a, inbox.AccountID, client)

	res, err := a.ArchiveMessages([]int64{first, second, already})
	if err != nil {
		t.Fatal(err)
	}
	if client.moveCalls != 1 || len(client.moved) != 2 || client.moved[0].dest != archive.IMAPPath {
		t.Errorf("moves = %d calls, %+v", client.moveCalls, client.moved)
	}
	if len(res.Done) != 2 || len(res.Undo) != 2 || res.Failed != 1 {
		t.Errorf("result = %+v", res)
	}
	if res.Undo[0].OriginalFolderID != inbox.ID || res.Undo[0].DestFolderID != archive.ID || res.Undo[0].MessageID == "" {
		t.Errorf("undo = %+v", res.Undo[0])
	}
	if _, err := db.GetMessage(ctx, first); err == nil {
		t.Error("an archived row survived")
	}
	if _, err := db.GetMessage(ctx, already); err != nil {
		t.Errorf("the message already in Archive was dropped: %v", err)
	}
	if !client.loggedOut {
		t.Error("the imap session was left open")
	}
}

func TestArchiveMessagesKeepsRowsWhenTheServerRefuses(t *testing.T) {
	a, db, ctx := moveTestApp(t)
	inbox, _, first := moveTestAccount(t, db, ctx, false)
	useFake(t, a, inbox.AccountID, &fakeIMAP{failMove: errors.New("over quota")})

	res, err := a.ArchiveMessages([]int64{first})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed != 1 || len(res.Done) != 0 || res.FirstError == "" {
		t.Errorf("result = %+v", res)
	}
	if _, err := db.GetMessage(ctx, first); err != nil {
		t.Errorf("the row was dropped after a failed move: %v", err)
	}
}
