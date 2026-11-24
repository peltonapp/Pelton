package sieve

import (
	"fmt"
	"strconv"
	"strings"
)

// Parse reads a script Pelton wrote. It returns ErrForeign, wrapped with the
// reason, for any script that does not come back byte for byte (line ends
// aside) when generated again, so editing it can never change something the
// model does not show. A script that does not parse at all is a *SyntaxError.
func Parse(text string) (*Script, error) {
	text = normalize(text)
	if !strings.HasPrefix(text, Header+"\n") {
		return nil, fmt.Errorf("%w: no Pelton header", ErrForeign)
	}
	cmds, err := parseCommands(text)
	if err != nil {
		return nil, err
	}
	s := &Script{}
	for i, cmd := range cmds {
		if i == 0 && cmd.Name == "require" {
			continue
		}
		if err := s.addSection(cmd); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrForeign, err)
		}
	}
	if s.Generate() != text {
		return nil, fmt.Errorf("%w: it was changed outside Pelton", ErrForeign)
	}
	return s, nil
}

// addSection adds one top-level command, recognized by the comment above it.
func (s *Script) addSection(cmd Command) error {
	if len(cmd.Comments) == 0 {
		return fmt.Errorf("%s has no Pelton comment", cmd.Name)
	}
	comment := cmd.Comments[len(cmd.Comments)-1]
	switch {
	case comment == vacationComment:
		if s.Vacation != nil {
			return fmt.Errorf("a second out of office")
		}
		v, err := vacationFrom(cmd)
		if err != nil {
			return err
		}
		s.Vacation = v
	case strings.HasPrefix(comment, ruleComment):
		r, err := ruleFrom(cmd)
		if err != nil {
			return err
		}
		r.Name = strings.TrimPrefix(comment, ruleComment)
		s.Rules = append(s.Rules, r)
	case comment == includeComment:
		if cmd.Name != "include" || len(cmd.Tests) > 0 || cmd.HasBlock {
			return fmt.Errorf("unexpected %s", cmd.Name)
		}
		strs := stringArgs(cmd.Args)
		if len(strs) != 1 || len(strs[0].Strings) != 1 {
			return fmt.Errorf("include without one script name")
		}
		s.Includes = append(s.Includes, strs[0].Strings[0])
	default:
		return fmt.Errorf("unknown section %q", comment)
	}
	return nil
}

func vacationFrom(cmd Command) (*Vacation, error) {
	v := &Vacation{Enabled: true}
	body := cmd
	if cmd.Name == "if" {
		if len(cmd.Block) != 1 || len(cmd.Tests) != 1 {
			return nil, fmt.Errorf("out of office block holds more than the reply")
		}
		tests := cmd.Tests
		if cmd.Tests[0].Name == "allof" {
			tests = cmd.Tests[0].Tests
		}
		for _, t := range tests {
			switch {
			case t.Name == "false":
				v.Enabled = false
			case t.Name == "currentdate":
				rel, day, ok := dateBound(t)
				if !ok {
					return nil, fmt.Errorf("unexpected date test")
				}
				if rel == "ge" {
					v.Start = day
				} else {
					v.End = day
				}
			default:
				return nil, fmt.Errorf("unexpected test %s in the out of office", t.Name)
			}
		}
		body = cmd.Block[0]
	}
	if body.Name != "vacation" {
		return nil, fmt.Errorf("unexpected %s in the out of office", body.Name)
	}
	args := body.Args
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a.Kind != ArgTag {
			if i != len(args)-1 || a.Kind != ArgStrings || len(a.Strings) != 1 {
				return nil, fmt.Errorf("unexpected vacation argument")
			}
			v.Body = strings.TrimSuffix(a.Strings[0], "\n")
			continue
		}
		if i+1 >= len(args) {
			return nil, fmt.Errorf("vacation tag :%s without a value", a.Value)
		}
		val := args[i+1]
		i++
		switch a.Value {
		case "days":
			if val.Kind != ArgNumber {
				return nil, fmt.Errorf(":days needs a number")
			}
			n, err := strconv.Atoi(val.Value)
			if err != nil {
				return nil, fmt.Errorf(":days %q", val.Value)
			}
			v.Days = n
		case "subject", "from":
			if val.Kind != ArgStrings || len(val.Strings) != 1 {
				return nil, fmt.Errorf(":%s needs a string", a.Value)
			}
			if a.Value == "subject" {
				v.Subject = val.Strings[0]
			} else {
				v.From = val.Strings[0]
			}
		case "addresses":
			if val.Kind != ArgStrings {
				return nil, fmt.Errorf(":addresses needs strings")
			}
			v.Addresses = append([]string(nil), val.Strings...)
		default:
			return nil, fmt.Errorf("vacation tag :%s is outside the subset", a.Value)
		}
	}
	return v, nil
}

// dateBound reads currentdate :value "ge"|"le" "date" "<day>".
func dateBound(t Test) (rel, day string, ok bool) {
	a := t.Args
	if len(a) != 4 || a[0].Kind != ArgTag || a[0].Value != "value" {
		return "", "", false
	}
	for _, x := range a[1:] {
		if x.Kind != ArgStrings || len(x.Strings) != 1 {
			return "", "", false
		}
	}
	rel = a[1].Strings[0]
	if (rel != "ge" && rel != "le") || a[2].Strings[0] != "date" {
		return "", "", false
	}
	return rel, a[3].Strings[0], true
}

func ruleFrom(cmd Command) (Rule, error) {
	r := Rule{Enabled: true}
	if cmd.Name != "if" || len(cmd.Tests) != 1 || !cmd.HasBlock {
		return r, fmt.Errorf("a rule is an if with a block")
	}
	test := cmd.Tests[0]
	if test.Name == "allof" && len(test.Tests) == 2 && test.Tests[0].Name == "false" {
		r.Enabled = false
		test = test.Tests[1]
	}
	switch test.Name {
	case "true":
	case "allof", "anyof":
		r.MatchAny = test.Name == "anyof"
		for _, t := range test.Tests {
			c, err := conditionFrom(t)
			if err != nil {
				return r, err
			}
			r.Conditions = append(r.Conditions, c)
		}
	default:
		return r, fmt.Errorf("unexpected rule test %s", test.Name)
	}
	for _, a := range cmd.Block {
		act, err := actionFrom(a)
		if err != nil {
			return r, err
		}
		r.Actions = append(r.Actions, act)
	}
	return r, nil
}

func conditionFrom(t Test) (Condition, error) {
	var c Condition
	if t.Name == "not" {
		if len(t.Tests) != 1 {
			return c, fmt.Errorf("not without one test")
		}
		inner, err := conditionFrom(t.Tests[0])
		if err != nil {
			return c, err
		}
		if inner.Negate {
			return c, fmt.Errorf("double negation")
		}
		inner.Negate = true
		return inner, nil
	}
	tags, strs, nums := splitArgs(t.Args)
	switch t.Name {
	case "size":
		if len(tags) != 1 || len(nums) != 1 || len(strs) != 0 {
			return c, fmt.Errorf("unexpected size test")
		}
		return Condition{Field: FieldSize, Op: tags[0], Value: nums[0]}, nil
	case "exists":
		if len(tags) != 0 || len(strs) != 1 || len(strs[0].Strings) != 1 {
			return c, fmt.Errorf("unexpected exists test")
		}
		return Condition{Field: FieldHeader, Op: OpExists, Header: strs[0].Strings[0]}, nil
	case "header", "address", "envelope":
		if len(tags) != 1 || len(strs) != 2 || len(nums) != 0 || len(strs[0].Strings) != 1 || len(strs[1].Strings) != 1 {
			return c, fmt.Errorf("%s test outside the subset", t.Name)
		}
		c.Op = tags[0]
		name, value := strs[0].Strings[0], strs[1].Strings[0]
		c.Value = value
		switch {
		case t.Name == "header" && name == "subject":
			c.Field = FieldSubject
		case t.Name == "header":
			c.Field, c.Header = FieldHeader, name
		case t.Name == "address" && (name == "from" || name == "to"):
			c.Field = name
		case t.Name == "envelope" && name == "from":
			c.Field = FieldEnvFrom
		case t.Name == "envelope" && name == "to":
			c.Field = FieldEnvTo
		default:
			return c, fmt.Errorf("%s on %q is outside the subset", t.Name, name)
		}
		return c, nil
	}
	return c, fmt.Errorf("test %s is outside the subset", t.Name)
}

func actionFrom(cmd Command) (Action, error) {
	if len(cmd.Tests) > 0 || cmd.HasBlock {
		return Action{}, fmt.Errorf("unexpected %s in a rule", cmd.Name)
	}
	switch cmd.Name {
	case ActionKeep, ActionDiscard, ActionStop:
		if len(cmd.Args) != 0 {
			return Action{}, fmt.Errorf("%s takes no arguments", cmd.Name)
		}
		return Action{Kind: cmd.Name}, nil
	case ActionFileInto, ActionRedirect, ActionAddFlag:
		if len(cmd.Args) != 1 || cmd.Args[0].Kind != ArgStrings || len(cmd.Args[0].Strings) != 1 {
			return Action{}, fmt.Errorf("%s outside the subset", cmd.Name)
		}
		return Action{Kind: cmd.Name, Arg: cmd.Args[0].Strings[0]}, nil
	}
	return Action{}, fmt.Errorf("action %s is outside the subset", cmd.Name)
}

func splitArgs(args []Arg) (tags []string, strs []Arg, nums []string) {
	for _, a := range args {
		switch a.Kind {
		case ArgTag:
			tags = append(tags, a.Value)
		case ArgNumber:
			nums = append(nums, a.Value)
		case ArgStrings:
			strs = append(strs, a)
		}
	}
	return tags, strs, nums
}

func stringArgs(args []Arg) []Arg {
	_, strs, _ := splitArgs(args)
	return strs
}
