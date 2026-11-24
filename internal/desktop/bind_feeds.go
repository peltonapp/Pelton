package desktop

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/feeds"
	"github.com/peltonapp/Pelton/internal/mailview"
	"github.com/peltonapp/Pelton/internal/storage"
)

// The Feeds space: subscriptions to RSS, Atom and JSON feeds, read in the same
// three panes as mail. Feeds are fetched straight from the servers that
// publish them (feeds_refresh.go); entries are kept in the local store and
// shown through the mail body sanitizer, remote images blocked like mail's.

const (
	settingFeedsRefresh    = "feeds_refresh_minutes"
	settingFeedsKeepDays   = "feeds_keep_days"
	settingFeedsKeepCount  = "feeds_keep_count"
	settingFeedsLoadImages = "feeds_load_images"
	settingFeedsMarkScroll = "feeds_mark_read_on_scroll"
)

// defaults for the settings above.
const (
	defaultFeedsRefresh   = 30
	defaultFeedsKeepDays  = 90
	defaultFeedsKeepCount = 500
)

// feedPageMax caps one page of the entry list.
const feedPageMax = 500

// FeedFolderDTO is one folder of the Feeds sidebar.
type FeedFolderDTO struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Position int    `json:"position"`
}

// FeedDTO is one subscription for the sidebar and its settings. Title is the
// name to show; FeedTitle is what the feed calls itself and CustomTitle what
// the user renamed it to. Error is the reason the last fetch failed, ” when
// it worked. Dates are RFC 3339, ” for never.
type FeedDTO struct {
	ID              int64  `json:"id"`
	FolderID        int64  `json:"folderId"`
	URL             string `json:"url"`
	SiteURL         string `json:"siteUrl"`
	Title           string `json:"title"`
	FeedTitle       string `json:"feedTitle"`
	CustomTitle     string `json:"customTitle"`
	Description     string `json:"description"`
	Icon            string `json:"icon"`
	Error           string `json:"error"`
	ErrorCount      int    `json:"errorCount"`
	LastFetched     string `json:"lastFetched"`
	LastSuccess     string `json:"lastSuccess"`
	NextFetch       string `json:"nextFetch"`
	IntervalMinutes int    `json:"intervalMinutes"`
	LoadImages      bool   `json:"loadImages"`
	Notify          bool   `json:"notify"`
	Position        int    `json:"position"`
	Unread          int    `json:"unread"`
}

// FeedCountsDTO are the counts beside the smart views.
type FeedCountsDTO struct {
	Unread    int `json:"unread"`
	Today     int `json:"today"`
	Starred   int `json:"starred"`
	ReadLater int `json:"readLater"`
}

// FeedSidebarDTO is everything the Feeds sidebar shows.
type FeedSidebarDTO struct {
	Folders []FeedFolderDTO `json:"folders"`
	Feeds   []FeedDTO       `json:"feeds"`
	Counts  FeedCountsDTO   `json:"counts"`
}

// FeedEnclosureDTO is a file attached to an entry.
type FeedEnclosureDTO struct {
	URL    string `json:"url"`
	Type   string `json:"type"`
	Length int64  `json:"length"`
	Title  string `json:"title"`
}

// FeedEntryDTO is one row of the entry list. Published is RFC 3339.
type FeedEntryDTO struct {
	ID        int64              `json:"id"`
	FeedID    int64              `json:"feedId"`
	Title     string             `json:"title"`
	URL       string             `json:"url"`
	Author    string             `json:"author"`
	Published string             `json:"published"`
	Snippet   string             `json:"snippet"`
	Image     string             `json:"image"`
	Read      bool               `json:"read"`
	Starred   bool               `json:"starred"`
	ReadLater bool               `json:"readLater"`
	Media     []FeedEnclosureDTO `json:"media"`
}

// FeedEntryDetailDTO is an entry opened in the reader. BodyHTMLSafe is the
// sanitized html, shaped like a message's so the mail body renderer shows it.
type FeedEntryDetailDTO struct {
	FeedEntryDTO
	BodyHTMLSafe     string             `json:"bodyHtmlSafe"`
	HasRemoteContent bool               `json:"hasRemoteContent"`
	RemoteAllowed    bool               `json:"remoteAllowed"`
	RemoteHosts      []string           `json:"remoteHosts"`
	TrackingPixels   []TrackingPixelDTO `json:"trackingPixels"`
}

// FeedScopeDTO names what the entry list shows. Kind is one of unread (every
// unread entry), today, starred, later, all, feed (ID is the feed) or folder
// (ID is the folder, 0 for feeds in none). UnreadOnly narrows feed, folder,
// all, starred and later to unread entries; Search narrows any of them.
type FeedScopeDTO struct {
	Kind       string `json:"kind"`
	ID         int64  `json:"id"`
	UnreadOnly bool   `json:"unreadOnly"`
	Search     string `json:"search"`
}

// FeedSettingsDTO is what the user edits on a feed.
type FeedSettingsDTO struct {
	ID              int64  `json:"id"`
	CustomTitle     string `json:"customTitle"`
	FolderID        int64  `json:"folderId"`
	IntervalMinutes int    `json:"intervalMinutes"`
	LoadImages      bool   `json:"loadImages"`
	Notify          bool   `json:"notify"`
}

// FeedCandidateDTO is a feed found for an address the user typed, with a
// preview of what it holds.
type FeedCandidateDTO struct {
	URL        string   `json:"url"`
	Title      string   `json:"title"`
	SiteURL    string   `json:"siteUrl"`
	Kind       string   `json:"kind"`
	EntryCount int      `json:"entryCount"`
	Latest     []string `json:"latest"`
	LastPost   string   `json:"lastPost"`
	// Subscribed is set when the feed is already in the list.
	Subscribed bool `json:"subscribed"`
}

// FeedImportDTO reports an OPML import.
type FeedImportDTO struct {
	Added   int `json:"added"`
	Skipped int `json:"skipped"`
}

func toFeedDTO(f storage.Feed, unread int) FeedDTO {
	return FeedDTO{
		ID:              f.ID,
		FolderID:        f.FolderID,
		URL:             f.URL,
		SiteURL:         f.SiteURL,
		Title:           f.DisplayTitle(),
		FeedTitle:       f.Title,
		CustomTitle:     f.CustomTitle,
		Description:     f.Description,
		Icon:            f.Icon,
		Error:           f.LastError,
		ErrorCount:      f.ErrorCount,
		LastFetched:     formatDate(f.LastFetchedAt),
		LastSuccess:     formatDate(f.LastSuccessAt),
		NextFetch:       formatDate(f.NextFetchAt),
		IntervalMinutes: f.IntervalMinutes,
		LoadImages:      f.LoadImages,
		Notify:          f.Notify,
		Position:        f.Position,
		Unread:          unread,
	}
}

func toEntryDTO(e storage.FeedEntry) FeedEntryDTO {
	media := make([]FeedEnclosureDTO, 0, len(e.Enclosures))
	for _, enc := range e.Enclosures {
		media = append(media, FeedEnclosureDTO(enc))
	}
	snippet := e.Snippet
	if snippet == "" && e.SearchText != "" {
		snippet = e.SearchText
	}
	return FeedEntryDTO{
		ID:        e.ID,
		FeedID:    e.FeedID,
		Title:     e.Title,
		URL:       e.URL,
		Author:    e.Author,
		Published: formatDate(e.Published),
		Snippet:   snippetOf(snippet, e.Title),
		Image:     e.Image,
		Read:      e.Read,
		Starred:   e.Starred,
		ReadLater: e.ReadLater,
		Media:     media,
	}
}

// snippetOf trims the entry text for the list, dropping a leading repeat of
// the title, which many feeds start their text with.
func snippetOf(text, title string) string {
	text = strings.TrimSpace(text)
	if title != "" && strings.HasPrefix(text, title) {
		text = strings.TrimSpace(strings.TrimPrefix(text, title))
	}
	const max = 200
	if r := []rune(text); len(r) > max {
		return strings.TrimSpace(string(r[:max])) + "…"
	}
	return text
}

// ListFeedSidebar returns the folders, the feeds with their unread counts and
// the counts of the smart views.
func (a *App) ListFeedSidebar() (FeedSidebarDTO, error) {
	if err := a.ready(); err != nil {
		return FeedSidebarDTO{}, err
	}
	folders, err := a.store.ListFeedFolders(a.ctx)
	if err != nil {
		return FeedSidebarDTO{}, err
	}
	list, err := a.store.ListFeeds(a.ctx)
	if err != nil {
		return FeedSidebarDTO{}, err
	}
	counts, err := a.store.CountFeedEntries(a.ctx, startOfToday(time.Now()))
	if err != nil {
		return FeedSidebarDTO{}, err
	}
	out := FeedSidebarDTO{
		Folders: make([]FeedFolderDTO, 0, len(folders)),
		Feeds:   make([]FeedDTO, 0, len(list)),
		Counts:  FeedCountsDTO{Unread: counts.Unread, Today: counts.Today, Starred: counts.Starred, ReadLater: counts.ReadLater},
	}
	for _, f := range folders {
		out.Folders = append(out.Folders, FeedFolderDTO{ID: f.ID, Name: f.Name, Position: f.Position})
	}
	for _, f := range list {
		out.Feeds = append(out.Feeds, toFeedDTO(f, counts.PerFeed[f.ID]))
	}
	return out, nil
}

func startOfToday(now time.Time) time.Time {
	y, m, d := now.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, now.Location())
}

// feedQuery turns a scope into a store query.
func (a *App) feedQuery(scope FeedScopeDTO) (storage.FeedEntryQuery, error) {
	q := storage.FeedEntryQuery{Search: scope.Search, UnreadOnly: scope.UnreadOnly}
	switch scope.Kind {
	case "unread":
		q.UnreadOnly = true
	case "today":
		q.UnreadOnly = true
		q.Since = startOfToday(time.Now())
	case "starred":
		q.Starred = true
	case "later":
		q.ReadLater = true
	case "all":
	case "feed":
		q.FeedIDs = []int64{scope.ID}
	case "folder":
		list, err := a.store.ListFeeds(a.ctx)
		if err != nil {
			return q, err
		}
		q.FeedIDs = []int64{}
		for _, f := range list {
			if f.FolderID == scope.ID {
				q.FeedIDs = append(q.FeedIDs, f.ID)
			}
		}
	default:
		return q, fmt.Errorf("pelton: unknown feed scope %q", scope.Kind)
	}
	return q, nil
}

// ListFeedEntries returns a page of the entries a scope selects, newest first.
func (a *App) ListFeedEntries(scope FeedScopeDTO, limit, offset int) ([]FeedEntryDTO, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	q, err := a.feedQuery(scope)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > feedPageMax {
		limit = feedPageMax
	}
	q.Limit, q.Offset = limit, offset
	list, err := a.store.QueryFeedEntries(a.ctx, q)
	if err != nil {
		return nil, err
	}
	out := make([]FeedEntryDTO, 0, len(list))
	for _, e := range list {
		out = append(out, toEntryDTO(e))
	}
	return out, nil
}

// feedRemoteAllowed reports whether an entry of the feed shows its remote
// images without asking: the app-wide remote content override, the Feeds
// default, or the feed's own choice.
func (a *App) feedRemoteAllowed(f *storage.Feed) bool {
	return a.boolSetting(settingRemoteAlways, false) || a.boolSetting(settingFeedsLoadImages, false) || (f != nil && f.LoadImages)
}

// entryHTML is the html an entry shows: its content, or its summary when the
// feed sent no content, with the known feed counting pixels taken out.
func entryHTML(e *storage.FeedEntry) string {
	body := e.Content
	if strings.TrimSpace(mailview.PlainText(body)) == "" && !strings.Contains(body, "<img") {
		body = e.Summary
	}
	return feeds.StripFeedTrackers(body)
}

// renderFeedHTML sanitizes entry html the way renderHTML does mail.
func (a *App) renderFeedHTML(html string, allowRemote, includeTrackers bool) string {
	if html == "" {
		return ""
	}
	if allowRemote && !includeTrackers && a.blockTrackers() {
		html = mailview.StripTrackers(html, mailview.ScanRemoteImages(html).TrackerURLs())
	}
	return mailview.Sanitize(html, allowRemote, a.senderFonts())
}

// GetFeedEntry returns an entry for the reader. Remote images are shown when
// the feed, the Feeds default or the app-wide override allows them.
func (a *App) GetFeedEntry(id int64) (FeedEntryDetailDTO, error) {
	if err := a.ready(); err != nil {
		return FeedEntryDetailDTO{}, err
	}
	e, err := a.store.GetFeedEntry(a.ctx, id)
	if err != nil {
		return FeedEntryDetailDTO{}, err
	}
	f, err := a.store.GetFeed(a.ctx, e.FeedID)
	if err != nil {
		return FeedEntryDetailDTO{}, err
	}
	allow := a.feedRemoteAllowed(f)
	body := entryHTML(e)
	out := FeedEntryDetailDTO{
		FeedEntryDTO:     toEntryDTO(*e),
		BodyHTMLSafe:     a.renderFeedHTML(body, allow, false),
		HasRemoteContent: mailview.HasRemoteContent(body) || e.Image != "",
		RemoteAllowed:    allow,
		RemoteHosts:      mailview.RemoteHosts(body),
		TrackingPixels:   a.trackerDTOs(body),
	}
	if out.RemoteHosts == nil {
		out.RemoteHosts = []string{}
	}
	if out.TrackingPixels == nil {
		out.TrackingPixels = []TrackingPixelDTO{}
	}
	return out, nil
}

// GetFeedEntryHTML renders an entry again with remote content allowed or not,
// for the reader's load images control. includeTrackers also loads the images
// that look like tracking pixels.
func (a *App) GetFeedEntryHTML(id int64, allowRemote, includeTrackers bool) (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	e, err := a.store.GetFeedEntry(a.ctx, id)
	if err != nil {
		return "", err
	}
	return a.renderFeedHTML(entryHTML(e), allowRemote, includeTrackers), nil
}

// SetFeedEntriesRead marks entries read or unread and returns the ids that
// changed.
func (a *App) SetFeedEntriesRead(ids []int64, read bool) ([]int64, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	changed, err := a.store.SetFeedEntriesRead(a.ctx, ids, read)
	if changed == nil {
		changed = []int64{}
	}
	return changed, err
}

// MarkFeedScopeRead marks every unread entry of a scope read and returns
// their ids, so the ui can offer to undo it.
func (a *App) MarkFeedScopeRead(scope FeedScopeDTO) ([]int64, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	q, err := a.feedQuery(scope)
	if err != nil {
		return nil, err
	}
	ids, err := a.store.MarkFeedEntriesRead(a.ctx, q)
	if ids == nil {
		ids = []int64{}
	}
	return ids, err
}

// SetFeedEntryStarred stars or unstars an entry.
func (a *App) SetFeedEntryStarred(id int64, starred bool) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.store.SetFeedEntryStarred(a.ctx, id, starred)
}

// SetFeedEntryReadLater puts an entry on the read-later list or takes it off.
func (a *App) SetFeedEntryReadLater(id int64, later bool) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.store.SetFeedEntryReadLater(a.ctx, id, later)
}

// --- subscriptions --------------------------------------------------------------

// normalizeFeedAddress turns what the user typed into a web address: a bare
// host gets https, and feed: and feed:// prefixes are taken off.
func normalizeFeedAddress(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "feed:")
	s = strings.TrimPrefix(s, "//")
	if s == "" {
		return "", errors.New("pelton: enter a web address")
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("pelton: %q is not a web address", raw)
	}
	return u.String(), nil
}

// DiscoverFeeds finds the feeds at an address: the address itself when it is
// a feed, else the feeds its page links to, else the places sites usually keep
// one. Each candidate is fetched and read, so only working feeds come back.
func (a *App) DiscoverFeeds(address string) ([]FeedCandidateDTO, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	start, err := normalizeFeedAddress(address)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(a.ctx, 45*time.Second)
	defer cancel()
	fetcher := a.feedFetcher()

	resp, err := fetcher.Fetch(ctx, feeds.Request{URL: start, Accept: "text/html, application/xhtml+xml, " + "application/rss+xml, application/atom+xml, application/feed+json;q=0.9, */*;q=0.5"})
	if err != nil {
		return nil, friendlyFeedError(err)
	}
	if feeds.LooksLikeFeed(resp.Body, resp.ContentType) {
		if f, perr := feeds.Parse(resp.Body, resp.URL); perr == nil {
			return []FeedCandidateDTO{a.candidate(resp.URL, f)}, nil
		}
	}

	links := feeds.DiscoverLinks(resp.Body, resp.URL)
	var tries []string
	for _, l := range links {
		tries = append(tries, l.URL)
	}
	if len(tries) == 0 {
		base, _ := url.Parse(resp.URL)
		for _, p := range feeds.CommonPaths {
			tries = append(tries, (&url.URL{Scheme: base.Scheme, Host: base.Host, Path: p}).String())
		}
	}
	var out []FeedCandidateDTO
	seen := map[string]bool{}
	for _, u := range tries {
		if ctx.Err() != nil {
			break
		}
		r, err := fetcher.Fetch(ctx, feeds.Request{URL: u})
		if err != nil {
			continue
		}
		f, err := feeds.Parse(r.Body, r.URL)
		if err != nil || seen[r.URL] {
			continue
		}
		seen[r.URL] = true
		out = append(out, a.candidate(r.URL, f))
		if len(links) == 0 {
			break
		}
	}
	if len(out) == 0 {
		return nil, errors.New("pelton: no feed found at this address")
	}
	return out, nil
}

func (a *App) candidate(addr string, f *feeds.Feed) FeedCandidateDTO {
	c := FeedCandidateDTO{URL: addr, Title: f.Title, SiteURL: f.SiteURL, Kind: f.Kind, EntryCount: len(f.Entries), Latest: []string{}}
	var newest time.Time
	for i, e := range f.Entries {
		if i < 3 && e.Title != "" {
			c.Latest = append(c.Latest, e.Title)
		}
		if e.Published.After(newest) {
			newest = e.Published
		}
	}
	c.LastPost = formatDate(newest)
	if _, err := a.store.FeedByURL(a.ctx, addr); err == nil {
		c.Subscribed = true
	}
	return c
}

// AddFeed subscribes to a feed, fetches it at once and returns it. The site's
// icon follows in the background.
func (a *App) AddFeed(address string, folderID int64, title string) (FeedDTO, error) {
	if err := a.ready(); err != nil {
		return FeedDTO{}, err
	}
	addr, err := normalizeFeedAddress(address)
	if err != nil {
		return FeedDTO{}, err
	}
	f := &storage.Feed{URL: addr, FolderID: folderID, CustomTitle: strings.TrimSpace(title)}
	if err := a.store.CreateFeed(a.ctx, f); err != nil {
		if errors.Is(err, storage.ErrFeedExists) {
			return FeedDTO{}, errors.New("pelton: this feed is already in your list")
		}
		return FeedDTO{}, err
	}
	ctx, cancel := context.WithTimeout(a.sessionCtx(), time.Minute)
	defer cancel()
	added, _ := a.refreshFeed(ctx, *f, a.feedFetcher())
	got, err := a.store.GetFeed(a.ctx, f.ID)
	if err != nil {
		return FeedDTO{}, err
	}
	a.pruneFeeds()
	goSafe("fetching a feed icon", func() { a.fetchFeedIcon(a.sessionCtx(), *got) })
	a.emit(EventFeedsChanged, FeedsChangedEvent{FeedIDs: []int64{got.ID}, Added: len(added)})
	return toFeedDTO(*got, len(added)), nil
}

// UpdateFeed saves what the user set on a feed.
func (a *App) UpdateFeed(s FeedSettingsDTO) error {
	if err := a.ready(); err != nil {
		return err
	}
	if err := a.store.UpdateFeedSettings(a.ctx, s.ID, storage.FeedSettings{
		CustomTitle:     strings.TrimSpace(s.CustomTitle),
		FolderID:        s.FolderID,
		IntervalMinutes: max(s.IntervalMinutes, 0),
		LoadImages:      s.LoadImages,
		Notify:          s.Notify,
	}); err != nil {
		return err
	}
	a.emit(EventFeedsChanged, FeedsChangedEvent{FeedIDs: []int64{s.ID}})
	return nil
}

// DeleteFeed unsubscribes from a feed and drops its entries.
func (a *App) DeleteFeed(id int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.store.DeleteFeed(a.ctx, id)
}

// MoveFeeds puts feeds into a folder (0 for none) in the given order.
func (a *App) MoveFeeds(folderID int64, ids []int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.store.MoveFeeds(a.ctx, folderID, ids)
}

// CreateFeedFolder adds a folder.
func (a *App) CreateFeedFolder(name string) (FeedFolderDTO, error) {
	if err := a.ready(); err != nil {
		return FeedFolderDTO{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return FeedFolderDTO{}, errors.New("pelton: a folder needs a name")
	}
	f, err := a.store.CreateFeedFolder(a.ctx, name)
	if err != nil {
		return FeedFolderDTO{}, err
	}
	return FeedFolderDTO{ID: f.ID, Name: f.Name, Position: f.Position}, nil
}

// RenameFeedFolder renames a folder.
func (a *App) RenameFeedFolder(id int64, name string) error {
	if err := a.ready(); err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("pelton: a folder needs a name")
	}
	return a.store.RenameFeedFolder(a.ctx, id, name)
}

// DeleteFeedFolder removes a folder; its feeds move out of it.
func (a *App) DeleteFeedFolder(id int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.store.DeleteFeedFolder(a.ctx, id)
}

// ReorderFeedFolders puts the folders in the given order.
func (a *App) ReorderFeedFolders(ids []int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.store.ReorderFeedFolders(a.ctx, ids)
}

// --- OPML -------------------------------------------------------------------------

// ImportOPML asks for an OPML file and subscribes to every feed in it that is
// not in the list yet. The feeds are fetched in the background afterwards.
func (a *App) ImportOPML() (FeedImportDTO, error) {
	if err := a.ready(); err != nil {
		return FeedImportDTO{}, err
	}
	path, err := a.openFileDialog(openDialogOptions{
		Title:   "Import feeds",
		Filters: []fileFilter{{DisplayName: "OPML (*.opml, *.xml)", Pattern: "*.opml;*.xml"}},
	})
	if err != nil || path == "" {
		return FeedImportDTO{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return FeedImportDTO{}, err
	}
	return a.importOPML(data)
}

// importOPML subscribes to the feeds of an OPML document, making the folders
// it names, and starts a refresh.
func (a *App) importOPML(data []byte) (FeedImportDTO, error) {
	subs, err := feeds.ParseOPML(data)
	if err != nil {
		return FeedImportDTO{}, fmt.Errorf("pelton: this file is not an OPML list: %w", err)
	}
	folders, err := a.store.ListFeedFolders(a.ctx)
	if err != nil {
		return FeedImportDTO{}, err
	}
	byName := map[string]int64{}
	for _, f := range folders {
		byName[strings.ToLower(f.Name)] = f.ID
	}
	var out FeedImportDTO
	for _, s := range subs {
		addr, err := normalizeFeedAddress(s.URL)
		if err != nil {
			out.Skipped++
			continue
		}
		var folderID int64
		if s.Folder != "" {
			id, ok := byName[strings.ToLower(s.Folder)]
			if !ok {
				made, err := a.store.CreateFeedFolder(a.ctx, s.Folder)
				if err != nil {
					return out, err
				}
				id = made.ID
				byName[strings.ToLower(s.Folder)] = id
			}
			folderID = id
		}
		f := &storage.Feed{URL: addr, FolderID: folderID, Title: s.Title, SiteURL: s.SiteURL}
		if err := a.store.CreateFeed(a.ctx, f); err != nil {
			if errors.Is(err, storage.ErrFeedExists) {
				out.Skipped++
				continue
			}
			return out, err
		}
		out.Added++
	}
	if out.Added > 0 {
		a.emit(EventFeedsChanged, FeedsChangedEvent{})
		a.refreshFeedsInBackground(false)
	}
	return out, nil
}

// ExportOPML writes every subscription to an OPML file the user picks and
// returns its path, or "" when they cancelled.
func (a *App) ExportOPML() (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	data, err := a.exportOPML()
	if err != nil {
		return "", err
	}
	path, err := a.saveFileDialog(saveDialogOptions{
		Title:           "Export feeds",
		DefaultFilename: "pelton-feeds.opml",
		Filters:         []fileFilter{{DisplayName: "OPML (*.opml)", Pattern: "*.opml"}},
	})
	if err != nil || path == "" {
		return "", err
	}
	return path, os.WriteFile(path, data, 0o644)
}

func (a *App) exportOPML() ([]byte, error) {
	folders, err := a.store.ListFeedFolders(a.ctx)
	if err != nil {
		return nil, err
	}
	names := map[int64]string{}
	for _, f := range folders {
		names[f.ID] = f.Name
	}
	list, err := a.store.ListFeeds(a.ctx)
	if err != nil {
		return nil, err
	}
	subs := make([]feeds.Subscription, 0, len(list))
	for _, pass := range append([]int64{0}, folderIDs(folders)...) {
		for _, f := range list {
			if f.FolderID == pass {
				subs = append(subs, feeds.Subscription{Title: f.DisplayTitle(), URL: f.URL, SiteURL: f.SiteURL, Folder: names[f.FolderID]})
			}
		}
	}
	return feeds.WriteOPML("Pelton feeds", subs, time.Now())
}

func folderIDs(list []storage.FeedFolder) []int64 {
	out := make([]int64, 0, len(list))
	for _, f := range list {
		out = append(out, f.ID)
	}
	return out
}

// --- newsletters ------------------------------------------------------------------

// ListNewsletterMail returns the newest mailing list mail, the newsletters the
// newsletter manager tracks, for the Newsletters view of the Feeds space. The
// rows are the mail itself: reading one marks the message read.
func (a *App) ListNewsletterMail(limit, offset int, unreadOnly bool) ([]MessageSummaryDTO, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	folders, err := a.newsletterFolders(true)
	if err != nil || len(folders) == 0 {
		return []MessageSummaryDTO{}, err
	}
	if limit <= 0 || limit > feedPageMax {
		limit = feedPageMax
	}
	q := storage.MessageQuery{FolderIDs: folders, ListMail: true, OneCopy: true, HideMuted: true, Limit: limit, Offset: offset}
	if unreadOnly {
		q.ExcludeFlags = storage.FlagSeen
	}
	msgs, err := a.store.QueryMessages(a.ctx, q)
	if err != nil {
		return nil, err
	}
	return a.buildSummaries(a.ctx, msgs)
}

// CountNewsletterMail counts the unread newsletter mail for the smart view.
func (a *App) CountNewsletterMail() (int, error) {
	if err := a.ready(); err != nil {
		return 0, err
	}
	folders, err := a.newsletterFolders(true)
	if err != nil || len(folders) == 0 {
		return 0, err
	}
	return a.store.CountMessages(a.ctx, storage.MessageQuery{FolderIDs: folders, ListMail: true, ExcludeFlags: storage.FlagSeen, HideMuted: true})
}
