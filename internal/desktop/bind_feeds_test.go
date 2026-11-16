package desktop

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/storage"
)

// feedServer is a site with a feed whose items a test can change.
type feedServer struct {
	*httptest.Server
	mu        sync.Mutex
	items     []string
	feedHits  atomic.Int32
	notModded atomic.Int32
	status    int
	retry     string
}

func newFeedServer(t *testing.T) *feedServer {
	t.Helper()
	fs := &feedServer{}
	fs.Server = httptest.NewServer(http.HandlerFunc(fs.serve))
	t.Cleanup(fs.Close)
	return fs
}

func (fs *feedServer) setItems(items ...string) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.items = items
}

func (fs *feedServer) serve(w http.ResponseWriter, r *http.Request) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	switch r.URL.Path {
	case "/":
		_, _ = fmt.Fprintf(w, `<html><head><link rel="alternate" type="application/rss+xml" title="Site feed" href="/feed.xml"><link rel="icon" href="/icon.png"></head><body></body></html>`)
	case "/icon.png":
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("\x89PNG\r\n\x1a\nicon"))
	case "/feed.xml":
		fs.feedHits.Add(1)
		if fs.status != 0 {
			if fs.retry != "" {
				w.Header().Set("Retry-After", fs.retry)
			}
			w.WriteHeader(fs.status)
			return
		}
		etag := fmt.Sprintf(`"%d"`, len(fs.items))
		if r.Header.Get("If-None-Match") == etag {
			fs.notModded.Add(1)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		w.Header().Set("Content-Type", "application/rss+xml")
		var b strings.Builder
		b.WriteString(`<?xml version="1.0"?><rss version="2.0"><channel><title>Test site</title><link>` + fs.URL + `/</link>`)
		for i, it := range fs.items {
			fmt.Fprintf(&b, `<item><title>%s</title><link>/posts/%d</link><guid>id-%d</guid><pubDate>%s</pubDate><description><![CDATA[%s]]></description></item>`,
				it, i, i, time.Now().Add(-time.Duration(i)*time.Hour).UTC().Format(time.RFC1123Z), "<p>Body of "+it+`</p><img src="/pic.png"><img src="https://feeds.feedburner.com/~r/x/~4/1"><script>alert(1)</script>`)
		}
		b.WriteString(`</channel></rss>`)
		_, _ = w.Write([]byte(b.String()))
	default:
		http.NotFound(w, r)
	}
}

func entryTitlesOf(list []FeedEntryDTO) string {
	out := make([]string, len(list))
	for i, e := range list {
		out[i] = e.Title
	}
	return strings.Join(out, ",")
}

func TestFeedSubscribeRefreshAndDedupe(t *testing.T) {
	a := newCalendarTestApp(t)
	srv := newFeedServer(t)
	srv.setItems("One", "Two")

	feed, err := a.AddFeed(srv.URL+"/feed.xml", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if feed.Title != "Test site" || feed.Unread != 2 || feed.SiteURL != srv.URL+"/" {
		t.Fatalf("added feed = %+v", feed)
	}
	if _, err := a.AddFeed(srv.URL+"/feed.xml", 0, ""); err == nil {
		t.Error("the same feed was added twice")
	}

	list, err := a.ListFeedEntries(FeedScopeDTO{Kind: "unread"}, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if entryTitlesOf(list) != "One,Two" || list[0].URL != srv.URL+"/posts/0" {
		t.Fatalf("entries = %+v", list)
	}

	stored, _ := a.store.GetFeed(a.ctx, feed.ID)
	if _, err := a.refreshFeed(a.ctx, *stored, a.feedFetcher()); err != nil {
		t.Fatal(err)
	}
	if srv.notModded.Load() != 1 {
		t.Errorf("unchanged feed was not revalidated with its etag: %d", srv.notModded.Load())
	}

	srv.setItems("One", "Two", "Three")
	stored, _ = a.store.GetFeed(a.ctx, feed.ID)
	added, err := a.refreshFeed(a.ctx, *stored, a.feedFetcher())
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 1 {
		t.Errorf("third fetch added %d entries, want 1", len(added))
	}
	side, err := a.ListFeedSidebar()
	if err != nil {
		t.Fatal(err)
	}
	if side.Counts.Unread != 3 || side.Feeds[0].Unread != 3 || side.Feeds[0].Error != "" {
		t.Errorf("sidebar = %+v", side)
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		got, _ := a.store.GetFeed(a.ctx, feed.ID)
		if strings.HasPrefix(got.Icon, "data:image/png;base64,") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("icon = %q", got.Icon)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestFeedEntryRendering(t *testing.T) {
	a := newCalendarTestApp(t)
	srv := newFeedServer(t)
	srv.setItems("Pictures")
	feed, err := a.AddFeed(srv.URL+"/feed.xml", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	list, _ := a.ListFeedEntries(FeedScopeDTO{Kind: "feed", ID: feed.ID}, 10, 0)
	if len(list) != 1 {
		t.Fatalf("entries = %+v", list)
	}

	blocked, err := a.GetFeedEntry(list[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if blocked.RemoteAllowed || !blocked.HasRemoteContent {
		t.Errorf("remote = %v allowed %v", blocked.HasRemoteContent, blocked.RemoteAllowed)
	}
	for _, bad := range []string{"<script", "alert(", "pic.png", "feedburner"} {
		if strings.Contains(blocked.BodyHTMLSafe, bad) {
			t.Errorf("blocked body keeps %q: %s", bad, blocked.BodyHTMLSafe)
		}
	}
	if !strings.Contains(blocked.BodyHTMLSafe, "Body of Pictures") {
		t.Errorf("body = %s", blocked.BodyHTMLSafe)
	}

	if err := a.UpdateFeed(FeedSettingsDTO{ID: feed.ID, LoadImages: true}); err != nil {
		t.Fatal(err)
	}
	shown, _ := a.GetFeedEntry(list[0].ID)
	if !shown.RemoteAllowed || !strings.Contains(shown.BodyHTMLSafe, srv.URL+"/pic.png") {
		t.Errorf("allowed body = %s", shown.BodyHTMLSafe)
	}
	if strings.Contains(shown.BodyHTMLSafe, "feedburner") || strings.Contains(shown.BodyHTMLSafe, "<script") {
		t.Errorf("allowed body keeps a pixel or script: %s", shown.BodyHTMLSafe)
	}
	once, err := a.GetFeedEntryHTML(list[0].ID, true, false)
	if err != nil || !strings.Contains(once, "pic.png") {
		t.Errorf("load once = %q, %v", once, err)
	}
}

func TestFeedRefreshFailures(t *testing.T) {
	a := newCalendarTestApp(t)
	srv := newFeedServer(t)
	srv.setItems("One")
	feed, err := a.AddFeed(srv.URL+"/feed.xml", 0, "")
	if err != nil {
		t.Fatal(err)
	}

	srv.mu.Lock()
	srv.status, srv.retry = http.StatusTooManyRequests, "7200"
	srv.mu.Unlock()
	stored, _ := a.store.GetFeed(a.ctx, feed.ID)
	before := time.Now()
	if _, err := a.refreshFeed(a.ctx, *stored, a.feedFetcher()); err == nil {
		t.Fatal("429 was not an error")
	}
	got, _ := a.store.GetFeed(a.ctx, feed.ID)
	if !strings.Contains(got.LastError, "slow down") || got.ErrorCount != 1 {
		t.Errorf("error = %q (%d)", got.LastError, got.ErrorCount)
	}
	if got.NextFetchAt.Before(before.Add(2*time.Hour - time.Minute)) {
		t.Errorf("Retry-After ignored: next fetch %v", got.NextFetchAt)
	}
	if due, _ := a.dueFeeds(); len(due) != 0 {
		t.Errorf("a feed told to wait is due: %+v", due)
	}

	srv.mu.Lock()
	srv.status, srv.retry = http.StatusNotFound, ""
	srv.mu.Unlock()
	if _, err := a.refreshFeed(a.ctx, *got, a.feedFetcher()); err == nil {
		t.Fatal("404 was not an error")
	}
	got, _ = a.store.GetFeed(a.ctx, feed.ID)
	if !strings.Contains(got.LastError, "not found") || got.ErrorCount != 2 {
		t.Errorf("error = %q (%d)", got.LastError, got.ErrorCount)
	}
	if d := feedBackoff(3, 30*time.Minute, nil); d != 2*time.Hour {
		t.Errorf("backoff after three failures = %v", d)
	}
	if d := feedBackoff(20, 30*time.Minute, nil); d != feedMaxBackoff {
		t.Errorf("backoff cap = %v", d)
	}
}

func TestDiscoverFeeds(t *testing.T) {
	a := newCalendarTestApp(t)
	srv := newFeedServer(t)
	srv.setItems("One", "Two", "Three", "Four")

	found, err := a.DiscoverFeeds(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].URL != srv.URL+"/feed.xml" || found[0].EntryCount != 4 || len(found[0].Latest) != 3 {
		t.Fatalf("from the page = %+v", found)
	}
	direct, err := a.DiscoverFeeds(srv.URL + "/feed.xml")
	if err != nil || len(direct) != 1 || direct[0].Title != "Test site" {
		t.Fatalf("direct = %+v, %v", direct, err)
	}
	if _, err := a.DiscoverFeeds(srv.URL + "/nothing"); err == nil {
		t.Error("a missing page found a feed")
	}
	if _, err := normalizeFeedAddress("ftp://x"); err == nil {
		t.Error("ftp accepted")
	}
	if got, _ := normalizeFeedAddress("feed://example.com/rss"); got != "https://example.com/rss" {
		t.Errorf("feed:// = %q", got)
	}
}

func TestFeedScopesAndMarkRead(t *testing.T) {
	a := newCalendarTestApp(t)
	srv := newFeedServer(t)
	srv.setItems("One", "Two")
	folder, err := a.CreateFeedFolder("News")
	if err != nil {
		t.Fatal(err)
	}
	feed, err := a.AddFeed(srv.URL+"/feed.xml", folder.ID, "Mine")
	if err != nil {
		t.Fatal(err)
	}
	if feed.Title != "Mine" || feed.FolderID != folder.ID {
		t.Fatalf("feed = %+v", feed)
	}

	inFolder, _ := a.ListFeedEntries(FeedScopeDTO{Kind: "folder", ID: folder.ID}, 10, 0)
	if len(inFolder) != 2 {
		t.Fatalf("folder scope = %+v", inFolder)
	}
	none, _ := a.ListFeedEntries(FeedScopeDTO{Kind: "folder", ID: 0}, 10, 0)
	if len(none) != 0 {
		t.Errorf("no-folder scope = %+v", none)
	}
	found, _ := a.ListFeedEntries(FeedScopeDTO{Kind: "all", Search: "two"}, 10, 0)
	if entryTitlesOf(found) != "Two" {
		t.Errorf("search = %s", entryTitlesOf(found))
	}

	if err := a.SetFeedEntryStarred(inFolder[1].ID, true); err != nil {
		t.Fatal(err)
	}
	if err := a.SetFeedEntryReadLater(inFolder[0].ID, true); err != nil {
		t.Fatal(err)
	}
	marked, err := a.MarkFeedScopeRead(FeedScopeDTO{Kind: "folder", ID: folder.ID})
	if err != nil || len(marked) != 2 {
		t.Fatalf("marked = %v, %v", marked, err)
	}
	unread, _ := a.ListFeedEntries(FeedScopeDTO{Kind: "unread"}, 10, 0)
	if len(unread) != 0 {
		t.Errorf("unread after mark all = %+v", unread)
	}
	back, _ := a.SetFeedEntriesRead(marked, false)
	if len(back) != 2 {
		t.Errorf("undo = %v", back)
	}
	starred, _ := a.ListFeedEntries(FeedScopeDTO{Kind: "starred"}, 10, 0)
	later, _ := a.ListFeedEntries(FeedScopeDTO{Kind: "later"}, 10, 0)
	if entryTitlesOf(starred) != "Two" || entryTitlesOf(later) != "One" {
		t.Errorf("starred %s, later %s", entryTitlesOf(starred), entryTitlesOf(later))
	}
	if _, err := a.ListFeedEntries(FeedScopeDTO{Kind: "bogus"}, 10, 0); err == nil {
		t.Error("unknown scope accepted")
	}

	if err := a.DeleteFeedFolder(folder.ID); err != nil {
		t.Fatal(err)
	}
	side, _ := a.ListFeedSidebar()
	if len(side.Folders) != 0 || side.Feeds[0].FolderID != 0 {
		t.Errorf("after folder delete = %+v", side)
	}
}

func TestOPMLImportExport(t *testing.T) {
	a := newCalendarTestApp(t)
	srv := newFeedServer(t)
	srv.setItems("One")
	other := newFeedServer(t)
	other.setItems("Elsewhere")

	opml := `<?xml version="1.0"?><opml version="2.0"><head><title>x</title></head><body>
<outline text="Tech"><outline text="Site" type="rss" xmlUrl="` + srv.URL + `/feed.xml"/></outline>
<outline text="Other" type="rss" xmlUrl="` + other.URL + `/feed.xml"/>
<outline text="Broken" type="rss" xmlUrl="javascript:alert(1)"/>
</body></opml>`
	res, err := a.importOPML([]byte(opml))
	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 2 || res.Skipped != 1 {
		t.Errorf("import = %+v", res)
	}
	again, _ := a.importOPML([]byte(opml))
	if again.Added != 0 || again.Skipped != 3 {
		t.Errorf("second import = %+v", again)
	}
	side, _ := a.ListFeedSidebar()
	if len(side.Folders) != 1 || side.Folders[0].Name != "Tech" || len(side.Feeds) != 2 {
		t.Fatalf("sidebar = %+v", side)
	}

	out, err := a.exportOPML()
	if err != nil {
		t.Fatal(err)
	}
	text := string(out)
	if !strings.Contains(text, `xmlUrl="`+srv.URL+`/feed.xml"`) || !strings.Contains(text, `text="Tech"`) {
		t.Errorf("export = %s", text)
	}
	if strings.Index(text, other.URL) > strings.Index(text, `text="Tech"`) {
		t.Errorf("feeds outside folders should come first: %s", text)
	}
}

func TestNewsletterMail(t *testing.T) {
	a, db, ctx := moveTestApp(t)
	acc := newStatsAccount(t, db, ctx)
	now := time.Now()
	unsub := "<https://news.example/u>"
	news := acc.add(t, db, ctx, storage.Message{Subject: "Weekly", FromAddress: "news@news.example", Date: now, ListUnsubscribe: unsub})
	acc.add(t, db, ctx, storage.Message{Subject: "Archived issue", FromAddress: "news@news.example", Date: now, ListUnsubscribe: unsub, FolderID: acc.archive.ID})
	acc.add(t, db, ctx, storage.Message{Subject: "Personal", FromAddress: "friend@example.com", Date: now})
	acc.add(t, db, ctx, storage.Message{Subject: "Old issue", FromAddress: "news@news.example", Date: now.Add(-time.Hour), ListUnsubscribe: unsub, Flags: storage.FlagSeen})

	list, err := a.ListNewsletterMail(50, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	var subjects []string
	for _, m := range list {
		subjects = append(subjects, m.Subject)
	}
	if strings.Join(subjects, ",") != "Weekly,Old issue" {
		t.Errorf("newsletters = %v", subjects)
	}
	unread, _ := a.ListNewsletterMail(50, 0, true)
	if len(unread) != 1 || unread[0].ID != news {
		t.Errorf("unread newsletters = %+v", unread)
	}
	n, err := a.CountNewsletterMail()
	if err != nil || n != 1 {
		t.Errorf("count = %d, %v", n, err)
	}
	if err := a.SetSeen(news, true); err != nil {
		t.Fatal(err)
	}
	if n, _ := a.CountNewsletterMail(); n != 0 {
		t.Errorf("count after reading = %d", n)
	}
}
