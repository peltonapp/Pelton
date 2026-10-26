package imap

import (
	"fmt"
	"time"

	"github.com/emersion/go-imap/v2"
)

// SearchByMessageID returns the UIDs in the selected mailbox whose Message-ID
// header matches messageID. It backs undo-archive: after a move the message has a
// new UID in the destination, so we relocate it by its stable rfc Message-ID.
func (c *Client) SearchByMessageID(messageID string) ([]imap.UID, error) {
	if c.selectedMailbox() == nil {
		return nil, fmt.Errorf("imap: no mailbox selected for search")
	}
	if messageID == "" {
		return nil, fmt.Errorf("imap: empty message-id")
	}
	criteria := &imap.SearchCriteria{
		Header: []imap.SearchCriteriaHeaderField{{Key: "Message-Id", Value: messageID}},
	}
	data, err := c.raw.UIDSearch(criteria, nil).Wait()
	if err != nil {
		return nil, fmt.Errorf("imap: search message-id %q: %w", messageID, err)
	}
	return data.AllUIDs(), nil
}

// SearchSince returns the UIDs of messages in the selected mailbox whose
// internal date is on or after since. It backs the bulk offline download, which
// walks a date range and fetches whatever is not cached yet. A mailbox must be
// selected first.
func (c *Client) SearchSince(since time.Time) ([]imap.UID, error) {
	if c.selectedMailbox() == nil {
		return nil, fmt.Errorf("imap: no mailbox selected for search")
	}
	criteria := &imap.SearchCriteria{Since: since}
	data, err := c.raw.UIDSearch(criteria, nil).Wait()
	if err != nil {
		return nil, fmt.Errorf("imap: search since %s: %w", since.Format("2006-01-02"), err)
	}
	return data.AllUIDs(), nil
}

// SearchAllUIDs returns every UID in the selected mailbox, asking for the
// compact ESEARCH form when the server offers it, which keeps the answer a few
// ranges long even for a large mailbox.
func (c *Client) SearchAllUIDs() ([]imap.UID, error) {
	if c.selectedMailbox() == nil {
		return nil, fmt.Errorf("imap: no mailbox selected for search")
	}
	var options *imap.SearchOptions
	if c.raw.Caps().Has(imap.CapESearch) {
		options = &imap.SearchOptions{ReturnAll: true}
	}
	data, err := c.raw.UIDSearch(&imap.SearchCriteria{}, options).Wait()
	if err != nil {
		return nil, fmt.Errorf("imap: search all uids: %w", err)
	}
	return data.AllUIDs(), nil
}
