package scripting

import (
	"context"
	"strings"
	"testing"
	"unicode/utf16"
	"unicode/utf8"
)

func TestMatchPattern(t *testing.T) {
	mail := newFakeMail()
	s := testService(t, mail, nil)
	rep, err := s.TestMatch(context.Background(), MatchRequest{Folder: "inbox", Pattern: `(?i)invoice \d+`, Field: "subject"})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Scanned != 2 || rep.Matched != 1 || len(rep.Matches) != 1 {
		t.Fatalf("report %+v", rep)
	}
	m := rep.Matches[0]
	if m.MessageID != 1 || m.Text != "Invoice 42" || len(m.Spans) != 1 || m.Spans[0] != [2]int{0, 10} {
		t.Fatalf("match %+v", m)
	}
	rep, err = s.TestMatch(context.Background(), MatchRequest{Folder: "inbox", Pattern: `EUR`, Field: "body"})
	if err != nil || rep.Matched != 2 {
		t.Fatalf("body: %+v %v", rep, err)
	}
	if _, err := s.TestMatch(context.Background(), MatchRequest{Folder: "inbox", Pattern: `(`}); err == nil {
		t.Fatal("an invalid pattern was tried")
	}
	if len(mail.Calls()) != 0 {
		t.Fatalf("the test changed mail: %v", mail.Calls())
	}
}

func TestMatchConditionGroup(t *testing.T) {
	mail := newFakeMail()
	s := testService(t, mail, nil)
	when := Cond{Kind: CondAll, Items: []Cond{
		{Kind: CondCondition, Type: "mail.has_attachment"},
		{Kind: CondCondition, Type: "mail.header", Args: map[string]Arg{
			"name": {Value: "List-Id"}, "op": {Value: OpMatches}, "value": {Value: `shop\.example`},
		}},
	}}
	rep, err := s.TestMatch(context.Background(), MatchRequest{Folder: "inbox", When: &when})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Matched != 1 || rep.Matches[0].MessageID != 1 {
		t.Fatalf("report %+v", rep)
	}
	if m := rep.Matches[0]; m.Text != "<news.shop.example>" || m.Spans[0] != [2]int{6, 18} {
		t.Fatalf("the pattern of the group is not marked: %+v", m)
	}
	none := Cond{Kind: CondNot, Items: []Cond{{Kind: CondCondition, Type: "mail.folder", Args: map[string]Arg{"folder": {Value: "inbox"}}}}}
	rep, err = s.TestMatch(context.Background(), MatchRequest{Folder: "inbox", When: &none})
	if err != nil || rep.Matched != 0 || rep.Scanned != 2 {
		t.Fatalf("not in inbox: %+v %v", rep, err)
	}
	bad := Cond{Kind: CondCondition, Type: "nope"}
	if _, err := s.TestMatch(context.Background(), MatchRequest{Folder: "inbox", When: &bad}); err == nil || !strings.Contains(err.Error(), "unknown condition") {
		t.Fatalf("unknown condition: %v", err)
	}
}

func TestMatchConditionChangesNothing(t *testing.T) {
	mail := newFakeMail()
	s := testService(t, mail, nil)
	if err := s.reg.RegisterCondition(Condition{Meta: Meta{ID: "sneaky.archive", Label: "x"}, Permission: PermMailModify,
		Params: []Param{messageParam()}, Lua: "mail.archive({{message}}) == nil"}); err != nil {
		t.Fatal(err)
	}
	when := Cond{Kind: CondCondition, Type: "sneaky.archive"}
	rep, err := s.TestMatch(context.Background(), MatchRequest{Folder: "inbox", When: &when})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Matched != 2 || len(mail.Calls()) != 0 {
		t.Fatalf("matched %d, calls %v", rep.Matched, mail.Calls())
	}
}

func TestExcerptCountsUTF16(t *testing.T) {
	text := strings.Repeat("ä", 100) + "😀 match here"
	locs := [][]int{{strings.Index(text, "match"), strings.Index(text, "match") + 5}}
	window, spans := excerpt(text, locs)
	units := utf16.Encode([]rune(window))
	if got := string(utf16.Decode(units[spans[0][0]:spans[0][1]])); got != "match" {
		t.Fatalf("span %v marks %q in %q", spans[0], got, window)
	}
	if !utf8.ValidString(window) {
		t.Fatalf("the window cuts a character: %q", window)
	}
}
