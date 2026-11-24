package scripting

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

// The bounds of a match test: how many messages it looks at by default and
// at most, how many matches it returns, how long it may take in all and how
// long each message's run may take.
const (
	defaultMatchLimit = 100
	maxMatchLimit     = 500
	shownMatches      = 50
	matchTime         = 10 * time.Second
	matchRunTime      = 2 * time.Second
	maxSpans          = 20
	excerptBefore     = 80
	excerptLength     = 240
)

// MatchRequest asks which of the recent messages of a folder a regular
// expression or a condition matches.
type MatchRequest struct {
	// Folder is a path or a role; AccountID narrows it to one account, 0
	// looks in every account. Limit is how many of the newest messages are
	// looked at.
	Folder    string `json:"folder"`
	AccountID int64  `json:"accountId"`
	Limit     int    `json:"limit"`
	// Pattern is an RE2 expression tried on Field: "subject", "from", "to",
	// "body" or "headers.<name>".
	Pattern string `json:"pattern"`
	Field   string `json:"field"`
	// When is a condition or a group of them, as an if block holds it. With
	// it, a message matches when the condition holds in a dry run of the same
	// compiled code a script runs; Pattern then only marks where it matched.
	When *Cond `json:"when,omitempty"`
}

// Match is one message a test matched.
type Match struct {
	MessageID int64  `json:"messageId"`
	Subject   string `json:"subject"`
	From      string `json:"from"`
	Date      int64  `json:"date"`
	// Text is the tested field around the first place the pattern matched,
	// and Spans the matched ranges in it as UTF-16 offsets, start and end.
	Text  string   `json:"text,omitempty"`
	Spans [][2]int `json:"spans,omitempty"`
}

// MatchReport is what a match test found.
type MatchReport struct {
	Scanned int     `json:"scanned"`
	Matched int     `json:"matched"`
	Matches []Match `json:"matches"`
	// TimedOut is set when the test ran out of time before the last message.
	TimedOut bool `json:"timedOut"`
}

// TestMatch tries a pattern or a condition on the newest messages of a
// folder, read only: a condition runs as a dry run, so nothing it calls can
// change anything, within a time limit for the whole test.
func (s *Service) TestMatch(ctx context.Context, req MatchRequest) (*MatchReport, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = defaultMatchLimit
	}
	if limit > maxMatchLimit {
		limit = maxMatchLimit
	}
	pattern, field := req.Pattern, req.Field
	if pattern == "" && req.When != nil {
		pattern, field = s.reg.patternOf(*req.When)
	}
	var re *regexp.Regexp
	if pattern != "" {
		if len(pattern) > maxPattern {
			return nil, fmt.Errorf("scripting: the regular expression is longer than %d characters", maxPattern)
		}
		var err error
		if re, err = regexp.Compile(pattern); err != nil {
			return nil, fmt.Errorf("scripting: invalid regular expression: %w", err)
		}
	}
	if re == nil && req.When == nil {
		return nil, errors.New("scripting: give a regular expression or a condition to test")
	}
	var probe *Script
	if req.When != nil {
		var err error
		if probe, err = s.matchScript(*req.When); err != nil {
			return nil, err
		}
	}
	lister, _ := s.cfg.Hosts.Mail.(MailLister)
	if lister == nil {
		return nil, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, matchTime)
	defer cancel()
	list, err := lister.Recent(ctx, req.Folder, req.AccountID, limit)
	if err != nil {
		return nil, err
	}
	engine := *s.engine
	engine.Limits.Timeout = matchRunTime
	engine.OnAllow = nil
	report := &MatchReport{Matches: []Match{}}
	for _, m := range list {
		if ctx.Err() != nil {
			report.TimedOut = true
			break
		}
		report.Scanned++
		text := ""
		var locs [][]int
		if re != nil {
			text = s.fieldText(ctx, m, field)
			locs = re.FindAllStringIndex(text, maxSpans)
		}
		hit := locs != nil
		if probe != nil {
			run := engine.Run(ctx, RunRequest{Script: probe, Trigger: TriggerMailArrived, Dry: true, Event: map[string]any{"message": MessageObject(m)}})
			if run.Status == StatusTimeout && ctx.Err() != nil {
				report.TimedOut = true
				break
			}
			if run.Status != StatusOK {
				return nil, fmt.Errorf("scripting: the condition failed on %q: %s", clip(m.Subject, 60), run.Error)
			}
			hit = checkResult(run)
		}
		if !hit {
			continue
		}
		report.Matched++
		if len(report.Matches) < shownMatches {
			match := Match{MessageID: m.ID, Subject: m.Subject, From: m.From, Date: m.Date.Unix()}
			if locs != nil {
				match.Text, match.Spans = excerpt(text, locs)
			}
			report.Matches = append(report.Matches, match)
		}
	}
	return report, nil
}

// matchScript wraps a condition in a dry run block script whose run records
// the condition's outcome as a check step.
func (s *Service) matchScript(when Cond) (*Script, error) {
	tree := Tree{Version: TreeVersion, Blocks: []Block{{ID: "test", Kind: BlockIf, Branches: []Branch{{When: when}}}}}
	res := Compile(s.reg, tree, TriggerMailArrived, false)
	if len(res.Errors) > 0 {
		return nil, res.Errors[0]
	}
	return &Script{Name: "match test", Kind: KindBlocks, Blocks: &tree, Trigger: TriggerConfig{Type: TriggerMailArrived},
		Permissions: res.Permissions, CatchUp: CatchUpLive}, nil
}

func checkResult(run *Run) bool {
	for _, st := range run.Steps {
		if st.Kind == StepCheck && st.Block == "test" {
			return st.Result
		}
	}
	return false
}

// patternOf finds the regular expression a condition tests and the field it
// tests it on: the first condition, alone or in a group, that matches a
// literal pattern on a field it names.
func (r *Registry) patternOf(c Cond) (string, string) {
	if c.Kind != CondCondition {
		for _, item := range c.Items {
			if p, f := r.patternOf(item); p != "" {
				return p, f
			}
		}
		return "", ""
	}
	def, ok := r.Condition(c.Type)
	if !ok || def.Field == "" || c.Args["op"].Value != OpMatches {
		return "", ""
	}
	pattern, _ := c.Args["value"].Value.(string)
	field := def.Field
	if field == "headers" {
		name, _ := c.Args["name"].Value.(string)
		if name == "" {
			return "", ""
		}
		field += "." + strings.ToLower(name)
	}
	return pattern, field
}

// fieldText is the text of a message's field as the conditions test it.
func (s *Service) fieldText(ctx context.Context, m Message, field string) string {
	switch {
	case field == "subject":
		return m.Subject
	case field == "from":
		if m.FromName == "" {
			return m.From
		}
		return m.FromName + " <" + m.From + ">"
	case field == "to":
		return strings.Trim(m.To+", "+m.Cc, ", ")
	case field == "body":
		body, err := s.cfg.Hosts.Mail.Body(ctx, m.ID)
		if err != nil {
			return ""
		}
		return body
	case strings.HasPrefix(field, "headers."):
		return m.Headers[strings.TrimPrefix(field, "headers.")]
	}
	return ""
}

// excerpt cuts text down to a window around the first match and returns
// the matches inside it as UTF-16 offsets, which is how the frontend counts.
func excerpt(text string, locs [][]int) (string, [][2]int) {
	start := max(0, locs[0][0]-excerptBefore)
	for start > 0 && !utf8.RuneStart(text[start]) {
		start--
	}
	end := min(len(text), start+excerptLength)
	for end < len(text) && !utf8.RuneStart(text[end]) {
		end++
	}
	window := text[start:end]
	units := func(s string) int { return len(utf16.Encode([]rune(s))) }
	var spans [][2]int
	for _, l := range locs {
		if l[0] < start || l[1] > end {
			continue
		}
		from := units(text[start:l[0]])
		spans = append(spans, [2]int{from, from + units(text[l[0]:l[1]])})
	}
	return window, spans
}
