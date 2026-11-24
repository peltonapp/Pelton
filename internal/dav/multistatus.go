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

// Node is one xml element of a property value, with its namespace resolved,
// which raw inner xml loses once it is cut out of the document that declared
// the prefixes.
type Node struct {
	XMLName  xml.Name
	Attrs    []xml.Attr `xml:",any,attr"`
	Children []Node     `xml:",any"`
	Text     string     `xml:",chardata"`
}

// Find returns every element below n, at any depth, with the given name.
func (n Node) Find(space, local string) []Node {
	var out []Node
	for _, child := range n.Children {
		if child.XMLName.Space == space && child.XMLName.Local == local {
			out = append(out, child)
		}
		out = append(out, child.Find(space, local)...)
	}
	return out
}

// Hrefs returns the DAV:href values below n, unescaped, for properties like
// current-user-principal whose value is a nested href.
func (n Node) Hrefs() []string {
	var out []string
	for _, h := range n.Find("DAV:", "href") {
		if v := strings.TrimSpace(h.Text); v != "" {
			out = append(out, unescapeHref(v))
		}
	}
	return out
}

// Attr returns the value of an attribute by local name.
func (n Node) Attr(local string) string {
	for _, a := range n.Attrs {
		if a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}

// Response is one <response> of a 207 multistatus reply.
type Response struct {
	// Href is the resource path, unescaped.
	Href string
	// Status is the response-level status code, 0 when the server put the
	// status on each propstat instead.
	Status int
	// Props holds the properties reported with a 2xx propstat, keyed by
	// namespace and local name.
	Props map[xml.Name]Node
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

// Prop returns a property's element.
func (r Response) Prop(space, local string) Node {
	return r.Props[xml.Name{Space: space, Local: local}]
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
	Prop   Node   `xml:"DAV: prop"`
	Status string `xml:"DAV: status"`
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
		resp := Response{Status: statusCode(r.Status), Props: map[xml.Name]Node{}}
		resp.Href = unescapeHref(r.Hrefs[0])
		for _, ps := range r.Propstats {
			code := statusCode(ps.Status)
			if code != 0 && (code < 200 || code > 299) {
				continue
			}
			for _, item := range ps.Prop.Children {
				resp.Props[item.XMLName] = item
			}
		}
		out.Responses = append(out.Responses, resp)
		// a server may list several hrefs in one response for the same status,
		// which sync-collection uses for deletions.
		for _, extra := range r.Hrefs[1:] {
			out.Responses = append(out.Responses, Response{Href: unescapeHref(extra), Status: resp.Status, Props: map[xml.Name]Node{}})
		}
	}
	return out, nil
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
	if u, err := url.Parse(href); err == nil && u.Path != "" {
		return u.Path
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
