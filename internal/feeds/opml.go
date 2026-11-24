package feeds

import (
	"bytes"
	"encoding/xml"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/charsetguess"
)

// Subscription is one feed in an OPML list. Folder is the outline it sat in,
// "" for one at the top level. Outlines nested deeper than one folder are
// flattened into their top-level folder.
type Subscription struct {
	Title   string
	URL     string
	SiteURL string
	Folder  string
}

type opmlDoc struct {
	XMLName xml.Name    `xml:"opml"`
	Version string      `xml:"version,attr"`
	Head    opmlHead    `xml:"head"`
	Body    []opmlEntry `xml:"body>outline"`
}

type opmlHead struct {
	Title       string `xml:"title"`
	DateCreated string `xml:"dateCreated,omitempty"`
}

type opmlEntry struct {
	Text     string      `xml:"text,attr"`
	Title    string      `xml:"title,attr,omitempty"`
	Type     string      `xml:"type,attr,omitempty"`
	XMLURL   string      `xml:"xmlUrl,attr,omitempty"`
	HTMLURL  string      `xml:"htmlUrl,attr,omitempty"`
	Children []opmlEntry `xml:"outline"`
}

// ParseOPML reads the subscriptions of an OPML document. Outlines without a
// feed address are folders; the same address listed twice is kept once.
func ParseOPML(data []byte) ([]Subscription, error) {
	d := xml.NewDecoder(bytes.NewReader(prepareXML(data)))
	d.Strict = false
	d.Entity = xml.HTMLEntity
	d.CharsetReader = charsetguess.Reader
	var doc opmlDoc
	if err := d.Decode(&doc); err != nil {
		return nil, err
	}
	var out []Subscription
	seen := map[string]bool{}
	var walk func(list []opmlEntry, folder string, top bool)
	walk = func(list []opmlEntry, folder string, top bool) {
		for _, o := range list {
			title := strings.TrimSpace(firstNonEmpty(o.Title, o.Text))
			if u := strings.TrimSpace(o.XMLURL); u != "" {
				if !seen[u] {
					seen[u] = true
					out = append(out, Subscription{Title: title, URL: u, SiteURL: strings.TrimSpace(o.HTMLURL), Folder: folder})
				}
				continue
			}
			next := folder
			if top {
				next = title
			}
			walk(o.Children, next, false)
		}
	}
	walk(doc.Body, "", true)
	return out, nil
}

// WriteOPML writes subscriptions as an OPML 2.0 document, one outline per
// folder in the order folders first appear.
func WriteOPML(title string, subs []Subscription, now time.Time) ([]byte, error) {
	doc := opmlDoc{Version: "2.0", Head: opmlHead{Title: title, DateCreated: now.UTC().Format(time.RFC1123Z)}}
	folders := map[string]int{}
	for _, s := range subs {
		entry := opmlEntry{Text: s.Title, Title: s.Title, Type: "rss", XMLURL: s.URL, HTMLURL: s.SiteURL}
		if s.Folder == "" {
			doc.Body = append(doc.Body, entry)
			continue
		}
		i, ok := folders[s.Folder]
		if !ok {
			i = len(doc.Body)
			folders[s.Folder] = i
			doc.Body = append(doc.Body, opmlEntry{Text: s.Folder, Title: s.Folder})
		}
		doc.Body[i].Children = append(doc.Body[i].Children, entry)
	}
	body, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), append(body, '\n')...), nil
}
