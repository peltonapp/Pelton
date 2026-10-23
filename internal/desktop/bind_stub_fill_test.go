package desktop

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"

	"github.com/peltonapp/Pelton/internal/credentials"
	"github.com/peltonapp/Pelton/internal/desktop/syncsched"
	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/storage"
)

// bodyIMAP is a server holding the body of uid 7, with one attachment, or
// failing every fetch with fail. It answers SEARCH SINCE with uid 7.
type bodyIMAP struct {
	fakeIMAP
	fail    error
	fetched int
}

func (c *bodyIMAP) FetchMessages(uids []imap.UID, fn func(imap.UID, *pimap.Message, error) error) error {
	c.fetched++
	if c.fail != nil {
		return c.fail
	}
	for _, uid := range uids {
		if uid != 7 {
			continue
		}
		if err := fn(uid, &pimap.Message{
			UID: uid, MessageID: "<stub@example.com>", Subject: "stub", From: "ada@example.com",
			Date: time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC), Text: "the body",
			Attachments: []pimap.Attachment{{Filename: "a.txt", ContentType: "text/plain", Content: []byte("x")}},
		}, nil); err != nil {
			return err
		}
	}
	return nil
}

// FetchMessage is the main-era single fetch the range download uses.
func (c *bodyIMAP) FetchMessage(uid imap.UID) (*pimap.Message, error) {
	var out *pimap.Message
	err := c.FetchMessages([]imap.UID{uid}, func(_ imap.UID, m *pimap.Message, _ error) error { out = m; return nil })
	if out == nil && err == nil {
		err = errors.New("no such message")
	}
	return out, err
}

func (c *bodyIMAP) SearchSince(time.Time) ([]imap.UID, error) { return []imap.UID{7}, nil }

// stubRig is an account with an INBOX holding the list stub of uid 7 (no
// body yet), whose sessions all use client.
func stubRig(t *testing.T, client mailClient) (*App, storage.Account, storage.Folder, int64) {
	t.Helper()
	a, db, ctx := moveTestApp(t)
	accountID, err := db.CreateAccount(ctx, &storage.Account{Email: "me@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	inbox := &storage.Folder{AccountID: accountID, Name: "INBOX", IMAPPath: "INBOX", RemoteID: "INBOX"}
	if _, err := db.CreateFolder(ctx, inbox); err != nil {
		t.Fatal(err)
	}
	id, err := db.UpsertMessageListMeta(ctx, &storage.Message{
		AccountID: accountID, FolderID: inbox.ID, UID: 7, RemoteID: "7",
		Subject: "stub", FromAddress: "ada@example.com", Date: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := credentials.Store(accountID, credentials.Secret{Method: credentials.MethodPassword, Password: "hunter2"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = credentials.Delete(accountID) })
	a.newIMAPClient = func(pimap.Config) (mailClient, error) { return client, nil }
	// the fast-lane fetch starts the account's runtime; it stops first.
	t.Cleanup(func() { a.stopAccountScheduler(accountID, false) })
	account, err := db.GetAccount(ctx, accountID)
	if err != nil {
		t.Fatal(err)
	}
	return a, *account, *inbox, id
}

func mustMessage(t *testing.T, a *App, id int64) *storage.Message {
	t.Helper()
	m, err := a.store.GetMessage(a.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// Opening a stub the sync has not filled yet returns the whole message, its
// attachment included, and stores it.
func TestOpenedStubReturnsItsBody(t *testing.T) {
	client := &bodyIMAP{}
	a, _, _, id := stubRig(t, client)
	detail, err := a.GetMessage(id)
	if err != nil {
		t.Fatalf("GetMessage: %v", err)
	}
	if detail.BodyPlain != "the body" || len(detail.Attachments) != 1 {
		t.Fatalf("detail body %q, %d attachments; want the body and one attachment", detail.BodyPlain, len(detail.Attachments))
	}
	if m := mustMessage(t, a, id); !m.BodyComplete {
		t.Fatal("the opened stub was not stored with its body")
	}
	if _, err := a.GetMessage(id); err != nil || client.fetched != 1 {
		t.Fatalf("second open: err %v, %d fetches; want the stored body, one fetch", err, client.fetched)
	}
}

// A failed fetch is an error the pane shows, not an empty message.
func TestOpenedStubFetchFailureIsAnError(t *testing.T) {
	a, _, _, id := stubRig(t, &bodyIMAP{fail: errors.New("connection reset by peer")})
	if _, err := a.GetMessage(id); !errors.Is(err, errBodyUnavailable) {
		t.Fatalf("GetMessage err = %v, want errBodyUnavailable", err)
	}
	if m := mustMessage(t, a, id); m.BodyComplete {
		t.Fatal("a failed fetch marked the stub complete")
	}
}

// A successful offline download leaves the message readable offline; a
// failed one returns its error and pins nothing.
func TestR7OfflinePinRequiresBody(t *testing.T) {
	t.Run("fetched", func(t *testing.T) {
		a, _, _, id := stubRig(t, &bodyIMAP{})
		if err := a.DownloadMessageOffline(id); err != nil {
			t.Fatalf("DownloadMessageOffline: %v", err)
		}
		if m := mustMessage(t, a, id); !m.Offline || !m.BodyComplete || m.BodyPlain != "the body" {
			t.Fatalf("after download Offline=%v BodyComplete=%v BodyPlain=%q, want a pinned body", m.Offline, m.BodyComplete, m.BodyPlain)
		}
	})
	t.Run("fetch failed", func(t *testing.T) {
		a, _, _, id := stubRig(t, &bodyIMAP{fail: errors.New("connection reset by peer")})
		if err := a.DownloadMessageOffline(id); err == nil {
			t.Fatal("a failed fetch reported a successful offline download")
		}
		if m := mustMessage(t, a, id); m.Offline {
			t.Fatal("a stub without its body was pinned offline")
		}
	})
}

// A date-range download fetches a cached stub (the server's SINCE lists
// it) and pins it once its body is stored, rather than counting it as
// downloaded already. The stub keeps its attachments even when the user
// chose to leave them out, as a sync's body fill would.
func TestR7RangeDownloadMustIncludeStub(t *testing.T) {
	a, account, _, id := stubRig(t, &bodyIMAP{})
	tasks, pin, err := a.planAccount(a.ctx, account, time.Now().AddDate(0, -1, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].remoteID != "7" {
		t.Fatalf("stub omitted: tasks=%+v pin=%v", tasks, pin)
	}
	if err := a.runDownload(a.ctx, tasks, pin, false, len(tasks)); err != nil {
		t.Fatal(err)
	}
	if atts, err := a.store.ListAttachments(a.ctx, id); err != nil || len(atts) != 1 {
		t.Errorf("stub has %d attachments (%v), want its one", len(atts), err)
	}
	if m := mustMessage(t, a, id); !m.Offline || !m.BodyComplete || m.BodyPlain != "the body" {
		t.Fatalf("after the range download Offline=%v BodyComplete=%v BodyPlain=%q, want a pinned body", m.Offline, m.BodyComplete, m.BodyPlain)
	}
}

// r8IMAP is bodyIMAP under a given UIDVALIDITY; before runs as the FETCH
// starts and amend edits each fetched message.
type r8IMAP struct {
	bodyIMAP
	validity uint32
	before   func()
	amend    func(*pimap.Message)
}

func (c *r8IMAP) Select(path string) (*pimap.Mailbox, error) {
	return &pimap.Mailbox{Name: path, UIDValidity: c.validity}, nil
}

func (c *r8IMAP) FetchMessages(uids []imap.UID, fn func(imap.UID, *pimap.Message, error) error) error {
	if c.before != nil {
		c.before()
	}
	return c.bodyIMAP.FetchMessages(uids, func(uid imap.UID, m *pimap.Message, err error) error {
		if c.amend != nil && m != nil {
			c.amend(m)
		}
		return fn(uid, m, err)
	})
}

// A stub cached under one UIDVALIDITY is never filled with what the same
// UID names under another: the open fails, and the next sync resets the
// folder.
func TestR8RejectChangedUIDValidity(t *testing.T) {
	c := &r8IMAP{validity: 222}
	a, _, folder, id := stubRig(t, c)
	if err := a.store.SetFolderUIDValidity(a.ctx, folder.ID, 111); err != nil {
		t.Fatal(err)
	}
	c.amend = func(m *pimap.Message) { m.Subject = "DIFFERENT MESSAGE under reused UID" }
	_, err := a.GetMessage(id)
	if m := mustMessage(t, a, id); m.BodyComplete {
		t.Fatalf("stored wrong-generation body: err=%v subject=%q", err, m.Subject)
	}
	if !errors.Is(err, errBodyUnavailable) {
		t.Fatalf("GetMessage err = %v, want errBodyUnavailable", err)
	}
}

// A stub moved or deleted while its body is in flight stays gone: the fill
// updates the existing row only.
func TestR8RemovedStubMustNotReturn(t *testing.T) {
	c := &r8IMAP{}
	a, _, folder, id := stubRig(t, c)
	c.before = func() {
		if err := a.store.DeleteMessage(a.ctx, id); err != nil {
			t.Error(err)
		}
	}
	_, err := a.GetMessage(id)
	if !errors.Is(err, storage.ErrMessageNotFound) {
		t.Errorf("GetMessage err = %v, want ErrMessageNotFound", err)
	}
	if newID, lookup := a.store.MessageIDByRemoteID(a.ctx, folder.ID, "7"); !errors.Is(lookup, storage.ErrMessageNotFound) {
		t.Fatalf("deleted stub came back as row %d (old %d): %v", newID, id, lookup)
	}
}

// The fill stores the whole fetched message, as a sync would: Reply-To,
// unsubscribe, charset and the source of protected mail.
func TestR8PreserveFetchedHeadersAndProtectedSource(t *testing.T) {
	c := &r8IMAP{}
	a, _, _, id := stubRig(t, c)
	c.amend = func(m *pimap.Message) {
		m.ReplyTo = "reply@example.com"
		m.ListUnsubscribe = "<mailto:unsub@example.com>"
		m.ListUnsubscribePost = true
		m.CharsetGuess = "windows-1250"
		m.Raw = []byte("Content-Type: multipart/encrypted; protocol=\"application/pgp-encrypted\"; boundary=x\r\n\r\n--x--\r\n")
	}
	if _, err := a.GetMessage(id); err != nil {
		t.Fatal(err)
	}
	m := mustMessage(t, a, id)
	if m.ReplyTo != "reply@example.com" || m.ListUnsubscribe == "" || m.CharsetGuess == "" {
		t.Errorf("fetched metadata dropped: ReplyTo=%q ListUnsubscribe=%q CharsetGuess=%q", m.ReplyTo, m.ListUnsubscribe, m.CharsetGuess)
	}
	if raw, err := a.store.MessagePGPSource(a.ctx, id); err != nil || len(raw) == 0 {
		t.Errorf("protected source dropped: len=%d err=%v", len(raw), err)
	}
}

// A sync that fills the stub while the open's fetch is in flight wins: its
// body and its one attachment stay, and the text revision moves once.
func TestR8ConcurrentFillKeepsSingleAttachmentAndRevision(t *testing.T) {
	c := &r8IMAP{}
	a, _, folder, id := stubRig(t, c)
	before := mustMessage(t, a, id)
	c.before = func() {
		m := &storage.Message{AccountID: folder.AccountID, FolderID: folder.ID, UID: 7, RemoteID: "7", BodyPlain: "sync won"}
		atts := []storage.IncomingAttachment{{Filename: "one.txt", Content: strings.NewReader("sync file")}}
		if _, err := a.store.FillMessageBody(a.ctx, storage.BodyTarget{}, m, atts); err != nil {
			t.Error(err)
		}
	}
	if _, err := a.GetMessage(id); err != nil {
		t.Fatal(err)
	}
	after := mustMessage(t, a, id)
	atts, err := a.store.ListAttachments(a.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if after.BodyPlain != "sync won" || len(atts) != 1 || after.TextRev != before.TextRev+1 || after.SearchState > storage.SearchHeaders {
		t.Fatalf("concurrent fill: body %q, %d attachments, text rev %d (was %d), search state %d", after.BodyPlain, len(atts), after.TextRev, before.TextRev, after.SearchState)
	}
}

// Opening stubs on a server that refuses them counts toward the throttle:
// two failures in a row halve the account's sessions.
func TestR8StubFetchFeedsThrottle(t *testing.T) {
	c := &bodyIMAP{fail: errors.New("NO [LIMIT] too many open connections")}
	a, account, _, id := stubRig(t, c)
	setParallel(t, a, 4)
	rt := mustAccountSync(t, a, account.ID)
	for range 2 {
		if _, err := a.GetMessage(id); err == nil {
			t.Fatal("expected the refusal")
		}
	}
	if n := rt.pool.Effective(); n != 2 {
		t.Fatalf("two failed body fetches left effective N=%d, want 2", n)
	}
}

// Opening a stub larger than the chunk size fetches it in chunks in the
// normal lane, never whole in one fast-lane unit, and returns it whole.
func TestR8LargeOpenComesInChunks(t *testing.T) {
	const size = 12 << 20
	client := &pagingIMAP{n: 1, sources: map[imap.UID][]byte{1: bigSource(size)}}
	a, _, inbox, _ := pagingInbox(t, client)
	seedStub(t, a, inbox, 1, size)
	id, err := a.store.MessageIDByRemoteID(a.ctx, inbox.ID, "1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.GetMessage(id); err != nil {
		t.Fatalf("GetMessage: %v", err)
	}
	ops := client.opsSnapshot()
	if len(ops) != 3 || !strings.HasPrefix(ops[0], "chunk ") {
		t.Fatalf("ops %v, want three chunks", ops)
	}
	if m := mustMessage(t, a, id); !m.BodyComplete {
		t.Fatal("the opened message has no body")
	}
}

// generationResetIMAP selects INBOX under UIDVALIDITY 111 and, while the
// FETCH is in flight, runs reset: a listing that found 222 and replaced the
// local stub of uid 7.
type generationResetIMAP struct {
	fakeIMAP
	reset func()
}

func (c *generationResetIMAP) Select(path string) (*pimap.Mailbox, error) {
	return &pimap.Mailbox{Name: path, UIDValidity: 111}, nil
}

func (c *generationResetIMAP) FetchMessages(_ []imap.UID, fn func(imap.UID, *pimap.Message, error) error) error {
	c.reset()
	return fn(7, &pimap.Message{UID: 7, Subject: "old generation", Text: "OLD-GENERATION BODY"}, nil)
}

// generationRig is an INBOX under UIDVALIDITY 111 holding the stub of uid 7,
// and a server whose FETCH replaces that stub with a new-generation one
// (222) while the old body is in flight. newID receives the replacement.
func generationRig(t *testing.T) (*App, storage.Folder, int64, *generationResetIMAP, chan int64) {
	t.Helper()
	a, db, ctx := moveTestApp(t)
	accountID, err := db.CreateAccount(ctx, &storage.Account{Email: "me@example.test"})
	if err != nil {
		t.Fatal(err)
	}
	folder := storage.Folder{AccountID: accountID, Name: "INBOX", IMAPPath: "INBOX", RemoteID: "INBOX", UIDValidity: 111}
	if _, err := db.CreateFolder(ctx, &folder); err != nil {
		t.Fatal(err)
	}
	if err := db.SetFolderUIDValidity(ctx, folder.ID, 111); err != nil {
		t.Fatal(err)
	}
	oldID, err := db.UpsertMessageListMeta(ctx, &storage.Message{AccountID: accountID, FolderID: folder.ID, UID: 7, RemoteID: "7", Subject: "old stub"})
	if err != nil {
		t.Fatal(err)
	}
	if err := credentials.Store(accountID, credentials.Secret{Method: credentials.MethodPassword, Password: "hunter2"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = credentials.Delete(accountID) })
	replaced := make(chan int64, 1)
	c := &generationResetIMAP{reset: func() {
		if err := db.DeleteMessage(ctx, oldID); err != nil {
			t.Error(err)
			return
		}
		if err := db.SetFolderUIDValidity(ctx, folder.ID, 222); err != nil {
			t.Error(err)
			return
		}
		newID, err := db.UpsertMessageListMeta(ctx, &storage.Message{AccountID: accountID, FolderID: folder.ID, UID: 7, RemoteID: "7", Subject: "NEW GENERATION STUB"})
		if err != nil {
			t.Error(err)
			return
		}
		replaced <- newID
	}}
	a.newIMAPClient = func(pimap.Config) (mailClient, error) { return c, nil }
	return a, folder, oldID, c, replaced
}

// The engine's fill drops a body fetched under the old generation when a
// listing reset the folder meanwhile: the replacement stub for the reused
// uid stays a stub.
func TestR9LocalGenerationResetDuringFill(t *testing.T) {
	a, folder, _, c, replaced := generationRig(t)
	if err := a.fillStubOnSession(a.ctx, c, folder, "7"); err != nil {
		t.Logf("fill: %v", err)
	}
	newID := <-replaced
	if m := mustMessage(t, a, newID); m.BodyComplete {
		t.Fatalf("old-generation body filled the replacement row %d: %q", newID, m.BodyPlain)
	}
}

// The same through GetMessage: opening the old stub never fills the
// replacement row.
func TestR9ActualOpenGenerationReset(t *testing.T) {
	a, _, oldID, _, replaced := generationRig(t)
	_, openErr := a.GetMessage(oldID)
	var newID int64
	select {
	case newID = <-replaced:
	case <-time.After(2 * time.Second):
		t.Fatalf("no fetch; GetMessage err=%v", openErr)
	}
	if openErr == nil {
		t.Error("opening a stub whose folder was reset meanwhile succeeded")
	}
	if m := mustMessage(t, a, newID); m.BodyComplete {
		t.Fatalf("opening old row %d filled the new-generation row %d: %q", oldID, newID, m.BodyPlain)
	}
}

// Two callers that join one open (GetMessage and DownloadMessageOffline
// while the account is busy) share its one FETCH, and its refusal counts
// once: N stays 4.
func TestR9JoinedStubOutcomeCountedOnce(t *testing.T) {
	c := &bodyIMAP{fail: errors.New("NO [LIMIT] too many open connections")}
	a, account, folder, id := stubRig(t, c)
	setParallel(t, a, 4)
	rt := mustAccountSync(t, a, account.ID)
	started, release := make(chan struct{}), make(chan struct{})
	rt.sched.Enqueue(imapTask{Kind: syncsched.KindListStubs, FolderID: folder.ID, Barrier: true, Run: func(context.Context, mailClient) error {
		close(started)
		<-release
		return nil
	}})
	<-started
	out := make(chan error, 2)
	go func() { _, err := a.GetMessage(id); out <- err }()
	go func() { out <- a.DownloadMessageOffline(id) }()
	waitUntil(t, 2*time.Second, "the two opens never joined one queued task", func() bool { return rt.sched.Stats().Fast == 1 })
	time.Sleep(50 * time.Millisecond)
	if st := rt.sched.Stats(); st.Fast != 1 {
		close(release)
		t.Fatalf("want one joined queued open: %+v", st)
	}
	close(release)
	for range 2 {
		if err := <-out; err == nil {
			t.Fatal("expected the fetch failure")
		}
	}
	if c.fetched != 1 {
		t.Fatalf("%d fetches, want 1", c.fetched)
	}
	if n := rt.pool.Effective(); n != 4 {
		t.Fatalf("one refused FETCH joined by two callers changed N to %d, want 4", n)
	}
}

// chunkRefusingIMAP refuses every partial FETCH.
type chunkRefusingIMAP struct {
	pagingIMAP
	calls int
}

func (c *chunkRefusingIMAP) FetchPartial(imap.UID, int64, int64) (*pimap.Message, error) {
	c.calls++
	return nil, errors.New("NO [LIMIT] too many open connections")
}

// Each refused chunk of a large open counts toward the throttle once: two
// failed large opens halve N.
func TestR9L7LargeOpenFeedsThrottle(t *testing.T) {
	c := &chunkRefusingIMAP{pagingIMAP: pagingIMAP{n: 1}}
	a, account, folder := reconcileInbox(t, c)
	setParallel(t, a, 4)
	rt := mustAccountSync(t, a, account.ID)
	seedStub(t, a, folder, 1, 12<<20)
	id, err := a.store.MessageIDByRemoteID(a.ctx, folder.ID, "1")
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := a.GetMessage(id); err == nil {
			t.Fatal("expected the refusal")
		}
	}
	if c.calls != 2 {
		t.Fatalf("%d partial fetches, want 2", c.calls)
	}
	if n := rt.pool.Effective(); n != 2 {
		t.Fatalf("two refused large opens left N=%d, want 2", n)
	}
}
