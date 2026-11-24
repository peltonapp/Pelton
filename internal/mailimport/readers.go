package mailimport

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/storage"
)

// The on-disk shapes a mail folder can take.
const (
	FormatMbox    = "mbox"
	FormatMaildir = "maildir"
	FormatEmlx    = "emlx"
	FormatEml     = "eml"
	FormatEmlDir  = "emldir"
	FormatPST     = "pst"
)

// maxMessageBytes caps one message. Anything bigger is skipped and reported
// rather than held in memory.
const maxMessageBytes = 256 << 20

// Item is one message read from a source.
type Item struct {
	Raw []byte
	// Flags are the read, flagged and answered state the source recorded.
	Flags storage.Flag
	// Received is when the source says the message arrived, zero when it
	// does not say. It stands in for a missing Date header.
	Received time.Time
	// Next is the position to resume from after this item.
	Next int64
	// Skip is a reason code when the source marks the message as one not to
	// bring over (deleted there, too large, only partly stored).
	Skip string
	// Note is a reason code for something about an imported message the
	// report should mention.
	Note string
}

// Reason codes the report carries. The ui words them.
const (
	ReasonDuplicate       = "duplicate"
	ReasonDeletedInSource = "deletedInSource"
	ReasonTooLarge        = "tooLarge"
	ReasonUnreadable      = "unreadable"
	ReasonPartial         = "partial"
	ReasonNotMail         = "notMail"
	ReasonUploadFailed    = "uploadFailed"
)

// MessageReader yields a folder's messages one at a time.
type MessageReader interface {
	// Next returns the next item, or io.EOF when the folder is done.
	Next() (Item, error)
	Close() error
}

// openDiskFolder opens a folder stored in one of the file formats, starting
// at a position an earlier run recorded.
func openDiskFolder(f MailFolder, from int64) (MessageReader, error) {
	switch f.Format {
	case FormatMbox:
		return openMbox(f.Location, from)
	case FormatMaildir:
		return openMaildir(f.Location, from)
	case FormatEmlx:
		return openFileList(emlxFiles(f.Location), from, readEmlx)
	case FormatEml:
		return openFileList([]string{f.Location}, from, readEml)
	case FormatEmlDir:
		return openFileList(emlFiles(f.Location), from, readEml)
	}
	return nil, fmt.Errorf("mailimport: unknown folder format %q", f.Format)
}

type mboxItems struct {
	f      *os.File
	reader *MboxReader
}

func openMbox(path string, from int64) (MessageReader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("mailimport: open %s: %w", filepath.Base(path), err)
	}
	if from > 0 {
		if _, err := f.Seek(from, io.SeekStart); err != nil {
			f.Close()
			return nil, fmt.Errorf("mailimport: seek %s: %w", filepath.Base(path), err)
		}
	}
	return &mboxItems{f: f, reader: newMboxReaderAt(f, from, maxMessageBytes)}, nil
}

func (m *mboxItems) Next() (Item, error) {
	raw, err := m.reader.Next()
	switch {
	case errors.Is(err, ErrMessageTooLarge):
		return Item{Skip: ReasonTooLarge, Next: m.reader.Offset()}, nil
	case errors.Is(err, ErrNotMbox):
		return Item{}, io.EOF
	case err != nil:
		return Item{}, err
	}
	item := Item{Raw: raw, Next: m.reader.Offset()}
	item.Flags, item.Skip = mboxFlags(raw)
	return item, nil
}

func (m *mboxItems) Close() error { return m.f.Close() }

// Thunderbird records per-message state in an X-Mozilla-Status header, a hex
// bitmask it writes into the mbox itself. Expunged messages stay in the file
// until the folder is compacted.
const (
	mozillaStatusRead      = 0x0001
	mozillaStatusReplied   = 0x0002
	mozillaStatusFlagged   = 0x0004
	mozillaStatusExpunged  = 0x0008
	mozillaStatusForwarded = 0x1000
)

// mboxFlags reads the state an mbox writer left in the headers: Thunderbird's
// X-Mozilla-Status, or the Status and X-Status pair mutt, Apple Mail's export
// and most Unix tools write. With neither, the message counts as read: an
// archive is something the user already dealt with.
func mboxFlags(raw []byte) (storage.Flag, string) {
	head := headerBlock(raw)
	if v := headerValue(head, "X-Mozilla-Status"); v != "" {
		bits, err := strconv.ParseUint(strings.TrimSpace(v), 16, 32)
		if err == nil {
			var flags storage.Flag
			if bits&mozillaStatusRead != 0 {
				flags |= storage.FlagSeen
			}
			if bits&mozillaStatusFlagged != 0 {
				flags |= storage.FlagFlagged
			}
			if bits&mozillaStatusReplied != 0 {
				flags |= storage.FlagAnswered
			}
			if bits&mozillaStatusForwarded != 0 {
				flags |= storage.FlagForwarded
			}
			if bits&mozillaStatusExpunged != 0 {
				return flags, ReasonDeletedInSource
			}
			return flags, ""
		}
	}
	status, xstatus := headerValue(head, "Status"), headerValue(head, "X-Status")
	if status == "" && xstatus == "" {
		return storage.FlagSeen, ""
	}
	var flags storage.Flag
	if strings.ContainsRune(status, 'R') {
		flags |= storage.FlagSeen
	}
	if strings.ContainsRune(xstatus, 'F') {
		flags |= storage.FlagFlagged
	}
	if strings.ContainsRune(xstatus, 'A') {
		flags |= storage.FlagAnswered
	}
	if strings.ContainsRune(xstatus, 'D') {
		return flags, ReasonDeletedInSource
	}
	return flags, ""
}

// headerBlock is the raw header of a message, up to the first blank line.
func headerBlock(raw []byte) []byte {
	head, _ := splitHeader(raw)
	return head
}

// splitHeader splits a message at the first blank line, whether its lines end
// in CRLF or LF.
func splitHeader(raw []byte) (head, body []byte) {
	for i := 0; i < len(raw); i++ {
		if raw[i] != '\n' {
			continue
		}
		rest := raw[i+1:]
		if bytes.HasPrefix(rest, []byte("\n")) {
			return raw[:i], rest[1:]
		}
		if bytes.HasPrefix(rest, []byte("\r\n")) {
			return raw[:i], rest[2:]
		}
	}
	return raw, nil
}

// headerValue returns the first value of a header in a raw header block,
// unfolded, or "" when it is absent.
func headerValue(head []byte, name string) string {
	prefix := strings.ToLower(name) + ":"
	lines := bytes.Split(head, []byte("\n"))
	for i := 0; i < len(lines); i++ {
		line := strings.TrimRight(string(lines[i]), "\r")
		if !strings.HasPrefix(strings.ToLower(line), prefix) {
			continue
		}
		value := line[len(prefix):]
		for i+1 < len(lines) && len(lines[i+1]) > 0 && (lines[i+1][0] == ' ' || lines[i+1][0] == '\t') {
			i++
			value += " " + strings.TrimSpace(strings.TrimRight(string(lines[i]), "\r"))
		}
		return strings.TrimSpace(value)
	}
	return ""
}

// fileList reads a sorted list of one-message files; the position is the
// index of the next file.
type fileList struct {
	files []string
	next  int
	read  func(path string) (Item, error)
}

func openFileList(files []string, from int64, read func(string) (Item, error)) (MessageReader, error) {
	if from < 0 || from > int64(len(files)) {
		from = 0
	}
	return &fileList{files: files, next: int(from), read: read}, nil
}

func (l *fileList) Next() (Item, error) {
	if l.next >= len(l.files) {
		return Item{}, io.EOF
	}
	path := l.files[l.next]
	l.next++
	item, err := l.read(path)
	item.Next = int64(l.next)
	if err != nil {
		return Item{Skip: ReasonUnreadable, Next: item.Next}, nil
	}
	return item, nil
}

func (l *fileList) Close() error { return nil }

func readLimited(path string) ([]byte, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	if info, err := f.Stat(); err == nil && info.Size() > maxMessageBytes+4096 {
		return nil, true, nil
	}
	data, err := io.ReadAll(io.LimitReader(f, maxMessageBytes+4097))
	return data, len(data) > maxMessageBytes+4096, err
}

func readEml(path string) (Item, error) {
	data, tooLarge, err := readLimited(path)
	if err != nil {
		return Item{}, err
	}
	if tooLarge {
		return Item{Skip: ReasonTooLarge}, nil
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return Item{Skip: ReasonNotMail}, nil
	}
	item := Item{Raw: data}
	item.Flags, item.Skip = mboxFlags(data)
	return item, nil
}

// emlFiles lists the .eml files directly inside dir, sorted.
func emlFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".eml") {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	slices.Sort(out)
	return out
}

// maildirItems reads cur and new of one Maildir; the position is the index
// into the sorted list of both.
type maildirItems struct {
	*fileList
}

func openMaildir(dir string, from int64) (MessageReader, error) {
	files := maildirFiles(dir)
	l, _ := openFileList(files, from, readMaildirFile)
	return maildirItems{l.(*fileList)}, nil
}

func maildirFiles(dir string) []string {
	var out []string
	for _, sub := range []string{"cur", "new"} {
		entries, err := os.ReadDir(filepath.Join(dir, sub))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			out = append(out, filepath.Join(dir, sub, e.Name()))
		}
	}
	slices.Sort(out)
	return out
}

func readMaildirFile(path string) (Item, error) {
	data, tooLarge, err := readLimited(path)
	if err != nil {
		return Item{}, err
	}
	if tooLarge {
		return Item{Skip: ReasonTooLarge}, nil
	}
	item := Item{Raw: data}
	if info, err := os.Stat(path); err == nil {
		item.Received = info.ModTime()
	}
	inNew := filepath.Base(filepath.Dir(path)) == "new"
	item.Flags, item.Skip = maildirFlags(filepath.Base(path), inNew)
	return item, nil
}

// maildirFlags reads the info suffix of a Maildir file name ("...:2,FRS").
// Windows tools write "!" or ";" instead of ":", which a file name there
// cannot hold. A message still in new/ has not been seen by any client.
func maildirFlags(name string, inNew bool) (storage.Flag, string) {
	if inNew {
		return 0, ""
	}
	info := ""
	for _, sep := range []string{":2,", "!2,", ";2,"} {
		if i := strings.LastIndex(name, sep); i >= 0 {
			info = name[i+len(sep):]
			break
		}
	}
	var flags storage.Flag
	for _, c := range info {
		switch c {
		case 'S':
			flags |= storage.FlagSeen
		case 'F':
			flags |= storage.FlagFlagged
		case 'R':
			flags |= storage.FlagAnswered
		case 'P':
			flags |= storage.FlagForwarded
		}
	}
	if strings.ContainsRune(info, 'T') {
		return flags, ReasonDeletedInSource
	}
	return flags, ""
}

// isMaildir reports whether dir holds a Maildir's cur and new directories.
func isMaildir(dir string) bool {
	for _, sub := range []string{"cur", "new"} {
		info, err := os.Stat(filepath.Join(dir, sub))
		if err != nil || !info.IsDir() {
			return false
		}
	}
	return true
}

// sizeOf adds up the sizes of files, for the summary.
func sizeOf(files []string) int64 {
	var total int64
	for _, f := range files {
		if info, err := os.Stat(f); err == nil {
			total += info.Size()
		}
	}
	return total
}

// sniffFile says what a single file is by its first bytes: an mbox, one
// message, or neither.
func sniffFile(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	head := make([]byte, 4096)
	n, _ := io.ReadFull(f, head)
	head = head[:n]
	if bytes.HasPrefix(head, fromPrefix) {
		return FormatMbox
	}
	sc := bufio.NewScanner(bytes.NewReader(head))
	if sc.Scan() {
		line := sc.Text()
		if i := strings.IndexByte(line, ':'); i > 0 && !strings.ContainsAny(line[:i], " \t") {
			return FormatEml
		}
	}
	return ""
}
