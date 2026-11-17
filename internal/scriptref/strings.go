package scriptref

import (
	"regexp"
	"strings"
)

// catalogLine is one single-line entry of a frontend locale file:
// `  'key': 'text',` with either quote style.
var catalogLine = regexp.MustCompile(`^\s*'([^']+)':\s*(?:'((?:[^'\\]|\\.)*)'|"((?:[^"\\]|\\.)*)")\s*,?\s*$`)

// EnglishStrings reads the single-line entries of the frontend's English
// locale file (frontend/src/lib/locales/en.ts) into a map. Entries written
// over several lines are skipped; the scripting entries are all single-line.
func EnglishStrings(src string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(src, "\n") {
		m := catalogLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		text := m[2]
		if text == "" {
			text = m[3]
		}
		out[m[1]] = unescape(text)
	}
	return out
}

func unescape(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
			switch s[i] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			default:
				b.WriteByte(s[i])
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
