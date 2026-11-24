package notes

import (
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search/query"
)

// vaultKey is the internal bleve key naming the vault an index was built for.
var vaultKey = []byte("pelton:vault")

// searchIndex is the full-text index over a vault's notes, the same Bleve
// index type mail search uses. Documents are keyed by vault path.
type searchIndex struct {
	idx bleve.Index
}

type searchDoc struct {
	Title    string `json:"title"`
	Aliases  string `json:"aliases"`
	Headings string `json:"headings"`
	Tags     string `json:"tags"`
	Body     string `json:"body"`
}

// openSearch opens the index at dir for vault. An index built for another
// vault is thrown away and started over; fresh reports that the index is
// empty and every note needs adding.
func openSearch(dir, vault string) (*searchIndex, bool, error) {
	idx, err := bleve.Open(dir)
	if err == nil {
		v, gerr := idx.GetInternal(vaultKey)
		if gerr == nil && string(v) == vault {
			return &searchIndex{idx: idx}, false, nil
		}
		idx.Close()
		if err := os.RemoveAll(dir); err != nil {
			return nil, false, fmt.Errorf("notes: reset search index: %w", err)
		}
	} else if err != bleve.ErrorIndexPathDoesNotExist {
		// a damaged index is only a cache; start it again.
		if rmErr := os.RemoveAll(dir); rmErr != nil {
			return nil, false, fmt.Errorf("notes: open search index: %w", err)
		}
	}
	idx, err = bleve.New(dir, searchMapping())
	if err != nil {
		return nil, false, fmt.Errorf("notes: create search index: %w", err)
	}
	if err := idx.SetInternal(vaultKey, []byte(vault)); err != nil {
		idx.Close()
		return nil, false, fmt.Errorf("notes: tag search index: %w", err)
	}
	return &searchIndex{idx: idx}, true, nil
}

func searchMapping() *mapping.IndexMappingImpl {
	text := bleve.NewTextFieldMapping()
	text.Store = false
	doc := bleve.NewDocumentMapping()
	for _, name := range []string{"title", "aliases", "headings", "tags", "body"} {
		doc.AddFieldMappingsAt(name, text)
	}
	im := bleve.NewIndexMapping()
	im.DefaultMapping = doc
	return im
}

func (s *searchIndex) close() error {
	return s.idx.Close()
}

// put adds or replaces notes and removes paths in one batch.
func (s *searchIndex) put(docs map[string]searchDoc, removed []string) error {
	if len(docs) == 0 && len(removed) == 0 {
		return nil
	}
	b := s.idx.NewBatch()
	for p, d := range docs {
		if err := b.Index(p, d); err != nil {
			return fmt.Errorf("notes: index %q: %w", p, err)
		}
	}
	for _, p := range removed {
		b.Delete(p)
	}
	if err := s.idx.Batch(b); err != nil {
		return fmt.Errorf("notes: commit search batch: %w", err)
	}
	return nil
}

var searchFields = []struct {
	name  string
	boost float64
}{
	{"title", 4},
	{"aliases", 3},
	{"tags", 2},
	{"headings", 1.5},
	{"body", 1},
}

// phrase returns the paths of the notes whose text holds the words of text
// in that order.
func (s *searchIndex) phrase(text string, limit int) ([]string, error) {
	q := bleve.NewMatchPhraseQuery(text)
	q.SetField("body")
	res, err := s.idx.Search(bleve.NewSearchRequestOptions(q, limit, 0, false))
	if err != nil {
		return nil, fmt.Errorf("notes: phrase search: %w", err)
	}
	out := make([]string, 0, len(res.Hits))
	for _, h := range res.Hits {
		out = append(out, h.ID)
	}
	return out, nil
}

// search returns the paths of the notes matching text, best first. Every word
// has to be in one field; the last word also matches as a prefix of a title,
// so a note turns up while its name is still being typed.
func (s *searchIndex) search(text string, limit int) ([]string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, nil
	}
	alts := make([]query.Query, 0, len(searchFields)+1)
	for _, f := range searchFields {
		mq := bleve.NewMatchQuery(text)
		mq.SetField(f.name)
		mq.SetBoost(f.boost)
		mq.SetOperator(query.MatchQueryOperatorAnd)
		alts = append(alts, mq)
	}
	terms := strings.Fields(strings.ToLower(text))
	if last := terms[len(terms)-1]; utf8.RuneCountInString(last) >= 2 {
		for _, field := range []string{"title", "body"} {
			pq := bleve.NewPrefixQuery(last)
			pq.SetField(field)
			pq.SetBoost(0.5)
			if len(terms) == 1 {
				alts = append(alts, pq)
				continue
			}
			mq := bleve.NewMatchQuery(strings.Join(terms[:len(terms)-1], " "))
			mq.SetField(field)
			mq.SetOperator(query.MatchQueryOperatorAnd)
			alts = append(alts, bleve.NewConjunctionQuery(mq, pq))
		}
	}
	req := bleve.NewSearchRequestOptions(bleve.NewDisjunctionQuery(alts...), limit, 0, false)
	req.SortBy([]string{"-_score", "_id"})
	res, err := s.idx.Search(req)
	if err != nil {
		return nil, fmt.Errorf("notes: search %q: %w", text, err)
	}
	out := make([]string, 0, len(res.Hits))
	for _, h := range res.Hits {
		out = append(out, h.ID)
	}
	return out, nil
}
