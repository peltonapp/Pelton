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
	var n [4]uint64
	for i, p := range parts {
		v, err := strconv.ParseUint(p, 10, 64)
		if err != nil {
			return imapCursor{}, false
		}
		n[i] = v
	}
	if n[0] > 1<<32-1 || n[2] > 1<<32-1 || n[3] > 1<<32-1 {
		return imapCursor{}, false
	}
	return imapCursor{UIDValidity: uint32(n[0]), HighestModSeq: n[1], UIDNext: uint32(n[2]), Messages: uint32(n[3])}, true
}

// cursorFor is the cursor for a freshly selected mailbox, or "" when the
// server reports no HIGHESTMODSEQ and so cannot answer CHANGEDSINCE.
func cursorFor(m *Mailbox) string {
	if m == nil || m.HighestModSeq == 0 {
		return ""
	}
	return imapCursor{UIDValidity: m.UIDValidity, HighestModSeq: m.HighestModSeq, UIDNext: uint32(m.UIDNext), Messages: m.NumMessages}.String()
}
