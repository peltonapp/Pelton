package imap

import (
	"fmt"
	"slices"
	"strings"

	"github.com/emersion/go-imap/v2"
)

const (
	listReference  = ""  // root of the personal namespace
	listAllPattern = "*" // recurse all levels ("%" is top level only)
)

// Folder is one mailbox in the account hierarchy.
type Folder struct {
	Name string
	// Delimiter is the hierarchy separator; varies by provider ("/", "."), 0 if
	// flat. Never assume "/" when splitting paths.
	Delimiter rune
	Attrs     []imap.MailboxAttr
}

// Selectable reports whether the folder can hold messages and be selected.
func (f Folder) Selectable() bool {
	return !f.HasAttr(imap.MailboxAttrNoSelect) && !f.HasAttr(imap.MailboxAttrNonExistent)
}

// HasAttr reports whether the folder carries the given attribute.
func (f Folder) HasAttr(attr imap.MailboxAttr) bool {
	return slices.Contains(f.Attrs, attr)
}

// StripInboxNamespace returns path without a leading "INBOX" plus delim, the
// namespace some servers (Courier, Dovecot with prefix=INBOX.) put every folder
// under, so "INBOX.Sent" becomes "Sent". Any other path, and INBOX itself, is
// returned unchanged.
func StripInboxNamespace(path, delim string) string {
	const inbox = "INBOX"
	if delim == "" || len(path) <= len(inbox)+len(delim) {
		return path
	}
	if !strings.EqualFold(path[:len(inbox)], inbox) || path[len(inbox):len(inbox)+len(delim)] != delim {
		return path
	}
	return path[len(inbox)+len(delim):]
}

// ListFolders returns every mailbox, including non-selectable containers. An
// entry with an empty name is dropped: it cannot be selected, and a later
// SELECT of it is what a server answers with an invalid mailbox name.
func (c *Client) ListFolders() ([]Folder, error) {
	// nil options keeps this working on IMAP4rev1-only servers
	data, err := c.raw.List(listReference, listAllPattern, nil).Collect()
	if err != nil {
		return nil, fmt.Errorf("imap: list folders: %w", err)
	}

	folders := make([]Folder, 0, len(data))
	for _, d := range data {
		if d.Mailbox == "" {
			continue
		}
		folders = append(folders, Folder{
			Name:      d.Mailbox,
			Delimiter: d.Delim,
			Attrs:     d.Attrs,
		})
	}
	return folders, nil
}
