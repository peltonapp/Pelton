package storage

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

// statsFixture is a local account with an inbox, a sent folder and a trash
// folder, and a helper that stores one message per call.
type statsFixture struct {
	t       *testing.T
	db      *DB
	ctx     context.Context
	account int64
	inbox   int64
	sent    int64
	trash   int64
	uid     uint32
}

func newStatsFixture(t *testing.T) *statsFixture {
	t.Helper()
	db := newTestDB(t)
	ctx := context.Background()
	acc, err := db.EnsureLocalAccount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	f := &statsFixture{t: t, db: db, ctx: ctx, account: acc.ID}
	for name, id := range map[string]*int64{"INBOX": &f.inbox, "Sent": &f.sent, "Trash": &f.trash} {
		folder, err := db.EnsureLocalFolder(ctx, acc.ID, name)
		if err != nil {
			t.Fatal(err)
		}
		*id = folder.ID
	}
	return f
}

// add stores m with the next uid and returns its id.
func (f *statsFixture) add(m Message) int64 {
	f.t.Helper()
	f.uid++
	m.AccountID = f.account
	if m.FolderID == 0 {
		m.FolderID = f.inbox
	}
	m.UID = f.uid
	if m.MessageID == "" {
		m.MessageID = fmt.Sprintf("<m%d@test>", f.uid)
	}
	id, err := f.db.InsertMessage(f.ctx, &m)
	if err != nil {
		f.t.Fatal(err)
	}
	return id
}

// plan returns sqlite's query plan for query, one detail per line.
func plan(t *testing.T, db *DB, query string, args []any) string {
	t.Helper()
	rows, err := db.sql.Query("EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, detail)
	}
	return strings.Join(lines, "\n")
}

var statsNow = time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)

func daysAgo(n int) time.Time {
	return statsNow.Add(-time.Duration(n) * 24 * time.Hour)
}

func TestNewsletterSendersAggregates(t *testing.T) {
	f := newStatsFixture(t)
	news := "<https://news.example/unsub>"
	f.add(Message{FromAddress: "Weekly News <news@example.com>", ListUnsubscribe: news, Date: daysAgo(40), SizeBytes: 100, Flags: FlagSeen})
	f.add(Message{FromAddress: "Weekly News <NEWS@example.com>", ListUnsubscribe: news, Date: daysAgo(10), SizeBytes: 200})
	latest := f.add(Message{FromAddress: "The Weekly <news@example.com>", ListUnsubscribe: news, ListUnsubscribePost: true,
		ListID: "<weekly.example.com>", Date: daysAgo(3), SizeBytes: 300})
	// left out: no List-Unsubscribe, awaiting expunge, or outside the folders asked for.
	f.add(Message{FromAddress: "news@example.com", Date: daysAgo(1), SizeBytes: 999})
	gone := f.add(Message{FromAddress: "news@example.com", ListUnsubscribe: news, Date: daysAgo(1), SizeBytes: 999})
	if err := f.db.MarkDeletePending(f.ctx, gone); err != nil {
		t.Fatal(err)
	}
	f.add(Message{FolderID: f.trash, FromAddress: "news@example.com", ListUnsubscribe: news, Date: daysAgo(1), SizeBytes: 999})
	f.add(Message{FromAddress: "shop@example.org", ListUnsubscribe: "<mailto:off@example.org>", Date: daysAgo(100), SizeBytes: 50})

	got, err := f.db.NewsletterSenders(f.ctx, []int64{f.inbox}, daysAgo(30))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d senders, want 2: %+v", len(got), got)
	}
	slices.SortFunc(got, func(a, b NewsletterSender) int { return strings.Compare(a.Email, b.Email) })
	n := got[0]
	if n.Email != "news@example.com" || n.Count != 3 || n.Unread != 2 || n.Bytes != 600 || n.Recent != 2 {
		t.Errorf("news sender = %+v", n)
	}
	if !n.First.Equal(daysAgo(40)) || !n.Last.Equal(daysAgo(3)) {
		t.Errorf("first/last = %v / %v", n.First, n.Last)
	}
	if n.LatestID != latest || n.LatestFromAddress != "The Weekly <news@example.com>" || n.ListID != "<weekly.example.com>" || !n.ListUnsubscribePost {
		t.Errorf("latest fields = %+v", n)
	}
	if got[1].Email != "shop@example.org" || got[1].Count != 1 || got[1].Recent != 0 {
		t.Errorf("shop sender = %+v", got[1])
	}
}

func TestNewsletterSendersEmpty(t *testing.T) {
	f := newStatsFixture(t)
	got, err := f.db.NewsletterSenders(f.ctx, nil, statsNow)
	if err != nil || got != nil {
		t.Fatalf("no folders: %v, %v", got, err)
	}
	got, err = f.db.NewsletterSenders(f.ctx, []int64{f.inbox}, statsNow)
	if err != nil || len(got) != 0 {
		t.Fatalf("empty mailbox: %v, %v", got, err)
	}
	ids, err := f.db.SenderMessageIDs(f.ctx, SenderMessagesQuery{Emails: []string{"a@b"}, FolderIDs: []int64{f.inbox}})
	if err != nil || len(ids) != 0 {
		t.Fatalf("empty mailbox ids: %v, %v", ids, err)
	}
}

func TestSenderMessageIDs(t *testing.T) {
	f := newStatsFixture(t)
	unsub := "<https://x.example/u>"
	oldest := f.add(Message{FromAddress: "a@example.com", ListUnsubscribe: unsub, Date: daysAgo(60)})
	middle := f.add(Message{FromAddress: "a@example.com", ListUnsubscribe: unsub, Date: daysAgo(20)})
	newest := f.add(Message{FromAddress: "A <a@example.com>", ListUnsubscribe: unsub, Date: daysAgo(1)})
	onlyB := f.add(Message{FromAddress: "b@example.com", ListUnsubscribe: unsub, Date: daysAgo(90)})
	f.add(Message{FromAddress: "a@example.com", Date: daysAgo(90)})

	cases := []struct {
		name string
		q    SenderMessagesQuery
		want []int64
	}{
		{"all of one sender", SenderMessagesQuery{Emails: []string{"A@Example.com"}}, []int64{newest, middle, oldest}},
		{"older than a month", SenderMessagesQuery{Emails: []string{"a@example.com", "b@example.com"}, Before: daysAgo(30)}, []int64{oldest, onlyB}},
		{"keep the latest", SenderMessagesQuery{Emails: []string{"a@example.com", "b@example.com"}, KeepLatest: true}, []int64{middle, oldest}},
	}
	for _, c := range cases {
		c.q.FolderIDs = []int64{f.inbox}
		got, err := f.db.SenderMessageIDs(f.ctx, c.q)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if fmt.Sprint(got) != fmt.Sprint(c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestStorageStatistics(t *testing.T) {
	f := newStatsFixture(t)
	f.add(Message{FromAddress: "Big <big@example.com>", SizeBytes: 5000, Date: daysAgo(2)})
	f.add(Message{FromAddress: "big@example.com", SizeBytes: 3000, Date: daysAgo(1)})
	f.add(Message{FromAddress: "small@example.com", SizeBytes: 10, Date: daysAgo(1)})
	f.add(Message{FolderID: f.sent, FromAddress: "me@example.com", SizeBytes: 70, Date: daysAgo(1)})
	gone := f.add(Message{FromAddress: "big@example.com", SizeBytes: 99999, Date: daysAgo(1)})
	if err := f.db.MarkDeletePending(f.ctx, gone); err != nil {
		t.Fatal(err)
	}

	usage, err := f.db.FolderUsage(f.ctx, []int64{f.inbox, f.sent, f.trash})
	if err != nil {
		t.Fatal(err)
	}
	byFolder := map[int64]FolderUsage{}
	for _, u := range usage {
		byFolder[u.FolderID] = u
	}
	if u := byFolder[f.inbox]; u.Count != 3 || u.Bytes != 8010 {
		t.Errorf("inbox usage = %+v", u)
	}
	if u := byFolder[f.sent]; u.Count != 1 || u.Bytes != 70 {
		t.Errorf("sent usage = %+v", u)
	}
	if _, ok := byFolder[f.trash]; ok {
		t.Errorf("empty trash listed: %+v", byFolder[f.trash])
	}

	senders, err := f.db.BiggestSenders(f.ctx, []int64{f.inbox, f.sent}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(senders) != 2 || senders[0].Email != "big@example.com" || senders[0].Bytes != 8000 || senders[0].Count != 2 {
		t.Fatalf("biggest senders = %+v", senders)
	}
	// the newest message has no display name, so the newest one with a name wins.
	if senders[0].FromAddress != "Big <big@example.com>" {
		t.Errorf("name comes from the newest named message, got %q", senders[0].FromAddress)
	}
	if senders[1].Email != "me@example.com" {
		t.Errorf("second biggest = %+v", senders[1])
	}
}

func TestBiggestAttachments(t *testing.T) {
	f := newStatsFixture(t)
	f.uid++
	m := Message{AccountID: f.account, FolderID: f.inbox, UID: f.uid, Subject: "photos", FromAddress: "a@example.com", Date: daysAgo(1)}
	id, err := f.db.InsertMessageWithAttachments(f.ctx, &m, []IncomingAttachment{
		{Filename: "small.txt", ContentType: "text/plain", Content: strings.NewReader("hi")},
		{Filename: "big.bin", ContentType: "application/octet-stream", Content: strings.NewReader(strings.Repeat("x", 4096))},
		{Filename: "photo.png", ContentType: "image/png", ContentID: "inline", Content: strings.NewReader(strings.Repeat("y", 9000))},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := f.db.BiggestAttachments(f.ctx, []int64{f.inbox}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Filename != "photo.png" || got[1].Filename != "big.bin" || got[1].Bytes != 4096 || got[1].MessageID != id || got[1].Subject != "photos" {
		t.Fatalf("biggest attachments = %+v", got)
	}
	none, err := f.db.BiggestAttachments(f.ctx, []int64{f.trash}, 10)
	if err != nil || len(none) != 0 {
		t.Fatalf("empty folder: %v, %v", none, err)
	}
}

func TestVolumeAndHourGrid(t *testing.T) {
	f := newStatsFixture(t)
	// 23:30 UTC is the next day an hour east of UTC.
	late := time.Date(2026, 11, 10, 23, 30, 0, 0, time.UTC)
	f.add(Message{FromAddress: "a@example.com", Date: late})
	f.add(Message{FromAddress: "b@example.com", Date: late.Add(-12 * time.Hour)})
	f.add(Message{FolderID: f.sent, FromAddress: "me@example.com", Date: late.Add(-11 * time.Hour)})
	f.add(Message{FolderID: f.trash, FromAddress: "c@example.com", Date: late})
	f.add(Message{FromAddress: "old@example.com", Date: late.Add(-400 * 24 * time.Hour)})

	scope := StatsScope{Counted: []int64{f.inbox, f.sent}, Sent: []int64{f.sent}, Since: late.Add(-30 * 24 * time.Hour), Offset: time.Hour}
	days, err := f.db.DailyVolume(f.ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	want := []DayVolume{{Day: "2026-11-10", Received: 1, Sent: 1}, {Day: "2026-11-11", Received: 1}}
	if fmt.Sprint(days) != fmt.Sprint(want) {
		t.Errorf("daily volume = %v, want %v", days, want)
	}

	grid, err := f.db.ReceivedByHour(f.ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	// 2026-11-11 is a Wednesday; 00:30 local. The other arrived Tuesday 12:30.
	wantGrid := []HourCount{{Weekday: 2, Hour: 12, Count: 1}, {Weekday: 3, Hour: 0, Count: 1}}
	if fmt.Sprint(grid) != fmt.Sprint(wantGrid) {
		t.Errorf("hour grid = %v, want %v", grid, wantGrid)
	}

	scope.Since = time.Time{}
	all, err := f.db.DailyVolume(f.ctx, scope)
	if err != nil || len(all) != 3 {
		t.Errorf("all time = %v, %v", all, err)
	}
}

func TestPeopleStatistics(t *testing.T) {
	f := newStatsFixture(t)
	for range 3 {
		f.add(Message{FromAddress: "Ada <ada@example.com>", Date: daysAgo(2)})
	}
	f.add(Message{FromAddress: "bob@example.com", Date: daysAgo(2)})
	f.add(Message{FromAddress: "me@example.com", Date: daysAgo(2)})
	for range 5 {
		f.add(Message{FromAddress: "list@example.com", ListUnsubscribe: "<mailto:x@y>", Date: daysAgo(2)})
	}
	f.add(Message{FolderID: f.sent, FromAddress: "me@example.com", ToAddresses: "Ada <ada@example.com>", CcAddresses: "bob@example.com", Date: daysAgo(1)})
	f.add(Message{FolderID: f.sent, FromAddress: "me@example.com", ToAddresses: "Ada <ada@example.com>", CcAddresses: "bob@example.com", Date: daysAgo(1)})
	f.add(Message{FolderID: f.sent, FromAddress: "me@example.com", ToAddresses: "ada@example.com", Date: daysAgo(1)})

	scope := StatsScope{Counted: []int64{f.inbox, f.sent}, Sent: []int64{f.sent}, Own: []string{"me@example.com"}, Since: daysAgo(30)}
	top, err := f.db.TopSenders(f.ctx, scope, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(top) != 2 || top[0].Email != "ada@example.com" || top[0].Count != 3 || top[0].FromAddress != "Ada <ada@example.com>" || top[1].Email != "bob@example.com" {
		t.Fatalf("top senders = %+v", top)
	}

	groups, err := f.db.SentRecipients(f.ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, g := range groups {
		counts[g.To+"|"+g.Cc] = g.Count
	}
	if counts["Ada <ada@example.com>|bob@example.com"] != 2 || counts["ada@example.com|"] != 1 || len(counts) != 2 {
		t.Errorf("sent recipients = %+v", groups)
	}

	empty := StatsScope{}
	if got, err := f.db.TopSenders(f.ctx, empty, 10); err != nil || got != nil {
		t.Errorf("empty scope top senders = %v, %v", got, err)
	}
	if got, err := f.db.SentRecipients(f.ctx, empty); err != nil || got != nil {
		t.Errorf("empty scope recipients = %v, %v", got, err)
	}
}

func TestReplyTimes(t *testing.T) {
	f := newStatsFixture(t)
	start := daysAgo(10)
	// thread one: they write, I answer after 2h, they answer after 6h.
	f.add(Message{MessageID: "<t1a@x>", FromAddress: "ada@example.com", Date: start})
	f.add(Message{MessageID: "<t1b@x>", InReplyTo: []string{"t1a@x"}, FolderID: f.sent, FromAddress: "me@example.com", Date: start.Add(2 * time.Hour)})
	f.add(Message{MessageID: "<t1c@x>", InReplyTo: []string{"t1b@x"}, FromAddress: "ada@example.com", Date: start.Add(8 * time.Hour)})
	// thread two: I answer after 4h; a second message of mine in a row is not a reply.
	f.add(Message{MessageID: "<t2a@x>", FromAddress: "bob@example.com", Date: start})
	f.add(Message{MessageID: "<t2b@x>", InReplyTo: []string{"t2a@x"}, FolderID: f.sent, FromAddress: "me@example.com", Date: start.Add(4 * time.Hour)})
	f.add(Message{MessageID: "<t2c@x>", InReplyTo: []string{"t2b@x"}, FolderID: f.sent, FromAddress: "me@example.com", Date: start.Add(5 * time.Hour)})
	// thread three: an answer two months later is a new start, not a reply.
	f.add(Message{MessageID: "<t3a@x>", FromAddress: "cy@example.com", Date: daysAgo(90)})
	f.add(Message{MessageID: "<t3b@x>", InReplyTo: []string{"t3a@x"}, FolderID: f.sent, FromAddress: "me@example.com", Date: daysAgo(20)})

	scope := StatsScope{Counted: []int64{f.inbox, f.sent}, Sent: []int64{f.sent}, Own: []string{"me@example.com"}, Since: daysAgo(30)}
	got, err := f.db.ReplyTimes(f.ctx, scope, 30*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if got.Mine.N != 2 || got.Mine.Median != 3*time.Hour {
		t.Errorf("mine = %+v, want 2 replies with a 3h median", got.Mine)
	}
	if got.Theirs.N != 1 || got.Theirs.Median != 6*time.Hour {
		t.Errorf("theirs = %+v, want 1 reply of 6h", got.Theirs)
	}

	empty, err := f.db.ReplyTimes(f.ctx, StatsScope{Counted: []int64{f.trash}}, time.Hour)
	if err != nil || empty != (ReplyTimes{}) {
		t.Errorf("empty folder = %+v, %v", empty, err)
	}
}

func TestStatisticsOnEmptyMailbox(t *testing.T) {
	f := newStatsFixture(t)
	scope := StatsScope{Counted: []int64{f.inbox, f.sent}, Sent: []int64{f.sent}, Own: []string{"me@example.com"}}
	if got, err := f.db.FolderUsage(f.ctx, scope.Counted); err != nil || len(got) != 0 {
		t.Errorf("folder usage = %v, %v", got, err)
	}
	if got, err := f.db.BiggestSenders(f.ctx, scope.Counted, 5); err != nil || len(got) != 0 {
		t.Errorf("biggest senders = %v, %v", got, err)
	}
	if got, err := f.db.DailyVolume(f.ctx, scope); err != nil || len(got) != 0 {
		t.Errorf("daily volume = %v, %v", got, err)
	}
	if got, err := f.db.ReceivedByHour(f.ctx, scope); err != nil || len(got) != 0 {
		t.Errorf("hour grid = %v, %v", got, err)
	}
	if got, err := f.db.TopSenders(f.ctx, scope, 5); err != nil || len(got) != 0 {
		t.Errorf("top senders = %v, %v", got, err)
	}
	if got, err := f.db.ReplyTimes(f.ctx, scope, time.Hour); err != nil || got != (ReplyTimes{}) {
		t.Errorf("reply times = %v, %v", got, err)
	}
	if got, err := f.db.FolderUsage(f.ctx, nil); err != nil || got != nil {
		t.Errorf("no folders = %v, %v", got, err)
	}
}

// The aggregates must read an index that holds every column they touch, and
// the per sender ones must walk it in sender order rather than sorting, or a
// mailbox of a few hundred thousand messages makes the screens crawl.
func TestStatisticsQueryPlans(t *testing.T) {
	f := newStatsFixture(t)
	scope := StatsScope{Counted: []int64{f.inbox, f.sent}, Sent: []int64{f.sent}, Own: []string{"me@example.com"}, Since: daysAgo(30)}
	folders := scope.Counted

	type planCase struct {
		name    string
		query   string
		args    []any
		want    []string
		without []string
	}
	q := func(query string, args []any) (string, []any) { return query, args }
	cases := []planCase{}
	add := func(name string, query string, args []any, want []string, without ...string) {
		cases = append(cases, planCase{name, query, args, want, without})
	}
	query, args := q(newsletterSendersSQL(folders, daysAgo(90)))
	add("newsletter senders", query, args, []string{"COVERING INDEX idx_messages_newsletters", "SEARCH m USING INTEGER PRIMARY KEY"}, "TEMP B-TREE FOR GROUP BY")
	query, args = q(senderMessageIDsSQL(SenderMessagesQuery{Emails: []string{"a@b"}, FolderIDs: folders}))
	add("sender message ids", query, args, []string{"COVERING INDEX idx_messages_newsletters"})
	query, args = q(folderUsageSQL(folders))
	add("folder usage", query, args, []string{"COVERING INDEX idx_messages_date"})
	query, args = q(biggestSendersSQL(folders, 10))
	add("biggest senders", query, args, []string{"COVERING INDEX idx_messages_newsletters", "COVERING INDEX idx_messages_people"})
	query, args = q(topSendersSQL(scope, 10))
	add("top senders", query, args, []string{"COVERING INDEX idx_messages_people"}, "TEMP B-TREE FOR GROUP BY")
	add("sender name", senderNameSQL, []any{"a@b"}, []string{"INDEX idx_messages_newsletters", "INDEX idx_messages_people"})
	query, args = q(biggestAttachmentsSQL(folders, 10))
	add("biggest attachments", query, args, []string{"idx_attachments_size"}, "TEMP B-TREE")
	query, args = q(dailyVolumeSQL(scope))
	add("daily volume", query, args, []string{"COVERING INDEX idx_messages_date"})
	query, args = q(receivedByHourSQL(scope))
	add("hour grid", query, args, []string{"COVERING INDEX idx_messages_date"})
	query, args = q(sentRecipientsSQL(scope))
	add("sent recipients", query, args, []string{"idx_messages_list"})
	query, args = q(replyTimesSQL(scope, time.Hour))
	add("reply times", query, args, []string{"COVERING INDEX idx_messages_thread_date"})

	for _, c := range cases {
		p := plan(t, f.db, c.query, c.args)
		for _, w := range c.want {
			if !strings.Contains(p, w) {
				t.Errorf("%s: plan lacks %q:\n%s", c.name, w, p)
			}
		}
		for _, w := range c.without {
			if strings.Contains(p, w) {
				t.Errorf("%s: plan has %q:\n%s", c.name, w, p)
			}
		}
	}
}
