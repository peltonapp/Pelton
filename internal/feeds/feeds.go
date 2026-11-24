// Package feeds reads web feeds: RSS 2.0, RSS 1.0 (RDF), Atom 1.0 and JSON
// Feed 1.1. It finds the feeds a site offers, fetches them from their own
// servers with conditional requests and per-host politeness, and reads and
// writes OPML subscription lists. Parsing uses encoding/xml and encoding/json
// only, and is tolerant of the breakage real feeds ship with: unparseable
// dates, missing ids, relative links and wrong character sets.
package feeds

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

// ErrNotFeed is returned when a document is neither a feed nor anything a feed
// could be read from.
var ErrNotFeed = errors.New("feeds: not a feed")

// Feed is a parsed feed document.
type Feed struct {
	// Kind is "rss", "rdf", "atom" or "json".
	Kind        string
	Title       string
	SiteURL     string
	Description string
	// IconURL is an icon or logo the feed names, if any.
	IconURL string
	Entries []Entry
}

// Entry is one item of a feed. Content and Summary are html; Title and Author
// are plain text. GUID is never empty after parsing: a feed without ids gets one
// derived from the link, or from the title and date.
type Entry struct {
	GUID       string
	URL        string
	Title      string
	Author     string
	Summary    string
	Content    string
	Published  time.Time
	Updated    time.Time
	Enclosures []Enclosure
	// Image is a thumbnail the feed names for the entry.
	Image string
}

// Enclosure is a file attached to an entry, such as a podcast episode.
type Enclosure struct {
	URL    string `json:"url"`
	Type   string `json:"type"`
	Length int64  `json:"length"`
	Title  string `json:"title,omitempty"`
}

// finish fills what the formats leave open, the same way for all of them: ids
// for entries without one, published dates from updated ones and the other
// way round, and unique enclosures.
func (f *Feed) finish() {
	f.Title = cleanText(f.Title)
	for i := range f.Entries {
		e := &f.Entries[i]
		e.Title = cleanText(e.Title)
		e.Author = cleanText(e.Author)
		if e.Published.IsZero() {
			e.Published = e.Updated
		}
		if e.Updated.IsZero() {
			e.Updated = e.Published
		}
		e.GUID = strings.TrimSpace(e.GUID)
		if e.GUID == "" {
			e.GUID = fallbackGUID(*e)
		}
		e.Enclosures = uniqueEnclosures(e.Enclosures)
	}
}

// fallbackGUID derives an id for an entry the feed gave none: its link when it
// has one, else a hash of what it says, which stays the same between fetches
// as long as the entry does.
func fallbackGUID(e Entry) string {
	if e.URL != "" {
		return e.URL
	}
	sum := sha256.Sum256([]byte(e.Title + "\x00" + e.Published.UTC().Format(time.RFC3339) + "\x00" + e.Summary + "\x00" + e.Content))
	return "sha256:" + hex.EncodeToString(sum[:16])
}

func uniqueEnclosures(list []Enclosure) []Enclosure {
	if len(list) < 2 {
		return list
	}
	seen := make(map[string]bool, len(list))
	out := list[:0]
	for _, e := range list {
		if e.URL == "" || seen[e.URL] {
			continue
		}
		seen[e.URL] = true
		out = append(out, e)
	}
	return out
}
