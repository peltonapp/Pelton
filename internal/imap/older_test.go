package imap

import (
	"context"
	"errors"
	"testing"

	goimap "github.com/emersion/go-imap/v2"

	psync "github.com/peltonapp/Pelton/internal/sync"
)

// sparseClient is a mailbox holding exactly uids, as a server does: a UID
// FETCH returns only the UIDs that exist. rows counts the rows returned,
// fullLists the SEARCH ALL and FETCH-everything commands.
type sparseClient struct {
	fakeSyncClient
	has       map[goimap.UID]bool
	rows      int
	envRows   int
	fullLists int
}

func newSparseClient(uids ...goimap.UID) *sparseClient {
	c := &sparseClient{has: map[goimap.UID]bool{}}
	for _, u := range uids {
		c.has[u] = true
	}
	c.mailbox = &Mailbox{Name: "INBOX", UIDValidity: 7, NumMessages: uint32(len(uids))}
	return c
}

func uidRange(lo, hi goimap.UID) []goimap.UID {
	var out []goimap.UID
	for u := lo; u <= hi; u++ {
		out = append(out, u)
	}
	return out
}

func (c *sparseClient) FetchHeaders(uids []goimap.UID, options *goimap.FetchOptions) ([]MessageHeader, error) {
	c.headerFetches = append(c.headerFetches, recordedHeaderFetch{uids: uids, options: options})
	var out []MessageHeader
	for _, u := range uids {
		if !c.has[u] {
			continue
		}
		h := MessageHeader{UID: u, Flags: []goimap.Flag{goimap.FlagSeen}}
		if options.Envelope {
			h.HasEnvelope, h.Subject = true, "s"
			c.envRows++
		}
		c.rows++
		out = append(out, h)
	}
	return out, nil
}

func (c *sparseClient) FetchAllFlags() ([]MessageHeader, error) {
	c.fullLists++
	return nil, errors.New("full list")
}

func (c *sparseClient) SearchAllUIDs() ([]goimap.UID, error) {
	c.fullLists++
	return nil, errors.New("search all")
}

func TestListBelowIsBoundedWhateverTheFolderHolds(t *testing.T) {
	for _, tc := range []struct {
		name       string
		uids       []goimap.UID
		below      string
		wantFirst  string
		wantLast   string
		wantN      int
		wantBottom bool
		maxRows    int
	}{
		// 2000 messages: one range of 50, then the 50 envelopes.
		{"dense", uidRange(1, 2000), "1951", "1950", "1901", 50, false, 100},
		// A gap of 489 UIDs below the floor: 4 ranges (50, 100, 200, 200).
		{"gap", append(uidRange(1, 1500), uidRange(1990, 2000)...), "1990", "1500", "1451", 50, false, 61 + 50},
		// Too sparse for the bounded look: fewer than 50, no bottom.
		{"sparse", append([]goimap.UID{5}, uidRange(1990, 2000)...), "1990", "", "", 0, false, 0},
		// The bottom of the folder.
		{"bottom", uidRange(1, 100), "31", "30", "1", 30, true, 60},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newSparseClient(tc.uids...)
			a := newAdapter(c)
			page, err := a.ListBelow(context.Background(), psync.RemoteMailbox{RemoteID: "INBOX", Generation: "7"}, tc.below, 50)
			if err != nil {
				t.Fatalf("ListBelow: %v", err)
			}
			if c.fullLists != 0 {
				t.Fatalf("%d full-folder commands, want none", c.fullLists)
			}
			if len(page.Headers) != tc.wantN || page.Bottom != tc.wantBottom {
				t.Fatalf("%d headers, bottom %v; want %d, %v", len(page.Headers), page.Bottom, tc.wantN, tc.wantBottom)
			}
			if tc.wantN > 0 && (page.Headers[0].RemoteID != tc.wantFirst || page.Headers[tc.wantN-1].RemoteID != tc.wantLast) {
				t.Fatalf("headers %s..%s, want %s..%s", page.Headers[0].RemoteID, page.Headers[tc.wantN-1].RemoteID, tc.wantFirst, tc.wantLast)
			}
			for _, h := range page.Headers {
				if !h.HasListMeta || h.Flags == 0 {
					t.Fatalf("header %s without list meta or flags", h.RemoteID)
				}
			}
			if c.rows > tc.maxRows || c.envRows > 50 {
				t.Fatalf("%d rows (%d envelopes), want at most %d", c.rows, c.envRows, tc.maxRows)
			}
			if len(c.headerFetches) > olderSteps+1 {
				t.Fatalf("%d fetches, want at most %d", len(c.headerFetches), olderSteps+1)
			}
			for _, f := range c.headerFetches {
				if len(f.uids) > olderSpanMax*50 {
					t.Fatalf("a range of %d UIDs, want at most %d", len(f.uids), olderSpanMax*50)
				}
			}
		})
	}
}

func TestListBelowRefusesAnotherGeneration(t *testing.T) {
	c := newSparseClient(uidRange(1, 10)...)
	_, err := newAdapter(c).ListBelow(context.Background(), psync.RemoteMailbox{RemoteID: "INBOX", Generation: "8"}, "5", 50)
	if !errors.Is(err, psync.ErrGenerationChanged) {
		t.Fatalf("err %v, want ErrGenerationChanged", err)
	}
	if len(c.headerFetches) != 0 {
		t.Fatal("fetched from a mailbox of another generation")
	}
}

func TestListBelowRejectsAFloorThatIsNoUID(t *testing.T) {
	c := newSparseClient(uidRange(1, 10)...)
	if _, err := newAdapter(c).ListBelow(context.Background(), psync.RemoteMailbox{RemoteID: "INBOX"}, "x", 50); err == nil {
		t.Fatal("a floor that is no UID was accepted")
	}
}
