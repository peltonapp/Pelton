package meetingprep

import (
	"reflect"
	"testing"
	"time"
)

var now = time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)

func daysAgo(n int) time.Time { return now.AddDate(0, 0, -n) }

func keys(ranked []RankedThread) []string {
	var out []string
	for _, r := range ranked {
		out = append(out, r.Key)
	}
	return out
}

func TestSubjectMatchesTitle(t *testing.T) {
	tests := []struct {
		name, subject, title string
		want                 bool
	}{
		{"same words", "Roadmap review", "Roadmap review", true},
		{"reply prefix and case", "RE: roadmap Review", "Roadmap review", true},
		{"subject says more", "Re: Atlas kickoff notes and next steps", "Atlas kickoff", true},
		{"missing a word", "Atlas budget", "Atlas kickoff", false},
		{"most of a long title", "Atlas kickoff", "Project Atlas kickoff sync", true},
		{"half of a long title", "Atlas", "Project Atlas kickoff review", false},
		{"generic title matches nothing", "Weekly sync call", "Weekly sync", false},
		{"empty title", "Anything", "", false},
		{"punctuation", "Q4: planning!", "Q4 planning", true},
		{"another word entirely", "Lunch on friday", "Roadmap review", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SubjectMatchesTitle(tt.subject, tt.title); got != tt.want {
				t.Errorf("SubjectMatchesTitle(%q, %q) = %v, want %v", tt.subject, tt.title, got, tt.want)
			}
		})
	}
}

func TestRankThreadsOrdersByAttendeesThenTitleThenRecency(t *testing.T) {
	attendees := []string{"ada@x.example", "bob@x.example", "cy@x.example"}
	threads := []Thread{
		{Key: "single", Subject: "Lunch", Last: daysAgo(1), People: []string{"ada@x.example", "me@x.example"}},
		{Key: "two", Subject: "Budget", Last: daysAgo(40), People: []string{"ada@x.example", "bob@x.example"}},
		{Key: "title", Subject: "Re: Roadmap review", Last: daysAgo(60), People: []string{"zed@y.example"}},
		{Key: "unrelated", Subject: "Newsletter", Last: daysAgo(0), People: []string{"news@y.example"}},
		{Key: "all", Subject: "Offsite", Last: daysAgo(80), People: []string{"ADA@x.example", "bob@x.example", "cy@x.example"}},
		{Key: "older single", Subject: "Coffee", Last: daysAgo(30), People: []string{"bob@x.example"}},
	}
	got := keys(RankThreads(threads, attendees, "Roadmap review", now, 0))
	want := []string{"all", "two", "title", "single", "older single"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

func TestRankThreadsRecencyBreaksTies(t *testing.T) {
	attendees := []string{"ada@x.example"}
	threads := []Thread{
		{Key: "old", Subject: "A", Last: daysAgo(70), People: []string{"ada@x.example"}},
		{Key: "new", Subject: "B", Last: daysAgo(2), People: []string{"ada@x.example"}},
		{Key: "oldest", Subject: "C", Last: daysAgo(400), People: []string{"ada@x.example"}},
	}
	got := keys(RankThreads(threads, attendees, "", now, 0))
	if want := []string{"new", "old", "oldest"}; !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

func TestRankThreadsReportsWhyAndLimits(t *testing.T) {
	attendees := []string{"ada@x.example", "bob@x.example"}
	threads := []Thread{
		{Key: "a", Subject: "Roadmap review", Last: daysAgo(3), People: []string{"ada@x.example", "bob@x.example"}},
		{Key: "b", Subject: "Other", Last: daysAgo(3), People: []string{"ada@x.example"}},
		{Key: "c", Subject: "Another", Last: daysAgo(3), People: []string{"bob@x.example"}},
	}
	ranked := RankThreads(threads, attendees, "Roadmap review", now, 2)
	if len(ranked) != 2 {
		t.Fatalf("limit not applied: %d", len(ranked))
	}
	first := ranked[0]
	if !first.TitleMatch || !reflect.DeepEqual(first.Involved, attendees) {
		t.Errorf("first = %+v", first)
	}
	if ranked[1].TitleMatch || len(ranked[1].Involved) != 1 {
		t.Errorf("second = %+v", ranked[1])
	}
}

func TestRankThreadsWithoutAttendeesOrTitleFindsNothing(t *testing.T) {
	threads := []Thread{{Key: "a", Subject: "Hello", Last: now, People: []string{"ada@x.example"}}}
	if got := RankThreads(threads, nil, "Weekly sync", now, 0); len(got) != 0 {
		t.Errorf("got %v", keys(got))
	}
}

func meeting(id int64, uid string, start time.Time, people ...string) Meeting {
	return Meeting{EventID: id, UID: uid, Title: "m", Start: start, People: people}
}

func TestRankMeetings(t *testing.T) {
	attendees := []string{"ada@x.example", "bob@x.example", "cy@x.example", "di@x.example"}
	before := now
	candidates := []Meeting{
		meeting(1, "series", daysAgo(14), "ada@x.example"),
		meeting(2, "series", daysAgo(7)),
		meeting(3, "other-a", daysAgo(3), "ada@x.example", "bob@x.example"),
		meeting(4, "other-b", daysAgo(1), "ada@x.example"),
		meeting(5, "other-c", daysAgo(20), "ada@x.example", "bob@x.example", "cy@x.example"),
		meeting(6, "series", now.Add(time.Hour), "ada@x.example"),
		meeting(7, "other-d", daysAgo(2), "ADA@x.example", "Bob@x.example"),
	}
	ranked := RankMeetings(candidates, "series", attendees, before, 0)
	var ids []int64
	for _, r := range ranked {
		ids = append(ids, r.EventID)
	}
	// the same series first, then other meetings with at least two of the four
	// attendees, more shared first and then the most recent.
	if want := []int64{1, 2, 5, 7, 3}; !reflect.DeepEqual(ids, want) {
		t.Errorf("ids = %v, want %v", ids, want)
	}
	if !ranked[0].SameSeries || ranked[2].SameSeries {
		t.Errorf("series flags = %v %v", ranked[0].SameSeries, ranked[2].SameSeries)
	}
	if len(ranked[2].Shared) != 3 {
		t.Errorf("shared = %v", ranked[2].Shared)
	}
}

func TestRankMeetingsNeedsOnePersonForASmallMeeting(t *testing.T) {
	ranked := RankMeetings([]Meeting{meeting(1, "a", daysAgo(5), "ada@x.example"), meeting(2, "b", daysAgo(5))}, "", []string{"ada@x.example"}, now, 0)
	if len(ranked) != 1 || ranked[0].EventID != 1 {
		t.Errorf("ranked = %+v", ranked)
	}
}

func TestRankMeetingsLimit(t *testing.T) {
	var list []Meeting
	for i := 1; i <= 8; i++ {
		list = append(list, meeting(int64(i), "s", daysAgo(i)))
	}
	if got := RankMeetings(list, "s", nil, now, 3); len(got) != 3 || got[0].EventID != 1 {
		t.Errorf("got %+v", got)
	}
}

func TestConferenceLink(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		texts    []string
		wantURL  string
		wantProv string
		ok       bool
	}{
		{"zoom in the description", "", []string{"Room 4", "Join: https://acme.zoom.us/j/123456789?pwd=abc."}, "https://acme.zoom.us/j/123456789?pwd=abc", "Zoom", true},
		{"teams in the location", "", []string{"https://teams.microsoft.com/l/meetup-join/19%3ameeting"}, "https://teams.microsoft.com/l/meetup-join/19%3ameeting", "Microsoft Teams", true},
		{"meet in the event url", "https://meet.google.com/abc-defg-hij", nil, "https://meet.google.com/abc-defg-hij", "Google Meet", true},
		{"a known service wins over a plain event url", "https://example.com/room", []string{"https://meet.jit.si/Weekly"}, "https://meet.jit.si/Weekly", "Jitsi", true},
		{"a plain event url is the fallback", "https://example.com/room", []string{"Bring snacks"}, "https://example.com/room", "", true},
		{"a look-alike host is not a service", "", []string{"https://notzoom.us/j/1 and https://zoom.us.evil.example/j/1"}, "", "", false},
		{"nothing", "", []string{"Room 4"}, "", "", false},
		{"not a web address", "mailto:a@b.example", nil, "", "", false},
		{"parenthesised", "", []string{"(https://whereby.com/acme)"}, "https://whereby.com/acme", "Whereby", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ConferenceLink(tt.url, tt.texts...)
			if ok != tt.ok || got.URL != tt.wantURL || got.Provider != tt.wantProv {
				t.Errorf("ConferenceLink = %+v, %v; want %q (%s), %v", got, ok, tt.wantURL, tt.wantProv, tt.ok)
			}
		})
	}
}
