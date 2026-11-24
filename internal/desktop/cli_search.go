package desktop

import (
	"regexp"
	"strings"
	"time"
)

// The search syntax of the app's search field (frontend/src/lib/searchquery.ts),
// read here for the pelton command: from:, to:, subject:, in:, has:, is:,
// before:, after: and note: tokens, a quoted value kept in one piece, and
// whatever does not parse as free text.

var cliSearchToken = regexp.MustCompile(`[^\s"]*"[^"]*"?|\S+`)

var cliSearchFields = map[string]string{
	"from": "from", "sender": "from", "to": "to", "subject": "subject", "in": "in", "folder": "in",
	"has": "has", "is": "is", "before": "before", "after": "after", "note": "note",
}

// parseCLISearch turns a query into a search request. folder resolves the
// value of in:; an in: it cannot resolve stays in the free text.
func parseCLISearch(query string, now time.Time, folder func(string) (int64, bool)) SearchRequestDTO {
	var req SearchRequestDTO
	var rest []string
	for _, token := range cliSearchToken.FindAllString(query, -1) {
		if !applySearchToken(&req, token, now, folder) {
			rest = append(rest, token)
		}
	}
	req.Query = strings.Join(rest, " ")
	return req
}

func applySearchToken(req *SearchRequestDTO, token string, now time.Time, folder func(string) (int64, bool)) bool {
	at := strings.IndexByte(token, ':')
	if at <= 0 {
		return false
	}
	field, ok := cliSearchFields[strings.ToLower(token[:at])]
	value := unquoteCLI(strings.TrimSpace(token[at+1:]))
	if !ok || value == "" {
		return false
	}
	lower := strings.ToLower(value)
	switch field {
	case "from":
		req.From = value
	case "to":
		req.To = value
	case "subject":
		req.Subject = value
	case "note":
		req.Note = value
	case "has":
		switch {
		case strings.HasPrefix(lower, "note"):
			req.HasNote = true
		case strings.HasPrefix(lower, "attach"):
			req.HasAttachment = true
		default:
			return false
		}
	case "is":
		switch {
		case strings.HasPrefix(lower, "unread"):
			req.UnreadOnly = true
		case strings.HasPrefix(lower, "flag"), strings.HasPrefix(lower, "star"):
			req.FlaggedOnly = true
		default:
			return false
		}
	case "before", "after":
		day, ok := cliSearchDate(lower, now)
		if !ok {
			return false
		}
		if field == "after" {
			req.AfterUnix = day.Unix()
		} else {
			req.BeforeUnix = day.Add(24*time.Hour).Unix() - 1
		}
	case "in":
		id, ok := folder(value)
		if !ok {
			return false
		}
		req.FolderID = id
	}
	return true
}

// cliSearchDate reads a YYYY-MM-DD date, today or yesterday, as midnight UTC
// the way the search field reads a date.
func cliSearchDate(value string, now time.Time) (time.Time, bool) {
	switch value {
	case "today", "yesterday":
		y, m, d := now.Date()
		day := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
		if value == "yesterday" {
			day = day.AddDate(0, 0, -1)
		}
		return day, true
	}
	day, err := time.Parse("2006-01-02", value)
	return day, err == nil
}

func unquoteCLI(value string) string {
	if strings.HasPrefix(value, `"`) {
		value = value[1:]
		value = strings.TrimSuffix(value, `"`)
	}
	return value
}
