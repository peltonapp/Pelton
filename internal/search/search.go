// Package search is Pelton's local full-text search over cached messages, built
// on Bleve. It replaces the sqlite fts5 path with a ranked, typo-tolerant index:
// queries match across subject, sender, recipients and body with fuzzy matching
// so small spelling slips still find mail, and results come back scored by
// relevance. The index lives on disk next to the database and is kept current by
// the desktop layer (incremental indexing on sync, with a startup backfill).
package search

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search/query"
)

// field boosts weight a hit by where the term matched: a subject or sender match
// ranks above a body match.
const (
	boostSubject = 3.0
	boostFrom    = 2.0
	boostRecip   = 1.5
	boostBody    = 1.0

	// fuzziness is the edit distance tolerated per term (1 catches most typos
	// without drowning results in noise).
	fuzziness = 1

	// fuzzyPrefix pins the first character of a fuzzy term. Without it an edit
	// distance of 1 wanders onto unrelated words that merely rhyme.
	fuzzyPrefix = 1

	// minFuzzyTerm is the shortest term worth fuzzing. Below it, one edit covers
	// a large share of the vocabulary, and the noise pushes the exact match out
	// of the page the user is looking at.
	minFuzzyTerm = 4

	// boostPrefix ranks a "still typing" prefix hit below a whole-term match, so
	// completing the word promotes the exact result rather than reshuffling.
	boostPrefix = 0.5

	// minPrefixTerm is the shortest term that gets prefix matching. One or two
	// characters match too much of the index to be worth ranking.
	minPrefixTerm = 3

	defaultLimit = 50
)

// Doc is the indexable projection of a message. Only the fields worth searching
// are kept; the id ties a hit back to the stored message.
type Doc struct {
	ID        int64
	AccountID int64
	FolderID  int64
	Subject   string
	From      string
	To        string
	Cc        string
	Body      string
	Date      time.Time
}

// Sort is the order a page of results comes back in.
type Sort string

const (
	// SortRelevance ranks by score, which is what a free-text query wants.
	SortRelevance Sort = "relevance"
	// SortNewest and SortOldest order by message date, ignoring score. With no
	// free text there is nothing meaningful to score, so these are what a
	// query built only from chips should use.
	SortNewest Sort = "newest"
	SortOldest Sort = "oldest"
	// SortSubjectAsc and SortSubjectDesc order alphabetically by subject.
	SortSubjectAsc  Sort = "subjectAsc"
	SortSubjectDesc Sort = "subjectDesc"
)

// fieldSubjectSort is the sortable copy of the subject: one keyword term rather
// than the analyzed tokens the searchable subject field holds. Sorting on an
// analyzed field orders by whichever of its terms sorts first, which for a
// subject is close to arbitrary.
const fieldSubjectSort = "subject_sort"

// sortOrders maps a Sort onto the Bleve sort spec it runs as.
//
// Every order ends in _id so it is total. Anything less is not a detail: equal
// sort keys are the normal case here, not an edge case (every message in a
// thread shares a subject, a newsletter arrives on the same score every month),
// and Bleve's order among equal keys is not stable between queries. Paging by
// offset over an unstable order returns one message on two pages and never
// returns another at all.
var sortOrders = map[Sort][]string{
	SortRelevance:   {"-_score", "-date", "_id"},
	SortNewest:      {"-date", "_id"},
	SortOldest:      {"date", "_id"},
	SortSubjectAsc:  {fieldSubjectSort, "-date", "_id"},
	SortSubjectDesc: {"-" + fieldSubjectSort, "-date", "_id"},
}

// Query is a search request: free text plus optional field constraints and a
// date window. The field constraints (From/To/Subject) come from typed search
// chips (from:, to:, subject:) and each narrows the results to that field; they
// are conjoined with the free text and each other.
type Query struct {
	Text string
	// From/To/Subject scope the match to a single field when non-empty. To also
	// matches the cc field.
	From    string
	To      string
	Subject string
	// After/Before bound the message date. A zero time means the side is open.
	After  time.Time
	Before time.Time
	Limit  int
	// Offset skips that many ranked hits, so the caller can page instead of
	// silently losing everything past the first page.
	Offset int
	// Sort is the order results come back in. An empty or unrecognized value
	// means relevance, which is what every caller wanted before there was a
	// choice.
	Sort Sort
}

// Hit is one search result: the message id and its relevance score.
type Hit struct {
	ID    int64
	Score float64
}

// Results is one page of hits plus how many documents matched in total. Callers
// need the total to tell "these are all the matches" apart from "this is the
// first page of many", which is the difference between a trustworthy search and
// one that looks like it lost your mail.
type Results struct {
	Hits  []Hit
	Total uint64
}

// Index is the Bleve-backed search index. It is safe for concurrent use; Bleve
// serializes writes internally.
type Index struct {
	idx bleve.Index
}

// Open opens the index at path, creating it with the message mapping if it does
// not exist yet.
func Open(path string) (*Index, error) {
	idx, err := bleve.Open(path)
	if err == bleve.ErrorIndexPathDoesNotExist {
		idx, err = bleve.New(path, buildMapping())
	}
	if err != nil {
		return nil, fmt.Errorf("search: open index %q: %w", path, err)
	}
	return &Index{idx: idx}, nil
}

// Close releases the index.
func (i *Index) Close() error {
	return i.idx.Close()
}

// DocCount reports how many documents are indexed, used to decide whether a
// backfill is needed.
func (i *Index) DocCount() (uint64, error) {
	return i.idx.DocCount()
}

// IndexDoc adds or replaces one message in the index.
func (i *Index) IndexDoc(d Doc) error {
	if err := i.idx.Index(docID(d.ID), toIndexable(d)); err != nil {
		return fmt.Errorf("search: index message %d: %w", d.ID, err)
	}
	return nil
}

// IndexBatch indexes many messages in one commit, far faster than one at a time
// for the backfill.
func (i *Index) IndexBatch(docs []Doc) error {
	batch := i.idx.NewBatch()
	for _, d := range docs {
		if err := batch.Index(docID(d.ID), toIndexable(d)); err != nil {
			return fmt.Errorf("search: batch message %d: %w", d.ID, err)
		}
	}
	if err := i.idx.Batch(batch); err != nil {
		return fmt.Errorf("search: commit batch: %w", err)
	}
	return nil
}

// Delete removes a message from the index.
func (i *Index) Delete(id int64) error {
	if err := i.idx.Delete(docID(id)); err != nil {
		return fmt.Errorf("search: delete message %d: %w", id, err)
	}
	return nil
}

// Search runs a query and returns one page of matching message ids in the order
// q.Sort asks for (relevance by default), along with the total number of
// matches.
func (i *Index) Search(q Query) (Results, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	offset := max(q.Offset, 0)

	req := bleve.NewSearchRequestOptions(i.build(q), limit, offset, false)
	req.SortBy(sortOrder(q.Sort))
	res, err := i.idx.Search(req)
	if err != nil {
		return Results{}, fmt.Errorf("search: query %q: %w", q.Text, err)
	}

	hits := make([]Hit, 0, len(res.Hits))
	for _, h := range res.Hits {
		id, convErr := strconv.ParseInt(h.ID, 10, 64)
		if convErr != nil {
			continue
		}
		hits = append(hits, Hit{ID: id, Score: h.Score})
	}
	return Results{Hits: hits, Total: res.Total}, nil
}

// sortOrder returns the Bleve sort spec for s, falling back to relevance for an
// empty or unrecognized value so an older frontend, or a value that stopped
// existing, still gets the order search always had.
func sortOrder(s Sort) []string {
	if order, ok := sortOrders[s]; ok {
		return order
	}
	return sortOrders[SortRelevance]
}

// build assembles the Bleve query from the request: a text part (fuzzy, multi
// field, ranked) conjoined with a date-range part when bounds are set.
func (i *Index) build(q Query) query.Query {
	var parts []query.Query

	if q.Text != "" {
		parts = append(parts, textQuery(q.Text))
	}
	if q.From != "" {
		parts = append(parts, fieldQuery(q.From, "from"))
	}
	if q.To != "" {
		// a "to:" chip should match either recipient header.
		parts = append(parts, bleve.NewDisjunctionQuery(fieldQuery(q.To, "to"), fieldQuery(q.To, "cc")))
	}
	if q.Subject != "" {
		parts = append(parts, fieldQuery(q.Subject, "subject"))
	}
	if !q.After.IsZero() || !q.Before.IsZero() {
		parts = append(parts, dateQuery(q.After, q.Before))
	}

	switch len(parts) {
	case 0:
		return bleve.NewMatchAllQuery()
	case 1:
		return parts[0]
	default:
		return bleve.NewConjunctionQuery(parts...)
	}
}

// textFields are the free-text fields a query is matched against, with the boost
// that decides which one winning matters most.
var textFields = []struct {
	name  string
	boost float64
}{
	{"subject", boostSubject},
	{"from", boostFrom},
	{"to", boostRecip},
	{"cc", boostRecip},
	{"body", boostBody},
}

// textQuery matches the free text across all fields with per-field boosts, so
// the best field wins the score. Each hit must match the text in at least one
// field (the per-field alternatives form a disjunction).
//
// Every term has to be present in whichever field matches. Without that, a
// four-word query matched any message carrying any one of the four words, and
// the only thing keeping the results sensible was the ranking: the message the
// user meant scored highest and the thousands of one-word matches sat below the
// page. Sorting by date instead of score removed that cover and handed back the
// newest message containing the word "von" (#435). It also made the total a
// count of a set nobody asked for.
//
// Requiring the terms within a field rather than across the document is what
// keeps stop words harmless. The analyzer drops them, and it drops them inside
// the match query, so "invoice for march" asks for invoice AND march. Building
// the conjunction here instead, one clause per typed word, would ask for a word
// the index deliberately does not hold and match nothing at all.
//
// Fuzziness is applied only when it pays for itself. Measured against a real
// 5k-message mailbox, unconditional edit-distance-1 grew the matching set by
// about 40% and, because a page is finite, pushed exact matches off it: recall
// at the first page was worse with fuzz on than off. Pinning a prefix and
// skipping short terms keeps the typo tolerance without the flood.
func textQuery(text string) query.Query {
	fuzzy := shouldFuzz(text)

	alts := make([]query.Query, 0, len(textFields)+2)
	for _, f := range textFields {
		mq := bleve.NewMatchQuery(text)
		mq.SetField(f.name)
		mq.SetBoost(f.boost)
		mq.SetOperator(query.MatchQueryOperatorAnd)
		if fuzzy {
			mq.SetFuzziness(fuzziness)
			mq.SetPrefix(fuzzyPrefix)
		}
		alts = append(alts, mq)
	}
	alts = append(alts, prefixAlternatives(text)...)
	return bleve.NewDisjunctionQuery(alts...)
}

// shouldFuzz reports whether every term is long enough that one edit stays
// meaningful. A single short term in the query is enough to disable it, since
// that term is the one that would drag the noise in.
func shouldFuzz(text string) bool {
	terms := strings.Fields(text)
	if len(terms) == 0 {
		return false
	}
	for _, t := range terms {
		if utf8.RuneCountInString(t) < minFuzzyTerm {
			return false
		}
	}
	return true
}

// prefixAlternatives matches the final term as a prefix, so a query still finds
// mail while the word is being typed ("invoi" finds "invoice") rather than
// looking broken until the last keystroke. It is limited to subject and sender:
// a prefix scan of every body term costs far more and matches far too much to
// rank usefully. Matching a prefix also covers the common word-ending case, so
// "invoice" reaches "invoices" without a stemmer.
//
// The terms before it still have to match. A half-typed last word is the least
// certain part of a query, and on its own it would widen the search at the
// moment the user is narrowing it: "Rechnung App" would reach every message
// from Apple, none of which mention a Rechnung.
func prefixAlternatives(text string) []query.Query {
	terms := strings.Fields(text)
	if len(terms) == 0 {
		return nil
	}
	// the analyzer lowercases everything it indexes; a prefix query is not
	// analyzed, so it has to be lowercased here to match anything.
	last := strings.ToLower(terms[len(terms)-1])
	if utf8.RuneCountInString(last) < minPrefixTerm {
		return nil
	}
	rest := strings.Join(terms[:len(terms)-1], " ")
	out := make([]query.Query, 0, 2)
	for _, field := range []string{"subject", "from"} {
		pq := bleve.NewPrefixQuery(last)
		pq.SetField(field)
		pq.SetBoost(boostPrefix)
		if rest == "" {
			out = append(out, pq)
			continue
		}
		mq := bleve.NewMatchQuery(rest)
		mq.SetField(field)
		mq.SetOperator(query.MatchQueryOperatorAnd)
		out = append(out, bleve.NewConjunctionQuery(mq, pq))
	}
	return out
}

// fieldQuery matches value against a single field, requiring every token to be
// present (AND) so a "from:jane@x.com" chip stays precise instead of matching
// any of its tokenized parts. No fuzziness here: field chips are deliberate.
func fieldQuery(value, field string) query.Query {
	mq := bleve.NewMatchQuery(value)
	mq.SetField(field)
	mq.SetOperator(query.MatchQueryOperatorAnd)
	return mq
}

// dateQuery bounds the message date. Bleve treats a zero time as an open side
// when passed through; we pass explicit min/max to keep the meaning clear.
func dateQuery(after, before time.Time) query.Query {
	lo := after
	if lo.IsZero() {
		lo = time.Unix(0, 0)
	}
	hi := before
	if hi.IsZero() {
		hi = time.Now().AddDate(100, 0, 0)
	}
	dq := bleve.NewDateRangeQuery(lo, hi)
	dq.SetField("date")
	return dq
}

// toIndexable projects a Doc into the field map Bleve indexes (keys must match
// the mapping field names).
func toIndexable(d Doc) map[string]any {
	return map[string]any{
		"subject":        d.Subject,
		fieldSubjectSort: subjectSortKey(d.Subject),
		"from":           d.From,
		"to":             d.To,
		"cc":             d.Cc,
		"body":           d.Body,
		"date":           d.Date,
	}
}

// subjectSortKey is the single term the subject sorts on. Lowercased, because
// otherwise every capitalized subject sorts ahead of every lowercase one and the
// order looks broken; trimmed, because leading whitespace would sort a subject
// to the front for a reason nobody can see.
//
// It is otherwise the subject verbatim. Reply and forward prefixes are left
// alone: stripping them means a list of prefixes, and mail arrives with
// localized ones (AW:, WG:, SV:, RIF:) that no fixed list covers, so a partial
// list would sort some replies by their subject and others by "re".
func subjectSortKey(subject string) string {
	return strings.ToLower(strings.TrimSpace(subject))
}

// docID renders a message id as the stable Bleve document id.
func docID(id int64) string {
	return strconv.FormatInt(id, 10)
}

// buildMapping describes the message document: standard-analyzed text fields and
// a datetime field for range filtering.
func buildMapping() *mapping.IndexMappingImpl {
	text := bleve.NewTextFieldMapping()
	text.Store = false

	date := bleve.NewDateTimeFieldMapping()
	date.Store = false

	// the sortable subject is a keyword: one term, unanalyzed, so sorting on it
	// orders by the whole subject rather than by whichever word in it happens to
	// sort first. It is never searched, only sorted on.
	subjectSort := bleve.NewKeywordFieldMapping()
	subjectSort.Store = false
	subjectSort.IncludeInAll = false

	doc := bleve.NewDocumentMapping()
	for _, name := range []string{"subject", "from", "to", "cc", "body"} {
		doc.AddFieldMappingsAt(name, text)
	}
	doc.AddFieldMappingsAt(fieldSubjectSort, subjectSort)
	doc.AddFieldMappingsAt("date", date)

	im := bleve.NewIndexMapping()
	im.DefaultMapping = doc
	return im
}
