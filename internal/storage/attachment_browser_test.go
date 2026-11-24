package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"testing"
	"time"
)

type browserFixture struct {
	db      *DB
	account int64
	inbox   int64
	archive int64
	trash   int64
}

func newBrowserFixture(t *testing.T) browserFixture {
	t.Helper()
	db := newTestDB(t)
	ctx := context.Background()
	acc, err := db.EnsureLocalAccount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	f := browserFixture{db: db, account: acc.ID}
	for _, name := range []string{"INBOX", "Archive", "Trash"} {
		folder, err := db.EnsureLocalFolder(ctx, acc.ID, name)
		if err != nil {
			t.Fatal(err)
		}
		switch name {
		case "INBOX":
			f.inbox = folder.ID
		case "Archive":
			f.archive = folder.ID
		default:
			f.trash = folder.ID
		}
	}
	return f
}

var fixtureUID uint32

// add stores a message with the given parts in name order; a part's size is
// its content length, and a "cid:" prefix on the name gives it a content id.
func (f browserFixture) add(t *testing.T, folder int64, from, messageID string, date time.Time, parts map[string]int) int64 {
	t.Helper()
	fixtureUID++
	var atts []IncomingAttachment
	ordered := make([]string, 0, len(parts))
	for name := range parts {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	for _, name := range ordered {
		size := parts[name]
		cid := ""
		if strings.HasPrefix(name, "cid:") {
			name = strings.TrimPrefix(name, "cid:")
			cid = "<" + name + ">"
		}
		atts = append(atts, IncomingAttachment{Filename: name, ContentID: cid, Content: bytes.NewReader(make([]byte, size))})
	}
	id, err := f.db.InsertMessageWithAttachments(context.Background(), &Message{
		AccountID: f.account, FolderID: folder, UID: fixtureUID, MessageID: messageID,
		Subject: "s " + messageID, FromAddress: from, Date: date,
	}, atts)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func names(entries []AttachmentEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Filename
	}
	return out
}

func TestClassifyAttachment(t *testing.T) {
	cases := []struct {
		ct, name string
		want     AttachmentKind
	}{
		{"application/pdf", "x", KindPDF},
		{"", "Report.PDF", KindPDF},
		{"image/png", "a.png", KindImage},
		{"text/csv", "data.csv", KindSpreadsheet},
		{"application/octet-stream", "budget.xlsx", KindSpreadsheet},
		{"application/zip", "a.zip", KindArchive},
		{"", "backup.tar.gz", KindArchive},
		{"video/mp4", "clip", KindMedia},
		{"audio/mpeg", "song", KindMedia},
		{"text/plain", "notes.txt", KindDocument},
		{"", "letter.docx", KindDocument},
		{"application/octet-stream", "setup.exe", KindOther},
	}
	for _, c := range cases {
		if got := ClassifyAttachment(c.ct, c.name); got != c.want {
			t.Errorf("ClassifyAttachment(%q, %q) = %q, want %q", c.ct, c.name, got, c.want)
		}
	}
}

// the SQL classification and the Go one must agree, or a filter would hide
// files the row says belong to it.
func TestKindExprMatchesClassify(t *testing.T) {
	f := newBrowserFixture(t)
	ctx := context.Background()
	files := []string{"a.pdf", "b.jpeg", "c.csv", "d.7z", "e.mov", "f.md", "g.bin", "h.ODS"}
	parts := map[string]int{}
	for _, n := range files {
		parts[n] = 4096
	}
	f.add(t, f.inbox, "x@y", "<k@x>", time.Now(), parts)
	got, err := f.db.QueryAttachments(ctx, AttachmentQuery{FolderIDs: []int64{f.inbox}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(files) {
		t.Fatalf("got %d entries, want %d", len(got), len(files))
	}
	for _, e := range got {
		if want := ClassifyAttachment(e.ContentType, e.Filename); e.Kind != want {
			t.Errorf("%s: sql says %q, go says %q", e.Filename, e.Kind, want)
		}
	}
}

func TestQueryAttachmentsFiltersAndOrder(t *testing.T) {
	f := newBrowserFixture(t)
	ctx := context.Background()
	day := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	f.add(t, f.inbox, "Ann <ann@example.com>", "<1@x>", day, map[string]int{"plan.pdf": 50000})
	f.add(t, f.inbox, "bob@example.com", "<2@x>", day.Add(48*time.Hour), map[string]int{
		"photo.jpg":    300000,
		"pixel.gif":    43,
		"cid:logo.png": 9000,
		"cid:big.png":  90000,
	})
	f.add(t, f.archive, "Ann <ANN@example.com>", "<3@x>", day.Add(24*time.Hour), map[string]int{"sheet.xlsx": 7000})
	f.add(t, f.trash, "ann@example.com", "<4@x>", day.Add(72*time.Hour), map[string]int{"gone.pdf": 7000})
	visible := []int64{f.inbox, f.archive}

	all, err := f.db.QueryAttachments(ctx, AttachmentQuery{FolderIDs: visible})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(names(all), ",")
	// newest first; the tracking pixel and the small signature logo are out,
	// the large inline image stays, the trash folder is not asked for.
	if got != "big.png,photo.jpg,sheet.xlsx,plan.pdf" {
		t.Fatalf("got %s", got)
	}
	n, err := f.db.CountAttachments(ctx, AttachmentQuery{FolderIDs: visible})
	if err != nil || n != 4 {
		t.Fatalf("count = %d, %v; want 4", n, err)
	}

	cases := []struct {
		name string
		q    AttachmentQuery
		want string
	}{
		{"kind", AttachmentQuery{Kinds: []AttachmentKind{KindPDF, KindSpreadsheet}}, "sheet.xlsx,plan.pdf"},
		{"exact sender", AttachmentQuery{SenderAddress: "ann@example.com"}, "sheet.xlsx,plan.pdf"},
		{"sender text", AttachmentQuery{Sender: "bob"}, "big.png,photo.jpg"},
		{"name", AttachmentQuery{Name: "PLAN"}, "plan.pdf"},
		{"since", AttachmentQuery{Since: day.Add(time.Hour)}, "big.png,photo.jpg,sheet.xlsx"},
		{"until", AttachmentQuery{Until: day.Add(time.Hour)}, "plan.pdf"},
		{"page", AttachmentQuery{Limit: 2, Offset: 1}, "photo.jpg,sheet.xlsx"},
		{"wildcards are literal", AttachmentQuery{Name: "%"}, ""},
	}
	for _, c := range cases {
		q := c.q
		q.FolderIDs = visible
		page, err := f.db.QueryAttachments(ctx, q)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := strings.Join(names(page), ","); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
		q.Limit, q.Offset = 0, 0
		count, err := f.db.CountAttachments(ctx, q)
		if err != nil {
			t.Fatalf("%s count: %v", c.name, err)
		}
		if c.name != "page" && count != len(page) {
			t.Errorf("%s: count %d, page %d", c.name, count, len(page))
		}
	}
}

// gmail holds a message once per label; its files are listed once.
func TestQueryAttachmentsListsCopiesOnce(t *testing.T) {
	f := newBrowserFixture(t)
	ctx := context.Background()
	date := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	f.add(t, f.inbox, "a@b", "<same@x>", date, map[string]int{"r.pdf": 4000})
	f.add(t, f.archive, "a@b", "<same@x>", date, map[string]int{"r.pdf": 4000})
	got, err := f.db.QueryAttachments(ctx, AttachmentQuery{FolderIDs: []int64{f.inbox, f.archive}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].FolderID != f.inbox {
		t.Fatalf("got %+v, want the inbox copy only", got)
	}
}

// the browser has to stay fast on a large store: the plans must use the new
// indexes rather than sorting every attachment or scanning every message.
func TestAttachmentQueriesUseIndexes(t *testing.T) {
	f := newBrowserFixture(t)
	plan := func(query string, args ...any) string {
		rows, err := f.db.sql.Query("EXPLAIN QUERY PLAN "+query, args...)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var lines []string
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
				t.Fatal(err)
			}
			lines = append(lines, detail)
		}
		return strings.Join(lines, "\n")
	}
	q := AttachmentQuery{FolderIDs: []int64{f.inbox}}
	where, args := attachmentWhere(q)
	page := plan(`SELECT a.id FROM messages m INDEXED BY idx_messages_with_attachments
JOIN attachments a ON a.message_id = m.id WHERE `+where+` ORDER BY m.date DESC, m.id DESC, a.id LIMIT 10`, args...)
	if !strings.Contains(page, "idx_messages_with_attachments") {
		t.Errorf("page plan does not walk the date index:\n%s", page)
	}

	// INDEXED BY fails the statement outright when the index cannot serve it,
	// so this also guards the expression against drifting from the migration.
	sender := plan(`SELECT id FROM messages INDEXED BY idx_messages_sender WHERE `+senderMatch+` AND folder_id IN (?)`, "a@b", f.inbox)
	if !strings.Contains(sender, "idx_messages_sender") {
		t.Errorf("sender plan does not use the sender index:\n%s", sender)
	}
}

func TestMetadataOnlyAttachmentRestores(t *testing.T) {
	f := newBrowserFixture(t)
	ctx := context.Background()
	id, err := f.db.InsertMessageWithAttachments(ctx, &Message{
		AccountID: f.account, FolderID: f.inbox, UID: 900, MessageID: "<m@x>",
		Subject: "s", FromAddress: "a@b", Date: time.Now(),
	}, []IncomingAttachment{{Filename: "../evil/report.pdf", ContentType: "application/pdf", Size: 12345}})
	if err != nil {
		t.Fatal(err)
	}
	atts, err := f.db.ListAttachments(ctx, id)
	if err != nil || len(atts) != 1 {
		t.Fatalf("atts %v, %v", atts, err)
	}
	att := atts[0]
	if att.DiskPath != "" || att.SizeBytes != 12345 || att.Filename != "report.pdf" {
		t.Fatalf("metadata row = %+v", att)
	}
	if f.db.AttachmentOnDisk(att.DiskPath) {
		t.Fatal("a row without a file reads as on disk")
	}
	if _, err := f.db.OpenAttachment(ctx, att); err != ErrAttachmentNotLocal {
		t.Fatalf("open = %v, want ErrAttachmentNotLocal", err)
	}

	restored, err := f.db.RestoreAttachmentFile(ctx, f.account, att, strings.NewReader("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if !f.db.AttachmentOnDisk(restored.DiskPath) || restored.SizeBytes != 5 {
		t.Fatalf("restored = %+v", restored)
	}
	rc, err := f.db.OpenAttachment(ctx, *restored)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(rc)
	rc.Close()
	if string(data) != "hello" {
		t.Fatalf("data = %q", data)
	}
	entry, err := f.db.GetAttachmentEntry(ctx, att.ID)
	if err != nil || entry.DiskPath != restored.DiskPath || entry.Kind != KindPDF {
		t.Fatalf("entry = %+v, %v", entry, err)
	}
}

func TestSenderMessagesAndStats(t *testing.T) {
	f := newBrowserFixture(t)
	ctx := context.Background()
	base := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		f.add(t, f.inbox, "Ann <Ann@Example.com>", fmt.Sprintf("<s%d@x>", i), base.Add(time.Duration(i)*time.Hour), nil)
	}
	f.add(t, f.inbox, "annex@example.com", "<other@x>", base, nil)
	f.add(t, f.trash, "ann@example.com", "<t@x>", base.Add(10*time.Hour), nil)

	msgs, err := f.db.SenderMessages(ctx, "ann@example.com", []int64{f.inbox}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 || msgs[0].MessageID != "<s2@x>" {
		t.Fatalf("got %+v", msgs)
	}
	stats, err := f.db.SenderStatsFor(ctx, "ANN@example.com", []int64{f.inbox})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Count != 3 || !stats.First.Equal(base) || !stats.Last.Equal(base.Add(2*time.Hour)) {
		t.Fatalf("stats = %+v", stats)
	}
}
