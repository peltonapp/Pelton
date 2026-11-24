package desktop

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

// defaultDailyFormat names a daily note when the user set no format.
const defaultDailyFormat = "YYYY-MM-DD"

// noteDateToken matches the moment-style tokens the notes space uses for
// daily note names, with text in brackets kept as written. It mirrors
// frontend/src/lib/notetemplate.ts.
var noteDateToken = regexp.MustCompile(`\[([^\]]*)\]|YYYY|YY|MMMM|MMM|MM|M|DDDD|DD|Do|D|dddd|ddd|dd|HH|H|hh|h|mm|m|ss|s|A|a|ww|w|Q`)

// formatNoteDate formats a day the way the notes space names daily notes.
// Month and weekday names are English.
func formatNoteDate(t time.Time, format string) string {
	if format == "" {
		format = defaultDailyFormat
	}
	t = t.In(time.Local)
	pad := func(n int) string { return fmt.Sprintf("%02d", n) }
	h12 := t.Hour() % 12
	if h12 == 0 {
		h12 = 12
	}
	_, week := t.ISOWeek()
	return noteDateToken.ReplaceAllStringFunc(format, func(tok string) string {
		if tok[0] == '[' {
			return tok[1 : len(tok)-1]
		}
		switch tok {
		case "YYYY":
			return strconv.Itoa(t.Year())
		case "YY":
			return pad(t.Year() % 100)
		case "MMMM":
			return t.Month().String()
		case "MMM":
			return t.Month().String()[:3]
		case "MM":
			return pad(int(t.Month()))
		case "M":
			return strconv.Itoa(int(t.Month()))
		case "DDDD":
			return fmt.Sprintf("%03d", t.YearDay())
		case "DD":
			return pad(t.Day())
		case "D":
			return strconv.Itoa(t.Day())
		case "Do":
			return ordinalDay(t.Day())
		case "dddd":
			return t.Weekday().String()
		case "ddd":
			return t.Weekday().String()[:3]
		case "dd":
			return t.Weekday().String()[:1]
		case "HH":
			return pad(t.Hour())
		case "H":
			return strconv.Itoa(t.Hour())
		case "hh":
			return pad(h12)
		case "h":
			return strconv.Itoa(h12)
		case "mm":
			return pad(t.Minute())
		case "m":
			return strconv.Itoa(t.Minute())
		case "ss":
			return pad(t.Second())
		case "s":
			return strconv.Itoa(t.Second())
		case "A":
			if t.Hour() < 12 {
				return "AM"
			}
			return "PM"
		case "a":
			if t.Hour() < 12 {
				return "am"
			}
			return "pm"
		case "ww":
			return pad(week)
		case "w":
			return strconv.Itoa(week)
		case "Q":
			return strconv.Itoa((int(t.Month())-1)/3 + 1)
		}
		return tok
	})
}

func ordinalDay(n int) string {
	if r := n % 100; r >= 11 && r <= 13 {
		return strconv.Itoa(n) + "th"
	}
	switch n % 10 {
	case 1:
		return strconv.Itoa(n) + "st"
	case 2:
		return strconv.Itoa(n) + "nd"
	case 3:
		return strconv.Itoa(n) + "rd"
	}
	return strconv.Itoa(n) + "th"
}
