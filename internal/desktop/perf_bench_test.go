package desktop

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/search"
	"github.com/peltonapp/Pelton/internal/storage"
)

// The benchmarks here run the hot bindings against one synthetic store built
// on first use: 3 accounts with 17 folders each, 200k messages threaded through
// the real insert path, 5k contacts, 20k calendar events, 2k vault notes and
// 50k feed entries. PELTON_BENCH_MESSAGES scales the mail down for a quick run.
//
//	go test -tags goolm -run '^$' -bench . -benchtime 2s ./internal/desktop/

var (
	benchOnce sync.Once
	benchApp  *App
	benchErr  error
	benchDir  string
	benchSeed benchFixture
)

type benchFixture struct {
	inbox     int64
	archive   int64
	threadID  int64
	messageID int64
	calendar  int64
}

const (
	benchAccounts     = 3
	benchContacts     = 5000
	benchEvents       = 20000
	benchVaultNotes   = 2000
	benchFeeds        = 50
	benchFeedEntries  = 50000
	benchSearchDocs   = 20000
	benchPageSize     = 100
	benchDefaultCount = 200000
)

var benchFolderNames = []string{"INBOX", "Sent", "Drafts", "Archive", "Junk", "Trash",
	"Work", "Family", "Receipts", "Travel", "Lists", "Projects", "Old", "Bills", "School", "Friends", "Misc"}

// benchFolderWeights is how mail spreads over benchFolderNames.
var benchFolderWeights = []int{30, 10, 1, 30, 3, 5, 4, 2, 2, 2, 3, 2, 2, 1, 1, 1, 1}

func benchMessageCount() int {
	if v, err := strconv.Atoi(os.Getenv("PELTON_BENCH_MESSAGES")); err == nil && v > 0 {
		return v
	}
	return benchDefaultCount
}

// removeBenchData deletes the synthetic store, if a benchmark built one.
func removeBenchData() {
	if benchApp != nil {
		if benchApp.index != nil {
			_ = benchApp.index.Close()
		}
		_ = benchApp.store.Close()
	}
	if benchDir != "" {
		_ = os.RemoveAll(benchDir)
	}
}

func benchStore(b *testing.B) (*App, benchFixture) {
	b.Helper()
	benchOnce.Do(func() {
		start := time.Now()
		benchApp, benchErr = seedBenchStore(context.Background())
		if benchErr == nil {
			fmt.Fprintf(os.Stderr, "bench store seeded in %s\n", time.Since(start).Round(time.Millisecond))
		}
	})
	if benchErr != nil {
		b.Fatalf("seed: %v", benchErr)
	}
	return benchApp, benchSeed
}

const benchWords = "the quick brown fox jumps over a lazy dog while project budget meeting invoice travel report weekly update release notes review draft plan offsite agenda quarterly results feedback design proposal schedule contract renewal"

func benchText(r *rand.Rand, words int) string {
	vocab := strings.Fields(benchWords)
	var sb strings.Builder
	for i := 0; i < words; i++ {
		if i > 0 {
			if i%14 == 0 {
				sb.WriteString(".\n\n")
			} else {
				sb.WriteByte(' ')
			}
		}
		sb.WriteString(vocab[r.IntN(len(vocab))])
	}
	return sb.String()
}

func benchHTML(text string) string {
	var sb strings.Builder
	sb.WriteString(`<html><head><style>p{margin:0}</style></head><body><table width="100%"><tr><td>`)
	for _, para := range strings.Split(text, "\n\n") {
		sb.WriteString(`<p style="font-family:Arial,sans-serif;font-size:14px;color:#333333">`)
		sb.WriteString(para)
		sb.WriteString(`</p>`)
	}
	sb.WriteString(`<img src="https://example.invalid/pixel.gif" width="1" height="1"></td></tr></table></body></html>`)
	return sb.String()
}

func seedBenchStore(ctx context.Context) (*App, error) {
	dir, err := os.MkdirTemp("", "pelton-bench-")
	if err != nil {
		return nil, err
	}
	benchDir = dir
	db, err := storage.Open(filepath.Join(dir, "bench.db"))
	if err != nil {
		return nil, err
	}
	if err := db.RunMigrations(ctx); err != nil {
		return nil, err
	}
	app := &App{ctx: ctx, store: db, log: slog.New(slog.DiscardHandler)}

	h := db.Handle()
	h.SetMaxOpenConns(1)
	if _, err := h.ExecContext(ctx, `PRAGMA synchronous=OFF`); err != nil {
		return nil, err
	}
	r := rand.New(rand.NewPCG(1, 2))
	if err := seedBenchMail(ctx, db, r); err != nil {
		return nil, err
	}
	if err := seedBenchContacts(ctx, db, r); err != nil {
		return nil, err
	}
	if err := seedBenchCalendar(ctx, db, r); err != nil {
		return nil, err
	}
	if err := seedBenchFeeds(ctx, db, r); err != nil {
		return nil, err
	}
	if err := seedBenchVault(ctx, db, r); err != nil {
		return nil, err
	}
	if _, err := h.ExecContext(ctx, `PRAGMA synchronous=FULL`); err != nil {
		return nil, err
	}
	if _, err := h.ExecContext(ctx, `ANALYZE`); err != nil {
		return nil, err
	}
	h.SetMaxOpenConns(8)

	idx, err := search.Open(filepath.Join(dir, "search.bleve"))
	if err != nil {
		return nil, err
	}
	app.index = idx
	msgs, err := db.ListMessagesForIndex(ctx, 0, benchSearchDocs)
	if err != nil {
		return nil, err
	}
	docs := make([]search.Doc, 0, len(msgs))
	for _, m := range msgs {
		doc := toSearchDoc(m)
		doc.Body = app.searchBody(m)
		docs = append(docs, doc)
	}
	for start := 0; start < len(docs); start += 1000 {
		if err := idx.IndexBatch(docs[start:min(start+1000, len(docs))]); err != nil {
			return nil, err
		}
	}
	return app, nil
}

func seedBenchMail(ctx context.Context, db *storage.DB, r *rand.Rand) error {
	type folderRef struct {
		id      int64
		account int64
	}
	var folders []folderRef
	var weights []int
	for a := 0; a < benchAccounts; a++ {
		accountID, err := db.CreateAccount(ctx, &storage.Account{Email: fmt.Sprintf("me%d@example.com", a)})
		if err != nil {
			return err
		}
		for i, name := range benchFolderNames {
			f := &storage.Folder{AccountID: accountID, Name: name, IMAPPath: name, Delimiter: "/"}
			id, err := db.CreateFolder(ctx, f)
			if err != nil {
				return err
			}
			folders = append(folders, folderRef{id, accountID})
			weights = append(weights, benchFolderWeights[i])
			if a == 0 && name == "INBOX" {
				benchSeed.inbox = id
			}
			if a == 0 && name == "Archive" {
				benchSeed.archive = id
			}
		}
	}
	total := 0
	for _, w := range weights {
		total += w
	}
	pick := func() folderRef {
		n := r.IntN(total)
		for i, w := range weights {
			if n < w {
				return folders[i]
			}
			n -= w
		}
		return folders[len(folders)-1]
	}

	senders := make([]string, 800)
	for i := range senders {
		senders[i] = fmt.Sprintf("sender%d@domain%d.example", i, i%60)
	}
	count := benchMessageCount()
	base := time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)
	span := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC).Sub(base)
	recent := map[int64][]string{}
	uids := map[int64]uint32{}
	for i := 0; i < count; i++ {
		f := pick()
		uids[f.id]++
		m := &storage.Message{
			AccountID:   f.account,
			FolderID:    f.id,
			UID:         uids[f.id],
			MessageID:   fmt.Sprintf("<m%d@bench.example>", i),
			FromAddress: senders[r.IntN(len(senders))],
			FromName:    "Sender " + strconv.Itoa(r.IntN(800)),
			ToAddresses: fmt.Sprintf("me%d@example.com", f.account),
			Date:        base.Add(time.Duration(float64(span) * float64(i) / float64(count))),
			SizeBytes:   int64(4000 + r.IntN(60000)),
		}
		if r.IntN(100) < 85 {
			m.Flags |= storage.FlagSeen
		}
		if r.IntN(100) < 3 {
			m.Flags |= storage.FlagFlagged
		}
		pool := recent[f.account]
		if len(pool) > 0 && r.IntN(100) < 40 {
			parent := pool[r.IntN(len(pool))]
			m.InReplyTo = []string{parent}
			m.References = []string{parent}
			m.Subject = "Re: thread " + parent
		} else {
			m.Subject = benchText(r, 4+r.IntN(6))
		}
		if len(pool) < 400 {
			pool = append(pool, m.MessageID)
		} else {
			pool[r.IntN(len(pool))] = m.MessageID
		}
		recent[f.account] = pool
		text := benchText(r, 150+r.IntN(500))
		switch n := r.IntN(100); {
		case n < 15:
			m.BodyHTML = benchHTML(text)
			m.ListUnsubscribe = "<https://example.invalid/unsub>"
		case n < 75:
			m.BodyPlain = text
			m.BodyHTML = benchHTML(text)
		default:
			m.BodyPlain = text
		}
		id, err := db.InsertMessage(ctx, m)
		if err != nil {
			return err
		}
		if f.id == benchSeed.inbox {
			benchSeed.messageID = id
		}
	}
	var threadID int64
	err := db.Handle().QueryRowContext(ctx,
		`SELECT thread_id FROM messages WHERE thread_id != 0 GROUP BY thread_id ORDER BY COUNT(*) DESC LIMIT 1`).Scan(&threadID)
	benchSeed.threadID = threadID
	return err
}

func seedBenchContacts(ctx context.Context, db *storage.DB, r *rand.Rand) error {
	bookID, err := db.CreateAddressBook(ctx, &storage.AddressBook{Name: "Contacts"})
	if err != nil {
		return err
	}
	for i := 0; i < benchContacts; i++ {
		c := &storage.Contact{
			BookID:   bookID,
			Path:     fmt.Sprintf("/c/%d.vcf", i),
			UID:      fmt.Sprintf("c-%d", i),
			FullName: fmt.Sprintf("Person %d %s", i, strings.Fields(benchWords)[r.IntN(30)]),
			Emails:   []storage.ContactValue{{Value: fmt.Sprintf("person%d@corp%d.example", i, i%40), Label: "work"}},
			Phones:   []storage.ContactValue{{Value: fmt.Sprintf("+49 30 %07d", i), Label: "cell"}},
		}
		if err := db.SaveContact(ctx, c); err != nil {
			return err
		}
	}
	return nil
}

func seedBenchCalendar(ctx context.Context, db *storage.DB, r *rand.Rand) error {
	calID, err := db.CreateCalendar(ctx, &storage.Calendar{Path: "local", Name: "Personal", Visible: true, Components: "VEVENT"})
	if err != nil {
		return err
	}
	benchSeed.calendar = calID
	base := time.Date(2022, 1, 1, 8, 0, 0, 0, time.UTC)
	for i := 0; i < benchEvents; i++ {
		start := base.Add(time.Duration(i) * 2 * time.Hour).Add(time.Duration(r.IntN(60)) * time.Minute)
		end := start.Add(time.Duration(30+r.IntN(90)) * time.Minute)
		uid := fmt.Sprintf("ev-%d", i)
		rrule := ""
		rangeEnd := end
		if i%200 == 0 {
			rrule = "RRULE:FREQ=WEEKLY\r\n"
			rangeEnd = time.Time{}
		}
		data := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:bench\r\nBEGIN:VEVENT\r\nUID:" + uid +
			"\r\nDTSTAMP:20220101T000000Z\r\nDTSTART:" + start.Format("20060102T150405Z") +
			"\r\nDTEND:" + end.Format("20060102T150405Z") + "\r\n" + rrule +
			"SUMMARY:" + benchText(r, 4) + "\r\nLOCATION:Room " + strconv.Itoa(i%30) + "\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
		ev := &storage.CalendarEvent{
			CalendarID: calID, UID: uid, Path: uid + ".ics", Data: data,
			Summary: "event", RangeStart: start, RangeEnd: rangeEnd, Recurring: rrule != "", Kind: storage.KindEvent,
		}
		if err := db.SaveCalendarEvent(ctx, ev); err != nil {
			return err
		}
	}
	return nil
}

func seedBenchFeeds(ctx context.Context, db *storage.DB, r *rand.Rand) error {
	now := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	per := benchFeedEntries / benchFeeds
	for f := 0; f < benchFeeds; f++ {
		feed := &storage.Feed{URL: fmt.Sprintf("https://feed%d.example.invalid/rss", f), Title: fmt.Sprintf("Feed %d", f), IntervalMinutes: 60}
		if err := db.CreateFeed(ctx, feed); err != nil {
			return err
		}
		entries := make([]storage.FeedEntry, 0, per)
		for i := 0; i < per; i++ {
			text := benchText(r, 80+r.IntN(200))
			entries = append(entries, storage.FeedEntry{
				GUID:       fmt.Sprintf("f%d-e%d", f, i),
				URL:        fmt.Sprintf("https://feed%d.example.invalid/e/%d", f, i),
				Title:      benchText(r, 6),
				Summary:    text[:min(len(text), 200)],
				Content:    benchHTML(text),
				SearchText: text,
				Snippet:    text[:min(len(text), 140)],
				Published:  now.Add(-time.Duration(f*per+i) * 17 * time.Minute),
				Read:       r.IntN(100) < 70,
			})
		}
		if _, err := db.UpsertFeedEntries(ctx, feed.ID, entries, now); err != nil {
			return err
		}
	}
	return nil
}

func seedBenchVault(ctx context.Context, db *storage.DB, r *rand.Rand) error {
	notes := make([]storage.VaultNote, 0, benchVaultNotes)
	for i := 0; i < benchVaultNotes; i++ {
		n := storage.VaultNote{
			Path:     fmt.Sprintf("folder%d/note%d.md", i%20, i),
			Title:    fmt.Sprintf("Note %d", i),
			MTime:    int64(1700000000 + i),
			Size:     int64(500 + r.IntN(4000)),
			Hash:     strconv.Itoa(i),
			Headings: []storage.VaultHeading{{Level: 1, Text: "Heading", Line: 1}, {Level: 2, Text: "Details", Line: 8}},
			Tags:     []string{"tag" + strconv.Itoa(i%15)},
			Snippet:  benchText(r, 20),
		}
		for l := 0; l < 5; l++ {
			n.Links = append(n.Links, storage.VaultLink{Kind: "wiki", Target: fmt.Sprintf("note%d", r.IntN(benchVaultNotes)), Line: 3 + l})
		}
		notes = append(notes, n)
	}
	return db.SaveVaultNotes(ctx, "bench", notes)
}

func BenchmarkListMessagesFolder(b *testing.B) {
	app, fx := benchStore(b)
	b.ResetTimer()
	for b.Loop() {
		if _, err := app.ListMessages(ListMessagesRequest{Kind: "folder", FolderID: fx.archive, Limit: benchPageSize}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkListMessagesFolderDeepPage(b *testing.B) {
	app, fx := benchStore(b)
	b.ResetTimer()
	for b.Loop() {
		if _, err := app.ListMessages(ListMessagesRequest{Kind: "folder", FolderID: fx.archive, Limit: benchPageSize, Offset: 5000}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkListMessagesUnifiedInbox(b *testing.B) {
	app, _ := benchStore(b)
	b.ResetTimer()
	for b.Loop() {
		if _, err := app.ListMessages(ListMessagesRequest{Kind: "view", View: viewInbox, Limit: benchPageSize}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkListThreadsInbox(b *testing.B) {
	app, fx := benchStore(b)
	b.ResetTimer()
	for b.Loop() {
		if _, err := app.ListThreads(ListMessagesRequest{Kind: "folder", FolderID: fx.inbox, Limit: benchPageSize}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkListThreadsUnifiedInbox(b *testing.B) {
	app, _ := benchStore(b)
	b.ResetTimer()
	for b.Loop() {
		if _, err := app.ListThreads(ListMessagesRequest{Kind: "view", View: viewInbox, Limit: benchPageSize}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGetThread(b *testing.B) {
	app, fx := benchStore(b)
	b.ResetTimer()
	for b.Loop() {
		if _, err := app.GetThread(fx.threadID); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGetMessage(b *testing.B) {
	app, fx := benchStore(b)
	b.ResetTimer()
	for b.Loop() {
		if _, err := app.GetMessage(fx.messageID); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSearch(b *testing.B) {
	app, _ := benchStore(b)
	b.ResetTimer()
	for b.Loop() {
		if _, err := app.Search(SearchRequestDTO{Query: "budget", Limit: benchPageSize}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSidebarFolders(b *testing.B) {
	app, _ := benchStore(b)
	accounts, err := app.store.ListAccounts(app.ctx)
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for b.Loop() {
		for _, acc := range accounts {
			if _, err := app.ListFolders(acc.ID); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkUnifiedViewCounts(b *testing.B) {
	app, _ := benchStore(b)
	b.ResetTimer()
	for b.Loop() {
		if _, err := app.ListUnifiedViews(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCalendarMonth(b *testing.B) {
	app, _ := benchStore(b)
	from := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)
	to := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)
	b.ResetTimer()
	for b.Loop() {
		if _, err := app.ListOccurrences(from, to, nil); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCalendarYear(b *testing.B) {
	app, _ := benchStore(b)
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)
	to := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)
	b.ResetTimer()
	for b.Loop() {
		if _, err := app.ListOccurrences(from, to, nil); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkContactSearch(b *testing.B) {
	app, _ := benchStore(b)
	b.ResetTimer()
	for b.Loop() {
		if _, err := app.SearchAddresses("pers", 8); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFeedListAll(b *testing.B) {
	app, _ := benchStore(b)
	b.ResetTimer()
	for b.Loop() {
		if _, err := app.ListFeedEntries(FeedScopeDTO{Kind: "all"}, 100, 0); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFeedListUnread(b *testing.B) {
	app, _ := benchStore(b)
	b.ResetTimer()
	for b.Loop() {
		if _, err := app.ListFeedEntries(FeedScopeDTO{Kind: "unread"}, 100, 0); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFeedSidebar(b *testing.B) {
	app, _ := benchStore(b)
	b.ResetTimer()
	for b.Loop() {
		if _, err := app.ListFeedSidebar(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkListingIndexMigration is what migration 0106 costs on the synthetic
// store: its indexes are dropped and the migration's sql run again. Run it with
// -benchtime 1x.
func BenchmarkListingIndexMigration(b *testing.B) {
	app, _ := benchStore(b)
	migration, err := os.ReadFile(filepath.Join("..", "storage", "migrations", "0106_listing_indexes.sql"))
	if err != nil {
		b.Fatal(err)
	}
	h := app.store.Handle()
	b.ResetTimer()
	for b.Loop() {
		b.StopTimer()
		for _, name := range []string{"idx_messages_listing", "idx_messages_copies", "idx_feed_entries_unread_list"} {
			if _, err := h.ExecContext(app.ctx, `DROP INDEX IF EXISTS `+name); err != nil {
				b.Fatal(err)
			}
		}
		b.StartTimer()
		if _, err := h.ExecContext(app.ctx, string(migration)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkVaultIndexLoad(b *testing.B) {
	app, _ := benchStore(b)
	b.ResetTimer()
	for b.Loop() {
		if _, err := app.store.LoadVaultNotes(app.ctx, "bench"); err != nil {
			b.Fatal(err)
		}
	}
}
