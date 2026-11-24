package imap

import (
	"fmt"

	"github.com/emersion/go-imap/v2"
)

// AddFlags adds flags to the message with the given UID (additive, idempotent).
func (c *Client) AddFlags(uid imap.UID, flags ...imap.Flag) error {
	return c.store(uid, imap.StoreFlagsAdd, flags)
}

// RemoveFlags clears flags from the message with the given UID.
func (c *Client) RemoveFlags(uid imap.UID, flags ...imap.Flag) error {
	return c.store(uid, imap.StoreFlagsDel, flags)
}

// store issues a UID STORE; UID keeps the target stable across changes.
func (c *Client) store(uid imap.UID, op imap.StoreFlagsOp, flags []imap.Flag) error {
	if len(flags) == 0 {
		return fmt.Errorf("imap: no flags given for store on uid %d", uid)
	}
	storeFlags := &imap.StoreFlags{Op: op, Flags: flags, Silent: true}
	if err := c.raw.Store(imap.UIDSetNum(uid), storeFlags, nil).Close(); err != nil {
		return fmt.Errorf("imap: store flags on uid %d: %w", uid, err)
	}
	return nil
}

// FetchFlags returns the current flags of the message with the given UID.
func (c *Client) FetchFlags(uid imap.UID) ([]imap.Flag, error) {
	options := &imap.FetchOptions{Flags: true, UID: true}
	buffers, err := c.raw.Fetch(imap.UIDSetNum(uid), options).Collect()
	if err != nil {
		return nil, fmt.Errorf("imap: fetch flags for uid %d: %w", uid, err)
	}
	if len(buffers) == 0 {
		return nil, fmt.Errorf("imap: message uid %d not found", uid)
	}
	return buffers[0].Flags, nil
}
