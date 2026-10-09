package imap

import (
	"fmt"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/peltonapp/Pelton/internal/charsetguess"
	"github.com/peltonapp/Pelton/internal/rfc822"
)

// Mailbox summarises a selected mailbox.
type Mailbox struct {
	Name        string
	NumMessages uint32
	UIDNext     imap.UID
	// UIDValidity invalidates cached UIDs when it changes.
	UIDValidity uint32
	// HighestModSeq is the mailbox's CONDSTORE mod-sequence, or 0 when the
	// server has no CONDSTORE or reports NOMODSEQ for this mailbox.
	HighestModSeq uint64
}

// MessageHeader is the envelope-level summary used for listings.
type MessageHeader struct {
	SeqNum   uint32
	UID      imap.UID
	Subject  string
	From     string
	FromName string
	To       string
	Date     time.Time
	Flags    []imap.Flag
	// HasEnvelope is true when this header was filled from a FETCH ENVELOPE.
	// A flags-only snapshot leaves it false.
	HasEnvelope bool
}

// Message is a fully parsed message with extracted bodies and attachments.
type Message struct {
	UID         imap.UID
	MessageID   string // rfc Message-ID header, for threading and dedup
	Subject     string
	From        string
	To          string
	Cc          string
	Date        time.Time
	Flags       []imap.Flag
	Text        string
	HTML        string
	Size        int64 // raw rfc822 byte length
	Attachments []Attachment
	// Raw is the message exactly as the server sent it. A signature is computed
	// over these bytes, so verifying one means checking them rather than
	// anything reassembled from the parsed fields above. It is handed to the
	// sync layer and dropped once the message is stored; nothing caches it.
	Raw []byte
	// ListUnsubscribe carries the raw List-Unsubscribe header value, and
	// ListUnsubscribePost whether the message declares RFC 8058 one-click
	// support via List-Unsubscribe-Post.
	ListUnsubscribe     string
	ListUnsubscribePost bool
	// ReplyTo is the Reply-To header value ('' when there is none), and
	// AuthResults every Authentication-Results header in the order they appear.
	// Both are read here because the raw bytes are dropped after storing, so
	// nothing downstream can go back for them.
	ReplyTo     string
	AuthResults []string
	// CharsetGuess names what the body was read as when the message declared no
	// charset or one nothing knows, and is empty for mail that was right about
	// itself. It is kept so the reader can be told the text was guessed at.
	CharsetGuess string
}

// Attachment holds attachment metadata and its decoded content. It is the
// parser's type: the imap and file-import paths produce the same attachments,
// so callers can handle both without converting.
type Attachment = rfc822.Attachment

// Select opens a mailbox. IMAP selects one mailbox per connection, so this
// must precede the fetch and flag methods. When the server offers CONDSTORE the
// mailbox is selected with it, so HighestModSeq is filled.
func (c *Client) Select(mailbox string) (*Mailbox, error) {
	// go-imap encodes non-ASCII names as modified UTF-7 when needed
	data, err := c.raw.Select(mailbox, &imap.SelectOptions{CondStore: c.raw.Caps().Has(imap.CapCondStore)}).Wait()
	if err != nil {
		c.setSelectedMailbox(nil)
		return nil, fmt.Errorf("imap: select %q: %w", mailbox, err)
	}
	m := &Mailbox{
		Name:        mailbox,
		NumMessages: data.NumMessages,
		UIDNext:     data.UIDNext,
		UIDValidity: data.UIDValidity,

		HighestModSeq: data.HighestModSeq,
	}
	// recorded from the response we already hold rather than read back off the
	// client afterwards, which is racy. See Client.selected.
	c.setSelectedMailbox(m)
	return m, nil
}

// FetchRecentHeaders returns up to limit recent messages, newest first. No
// bodies are fetched, so it stays cheap on large mailboxes.
func (c *Client) FetchRecentHeaders(limit int) ([]MessageHeader, error) {
	mbox := c.selectedMailbox()
	if mbox == nil {
		return nil, fmt.Errorf("imap: no mailbox selected")
	}
	total := mbox.NumMessages
	if total == 0 || limit <= 0 {
		return nil, nil
	}

	// take the tail window by sequence number; each header also carries the
	// stable UID since sequence numbers shift as the mailbox changes
	var start uint32 = 1
	if uint32(limit) < total {
		start = total - uint32(limit) + 1
	}
	seqSet := imap.SeqSet{}
	seqSet.AddRange(start, total)

	options := &imap.FetchOptions{Envelope: true, Flags: true, UID: true}
	buffers, err := c.raw.Fetch(seqSet, options).Collect()
	if err != nil {
		return nil, fmt.Errorf("imap: fetch headers: %w", err)
	}

	headers := make([]MessageHeader, 0, len(buffers))
	for _, b := range buffers {
		headers = append(headers, headerFromBuffer(b))
	}
	reverse(headers) // ascending -> newest first
	return headers, nil
}

// bodyFetchOptions asks for everything a stored message needs: the envelope for
// the list columns, the flags, and the source with PEEK so reading it does not
// set \Seen. The section value is also how the body is found in the response,
// so it is built once and shared.
func bodyFetchOptions() (*imap.FetchItemBodySection, *imap.FetchOptions) {
	section := &imap.FetchItemBodySection{Peek: true}
	return section, &imap.FetchOptions{
		Envelope:    true,
		Flags:       true,
		UID:         true,
		BodySection: []*imap.FetchItemBodySection{section},
	}
}

// FetchMessages fetches a set of messages in one command and hands each to fn as
// it arrives.
//
// One command rather than one per message is the difference between a first
// sync being bound by bandwidth and being bound by round trips: 14k messages
// fetched one at a time is 14k times the latency to the server before a single
// byte of anything else moves (#310). Messages are handled as they stream in, so
// only one is held at a time and rows appear while the rest are still coming.
//
// fn is called once per message the server returns, in the order it returns
// them, with the parse error instead of a message when the source could not be
// walked. The caller decides what a bad message costs: returning an error from
// fn abandons the rest of the batch, returning nil carries on. The connection
// is left clean either way.
func (c *Client) FetchMessages(uids []imap.UID, fn func(uid imap.UID, msg *Message, err error) error) error {
	if len(uids) == 0 {
		return nil
	}
	section, options := bodyFetchOptions()
	cmd := c.raw.Fetch(imap.UIDSetNum(uids...), options)
	defer cmd.Close()

	for {
		data := cmd.Next()
		if data == nil {
			break
		}
		buf, err := data.Collect()
		if err != nil {
			return fmt.Errorf("imap: fetch batch of %d: %w", len(uids), err)
		}
		raw := buf.FindBodySection(section)
		if raw == nil {
			// the server listed the message but gave no body for it. Skipping it
			// leaves it uncached and the next sync asks again, which is better
			// than storing an empty message.
			continue
		}
		msg := messageFromBuffer(buf, section, raw)
		parseErr := parseBody(raw, msg)
		if parseErr != nil {
			msg = nil
		}
		if err := fn(buf.UID, msg, parseErr); err != nil {
			return err
		}
	}
	if err := cmd.Close(); err != nil {
		return fmt.Errorf("imap: fetch batch of %d: %w", len(uids), err)
	}
	return nil
}

// FetchMessage fetches and parses a full message by UID.
func (c *Client) FetchMessage(uid imap.UID) (*Message, error) {
	section, options := bodyFetchOptions()

	buffers, err := c.raw.Fetch(imap.UIDSetNum(uid), options).Collect()
	if err != nil {
		return nil, fmt.Errorf("imap: fetch message uid %d: %w", uid, err)
	}
	if len(buffers) == 0 {
		return nil, fmt.Errorf("imap: message uid %d not found", uid)
	}
	buf := buffers[0]

	raw := buf.FindBodySection(section)
	if raw == nil {
		return nil, fmt.Errorf("imap: message uid %d returned no body", uid)
	}
	msg := messageFromBuffer(buf, section, raw)
	if err := parseBody(raw, msg); err != nil {
		return nil, fmt.Errorf("imap: parse message uid %d: %w", uid, err)
	}
	return msg, nil
}

// messageFromBuffer turns one fetch response into a Message, envelope fields and
// all. The body walk is the caller's, since the single fetch reports a parse
// failure as its own error and the batch one keeps going.
func messageFromBuffer(buf *imapclient.FetchMessageBuffer, section *imap.FetchItemBodySection, raw []byte) *Message {
	msg := &Message{UID: buf.UID, Flags: buf.Flags, Size: int64(len(raw)), Raw: raw}
	if buf.Envelope != nil {
		msg.MessageID = buf.Envelope.MessageID
		msg.Subject, msg.CharsetGuess = charsetguess.Text(buf.Envelope.Subject)
		msg.From = formatAddresses(buf.Envelope.From)
		msg.To = formatAddresses(buf.Envelope.To)
		msg.Cc = formatAddresses(buf.Envelope.Cc)
		msg.Date = buf.Envelope.Date
	}
	return msg
}

// FetchRawMessage returns a message's RFC 822 source by UID, exactly as the
// server stores it, undecoded and unparsed (PEEK, so reading it never sets
// \Seen).
func (c *Client) FetchRawMessage(uid imap.UID) ([]byte, error) {
	section := &imap.FetchItemBodySection{Peek: true}
	options := &imap.FetchOptions{
		UID:         true,
		BodySection: []*imap.FetchItemBodySection{section},
	}

	buffers, err := c.raw.Fetch(imap.UIDSetNum(uid), options).Collect()
	if err != nil {
		return nil, fmt.Errorf("imap: fetch raw message uid %d: %w", uid, err)
	}
	if len(buffers) == 0 {
		return nil, fmt.Errorf("imap: message uid %d not found", uid)
	}
	raw := buffers[0].FindBodySection(section)
	if raw == nil {
		return nil, fmt.Errorf("imap: message uid %d returned no body", uid)
	}
	return raw, nil
}

// FetchAllFlags returns the UID and flags of every message in the selected
// mailbox and nothing else, so it stays cheap. sync diffs this against the
// local cache to find new, deleted and reflagged messages. over very large
// mailboxes CONDSTORE would let us ask only for what changed since last sync,
// but a full compare is correct and is enough for now.
func (c *Client) FetchAllFlags() ([]MessageHeader, error) {
	mbox := c.selectedMailbox()
	if mbox == nil {
		return nil, fmt.Errorf("imap: no mailbox selected")
	}
	if mbox.NumMessages == 0 {
		return nil, nil
	}

	seqSet := imap.SeqSet{}
	seqSet.AddRange(1, mbox.NumMessages)
	options := &imap.FetchOptions{Flags: true, UID: true}
	buffers, err := c.raw.Fetch(seqSet, options).Collect()
	if err != nil {
		return nil, fmt.Errorf("imap: fetch all flags: %w", err)
	}

	headers := make([]MessageHeader, 0, len(buffers))
	for _, b := range buffers {
		headers = append(headers, MessageHeader{UID: b.UID, Flags: b.Flags})
	}
	return headers, nil
}

// FetchFlagsChangedSince returns UID and flags for every message in the
// selected mailbox whose mod-sequence is above modSeq (RFC 7162 CHANGEDSINCE).
// It needs a mailbox selected with CONDSTORE.
func (c *Client) FetchFlagsChangedSince(modSeq uint64) ([]MessageHeader, error) {
	mbox := c.selectedMailbox()
	if mbox == nil {
		return nil, fmt.Errorf("imap: no mailbox selected")
	}
	if mbox.NumMessages == 0 {
		return nil, nil
	}
	seqSet := imap.SeqSet{}
	seqSet.AddRange(1, mbox.NumMessages)
	buffers, err := c.raw.Fetch(seqSet, &imap.FetchOptions{Flags: true, UID: true, ChangedSince: modSeq}).Collect()
	if err != nil {
		return nil, fmt.Errorf("imap: fetch flags changed since %d: %w", modSeq, err)
	}
	headers := make([]MessageHeader, 0, len(buffers))
	for _, b := range buffers {
		headers = append(headers, MessageHeader{UID: b.UID, Flags: b.Flags})
	}
	return headers, nil
}

// FetchHeaders fetches the requested items for uids in one UID FETCH. Callers
// that only need list stubs pass ENVELOPE and UID, with no body section.
func (c *Client) FetchHeaders(uids []imap.UID, options *imap.FetchOptions) ([]MessageHeader, error) {
	if len(uids) == 0 {
		return nil, nil
	}
	if c.selectedMailbox() == nil {
		return nil, fmt.Errorf("imap: no mailbox selected")
	}
	if options == nil {
		options = &imap.FetchOptions{Envelope: true, UID: true}
	}
	buffers, err := c.raw.Fetch(imap.UIDSetNum(uids...), options).Collect()
	if err != nil {
		return nil, fmt.Errorf("imap: fetch envelopes: %w", err)
	}
	headers := make([]MessageHeader, 0, len(buffers))
	for _, b := range buffers {
		headers = append(headers, headerFromBuffer(b))
	}
	return headers, nil
}

func headerFromBuffer(b *imapclient.FetchMessageBuffer) MessageHeader {
	h := MessageHeader{SeqNum: b.SeqNum, UID: b.UID, Flags: b.Flags}
	if b.Envelope != nil {
		h.HasEnvelope = true
		h.Subject, _ = charsetguess.Text(b.Envelope.Subject)
		h.From = formatAddresses(b.Envelope.From)
		h.FromName = envelopeName(b.Envelope.From)
		h.To = formatAddresses(b.Envelope.To)
		h.Date = b.Envelope.Date
	}
	return h
}

func envelopeName(addrs []imap.Address) string {
	if len(addrs) == 0 {
		return ""
	}
	name, _ := charsetguess.Text(addrs[0].Name)
	return name
}

// parseBody extracts text, HTML and attachment metadata from a raw message.
// The envelope fields come from the server's ENVELOPE response instead, so only
// the body is walked here.
func parseBody(raw []byte, msg *Message) error {
	parsed, err := rfc822.ParseBody(raw)
	if err != nil {
		return err
	}
	msg.Text = parsed.Text
	msg.HTML = parsed.HTML
	if msg.CharsetGuess == "" {
		msg.CharsetGuess = parsed.CharsetGuess
	}
	msg.Attachments = parsed.Attachments
	msg.ListUnsubscribe = parsed.ListUnsubscribe
	msg.ListUnsubscribePost = parsed.ListUnsubscribePost
	msg.ReplyTo = parsed.ReplyTo
	msg.AuthResults = parsed.AuthResults
	return nil
}
