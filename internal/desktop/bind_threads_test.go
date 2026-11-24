package desktop

import (
	"bytes"
	"database/sql"
	"net/mail"
	"slices"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/outbox"
	"github.com/peltonapp/Pelton/internal/storage"
)

type threadTestMailbox struct {
	app     *App
	db      *storage.DB
	account int64
	inbox   *storage.Folder
	sent    *storage.Folder
	trash   *storage.Folder
	uid     uint32
}

func newThreadTestMailbox(t *testing.T) *threadTestMailbox {
	t.Helper()
	app, db, accountID := pgpTestApp(t, nil, nil)
	return &threadTestMailbox{
		app:     app,
		db:      db,
		account: accountID,
		inbox:   addFolder(t, app, accountID, "INBOX"),
		sent:    addFolder(t, app, accountID, "Sent", `\Sent`),
		trash:   addFolder(t, app, accountID, "Trash", `\Trash`),
	}
}

func (b *threadTestMailbox) add(t *testing.T, folder *storage.Folder, m storage.Message, hours int) int64 {
	t.Helper()
	b.uid++
	m.AccountID = b.account
	m.FolderID = folder.ID
	m.UID = b.uid
	m.Date = time.Date(2026, 4, 1, 8, 0, 0, 0, time.UTC).Add(time.Duration(hours) * time.Hour)
	id, err := b.db.InsertMessage(b.app.ctx, &m)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	return id
}

func TestListThreadsGroupsAConversation(t *testing.T) {
	b := newThreadTestMailbox(t)
	first := b.add(t, b.inbox, storage.Message{MessageID: "a@x", Subject: "plans", FromAddress: "Alice <alice@x>", Flags: storage.FlagSeen}, 0)
	reply := b.add(t, b.sent, storage.Message{MessageID: "b@x", Subject: "Re: plans", FromAddress: "me@example.com", InReplyTo: []string{"a@x"}, Flags: storage.FlagSeen}, 1)
	last := b.add(t, b.inbox, storage.Message{MessageID: "c@x", Subject: "Re: plans", FromAddress: "Bob <bob@x>", InReplyTo: []string{"b@x"}, HasAttachments: true}, 2)
	b.add(t, b.trash, storage.Message{MessageID: "d@x", Subject: "Re: plans", FromAddress: "Carol <carol@x>", InReplyTo: []string{"c@x"}}, 3)
	other := b.add(t, b.inbox, storage.Message{MessageID: "e@x", Subject: "lunch", FromAddress: "dan@x", Flags: storage.FlagSeen | storage.FlagAnswered}, -1)

	for _, req := range []ListMessagesRequest{
		{Kind: "folder", FolderID: b.inbox.ID, Limit: 50},
		{Kind: "view", View: viewInbox, Limit: 50},
	} {
		list, err := b.app.ListThreads(req)
		if err != nil {
			t.Fatalf("ListThreads(%+v): %v", req, err)
		}
		if list.Total != 2 || len(list.Threads) != 2 {
			t.Fatalf("%s: total %d, %d threads; want 2", req.Kind, list.Total, len(list.Threads))
		}
		conv := list.Threads[0]
		if conv.Head.ID != last || conv.Count != 3 || conv.UnreadCount != 1 || !conv.HasAttachments || conv.Flagged {
			t.Fatalf("%s: conversation = %+v", req.Kind, conv)
		}
		if !slices.Equal(conv.MessageIDs, []int64{first, reply, last}) {
			t.Fatalf("%s: member ids %v, want %v without the trashed reply", req.Kind, conv.MessageIDs, []int64{first, reply, last})
		}
		if !slices.Equal(conv.Participants, []string{"Bob", "me@example.com", "Alice"}) {
			t.Fatalf("%s: participants %v", req.Kind, conv.Participants)
		}
		if conv.ThreadID == 0 || conv.ThreadID != conv.Head.ThreadID {
			t.Fatalf("%s: thread id %d, head thread %d", req.Kind, conv.ThreadID, conv.Head.ThreadID)
		}
		if single := list.Threads[1]; single.Head.ID != other || single.Count != 1 || !single.Answered || !single.Head.Answered {
			t.Fatalf("%s: single thread = %+v", req.Kind, single)
		}
	}

	paged, err := b.app.ListThreads(ListMessagesRequest{Kind: "folder", FolderID: b.inbox.ID, Limit: 1, Offset: 1})
	if err != nil || len(paged.Threads) != 1 || paged.Threads[0].Head.ID != other || paged.Total != 2 {
		t.Fatalf("second page = %+v, %v", paged, err)
	}

	trash, err := b.app.ListThreads(ListMessagesRequest{Kind: "folder", FolderID: b.trash.ID, Limit: 50})
	if err != nil || len(trash.Threads) != 1 || trash.Threads[0].Count != 4 {
		t.Fatalf("trash listing = %+v, %v; want the conversation with its trashed member", trash, err)
	}

	flat, err := b.app.ListMessages(ListMessagesRequest{Kind: "folder", FolderID: b.inbox.ID, Limit: 50})
	if err != nil || len(flat.Messages) != 3 {
		t.Fatalf("flat list = %+v, %v; want every inbox message", flat, err)
	}
	if flat.Messages[0].ThreadID == 0 || flat.Messages[0].ThreadID != flat.Messages[1].ThreadID || flat.Messages[2].ThreadID == flat.Messages[0].ThreadID {
		t.Fatalf("flat list thread ids %d %d %d", flat.Messages[0].ThreadID, flat.Messages[1].ThreadID, flat.Messages[2].ThreadID)
	}
}

func TestGetThreadMarksFolderAndSender(t *testing.T) {
	b := newThreadTestMailbox(t)
	archive := addFolder(t, b.app, b.account, "Archive", `\Archive`)
	b.add(t, archive, storage.Message{MessageID: "a@x", Subject: "plans", FromAddress: "Alice <alice@x>"}, 0)
	inboxCopy := b.add(t, b.inbox, storage.Message{MessageID: "a@x", Subject: "plans", FromAddress: "Alice <alice@x>"}, 0)
	reply := b.add(t, b.sent, storage.Message{MessageID: "b@x", Subject: "Re: plans", FromAddress: "Me <me@example.com>", InReplyTo: []string{"a@x"}}, 1)
	b.add(t, b.trash, storage.Message{MessageID: "c@x", Subject: "Re: plans", FromAddress: "bob@x", InReplyTo: []string{"b@x"}}, 2)

	m, err := b.db.GetMessage(b.app.ctx, reply)
	if err != nil {
		t.Fatal(err)
	}
	thread, err := b.app.GetThread(m.ThreadID)
	if err != nil {
		t.Fatalf("GetThread: %v", err)
	}
	if len(thread.Messages) != 2 {
		t.Fatalf("got %d messages, want the inbox copy and the reply", len(thread.Messages))
	}
	got := thread.Messages
	if got[0].ID != inboxCopy || got[0].FolderRole != roleInbox || got[0].FromMe {
		t.Fatalf("first = %+v, want the inbox copy from someone else", got[0])
	}
	if got[1].ID != reply || got[1].FolderRole != roleSent || !got[1].FromMe || got[1].FolderName != "Sent" {
		t.Fatalf("second = %+v, want the user's reply in Sent", got[1])
	}

	if empty, err := b.app.GetThread(0); err != nil || len(empty.Messages) != 0 {
		t.Fatalf("GetThread(0) = %+v, %v; want nothing", empty, err)
	}
}

func TestSendingMarksTheOriginal(t *testing.T) {
	b := newThreadTestMailbox(t)
	b.app.queue = outbox.NewQueue(b.db)
	parent := b.add(t, b.inbox, storage.Message{MessageID: "parent@x", Subject: "question", FromAddress: "alice@x"}, 0)
	forwarded := b.add(t, b.inbox, storage.Message{MessageID: "fwd@x", Subject: "report", FromAddress: "bob@x"}, 1)
	cancelled := b.add(t, b.inbox, storage.Message{MessageID: "never@x", Subject: "draft", FromAddress: "carol@x"}, 2)
	to := []AddressDTO{{Email: "alice@x"}}

	if _, err := b.app.SendMessage(ComposeRequest{AccountID: b.account, To: to, Subject: "Re: question", Text: "yes", InReplyTo: "<parent@x>"}); err != nil {
		t.Fatalf("send reply: %v", err)
	}
	if _, err := b.app.SendMessage(ComposeRequest{AccountID: b.account, To: to, Subject: "Fwd: report", Text: "see below", ForwardOf: forwarded}); err != nil {
		t.Fatalf("send forward: %v", err)
	}
	if _, err := b.app.SendMessage(ComposeRequest{AccountID: b.account, To: to, Subject: "Re: draft", Text: "later", InReplyTo: "never@x"}); err != nil {
		t.Fatalf("send third: %v", err)
	}

	if m, _ := b.db.GetMessage(b.app.ctx, parent); m.Flags.Has(storage.FlagAnswered) {
		t.Fatal("marked answered while the reply is still queued")
	}

	queued, err := b.app.queue.List(b.app.ctx)
	if err != nil || len(queued) != 3 {
		t.Fatalf("queue = %d rows, %v", len(queued), err)
	}
	var replyHeaderID string
	for _, q := range queued {
		parsed, err := mail.ReadMessage(bytes.NewReader(q.Raw))
		if err != nil {
			t.Fatal(err)
		}
		header := parsed.Header
		if header.Get("In-Reply-To") == "<never@x>" {
			continue
		}
		if header.Get("In-Reply-To") == "<parent@x>" {
			replyHeaderID = header.Get("Message-ID")
		}
		b.app.afterSent(b.app.ctx, q)
	}

	for _, tc := range []struct {
		id   int64
		want storage.Flag
	}{
		{parent, storage.FlagAnswered},
		{forwarded, storage.FlagForwarded},
		{cancelled, 0},
	} {
		m, err := b.db.GetMessage(b.app.ctx, tc.id)
		if err != nil {
			t.Fatal(err)
		}
		if got := m.Flags & (storage.FlagAnswered | storage.FlagForwarded); got != tc.want {
			t.Fatalf("message %q reply state = %d, want %d", m.Subject, got, tc.want)
		}
	}
	states, err := b.db.ListMessageStates(b.app.ctx, b.inbox.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range states {
		if (s.ID == parent || s.ID == forwarded) && (!s.PendingFlags || s.PendingMask&(storage.FlagAnswered|storage.FlagForwarded) == 0) {
			t.Fatalf("message %d not queued for the server: %+v", s.ID, s)
		}
	}

	sentCopy := b.add(t, b.sent, storage.Message{MessageID: replyHeaderID, Subject: "Re: question", FromAddress: "me@example.com", InReplyTo: []string{"parent@x"}}, 3)
	detail, err := b.app.GetMessage(parent)
	if err != nil {
		t.Fatalf("GetMessage: %v", err)
	}
	if !detail.Answered || !slices.Equal(detail.ReplyIDs, []int64{sentCopy}) || len(detail.ForwardIDs) != 0 {
		t.Fatalf("detail answered %v replies %v forwards %v, want the sent copy", detail.Answered, detail.ReplyIDs, detail.ForwardIDs)
	}
	if detail.ThreadID == 0 || detail.ThreadID != mustThread(t, b.db, sentCopy) {
		t.Fatal("the sent copy did not join the original's thread")
	}
}

func mustThread(t *testing.T, db *storage.DB, id int64) int64 {
	t.Helper()
	m, err := db.GetMessage(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return m.ThreadID
}

func TestBackfillThreadsThreadsLegacyMail(t *testing.T) {
	b := newThreadTestMailbox(t)
	parent := b.add(t, b.inbox, storage.Message{MessageID: "p@x", Subject: "x"}, 0)
	child := b.add(t, b.inbox, storage.Message{MessageID: "c@x", Subject: "Re: x", InReplyTo: []string{"p@x"}}, 1)

	raw, err := sql.Open("sqlite", b.db.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	for _, q := range []string{`UPDATE messages SET thread_id = 0`, `DELETE FROM thread_keys`, `DELETE FROM threads`} {
		if _, err := raw.Exec(q); err != nil {
			t.Fatal(err)
		}
	}

	list, err := b.app.ListThreads(ListMessagesRequest{Kind: "folder", FolderID: b.inbox.ID, Limit: 50})
	if err != nil || len(list.Threads) != 2 || list.Threads[0].ThreadID != 0 {
		t.Fatalf("before the backfill = %+v, %v; want each message on its own", list, err)
	}

	b.app.backfillThreads()
	if mustThread(t, b.db, parent) == 0 || mustThread(t, b.db, parent) != mustThread(t, b.db, child) {
		t.Fatal("backfill did not join the reply to its parent")
	}
	list, err = b.app.ListThreads(ListMessagesRequest{Kind: "folder", FolderID: b.inbox.ID, Limit: 50})
	if err != nil || len(list.Threads) != 1 || list.Threads[0].Count != 2 {
		t.Fatalf("after the backfill = %+v, %v; want one conversation", list, err)
	}
}
