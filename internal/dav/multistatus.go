package dav

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Response is one <response> of a 207 multistatus reply.
type Response struct {
	// Href is the resource path, unescaped.
	Href string
	// Status is the response-level status code, 0 when the server put the
	// status on each propstat instead.
	Status int
	// Props holds the properties reported with a 2xx propstat, keyed by
	// namespace and local name.
	Props map[xml.Name]Prop
}

// Prop is one reported property: its text and the raw inner xml, for the
// properties whose value is a nested element (resourcetype, an href).
type Prop struct {
	Text  string
	Inner []byte
}

// Has reports whether the response carries the property.
func (r Response) Has(space, local string) bool {
	_, ok := r.Props[xml.Name{Space: space, Local: local}]
	return ok
}

// Text returns a property's trimmed text, or "" when it was not reported.
func (r Response) Text(space, local string) string {
	return strings.TrimSpace(r.Props[xml.Name{Space: space, Local: local}].Text)
}

// Inner returns a property's raw inner xml.
func (r Response) Inner(space, local string) []byte {
	return r.Props[xml.Name{Space: space, Local: local}].Inner
}

// Multistatus is a parsed 207 reply plus the sync token a sync-collection
// report ends with.
type Multistatus struct {
	Responses []Response
	SyncToken string
}

type rawMultistatus struct {
	Responses []rawResponse `xml:"DAV: response"`
	SyncToken string        `xml:"DAV: sync-token"`
}

type rawResponse struct {
	Hrefs     []string      `xml:"DAV: href"`
	Status    string        `xml:"DAV: status"`
	Propstats []rawPropstat `xml:"DAV: propstat"`
}

type rawPropstat struct {
	Prop   rawProp `xml:"DAV: prop"`
	Status string  `xml:"DAV: status"`
}

type rawProp struct {
	Items []rawItem `xml:",any"`
}

type rawItem struct {
	XMLName  xml.Name
	Inner    []byte `xml:",innerxml"`
	CharData string `xml:",chardata"`
}

// ParseMultistatus decodes a multistatus body.
func ParseMultistatus(body io.Reader) (Multistatus, error) {
	var raw rawMultistatus
	if err := xml.NewDecoder(body).Decode(&raw); err != nil {
		return Multistatus{}, fmt.Errorf("dav: parse multistatus: %w", err)
	}
	out := Multistatus{SyncToken: strings.TrimSpace(raw.SyncToken)}
	for _, r := range raw.Responses {
		if len(r.Hrefs) == 0 {
			continue
		}
		resp := Response{Status: statusCode(r.Status), Props: map[xml.Name]Prop{}}
		resp.Href = unescapeHref(r.Hrefs[0])
		for _, ps := range r.Propstats {
			code := statusCode(ps.Status)
			if code != 0 && (code < 200 || code > 299) {
				continue
			}
			for _, item := range ps.Prop.Items {
				resp.Props[item.XMLName] = Prop{Text: item.CharData, Inner: item.Inner}
			}
		}
		out.Responses = append(out.Responses, resp)
		// a server may list several hrefs in one response for the same status,
		// which sync-collection uses for deletions.
		for _, extra := range r.Hrefs[1:] {
			out.Responses = append(out.Responses, Response{Href: unescapeHref(extra), Status: resp.Status, Props: map[xml.Name]Prop{}})
		}
	}
	return out, nil
}

// Hrefs returns every <href> inside raw inner xml, unescaped, for properties
// like current-user-principal whose value is a nested href.
func Hrefs(inner []byte) []string {
	dec := xml.NewDecoder(bytes.NewReader(inner))
	var out []string
	inHref := false
	for {
		tok, err := dec.Token()
		if err != nil {
			return out
		}
		switch t := tok.(type) {
		case xml.StartElement:
			inHref = t.Name.Local == "href"
		case xml.EndElement:
			inHref = false
		case xml.CharData:
			if inHref {
				if v := strings.TrimSpace(string(t)); v != "" {
					out = append(out, unescapeHref(v))
				}
			}
		}
	}
}

// Elements returns the names of the direct child elements in raw inner xml,
// for properties like resourcetype whose value is a set of empty elements.
func Elements(inner []byte) []xml.Name {
	dec := xml.NewDecoder(bytes.NewReader(inner))
	var out []xml.Name
	depth := 0
	for {
		tok, err := dec.Token()
		if err != nil {
			return out
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if depth == 0 {
				out = append(out, t.Name)
			}
			depth++
		case xml.EndElement:
			depth--
		}
	}
}

// statusCode reads the code out of a status line like "HTTP/1.1 404 Not Found".
func statusCode(line string) int {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0
	}
	code, _ := strconv.Atoi(fields[1])
	return code
}

// unescapeHref turns an href into a path. An absolute url keeps only its
// path, since everything is resolved against the connection's endpoint.
func unescapeHref(href string) string {
	href = strings.TrimSpace(href)
	if u, err := url.Parse(href); err == nil {
		if u.Path != "" {
			return u.Path
		}
	}
	if p, err := url.PathUnescape(href); err == nil {
		return p
	}
	return href
}

// Do sends a PROPFIND or REPORT with an xml body and parses the multistatus
// reply. A server error comes back as a *StatusError, so a caller can tell a
// rejected sync token (403, 409) from a network failure.
func (c *Conn) Do(ctx context.Context, method, path, depth string, body []byte) (Multistatus, error) {
	req, err := c.Request(ctx, method, path, bytes.NewReader(body))
	if err != nil {
		return Multistatus{}, err
	}
	req.Header.Set("Content-Type", "application/xml; charset=utf-8")
	if depth != "" {
		req.Header.Set("Depth", depth)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Multistatus{}, fmt.Errorf("dav: %s %s: %w", strings.ToLower(method), path, err)
	}
	defer Drain(resp)
	if resp.StatusCode != http.StatusMultiStatus {
		return Multistatus{}, &StatusError{Method: method, Path: path, Code: resp.StatusCode, Status: resp.Status}
	}
	return ParseMultistatus(io.LimitReader(resp.Body, maxBody))
}
