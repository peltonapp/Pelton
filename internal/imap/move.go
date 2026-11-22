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

// MoveUID moves the message with the given UID to another mailbox, like Move,
// and reports the UID the server gave the message there and the destination's
// UIDVALIDITY, from the COPYUID response code (RFC 4315 UIDPLUS). Both are 0
// when the server sent no COPYUID; the move itself has still happened then.
func (c *Client) MoveUID(uid imap.UID, mailbox string) (destUID imap.UID, uidValidity uint32, err error) {
	data, err := c.moveMessages([]imap.UID{uid}, mailbox)
	if err != nil || data == nil || data.UIDValidity == 0 {
		return 0, 0, err
	}
	dest, ok := data.DestUIDs.Nums()
	if !ok || len(dest) != 1 {
		return 0, 0, nil
	}
	return dest[0], data.UIDValidity, nil
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
	_, err := c.moveMessages(uids, mailbox)
	return err
}

// moveMessages is MoveMessages, returning what the server's COPYUID said (zero
// fields when it said nothing). The data is nil when there was nothing to move.
func (c *Client) moveMessages(uids []imap.UID, mailbox string) (*imap.CopyData, error) {
	if len(uids) == 0 {
		return nil, nil
	}
	if c.selectedMailbox() == nil {
		return nil, fmt.Errorf("imap: no mailbox selected for move")
	}
	if c.raw.Caps().Has(imap.CapMove) {
		data, err := c.raw.Move(imap.UIDSetNum(uids...), mailbox).Wait()
		if err != nil {
			return nil, fmt.Errorf("imap: move %d uid(s) to %q: %w", len(uids), mailbox, err)
		}
		out := &imap.CopyData{UIDValidity: data.UIDValidity}
		if s, ok := data.SourceUIDs.(imap.UIDSet); ok {
			out.SourceUIDs = s
		}
		if d, ok := data.DestUIDs.(imap.UIDSet); ok {
			out.DestUIDs = d
		}
		return out, nil
	}

	// copy first: if this fails nothing has been removed, and the mail is still
	// where it was.
	data, err := c.raw.Copy(imap.UIDSetNum(uids...), mailbox).Wait()
	if err != nil {
		return nil, fmt.Errorf("imap: copy %d uid(s) to %q: %w", len(uids), mailbox, err)
	}
	if err := c.DeleteMessages(uids...); err != nil {
		return nil, fmt.Errorf("imap: move %d uid(s) to %q, copied but not removed from the source: %w", len(uids), mailbox, err)
	}
	return data, nil
}
