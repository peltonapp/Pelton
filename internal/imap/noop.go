package imap

import "fmt"

// Noop sends NOOP, which checks that the server still answers on this
// connection.
func (c *Client) Noop() error {
	if err := c.raw.Noop().Wait(); err != nil {
		return fmt.Errorf("imap: noop: %w", err)
	}
	return nil
}
