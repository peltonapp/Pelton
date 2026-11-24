package cli

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// clean makes mail-derived text safe for a terminal. Control characters,
// DEL, the C1 controls (one of them starts a terminal sequence on its own)
// and the bidirectional overrides that can hide or reorder text are written
// as visible escapes, so an escape sequence arrives as harmless text. Bytes
// that are not UTF-8 become U+FFFD. With multiline, newlines and tabs stay;
// without, they become spaces.
func clean(s string, multiline bool) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		switch {
		case r == utf8.RuneError && size == 1:
			b.WriteRune(utf8.RuneError)
		case r == '\n' || r == '\t':
			if multiline {
				b.WriteRune(r)
			} else {
				b.WriteByte(' ')
			}
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\x%02x`, r)
		case r >= 0x80 && r <= 0x9f, isBidiControl(r):
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isBidiControl(r rune) bool {
	return (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) || r == 0x061c || r == 0x200e || r == 0x200f
}

// line cleans one field for a one-line display and cuts it to width runes.
func line(s string, width int) string {
	s = clean(s, false)
	if width > 0 && utf8.RuneCountInString(s) > width {
		r := []rune(s)
		return string(r[:width-1]) + "~"
	}
	return s
}

// table writes rows with columns padded to the widest cell. Cells are
// expected clean.
func table(w io.Writer, rows [][]string) {
	if len(rows) == 0 {
		return
	}
	widths := make([]int, len(rows[0]))
	for _, row := range rows {
		for i, cell := range row {
			widths[i] = max(widths[i], utf8.RuneCountInString(cell))
		}
	}
	for _, row := range rows {
		var b strings.Builder
		for i, cell := range row {
			if i == len(row)-1 {
				b.WriteString(cell)
				break
			}
			b.WriteString(cell)
			b.WriteString(strings.Repeat(" ", widths[i]-utf8.RuneCountInString(cell)+2))
		}
		fmt.Fprintln(w, strings.TrimRight(b.String(), " "))
	}
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}
