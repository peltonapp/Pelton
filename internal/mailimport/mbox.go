package mailimport

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
)

// MboxReader walks an mbox file, returning one raw message per call. mbox has
// no length prefix or framing: messages are separated by a line beginning
// "From " that sits at the start of the file or directly after a blank line.
// A body line that would look like a separator is escaped on write by
// prefixing it with ">", which Next undoes.
//
// It reads one message into memory at a time rather than the whole file, since
// an archive exported from another client is routinely gigabytes.
type MboxReader struct {
	br *bufio.Reader
	// pending holds the "From " line that ended the previous message, since
	// finding it is what tells us the previous one is complete.
	pending []byte
	// afterBlank tracks whether the last line written was empty, which is half
	// of what makes a "From " line a separator rather than body text.
	afterBlank bool
	started    bool
	// pos counts the bytes handed out by readLine, which is where the parser
	// stands in the file regardless of how far bufio has read ahead.
	pos int64
	// base is the file offset the reader started at.
	base int64
	// limit caps one message's size; 0 means no cap.
	limit int
}

// mboxBufSize is the read buffer. Messages routinely carry base64 lines and
// long headers; the buffer only affects read syscalls, not the maximum line.
const mboxBufSize = 64 * 1024

// NewMboxReader reads mbox messages from r.
func NewMboxReader(r io.Reader) *MboxReader {
	return &MboxReader{br: bufio.NewReaderSize(r, mboxBufSize)}
}

// newMboxReaderAt reads from r, which has been positioned at offset base of
// the file, and caps each message at limit bytes.
func newMboxReaderAt(r io.Reader, base int64, limit int) *MboxReader {
	m := NewMboxReader(r)
	m.base, m.limit = base, limit
	return m
}

// ErrMessageTooLarge is returned by Next for a message over the reader's
// size cap. The message is skipped and the next call carries on after it.
var ErrMessageTooLarge = errors.New("mailimport: message too large")

// Offset is the file offset where the next message starts, which is where a
// later reader can pick up after an interruption.
func (m *MboxReader) Offset() int64 {
	return m.base + m.pos - int64(len(m.pending))
}

// Next returns the next message's raw source, or io.EOF when the file is done.
// A file that is not mbox at all (no separator line anywhere) yields no
// messages and returns ErrNotMbox, so the caller can say so rather than
// silently importing nothing.
func (m *MboxReader) Next() ([]byte, error) {
	var msg bytes.Buffer
	tooLarge := false

	if m.pending != nil {
		m.pending = nil
	} else if !m.started {
		// the first separator has to be found before anything is a message.
		if err := m.seekFirstSeparator(); err != nil {
			return nil, err
		}
	}
	m.started = true
	m.afterBlank = false

	finish := func() ([]byte, error) {
		if tooLarge {
			return nil, ErrMessageTooLarge
		}
		return msg.Bytes(), nil
	}
	for {
		line, err := m.readLine()
		if err == io.EOF {
			if msg.Len() == 0 && !tooLarge {
				return nil, io.EOF
			}
			return finish()
		}
		if err != nil {
			return nil, err
		}

		if m.afterBlank && isSeparator(line) && looksLikeEnvelope(line) {
			m.pending = line
			return finish()
		}
		m.afterBlank = isBlank(line)
		if tooLarge {
			continue
		}
		msg.Write(unescapeFrom(line))
		if m.limit > 0 && msg.Len() > m.limit {
			tooLarge = true
			msg = bytes.Buffer{}
		}
	}
}

// ErrNotMbox is returned when a file handed to MboxReader has no mbox
// separator line at all, which means it is some other format.
var ErrNotMbox = errors.New("mailimport: not an mbox file")

// seekFirstSeparator consumes everything up to and including the first
// separator line. A well-formed mbox starts with one on line 1.
func (m *MboxReader) seekFirstSeparator() error {
	for {
		line, err := m.readLine()
		if err == io.EOF {
			return ErrNotMbox
		}
		if err != nil {
			return err
		}
		if isSeparator(line) {
			return nil
		}
		if !isBlank(line) {
			// content before any separator means this is not an mbox; stopping
			// here avoids scanning a whole large file to reach the same answer.
			return ErrNotMbox
		}
	}
}

// readLine reads one line including its terminator. bufio.Scanner is not used
// because its token limit would fail on messages with very long single lines.
func (m *MboxReader) readLine() ([]byte, error) {
	line, err := m.br.ReadBytes('\n')
	m.pos += int64(len(line))
	if err == io.EOF && len(line) > 0 {
		return line, nil
	}
	if err != nil {
		if err == io.EOF {
			return nil, io.EOF
		}
		return nil, fmt.Errorf("mailimport: read mbox: %w", err)
	}
	return line, nil
}

var fromPrefix = []byte("From ")

func isSeparator(line []byte) bool {
	return bytes.HasPrefix(line, fromPrefix)
}

// envelopeTime matches the clock and year every mbox writer puts on its
// separator line ("From sender Mon Jan  6 10:00:00 2020"), which a sentence
// in a body that happens to start with "From " after a blank line lacks.
var envelopeTime = regexp.MustCompile(`\d{1,2}:\d{2}(:\d{2})?.*\d{4}|\d{4}.*\d{1,2}:\d{2}`)

func looksLikeEnvelope(line []byte) bool {
	return envelopeTime.Match(line)
}

func isBlank(line []byte) bool {
	return len(bytes.TrimRight(line, "\r\n")) == 0
}

// unescapeFrom reverses the write-side quoting of body lines that would
// otherwise read as separators. mboxrd escapes any run of ">" before "From ",
// so exactly one ">" comes back off.
func unescapeFrom(line []byte) []byte {
	i := 0
	for i < len(line) && line[i] == '>' {
		i++
	}
	if i == 0 || !bytes.HasPrefix(line[i:], fromPrefix) {
		return line
	}
	return line[1:]
}

// countMbox counts the messages in an mbox by its separator lines, without
// keeping any of them.
func countMbox(r io.Reader) (int, error) {
	br := bufio.NewReaderSize(r, mboxBufSize)
	count := 0
	afterBlank := true
	for {
		line, err := br.ReadSlice('\n')
		if len(line) > 0 {
			if afterBlank && isSeparator(line) && (count == 0 || looksLikeEnvelope(line)) {
				count++
			}
			afterBlank = isBlank(line)
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			// the rest of an overlong line: it is neither blank nor a separator.
			afterBlank = false
			for errors.Is(err, bufio.ErrBufferFull) {
				_, err = br.ReadSlice('\n')
			}
		}
		if err == io.EOF {
			return count, nil
		}
		if err != nil && !errors.Is(err, bufio.ErrBufferFull) {
			return count, fmt.Errorf("mailimport: count mbox: %w", err)
		}
	}
}
