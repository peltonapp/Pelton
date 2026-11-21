package imap

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// scriptedSelect starts a server that answers every SELECT with the given
// untagged lines and returns a client connected to it.
func scriptedSelect(t *testing.T, untagged ...string) *Client {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		fmt.Fprint(conn, "* OK ready\r\n")
		r := bufio.NewReader(conn)
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			tag, rest, _ := strings.Cut(strings.TrimSpace(line), " ")
			if !strings.HasPrefix(strings.ToUpper(rest), "SELECT") {
				fmt.Fprintf(conn, "%s OK done\r\n", tag)
				continue
			}
			for _, l := range untagged {
				fmt.Fprint(conn, l+"\r\n")
			}
			fmt.Fprintf(conn, "%s OK [READ-WRITE] selected\r\n", tag)
		}
	}()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	raw := imapclient.New(conn, nil)
	t.Cleanup(func() { _ = raw.Close() })
	return &Client{raw: raw}
}

// Gmail lists every keyword ever set on the account in each folder's FLAGS,
// so one keyword with a "]" in it, left by another client, used to fail the
// SELECT of every folder at once (#474).
func TestSelectAcceptsAKeywordWithABracket(t *testing.T) {
	c := scriptedSelect(t,
		`* FLAGS (\Answered \Flagged \Draft \Deleted \Seen $NotPhishing $Phishing [Imap]/Todo)`,
		`* OK [PERMANENTFLAGS (\Answered \Flagged \Draft \Deleted \Seen $NotPhishing $Phishing [Imap]/Todo \*)] Flags permitted.`,
		`* OK [UIDVALIDITY 1] UIDs valid.`,
		`* 3 EXISTS`,
		`* 0 RECENT`,
		`* OK [UIDNEXT 4] Predicted next UID.`,
	)

	mbox, err := c.Select("#all")
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if mbox.NumMessages != 3 {
		t.Errorf("NumMessages = %d, want 3", mbox.NumMessages)
	}
}

// A spam mark only writes $Junk/$NotJunk where the server keeps them, so what
// PERMANENTFLAGS allows has to survive the SELECT intact.
func TestSelectRecordsWhichKeywordsStick(t *testing.T) {
	tests := []struct {
		name      string
		permanent string
		want      map[imap.Flag]bool
	}{
		{
			name:      "any keyword allowed",
			permanent: `* OK [PERMANENTFLAGS (\Seen \Deleted \*)] Flags permitted.`,
			want:      map[imap.Flag]bool{"$Junk": true, "$NotJunk": true},
		},
		{
			name:      "only listed keywords, matched without case",
			permanent: `* OK [PERMANENTFLAGS (\Seen \Deleted $junk)] Flags permitted.`,
			want:      map[imap.Flag]bool{"$Junk": true, "$NotJunk": false},
		},
		{
			name:      "no keywords kept",
			permanent: `* OK [PERMANENTFLAGS (\Seen \Deleted)] Flags permitted.`,
			want:      map[imap.Flag]bool{"$Junk": false, "$NotJunk": false},
		},
		{
			name: "no PERMANENTFLAGS at all",
			want: map[imap.Flag]bool{"$Junk": false},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			untagged := []string{`* FLAGS (\Seen \Deleted)`, `* 1 EXISTS`, `* OK [UIDVALIDITY 1] UIDs valid.`}
			if tt.permanent != "" {
				untagged = append(untagged, tt.permanent)
			}
			mbox, err := scriptedSelect(t, untagged...).Select("Junk")
			if err != nil {
				t.Fatalf("select: %v", err)
			}
			for kw, want := range tt.want {
				if got := mbox.KeepsKeyword(kw); got != want {
					t.Errorf("KeepsKeyword(%s) = %v, want %v", kw, got, want)
				}
			}
		})
	}
}
