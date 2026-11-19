package notes

import (
	"regexp"
	"strings"
)

const snippetRunes = 160

var (
	snippetWiki   = regexp.MustCompile(`!?\[\[([^\]|#]*)(?:#[^\]|]*)?(?:\|([^\]]*))?\]\]`)
	snippetLink   = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)
	snippetPrefix = regexp.MustCompile(`^\s*(?:#{1,6}\s+|>\s*(?:\[![^\]]*\][+-]?\s*)?|[-*+]\s+(?:\[[ xX]\]\s+)?|\d+[.)]\s+(?:\[[ xX]\]\s+)?)`)
	snippetMarks  = strings.NewReplacer("**", "", "__", "", "==", "", "~~", "", "`", "")
)

// Snippet is the opening words of a note's body as plain text, for the note
// list: Markdown marks are dropped, links keep their visible text, code
// blocks are skipped.
func Snippet(body string) string {
	var parts []string
	n := 0
	fence := false
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if fenceRe.MatchString(line) {
			fence = !fence
			continue
		}
		if fence || line == "" || strings.Trim(line, "-*_ ") == "" || strings.HasPrefix(line, "|") {
			continue
		}
		line = snippetPrefix.ReplaceAllString(line, "")
		line = snippetWiki.ReplaceAllStringFunc(line, func(m string) string {
			sub := snippetWiki.FindStringSubmatch(m)
			if sub[2] != "" {
				return sub[2]
			}
			return sub[1]
		})
		line = snippetLink.ReplaceAllString(line, "$1")
		line = strings.TrimSpace(snippetMarks.Replace(line))
		if line == "" {
			continue
		}
		parts = append(parts, line)
		n += len([]rune(line))
		if n >= snippetRunes {
			break
		}
	}
	out := []rune(strings.Join(parts, " "))
	if len(out) > snippetRunes {
		return strings.TrimSpace(string(out[:snippetRunes])) + "…"
	}
	return string(out)
}
