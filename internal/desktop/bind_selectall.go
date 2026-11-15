package desktop

// selectAllCap is the most ids one select-all will hand back. A selection is an
// id list all the way through to the bulk actions, so this is what keeps a
// mailbox of a hundred thousand messages from becoming a hundred thousand
// element array in the frontend. The ui says when it stopped short.
const selectAllCap = 50000

// MessageIDsDTO is every message id a list matches, newest first, plus whether
// the cap cut it short so the ui can say so rather than quietly selecting less
// than it claimed.
type MessageIDsDTO struct {
	IDs      []int64 `json:"ids"`
	Capped   bool    `json:"capped"`
	Matching int     `json:"matching"`
}

// MessageIDs returns the ids of every message in a list, ignoring paging. It is
// what select-all uses: the list in the ui holds only the pages that were
// scrolled to, so selecting a whole mailbox has to come from the query rather
// than from what is on screen (#320).
func (a *App) MessageIDs(req ListMessagesRequest) (MessageIDsDTO, error) {
	if err := a.ready(); err != nil {
		return MessageIDsDTO{}, err
	}
	if req.Kind == "savedView" {
		return a.savedViewIDs(req.ViewID)
	}
	q, err := a.requestQuery(a.ctx, req)
	if err != nil {
		return MessageIDsDTO{}, err
	}
	ids, err := a.store.QueryMessageIDs(a.ctx, q)
	if err != nil {
		return MessageIDsDTO{}, err
	}
	return capIDs(ids), nil
}

// savedViewIDs runs a saved view and takes the ids off its matches. The view
// runner already returns every match rather than a page, so there is nothing to
// widen here.
func (a *App) savedViewIDs(viewID int64) (MessageIDsDTO, error) {
	v, err := a.store.GetView(a.ctx, viewID)
	if err != nil {
		return MessageIDsDTO{}, err
	}
	matches, err := a.runViewMatches(a.ctx, *v)
	if err != nil {
		return MessageIDsDTO{}, err
	}
	ids := make([]int64, 0, len(matches))
	for _, m := range matches {
		ids = append(ids, m.ID)
	}
	return capIDs(ids), nil
}

// SearchMessageIDs is MessageIDs for a search result set. It runs the same
// query the result list came from and keeps only the ids, so selecting every
// match does not pay for building summaries of messages nobody is looking at.
func (a *App) SearchMessageIDs(req SearchRequestDTO) (MessageIDsDTO, error) {
	if err := a.ready(); err != nil {
		return MessageIDsDTO{}, err
	}
	if a.index == nil {
		return MessageIDsDTO{}, errSearchUnavailable
	}
	// the same order the list is showing. The cap means this can be a prefix of
	// the matches rather than all of them, and "select everything" should take
	// the ones the user can see, not a different slice ranked another way.
	req.Offset = 0
	q, ok := req.searchQuery(selectAllCap)
	if !ok {
		return MessageIDsDTO{IDs: []int64{}}, nil
	}
	if err := a.restrictToNotes(&q, req); err != nil {
		return MessageIDsDTO{}, err
	}

	res, err := a.index.Search(q)
	if err != nil {
		return MessageIDsDTO{}, err
	}
	ids := make([]int64, 0, len(res.Hits))
	for _, h := range res.Hits {
		// the stored-field filters are message fields rather than indexed ones,
		// so they are applied here exactly as the result list applies them. A
		// hit whose message is gone is skipped, which also covers stale index
		// entries.
		if req.hasStoredFilter() {
			m, err := a.store.GetMessage(a.ctx, h.ID)
			if err != nil || !req.keeps(m) {
				continue
			}
		}
		ids = append(ids, h.ID)
	}
	return capIDs(ids), nil
}

// capIDs trims a select-all to the cap and reports the untrimmed size, so the
// ui can say "the first 50,000" instead of pretending it selected everything.
func capIDs(ids []int64) MessageIDsDTO {
	out := MessageIDsDTO{IDs: ids, Matching: len(ids)}
	if len(ids) > selectAllCap {
		out.IDs = ids[:selectAllCap]
		out.Capped = true
	}
	if out.IDs == nil {
		out.IDs = []int64{}
	}
	return out
}
