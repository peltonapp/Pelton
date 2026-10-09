package desktop

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/search"
	"github.com/peltonapp/Pelton/internal/storage"
)

// maxViewMatches caps how many messages a single view resolves to. It bounds the
// per-view eager-run cost (counts run on startup and after every sync) and the
// warm result set. A view that would match more is truncated newest-first.
const maxViewMatches = 500

// ViewDTO is the settings/sidebar view of a saved search: its definition plus the
// eager-run counts. AccountID is 0 for "all accounts".
type ViewDTO struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Icon  string `json:"icon"`
	Color string `json:"color"`

	// QueryFrom and QueryTo are address chips, matched as OR within the field.
	QueryText    string   `json:"queryText"`
	QueryFrom    []string `json:"queryFrom"`
	QueryTo      []string `json:"queryTo"`
	QuerySubject string   `json:"querySubject"`
	WithinDays   int      `json:"withinDays"`

	// UseRegex treats the text criteria as regular expressions.
	UseRegex bool `json:"useRegex"`

	UnreadOnly    bool  `json:"unreadOnly"`
	FlaggedOnly   bool  `json:"flaggedOnly"`
	HasAttachment bool  `json:"hasAttachment"`
	AccountID     int64 `json:"accountId"`

	Position    int `json:"position"`
	UnreadCount int `json:"unreadCount"`
	TotalCount  int `json:"totalCount"`
}

// ListViews returns every saved view with its current eager-run counts, ordered
// for the sidebar.
func (a *App) ListViews() ([]ViewDTO, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	views, err := a.store.ListViews(a.ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ViewDTO, 0, len(views))
	for _, v := range views {
		total, unread := a.viewCounts(a.ctx, v)
		dto := viewToDTO(v)
		dto.TotalCount = total
		dto.UnreadCount = unread
		out = append(out, dto)
	}
	return out, nil
}

// SaveView creates a view (id 0) or updates an existing one, then returns the
// saved view with fresh counts. A blank name is rejected.
func (a *App) SaveView(dto ViewDTO) (ViewDTO, error) {
	if err := a.ready(); err != nil {
		return ViewDTO{}, err
	}
	if strings.TrimSpace(dto.Name) == "" {
		return ViewDTO{}, fmt.Errorf("view name is required")
	}
	v := dtoToView(dto)
	if _, err := buildViewFilter(v); err != nil {
		return ViewDTO{}, fmt.Errorf("invalid regular expression: %w", err)
	}
	if v.ID == 0 {
		if _, err := a.store.CreateView(a.ctx, &v); err != nil {
			return ViewDTO{}, err
		}
	} else if err := a.store.UpdateView(a.ctx, &v); err != nil {
		return ViewDTO{}, err
	}

	a.emit(EventViewsChanged, nil)
	total, unread := a.viewCounts(a.ctx, v)
	out := viewToDTO(v)
	out.TotalCount = total
	out.UnreadCount = unread
	return out, nil
}

// DeleteView removes a saved view.
func (a *App) DeleteView(id int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	if err := a.store.DeleteView(a.ctx, id); err != nil {
		return err
	}
	a.emit(EventViewsChanged, nil)
	return nil
}

// ReorderViews persists a new sidebar order for the given view ids.
func (a *App) ReorderViews(orderedIDs []int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	if err := a.store.SetViewPositions(a.ctx, orderedIDs); err != nil {
		return err
	}
	a.emit(EventViewsChanged, nil)
	return nil
}

// refreshViewCounts recomputes every view's counts and notifies the ui. It is
// the eager-run entry point, called on startup and after each sync. It runs off
// the caller's hot path (sync) via a goroutine at the call sites.
func (a *App) refreshViewCounts() {
	if err := a.ready(); err != nil {
		return
	}
	views, err := a.store.ListViews(a.ctx)
	if err != nil {
		a.log.Error("refresh view counts", "err", err)
		return
	}
	if len(views) == 0 {
		return
	}
	a.emit(EventViewsChanged, nil)
}

// viewCounts returns the total and unread match counts for a view, swallowing
// errors as zero so one broken view never blanks the whole sidebar.
func (a *App) viewCounts(ctx context.Context, v storage.View) (total, unread int) {
	matches, err := a.runViewMatches(ctx, v)
	if err != nil {
		a.log.Error("run view", "view", v.Name, "err", err)
		return 0, 0
	}
	for _, m := range matches {
		if !m.Flags.Has(storage.FlagSeen) {
			unread++
		}
	}
	return len(matches), unread
}

// maxViewScan bounds how many recent messages the scan-and-filter path reads
// per run. Regex and multi-address views cannot use the full-text index, so they
// scan the newest messages in scope and match in Go. Matches are still capped at
// maxViewMatches; this only bounds how far back the scan looks.
const maxViewScan = 2000

// runViewMatches resolves a view to its matching messages, newest first. Plain
// single-address text criteria go through the full-text index. Regex or
// multi-address criteria scan the newest messages in scope and match in Go. A
// pure scope view reads the store directly. Scope, date and hygiene filters are
// then applied in Go, since flags are not indexed.
func (a *App) runViewMatches(ctx context.Context, v storage.View) ([]storage.Message, error) {
	var after time.Time
	if v.WithinDays > 0 {
		after = time.Now().Add(-time.Duration(v.WithinDays) * 24 * time.Hour)
	}

	froms := splitAddrs(v.QueryFrom)
	tos := splitAddrs(v.QueryTo)
	hasText := v.QueryText != "" || len(froms) > 0 || len(tos) > 0 || v.QuerySubject != ""
	canUseIndex := hasText && !v.UseRegex && len(froms) <= 1 && len(tos) <= 1

	var (
		candidates []storage.Message
		filter     *viewFilter
	)
	switch {
	case canUseIndex:
		if a.index == nil {
			return nil, errSearchUnavailable
		}
		hits, err := a.index.Search(search.Query{
			Text:    v.QueryText,
			From:    firstOrEmpty(froms),
			To:      firstOrEmpty(tos),
			Subject: v.QuerySubject,
			After:   after,
			Limit:   maxViewMatches,
		})
		if err != nil {
			return nil, err
		}
		for _, h := range hits.Hits {
			m, err := a.store.GetMessage(ctx, h.ID)
			if err != nil {
				continue
			}
			candidates = append(candidates, *m)
		}
	case hasText:
		// regex or multiple from/to addresses: the index cannot express these, so
		// scan the newest messages in scope and match in Go.
		f, err := buildViewFilter(v)
		if err != nil {
			// a view whose regex no longer compiles matches nothing rather than
			// breaking the sidebar.
			a.log.Error("compile view filter", "view", v.Name, "err", err)
			return nil, nil
		}
		filter = f
		folderIDs, err := a.viewScopeFolderIDs(ctx, v.AccountID)
		if err != nil {
			return nil, err
		}
		msgs, err := a.store.QueryMessages(ctx, storage.MessageQuery{FolderIDs: folderIDs, Limit: maxViewScan})
		if err != nil {
			return nil, err
		}
		candidates = msgs
	default:
		folderIDs, err := a.viewScopeFolderIDs(ctx, v.AccountID)
		if err != nil {
			return nil, err
		}
		msgs, err := a.store.QueryMessages(ctx, storage.MessageQuery{FolderIDs: folderIDs, Limit: maxViewMatches})
		if err != nil {
			return nil, err
		}
		candidates = msgs
	}

	out := make([]storage.Message, 0, len(candidates))
	for _, m := range candidates {
		if filter != nil && !filter.matches(m) {
			continue
		}
		if m.SnoozeHidden {
			continue
		}
		if v.AccountID != 0 && m.AccountID != v.AccountID {
			continue
		}
		if v.UnreadOnly && m.Flags.Has(storage.FlagSeen) {
			continue
		}
		if v.FlaggedOnly && !m.Flags.Has(storage.FlagFlagged) {
			continue
		}
		if v.HasAttachment && !m.HasAttachments {
			continue
		}
		if !after.IsZero() && m.Date.Before(after) {
			continue
		}
		out = append(out, m)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Date.After(out[j].Date) })
	if len(out) > maxViewMatches {
		out = out[:maxViewMatches]
	}
	return out, nil
}

// viewScopeFolderIDs returns the folder ids a scope-only view spans: one
// account's folders, or every account's when accountID is 0.
func (a *App) viewScopeFolderIDs(ctx context.Context, accountID int64) ([]int64, error) {
	var accountIDs []int64
	if accountID != 0 {
		accountIDs = []int64{accountID}
	} else {
		accounts, err := a.store.ListAccounts(ctx)
		if err != nil {
			return nil, err
		}
		for _, acc := range accounts {
			accountIDs = append(accountIDs, acc.ID)
		}
	}

	var folderIDs []int64
	for _, id := range accountIDs {
		folders, err := a.store.ListFolders(ctx, id)
		if err != nil {
			return nil, err
		}
		for _, f := range folders {
			folderIDs = append(folderIDs, f.ID)
		}
	}
	return folderIDs, nil
}

// listSavedView returns a page of a saved view's resolved messages. It backs the
// "savedView" list kind.
func (a *App) listSavedView(ctx context.Context, viewID int64, limit, offset int) (MessageListDTO, error) {
	v, err := a.store.GetView(ctx, viewID)
	if err != nil {
		return MessageListDTO{}, err
	}
	matches, err := a.runViewMatches(ctx, *v)
	if err != nil {
		return MessageListDTO{}, err
	}

	total := len(matches)
	if offset > total {
		offset = total
	}
	end := total
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	page := matches[offset:end]

	summaries, err := a.buildSummaries(ctx, page)
	if err != nil {
		return MessageListDTO{}, err
	}
	return MessageListDTO{Messages: summaries, Total: total}, nil
}

func viewToDTO(v storage.View) ViewDTO {
	return ViewDTO{
		ID:            v.ID,
		Name:          v.Name,
		Icon:          v.Icon,
		Color:         v.Color,
		QueryText:     v.QueryText,
		QueryFrom:     splitAddrs(v.QueryFrom),
		QueryTo:       splitAddrs(v.QueryTo),
		QuerySubject:  v.QuerySubject,
		WithinDays:    v.WithinDays,
		UseRegex:      v.UseRegex,
		UnreadOnly:    v.UnreadOnly,
		FlaggedOnly:   v.FlaggedOnly,
		HasAttachment: v.HasAttachment,
		AccountID:     v.AccountID,
		Position:      v.Position,
	}
}

func dtoToView(d ViewDTO) storage.View {
	return storage.View{
		ID:            d.ID,
		Name:          strings.TrimSpace(d.Name),
		Icon:          d.Icon,
		Color:         d.Color,
		QueryText:     strings.TrimSpace(d.QueryText),
		QueryFrom:     joinAddrs(d.QueryFrom),
		QueryTo:       joinAddrs(d.QueryTo),
		QuerySubject:  strings.TrimSpace(d.QuerySubject),
		WithinDays:    d.WithinDays,
		UseRegex:      d.UseRegex,
		UnreadOnly:    d.UnreadOnly,
		FlaggedOnly:   d.FlaggedOnly,
		HasAttachment: d.HasAttachment,
		AccountID:     d.AccountID,
		Position:      d.Position,
	}
}

// addrSep separates the addresses stored in a view's from/to field.
const addrSep = "\n"

// splitAddrs turns a stored newline list into trimmed, non-empty entries.
func splitAddrs(s string) []string {
	var out []string
	for line := range strings.SplitSeq(s, addrSep) {
		if t := strings.TrimSpace(line); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// joinAddrs stores a chip list back as a newline-separated string, dropping
// blanks so an empty field stays truly empty.
func joinAddrs(list []string) string {
	cleaned := make([]string, 0, len(list))
	for _, a := range list {
		if t := strings.TrimSpace(a); t != "" {
			cleaned = append(cleaned, t)
		}
	}
	return strings.Join(cleaned, addrSep)
}

func firstOrEmpty(list []string) string {
	if len(list) == 0 {
		return ""
	}
	return list[0]
}

// matcher tests one criterion against a haystack: a compiled regexp when the
// view uses regex, otherwise a case-insensitive substring.
type matcher struct {
	re     *regexp.Regexp
	needle string
}

func newMatcher(pattern string, regex bool) (matcher, error) {
	if regex {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return matcher{}, err
		}
		return matcher{re: re}, nil
	}
	return matcher{needle: strings.ToLower(pattern)}, nil
}

func (m matcher) match(s string) bool {
	if m.re != nil {
		return m.re.MatchString(s)
	}
	return strings.Contains(strings.ToLower(s), m.needle)
}

// viewFilter is the compiled text criteria of a view for the scan-and-filter
// path. Fields present are ANDed; the from/to chip lists are ORed within their
// field. A message matches when every present field matches.
type viewFilter struct {
	text    *matcher
	subject *matcher
	from    []matcher
	to      []matcher
}

// buildViewFilter compiles a view's text criteria, returning an error only when
// a regex fails to compile.
func buildViewFilter(v storage.View) (*viewFilter, error) {
	f := &viewFilter{}
	if v.QueryText != "" {
		m, err := newMatcher(v.QueryText, v.UseRegex)
		if err != nil {
			return nil, err
		}
		f.text = &m
	}
	if v.QuerySubject != "" {
		m, err := newMatcher(v.QuerySubject, v.UseRegex)
		if err != nil {
			return nil, err
		}
		f.subject = &m
	}
	for _, a := range splitAddrs(v.QueryFrom) {
		m, err := newMatcher(a, v.UseRegex)
		if err != nil {
			return nil, err
		}
		f.from = append(f.from, m)
	}
	for _, a := range splitAddrs(v.QueryTo) {
		m, err := newMatcher(a, v.UseRegex)
		if err != nil {
			return nil, err
		}
		f.to = append(f.to, m)
	}
	return f, nil
}

func (f *viewFilter) matches(m storage.Message) bool {
	if f.text != nil {
		hay := m.Subject + "\n" + m.BodyPlain + "\n" + m.FromName + " " + m.FromAddress + "\n" + m.ToAddresses + " " + m.CcAddresses
		if !f.text.match(hay) {
			return false
		}
	}
	if f.subject != nil && !f.subject.match(m.Subject) {
		return false
	}
	if len(f.from) > 0 && !anyMatch(f.from, m.FromName+" "+m.FromAddress) {
		return false
	}
	if len(f.to) > 0 && !anyMatch(f.to, m.ToAddresses+" "+m.CcAddresses) {
		return false
	}
	return true
}

func anyMatch(ms []matcher, s string) bool {
	for _, m := range ms {
		if m.match(s) {
			return true
		}
	}
	return false
}
