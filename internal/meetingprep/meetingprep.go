// Package meetingprep holds the pure parts of preparing for a meeting: ranking
// the conversations and earlier meetings that matter for one, matching a mail
// subject to an event title, and finding the link that joins a call. Gathering
// the data from the local store is the desktop layer's job; nothing here
// touches the disk or the network.
package meetingprep

import (
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

// Weights of the conversation ranking. A conversation scores per attendee that
// takes part, a flat amount for a subject that matches the event title, and a
// little for being recent, so a thread with two attendees outranks one with a
// matching title, which outranks one with a single attendee, and recency
// breaks the ties and orders the rest.
const (
	scorePerAttendee = 30
	scoreTitleMatch  = 45
	// scoreRecent is the most a conversation gains for being recent: it
	// falls from this to nothing over recentDays.
	scoreRecent = 10
	recentDays  = 90
)

// Thread is one conversation, or a lone message, found in recent mail.
type Thread struct {
	// Key identifies the conversation to the caller.
	Key     string
	Subject string
	// Last is when its newest message arrived.
	Last time.Time
	// People are the bare lowercase addresses that take part: sender and
	// recipients of any of its messages.
	People []string
	// Messages is how many of its messages were found.
	Messages int
}

// RankedThread is a conversation with why it ranks where it does.
type RankedThread struct {
	Thread
	// Involved are the attendees taking part, in the order the attendees were given.
	Involved   []string
	TitleMatch bool
	Score      int
}

// RankThreads orders conversations by how much they matter for a meeting:
// those with several of its attendees and those whose subject matches its
// title first, then the recent ones. Conversations with neither an attendee
// nor a matching subject are dropped. At most limit are returned, 0 for all.
func RankThreads(threads []Thread, attendees []string, title string, now time.Time, limit int) []RankedThread {
	want := make(map[string]bool, len(attendees))
	for _, a := range attendees {
		want[normalize(a)] = true
	}
	var out []RankedThread
	for _, t := range threads {
		have := make(map[string]bool, len(t.People))
		for _, p := range t.People {
			have[normalize(p)] = true
		}
		var involved []string
		for _, a := range attendees {
			if key := normalize(a); key != "" && have[key] {
				involved = append(involved, a)
			}
		}
		match := SubjectMatchesTitle(t.Subject, title)
		if len(involved) == 0 && !match {
			continue
		}
		score := len(involved) * scorePerAttendee
		if match {
			score += scoreTitleMatch
		}
		score += recency(t.Last, now)
		out = append(out, RankedThread{Thread: t, Involved: involved, TitleMatch: match, Score: score})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Last.After(out[j].Last)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// recency is scoreRecent for mail from now, falling to 0 at recentDays old.
func recency(at, now time.Time) int {
	days := int(now.Sub(at).Hours() / 24)
	if days < 0 {
		days = 0
	}
	if days >= recentDays {
		return 0
	}
	return scoreRecent - days*scoreRecent/recentDays
}

func normalize(address string) string {
	return strings.ToLower(strings.TrimSpace(address))
}

// genericWords are words an event title or a subject uses without saying what
// it is about, which would otherwise match half the mailbox.
var genericWords = map[string]bool{
	"meeting": true, "call": true, "sync": true, "catch": true, "up": true, "catchup": true,
	"weekly": true, "daily": true, "monthly": true, "check": true, "in": true, "chat": true,
	"invite": true, "invitation": true, "re": true, "fw": true, "fwd": true, "aw": true,
	"the": true, "a": true, "an": true, "and": true, "of": true, "with": true, "for": true,
	"to": true, "on": true,
}

// words splits text into lowercase words, leaving out the generic ones.
func words(text string) []string {
	fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	out := fields[:0]
	for _, f := range fields {
		if !genericWords[f] && len([]rune(f)) >= 2 {
			out = append(out, f)
		}
	}
	return out
}

// SubjectMatchesTitle says whether a mail subject is about the event with the
// given title: every meaningful word of the title is in the subject, or for a
// title of three or more of them, at least 60 percent. Reply and forward
// prefixes and generic words such as "meeting" or "sync" do not count, so a
// title made only of them matches nothing.
func SubjectMatchesTitle(subject, title string) bool {
	need := words(title)
	if len(need) == 0 {
		return false
	}
	have := make(map[string]bool)
	for _, w := range words(subject) {
		have[w] = true
	}
	found := 0
	for _, w := range need {
		if have[w] {
			found++
		}
	}
	if found == len(need) {
		return true
	}
	return len(need) >= 3 && found*10 >= len(need)*6
}

// Meeting is an event or one occurrence of a recurring one.
type Meeting struct {
	EventID      int64
	RecurrenceID string
	UID          string
	Title        string
	Start        time.Time
	// People are the bare lowercase addresses of its attendees and organizer.
	People []string
}

// RankedMeeting is an earlier meeting with why it was picked.
type RankedMeeting struct {
	Meeting
	// SameSeries is true for an earlier occurrence of the same recurring event.
	SameSeries bool
	// Shared are the attendees of the meeting being prepared who were there too.
	Shared []string
}

// RankMeetings picks the earlier meetings worth showing before the one with
// the given uid that starts at before: earlier occurrences of the same
// recurring event, and other events that share at least half of its
// attendees (one at least). Same-series ones come first, then those with more
// shared attendees, then the most recent. At most limit are returned, 0 for
// all; the meeting being prepared is never one of them.
func RankMeetings(candidates []Meeting, uid string, attendees []string, before time.Time, limit int) []RankedMeeting {
	need := (len(attendees) + 1) / 2
	if need < 1 {
		need = 1
	}
	var out []RankedMeeting
	for _, m := range candidates {
		if !m.Start.Before(before) {
			continue
		}
		same := uid != "" && m.UID == uid
		have := make(map[string]bool, len(m.People))
		for _, p := range m.People {
			have[normalize(p)] = true
		}
		var shared []string
		for _, a := range attendees {
			if have[normalize(a)] {
				shared = append(shared, a)
			}
		}
		if !same && len(shared) < need {
			continue
		}
		out = append(out, RankedMeeting{Meeting: m, SameSeries: same, Shared: shared})
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.SameSeries != b.SameSeries {
			return a.SameSeries
		}
		if len(a.Shared) != len(b.Shared) {
			return len(a.Shared) > len(b.Shared)
		}
		return a.Start.After(b.Start)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// Link is how to join a call.
type Link struct {
	URL string
	// Provider names the service when the address is a known one, else "".
	Provider string
}

var linkPattern = regexp.MustCompile(`https?://[^\s<>"'\x60]+`)

// providers maps a host, or a host suffix when it starts with a dot, to the
// service it belongs to.
var providers = []struct{ host, name string }{
	{".zoom.us", "Zoom"},
	{"zoom.us", "Zoom"},
	{"teams.microsoft.com", "Microsoft Teams"},
	{"teams.live.com", "Microsoft Teams"},
	{"meet.google.com", "Google Meet"},
	{".webex.com", "Webex"},
	{"webex.com", "Webex"},
	{"whereby.com", "Whereby"},
	{"meet.jit.si", "Jitsi"},
	{".gotomeeting.com", "GoTo Meeting"},
	{"gotomeet.me", "GoTo Meeting"},
	{"bluejeans.com", "BlueJeans"},
}

// providerOf names the service a URL joins a call on, or "".
func providerOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	for _, p := range providers {
		if host == p.host || (strings.HasPrefix(p.host, ".") && strings.HasSuffix(host, p.host)) {
			return p.name
		}
	}
	return ""
}

// ConferenceLink finds the link that joins a call among an event's location,
// description and URL: the first address of a known video service, looking in
// the texts in the order given. Failing that, the event's own URL is used as
// it stands, which is how some calendars carry a room.
func ConferenceLink(eventURL string, texts ...string) (Link, bool) {
	eventURL = strings.TrimSpace(eventURL)
	for _, text := range append([]string{eventURL}, texts...) {
		for _, found := range linkPattern.FindAllString(text, -1) {
			found = strings.TrimRight(found, ".,;:)]}>")
			if name := providerOf(found); name != "" {
				return Link{URL: found, Provider: name}, true
			}
		}
	}
	if strings.HasPrefix(strings.ToLower(eventURL), "http://") || strings.HasPrefix(strings.ToLower(eventURL), "https://") {
		return Link{URL: eventURL}, true
	}
	return Link{}, false
}
