package imap

import (
	"slices"
	"testing"

	"github.com/emersion/go-imap/v2"
)

// Saving a draft again swaps out the earlier copy with its Message-ID, and
// removing one takes only that one; drafts with other Message-IDs stay. With
// UIDPLUS the new copy is known from APPENDUID, without it from a re-scan.
func TestDraftsAreReplacedAndRemovedByMessageID(t *testing.T) {
	for name, opts := range map[string]testOptions{
		"no UIDPLUS": {noAppendUID: true},
		"UIDPLUS":    {caps: imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapUIDPlus: {}}},
	} {
		t.Run(name, func(t *testing.T) {
			c, _ := newTestServer(t, opts)
			if err := createMailbox(c, "Drafts"); err != nil {
				t.Fatalf("create Drafts: %v", err)
			}
			for _, d := range [][2]string{
				{"<other@example.com>", "other"},
				{"<d1@example.com>", "first"},
				{"<gone@example.com>", "gone"},
				{"<d1@example.com>", "second"},
			} {
				raw := "From: me@example.com\r\nSubject: " + d[1] + "\r\nMessage-ID: " + d[0] + "\r\n\r\nbody\r\n"
				if err := c.ReplaceDraft("Drafts", []byte(raw), d[0]); err != nil {
					t.Fatalf("save %s %q: %v", d[0], d[1], err)
				}
			}
			if err := c.RemoveDraft("Drafts", "<gone@example.com>"); err != nil {
				t.Fatalf("RemoveDraft: %v", err)
			}

			if got := mailboxSubjects(t, c, "Drafts"); !slices.Equal(got, []string{"other", "second"}) {
				t.Fatalf("Drafts holds %v, want [other second]", got)
			}
			headers, err := c.FetchRecentHeaders(10)
			if err != nil {
				t.Fatalf("fetch: %v", err)
			}
			for _, h := range headers {
				if !slices.Contains(h.Flags, imap.FlagDraft) {
					t.Errorf("%q is not flagged \\Draft: %v", h.Subject, h.Flags)
				}
			}
		})
	}
}

// Gmail stores one message per Message-ID, so saving a draft again may add
// nothing and hand back the copy already there. Deleting the earlier copies
// then would delete the only one, with or without UIDPLUS to say so.
func TestReplaceDraftKeepsTheOnlyCopyWhenTheServerDeduplicates(t *testing.T) {
	for name, opts := range map[string]testOptions{
		"no UIDPLUS": {noAppendUID: true},
		"UIDPLUS":    {caps: imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapUIDPlus: {}}},
	} {
		t.Run(name, func(t *testing.T) {
			opts.dedupAppend = true
			c, _ := newTestServer(t, opts)
			if err := createMailbox(c, "Drafts"); err != nil {
				t.Fatalf("create Drafts: %v", err)
			}
			const id = "<d1@example.com>"
			raw := "From: me@example.com\r\nSubject: kept\r\nMessage-ID: " + id + "\r\n\r\nbody\r\n"
			if _, err := c.append("Drafts", []byte(raw), imap.FlagDraft); err != nil {
				t.Fatalf("seed: %v", err)
			}
			if err := c.ReplaceDraft("Drafts", []byte(raw), id); err != nil {
				t.Fatalf("ReplaceDraft: %v", err)
			}
			if got := mailboxSubjects(t, c, "Drafts"); !slices.Equal(got, []string{"kept"}) {
				t.Fatalf("Drafts holds %v, want [kept]", got)
			}
		})
	}
}

// A message with no Message-ID is not a copy of a draft with an empty one:
// removing by an empty id must leave everything alone.
func TestRemoveDraftWithEmptyMessageIDRemovesNothing(t *testing.T) {
	c, _ := plainServer(t, "no id")
	for _, id := range []string{"", "<>", "  "} {
		if err := c.RemoveDraft("INBOX", id); err != nil {
			t.Fatalf("RemoveDraft(%q): %v", id, err)
		}
	}
	if got := mailboxSubjects(t, c, "INBOX"); !slices.Equal(got, []string{"no id"}) {
		t.Fatalf("INBOX holds %v, want [no id]", got)
	}
}
