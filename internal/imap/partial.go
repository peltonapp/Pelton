package imap

import (
	"context"
	"errors"
	"fmt"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	psync "github.com/peltonapp/Pelton/internal/sync"
)

// ErrNoMessage means the selected mailbox has no message with the UID asked for.
var ErrNoMessage = errors.New("imap: no such message")

// FetchPartial fetches up to length bytes of a message's source from offset
// (BODY.PEEK[]<offset.length>), with its UID, flags and envelope. Raw holds
// the bytes; fewer than length mean the end of the message. Nothing is
// parsed. A UID the mailbox no longer has returns ErrNoMessage.
func (c *Client) FetchPartial(uid imap.UID, offset, length int64) (*Message, error) {
	if length <= 0 {
		return nil, fmt.Errorf("imap: partial fetch of uid %d with length %d", uid, length)
	}
	section := &imap.FetchItemBodySection{Peek: true, Partial: &imap.SectionPartial{Offset: offset, Size: length}}
	options := &imap.FetchOptions{
		Envelope:    true,
		Flags:       true,
		UID:         true,
		BodySection: []*imap.FetchItemBodySection{section},
	}
	buffers, err := c.raw.Fetch(imap.UIDSetNum(uid), options).Collect()
	if err != nil {
		return nil, fmt.Errorf("imap: fetch uid %d from %d: %w", uid, offset, err)
	}
	for _, buf := range buffers {
		if buf.UID != uid {
			continue
		}
		data, ok := partialBytes(buf, offset, length)
		if !ok {
			return nil, fmt.Errorf("imap: uid %d returned no body from %d", uid, offset)
		}
		return messageFromBuffer(buf, section, data), nil
	}
	return nil, fmt.Errorf("imap: uid %d: %w", uid, ErrNoMessage)
}

// partialBytes finds the BODY[]<offset> answer in buf. go-imap matches a
// partial section by its origin, which the server echoes. A server that
// ignores the range and sends the whole body (no origin) is cut here, so a
// chunk is never the whole message again.
func partialBytes(buf *imapclient.FetchMessageBuffer, offset, length int64) ([]byte, bool) {
	for _, s := range buf.BodySection {
		sec := s.Section
		if sec == nil || sec.Specifier != imap.PartSpecifierNone || len(sec.Part) > 0 || len(sec.HeaderFields) > 0 || len(sec.HeaderFieldsNot) > 0 {
			continue
		}
		switch {
		case sec.Partial != nil && sec.Partial.Offset == offset:
			return s.Bytes[:min(int64(len(s.Bytes)), length)], true
		case sec.Partial == nil:
			whole := s.Bytes
			start := min(offset, int64(len(whole)))
			return whole[start:min(start+length, int64(len(whole)))], true
		}
	}
	return nil, false
}

// FetchChunk returns up to length bytes of a message's source from offset in
// Raw, with its flags and envelope fields. Fewer bytes than length mean the
// end. A message the mailbox no longer has returns psync.ErrNotOnServer; a
// UIDVALIDITY other than the generation on ctx returns
// psync.ErrGenerationChanged, so chunks of two messages are never joined.
func (a *Adapter) FetchChunk(ctx context.Context, mailboxID, remoteID string, offset, length int64) (psync.Fetched, error) {
	uid, err := parseUID(remoteID)
	if err != nil {
		return psync.Fetched{}, err
	}
	if _, err := a.selectBox(mailboxID, psync.ExpectedGeneration(ctx, mailboxID)); err != nil {
		return psync.Fetched{}, err
	}
	msg, err := a.c.FetchPartial(uid, offset, length)
	if errors.Is(err, ErrNoMessage) {
		return psync.Fetched{}, fmt.Errorf("imap adapter: uid %d: %w", uid, psync.ErrNotOnServer)
	}
	if err != nil {
		return psync.Fetched{}, err
	}
	return messageToFetched(msg), nil
}

// ParseChunked fills the body fields of f from f.Raw, the whole source put
// together from chunks. The envelope fields and flags stay as fetched.
func (a *Adapter) ParseChunked(f psync.Fetched) (psync.Fetched, error) {
	msg := &Message{CharsetGuess: f.CharsetGuess}
	if err := parseBody(f.Raw, msg); err != nil {
		return f, &psync.MessageError{RemoteIDs: []string{f.RemoteID}, Err: err}
	}
	f.Text = msg.Text
	f.HTML = msg.HTML
	f.CharsetGuess = msg.CharsetGuess
	f.ListUnsubscribe = msg.ListUnsubscribe
	f.ListUnsubscribePost = msg.ListUnsubscribePost
	f.ReplyTo = msg.ReplyTo
	f.AuthResults = msg.AuthResults
	f.Attachments = fetchedAttachments(msg.Attachments)
	f.Size = int64(len(f.Raw))
	return f, nil
}
