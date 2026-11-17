package imap

import (
	"fmt"

	"github.com/emersion/go-imap/v2"
)

// Move moves the message with the given UID to another mailbox. A mailbox must
// be selected.
func (c *Client) Move(uid imap.UID, mailbox string) error {
	return c.MoveMessages([]imap.UID{uid}, mailbox)
}

// CopyMessages copies the given UIDs to another mailbox, leaving them where
// they are. A mailbox must be selected.
func (c *Client) CopyMessages(uids []imap.UID, mailbox string) error {
	if len(uids) == 0 {
		return nil
	}
	if c.selectedMailbox() == nil {
		return fmt.Errorf("imap: no mailbox selected for copy")
	}
	if _, err := c.raw.Copy(imap.UIDSetNum(uids...), mailbox).Wait(); err != nil {
		return fmt.Errorf("imap: copy %d uid(s) to %q: %w", len(uids), mailbox, err)
	}
	return nil
}

// MoveMessages moves the given UIDs to another mailbox in one operation. A
// mailbox must be selected.
//
// When the server has the MOVE extension this is a single atomic command. When
// it does not, the move is a copy followed by a delete, and that delete goes
// through DeleteMessages so it stays scoped to these uids. go-imap has its own
// fallback for the same case, but it finishes with a plain EXPUNGE, which would
// take any message another client had merely flagged \Deleted (#276). So the
// fallback is done here instead.
func (c *Client) MoveMessages(uids []imap.UID, mailbox string) error {
	if len(uids) == 0 {
		return nil
	}
	if c.selectedMailbox() == nil {
		return fmt.Errorf("imap: no mailbox selected for move")
	}
	if c.raw.Caps().Has(imap.CapMove) {
		if _, err := c.raw.Move(imap.UIDSetNum(uids...), mailbox).Wait(); err != nil {
			return fmt.Errorf("imap: move %d uid(s) to %q: %w", len(uids), mailbox, err)
		}
		return nil
	}

	// copy first: if this fails nothing has been removed, and the mail is still
	// where it was.
	if _, err := c.raw.Copy(imap.UIDSetNum(uids...), mailbox).Wait(); err != nil {
		return fmt.Errorf("imap: copy %d uid(s) to %q: %w", len(uids), mailbox, err)
	}
	if err := c.DeleteMessages(uids...); err != nil {
		return fmt.Errorf("imap: move %d uid(s) to %q, copied but not removed from the source: %w", len(uids), mailbox, err)
	}
	return nil
}
