package imap

import (
	"slices"
	"strconv"

	"github.com/emersion/go-imap/v2"
)

// Command line limits for a UID set (RESEARCH-batches.md): servers used to
// cap a line near 1000 B; RFC 7162 section 4 asks a CONDSTORE server to take
// at least 8192 B. K-9 and Thunderbird split the same way.
const (
	commandBytes          = 1000
	condstoreCommandBytes = 8000
	// commandOverhead leaves room for the tag, "UID FETCH" and the items.
	commandOverhead = 120
)

// uidSetBytes is how long the UID set of one command may be on this server.
func (c *Client) uidSetBytes() int {
	if c.raw.Caps().Has(imap.CapCondStore) {
		return condstoreCommandBytes - commandOverhead
	}
	return commandBytes - commandOverhead
}

// splitUIDSet builds UID sets from ranges of uids (any order, repeats
// allowed), each at most maxLen bytes when written out. A single range
// longer than maxLen cannot happen: two numbers and a colon are < 25 B.
func splitUIDSet(uids []imap.UID, maxLen int) []imap.UIDSet {
	if len(uids) == 0 {
		return nil
	}
	sorted := slices.Clone(uids)
	slices.Sort(sorted)
	sorted = slices.Compact(sorted)

	var (
		out  []imap.UIDSet
		cur  imap.UIDSet
		size int
	)
	for i := 0; i < len(sorted); {
		j := i
		for j+1 < len(sorted) && sorted[j+1] == sorted[j]+1 {
			j++
		}
		n := rangeLen(sorted[i], sorted[j])
		if len(cur) > 0 && size+1+n > maxLen {
			out = append(out, cur)
			cur, size = nil, 0
		}
		if len(cur) > 0 {
			size++ // the comma
		}
		cur = append(cur, imap.UIDRange{Start: sorted[i], Stop: sorted[j]})
		size += n
		i = j + 1
	}
	return append(out, cur)
}

// rangeLen is the written length of "start" or "start:stop".
func rangeLen(start, stop imap.UID) int {
	n := len(strconv.FormatUint(uint64(start), 10))
	if stop != start {
		n += 1 + len(strconv.FormatUint(uint64(stop), 10))
	}
	return n
}
