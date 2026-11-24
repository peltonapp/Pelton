package feeds

import (
	"encoding/json"
	"net/url"
	"strings"
)

// jsonFeed is JSON Feed 1.0 and 1.1. Ids may be strings or numbers in the
// wild, so they are read raw.
type jsonFeed struct {
	Version     string       `json:"version"`
	Title       string       `json:"title"`
	HomePageURL string       `json:"home_page_url"`
	FeedURL     string       `json:"feed_url"`
	Description string       `json:"description"`
	Icon        string       `json:"icon"`
	Favicon     string       `json:"favicon"`
	Author      *jsonAuthor  `json:"author"`
	Authors     []jsonAuthor `json:"authors"`
	Items       []jsonItem   `json:"items"`
}

type jsonAuthor struct {
	Name string `json:"name"`
}

type jsonItem struct {
	ID            json.RawMessage  `json:"id"`
	URL           string           `json:"url"`
	ExternalURL   string           `json:"external_url"`
	Title         string           `json:"title"`
	ContentHTML   string           `json:"content_html"`
	ContentText   string           `json:"content_text"`
	Summary       string           `json:"summary"`
	Image         string           `json:"image"`
	BannerImage   string           `json:"banner_image"`
	DatePublished string           `json:"date_published"`
	DateModified  string           `json:"date_modified"`
	Author        *jsonAuthor      `json:"author"`
	Authors       []jsonAuthor     `json:"authors"`
	Attachments   []jsonAttachment `json:"attachments"`
}

type jsonAttachment struct {
	URL      string `json:"url"`
	MimeType string `json:"mime_type"`
	Title    string `json:"title"`
	Size     int64  `json:"size_in_bytes"`
}

func parseJSON(data []byte, base *url.URL) (*Feed, error) {
	var jf jsonFeed
	if err := json.Unmarshal(data, &jf); err != nil {
		return nil, ErrNotFeed
	}
	if !strings.Contains(jf.Version, "jsonfeed.org") && jf.Items == nil {
		return nil, ErrNotFeed
	}
	f := &Feed{
		Kind:        "json",
		Title:       jf.Title,
		SiteURL:     resolveURL(base, jf.HomePageURL),
		Description: cleanText(jf.Description),
		IconURL:     resolveURL(base, firstNonEmpty(jf.Favicon, jf.Icon)),
	}
	feedAuthor := jsonAuthors(jf.Author, jf.Authors)
	contentBase := siteBase(base, f.SiteURL)
	for _, it := range jf.Items {
		e := Entry{
			GUID:      jsonID(it.ID),
			URL:       resolveURL(contentBase, firstNonEmpty(it.URL, it.ExternalURL)),
			Title:     it.Title,
			Image:     resolveURL(contentBase, firstNonEmpty(it.Image, it.BannerImage)),
			Published: ParseDate(it.DatePublished),
			Updated:   ParseDate(it.DateModified),
			Author:    firstNonEmpty(jsonAuthors(it.Author, it.Authors), feedAuthor),
		}
		if it.ContentHTML != "" {
			e.Content = resolveHTML(it.ContentHTML, contentBase)
		} else {
			e.Content = textToHTML(it.ContentText)
		}
		if it.Summary != "" {
			e.Summary = textToHTML(it.Summary)
		}
		for _, a := range it.Attachments {
			e.Enclosures = append(e.Enclosures, Enclosure{
				URL:    resolveURL(contentBase, a.URL),
				Type:   a.MimeType,
				Length: a.Size,
				Title:  a.Title,
			})
		}
		f.Entries = append(f.Entries, e)
	}
	return f, nil
}

func jsonAuthors(one *jsonAuthor, many []jsonAuthor) string {
	var names []string
	for _, a := range many {
		if n := strings.TrimSpace(a.Name); n != "" {
			names = append(names, n)
		}
	}
	if len(names) == 0 && one != nil {
		return strings.TrimSpace(one.Name)
	}
	return strings.Join(names, ", ")
}

// jsonID reads an id given as a string or as a number.
func jsonID(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return strings.TrimSpace(string(raw))
}
