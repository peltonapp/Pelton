package mailimport

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/peltonapp/Pelton/internal/storage"
)

// fixtureMessage is a small made-up message.
func fixtureMessage(id, subject, body string) string {
	return "Message-ID: <" + id + ">\r\n" +
		"From: Wren Example <wren@example.org>\r\n" +
		"To: Juno Example <juno@example.org>\r\n" +
		"Date: Tue, 07 Jan 2020 10:00:00 +0000\r\n" +
		"Subject: " + subject + "\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n\r\n" + body + "\r\n"
}

func mboxEntry(msg string) string {
	return "From wren@example.org Tue Jan  7 10:00:00 2020\r\n" + msg + "\r\n"
}

func drain(t *testing.T, r MessageReader) []Item {
	t.Helper()
	var out []Item
	for {
		item, err := r.Next()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatalf("next: %v", err)
		}
		out = append(out, item)
	}
}

func TestMboxResumesFromSavedOffset(t *testing.T) {
	dir := t.TempDir()
	content := mboxEntry(fixtureMessage("a@x", "one", "first")) +
		mboxEntry(fixtureMessage("b@x", "two", "second")) +
		mboxEntry(fixtureMessage("c@x", "three", "third"))
	path := writeFile(t, dir, "Inbox", content)

	r, err := openMbox(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	first, err := r.Next()
	if err != nil {
		t.Fatal(err)
	}
	r.Close()
	if !strings.Contains(string(first.Raw), "first") {
		t.Fatalf("first = %q", first.Raw)
	}

	r, err = openMbox(path, first.Next)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	rest := drain(t, r)
	if len(rest) != 2 || !strings.Contains(string(rest[0].Raw), "second") || !strings.Contains(string(rest[1].Raw), "third") {
		t.Fatalf("resumed read = %d items", len(rest))
	}
	if rest[1].Next != int64(len(content)) {
		t.Fatalf("last offset %d, want end of file %d", rest[1].Next, len(content))
	}
}

// a sentence in a body that starts with "From " after a blank line is not a
// separator: real separators carry a time and a year.
func TestMboxKeepsFromSentenceAfterBlankLine(t *testing.T) {
	body := "See below.\r\n\r\nFrom here on we walk to the harbour.\r\nBye"
	content := mboxEntry(fixtureMessage("a@x", "walk", body)) + mboxEntry(fixtureMessage("b@x", "two", "x"))
	msgs := readAll(t, content)
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want 2", len(msgs))
	}
	if !strings.Contains(msgs[0], "From here on we walk") {
		t.Fatalf("body was cut at the From sentence: %q", msgs[0])
	}
}

func TestMboxTruncatedLastMessage(t *testing.T) {
	content := mboxEntry(fixtureMessage("a@x", "one", "whole")) +
		"From wren@example.org Tue Jan  7 10:00:00 2020\r\nMessage-ID: <b@x>\r\nSubject: cut\r\nContent-Ty"
	msgs := readAll(t, content)
	if len(msgs) != 2 || !strings.Contains(msgs[1], "Subject: cut") {
		t.Fatalf("truncated tail read as %d messages: %q", len(msgs), msgs)
	}
}

func TestMboxSkipsMessageOverTheCap(t *testing.T) {
	big := strings.Repeat("x", 4096)
	content := mboxEntry(fixtureMessage("a@x", "big", big)) + mboxEntry(fixtureMessage("b@x", "small", "ok"))
	r := newMboxReaderAt(strings.NewReader(content), 0, 1024)
	if _, err := r.Next(); !errors.Is(err, ErrMessageTooLarge) {
		t.Fatalf("first next = %v, want ErrMessageTooLarge", err)
	}
	msg, err := r.Next()
	if err != nil || !strings.Contains(string(msg), "small") {
		t.Fatalf("message after the big one = %q %v", msg, err)
	}
}

func TestCountMbox(t *testing.T) {
	content := mboxEntry(fixtureMessage("a@x", "one", "line\r\n\r\nFrom the start of a sentence")) +
		mboxEntry(fixtureMessage("b@x", "two", strings.Repeat("y", 200000)))
	n, err := countMbox(strings.NewReader(content))
	if err != nil || n != 2 {
		t.Fatalf("count = %d %v, want 2", n, err)
	}
}

func TestMboxFlags(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   storage.Flag
		skip   string
	}{
		{"none", "", storage.FlagSeen, ""},
		{"thunderbird read and flagged", "X-Mozilla-Status: 0005\r\n", storage.FlagSeen | storage.FlagFlagged, ""},
		{"thunderbird replied", "X-Mozilla-Status: 0003\r\n", storage.FlagSeen | storage.FlagAnswered, ""},
		{"thunderbird forwarded unread", "X-Mozilla-Status: 1000\r\n", storage.FlagForwarded, ""},
		{"thunderbird expunged", "X-Mozilla-Status: 0009\r\n", storage.FlagSeen, ReasonDeletedInSource},
		{"status pair", "Status: RO\r\nX-Status: AF\r\n", storage.FlagSeen | storage.FlagFlagged | storage.FlagAnswered, ""},
		{"status unread", "Status: O\r\n", 0, ""},
		{"status deleted", "Status: RO\r\nX-Status: D\r\n", storage.FlagSeen, ReasonDeletedInSource},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			raw := []byte(tc.header + fixtureMessage("a@x", "s", "b"))
			got, skip := mboxFlags(raw)
			if got != tc.want || skip != tc.skip {
				t.Fatalf("flags %v %q, want %v %q", got, skip, tc.want, tc.skip)
			}
		})
	}
}

func TestMaildirFlags(t *testing.T) {
	tests := []struct {
		name  string
		inNew bool
		want  storage.Flag
		skip  string
	}{
		{"1577000000.M1P1.host:2,S", false, storage.FlagSeen, ""},
		{"1577000000.M1P1.host:2,FRS", false, storage.FlagSeen | storage.FlagFlagged | storage.FlagAnswered, ""},
		{"1577000000.M1P1.host!2,PS", false, storage.FlagSeen | storage.FlagForwarded, ""},
		{"1577000000.M1P1.host;2,", false, 0, ""},
		{"1577000000.M1P1.host:2,ST", false, storage.FlagSeen, ReasonDeletedInSource},
		{"1577000000.M1P1.host", true, 0, ""},
	}
	for _, tc := range tests {
		got, skip := maildirFlags(tc.name, tc.inNew)
		if got != tc.want || skip != tc.skip {
			t.Errorf("%s: %v %q, want %v %q", tc.name, got, skip, tc.want, tc.skip)
		}
	}
}

// emlx is a length line, the message, then an XML plist; the plist carries
// the flags and must not end up in the message.
func TestReadEmlx(t *testing.T) {
	dir := t.TempDir()
	msg := fixtureMessage("apple@x", "from apple", "body")
	trailer := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict><key>date-received</key><integer>1578391200</integer><key>flags</key><integer>8589934613</integer></dict></plist>`
	path := writeFile(t, dir, "12.emlx", "     "+itoa(len(msg))+"\n"+msg+trailer)
	item, err := readEmlx(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(item.Raw) != msg {
		t.Fatalf("raw = %q", item.Raw)
	}
	// 8589934613 = read (1) | answered (4) | flagged (16) plus high bits.
	if item.Flags != storage.FlagSeen|storage.FlagAnswered|storage.FlagFlagged {
		t.Fatalf("flags = %v", item.Flags)
	}
	if item.Received.Unix() != 1578391200 {
		t.Fatalf("received = %v", item.Received)
	}

	deleted := writeFile(t, dir, "13.partial.emlx", itoa(len(msg))+"\n"+msg+
		`<plist version="1.0"><dict><key>flags</key><integer>3</integer></dict></plist>`)
	item, err = readEmlx(deleted)
	if err != nil || item.Skip != ReasonDeletedInSource || item.Note != ReasonPartial {
		t.Fatalf("deleted partial = %+v %v", item, err)
	}

	short := writeFile(t, dir, "14.emlx", "9999\n"+msg)
	item, err = readEmlx(short)
	if err != nil || string(item.Raw) != msg {
		t.Fatalf("a length past the end of the file did not keep what is there: %+v %v", item, err)
	}
	if _, err := readEmlx(writeFile(t, dir, "15.emlx", "not a number\n"+msg)); err == nil {
		t.Fatal("a file without its length line was read")
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

func TestMaildirReaderResumes(t *testing.T) {
	root := t.TempDir()
	for _, sub := range []string{"cur", "new", "tmp"} {
		if err := os.MkdirAll(filepath.Join(root, sub), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(root, "cur"), "1.host:2,S", fixtureMessage("1@x", "one", "a"))
	writeFile(t, filepath.Join(root, "cur"), "2.host:2,F", fixtureMessage("2@x", "two", "b"))
	writeFile(t, filepath.Join(root, "new"), "3.host", fixtureMessage("3@x", "three", "c"))
	r, err := openMaildir(root, 0)
	if err != nil {
		t.Fatal(err)
	}
	all := drain(t, r)
	if len(all) != 3 {
		t.Fatalf("%d items", len(all))
	}
	r, _ = openMaildir(root, all[0].Next)
	rest := drain(t, r)
	if len(rest) != 2 || rest[1].Flags != 0 {
		t.Fatalf("resumed = %+v", rest)
	}
}

func TestMailboxNames(t *testing.T) {
	tests := map[string]string{
		"Entw&APw-rfe": "Entwürfe",
		"Tom &- Jerry": "Tom & Jerry",
		"&ZeVnLIqe-":   "日本語",
		"plain":        "plain",
		"broken&AB":    "broken&AB",
		"caf\xe9":      "café",
		"&AOk-t&AOk-":  "été",
		"\xff\xfeodd":  "ÿþodd",
	}
	for in, want := range tests {
		if got := mailboxName(in); got != want {
			t.Errorf("mailboxName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFingerprint(t *testing.T) {
	base := fixtureMessage("same@x", "hello", "line one\r\nline two")
	lf := strings.ReplaceAll(base, "\r\n", "\n")
	withStatus := "X-Mozilla-Status: 0001\r\n" + base + "\r\n\r\n"
	other := fixtureMessage("same@x", "hello", "different text")
	if Fingerprint([]byte(base)) != Fingerprint([]byte(lf)) {
		t.Error("line endings changed the fingerprint")
	}
	if Fingerprint([]byte(base)) != Fingerprint([]byte(withStatus)) {
		t.Error("a client's own status header or trailing blank lines changed the fingerprint")
	}
	if Fingerprint([]byte(base)) == Fingerprint([]byte(other)) {
		t.Error("two messages sharing a Message-ID got the same fingerprint")
	}
}
