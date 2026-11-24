package sieve

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func fullScript() *Script {
	return &Script{
		Vacation: &Vacation{
			Enabled:   true,
			Start:     "2026-12-20",
			End:       "2027-01-06",
			Days:      7,
			Subject:   `Away "until" January`,
			From:      "me@example.org",
			Addresses: []string{"me@example.org", "alias@example.org"},
			Body:      "I am away.\n.hidden dot line\n\nBack on the 6th.",
		},
		Rules: []Rule{
			{
				Name:    "Newsletters",
				Enabled: true,
				Conditions: []Condition{
					{Field: FieldFrom, Op: OpContains, Value: "news@"},
					{Field: FieldHeader, Header: "List-Id", Op: OpExists},
				},
				MatchAny: true,
				Actions:  []Action{{Kind: ActionFileInto, Arg: "Lists/News"}, {Kind: ActionAddFlag, Arg: `\Seen`}, {Kind: ActionStop}},
			},
			{
				Name:    "Big from boss",
				Enabled: false,
				Conditions: []Condition{
					{Field: FieldSize, Op: OpOver, Value: "5M"},
					{Field: FieldTo, Op: OpIs, Value: "me@example.org", Negate: true},
					{Field: FieldSubject, Op: OpMatches, Value: `*\urgent*`},
					{Field: FieldEnvFrom, Op: OpIs, Value: "boss@example.org"},
				},
				Actions: []Action{{Kind: ActionRedirect, Arg: "phone@example.net"}, {Kind: ActionKeep}, {Kind: ActionAddFlag, Arg: `\Flagged`}},
			},
			{Name: "Everything else", Enabled: true, Actions: []Action{{Kind: ActionDiscard}}},
		},
		Includes: []string{"roundcube"},
	}
}

func allExtensions(string) bool { return true }

func TestRoundTripIsExact(t *testing.T) {
	for _, tt := range []struct {
		name   string
		script *Script
	}{
		{"empty", &Script{}},
		{"full", fullScript()},
		{"vacation only, off, no dates", &Script{Vacation: &Vacation{Body: "Away."}}},
		{"vacation from a day on", &Script{Vacation: &Vacation{Enabled: true, Start: "2026-01-01", Body: "x"}}},
		{"vacation off with an end", &Script{Vacation: &Vacation{End: "2026-01-01", Body: "x"}}},
		{"rule with all of one condition", &Script{Rules: []Rule{{Name: "", Enabled: true, Conditions: []Condition{{Field: FieldSubject, Op: OpIs, Value: "hi"}}, Actions: []Action{{Kind: ActionStop}}}}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.script.Validate(allExtensions); err != nil && tt.name != "empty" {
				t.Fatalf("validate: %v", err)
			}
			text := tt.script.Generate()
			got, err := Parse(text)
			if err != nil {
				t.Fatalf("parse of generated text: %v\n%s", err, text)
			}
			if again := got.Generate(); again != text {
				t.Fatalf("regenerated text differs:\n%s\n---\n%s", text, again)
			}
			if !reflect.DeepEqual(normalized(got), normalized(tt.script)) {
				t.Fatalf("model differs:\n got %+v\nwant %+v", got, tt.script)
			}
			wire := ToWire(text)
			if !strings.Contains(wire, "\r\n") || strings.Contains(strings.ReplaceAll(wire, "\r\n", ""), "\n") {
				t.Fatal("wire form is not CRLF throughout")
			}
			if _, err := Parse(wire); err != nil {
				t.Fatalf("parse of the CRLF form: %v", err)
			}
		})
	}
}

// normalized drops the distinction between nil and empty slices.
func normalized(s *Script) Script {
	out := *s
	if len(out.Rules) == 0 {
		out.Rules = nil
	}
	if len(out.Includes) == 0 {
		out.Includes = nil
	}
	return out
}

func TestGeneratedTextShape(t *testing.T) {
	text := fullScript().Generate()
	for _, want := range []string{
		`require ["date", "envelope", "fileinto", "imap4flags", "include", "relational", "vacation"];`,
		`if allof (currentdate :value "ge" "date" "2026-12-20", currentdate :value "le" "date" "2027-01-06") {`,
		`vacation :days 7 :subject "Away \"until\" January" :from "me@example.org" :addresses ["me@example.org", "alias@example.org"] text:`,
		"\n..hidden dot line\n",
		`if anyof (address :contains "from" "news@", exists "List-Id") {`,
		`addflag "\\Seen";`,
		`if allof (false, allof (size :over 5M, not address :is "to" "me@example.org", header :matches "subject" "*\\urgent*", envelope :is "from" "boss@example.org")) {`,
		"if true {\n    discard;\n}",
		`include :personal "roundcube";`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("generated script lacks %q:\n%s", want, text)
		}
	}
}

func TestForeignScriptsAreNotModelled(t *testing.T) {
	pelton := fullScript().Generate()
	for _, tt := range []struct {
		name string
		text string
	}{
		{"roundcube", "require [\"fileinto\"];\n# rule:[spam]\nif header :contains \"x-spam\" \"yes\" {\n\tfileinto \"Junk\";\n}\n"},
		{"pelton header but a hand edit", strings.Replace(pelton, "    stop;\n", "    stop; # done\n", 1)},
		{"pelton header with reformatting", strings.Replace(pelton, "if true {", "if true{", 1)},
		{"extension outside the subset", Header + "\nrequire [\"reject\"];\n\n# Pelton rule: r\nif true {\n    reject \"no\";\n}\n"},
		{"unknown section", Header + "\n\n# my own\nkeep;\n"},
		{"elsif", Header + "\n\n# Pelton rule: r\nif true {\n    keep;\n}\nelsif false {\n    stop;\n}\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.text)
			if err == nil {
				t.Fatal("a foreign script was accepted as Pelton's")
			}
			var syn *SyntaxError
			if !errors.Is(err, ErrForeign) && !errors.As(err, &syn) {
				t.Fatalf("err = %v, want ErrForeign or a syntax error", err)
			}
		})
	}
}

func TestSyntaxErrorNamesTheLine(t *testing.T) {
	_, err := Parse(Header + "\n\n# Pelton rule: r\nif true {\n    keep\n")
	var syn *SyntaxError
	if !errors.As(err, &syn) || syn.Line < 5 {
		t.Fatalf("err = %v, want a syntax error on line 5 or later", err)
	}
}

func TestGenericSyntax(t *testing.T) {
	src := "/* block\ncomment */ require [\"fileinto\", \"vacation\"]; # tail\r\n" +
		"if anyof (not exists [\"x\"], size :under 10k) { fileinto \"a\"; }\r\n" +
		"elsif header :is \"s\" \"t\" { stop; } else { keep; }\n" +
		"vacation text: # comment\n..dot\nline\n.\n;\n"
	cmds, err := parseCommands(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	names := make([]string, len(cmds))
	for i, c := range cmds {
		names[i] = c.Name
	}
	if !slices.Equal(names, []string{"require", "if", "elsif", "else", "vacation"}) {
		t.Fatalf("commands = %v", names)
	}
	if got := cmds[1].Tests[0].Tests[1].Args[1].Value; got != "10K" {
		t.Errorf("size number = %q, want 10K", got)
	}
	if got := cmds[4].Args[0].Strings[0]; got != ".dot\nline\n" {
		t.Errorf("text: body = %q", got)
	}
}

func TestCapabilityGating(t *testing.T) {
	s := &Script{Rules: []Rule{{Enabled: true, Actions: []Action{{Kind: ActionDiscard}}}}}
	if got := s.Requires(); len(got) != 0 {
		t.Errorf("discard only requires %v, want nothing", got)
	}
	if strings.Contains(s.Generate(), "require") {
		t.Error("a script needing no extension still has a require line")
	}

	s = &Script{Vacation: &Vacation{Enabled: true, Body: "x"}}
	if got := s.Requires(); !slices.Equal(got, []string{"vacation"}) {
		t.Errorf("undated vacation requires %v", got)
	}
	s.Vacation.Start = "2026-01-01"
	if got := s.Requires(); !slices.Equal(got, []string{"date", "relational", "vacation"}) {
		t.Errorf("dated vacation requires %v", got)
	}
	noDate := func(ext string) bool { return ext == "vacation" }
	var unsupported *UnsupportedError
	if err := s.Validate(noDate); !errors.As(err, &unsupported) || unsupported.Extension != "date" {
		t.Errorf("validate without date = %v, want UnsupportedError for date", err)
	}
	s.Vacation.Start = ""
	if err := s.Validate(noDate); err != nil {
		t.Errorf("validate an undated vacation without date: %v", err)
	}
}

func TestValidateRejectsUnwritableRules(t *testing.T) {
	for _, tt := range []struct {
		name string
		rule Rule
	}{
		{"no actions", Rule{}},
		{"size with contains", Rule{Conditions: []Condition{{Field: FieldSize, Op: OpContains, Value: "1"}}, Actions: []Action{{Kind: ActionKeep}}}},
		{"size not a number", Rule{Conditions: []Condition{{Field: FieldSize, Op: OpOver, Value: "big"}}, Actions: []Action{{Kind: ActionKeep}}}},
		{"empty value", Rule{Conditions: []Condition{{Field: FieldFrom, Op: OpIs}}, Actions: []Action{{Kind: ActionKeep}}}},
		{"header without a name", Rule{Conditions: []Condition{{Field: FieldHeader, Op: OpExists}}, Actions: []Action{{Kind: ActionKeep}}}},
		{"forward without an address", Rule{Actions: []Action{{Kind: ActionRedirect, Arg: "nobody"}}}},
		{"name with a line break", Rule{Name: "a\nb", Actions: []Action{{Kind: ActionKeep}}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := &Script{Rules: []Rule{tt.rule}}
			var invalid *InvalidError
			if err := s.Validate(allExtensions); !errors.As(err, &invalid) {
				t.Fatalf("validate = %v, want InvalidError", err)
			}
		})
	}
	bad := &Script{Vacation: &Vacation{Body: "x", Start: "2026-02-01", End: "2026-01-01"}}
	if err := bad.Validate(allExtensions); err == nil {
		t.Error("an end before the start was accepted")
	}
}
