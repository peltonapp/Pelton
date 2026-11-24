package caldav

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/peltonapp/Pelton/internal/caldav/caldavtest"
	"github.com/peltonapp/Pelton/internal/dav"
)

const workPath = caldavtest.WorkPath

func event(uid, summary string) string {
	return "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:test\r\nBEGIN:VEVENT\r\nUID:" + uid +
		"\r\nDTSTART:20260301T090000Z\r\nSUMMARY:" + summary + "\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
}

func setup(t *testing.T) (*caldavtest.Server, *Client) {
	t.Helper()
	f := caldavtest.NewServer("me", "secret")
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	client, err := Connect(Config{URL: srv.URL + "/dav/", Username: "me", Password: "secret", HTTP: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	return f, client
}

func TestCalendarsFollowsPrincipalAndHome(t *testing.T) {
	_, client := setup(t)
	cals, err := client.Calendars(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(cals) != 2 {
		t.Fatalf("got %d calendars, want the event calendar and the task list: %+v", len(cals), cals)
	}
	c := cals[0]
	if c.Path != workPath || c.Name != "Work" || c.Color != "#ff8800" || c.ReadOnly || c.SyncToken == "" {
		t.Errorf("calendar = %+v", c)
	}
	if c.Components() != "VEVENT" || cals[1].Components() != "VTODO" || cals[1].Name != "Tasks" {
		t.Errorf("components = %q, %+v", c.Components(), cals[1])
	}
}

// A url pointing at the collection itself is that one calendar.
func TestCalendarsFromCollectionURL(t *testing.T) {
	f := caldavtest.NewServer("me", "secret")
	srv := httptest.NewServer(f)
	defer srv.Close()
	client, _ := Connect(Config{URL: srv.URL + workPath, Username: "me", Password: "secret", HTTP: srv.Client()})
	cals, err := client.Calendars(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(cals) != 1 || cals[0].Path != workPath {
		t.Errorf("calendars = %+v", cals)
	}
}

func TestSyncThenIncremental(t *testing.T) {
	f, client := setup(t)
	ctx := context.Background()
	f.Put(workPath+"a.ics", event("a", "First"))
	f.Put(workPath+"b b.ics", event("b", "Second"))

	first, err := client.Sync(ctx, workPath, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Updated) != 2 || len(first.Deleted) != 0 || first.Token == "" {
		t.Fatalf("first sync = %+v", first)
	}
	if !strings.Contains(string(first.Updated[1].Data), "SUMMARY:Second") || first.Updated[1].Path != workPath+"b b.ics" {
		t.Errorf("resource = %+v", first.Updated[1])
	}

	f.Put(workPath+"a.ics", event("a", "First, edited"))
	f.Remove(workPath + "b b.ics")

	next, err := client.Sync(ctx, workPath, first.Token)
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Updated) != 1 || !strings.Contains(string(next.Updated[0].Data), "First, edited") {
		t.Errorf("updated = %+v", next.Updated)
	}
	if len(next.Deleted) != 1 || next.Deleted[0] != workPath+"b b.ics" {
		t.Errorf("deleted = %v", next.Deleted)
	}
}

func TestSyncReportsInvalidToken(t *testing.T) {
	f, client := setup(t)
	f.RejectToken = true
	if _, err := client.Sync(context.Background(), workPath, "tok-stale"); !errors.Is(err, ErrSyncTokenInvalid) {
		t.Errorf("err = %v, want ErrSyncTokenInvalid", err)
	}
}

func TestListAndMultiget(t *testing.T) {
	f, client := setup(t)
	ctx := context.Background()
	f.Put(workPath+"a.ics", event("a", "A"))
	etags, err := client.List(ctx, workPath, "VEVENT")
	if err != nil {
		t.Fatal(err)
	}
	if len(etags) != 1 || etags[workPath+"a.ics"] == "" {
		t.Fatalf("etags = %v", etags)
	}
	res, err := client.Multiget(ctx, workPath, []string{workPath + "a.ics", workPath + "missing.ics"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 {
		t.Errorf("multiget = %+v", res)
	}
}

func TestWritesAreConditional(t *testing.T) {
	f, client := setup(t)
	ctx := context.Background()
	p := ResourcePath(workPath, "new")

	etag, err := client.Put(ctx, p, []byte(event("new", "Created")), dav.PreconditionNew, "")
	if err != nil || etag == "" {
		t.Fatalf("create = %q, %v", etag, err)
	}
	if _, err := client.Put(ctx, p, []byte(event("new", "Again")), dav.PreconditionNew, ""); !errors.Is(err, ErrConflict) {
		t.Errorf("create over existing = %v", err)
	}

	f.Put(p, event("new", "Changed on the phone"))
	if _, err := client.Put(ctx, p, []byte(event("new", "Mine")), dav.PreconditionMatch, etag); !errors.Is(err, ErrConflict) {
		t.Errorf("stale update = %v, want ErrConflict", err)
	}
	if err := client.Delete(ctx, p, etag); !errors.Is(err, ErrConflict) {
		t.Errorf("stale delete = %v, want ErrConflict", err)
	}
	server, err := client.Get(ctx, p)
	if err != nil || !strings.Contains(string(server.Data), "Changed on the phone") {
		t.Fatalf("get = %+v, %v", server, err)
	}
	if err := client.Delete(ctx, p, server.ETag); err != nil {
		t.Errorf("delete = %v", err)
	}
	if err := client.Delete(ctx, p, ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete = %v, want ErrNotFound", err)
	}
}

func TestWrongPasswordIsAnError(t *testing.T) {
	f := caldavtest.NewServer("me", "secret")
	srv := httptest.NewServer(f)
	defer srv.Close()
	client, _ := Connect(Config{URL: srv.URL, Username: "me", Password: "wrong", HTTP: srv.Client()})
	if _, err := client.Calendars(context.Background()); err == nil {
		t.Error("a refused login listed calendars")
	}
}

func TestNormalizeColor(t *testing.T) {
	for in, want := range map[string]string{"#FF8800FF": "#ff8800", "#123abc": "#123abc", "red": "", "#12": "", "": ""} {
		if got := NormalizeColor(in); got != want {
			t.Errorf("NormalizeColor(%q) = %q, want %q", in, got, want)
		}
	}
}
