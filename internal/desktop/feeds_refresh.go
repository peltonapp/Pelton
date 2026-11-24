package desktop

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/peltonapp/Pelton/internal/feeds"
	"github.com/peltonapp/Pelton/internal/proxy"
	"github.com/peltonapp/Pelton/internal/storage"
)

// Feed refresh. A loop checks every half minute for feeds whose next fetch
// time has come and fetches them on a few goroutines, never on the ui thread.
// The Fetcher keeps one request per host in flight with a gap between them.
// When a feed may be fetched next follows its interval, the Cache-Control
// max-age of its last response, a Retry-After the server sent, and a doubling
// backoff after failures.

const (
	// feedFetchTimeout bounds one request.
	feedFetchTimeout = 30 * time.Second
	// feedWorkers is how many feeds are fetched at once, across hosts.
	feedWorkers = 4
	// feedMaxBackoff is the longest a failing feed waits between tries.
	feedMaxBackoff = 24 * time.Hour
	// feedMinBackoff is the shortest.
	feedMinBackoff = 5 * time.Minute
	// feedIconMax caps an icon download.
	feedIconMax = 128 << 10
	// feedSearchMax caps the text an entry puts into the search index.
	feedSearchMax = 64 << 10
)

// feedFetchState holds the fetcher, rebuilt when the proxy setting changes so
// feeds go through the same proxy as mail.
type feedFetchState struct {
	mu      sync.Mutex
	cfg     proxy.Config
	fetcher *feeds.Fetcher
	// refreshMu lets one refresh run at a time.
	refreshMu sync.Mutex
	// iconTried and iconHint remember, for this run of the app, which feeds an
	// icon was already looked for and the icon a feed named itself.
	iconTried sync.Map
	iconHint  sync.Map
}

// feedFetcher returns the fetcher for the current proxy setting.
func (a *App) feedFetcher() *feeds.Fetcher {
	cfg := a.currentProxy()
	a.feedState.mu.Lock()
	defer a.feedState.mu.Unlock()
	if a.feedState.fetcher == nil || a.feedState.cfg != cfg {
		a.feedState.fetcher = feeds.NewFetcher(cfg.HTTPClient(feedFetchTimeout), feeds.DefaultHostGap)
		a.feedState.cfg = cfg
	}
	return a.feedState.fetcher
}

// feedInterval is how often a feed is fetched: its own interval, else the
// app-wide one. 0 means only by hand.
func (a *App) feedInterval(f storage.Feed) time.Duration {
	if f.IntervalMinutes > 0 {
		return time.Duration(f.IntervalMinutes) * time.Minute
	}
	return time.Duration(max(a.intSetting(settingFeedsRefresh, defaultFeedsRefresh), 0)) * time.Minute
}

// feedBackoff is how long a feed waits after its nth failure in a row.
func feedBackoff(n int, interval time.Duration, err error) time.Duration {
	base := interval
	if base <= 0 {
		base = defaultFeedsRefresh * time.Minute
	}
	d := base
	for i := 1; i < n && d < feedMaxBackoff; i++ {
		d *= 2
	}
	d = min(max(d, feedMinBackoff), feedMaxBackoff)
	var se *feeds.StatusError
	if errors.As(err, &se) && se.RetryAfter > d {
		d = se.RetryAfter
	}
	return d
}

// friendlyFeedError says why a fetch failed in words for the sidebar's error
// badge.
func friendlyFeedError(err error) error {
	var se *feeds.StatusError
	var dnsErr *net.DNSError
	var certErr *x509.UnknownAuthorityError
	var hostErr x509.HostnameError
	var netErr net.Error
	switch {
	case errors.As(err, &se):
		switch {
		case se.Code == http.StatusNotFound:
			return fmt.Errorf("the feed was not found (%d)", se.Code)
		case se.Code == http.StatusGone:
			return fmt.Errorf("the feed has been removed (%d)", se.Code)
		case se.Code == http.StatusUnauthorized || se.Code == http.StatusForbidden:
			return fmt.Errorf("the server refused access (%d)", se.Code)
		case se.Code == http.StatusTooManyRequests:
			return fmt.Errorf("the server asked to slow down (%d)", se.Code)
		case se.Code >= 500:
			return fmt.Errorf("the server had a problem (%d)", se.Code)
		default:
			return fmt.Errorf("the server answered %d", se.Code)
		}
	case errors.Is(err, feeds.ErrTooLarge):
		return errors.New("the feed is too large")
	case errors.Is(err, feeds.ErrNotFeed):
		return errors.New("this address does not serve a feed")
	case errors.As(err, &dnsErr):
		return errors.New("the server could not be found")
	case errors.As(err, &certErr), errors.As(err, &hostErr):
		return errors.New("the server's certificate is not trusted")
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &netErr) && netErr.Timeout():
		return errors.New("the server did not answer in time")
	case strings.Contains(err.Error(), "connection refused"):
		return errors.New("the server refused the connection")
	}
	return err
}

// toStoreEntries converts parsed entries for the store, with the plain text
// the search index and the list snippet read.
func toStoreEntries(list []feeds.Entry) []storage.FeedEntry {
	out := make([]storage.FeedEntry, 0, len(list))
	for _, e := range list {
		body := e.Content
		if body == "" {
			body = e.Summary
		}
		text := feeds.PlainText(body)
		if len(text) > feedSearchMax {
			text = strings.ToValidUTF8(text[:feedSearchMax], "")
		}
		enc := make([]storage.FeedEnclosure, 0, len(e.Enclosures))
		for _, x := range e.Enclosures {
			enc = append(enc, storage.FeedEnclosure{URL: x.URL, Type: x.Type, Length: x.Length, Title: x.Title})
		}
		out = append(out, storage.FeedEntry{
			GUID:       e.GUID,
			URL:        e.URL,
			Title:      e.Title,
			Author:     e.Author,
			Summary:    e.Summary,
			Content:    e.Content,
			SearchText: text,
			Image:      e.Image,
			Enclosures: enc,
			Published:  e.Published,
			Updated:    e.Updated,
		})
	}
	return out
}

// refreshFeed fetches one feed and stores what is new, recording how the
// fetch went and when the next may happen. It returns the ids of the new
// entries.
func (a *App) refreshFeed(ctx context.Context, f storage.Feed, fetcher *feeds.Fetcher) ([]int64, error) {
	now := time.Now()
	interval := a.feedInterval(f)
	nextOK := func(maxAge time.Duration) time.Time {
		every := interval
		if every <= 0 {
			every = defaultFeedsRefresh * time.Minute
		}
		return now.Add(max(every, maxAge))
	}
	fail := func(err error) ([]int64, error) {
		if ctx.Err() != nil {
			return nil, err
		}
		friendly := friendlyFeedError(err)
		if rerr := a.store.RecordFeedFetch(a.ctx, f.ID, storage.FeedFetch{At: now, Next: now.Add(feedBackoff(f.ErrorCount+1, interval, err)), Err: friendly.Error()}); rerr != nil {
			a.log.Warn("record feed fetch", "feed", f.ID, "err", rerr)
		}
		return nil, friendly
	}

	resp, err := fetcher.Fetch(ctx, feeds.Request{URL: f.URL, ETag: f.ETag, LastModified: f.LastModified})
	if err != nil {
		return fail(err)
	}
	if resp.PermanentURL != "" && resp.PermanentURL != f.URL {
		if err := a.store.SetFeedURL(a.ctx, f.ID, resp.PermanentURL); err == nil {
			f.URL = resp.PermanentURL
		}
	}
	if resp.NotModified {
		return nil, a.store.RecordFeedFetch(a.ctx, f.ID, storage.FeedFetch{At: now, Next: nextOK(resp.MaxAge), ETag: resp.ETag, LastModified: resp.LastModified})
	}
	parsed, err := feeds.Parse(resp.Body, resp.URL)
	if err != nil {
		return fail(err)
	}
	parsed.ClampDates(now)
	if parsed.Title != f.Title || (parsed.SiteURL != "" && parsed.SiteURL != f.SiteURL) || parsed.Description != f.Description {
		site := parsed.SiteURL
		if site == "" {
			site = f.SiteURL
		}
		if err := a.store.UpdateFeedMeta(a.ctx, f.ID, parsed.Title, site, parsed.Description); err != nil {
			a.log.Warn("update feed meta", "feed", f.ID, "err", err)
		}
	}
	if parsed.IconURL != "" {
		a.feedState.iconHint.Store(f.ID, parsed.IconURL)
	}
	added, err := a.store.UpsertFeedEntries(a.ctx, f.ID, toStoreEntries(parsed.Entries), now)
	if err != nil {
		return nil, err
	}
	a.scriptFeedItems(added)
	return added, a.store.RecordFeedFetch(a.ctx, f.ID, storage.FeedFetch{At: now, Next: nextOK(resp.MaxAge), ETag: resp.ETag, LastModified: resp.LastModified})
}

// FeedsProgressEvent is the payload of EventFeedsProgress. Feed names the one
// just fetched; Failed counts the feeds that failed so far.
type FeedsProgressEvent struct {
	Running bool   `json:"running"`
	Done    int    `json:"done"`
	Total   int    `json:"total"`
	Feed    string `json:"feed"`
	Failed  int    `json:"failed"`
}

// FeedsChangedEvent is the payload of EventFeedsChanged. Added counts new
// entries; an empty FeedIDs means anything may have changed.
type FeedsChangedEvent struct {
	FeedIDs []int64 `json:"feedIds"`
	Added   int     `json:"added"`
}

// FeedsOpenEvent is the payload of EventFeedsOpen.
type FeedsOpenEvent struct {
	EntryID int64 `json:"entryId"`
}

// RefreshFeeds fetches every feed now, in the background. Progress and the
// result arrive as events. A refresh already running is left to finish.
func (a *App) RefreshFeeds() error {
	if err := a.ready(); err != nil {
		return err
	}
	a.refreshFeedsInBackground(true)
	return nil
}

// RefreshFeed fetches one feed now, in the background.
func (a *App) RefreshFeed(id int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	f, err := a.store.GetFeed(a.ctx, id)
	if err != nil {
		return err
	}
	goSafe("refreshing a feed", func() { a.runFeedRefresh(a.sessionCtx(), []storage.Feed{*f}) })
	return nil
}

// refreshFeedsInBackground fetches every feed, or only those that are due.
func (a *App) refreshFeedsInBackground(all bool) {
	goSafe("refreshing feeds", func() {
		var list []storage.Feed
		var err error
		if all {
			list, err = a.store.ListFeeds(a.ctx)
		} else {
			list, err = a.dueFeeds()
		}
		if err != nil {
			a.log.Warn("list feeds", "err", err)
			return
		}
		a.runFeedRefresh(a.sessionCtx(), list)
	})
}

// dueFeeds lists the feeds whose next fetch time has come and that are
// fetched on a schedule at all.
func (a *App) dueFeeds() ([]storage.Feed, error) {
	due, err := a.store.DueFeeds(a.ctx, time.Now())
	if err != nil {
		return nil, err
	}
	out := due[:0]
	for _, f := range due {
		if a.feedInterval(f) > 0 || f.LastFetchedAt.IsZero() {
			out = append(out, f)
		}
	}
	return out, nil
}

// runFeedLoop fetches due feeds until the app quits, and applies retention
// once an hour. Low-power mode pauses it.
func (a *App) runFeedLoop() {
	const tick = 30 * time.Second
	t := time.NewTicker(tick)
	defer t.Stop()
	var lastPrune time.Time
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-t.C:
		}
		if a.lowPowerMode() {
			continue
		}
		if due, err := a.dueFeeds(); err != nil {
			a.log.Warn("due feeds", "err", err)
		} else if len(due) > 0 {
			a.runFeedRefresh(a.sessionCtx(), due)
		}
		if time.Since(lastPrune) > time.Hour {
			lastPrune = time.Now()
			a.pruneFeeds()
		}
	}
}

// runFeedRefresh fetches the feeds on a few goroutines, reporting progress,
// then announces what changed and raises the notifications feeds asked for.
// It returns at once when another refresh is running.
func (a *App) runFeedRefresh(ctx context.Context, list []storage.Feed) {
	if len(list) == 0 || !a.feedState.refreshMu.TryLock() {
		return
	}
	defer a.feedState.refreshMu.Unlock()
	fetcher := a.feedFetcher()

	var (
		mu       sync.Mutex
		progress = FeedsProgressEvent{Running: true, Total: len(list)}
		changed  []int64
		added    int
		notify   []feedNews
	)
	a.emit(EventFeedsProgress, progress)
	jobs := make(chan storage.Feed)
	var wg sync.WaitGroup
	for range min(feedWorkers, len(list)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for f := range jobs {
				ids, err := a.refreshFeed(ctx, f, fetcher)
				mu.Lock()
				progress.Done++
				progress.Feed = f.DisplayTitle()
				if err != nil {
					progress.Failed++
				}
				if err != nil || len(ids) > 0 || f.LastError != "" {
					changed = append(changed, f.ID)
				}
				added += len(ids)
				if len(ids) > 0 && f.Notify && !f.LastSuccessAt.IsZero() {
					notify = append(notify, feedNews{feed: f, ids: ids})
				}
				a.emit(EventFeedsProgress, progress)
				mu.Unlock()
				if f.Icon == "" {
					a.fetchFeedIcon(ctx, f)
				}
			}
		}()
	}
	for _, f := range list {
		if ctx.Err() != nil {
			break
		}
		jobs <- f
	}
	close(jobs)
	wg.Wait()

	if added > 0 {
		a.pruneFeeds()
	}
	a.emit(EventFeedsProgress, FeedsProgressEvent{Done: progress.Done, Total: progress.Total, Failed: progress.Failed})
	a.emit(EventFeedsChanged, FeedsChangedEvent{FeedIDs: changed, Added: added})
	a.notifyFeeds(notify)
}

// pruneFeeds applies the retention settings.
func (a *App) pruneFeeds() {
	days := a.intSetting(settingFeedsKeepDays, defaultFeedsKeepDays)
	count := a.intSetting(settingFeedsKeepCount, defaultFeedsKeepCount)
	if _, err := a.store.PruneFeedEntries(a.ctx, max(days, 0), max(count, 0), time.Now()); err != nil {
		a.log.Warn("prune feed entries", "err", err)
	}
}

// --- icons ------------------------------------------------------------------------

// fetchFeedIcon looks for the site's icon once per run of the app: the icon
// the feed names, the icons the site's page links, then /favicon.ico. Only
// addresses on the feed's own site are fetched; the icon is kept in the store
// as a data url.
func (a *App) fetchFeedIcon(ctx context.Context, f storage.Feed) {
	if f.Icon != "" {
		return
	}
	if _, tried := a.feedState.iconTried.LoadOrStore(f.ID, true); tried {
		return
	}
	site := f.SiteURL
	if site == "" {
		if u, err := url.Parse(f.URL); err == nil {
			site = (&url.URL{Scheme: u.Scheme, Host: u.Host, Path: "/"}).String()
		}
	}
	if site == "" {
		return
	}
	fetcher := a.feedFetcher()
	var candidates []string
	if hint, ok := a.feedState.iconHint.Load(f.ID); ok {
		candidates = append(candidates, hint.(string))
	}
	if page, err := fetcher.Fetch(ctx, feeds.Request{URL: site, Accept: "text/html", MaxBytes: 1 << 20}); err == nil {
		candidates = append(candidates, feeds.IconLinks(page.Body, page.URL)...)
	} else {
		candidates = append(candidates, feeds.IconLinks(nil, site)...)
	}
	for _, c := range candidates {
		if !feeds.SameSite(c, site) && !feeds.SameSite(c, f.URL) {
			continue
		}
		r, err := fetcher.Fetch(ctx, feeds.Request{URL: c, Accept: "image/*", MaxBytes: feedIconMax})
		if err != nil || len(r.Body) == 0 {
			continue
		}
		mime := iconType(r.ContentType, r.Body)
		if mime == "" {
			continue
		}
		icon := "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(r.Body)
		if err := a.store.SetFeedIcon(a.ctx, f.ID, icon); err != nil {
			a.log.Warn("store feed icon", "feed", f.ID, "err", err)
			return
		}
		a.emit(EventFeedsChanged, FeedsChangedEvent{FeedIDs: []int64{f.ID}})
		return
	}
}

// iconType is the image type of an icon download, or "" when it is not an
// image.
func iconType(contentType string, body []byte) string {
	ct := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if strings.HasPrefix(ct, "image/") {
		return ct
	}
	if sniffed := http.DetectContentType(body); strings.HasPrefix(sniffed, "image/") {
		return sniffed
	}
	return ""
}

// --- notifications ----------------------------------------------------------------

// feedNews is what one refresh found in a feed that asked to notify.
type feedNews struct {
	feed storage.Feed
	ids  []int64
}

// notifyDataFeedEntry is the key of the entry id in a feed notification's
// data.
const notifyDataFeedEntry = "feedEntryId"

// notifyFeeds raises a notification per new entry of the feeds that opted in,
// or one summary when there are many. Focus mode keeps them quiet.
func (a *App) notifyFeeds(news []feedNews) {
	if len(news) == 0 || a.advanceFocus(time.Now()).Active {
		return
	}
	s := notifyStringsFor(a.stringSetting(settingLanguage, "en"))
	total := 0
	for _, n := range news {
		total += len(n.ids)
	}
	if total > notifyBurstLimit {
		title := s.feeds
		if len(news) == 1 {
			title = news[0].feed.DisplayTitle()
		}
		a.sendNotification(notification{title: title, body: fmt.Sprintf(s.feedNew, total)})
		return
	}
	for _, n := range news {
		for _, id := range n.ids {
			e, err := a.store.GetFeedEntry(a.ctx, id)
			if err != nil {
				continue
			}
			title := e.Title
			if title == "" {
				title = s.noSubject
			}
			a.sendNotification(notification{title: n.feed.DisplayTitle(), body: title, feedEntryID: e.ID})
		}
	}
}

// openFeedFromNotification shows the entry a clicked notification was about.
func (a *App) openFeedFromNotification(id int64) {
	a.showWindow()
	a.emit(EventFeedsOpen, FeedsOpenEvent{EntryID: id})
}
