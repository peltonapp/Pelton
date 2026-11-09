package imap

import (
	"errors"
	"testing"

	"github.com/emersion/go-imap/v2"
)

func TestStripInboxNamespace(t *testing.T) {
	for _, tc := range []struct{ path, delim, want string }{
		{"INBOX.Sent", ".", "Sent"},
		{"inbox/Sent Items", "/", "Sent Items"},
		{"INBOX.Archive.2024", ".", "Archive.2024"},
		{"INBOX", ".", "INBOX"},
		{"INBOX.", ".", "INBOX."},
		{"INBOXES.Sent", ".", "INBOXES.Sent"},
		{"INBOX.Sent", "", "INBOX.Sent"},
		{"Sent", ".", "Sent"},
	} {
		if got := StripInboxNamespace(tc.path, tc.delim); got != tc.want {
			t.Errorf("StripInboxNamespace(%q, %q) = %q, want %q", tc.path, tc.delim, got, tc.want)
		}
	}
}

// A server that lists every folder under INBOX. with a dot delimiter and no
// special-use attributes still resolves Sent and Drafts by name.
func TestFallbackNameMatchesInsideInboxNamespace(t *testing.T) {
	folders := []Folder{
		{Name: "INBOX", Delimiter: '.', Attrs: []imap.MailboxAttr{imap.MailboxAttrHasChildren}},
		{Name: "INBOX.Drafts", Delimiter: '.'},
		{Name: "INBOX.Sent Items", Delimiter: '.'},
		{Name: "INBOX.Archive.Sent", Delimiter: '.'},
	}
	got, err := matchFallbackName(folders, sentFolderNames, imap.MailboxAttrSent)
	if err != nil || got != "INBOX.Sent Items" {
		t.Fatalf("sent = %q, %v, want INBOX.Sent Items", got, err)
	}
	got, err = matchFallbackName(folders, draftFolderNames, imap.MailboxAttrDrafts)
	if err != nil || got != "INBOX.Drafts" {
		t.Fatalf("drafts = %q, %v, want INBOX.Drafts", got, err)
	}
	if _, err := matchFallbackName(folders[:1], sentFolderNames, imap.MailboxAttrSent); !errors.Is(err, ErrSpecialFolderNotFound) {
		t.Fatalf("no Sent folder: err = %v, want ErrSpecialFolderNotFound", err)
	}
}

// The post-send copy lands in a Sent folder that lives under INBOX.
func TestAppendToSentUnderInboxNamespace(t *testing.T) {
	c, _ := newTestServer(t, testOptions{})
	if err := createMailbox(c, "INBOX/Sent"); err != nil {
		t.Fatalf("create: %v", err)
	}
	raw := []byte("From: me@example.com\r\nSubject: sent copy\r\n\r\nbody\r\n")
	folder, err := c.AppendToSent(raw)
	if err != nil {
		t.Fatalf("AppendToSent: %v", err)
	}
	if folder != "INBOX/Sent" {
		t.Fatalf("folder = %q, want INBOX/Sent", folder)
	}
	if got := mailboxSubjects(t, c, "INBOX/Sent"); len(got) != 1 || got[0] != "sent copy" {
		t.Fatalf("Sent holds %v", got)
	}
}

// A \Noselect container named like Sent is not a place to append to.
func TestMatchFallbackSkipsNonSelectable(t *testing.T) {
	folders := []Folder{{Name: "Sent", Attrs: []imap.MailboxAttr{imap.MailboxAttrNoSelect}}}
	if _, err := matchFallbackName(folders, sentFolderNames, imap.MailboxAttrSent); !errors.Is(err, ErrSpecialFolderNotFound) {
		t.Fatalf("err = %v, want ErrSpecialFolderNotFound", err)
	}
}
