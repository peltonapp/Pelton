package feeds

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// dateLayouts are the shapes feed dates come in, after normalizeDate has taken
// off the weekday and turned zone names into offsets. RFC 822 and RFC 3339 are
// what the specs ask for; the rest are what feeds send anyway.
var dateLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2 Jan 2006 15:04:05 -0700",
	"2 Jan 2006 15:04 -0700",
	"2 January 2006 15:04:05 -0700",
	"2 January 2006 15:04 -0700",
	"2 Jan 06 15:04:05 -0700",
	"2 Jan 06 15:04 -0700",
	"2 Jan 2006 15:04:05",
	"2 Jan 2006 15:04",
	"2 January 2006 15:04:05",
	"2 Jan 2006",
	"2 January 2006",
	"02-Jan-06 15:04:05 -0700",
	"02-Jan-2006 15:04:05 -0700",
	"Jan 2 15:04:05 2006",
	"Jan 2 15:04:05 -0700 2006",
	"Jan 2, 2006 15:04:05 -0700",
	"Jan 2, 2006 3:04 PM -0700",
	"Jan 2, 2006 15:04",
	"Jan 2, 2006",
	"January 2, 2006 15:04:05 -0700",
	"January 2, 2006 3:04 PM",
	"January 2, 2006",
	"2006-01-02T15:04:05-0700",
	"2006-01-02T15:04:05.999999999-0700",
	"2006-01-02T15:04:05",
	"2006-01-02T15:04:05.999999999",
	"2006-01-02T15:04Z07:00",
	"2006-01-02T15:04",
	"2006-01-02 15:04:05 -0700",
	"2006-01-02 15:04:05-07:00",
	"2006-01-02 15:04:05Z07:00",
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
	"2006-01-02",
	"2006/01/02 15:04:05",
	"2006/01/02",
	"02.01.2006 15:04:05",
	"02.01.2006",
	"2006-01-02T15:04:05 -0700",
	"2006-01-02T15:04 -0700",
}

// zoneOffsets are the zone names feeds write instead of an offset. Go reads a
// name it does not know as UTC under that name, which would put every entry of
// an American feed several hours off.
var zoneOffsets = map[string]string{
	"UT": "+0000", "UTC": "+0000", "GMT": "+0000", "Z": "+0000", "WET": "+0000",
	"EST": "-0500", "EDT": "-0400", "CST": "-0600", "CDT": "-0500",
	"MST": "-0700", "MDT": "-0600", "PST": "-0800", "PDT": "-0700",
	"AKST": "-0900", "AKDT": "-0800", "HST": "-1000",
	"BST": "+0100", "IST": "+0530", "CET": "+0100", "CEST": "+0200",
	"MET": "+0100", "MEST": "+0200", "EET": "+0200", "EEST": "+0300",
	"WEST": "+0100", "MSK": "+0300", "JST": "+0900", "KST": "+0900",
	"AEST": "+1000", "AEDT": "+1100", "ACST": "+0930", "AWST": "+0800",
	"NZST": "+1200", "NZDT": "+1300",
}

var (
	leadingWeekday = regexp.MustCompile(`^[A-Za-z]{3,9}\.?,?\s+`)
	trailingZone   = regexp.MustCompile(`\s*\(?([A-Za-z]{1,5})\)?$`)
	gmtOffset      = regexp.MustCompile(`(?i)\s*(?:GMT|UTC)\s*([+-])(\d{1,2}):?(\d{2})?$`)
	spaces         = regexp.MustCompile(`\s+`)
	ordinalDay     = regexp.MustCompile(`\b(\d{1,2})(st|nd|rd|th)\b`)
)

// normalizeDate brings a date into a form the layouts above cover: the weekday
// goes (it is often wrong, and sometimes in another language), "GMT+1" and
// "(PST)" become numeric offsets and runs of space become one.
func normalizeDate(s string) string {
	s = spaces.ReplaceAllString(strings.TrimSpace(s), " ")
	s = leadingWeekday.ReplaceAllStringFunc(s, func(m string) string {
		word := strings.TrimRight(strings.TrimSpace(m), ".,")
		if _, err := strconv.Atoi(word); err == nil {
			return m
		}
		if isMonthName(word) {
			return m
		}
		return ""
	})
	s = ordinalDay.ReplaceAllString(s, "$1")
	if m := gmtOffset.FindStringSubmatchIndex(s); m != nil {
		sign := s[m[2]:m[3]]
		hours := s[m[4]:m[5]]
		mins := "00"
		if m[6] >= 0 {
			mins = s[m[6]:m[7]]
		}
		if len(hours) == 1 {
			hours = "0" + hours
		}
		s = s[:m[0]] + " " + sign + hours + mins
	} else if m := trailingZone.FindStringSubmatchIndex(s); m != nil {
		name := strings.ToUpper(s[m[2]:m[3]])
		if off, ok := zoneOffsets[name]; ok && m[0] > 0 {
			s = s[:m[0]] + " " + off
		}
	}
	return s
}

func isMonthName(word string) bool {
	w := strings.ToLower(word)
	for m := time.January; m <= time.December; m++ {
		name := strings.ToLower(m.String())
		if w == name || w == name[:3] {
			return true
		}
	}
	return w == "sept"
}

// ParseDate reads a feed date in any of the shapes feeds use. The zero time
// means it could not be read. A date with no zone is taken as UTC.
func ParseDate(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	n := normalizeDate(s)
	n = strings.Replace(n, "Sept ", "Sep ", 1)
	for _, layout := range dateLayouts {
		if t, err := time.Parse(layout, n); err == nil {
			return t
		}
	}
	if secs, err := strconv.ParseInt(s, 10, 64); err == nil && secs > 0 {
		return time.Unix(secs, 0).UTC()
	}
	return time.Time{}
}
