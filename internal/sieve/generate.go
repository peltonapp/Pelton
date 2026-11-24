package sieve

import (
	"strconv"
	"strings"
)

const indent = "    "

// Generate writes the script as sieve text with LF line ends. It does not
// validate; call Validate first.
func (s *Script) Generate() string {
	var b strings.Builder
	b.WriteString(Header)
	b.WriteByte('\n')
	if req := s.Requires(); len(req) > 0 {
		b.WriteString("require " + stringList(req) + ";\n")
	}
	if v := s.Vacation; v != nil {
		b.WriteString("\n" + vacationComment + "\n")
		writeVacation(&b, v)
	}
	for _, r := range s.Rules {
		b.WriteString("\n" + ruleComment + oneLine(r.Name) + "\n")
		writeRule(&b, r)
	}
	for _, name := range s.Includes {
		b.WriteString("\n" + includeComment + "\n")
		b.WriteString("include :personal " + quoted(name) + ";\n")
	}
	return b.String()
}

// ToWire turns LF line ends into the CRLF the sieve grammar asks for.
func ToWire(text string) string {
	return strings.ReplaceAll(normalize(text), "\n", "\r\n")
}

func writeVacation(b *strings.Builder, v *Vacation) {
	var tests []string
	if !v.Enabled {
		tests = append(tests, "false")
	}
	if v.Start != "" {
		tests = append(tests, `currentdate :value "ge" "date" `+quoted(v.Start))
	}
	if v.End != "" {
		tests = append(tests, `currentdate :value "le" "date" `+quoted(v.End))
	}
	var cmd strings.Builder
	cmd.WriteString("vacation")
	if v.Days > 0 {
		cmd.WriteString(" :days " + strconv.Itoa(v.Days))
	}
	if v.Subject != "" {
		cmd.WriteString(" :subject " + quoted(v.Subject))
	}
	if v.From != "" {
		cmd.WriteString(" :from " + quoted(v.From))
	}
	if len(v.Addresses) > 0 {
		cmd.WriteString(" :addresses " + stringList(v.Addresses))
	}
	cmd.WriteString(" " + textString(v.Body) + ";\n")

	switch len(tests) {
	case 0:
		b.WriteString(cmd.String())
	case 1:
		b.WriteString("if " + tests[0] + " {\n" + indent + cmd.String() + "}\n")
	default:
		b.WriteString("if allof (" + strings.Join(tests, ", ") + ") {\n" + indent + cmd.String() + "}\n")
	}
}

func writeRule(b *strings.Builder, r Rule) {
	test := "true"
	if len(r.Conditions) > 0 {
		parts := make([]string, len(r.Conditions))
		for i, c := range r.Conditions {
			parts[i] = conditionTest(c)
		}
		combine := "allof"
		if r.MatchAny {
			combine = "anyof"
		}
		test = combine + " (" + strings.Join(parts, ", ") + ")"
	}
	if !r.Enabled {
		test = "allof (false, " + test + ")"
	}
	b.WriteString("if " + test + " {\n")
	for _, a := range r.Actions {
		b.WriteString(indent + actionCommand(a) + ";\n")
	}
	b.WriteString("}\n")
}

func conditionTest(c Condition) string {
	var t string
	switch c.Field {
	case FieldSize:
		t = "size :" + c.Op + " " + c.Value
	case FieldHeader:
		if c.Op == OpExists {
			t = "exists " + quoted(c.Header)
		} else {
			t = "header :" + c.Op + " " + quoted(c.Header) + " " + quoted(c.Value)
		}
	case FieldSubject:
		t = "header :" + c.Op + ` "subject" ` + quoted(c.Value)
	case FieldFrom, FieldTo:
		t = "address :" + c.Op + " " + quoted(c.Field) + " " + quoted(c.Value)
	case FieldEnvFrom:
		t = "envelope :" + c.Op + ` "from" ` + quoted(c.Value)
	case FieldEnvTo:
		t = "envelope :" + c.Op + ` "to" ` + quoted(c.Value)
	}
	if c.Negate {
		return "not " + t
	}
	return t
}

func actionCommand(a Action) string {
	switch a.Kind {
	case ActionFileInto, ActionRedirect, ActionAddFlag:
		return a.Kind + " " + quoted(a.Arg)
	}
	return a.Kind
}

func quoted(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

func stringList(items []string) string {
	parts := make([]string, len(items))
	for i, s := range items {
		parts[i] = quoted(s)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// textString writes a multi-line text: string, the body ending in one line
// break and lines that start with a dot doubled.
func textString(body string) string {
	body = strings.TrimRight(normalize(body), "\n")
	var b strings.Builder
	b.WriteString("text:\n")
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, ".") {
			b.WriteByte('.')
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	b.WriteString(".\n")
	return b.String()
}

func oneLine(s string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(s)
}
