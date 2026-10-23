package imap

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// A message read in BODY.PEEK[]<offset.len> chunks from a real server puts
// back together byte for byte, the last chunk is short, reading past the end
// is empty, and \Seen stays unset.
func TestFetchPartialAgainstServer(t *testing.T) {
	c, wire := plainServer(t, strings.Repeat("big", 30000))
	uid := uidOf(t, c, strings.Repeat("big", 30000))
	whole, err := c.FetchRawMessage(uid)
	if err != nil {
		t.Fatal(err)
	}
	const size = 16 << 10
	var raw []byte
	for {
		msg, err := c.FetchPartial(uid, int64(len(raw)), size)
		if err != nil {
			t.Fatal(err)
		}
		if msg.UID != uid || msg.Subject == "" {
			t.Fatalf("chunk header %+v", msg)
		}
		raw = append(raw, msg.Raw...)
		if len(msg.Raw) < size {
			break
		}
	}
	if !bytes.Equal(raw, whole) {
		t.Fatalf("assembled %d B, want the %d B source", len(raw), len(whole))
	}
	if !strings.Contains(wire.String(), "BODY.PEEK[]<16384.16384>") {
		t.Fatal("no ranged BODY.PEEK on the wire")
	}
	past, err := c.FetchPartial(uid, int64(len(whole))+10, size)
	if err != nil || len(past.Raw) != 0 {
		t.Fatalf("past the end: %d B, %v", len(past.Raw), err)
	}
	if _, err := c.FetchPartial(uid+100, 0, size); !errors.Is(err, ErrNoMessage) {
		t.Fatalf("missing uid: %v, want ErrNoMessage", err)
	}
	flags, err := c.FetchHeaders([]imap.UID{uid}, &imap.FetchOptions{UID: true, Flags: true})
	if err != nil || len(flags) != 1 || len(flags[0].Flags) != 0 {
		t.Fatalf("flags after partial reads %+v, %v; want none", flags, err)
	}
}

// partialBytes takes the answer whose origin matches, and cuts the range out
// of a whole body a server sent without one.
func TestPartialBytes(t *testing.T) {
	whole := []byte("0123456789")
	for _, tc := range []struct {
		name   string
		resp   *imap.FetchItemBodySection
		data   []byte
		offset int64
		length int64
		want   string
		ok     bool
	}{
		{"origin echoed", &imap.FetchItemBodySection{Partial: &imap.SectionPartial{Offset: 4}}, []byte("4567"), 4, 4, "4567", true},
		{"other origin", &imap.FetchItemBodySection{Partial: &imap.SectionPartial{Offset: 0}}, []byte("0123"), 4, 4, "", false},
		{"no origin, whole body", &imap.FetchItemBodySection{}, whole, 4, 4, "4567", true},
		{"no origin, past the end", &imap.FetchItemBodySection{}, whole, 20, 4, "", true},
		{"header section ignored", &imap.FetchItemBodySection{Specifier: imap.PartSpecifierHeader}, whole, 0, 4, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			buf := &imapclient.FetchMessageBuffer{BodySection: []imapclient.FetchBodySectionBuffer{{Section: tc.resp, Bytes: tc.data}}}
			got, ok := partialBytes(buf, tc.offset, tc.length)
			if ok != tc.ok || string(got) != tc.want {
				t.Fatalf("got %q, %v; want %q, %v", got, ok, tc.want, tc.ok)
			}
		})
	}
}
