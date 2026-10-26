package imap

import "testing"

func TestCursorRoundTrip(t *testing.T) {
	c := imapCursor{UIDValidity: 9, HighestModSeq: 1234, UIDNext: 77, Messages: 70}
	got, ok := parseCursor(c.String())
	if !ok || got != c {
		t.Fatalf("parse(%q) = %+v, %v", c.String(), got, ok)
	}
	for _, bad := range []string{"", "v1:", "v1:1:2:3", "v2:1:2:3:4", "v1:a:2:3:4", "st-other-token"} {
		if _, ok := parseCursor(bad); ok {
			t.Fatalf("parse(%q) accepted", bad)
		}
	}
	if cursorFor(&Mailbox{UIDValidity: 9}) != "" {
		t.Fatal("a mailbox without HIGHESTMODSEQ has no cursor")
	}
}
