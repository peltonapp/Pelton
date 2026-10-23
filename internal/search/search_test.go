package search

import (
	"slices"
	"testing"
	"time"
)

// testIndex builds a throwaway index holding docs.
func testIndex(t *testing.T, docs ...Doc) *Index {
	t.Helper()
	idx, err := Open(t.TempDir() + "/test.bleve")
	if err != nil {
		t.Fatalf("open index: %v", err)
	}
	t.Cleanup(func() { _ = idx.Close() })
	for _, d := range docs {
		if d.Date.IsZero() {
			d.Date = time.Now()
		}
		if err := idx.IndexDoc(d); err != nil {
			t.Fatalf("index doc %d: %v", d.ID, err)
		}
	}
	return idx
}

// ids runs a query and returns the matching ids in rank order.
func ids(t *testing.T, idx *Index, q Query) []int64 {
	t.Helper()
	res, err := idx.Search(q)
	if err != nil {
		t.Fatalf("search %q: %v", q.Text, err)
	}
	out := make([]int64, 0, len(res.Hits))
	for _, h := range res.Hits {
		out = append(out, h.ID)
	}
	return out
}

func contains(ids []int64, want int64) bool {
	return slices.Contains(ids, want)
}

var corpus = []Doc{
	{ID: 1, Subject: "Invoice for March", Body: "the invoice is attached"},
	{ID: 2, Subject: "Invoices for Q1", From: "Billing billing@example.test"},
	{ID: 3, Subject: "Lunch on Friday", Body: "the place near the park"},
	{ID: 4, Subject: "Grüne Rechnung", Body: "die Rechnung ist fällig"},
}

// A word that is verbatim in a message must find it. This is the whole promise
// of the feature and the thing users reported broken.
func TestExactWordFindsItsMessage(t *testing.T) {
	idx := testIndex(t, corpus...)
	for _, tt := range []struct {
		query string
		want  int64
	}{
		{"invoice", 1},
		{"INVOICE", 1},
		{"Friday", 3},
		{"Rechnung", 4},
		{"billing", 2},
	} {
		if got := ids(t, idx, Query{Text: tt.query}); !contains(got, tt.want) {
			t.Errorf("search %q did not find message %d, got %v", tt.query, tt.want, got)
		}
	}
}

// Typing a partial word should already show results rather than looking broken
// until the last keystroke.
func TestPrefixMatchesWhileTyping(t *testing.T) {
	idx := testIndex(t, corpus...)
	got := ids(t, idx, Query{Text: "invoi"})
	if !contains(got, 1) {
		t.Errorf("prefix %q did not find message 1, got %v", "invoi", got)
	}
}

// Prefix matching is what makes a singular reach a plural without a stemmer.
func TestSingularReachesPlural(t *testing.T) {
	idx := testIndex(t, corpus...)
	got := ids(t, idx, Query{Text: "invoice"})
	if !contains(got, 2) {
		t.Errorf("search %q did not reach the plural subject, got %v", "invoice", got)
	}
}

// A term too short to fuzz safely must not drag in unrelated mail. "lun" is a
// prefix of Lunch and one edit from several other words.
func TestShortTermIsNotFuzzed(t *testing.T) {
	if got := shouldFuzz("lun"); got {
		t.Errorf("shouldFuzz(%q) = true, want false", "lun")
	}
	if got := shouldFuzz("invoice march"); !got {
		t.Errorf("shouldFuzz(%q) = false, want true", "invoice march")
	}
	// one short term is enough to disable fuzzing for the whole query.
	if got := shouldFuzz("invoice of"); got {
		t.Errorf("shouldFuzz(%q) = true, want false", "invoice of")
	}
}

// A typo within one edit still finds the mail, which is what fuzziness is for.
func TestTypoStillFinds(t *testing.T) {
	idx := testIndex(t, corpus...)
	got := ids(t, idx, Query{Text: "invoive"})
	if !contains(got, 1) {
		t.Errorf("typo %q did not find message 1, got %v", "invoive", got)
	}
}

// Total must report every match, not just the page, or the caller cannot tell a
// full page from a truncated one.
func TestTotalCountsBeyondThePage(t *testing.T) {
	docs := make([]Doc, 0, 30)
	for i := 1; i <= 30; i++ {
		docs = append(docs, Doc{ID: int64(i), Subject: "weekly report", Date: time.Now()})
	}
	idx := testIndex(t, docs...)

	res, err := idx.Search(Query{Text: "weekly", Limit: 10})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res.Hits) != 10 {
		t.Errorf("page holds %d hits, want 10", len(res.Hits))
	}
	if res.Total != 30 {
		t.Errorf("Total = %d, want 30", res.Total)
	}
}

// Paging must reach hits ranked past the first page, and never repeat one.
func TestOffsetPagesThroughResults(t *testing.T) {
	docs := make([]Doc, 0, 30)
	for i := 1; i <= 30; i++ {
		docs = append(docs, Doc{ID: int64(i), Subject: "weekly report", Date: time.Now()})
	}
	idx := testIndex(t, docs...)

	seen := map[int64]bool{}
	for offset := 0; offset < 30; offset += 10 {
		for _, id := range ids(t, idx, Query{Text: "weekly", Limit: 10, Offset: offset}) {
			if seen[id] {
				t.Errorf("message %d returned on more than one page", id)
			}
			seen[id] = true
		}
	}
	if len(seen) != 30 {
		t.Errorf("paging reached %d of 30 messages", len(seen))
	}
}

// sortCorpus has subjects and dates that disagree, so an order that silently
// fell back to relevance would still be visible in the result.
var sortCorpus = []Doc{
	{ID: 1, Subject: "Zebra crossing report", Date: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
	{ID: 2, Subject: "apple harvest report", Date: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)},
	{ID: 3, Subject: "Mango season report", Date: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)},
}

// Each sort actually orders by what it says. The date orders are the ones a
// user notices first; the subject orders are why the index carries a second,
// unanalyzed copy of the subject at all.
func TestSortOrders(t *testing.T) {
	idx := testIndex(t, sortCorpus...)

	cases := []struct {
		sort Sort
		want []int64
	}{
		{SortNewest, []int64{2, 3, 1}},
		{SortOldest, []int64{1, 3, 2}},
		// "apple" sorts ahead of "Zebra" only because the sort key is
		// lowercased; on the raw subject every capital letter would come first.
		{SortSubjectAsc, []int64{2, 3, 1}},
		{SortSubjectDesc, []int64{1, 3, 2}},
	}
	for _, c := range cases {
		got := ids(t, idx, Query{Text: "report", Sort: c.sort})
		if len(got) != len(c.want) {
			t.Errorf("%s: got %v, want %v", c.sort, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: got %v, want %v", c.sort, got, c.want)
				break
			}
		}
	}
}

// An unset sort has to keep behaving the way search did before there was a
// choice, since that is what an older caller sends.
func TestUnknownSortFallsBackToRelevance(t *testing.T) {
	want := sortOrder(SortRelevance)
	for _, s := range []Sort{"", "nonsense"} {
		got := sortOrder(s)
		if len(got) != len(want) {
			t.Fatalf("sort %q: got %v, want %v", s, got, want)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("sort %q: got %v, want %v", s, got, want)
			}
		}
	}
}

// Paging has to stay stable under a date sort too: messages sharing a date are
// the normal case, and without the id tiebreak they would drift between pages.
func TestDateSortPagesWithoutRepeats(t *testing.T) {
	sameDay := time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC)
	docs := make([]Doc, 0, 30)
	for i := 1; i <= 30; i++ {
		docs = append(docs, Doc{ID: int64(i), Subject: "weekly report", Date: sameDay})
	}
	idx := testIndex(t, docs...)

	seen := map[int64]bool{}
	for offset := 0; offset < 30; offset += 10 {
		for _, id := range ids(t, idx, Query{Text: "weekly", Limit: 10, Offset: offset, Sort: SortNewest}) {
			if seen[id] {
				t.Errorf("message %d returned on more than one page", id)
			}
			seen[id] = true
		}
	}
	if len(seen) != 30 {
		t.Errorf("paging reached %d of 30 messages", len(seen))
	}
}

// Field chips stay precise: no fuzziness, and every token must be present.
func TestFieldChipIsExact(t *testing.T) {
	idx := testIndex(t, corpus...)
	if got := ids(t, idx, Query{From: "billing@example.test"}); !contains(got, 2) {
		t.Errorf("from chip did not find message 2, got %v", got)
	}
	if got := ids(t, idx, Query{Subject: "Lunch"}); !contains(got, 3) {
		t.Errorf("subject chip did not find message 3, got %v", got)
	}
}

// phraseCorpus is the shape of the bug in #435: several messages share one word
// with the query and exactly one carries all of them.
var phraseCorpus = []Doc{
	{ID: 1, Subject: "Deine Rechnung von Apple", From: "Apple apple@example.test", Date: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
	{ID: 2, Subject: "Newsletter von Beispiel", Date: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)},
	{ID: 3, Subject: "Rechnung Stadtwerke", Body: "deine Zahlung", Date: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)},
	{ID: 4, Subject: "Deine Bestellung", Date: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)},
}

// Typing several words asks for all of them. Matching any one of them made the
// result set the union of four common words, which relevance ranking hid and
// the total reported as a real count.
func TestEveryWordMustMatch(t *testing.T) {
	idx := testIndex(t, phraseCorpus...)

	res, err := idx.Search(Query{Text: "Deine Rechnung von Apple"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if res.Total != 1 {
		t.Errorf("Total = %d, want 1: only message 1 carries every word", res.Total)
	}
}

// The order results come back in must not change which results they are. This
// is what the user saw: relevance found the message, newest-first returned
// unrelated mail, and retyping the query did not help because the match set was
// wrong rather than the ranking.
func TestSortDoesNotChangeWhatMatches(t *testing.T) {
	idx := testIndex(t, phraseCorpus...)

	for _, sort := range []Sort{SortRelevance, SortNewest, SortOldest, SortSubjectAsc, SortSubjectDesc} {
		got := ids(t, idx, Query{Text: "Deine Rechnung von Apple", Sort: sort})
		if len(got) != 1 || got[0] != 1 {
			t.Errorf("sort %s returned %v, want [1]", sort, got)
		}
	}
}

// Requiring every word must not turn a stop word into a query that matches
// nothing. The analyzer drops "for" from the index and from the query alike, so
// what is actually being asked for is invoice AND march.
func TestStopWordDoesNotEmptyTheResults(t *testing.T) {
	idx := testIndex(t, corpus...)
	if got := ids(t, idx, Query{Text: "invoice for march"}); !contains(got, 1) {
		t.Errorf("search %q did not find message 1, got %v", "invoice for march", got)
	}
}

// Still-typing keeps working with words in front of the one being typed.
func TestPrefixMatchesAfterEarlierWords(t *testing.T) {
	idx := testIndex(t, corpus...)
	if got := ids(t, idx, Query{Text: "Grüne Rechn"}); !contains(got, 4) {
		t.Errorf("search %q did not find message 4, got %v", "Grüne Rechn", got)
	}
}

// A half-typed last word narrows the search like any other. On its own it
// widened it: the prefix reached every message whose subject or sender started
// with those letters, whatever the rest of the query said.
func TestPrefixDoesNotWidenTheQuery(t *testing.T) {
	idx := testIndex(t, corpus...)
	if got := ids(t, idx, Query{Text: "Lunch invoi"}); len(got) != 0 {
		t.Errorf("search %q returned %v, want nothing: no message is both", "Lunch invoi", got)
	}
}
