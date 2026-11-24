package imap

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2"
)

func TestSplitUIDSet(t *testing.T) {
	var sparse, wide []imap.UID
	for i := range 500 {
		sparse = append(sparse, imap.UID(1000+2*i)) // no two adjacent
		wide = append(wide, imap.UID(4_000_000_000-3*i))
	}
	for _, tc := range []struct {
		name    string
		uids    []imap.UID
		maxLen  int
		wantSet []string // nil: only check limits and coverage
		minSets int
		maxSets int
	}{
		{"empty", nil, 100, nil, 0, 0},
		{"ranges from runs", []imap.UID{5, 3, 4, 9, 1, 10, 4}, 100, []string{"1,3:5,9:10"}, 1, 1},
		{"split at the limit", []imap.UID{1, 3, 5, 7, 9}, 3, []string{"1,3", "5,7", "9"}, 3, 3},
		// 500 four-digit uids are about 2500 B: three plain lines, one CONDSTORE line.
		{"sparse 500, plain line", sparse, commandBytes - commandOverhead, nil, 3, 3},
		{"sparse 500, condstore line", sparse, condstoreCommandBytes - commandOverhead, nil, 1, 1},
		{"ten digit uids", wide, commandBytes - commandOverhead, nil, 6, 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sets := splitUIDSet(tc.uids, tc.maxLen)
			var got []string
			var covered []imap.UID
			for _, s := range sets {
				str := s.String()
				if len(str) > tc.maxLen {
					t.Fatalf("set %d B over %d: %s", len(str), tc.maxLen, str)
				}
				got = append(got, str)
				nums, _ := s.Nums()
				covered = append(covered, nums...)
			}
			if len(sets) < tc.minSets || len(sets) > tc.maxSets {
				t.Fatalf("%d sets, want %d..%d", len(sets), tc.minSets, tc.maxSets)
			}
			if tc.wantSet != nil && !slices.Equal(got, tc.wantSet) {
				t.Fatalf("sets %v, want %v", got, tc.wantSet)
			}
			want := slices.Compact(slices.Sorted(slices.Values(tc.uids)))
			slices.Sort(covered)
			if !slices.Equal(covered, want) {
				t.Fatalf("sets cover %d uids, want %d", len(covered), len(want))
			}
		})
	}
}

// A list-meta fetch of 500 scattered UIDs keeps every command line on the
// wire within 1000 B. (The test server drops CONDSTORE from its caps, so the
// 8000 B line is covered by TestSplitUIDSet.)
func TestFetchHeadersKeepsCommandLinesShort(t *testing.T) {
	subjects := make([]string, 1000)
	for i := range subjects {
		subjects[i] = fmt.Sprintf("m%d", i)
	}
	var odd []imap.UID
	for uid := 1; uid <= 1000; uid += 2 {
		odd = append(odd, imap.UID(uid))
	}
	c, wire := plainServer(t, subjects...)
	got, err := c.FetchHeaders(odd, &imap.FetchOptions{UID: true, Envelope: true, RFC822Size: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(odd) || got[0].Size == 0 {
		t.Fatalf("got %d headers (first size %d), want %d with RFC822.SIZE", len(got), got[0].Size, len(odd))
	}
	fetches := 0
	for line := range strings.SplitSeq(wire.String(), "\n") {
		_, cmd, _ := strings.Cut(line, " ")
		if strings.HasPrefix(line, "*") || !strings.HasPrefix(cmd, "UID FETCH ") {
			continue
		}
		fetches++
		if n := len(strings.TrimRight(line, "\r")) + 2; n > commandBytes {
			t.Fatalf("command line %d B over %d", n, commandBytes)
		}
	}
	if fetches < 2 {
		t.Fatalf("%d FETCH commands, want the set split", fetches)
	}
}
