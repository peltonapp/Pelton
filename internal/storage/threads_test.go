package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

type threadFixture struct {
	t       *testing.T
	db      *DB
	account int64
	inbox   int64
	sent    int64
	uid     uint32
	base    time.Time
}

func newThreadFixture(t *testing.T) *threadFixture {
	t.Helper()
	db := newTestDB(t)
	return newThreadFixtureOn(t, db, "me@example.com")
}

func newThreadFixtureOn(t *testing.T, db *DB, email string) *threadFixture {
	t.Helper()
	ctx := context.Background()
	account, err := db.CreateAccount(ctx, &Account{Email: email})
	if err != nil {
		t.Fatal(err)
	}
	f := &threadFixture{t: t, db: db, account: account, base: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)}
	f.inbox = f.folder("INBOX")
	f.sent = f.folder("Sent")
	return f
}

func (f *threadFixture) folder(name string) int64 {
	f.t.Helper()
	folder := Folder{AccountID: f.account, Name: name, IMAPPath: name}
	id, err := f.db.CreateFolder(context.Background(), &folder)
	if err != nil {
		f.t.Fatal(err)
	}
	return id
}

// add inserts a message hours after the fixture's base time and returns it.
func (f *threadFixture) add(m Message, hours int) Message {
	f.t.Helper()
	f.uid++
	m.AccountID = f.account
	if m.FolderID == 0 {
		m.FolderID = f.inbox
	}
	m.UID = f.uid
	m.Date = f.base.Add(time.Duration(hours) * time.Hour)
	if _, err := f.db.InsertMessage(context.Background(), &m); err != nil {
		f.t.Fatal(err)
	}
	return m
}

// thread re-reads a message's thread id, which a later merge may have changed.
func (f *threadFixture) thread(m Message) int64 {
	f.t.Helper()
	got, err := f.db.GetMessage(context.Background(), m.ID)
	if err != nil {
		f.t.Fatal(err)
	}
	if got.ThreadID == 0 {
		f.t.Fatalf("message %d has no thread", m.ID)
	}
	return got.ThreadID
}

func (f *threadFixture) sameThread(msgs ...Message) {
	f.t.Helper()
	want := f.thread(msgs[0])
	for _, m := range msgs[1:] {
		if got := f.thread(m); got != want {
			f.t.Fatalf("message %q in thread %d, want %d with %q", m.Subject, got, want, msgs[0].Subject)
		}
	}
}

func (f *threadFixture) apart(a, b Message) {
	f.t.Helper()
	if f.thread(a) == f.thread(b) {
		f.t.Fatalf("%q and %q share thread %d", a.Subject, b.Subject, f.thread(a))
	}
}

func TestThreadReplyChain(t *testing.T) {
	f := newThreadFixture(t)
	a := f.add(Message{MessageID: "a@x", Subject: "plan", FromAddress: "alice@x"}, 0)
	b := f.add(Message{MessageID: "b@x", Subject: "Re: plan", FromAddress: "bob@x",
		InReplyTo: []string{"a@x"}, References: []string{"a@x"}}, 1)
	c := f.add(Message{MessageID: "<c@x>", Subject: "Re: plan", FromAddress: "alice@x",
		InReplyTo: []string{"b@x"}, References: []string{"a@x", "b@x"}}, 2)
	other := f.add(Message{MessageID: "d@x", Subject: "plan", FromAddress: "alice@x"}, 3)
	f.sameThread(a, b, c)
	f.apart(a, other)
}

func TestThreadForkStaysOneConversation(t *testing.T) {
	f := newThreadFixture(t)
	root := f.add(Message{MessageID: "root@x", Subject: "q"}, 0)
	left := f.add(Message{MessageID: "l@x", Subject: "Re: q", InReplyTo: []string{"root@x"}}, 1)
	right := f.add(Message{MessageID: "r@x", Subject: "Re: q", InReplyTo: []string{"root@x"}}, 2)
	leaf := f.add(Message{MessageID: "leaf@x", Subject: "Re: q", InReplyTo: []string{"r@x"}, References: []string{"root@x", "r@x"}}, 3)
	f.sameThread(root, left, right, leaf)
}

func TestThreadMissingParentArrivingLater(t *testing.T) {
	f := newThreadFixture(t)
	// newest first, the order sync stores mail in.
	child := f.add(Message{MessageID: "c@x", Subject: "Re: trip", InReplyTo: []string{"p@x"}}, 5)
	other := f.add(Message{MessageID: "o@x", Subject: "Re: trip", InReplyTo: []string{"g@x"}}, 4)
	f.apart(child, other)

	// the parent replied to g@x, so it links the two threads.
	parent := f.add(Message{MessageID: "p@x", Subject: "Re: trip", InReplyTo: []string{"g@x"}, References: []string{"g@x"}}, 3)
	f.sameThread(child, other, parent)
	grand := f.add(Message{MessageID: "g@x", Subject: "trip"}, 1)
	f.sameThread(child, grand)

	var threads int
	if err := f.db.sql.QueryRow(`SELECT COUNT(*) FROM threads WHERE account_id = ?`, f.account).Scan(&threads); err != nil {
		t.Fatal(err)
	}
	if threads != 1 {
		t.Fatalf("%d threads left after the merge, want 1", threads)
	}
}

func TestThreadSubjectFallback(t *testing.T) {
	f := newThreadFixture(t)
	orig := f.add(Message{MessageID: "o@x", Subject: "Lunch", FromAddress: "Alice <alice@x>", ToAddresses: "me@example.com"}, 0)
	reply := f.add(Message{MessageID: "r@x", Subject: "RE: Lunch", FromAddress: "me@example.com", ToAddresses: "Alice <alice@x>",
		InReplyTo: []string{}, References: []string{}}, 2)
	f.sameThread(orig, reply)

	stranger := f.add(Message{MessageID: "s@x", Subject: "Re: lunch", FromAddress: "bob@x", ToAddresses: "me@example.com"}, 3)
	f.apart(orig, stranger)

	late := f.add(Message{MessageID: "l@x", Subject: "Re: Lunch", FromAddress: "alice@x", ToAddresses: "me@example.com"}, 24*200)
	f.apart(orig, late)

	withHeaders := f.add(Message{MessageID: "h@x", Subject: "Re: Lunch", FromAddress: "alice@x", ToAddresses: "me@example.com",
		InReplyTo: []string{"elsewhere@x"}}, 4)
	f.apart(orig, withHeaders)
}

func TestThreadSubjectFallbackReplyStoredFirst(t *testing.T) {
	f := newThreadFixture(t)
	reply := f.add(Message{MessageID: "r@x", Subject: "Aw: [team] Budget", FromAddress: "carol@x", ToAddresses: "me@example.com"}, 5)
	orig := f.add(Message{MessageID: "o@x", Subject: "[team] Budget", FromAddress: "me@example.com", ToAddresses: "carol@x"}, 1)
	f.sameThread(orig, reply)
}

func TestThreadSpansFoldersNotAccounts(t *testing.T) {
	f := newThreadFixture(t)
	archive := f.folder("Archive")
	inbox := f.add(Message{MessageID: "a@x", Subject: "hi"}, 0)
	sent := f.add(Message{MessageID: "b@x", Subject: "Re: hi", FolderID: f.sent, InReplyTo: []string{"a@x"}}, 1)
	archived := f.add(Message{MessageID: "c@x", Subject: "Re: hi", FolderID: archive, InReplyTo: []string{"b@x"}}, 2)
	f.sameThread(inbox, sent, archived)

	g := newThreadFixtureOn(t, f.db, "other@example.com")
	copyElsewhere := g.add(Message{MessageID: "a@x", Subject: "hi"}, 0)
	replyElsewhere := g.add(Message{MessageID: "z@x", Subject: "Re: hi", InReplyTo: []string{"b@x"}}, 1)
	if g.thread(copyElsewhere) == f.thread(inbox) || g.thread(replyElsewhere) == f.thread(inbox) {
		t.Fatal("a thread crossed accounts")
	}
	g.sameThread(copyElsewhere)
}

// unthread puts the cache back to how a database migrated from before
// threading looks: every row unassigned and nothing recorded.
func unthread(t *testing.T, db *DB) {
	t.Helper()
	for _, q := range []string{
		`UPDATE messages SET thread_id = 0, thread_subject = '', thread_loose = 0`,
		`DELETE FROM thread_keys`,
		`DELETE FROM threads`,
	} {
		if _, err := db.sql.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
}

func runBackfill(t *testing.T, db *DB) int {
	t.Helper()
	total := 0
	for {
		n, err := db.BackfillThreads(context.Background(), 2)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			return total
		}
		total += n
	}
}

func TestBackfillThreadsIsResumableAndIdempotent(t *testing.T) {
	f := newThreadFixture(t)
	child := f.add(Message{MessageID: "c@x", Subject: "Re: x", InReplyTo: []string{"p@x"}}, 3)
	parent := f.add(Message{MessageID: "p@x", Subject: "x"}, 1)
	loose := f.add(Message{MessageID: "l@x", Subject: "Re: x", FromAddress: "dan@x"}, 4)
	alone := f.add(Message{MessageID: "a@x", Subject: "y"}, 2)
	unthread(t, f.db)

	// one batch, then a fresh start, as if the app quit part way through.
	if n, err := f.db.BackfillThreads(context.Background(), 1); err != nil || n != 1 {
		t.Fatalf("first batch = %d, %v", n, err)
	}
	if got := runBackfill(t, f.db); got != 3 {
		t.Fatalf("resumed backfill assigned %d, want the remaining 3", got)
	}
	f.sameThread(child, parent)
	f.apart(child, alone)
	f.apart(child, loose)

	before := map[int64]int64{}
	for _, m := range []Message{child, parent, loose, alone} {
		before[m.ID] = f.thread(m)
	}
	if got := runBackfill(t, f.db); got != 0 {
		t.Fatalf("second backfill assigned %d, want 0", got)
	}
	for _, m := range []Message{child, parent, loose, alone} {
		if f.thread(m) != before[m.ID] {
			t.Fatalf("message %d moved thread on a second backfill", m.ID)
		}
	}

	// mail arriving after the backfill still joins the backfilled thread.
	late := f.add(Message{MessageID: "z@x", Subject: "Re: x", InReplyTo: []string{"c@x"}}, 5)
	f.sameThread(parent, late)
}

func TestThreadMigrationLeavesLegacyRowsForBackfill(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.ensureMigrationsTable(ctx); err != nil {
		t.Fatal(err)
	}
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range migrations {
		if m.version < 43 {
			if err := db.applyMigration(ctx, m); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, query := range []string{
		`INSERT INTO accounts (id, email, imap_host, imap_port, created_at) VALUES (1, 'legacy@example.com', 'imap.example.com', 993, '')`,
		`INSERT INTO folders (id, account_id, name, imap_path) VALUES (1, 1, 'INBOX', 'INBOX')`,
		`INSERT INTO messages (id, account_id, folder_id, uid, message_id, subject, date) VALUES (1, 1, 1, 1, 'p@x', 'x', '2026-01-01T00:00:00Z')`,
		`INSERT INTO messages (id, account_id, folder_id, uid, message_id, subject, date, in_reply_to) VALUES (2, 1, 1, 2, 'c@x', 'Re: x', '2026-01-02T00:00:00Z', '["p@x"]')`,
	} {
		if _, err := db.sql.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := db.RunMigrations(ctx); err != nil {
			t.Fatalf("migration run %d: %v", i+1, err)
		}
	}
	var threadID int64
	if err := db.sql.QueryRow(`SELECT thread_id FROM messages WHERE id = 1`).Scan(&threadID); err != nil || threadID != 0 {
		t.Fatalf("legacy thread_id = %d, %v; want 0 until the backfill", threadID, err)
	}
	if got := runBackfill(t, db); got != 2 {
		t.Fatalf("backfill assigned %d, want 2", got)
	}
	a, _ := db.GetMessage(ctx, 1)
	b, _ := db.GetMessage(ctx, 2)
	if a.ThreadID == 0 || a.ThreadID != b.ThreadID {
		t.Fatalf("thread ids %d and %d, want one shared thread", a.ThreadID, b.ThreadID)
	}
}

func TestNormalizeSubject(t *testing.T) {
	tests := []struct {
		in        string
		want      string
		wantReply bool
	}{
		{"Hello  World", "hello world", false},
		{"Re: Hello", "hello", true},
		{"RE: re: Hello", "hello", true},
		{"Re[2]: Hello", "hello", true},
		{"Re^3: Hello", "hello", true},
		{"AW: WG: Hello", "hello", true},
		{"Fwd: Hello", "hello", false},
		{"[list] Re: Hello", "hello", true},
		{"Re: [list] Hello", "hello", true},
		{"[only tag]", "[only tag]", false},
		{"Recap: numbers", "recap: numbers", false},
		{"Ticket: 42", "ticket: 42", false},
		{"", "", false},
	}
	for _, tt := range tests {
		got, reply := normalizeSubject(tt.in)
		if got != tt.want || reply != tt.wantReply {
			t.Errorf("normalizeSubject(%q) = %q, %v; want %q, %v", tt.in, got, reply, tt.want, tt.wantReply)
		}
	}
}

func TestQueryThreads(t *testing.T) {
	ctx := context.Background()
	f := newThreadFixture(t)
	// thread one: two inbox messages, one unread, and the user's reply in Sent.
	a1 := f.add(Message{MessageID: "a1@x", Subject: "one", FromAddress: "Alice <alice@x>", Flags: FlagSeen}, 0)
	a2 := f.add(Message{MessageID: "a2@x", Subject: "Re: one", FromAddress: "Bob <bob@x>", InReplyTo: []string{"a1@x"}, HasAttachments: true}, 2)
	a3 := f.add(Message{MessageID: "a3@x", Subject: "Re: one", FromAddress: "me@example.com", FolderID: f.sent, InReplyTo: []string{"a2@x"}, Flags: FlagSeen}, 3)
	// thread two: a single newer message.
	b1 := f.add(Message{MessageID: "b1@x", Subject: "two", Flags: FlagSeen | FlagFlagged}, 5)
	// thread three: hidden entirely.
	c1 := f.add(Message{MessageID: "c1@x", Subject: "three"}, 6)
	// thread four: oldest, with a hidden member.
	d1 := f.add(Message{MessageID: "d1@x", Subject: "four", Flags: FlagSeen}, -5)
	d2 := f.add(Message{MessageID: "d2@x", Subject: "Re: four", InReplyTo: []string{"d1@x"}}, -4)
	if err := f.db.MarkDeletePending(ctx, c1.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.sql.Exec(`UPDATE messages SET snooze_hidden = 1 WHERE id = ?`, d2.ID); err != nil {
		t.Fatal(err)
	}

	q := ThreadQuery{Query: MessageQuery{FolderIDs: []int64{f.inbox}, Limit: 50}, MemberFolderIDs: []int64{f.sent}}
	pages, err := f.db.QueryThreads(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 3 {
		t.Fatalf("got %d threads, want 3", len(pages))
	}
	if pages[0].Head.ID != b1.ID || pages[1].Head.ID != a2.ID || pages[2].Head.ID != d1.ID {
		t.Fatalf("heads %d %d %d, want %d %d %d", pages[0].Head.ID, pages[1].Head.ID, pages[2].Head.ID, b1.ID, a2.ID, d1.ID)
	}
	one := pages[1]
	if len(one.Members) != 3 || one.Members[0].ID != a1.ID || one.Members[2].ID != a3.ID {
		t.Fatalf("thread one members = %+v, want a1, a2, a3 in date order", one.Members)
	}
	if len(pages[2].Members) != 1 {
		t.Fatalf("thread four has %d members, want the snoozed one left out", len(pages[2].Members))
	}
	if n, err := f.db.CountThreads(ctx, q); err != nil || n != 3 {
		t.Fatalf("CountThreads = %d, %v; want 3", n, err)
	}

	unread := q
	unread.Query.ExcludeFlags = FlagSeen
	pages, err = f.db.QueryThreads(ctx, unread)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 1 || pages[0].Head.ID != a2.ID {
		t.Fatalf("unread filter gave %d threads, want only thread one headed by a2", len(pages))
	}
}

func TestQueryThreadsPagingIsStable(t *testing.T) {
	ctx := context.Background()
	f := newThreadFixture(t)
	for i := 0; i < 7; i++ {
		// equal dates force the tie breaks to decide the order.
		f.add(Message{MessageID: string(rune('a'+i)) + "@x", Subject: "same time"}, 0)
	}
	unthreadedID := f.add(Message{MessageID: "u@x", Subject: "legacy"}, 0)
	if _, err := f.db.sql.Exec(`UPDATE messages SET thread_id = 0 WHERE id = ?`, unthreadedID.ID); err != nil {
		t.Fatal(err)
	}

	seen := map[int64]bool{}
	var order []int64
	for offset := 0; ; offset += 3 {
		pages, err := f.db.QueryThreads(ctx, ThreadQuery{Query: MessageQuery{FolderIDs: []int64{f.inbox}, Limit: 3, Offset: offset}})
		if err != nil {
			t.Fatal(err)
		}
		if len(pages) == 0 {
			break
		}
		for _, p := range pages {
			if seen[p.Head.ID] {
				t.Fatalf("head %d listed twice across pages", p.Head.ID)
			}
			seen[p.Head.ID] = true
			order = append(order, p.Head.ID)
			if p.ThreadID == 0 && (len(p.Members) != 1 || p.Members[0].ID != p.Head.ID) {
				t.Fatalf("unthreaded row has members %+v", p.Members)
			}
		}
	}
	if len(order) != 8 {
		t.Fatalf("paged through %d threads, want 8", len(order))
	}
	for i := 1; i < len(order); i++ {
		if order[i] > order[i-1] {
			t.Fatalf("order %v does not fall back to descending id", order)
		}
	}
}

func TestThreadMessagesListsEachCopyAndHonoursFolders(t *testing.T) {
	ctx := context.Background()
	f := newThreadFixture(t)
	a := f.add(Message{MessageID: "a@x", Subject: "s"}, 0)
	b := f.add(Message{MessageID: "b@x", Subject: "Re: s", FolderID: f.sent, InReplyTo: []string{"a@x"}}, 1)
	all, err := f.db.ThreadMessages(ctx, f.thread(a), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 || all[0].ID != a.ID || all[1].ID != b.ID {
		t.Fatalf("thread messages = %d rows, want a then b", len(all))
	}
	inboxOnly, err := f.db.ThreadMessages(ctx, f.thread(a), []int64{f.inbox})
	if err != nil {
		t.Fatal(err)
	}
	if len(inboxOnly) != 1 || inboxOnly[0].ID != a.ID {
		t.Fatalf("inbox-only thread messages = %d rows, want a", len(inboxOnly))
	}
}

func TestMarkReplySentFlagsEveryCopyOnce(t *testing.T) {
	ctx := context.Background()
	f := newThreadFixture(t)
	archive := f.folder("All Mail")
	orig := f.add(Message{MessageID: "<o@x>", Subject: "s"}, 0)
	copyOf := f.add(Message{MessageID: "<o@x>", Subject: "s", FolderID: archive}, 0)
	fwd := f.add(Message{MessageID: "f@x", Subject: "t"}, 1)

	if err := f.db.RecordOutgoingReply(ctx, f.account, "<reply@me>", "o@x", ReplyKindReply); err != nil {
		t.Fatal(err)
	}
	if err := f.db.RecordOutgoingReply(ctx, f.account, "fw@me", "<f@x>", ReplyKindForward); err != nil {
		t.Fatal(err)
	}
	if replies, _, _ := f.db.SentReplies(ctx, f.account, "o@x"); len(replies) != 0 {
		t.Fatal("an unsent reply is listed")
	}

	changed, err := f.db.MarkReplySent(ctx, f.account, "reply@me")
	if err != nil || len(changed) != 2 {
		t.Fatalf("MarkReplySent = %v, %v; want both copies", changed, err)
	}
	for _, m := range []Message{orig, copyOf} {
		got, _ := f.db.GetMessage(ctx, m.ID)
		if !got.Flags.Has(FlagAnswered) || got.Flags.Has(FlagForwarded) {
			t.Fatalf("copy %d flags = %d, want answered only", m.ID, got.Flags)
		}
	}
	if again, err := f.db.MarkReplySent(ctx, f.account, "reply@me"); err != nil || len(again) != 0 {
		t.Fatalf("second MarkReplySent = %v, %v; want no change", again, err)
	}
	if _, err := f.db.MarkReplySent(ctx, f.account, "<fw@me>"); err != nil {
		t.Fatal(err)
	}
	got, _ := f.db.GetMessage(ctx, fwd.ID)
	if !got.Flags.Has(FlagForwarded) {
		t.Fatalf("forwarded message flags = %d", got.Flags)
	}

	sentCopy := f.add(Message{MessageID: "reply@me", Subject: "Re: s", FolderID: f.sent, InReplyTo: []string{"o@x"}}, 2)
	replies, forwards, err := f.db.SentReplies(ctx, f.account, "<o@x>")
	if err != nil || len(replies) != 1 || replies[0] != sentCopy.ID || len(forwards) != 0 {
		t.Fatalf("SentReplies = %v %v %v, want the sent copy", replies, forwards, err)
	}
}

func TestPruneUnsentReplies(t *testing.T) {
	ctx := context.Background()
	f := newThreadFixture(t)
	if err := f.db.RecordOutgoingReply(ctx, f.account, "old@me", "o@x", ReplyKindReply); err != nil {
		t.Fatal(err)
	}
	if err := f.db.RecordOutgoingReply(ctx, f.account, "kept@me", "o@x", ReplyKindReply); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.MarkReplySent(ctx, f.account, "kept@me"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.sql.Exec(`UPDATE message_replies SET created_at = '2000-01-01T00:00:00Z'`); err != nil {
		t.Fatal(err)
	}
	n, err := f.db.PruneUnsentReplies(ctx, time.Hour)
	if err != nil || n != 1 {
		t.Fatalf("pruned %d, %v; want 1", n, err)
	}
	var left sql.NullString
	if err := f.db.sql.QueryRow(`SELECT reply_message_id FROM message_replies`).Scan(&left); err != nil || left.String != "kept@me" {
		t.Fatalf("left %v, %v; want the sent record", left, err)
	}
}
