package throttle

import (
	"regexp"
	"strconv"
	"time"
)

// maxSuggestedBackoff bounds a server's suggested wait, so a garbled number
// cannot stop the account from ever growing again.
const maxSuggestedBackoff = 24 * time.Hour

// suggestedBackoff is Exchange Online's "Suggested Backoff Time: 40464
// milliseconds" (a tagged BAD or NO), in any case and spacing; the unit may
// be missing, and then it is milliseconds.
var suggestedBackoff = regexp.MustCompile(`(?i)suggested\s*back-?off\s*time\s*[:=]?\s*(\d+)`)

// namedLimit is Dovecot's "(mail_max_userip_connections=10)".
var namedLimit = regexp.MustCompile(`(?i)mail_max_userip_connections\s*[=:]\s*(\d+)`)

// SuggestedBackoff is the wait a server asked for in msg, Exchange Online's
// "Suggested Backoff Time: N milliseconds", or 0 when msg names none. It is
// at most 24 hours.
func SuggestedBackoff(msg string) time.Duration {
	m := suggestedBackoff.FindStringSubmatch(msg)
	if m == nil {
		return 0
	}
	ms, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil || ms > maxSuggestedBackoff.Milliseconds() {
		return maxSuggestedBackoff
	}
	return time.Duration(ms) * time.Millisecond
}

// NamedLimit is the connection limit a server named in msg, Dovecot's
// "mail_max_userip_connections=N", or 0 when msg names none (or names 0).
func NamedLimit(msg string) int {
	m := namedLimit.FindStringSubmatch(msg)
	if m == nil {
		return 0
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n < 0 {
		return 0
	}
	return n
}
