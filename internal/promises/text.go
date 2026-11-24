package promises

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// The patterns match on a folded copy of the text: letters outside ASCII
// become ASCII ("ü" is "ue", "é" is "e") and typographic quotes and
// apostrophes become plain ones. RE2's \b only knows ASCII word characters,
// so without this "übermorgen" would never start a word.

var foldMap = map[rune]string{
	'ä': "ae", 'ö': "oe", 'ü': "ue", 'Ä': "Ae", 'Ö': "Oe", 'Ü': "Ue", 'ß': "ss",
	'à': "a", 'á': "a", 'â': "a", 'ã': "a", 'å': "a", 'æ': "ae", 'ç': "c",
	'è': "e", 'é': "e", 'ê': "e", 'ë': "e", 'ì': "i", 'í': "i", 'î': "i", 'ï': "i",
	'ñ': "n", 'ò': "o", 'ó': "o", 'ô': "o", 'õ': "o", 'ø': "o", 'ù': "u", 'ú': "u",
	'û': "u", 'ý': "y", 'ÿ': "y", 'À': "A", 'Á': "A", 'Â': "A", 'Ç': "C", 'È': "E",
	'É': "E", 'Ê': "E", 'Î': "I", 'Ñ': "N", 'Ó': "O", 'Ô': "O", 'Ú': "U",
	'‘': "'", '’': "'", '‚': "'", '′': "'",
	'“': `"`, '”': `"`, '„': `"`, '«': `"`, '»': `"`,
	'–': "-", '—': "-", ' ': " ", ' ': " ",
}

// foldString folds a string for matching.
func foldString(s string) string {
	out, _ := fold(s)
	return out
}

// fold folds s and returns, for every byte of the result, the offset in s it
// came from, with one more entry for the end.
func fold(s string) (string, []int) {
	var b strings.Builder
	b.Grow(len(s))
	idx := make([]int, 0, len(s)+1)
	for i, r := range s {
		rep, ok := foldMap[r]
		if !ok {
			if r < utf8.RuneSelf {
				b.WriteByte(byte(r))
				idx = append(idx, i)
				continue
			}
			rep = string(r)
		}
		b.WriteString(rep)
		for range len(rep) {
			idx = append(idx, i)
		}
	}
	idx = append(idx, len(s))
	return b.String(), idx
}

// line is one line of a text by byte offsets, without its line break.
type line struct {
	start, end int
}

func splitLines(text string) []line {
	var out []line
	start := 0
	for i := 0; i <= len(text); i++ {
		if i == len(text) || text[i] == '\n' {
			end := i
			if end > start && text[end-1] == '\r' {
				end--
			}
			out = append(out, line{start, end})
			start = i + 1
		}
	}
	return out
}

// authorText returns what the author of a message newly wrote: the text up
// to a reply or forward header, an "Original Message" line, a signature
// separator or a sign-off near the end, with quoted lines ("> ...") blanked.
// The result has the byte offsets of text, so a span found in it is a span
// of the message.
func authorText(text string, l *language) string {
	lines := splitLines(text)
	cut := len(text)
	keep := []byte(text)
lines:
	for i, ln := range lines {
		raw := text[ln.start:ln.end]
		trimmed := strings.TrimSpace(raw)
		if strings.HasPrefix(trimmed, ">") {
			for j := ln.start; j < ln.end; j++ {
				keep[j] = ' '
			}
			continue
		}
		folded := foldString(trimmed)
		two := folded
		if i+1 < len(lines) {
			two = folded + "\n" + foldString(strings.TrimSpace(text[lines[i+1].start:lines[i+1].end]))
		}
		for _, re := range l.cut {
			if loc := re.FindStringIndex(two); loc != nil && loc[0] == 0 {
				cut = ln.start
				break lines
			}
		}
		if l.signoff != nil && l.signoff.MatchString(folded) && trailingLines(text, lines[:i]) > 0 && trailingLines(text, lines[i+1:]) <= 6 {
			cut = ln.start
			break
		}
	}
	return string(keep[:cut])
}

// trailingLines counts the lines in rest that hold anything.
func trailingLines(text string, rest []line) int {
	n := 0
	for _, ln := range rest {
		if strings.TrimSpace(text[ln.start:ln.end]) != "" {
			n++
		}
	}
	return n
}

// sentence is a span of the author text.
type sentence struct {
	start, end int
}

// sentences cuts text into sentences: at a full stop, question or
// exclamation mark followed by space, at a blank line, before a bullet, and
// after a short line such as a greeting. A full stop after a number ("15.03.")
// or a known abbreviation does not end one.
func sentences(text string, l *language) []sentence {
	var out []sentence
	start := -1
	flush := func(end int) {
		if start < 0 {
			return
		}
		s := strings.TrimRightFunc(text[start:end], unicode.IsSpace)
		if strings.TrimSpace(s) != "" {
			out = append(out, sentence{start, start + len(s)})
		}
		start = -1
	}
	lines := splitLines(text)
	for li, ln := range lines {
		raw := text[ln.start:ln.end]
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			flush(ln.start)
			continue
		}
		if isBullet(trimmed) {
			flush(ln.start)
		}
		for i := ln.start; i < ln.end; {
			r, size := utf8.DecodeRuneInString(text[i:])
			if start < 0 && !unicode.IsSpace(r) {
				start = i
				if isBullet(text[i:ln.end]) {
					start = i + bulletLen(text[i:ln.end])
				}
			}
			i += size
			if r != '.' && r != '!' && r != '?' {
				continue
			}
			j := i
			for j < ln.end && strings.ContainsRune(".!?\"')]", rune(text[j])) {
				j++
			}
			if j < ln.end && !unicode.IsSpace(rune(text[j])) {
				continue
			}
			if r == '.' && !endsSentence(text[ln.start:i-1], l) {
				continue
			}
			flush(j)
			i = j
		}
		if li < len(lines)-1 && start >= ln.start && shortLine(trimmed, strings.TrimSpace(text[lines[li+1].start:lines[li+1].end])) {
			flush(ln.end)
		}
	}
	flush(len(text))
	return out
}

// endsSentence reports whether a full stop after before ends a sentence: not
// after a number, a single letter or an abbreviation.
func endsSentence(before string, l *language) bool {
	k := len(before)
	for k > 0 {
		r, size := utf8.DecodeLastRuneInString(before[:k])
		if unicode.IsSpace(r) || r == '(' {
			break
		}
		k -= size
	}
	word := strings.ToLower(foldString(before[k:]))
	if word == "" {
		return true
	}
	if strings.Trim(word, "0123456789.") == "" {
		return false
	}
	if len(word) == 1 {
		return false
	}
	return !l.abbrev[strings.TrimSuffix(word, ".")]
}

// shortLine reports whether a line stands on its own: a greeting such as
// "Hi Anna,", a line ending in a colon, or a line of at most three words
// without an end mark before a line that starts with a capital.
func shortLine(s, next string) bool {
	if strings.HasSuffix(s, ":") {
		return true
	}
	words := len(strings.Fields(s))
	if strings.HasSuffix(s, ",") {
		return words <= 4
	}
	last, _ := utf8.DecodeLastRuneInString(s)
	first, _ := utf8.DecodeRuneInString(next)
	return words <= 3 && !strings.ContainsRune(".!?", last) && unicode.IsUpper(first)
}

func isBullet(s string) bool {
	return bulletLen(s) > 0
}

func bulletLen(s string) int {
	for _, p := range []string{"- ", "* ", "• ", "– "} {
		if strings.HasPrefix(s, p) {
			return len(p)
		}
	}
	i := 0
	for i < len(s) && i < 3 && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i > 0 && i+1 < len(s) && (s[i] == '.' || s[i] == ')') && s[i+1] == ' ' {
		return i + 2
	}
	return 0
}
