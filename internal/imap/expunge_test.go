package imap

import (
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
)

// the messages the tests put in the mailbox. Content does not matter, only that
// each is a distinct message with its own uid.
const testMessage = "From: someone@example.com\r\n" +
	"To: me@example.com\r\n" +
	"Subject: %s\r\n" +
	"\r\n" +
	"body\r\n"

// testOptions configures the in-memory server a test runs against.
type testOptions struct {
	// caps is what the server advertises. nil is IMAP4rev1 only, which is a
	// server without UIDPLUS: the dangerous case.
	caps imap.CapSet
	// failSearch makes SEARCH fail, so the client cannot learn what else is
	// marked \Deleted.
	failSearch bool
	// failStoreAfter makes the nth STORE fail (1 = the first). 0 never fails.
	// It is how the restore step is broken on purpose.
	failStoreAfter int
	// subjects seeds one message per subject.
	subjects []string
}

// newTestServer starts an in-memory imap server on localhost and returns a
// logged-in client with INBOX selected, plus the raw protocol stream both ways.
//
// Nothing here is a mock: it is the real go-imap server, so a plain EXPUNGE
// behaves exactly as it does against a real one, which is the entire point of
// these tests.
func newTestServer(t *testing.T, opts testOptions) (*Client, *wireLog) {
	t.Helper()

	mem := imapmemserver.New()
	user := imapmemserver.NewUser("user", "pass")
	if err := user.Create("INBOX", nil); err != nil {
		t.Fatalf("create INBOX: %v", err)
	}
	mem.AddUser(user)

	wire := &wireLog{}
	srv := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			session := mem.NewSession()
			if opts.failSearch || opts.failStoreAfter > 0 {
				session = &brokenSession{Session: session, opts: opts}
			}
			return session, nil, nil
		},
		Caps:         opts.caps,
		InsecureAuth: true,
		Logger:       discardLogger{},
		DebugWriter:  wire,
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	raw := imapclient.New(conn, nil)
	t.Cleanup(func() { _ = raw.Close() })
	if err := raw.Login("user", "pass").Wait(); err != nil {
		t.Fatalf("login: %v", err)
	}

	for _, subject := range opts.subjects {
		body := strings.Replace(testMessage, "%s", subject, 1)
		cmd := raw.Append("INBOX", int64(len(body)), nil)
		if _, err := cmd.Write([]byte(body)); err != nil {
			t.Fatalf("append %q: %v", subject, err)
		}
		if err := cmd.Close(); err != nil {
			t.Fatalf("close append %q: %v", subject, err)
		}
		if _, err := cmd.Wait(); err != nil {
			t.Fatalf("append %q: %v", subject, err)
		}
	}

	c := &Client{raw: raw}
	if _, err := c.Select("INBOX"); err != nil {
		t.Fatalf("select: %v", err)
	}
	// the seeding above is not part of what a test asserts on.
	wire.reset()
	return c, wire
}

// createMailbox adds a mailbox to the running test server, for the move tests.
func createMailbox(c *Client, name string) error {
	return c.raw.Create(name, nil).Wait()
}

// discardLogger keeps the server's connection logging out of the test output.
type discardLogger struct{}

func (discardLogger) Printf(string, ...any) {}

// plainServer is the common case these tests care about: no UIDPLUS.
func plainServer(t *testing.T, subjects ...string) (*Client, *wireLog) {
	t.Helper()
	return newTestServer(t, testOptions{subjects: subjects})
}

// wireLog records the protocol stream so a test can assert on the commands
// actually sent, not just on the mailbox that came out the other end.
type wireLog struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (w *wireLog) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf.Write(p)
	return len(p), nil
}

func (w *wireLog) reset() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf.Reset()
}

func (w *wireLog) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

// commands returns the client commands seen, uppercased and without their tags,
// in order.
func (w *wireLog) commands() []string {
	var out []string
	for line := range strings.SplitSeq(w.String(), "\n") {
		line = strings.TrimSpace(line)
		// client lines are "<tag> COMMAND ..."; server lines start with * or +.
		if line == "" || strings.HasPrefix(line, "*") || strings.HasPrefix(line, "+") {
			continue
		}
		parts := strings.SplitN(line, " ", 2)
		if len(parts) != 2 {
			continue
		}
		out = append(out, strings.ToUpper(parts[1]))
	}
	return out
}

// hasBareExpunge reports whether an unscoped EXPUNGE was sent, which is the
// command that takes another client's marked mail with it.
func (w *wireLog) hasBareExpunge() bool {
	return slices.Contains(w.commands(), "EXPUNGE")
}

// brokenSession makes a chosen command fail, to exercise the paths where the
// server stops cooperating half way through a delete.
type brokenSession struct {
	imapserver.Session
	opts   testOptions
	stores int
}

func (s *brokenSession) Search(kind imapserver.NumKind, criteria *imap.SearchCriteria, options *imap.SearchOptions) (*imap.SearchData, error) {
	if s.opts.failSearch {
		return nil, errors.New("search is broken")
	}
	return s.Session.Search(kind, criteria, options)
}

func (s *brokenSession) Store(w *imapserver.FetchWriter, numSet imap.NumSet, flags *imap.StoreFlags, options *imap.StoreOptions) error {
	s.stores++
	if s.opts.failStoreAfter > 0 && s.stores == s.opts.failStoreAfter {
		return errors.New("store is broken")
	}
	return s.Session.Store(w, numSet, flags, options)
}

// mailboxSubjects returns the subjects a mailbox holds, in uid order. It
// re-selects first: the client's cached message count follows the untagged
// EXPUNGE responses, and those are not something a test should assert through.
func mailboxSubjects(t *testing.T, c *Client, mailbox string) []string {
	t.Helper()
	if _, err := c.Select(mailbox); err != nil {
		t.Fatalf("select %q: %v", mailbox, err)
	}
	headers, err := c.FetchRecentHeaders(1000)
	if err != nil {
		t.Fatalf("fetch headers: %v", err)
	}
	slices.SortFunc(headers, func(a, b MessageHeader) int { return int(a.UID) - int(b.UID) })
	out := make([]string, 0, len(headers))
	for _, h := range headers {
		out = append(out, h.Subject)
	}
	return out
}

// uidOf returns the uid of the message with the given subject in INBOX.
func uidOf(t *testing.T, c *Client, subject string) imap.UID {
	t.Helper()
	if _, err := c.Select("INBOX"); err != nil {
		t.Fatalf("select INBOX: %v", err)
	}
	headers, err := c.FetchRecentHeaders(1000)
	if err != nil {
		t.Fatalf("fetch headers: %v", err)
	}
	for _, h := range headers {
		if h.Subject == subject {
			return h.UID
		}
	}
	t.Fatalf("no message with subject %q", subject)
	return 0
}

// TestDeleteMessagesLeavesAnotherClientsDeletedMail is the regression test for
// the destructive case: Thunderbird marked a message \Deleted and has not
// expunged it, Pelton is asked to delete a different one, and the server has no
// UIDPLUS. A plain EXPUNGE here takes both. Only the one Pelton was told to
// delete may go.
func TestDeleteMessagesLeavesAnotherClientsDeletedMail(t *testing.T) {
	c, wire := plainServer(t, "theirs", "ours", "untouched")

	theirs := uidOf(t, c, "theirs")
	ours := uidOf(t, c, "ours")

	// another client marks its message and leaves it sitting there.
	if err := c.storeMany([]imap.UID{theirs}, imap.StoreFlagsAdd, imap.FlagDeleted); err != nil {
		t.Fatalf("mark theirs: %v", err)
	}
	wire.reset()

	if err := c.DeleteMessages(ours); err != nil {
		t.Fatalf("DeleteMessages: %v", err)
	}

	got := mailboxSubjects(t, c, "INBOX")
	want := []string{"theirs", "untouched"}
	if !slices.Equal(got, want) {
		t.Fatalf("mailbox holds %v, want %v", got, want)
	}

	// and their mark has to survive too, or their pending deletion is silently
	// undone.
	deleted, err := c.searchDeleted()
	if err != nil {
		t.Fatalf("search deleted: %v", err)
	}
	if !slices.Equal(deleted, []imap.UID{theirs}) {
		t.Errorf("\\Deleted uids = %v, want just %v", deleted, theirs)
	}

	// and the shape of it on the wire: their mark comes off, ours goes on, the
	// expunge runs, their mark goes back on.
	wantCmds := []string{"UID STORE", "UID STORE", "EXPUNGE", "UID STORE"}
	var gotCmds []string
	for _, cmd := range wire.commands() {
		for _, prefix := range wantCmds {
			if strings.HasPrefix(cmd, prefix) {
				gotCmds = append(gotCmds, prefix)
				break
			}
		}
	}
	if !slices.Equal(gotCmds, wantCmds) {
		t.Errorf("commands were %v, want %v\n%s", gotCmds, wantCmds, wire.String())
	}
}

// TestDeleteMessagesWithUIDPlus covers the same scenario on a server that does
// advertise UIDPLUS, where UID EXPUNGE does the scoping for us.
func TestDeleteMessagesWithUIDPlus(t *testing.T) {
	caps := imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapUIDPlus: {}}
	c, wire := newTestServer(t, testOptions{caps: caps, subjects: []string{"theirs", "ours", "untouched"}})

	theirs := uidOf(t, c, "theirs")
	ours := uidOf(t, c, "ours")
	if err := c.storeMany([]imap.UID{theirs}, imap.StoreFlagsAdd, imap.FlagDeleted); err != nil {
		t.Fatalf("mark theirs: %v", err)
	}

	if err := c.DeleteMessages(ours); err != nil {
		t.Fatalf("DeleteMessages: %v", err)
	}

	got := mailboxSubjects(t, c, "INBOX")
	want := []string{"theirs", "untouched"}
	if !slices.Equal(got, want) {
		t.Fatalf("mailbox holds %v, want %v", got, want)
	}
	if wire.hasBareExpunge() {
		t.Errorf("an unscoped EXPUNGE was sent even though the server has UIDPLUS\n%s", wire.String())
	}
}

// TestDeleteMessagesBatch deletes several at once, which is what a folder full
// of pending deletes or an emptied trash does.
func TestDeleteMessagesBatch(t *testing.T) {
	c, _ := plainServer(t, "one", "two", "three", "keep")

	uids := []imap.UID{uidOf(t, c, "one"), uidOf(t, c, "two"), uidOf(t, c, "three")}
	if err := c.DeleteMessages(uids...); err != nil {
		t.Fatalf("DeleteMessages: %v", err)
	}

	got := mailboxSubjects(t, c, "INBOX")
	if !slices.Equal(got, []string{"keep"}) {
		t.Fatalf("mailbox holds %v, want just [keep]", got)
	}
}

// TestDeleteMessagesNoUIDsIsANoOp guards the case that would be worst of all:
// an empty batch reaching a plain EXPUNGE and clearing whatever the mailbox
// happened to have marked.
func TestDeleteMessagesNoUIDsIsANoOp(t *testing.T) {
	c, wire := plainServer(t, "theirs", "keep")

	theirs := uidOf(t, c, "theirs")
	if err := c.storeMany([]imap.UID{theirs}, imap.StoreFlagsAdd, imap.FlagDeleted); err != nil {
		t.Fatalf("mark theirs: %v", err)
	}

	if err := c.DeleteMessages(); err != nil {
		t.Fatalf("DeleteMessages: %v", err)
	}

	got := mailboxSubjects(t, c, "INBOX")
	if !slices.Equal(got, []string{"theirs", "keep"}) {
		t.Fatalf("mailbox holds %v, want both messages", got)
	}
	if wire.hasBareExpunge() {
		t.Errorf("an empty batch still reached EXPUNGE\n%s", wire.String())
	}
}

// TestDeleteMessagesKeepsOurOwnEarlierMark covers a retry: a previous attempt
// already flagged our message, so it comes back from the search. It is ours, so
// it must not be treated as foreign and protected from its own deletion.
func TestDeleteMessagesKeepsOurOwnEarlierMark(t *testing.T) {
	c, _ := plainServer(t, "ours", "keep")

	ours := uidOf(t, c, "ours")
	if err := c.storeMany([]imap.UID{ours}, imap.StoreFlagsAdd, imap.FlagDeleted); err != nil {
		t.Fatalf("pre-mark ours: %v", err)
	}

	if err := c.DeleteMessages(ours); err != nil {
		t.Fatalf("DeleteMessages: %v", err)
	}
	if got := mailboxSubjects(t, c, "INBOX"); !slices.Equal(got, []string{"keep"}) {
		t.Fatalf("mailbox holds %v, want just [keep]", got)
	}
}

func TestForeignDeleted(t *testing.T) {
	tests := []struct {
		name     string
		existing []imap.UID
		ours     []imap.UID
		want     []imap.UID
	}{
		{"nothing marked", nil, []imap.UID{1}, nil},
		{"only ours marked", []imap.UID{1, 2}, []imap.UID{1, 2}, nil},
		{"one foreign", []imap.UID{1, 2, 3}, []imap.UID{2}, []imap.UID{1, 3}},
		{"all foreign", []imap.UID{7, 8}, []imap.UID{1}, []imap.UID{7, 8}},
		{"deleting nothing makes everything foreign", []imap.UID{7}, nil, []imap.UID{7}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := foreignDeleted(tt.existing, tt.ours); !slices.Equal(got, tt.want) {
				t.Errorf("foreignDeleted() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestDeleteMessagesLargeMixedBatch is the shape a real mailbox has: many
// messages, another client's marks scattered through them, and a big batch of
// our own deletions. Every one of ours must go and every one of theirs must
// stay, with its mark.
func TestDeleteMessagesLargeMixedBatch(t *testing.T) {
	const total = 60
	subjects := make([]string, 0, total)
	for i := range total {
		subjects = append(subjects, fmt.Sprintf("msg-%02d", i))
	}
	c, wire := plainServer(t, subjects...)

	// every third message is one another client marked, every other one is ours
	// to delete, and the rest are bystanders.
	var theirs, ours []imap.UID
	var wantLeft []string
	for i, subject := range subjects {
		uid := uidOf(t, c, subject)
		switch {
		case i%3 == 0:
			theirs = append(theirs, uid)
			wantLeft = append(wantLeft, subject)
		case i%2 == 1:
			ours = append(ours, uid)
		default:
			wantLeft = append(wantLeft, subject)
		}
	}
	if len(theirs) == 0 || len(ours) == 0 {
		t.Fatalf("bad fixture: %d theirs, %d ours", len(theirs), len(ours))
	}
	if err := c.storeMany(theirs, imap.StoreFlagsAdd, imap.FlagDeleted); err != nil {
		t.Fatalf("mark theirs: %v", err)
	}
	wire.reset()

	if err := c.DeleteMessages(ours...); err != nil {
		t.Fatalf("DeleteMessages: %v", err)
	}

	if got := mailboxSubjects(t, c, "INBOX"); !slices.Equal(got, wantLeft) {
		t.Errorf("mailbox holds %v,\nwant %v", got, wantLeft)
	}
	deleted, err := c.searchDeleted()
	if err != nil {
		t.Fatalf("search deleted: %v", err)
	}
	if !slices.Equal(deleted, theirs) {
		t.Errorf("\\Deleted uids = %v, want %v", deleted, theirs)
	}
}

// TestDeleteEveryMessageWithAForeignMark is the emptied trash: the batch is the
// whole folder except what another client marked, which still has to survive.
func TestDeleteEveryMessageWithAForeignMark(t *testing.T) {
	c, _ := plainServer(t, "theirs", "a", "b", "c")

	theirs := uidOf(t, c, "theirs")
	if err := c.storeMany([]imap.UID{theirs}, imap.StoreFlagsAdd, imap.FlagDeleted); err != nil {
		t.Fatalf("mark theirs: %v", err)
	}
	ours := []imap.UID{uidOf(t, c, "a"), uidOf(t, c, "b"), uidOf(t, c, "c")}

	if err := c.DeleteMessages(ours...); err != nil {
		t.Fatalf("DeleteMessages: %v", err)
	}
	if got := mailboxSubjects(t, c, "INBOX"); !slices.Equal(got, []string{"theirs"}) {
		t.Fatalf("mailbox holds %v, want just [theirs]", got)
	}
}

// TestDeleteMessagesRefusesWhenItCannotSeeWhatIsMarked: with no UIDPLUS and no
// working SEARCH there is no way to know what a plain EXPUNGE would take, so
// nothing is expunged at all. Failing a delete is recoverable; guessing is not.
func TestDeleteMessagesRefusesWhenItCannotSeeWhatIsMarked(t *testing.T) {
	c, wire := newTestServer(t, testOptions{
		failSearch: true,
		subjects:   []string{"theirs", "ours", "keep"},
	})

	theirs := uidOf(t, c, "theirs")
	if err := c.storeMany([]imap.UID{theirs}, imap.StoreFlagsAdd, imap.FlagDeleted); err != nil {
		t.Fatalf("mark theirs: %v", err)
	}
	wire.reset()

	err := c.DeleteMessages(uidOf(t, c, "ours"))
	if err == nil {
		t.Fatal("DeleteMessages returned no error when it could not search")
	}
	if wire.hasBareExpunge() {
		t.Errorf("expunged anyway after the search failed\n%s", wire.String())
	}
	if got := mailboxSubjects(t, c, "INBOX"); !slices.Equal(got, []string{"theirs", "ours", "keep"}) {
		t.Errorf("mailbox holds %v, want all three still there", got)
	}
}

// TestDeleteMessagesReportsAFailedRestore: the expunge went through but the
// other client's marks could not be put back. The caller has to hear about it,
// because from that client's side its pending deletions have been undone.
func TestDeleteMessagesReportsAFailedRestore(t *testing.T) {
	// stores in order: the test's own mark (1), lift the foreign mark (2), mark
	// ours (3), restore the foreign mark (4).
	c, _ := newTestServer(t, testOptions{
		failStoreAfter: 4,
		subjects:       []string{"theirs", "ours", "keep"},
	})

	theirs := uidOf(t, c, "theirs")
	if err := c.storeMany([]imap.UID{theirs}, imap.StoreFlagsAdd, imap.FlagDeleted); err != nil {
		t.Fatalf("mark theirs: %v", err)
	}

	err := c.DeleteMessages(uidOf(t, c, "ours"))
	if err == nil {
		t.Fatal("DeleteMessages hid a failed restore")
	}
	if !strings.Contains(err.Error(), "restore") {
		t.Errorf("error = %v, want it to say the marks could not be restored", err)
	}
	// the delete itself still happened, and nothing of theirs was lost with it.
	if got := mailboxSubjects(t, c, "INBOX"); !slices.Equal(got, []string{"theirs", "keep"}) {
		t.Errorf("mailbox holds %v, want [theirs keep]", got)
	}
}

// TestDeleteMessagesStopsWhenItCannotLiftForeignMarks: if the marks cannot be
// taken off, expunging would destroy that mail, so the delete does not happen.
func TestDeleteMessagesStopsWhenItCannotLiftForeignMarks(t *testing.T) {
	// stores in order: the test's own mark (1), lift the foreign mark (2).
	c, wire := newTestServer(t, testOptions{
		failStoreAfter: 2,
		subjects:       []string{"theirs", "ours", "keep"},
	})

	theirs := uidOf(t, c, "theirs")
	if err := c.storeMany([]imap.UID{theirs}, imap.StoreFlagsAdd, imap.FlagDeleted); err != nil {
		t.Fatalf("mark theirs: %v", err)
	}
	wire.reset()

	if err := c.DeleteMessages(uidOf(t, c, "ours")); err == nil {
		t.Fatal("DeleteMessages carried on after it could not lift the foreign marks")
	}
	if wire.hasBareExpunge() {
		t.Errorf("expunged with the foreign marks still on\n%s", wire.String())
	}
	if got := mailboxSubjects(t, c, "INBOX"); !slices.Equal(got, []string{"theirs", "ours", "keep"}) {
		t.Errorf("mailbox holds %v, want all three still there", got)
	}
}

// TestDeleteMessagesTwiceIsHarmless covers the retry a failed push does on the
// next sync, once the message is already gone.
func TestDeleteMessagesTwiceIsHarmless(t *testing.T) {
	c, _ := plainServer(t, "theirs", "ours", "keep")

	theirs := uidOf(t, c, "theirs")
	if err := c.storeMany([]imap.UID{theirs}, imap.StoreFlagsAdd, imap.FlagDeleted); err != nil {
		t.Fatalf("mark theirs: %v", err)
	}
	ours := uidOf(t, c, "ours")

	if err := c.DeleteMessages(ours); err != nil {
		t.Fatalf("first DeleteMessages: %v", err)
	}
	if err := c.DeleteMessages(ours); err != nil {
		t.Fatalf("second DeleteMessages on a gone uid: %v", err)
	}
	if got := mailboxSubjects(t, c, "INBOX"); !slices.Equal(got, []string{"theirs", "keep"}) {
		t.Errorf("mailbox holds %v, want [theirs keep]", got)
	}
}

// TestDeleteMessagesUnknownUID: a uid that was never in the mailbox must not
// turn into a mailbox-wide expunge.
func TestDeleteMessagesUnknownUID(t *testing.T) {
	c, _ := plainServer(t, "theirs", "keep")

	theirs := uidOf(t, c, "theirs")
	if err := c.storeMany([]imap.UID{theirs}, imap.StoreFlagsAdd, imap.FlagDeleted); err != nil {
		t.Fatalf("mark theirs: %v", err)
	}

	if err := c.DeleteMessages(imap.UID(9999)); err != nil {
		t.Fatalf("DeleteMessages: %v", err)
	}
	if got := mailboxSubjects(t, c, "INBOX"); !slices.Equal(got, []string{"theirs", "keep"}) {
		t.Errorf("mailbox holds %v, want both messages", got)
	}
}
