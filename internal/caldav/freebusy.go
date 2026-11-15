package caldav

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/peltonapp/Pelton/internal/dav"
	"github.com/peltonapp/Pelton/internal/ical"
)

// ErrNoScheduling means the server offers no scheduling outbox (RFC 6638), so
// it cannot answer for anyone's free time.
var ErrNoScheduling = errors.New("caldav: the server does not support scheduling")

// Scheduling is where the logged-in user sends scheduling requests and the
// addresses the server knows them by.
type Scheduling struct {
	Outbox string
	// Addresses are the calendar-user-address-set entries without their
	// mailto: scheme, in the order the server lists them.
	Addresses []string
}

const propfindScheduling = `<?xml version="1.0" encoding="utf-8"?>
<d:propfind xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">
  <d:prop><d:current-user-principal/><c:schedule-outbox-URL/><c:calendar-user-address-set/></d:prop>
</d:propfind>`

// Scheduling finds the user's schedule-outbox-URL on their principal. A server
// that has none, or refuses the question, reports ErrNoScheduling.
func (c *Client) Scheduling(ctx context.Context) (Scheduling, error) {
	base := c.conn.Base.Path
	if base == "" {
		base = "/"
	}
	r, err := c.schedulingProps(ctx, base)
	if err != nil {
		return Scheduling{}, err
	}
	if out, ok := schedulingFrom(r); ok {
		return out, nil
	}
	principals := r.Prop(nsDAV, "current-user-principal").Hrefs()
	if len(principals) == 0 || samePath(principals[0], base) {
		return Scheduling{}, ErrNoScheduling
	}
	r, err = c.schedulingProps(ctx, principals[0])
	if err != nil {
		return Scheduling{}, err
	}
	if out, ok := schedulingFrom(r); ok {
		return out, nil
	}
	return Scheduling{}, ErrNoScheduling
}

func (c *Client) schedulingProps(ctx context.Context, p string) (dav.Response, error) {
	ms, err := c.conn.Do(ctx, dav.MethodPropfind, p, "0", []byte(propfindScheduling))
	var status *dav.StatusError
	if errors.As(err, &status) && status.Code >= 400 && status.Code < 500 && status.Code != http.StatusUnauthorized {
		return dav.Response{}, ErrNoScheduling
	}
	if err != nil {
		return dav.Response{}, fmt.Errorf("caldav: find scheduling outbox: %w", err)
	}
	if len(ms.Responses) == 0 {
		return dav.Response{}, ErrNoScheduling
	}
	return ms.Responses[0], nil
}

func schedulingFrom(r dav.Response) (Scheduling, bool) {
	outbox := r.Prop(nsCalDAV, "schedule-outbox-URL").Hrefs()
	if len(outbox) == 0 {
		return Scheduling{}, false
	}
	out := Scheduling{Outbox: outbox[0]}
	for _, h := range r.Prop(nsCalDAV, "calendar-user-address-set").Find(nsDAV, "href") {
		v := strings.TrimSpace(h.Text)
		if strings.HasPrefix(strings.ToLower(v), "mailto:") {
			out.Addresses = append(out.Addresses, ical.MailtoAddress(v))
		}
	}
	return out, true
}

// FreeBusyQuery asks for the busy time of some attendees.
type FreeBusyQuery struct {
	// Organizer is the address asking. It has to be one of the user's own
	// addresses on the server, or the server refuses the request.
	Organizer  string
	Attendees  []string
	Start, End time.Time
}

// RecipientFreeBusy is the server's answer for one attendee.
type RecipientFreeBusy struct {
	// Status is the request-status the server gave, such as "2.0;Success" or
	// "3.7;Invalid calendar user".
	Status string
	// Known is true when the server answered with the attendee's free time;
	// Busy is meaningless otherwise.
	Known bool
	Busy  []ical.BusyPeriod
}

// FreeBusy posts a VFREEBUSY request to the outbox and reads the
// schedule-response, keyed by the attendee address as asked, lower case. An
// attendee the response leaves out comes back unknown. A server that refuses
// the post as unsupported reports ErrNoScheduling.
func (c *Client) FreeBusy(ctx context.Context, outbox string, q FreeBusyQuery) (map[string]RecipientFreeBusy, error) {
	body := ical.FreeBusyRequest(uuid.NewString(), q.Organizer, q.Attendees, q.Start, q.End, time.Now())
	req, err := c.conn.Request(ctx, http.MethodPost, outbox, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", ical.MIMEType+"; charset=utf-8")
	resp, err := c.conn.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("caldav: free/busy request: %w", err)
	}
	defer dav.Drain(resp)
	switch resp.StatusCode {
	case http.StatusForbidden, http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented:
		return nil, ErrNoScheduling
	}
	if resp.StatusCode >= 400 {
		return nil, &dav.StatusError{Method: http.MethodPost, Path: outbox, Code: resp.StatusCode, Status: resp.Status}
	}
	parsed, err := parseScheduleResponse(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	out := make(map[string]RecipientFreeBusy, len(q.Attendees))
	for _, a := range q.Attendees {
		out[strings.ToLower(a)] = parsed[strings.ToLower(a)]
	}
	return out, nil
}

type rawScheduleResponse struct {
	Responses []struct {
		Recipient struct {
			Hrefs []string `xml:"DAV: href"`
		} `xml:"urn:ietf:params:xml:ns:caldav recipient"`
		RequestStatus string `xml:"urn:ietf:params:xml:ns:caldav request-status"`
		CalendarData  string `xml:"urn:ietf:params:xml:ns:caldav calendar-data"`
	} `xml:"urn:ietf:params:xml:ns:caldav response"`
}

// parseScheduleResponse reads a CalDAV schedule-response body (RFC 6638
// 6.1.4) into one answer per recipient address, lower case. A recipient
// counts as known only with a 2.x request-status and calendar data that
// parses.
func parseScheduleResponse(body io.Reader) (map[string]RecipientFreeBusy, error) {
	var raw rawScheduleResponse
	if err := xml.NewDecoder(body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("caldav: parse schedule-response: %w", err)
	}
	out := map[string]RecipientFreeBusy{}
	for _, r := range raw.Responses {
		if len(r.Recipient.Hrefs) == 0 {
			continue
		}
		addr := strings.ToLower(ical.MailtoAddress(r.Recipient.Hrefs[0]))
		answer := RecipientFreeBusy{Status: strings.TrimSpace(r.RequestStatus)}
		if strings.HasPrefix(answer.Status, "2.") && strings.TrimSpace(r.CalendarData) != "" {
			if busy, err := ical.ParseFreeBusy([]byte(r.CalendarData), time.Local); err == nil {
				answer.Known, answer.Busy = true, busy
			}
		}
		out[addr] = answer
	}
	return out, nil
}
