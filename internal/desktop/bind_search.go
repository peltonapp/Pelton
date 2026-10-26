package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/mailview"
	"github.com/peltonapp/Pelton/internal/search"
	"github.com/peltonapp/Pelton/internal/storage"
)

// settingSearchWatermark records the highest message id already indexed, so the
// search backfill only walks newly synced rows instead of reindexing everything.
const settingSearchWatermark = "search_indexed_max_id"

// indexFileName is the on-disk Bleve index directory, kept next to the database.
const indexFileName = "search.bleve"

// settingSearchIndexVersion records which projection the index was built with.
// Bumping searchIndexVersion rebuilds the index from the cache, which is the
// only way a change to what gets indexed reaches mail that was synced under the
// old projection.
const settingSearchIndexVersion = "search_index_version"

// searchIndexVersion 2 indexes a text rendering of the html body for messages
// with no text/plain part. Those were previously indexed with an empty body, so
// their text was visible in the list snippet but could never be found.
//
// Version 3 adds the sortable subject field the alphabetical search orders on
// (#404). That one is a mapping change rather than a projection change, and
// Bleve fixes a mapping when the index is created: writing the new field into
// an index built without it would have it dynamically mapped as analyzed text,
// which sorts by whichever word comes first alphabetically. Hence the rebuild
// rather than a reindex in place.
const searchIndexVersion = 3

// searchBatchSize bounds how many messages are read and indexed per commit during
// a backfill.
const searchBatchSize = 500

// openSearchIndex opens (creating it if needed) the Bleve index alongside the
// database, in dataDir (see openStore).
func openSearchIndex(dataDir string) (*search.Index, error) {
	return search.Open(filepath.Join(dataDir, indexFileName))
}

// backfillSearch brings the index up to date with the cached messages. It runs at
// startup and is cheap once the watermark has caught up. When the index was built
// under an older version it is discarded and built again from the cache first,
// since a mapping only takes effect on a new index.
func (a *App) backfillSearch() {
	if a.index == nil {
		return
	}
	want := strconv.Itoa(searchIndexVersion)
	if a.stringSetting(settingSearchIndexVersion, "0") != want {
		if err := a.rebuildSearchIndex(); err != nil {
			// leave the version unstamped so the next startup tries again rather
			// than leaving the index permanently half-built.
			return
		}
		if err := a.store.Set(a.ctx, settingSearchIndexVersion, want); err != nil {
			a.log.Error("persist search index version", "err", err)
		}
		return
	}
	// indexNewMessages logs whatever went wrong itself.
	_ = a.indexNewMessages()
}

// indexNewMessages brings the index up to date in two stages, and is safe to
// call repeatedly (after each sync and each stored body batch); the mutex
// keeps concurrent passes from racing on the watermark.
//
//  1. Every row past the stored watermark, in id order. A stub (no body yet)
//     goes in by its headers and preview, so old mail outside the sync
//     window is still found by subject or sender.
//  2. Rows at or below the watermark whose index entry is behind their
//     text (storage.ListMessagesForReindex): a complete row whose body is
//     not indexed yet (search_state < full: a body that landed after its
//     stub was indexed, in any order, a list refresh or repair that changed
//     it, or a run that died before its index write), and a stub not indexed
//     (a list refresh changed its headers). Bleve replaces the document by
//     id, so the old text leaves the index.
//
// search_state is written only after the index write succeeded, so a failed
// or interrupted pass is redone by the next one from the data alone.
func (a *App) indexNewMessages() error {
	if a.index == nil {
		return nil
	}
	a.searchMu.Lock()
	defer a.searchMu.Unlock()

	watermark := a.searchWatermark()
	for {
		msgs, err := a.store.ListMessagesForIndex(a.ctx, watermark, searchBatchSize)
		if err != nil {
			a.log.Error("read messages for search index", "err", err)
			return err
		}
		if len(msgs) == 0 {
			break
		}
		if err := a.indexBatch(msgs); err != nil {
			return err
		}
		watermark = max(watermark, msgs[len(msgs)-1].ID)
		if err := a.store.Set(a.ctx, settingSearchWatermark, strconv.FormatInt(watermark, 10)); err != nil {
			a.log.Error("persist search watermark", "err", err)
		}
		if len(msgs) < searchBatchSize {
			break
		}
	}

	var after int64
	for {
		msgs, err := a.store.ListMessagesForReindex(a.ctx, after, watermark, searchBatchSize)
		if err != nil {
			a.log.Error("read filled messages for search index", "err", err)
			return err
		}
		if len(msgs) == 0 {
			return nil
		}
		if err := a.indexBatch(msgs); err != nil {
			return err
		}
		after = msgs[len(msgs)-1].ID
		if len(msgs) < searchBatchSize {
			return nil
		}
	}
}

// indexBatch writes msgs to the index in one Bleve batch and then records
// what each row now has indexed: headers for a stub, the full body
// otherwise.
func (a *App) indexBatch(msgs []storage.Message) error {
	docs := make([]search.Doc, 0, len(msgs))
	// What each row was when it was read: a row a fill or repair rewrote
	// meanwhile has a newer text_rev, keeps its state (RecordSearchState) and
	// is indexed again.
	var full, headers []storage.IndexRead
	for _, m := range msgs {
		doc := toSearchDoc(m)
		doc.Body = a.searchBody(m)
		docs = append(docs, doc)
		if m.BodyComplete {
			full = append(full, m.IndexRead())
		} else {
			headers = append(headers, m.IndexRead())
		}
	}
	if err := a.index.IndexBatch(docs); err != nil {
		a.log.Error("index message batch", "err", err)
		return err
	}
	// a state that fails to persist only means the row is indexed again.
	if err := a.store.RecordSearchState(a.ctx, full, storage.SearchFull); err != nil {
		a.log.Error("record search state", "err", err)
	}
	if err := a.store.RecordSearchState(a.ctx, headers, storage.SearchHeaders); err != nil {
		a.log.Error("record search state", "err", err)
	}
	return nil
}

// reindexMessages replaces the index entries of messages whose stored text
// changed after they were first indexed. The incremental pass only ever walks
// forward from the watermark, so a message repaired in place would otherwise
// stay searchable by its old broken text.
func (a *App) reindexMessages(ids []int64) {
	if a.index == nil || len(ids) == 0 {
		return
	}
	a.searchMu.Lock()
	defer a.searchMu.Unlock()

	msgs := make([]storage.Message, 0, len(ids))
	for _, id := range ids {
		m, err := a.store.GetMessage(a.ctx, id)
		if err != nil {
			a.log.Error("read repaired message for search index", "id", id, "err", err)
			continue
		}
		msgs = append(msgs, *m)
	}
	// indexBatch logs a failure; the next pass picks the rows up again from
	// their search_state.
	_ = a.indexBatch(msgs)
}

// searchWatermark reads the highest indexed message id, defaulting to 0 (index
// everything) when unset or unparsable.
func (a *App) searchWatermark() int64 {
	raw := a.stringSetting(settingSearchWatermark, "0")
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0
	}
	return v
}

// toSearchDoc projects a stored message into the search document. The sender name
// and address are combined so a search for either finds the mail.
// rebuildSearchIndex discards the index and builds it again from the cache.
// Used when what gets indexed changes in a way that overwriting documents would
// not undo, which is the case for decrypted text: a plaintext already written
// has to actually leave the index, not merely stop being added.
// It reports whether the rebuild finished, so a caller that stamps something
// against the new index (the version) can tell a rebuilt index apart from one
// that was left half-built.
func (a *App) rebuildSearchIndex() error {
	if a.index == nil {
		return nil
	}
	a.searchMu.Lock()
	if err := a.index.Close(); err != nil {
		a.log.Error("close search index for rebuild", "err", err)
	}
	path := filepath.Join(a.dataDir, indexFileName)
	if err := os.RemoveAll(path); err != nil {
		a.log.Error("remove search index for rebuild", "err", err)
	}
	idx, err := search.Open(path)
	if err != nil {
		a.log.Error("reopen search index after rebuild", "err", err)
		a.searchMu.Unlock()
		return err
	}
	a.index = idx
	if err := a.store.Set(a.ctx, settingSearchWatermark, "0"); err != nil {
		a.log.Error("rewind search watermark for rebuild", "err", err)
	}
	a.searchMu.Unlock()

	if err := a.indexNewMessages(); err != nil {
		a.log.Error("rebuild search index", "err", err)
		return err
	}
	return nil
}

// searchBody returns the text search should index for a message. Encrypted mail
// is indexed by its armor, which finds nothing, unless the user has opted in to
// indexing the decrypted text and the key is available to produce it.
//
// A locked key means the message is indexed unopened. Nothing re-indexes it
// when the key is later unlocked, which is a real limit rather than an
// oversight: re-indexing on unlock would mean decrypting the whole mailbox at
// the moment a passphrase is typed.
func (a *App) searchBody(m storage.Message) string {
	body := m.BodyPlain
	if strings.TrimSpace(body) == "" {
		body = mailview.PlainText(m.BodyHTML)
	}
	// a stub's body_plain is the list preview; there is nothing to decrypt.
	if !m.BodyComplete || !a.boolSetting(settingIndexDecrypted, false) {
		return body
	}
	opened, _, state, _ := a.openProtected(m)
	if state != pgpStateOpen {
		return body
	}
	return opened
}

func toSearchDoc(m storage.Message) search.Doc {
	// html-only mail carries no text/plain part, so indexing body_plain alone
	// left roughly a seventh of a real mailbox with an empty body: the text
	// showed in the list snippet (which already falls back to the html) but
	// could never be found.
	body := m.BodyPlain
	if strings.TrimSpace(body) == "" {
		body = mailview.PlainText(m.BodyHTML)
	}
	return search.Doc{
		ID:        m.ID,
		AccountID: m.AccountID,
		FolderID:  m.FolderID,
		Subject:   m.Subject,
		From:      strings.TrimSpace(m.FromName + " " + m.FromAddress),
		To:        m.ToAddresses,
		Cc:        m.CcAddresses,
		Body:      body,
		Date:      m.Date,
	}
}

// SearchRequestDTO is a search query from the ui: free text plus an optional date
// window. AfterUnix/BeforeUnix are unix seconds; 0 means that side is open.
type SearchRequestDTO struct {
	Query      string `json:"query"`
	AfterUnix  int64  `json:"afterUnix"`
	BeforeUnix int64  `json:"beforeUnix"`
	Limit      int    `json:"limit"`
	// Offset skips that many ranked hits, so the list can page through a large
	// result set instead of stopping at the first page.
	Offset int `json:"offset"`
	// From/To/Subject scope the match to a field, from typed search chips.
	From    string `json:"from"`
	To      string `json:"to"`
	Subject string `json:"subject"`
	// HasAttachment filters to messages that carry at least one attachment.
	HasAttachment bool `json:"hasAttachment"`
	// UnreadOnly filters to messages that have not been read.
	UnreadOnly bool `json:"unreadOnly"`
	// Sort is the order results come back in: relevance, newest, oldest,
	// subjectAsc or subjectDesc. Empty means relevance. The ui resolves its
	// "automatic" setting to one of these before asking, so the choice of what
	// suits a given query stays in one place rather than being split across the
	// two sides.
	Sort string `json:"sort"`
}

// searchSort maps the ui's sort name onto the index's. An unknown name is left
// to the index, which falls back to relevance.
func searchSort(name string) search.Sort {
	return search.Sort(strings.TrimSpace(name))
}

// SearchResultDTO is one page of search results. Total is how many documents
// matched, which the list needs to tell "that is everything" apart from "there
// is more below": without it a full page of hits is indistinguishable from a
// truncated one, which is what made search look like it lost mail.
//
// Total counts index matches, so it is an upper bound on what the list shows:
// the attachment and unread filters, and any message deleted since it was
// indexed, are applied after ranking, per page.
type SearchResultDTO struct {
	Messages []MessageSummaryDTO `json:"messages"`
	Total    int                 `json:"total"`
}

// Search runs a ranked, typo-tolerant search over cached messages and returns a
// page of matching summaries in relevance order. An empty query with a date
// window lists messages in that window; an empty request returns nothing.
func (a *App) Search(req SearchRequestDTO) (SearchResultDTO, error) {
	empty := SearchResultDTO{Messages: []MessageSummaryDTO{}}
	if err := a.ready(); err != nil {
		return empty, err
	}
	if a.index == nil {
		return empty, errSearchUnavailable
	}
	q := search.Query{
		Text:    strings.TrimSpace(req.Query),
		From:    strings.TrimSpace(req.From),
		To:      strings.TrimSpace(req.To),
		Subject: strings.TrimSpace(req.Subject),
		Limit:   req.Limit,
		Offset:  req.Offset,
		Sort:    searchSort(req.Sort),
	}
	if req.AfterUnix > 0 {
		q.After = time.Unix(req.AfterUnix, 0)
	}
	if req.BeforeUnix > 0 {
		q.Before = time.Unix(req.BeforeUnix, 0)
	}
	// an empty request (no text, no field chip, no date, no flag filter) means
	// "show the normal list", so return nothing here.
	if q.Text == "" && q.From == "" && q.To == "" && q.Subject == "" &&
		q.After.IsZero() && q.Before.IsZero() && !req.HasAttachment && !req.UnreadOnly {
		return empty, nil
	}

	res, err := a.index.Search(q)
	if err != nil {
		return empty, err
	}

	// hits are ranked; fetch each full message so rows render like the normal
	// list. a missing message (deleted since indexing) is simply skipped, which
	// also covers stale index entries without a separate cleanup pass. the
	// has:attachment and is:unread chips are applied here: attachments and read
	// state are stored message fields, not indexed ones. Read state is also the
	// one filter that goes stale the moment it is used, since opening a result
	// marks it read, so reading it from the store rather than the index is what
	// keeps it honest.
	vips := a.vipSet()
	out := make([]MessageSummaryDTO, 0, len(res.Hits))
	for _, h := range res.Hits {
		m, err := a.store.GetMessage(a.ctx, h.ID)
		if errors.Is(err, storage.ErrMessageNotFound) {
			// deleted since it was indexed; nothing removes documents when
			// mail is deleted, so drop the stale one now. Ids are never
			// reused (AUTOINCREMENT), so it can only cost Total.
			if derr := a.index.Delete(h.ID); derr != nil {
				a.log.Warn("drop deleted message from search index", "id", h.ID, "err", derr)
			}
			continue
		}
		if err != nil {
			continue
		}
		if req.HasAttachment && !m.HasAttachments {
			continue
		}
		if req.UnreadOnly && m.Flags.Has(storage.FlagSeen) {
			continue
		}
		email, folderName := a.lookupContext(a.ctx, m.AccountID, m.FolderID)
		dto := toSummaryDTO(*m, email, folderName)
		dto.SenderVIP = vips[bareAddress(m.FromAddress)]
		out = append(out, dto)
	}
	return SearchResultDTO{Messages: out, Total: int(res.Total)}, nil
}
