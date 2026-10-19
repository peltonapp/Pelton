package imap

import (
	"errors"
	"fmt"
	"strings"

	"github.com/emersion/go-imap/v2"
)

// ErrSpecialFolderNotFound means neither the special-use attribute nor any known
// fallback name located the requested folder. Callers treat appending to Sent as
// non-fatal, so this is surfaced as a warning rather than failing a send.
var ErrSpecialFolderNotFound = errors.New("imap: special-use folder not found")

// Fallback folder names by which servers commonly expose Sent and Drafts when
// they do not advertise the RFC 6154 special-use attribute.
var (
	sentFolderNames  = []string{"Sent", "Sent Items", "Sent Mail", "Gesendet"}
	draftFolderNames = []string{"Drafts", "Draft", "Entwürfe"}
)

// AppendToSent appends raw to the Sent folder with \Seen, since a message the
// user just sent is already read. It locates Sent by the \Sent special-use
// attribute, falling back to common names. Returns the folder used.
func (c *Client) AppendToSent(raw []byte) (string, error) {
	return c.appendSpecial(imap.MailboxAttrSent, sentFolderNames, raw, imap.FlagSeen)
}

// AppendToDrafts appends raw to the Drafts folder with the \Draft flag. It
// locates Drafts by the \Drafts special-use attribute, falling back to common
// names. Returns the folder used.
//
// Replacing an existing server draft is a later refinement: it would append the
// new version, then delete the previously appended draft by its uid. See the
// note in migration 0006.
func (c *Client) AppendToDrafts(raw []byte) (string, error) {
	return c.appendSpecial(imap.MailboxAttrDrafts, draftFolderNames, raw, imap.FlagDraft)
}

// appendSpecial resolves a special-use folder then appends raw with the flags.
func (c *Client) appendSpecial(attr imap.MailboxAttr, fallbacks []string, raw []byte, flags ...imap.Flag) (string, error) {
	mailbox, err := c.findSpecialFolder(attr, fallbacks)
	if err != nil {
		return "", err
	}
	if err := c.append(mailbox, raw, flags...); err != nil {
		return "", err
	}
	return mailbox, nil
}

// append uploads a message into a mailbox with the given flags via APPEND.
func (c *Client) append(mailbox string, raw []byte, flags ...imap.Flag) error {
	options := &imap.AppendOptions{Flags: flags}
	cmd := c.raw.Append(mailbox, int64(len(raw)), options)
	if _, err := cmd.Write(raw); err != nil {
		_ = cmd.Close()
		return fmt.Errorf("imap: append write to %q: %w", mailbox, err)
	}
	if err := cmd.Close(); err != nil {
		return fmt.Errorf("imap: append close to %q: %w", mailbox, err)
	}
	if _, err := cmd.Wait(); err != nil {
		return fmt.Errorf("imap: append to %q: %w", mailbox, err)
	}
	return nil
}

// findSpecialFolder returns the mailbox matching the special-use attribute, or
// the first matching fallback name, or ErrSpecialFolderNotFound.
//
// gmail diverges here: its folders are label views, but it does advertise
// \Sent and \Drafts special-use on the corresponding labels, so attribute
// matching works without special casing.
func (c *Client) findSpecialFolder(attr imap.MailboxAttr, fallbacks []string) (string, error) {
	folders, err := c.ListFolders()
	if err != nil {
		return "", err
	}

	for _, f := range folders {
		if f.HasAttr(attr) {
			return f.Name, nil
		}
	}
	for _, name := range fallbacks {
		for _, f := range folders {
			if strings.EqualFold(f.Name, name) {
				return f.Name, nil
			}
		}
	}
	return "", fmt.Errorf("%w: attr %q", ErrSpecialFolderNotFound, attr)
}
