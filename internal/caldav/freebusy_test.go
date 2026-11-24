package caldav

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/ical"
)

// schedServer is a CalDAV server with a scheduling outbox. principalProps is
// what a PROPFIND on the principal reports; the outbox answers with reply.
type schedServer struct {
	mu             sync.Mutex
	principalProps string
	outboxStatus   int
	reply          string
	posted         []byte
	postType       string
}

func (s *schedServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u, p, ok := r.BasicAuth(); !ok || u != "me" || p != "secret" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	switch {
	case r.Method == "PROPFIND" && r.URL.Path == "/dav/":
		w.WriteHeader(http.StatusMultiStatus)
		_, _ = io.WriteString(w, `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:"><d:response><d:href>/dav/</d:href>
<d:propstat><d:prop><d:current-user-principal><d:href>/dav/principals/me/</d:href></d:current-user-principal></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat>
<d:propstat><d:prop><c:schedule-outbox-URL xmlns:c="urn:ietf:params:xml:ns:caldav"/></d:prop><d:status>HTTP/1.1 404 Not Found</d:status></d:propstat>
</d:response></d:multistatus>`)
	case r.Method == "PROPFIND" && r.URL.Path == "/dav/principals/me/":
		w.WriteHeader(http.StatusMultiStatus)
		_, _ = io.WriteString(w, `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav"><d:response><d:href>/dav/principals/me/</d:href>
<d:propstat><d:prop>`+s.principalProps+`</d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response></d:multistatus>`)
	case r.Method == http.MethodPost && r.URL.Path == "/dav/outbox/":
		s.posted, _ = io.ReadAll(r.Body)
		s.postType = r.Header.Get("Content-Type")
		if s.outboxStatus != 0 {
			w.WriteHeader(s.outboxStatus)
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		_, _ = io.WriteString(w, s.reply)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

const withOutbox = `<c:schedule-outbox-URL><d:href>/dav/outbox/</d:href></c:schedule-outbox-URL>
<c:calendar-user-address-set><d:href>/dav/principals/me/</d:href><d:href>mailto:me@example.com</d:href><d:href>MAILTO:alias@example.com</d:href></c:calendar-user-address-set>`

func schedClient(t *testing.T, s *schedServer) *Client {
	t.Helper()
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	client, err := Connect(Config{URL: srv.URL + "/dav/", Username: "me", Password: "secret", HTTP: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestSchedulingFollowsPrincipalToOutbox(t *testing.T) {
	client := schedClient(t, &schedServer{principalProps: withOutbox})
	got, err := client.Scheduling(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Outbox != "/dav/outbox/" {
		t.Errorf("outbox = %q", got.Outbox)
	}
	if strings.Join(got.Addresses, ",") != "me@example.com,alias@example.com" {
		t.Errorf("addresses = %q", got.Addresses)
	}
}

func TestSchedulingWithoutOutbox(t *testing.T) {
	client := schedClient(t, &schedServer{principalProps: `<d:displayname>Me</d:displayname>`})
	if _, err := client.Scheduling(context.Background()); !errors.Is(err, ErrNoScheduling) {
		t.Fatalf("err = %v, want ErrNoScheduling", err)
	}
}

func TestSchedulingRefusedPropfindIsNoScheduling(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	defer srv.Close()
	client, _ := Connect(Config{URL: srv.URL + "/", HTTP: srv.Client()})
	if _, err := client.Scheduling(context.Background()); !errors.Is(err, ErrNoScheduling) {
		t.Fatalf("err = %v, want ErrNoScheduling", err)
	}
}

func TestSchedulingWrongPasswordIsAnError(t *testing.T) {
	s := &schedServer{principalProps: withOutbox}
	srv := httptest.NewServer(s)
	defer srv.Close()
	client, _ := Connect(Config{URL: srv.URL + "/dav/", Username: "me", Password: "wrong", HTTP: srv.Client()})
	_, err := client.Scheduling(context.Background())
	if err == nil || errors.Is(err, ErrNoScheduling) {
		t.Fatalf("err = %v, want an auth failure", err)
	}
}

const mixedReply = `<?xml version="1.0" encoding="utf-8"?>
<C:schedule-response xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">
  <C:response>
    <C:recipient><D:href>mailto:ada@example.com</D:href></C:recipient>
    <C:request-status>2.0;Success</C:request-status>
    <C:calendar-data>BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//Example//EN
METHOD:REPLY
BEGIN:VFREEBUSY
UID:x
DTSTAMP:20261116T080000Z
DTSTART:20261116T000000Z
DTEND:20261117T000000Z
ORGANIZER:mailto:me@example.com
ATTENDEE:mailto:ada@example.com
FREEBUSY;FBTYPE=BUSY:20261116T090000Z/20261116T100000Z
FREEBUSY;FBTYPE=BUSY-TENTATIVE:20261116T120000Z/PT30M
FREEBUSY;FBTYPE=BUSY-UNAVAILABLE:20261116T170000Z/20261116T180000Z,20261116T190000Z/20261116T200000Z
FREEBUSY;FBTYPE=FREE:20261116T130000Z/20261116T140000Z
FREEBUSY:20261116T150000Z/20261116T153000Z
END:VFREEBUSY
END:VCALENDAR
</C:calendar-data>
  </C:response>
  <C:response>
    <C:recipient><D:href>mailto:nobody@example.org</D:href></C:recipient>
    <C:request-status>3.7;Invalid calendar user</C:request-status>
  </C:response>
  <C:response>
    <C:recipient><D:href>mailto:Grace@Example.com</D:href></C:recipient>
    <C:request-status>2.0;Success</C:request-status>
    <C:calendar-data>BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//Example//EN
METHOD:REPLY
BEGIN:VFREEBUSY
UID:x
DTSTART:20261116T000000Z
DTEND:20261117T000000Z
END:VFREEBUSY
END:VCALENDAR
</C:calendar-data>
  </C:response>
</C:schedule-response>`

func TestFreeBusyRequestAndMixedResponse(t *testing.T) {
	s := &schedServer{principalProps: withOutbox, reply: mixedReply}
	client := schedClient(t, s)
	start := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	got, err := client.FreeBusy(context.Background(), "/dav/outbox/", FreeBusyQuery{
		Organizer: "me@example.com",
		Attendees: []string{"ada@example.com", "nobody@example.org", "grace@example.com", "silent@example.com"},
		Start:     start,
		End:       start.Add(24 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	body := strings.ReplaceAll(string(s.posted), "\r\n", "\n")
	if !strings.HasPrefix(s.postType, "text/calendar") {
		t.Errorf("content type = %q", s.postType)
	}
	for _, want := range []string{
		"BEGIN:VCALENDAR\n", "METHOD:REQUEST\n", "BEGIN:VFREEBUSY\n", "DTSTART:20261116T000000Z\n", "DTEND:20261117T000000Z\n",
		"ORGANIZER:mailto:me@example.com\n", "ATTENDEE:mailto:ada@example.com\n", "ATTENDEE:mailto:nobody@example.org\n", "DTSTAMP:", "UID:",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("request lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "BEGIN:VEVENT") {
		t.Errorf("request carries an event:\n%s", body)
	}

	ada := got["ada@example.com"]
	if !ada.Known || ada.Status != "2.0;Success" {
		t.Fatalf("ada = %+v", ada)
	}
	type period struct{ from, to, kind string }
	var periods []period
	for _, p := range ada.Busy {
		periods = append(periods, period{p.Start.UTC().Format("15:04"), p.End.UTC().Format("15:04"), p.Type})
	}
	want := []period{
		{"09:00", "10:00", ical.FBTypeBusy},
		{"12:00", "12:30", ical.FBTypeBusyTentative},
		{"15:00", "15:30", ical.FBTypeBusy},
		{"17:00", "18:00", ical.FBTypeBusyUnavailable},
		{"19:00", "20:00", ical.FBTypeBusyUnavailable},
	}
	if len(periods) != len(want) {
		t.Fatalf("periods = %v, want %v", periods, want)
	}
	for i := range want {
		if periods[i] != want[i] {
			t.Errorf("period %d = %v, want %v", i, periods[i], want[i])
		}
	}

	if nobody := got["nobody@example.org"]; nobody.Known || nobody.Status != "3.7;Invalid calendar user" {
		t.Errorf("nobody = %+v", nobody)
	}
	if grace := got["grace@example.com"]; !grace.Known || len(grace.Busy) != 0 {
		t.Errorf("grace = %+v, want known and free", grace)
	}
	if silent, ok := got["silent@example.com"]; !ok || silent.Known {
		t.Errorf("silent = %+v, %v, want an unknown entry", silent, ok)
	}
}

func TestFreeBusyRefusedPostIsNoScheduling(t *testing.T) {
	for _, code := range []int{http.StatusForbidden, http.StatusMethodNotAllowed, http.StatusNotImplemented} {
		client := schedClient(t, &schedServer{principalProps: withOutbox, outboxStatus: code})
		_, err := client.FreeBusy(context.Background(), "/dav/outbox/", FreeBusyQuery{Organizer: "me@example.com", Attendees: []string{"a@example.com"}, Start: time.Now(), End: time.Now().Add(time.Hour)})
		if !errors.Is(err, ErrNoScheduling) {
			t.Errorf("%d: err = %v, want ErrNoScheduling", code, err)
		}
	}
}

func TestFreeBusyServerErrorIsAnError(t *testing.T) {
	client := schedClient(t, &schedServer{principalProps: withOutbox, outboxStatus: http.StatusInternalServerError})
	_, err := client.FreeBusy(context.Background(), "/dav/outbox/", FreeBusyQuery{Organizer: "me@example.com", Attendees: []string{"a@example.com"}, Start: time.Now(), End: time.Now().Add(time.Hour)})
	if err == nil || errors.Is(err, ErrNoScheduling) {
		t.Fatalf("err = %v, want a server error", err)
	}
}

func TestFreeBusyHonoursCancellation(t *testing.T) {
	client := schedClient(t, &schedServer{principalProps: withOutbox, reply: mixedReply})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := client.FreeBusy(ctx, "/dav/outbox/", FreeBusyQuery{Organizer: "me@example.com", Attendees: []string{"a@example.com"}, Start: time.Now(), End: time.Now().Add(time.Hour)})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
