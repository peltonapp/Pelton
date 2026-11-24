package ical

import (
	"testing"
	"time"
)

func wall(s string) time.Time {
	t, err := time.ParseInLocation(layoutDateTime, s, time.UTC)
	if err != nil {
		panic(err)
	}
	return t
}

func TestRuleInstances(t *testing.T) {
	tests := []struct {
		name  string
		rule  string
		start string
		want  []string
	}{
		{
			name:  "weekly on three days",
			rule:  "FREQ=WEEKLY;BYDAY=MO,WE,FR;COUNT=5",
			start: "20260105T090000",
			want:  []string{"20260105T090000", "20260107T090000", "20260109T090000", "20260112T090000", "20260114T090000"},
		},
		{
			name:  "last friday of the month",
			rule:  "FREQ=MONTHLY;BYDAY=-1FR;COUNT=3",
			start: "20260130T100000",
			want:  []string{"20260130T100000", "20260227T100000", "20260327T100000"},
		},
		{
			name:  "the 31st skips short months",
			rule:  "FREQ=MONTHLY;COUNT=4",
			start: "20260131T100000",
			want:  []string{"20260131T100000", "20260331T100000", "20260531T100000", "20260731T100000"},
		},
		{
			name:  "leap day only in leap years",
			rule:  "FREQ=YEARLY;COUNT=2",
			start: "20240229T000000",
			want:  []string{"20240229T000000", "20280229T000000"},
		},
		{
			name:  "last weekday of the month",
			rule:  "FREQ=MONTHLY;BYDAY=MO,TU,WE,TH,FR;BYSETPOS=-1;COUNT=3",
			start: "20260130T170000",
			want:  []string{"20260130T170000", "20260227T170000", "20260331T170000"},
		},
		{
			name:  "fourth thursday of november",
			rule:  "FREQ=YEARLY;BYMONTH=11;BYDAY=4TH;COUNT=3",
			start: "20251127T120000",
			want:  []string{"20251127T120000", "20261126T120000", "20271125T120000"},
		},
		{
			name:  "rfc 5545 biweekly with sunday week start",
			rule:  "FREQ=WEEKLY;INTERVAL=2;COUNT=4;BYDAY=TU,SU;WKST=SU",
			start: "19970805T090000",
			want:  []string{"19970805T090000", "19970817T090000", "19970819T090000", "19970831T090000"},
		},
		{
			name:  "rfc 5545 biweekly with monday week start",
			rule:  "FREQ=WEEKLY;INTERVAL=2;COUNT=4;BYDAY=TU,SU;WKST=MO",
			start: "19970805T090000",
			want:  []string{"19970805T090000", "19970810T090000", "19970819T090000", "19970824T090000"},
		},
		{
			name:  "rfc 5545 monday of week 20",
			rule:  "FREQ=YEARLY;BYWEEKNO=20;BYDAY=MO;COUNT=3",
			start: "19970512T090000",
			want:  []string{"19970512T090000", "19980511T090000", "19990517T090000"},
		},
		{
			name:  "every other day",
			rule:  "FREQ=DAILY;INTERVAL=2;COUNT=3",
			start: "20260227T080000",
			want:  []string{"20260227T080000", "20260301T080000", "20260303T080000"},
		},
		{
			name:  "dtstart off the rule still counts first",
			rule:  "FREQ=WEEKLY;BYDAY=FR;COUNT=3",
			start: "20260107T090000",
			want:  []string{"20260107T090000", "20260109T090000", "20260116T090000"},
		},
		{
			name:  "byhour expands within the day",
			rule:  "FREQ=DAILY;BYHOUR=9,17;COUNT=4",
			start: "20260101T090000",
			want:  []string{"20260101T090000", "20260101T170000", "20260102T090000", "20260102T170000"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule, err := ParseRRule(tt.rule)
			if err != nil {
				t.Fatal(err)
			}
			got := rule.instances(wall(tt.start), time.Time{}, wall("21000101T000000"))
			if len(got) != len(tt.want) {
				t.Fatalf("got %d instances %v, want %v", len(got), got, tt.want)
			}
			for i := range got {
				if !got[i].Equal(wall(tt.want[i])) {
					t.Errorf("instance %d = %s, want %s", i, got[i].Format(layoutDateTime), tt.want[i])
				}
			}
		})
	}
}

func TestRuleUntilIsInclusive(t *testing.T) {
	rule, _ := ParseRRule("FREQ=DAILY")
	got := rule.instances(wall("20260101T080000"), wall("20260105T080000"), wall("21000101T000000"))
	if len(got) != 5 {
		t.Errorf("got %d instances, want 5: %v", len(got), got)
	}
}

func TestRuleNeverMatchingTerminates(t *testing.T) {
	rule, _ := ParseRRule("FREQ=YEARLY;BYMONTH=2;BYMONTHDAY=30")
	got := rule.instances(wall("20260101T080000"), time.Time{}, wall("99990101T000000"))
	if len(got) != 1 {
		t.Errorf("got %v, want only dtstart", got)
	}
}

func TestRRuleRoundTrip(t *testing.T) {
	for _, in := range []string{
		"FREQ=WEEKLY;COUNT=10;INTERVAL=2;BYDAY=MO,-1FR;WKST=SU",
		"FREQ=MONTHLY;UNTIL=20261231T235959Z;BYMONTHDAY=1,-1",
		"FREQ=YEARLY;UNTIL=20301231;BYMONTH=3;BYSETPOS=2;X-NAME=keep",
	} {
		rule, err := ParseRRule(in)
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if got := rule.String(); got != in {
			t.Errorf("round trip %q = %q", in, got)
		}
	}
	for _, bad := range []string{"", "COUNT=3", "FREQ=FORTNIGHTLY", "FREQ=DAILY;INTERVAL=0", "FREQ=WEEKLY;BYDAY=XX"} {
		if _, err := ParseRRule(bad); err == nil {
			t.Errorf("ParseRRule(%q) accepted", bad)
		}
	}
}
