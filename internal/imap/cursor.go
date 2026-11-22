package imap

import (
	"fmt"
	"strconv"
	"strings"
)

// imapCursor is the delta position the adapter stores as a folder's state
// token: the mailbox generation plus the three numbers that move whenever its
// contents change. HIGHESTMODSEQ moves on a flag change or arrival, UIDNEXT
// on an arrival and MESSAGES on an arrival or expunge.
type imapCursor struct {
	UIDValidity   uint32
	HighestModSeq uint64
	UIDNext       uint32
	Messages      uint32
}

const cursorPrefix = "v1:"

func (c imapCursor) String() string {
	return fmt.Sprintf("%s%d:%d:%d:%d", cursorPrefix, c.UIDValidity, c.HighestModSeq, c.UIDNext, c.Messages)
}

// parseCursor reads a token written by imapCursor.String. Anything else,
// including an empty token or another adapter's state token, is not a
// cursor.
func parseCursor(s string) (imapCursor, bool) {
	rest, ok := strings.CutPrefix(s, cursorPrefix)
	if !ok {
		return imapCursor{}, false
	}
	parts := strings.Split(rest, ":")
	if len(parts) != 4 {
		return imapCursor{}, false
	}
	validity, err := strconv.ParseUint(parts[0], 10, 32)
	if err != nil {
		return imapCursor{}, false
	}
	modSeq, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil {
		return imapCursor{}, false
	}
	next, err := strconv.ParseUint(parts[2], 10, 32)
	if err != nil {
		return imapCursor{}, false
	}
	messages, err := strconv.ParseUint(parts[3], 10, 32)
	if err != nil {
		return imapCursor{}, false
	}
	return imapCursor{UIDValidity: uint32(validity), HighestModSeq: modSeq, UIDNext: uint32(next), Messages: uint32(messages)}, true
}

// cursorFor is the cursor for a freshly selected mailbox, or "" when the
// server reports no HIGHESTMODSEQ and so cannot answer CHANGEDSINCE.
func cursorFor(m *Mailbox) string {
	if m == nil || m.HighestModSeq == 0 {
		return ""
	}
	return imapCursor{UIDValidity: m.UIDValidity, HighestModSeq: m.HighestModSeq, UIDNext: uint32(m.UIDNext), Messages: m.NumMessages}.String()
}
