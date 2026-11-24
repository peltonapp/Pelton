package scripting

import (
	"fmt"
	"regexp"
	"strings"
)

// The text match operators, as conditions offer them.
const (
	OpContains = "contains"
	OpEquals   = "equals"
	OpMatches  = "matches"
	OpStarts   = "starts"
	OpEnds     = "ends"
)

// textOps is the option list of a text match.
func textOps() []Option {
	return options(OpContains, OpEquals, OpMatches, OpStarts, OpEnds)
}

// candidates turns a string or a list of strings into the texts to test.
func candidates(v any) []string {
	switch x := v.(type) {
	case string:
		return []string{x}
	case []any:
		out := make([]string, 0, len(x))
		for _, item := range x {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case float64:
		return []string{fmt.Sprint(x)}
	}
	return []string{""}
}

// testText applies a match operator. Everything but matches ignores case;
// a regular expression asks for that itself with (?i).
func testText(c *Call, subject any, op, value string) (bool, error) {
	var re *regexp.Regexp
	if op == OpMatches {
		var err error
		if re, err = c.Regexp(value); err != nil {
			return false, err
		}
	}
	lv := strings.ToLower(value)
	for _, s := range candidates(subject) {
		ls := strings.ToLower(s)
		var hit bool
		switch op {
		case OpContains:
			hit = strings.Contains(ls, lv)
		case OpEquals:
			hit = ls == lv
		case OpStarts:
			hit = strings.HasPrefix(ls, lv)
		case OpEnds:
			hit = strings.HasSuffix(ls, lv)
		case OpMatches:
			hit = re.MatchString(s)
		default:
			return false, fmt.Errorf("text.test: unknown operator %q", op)
		}
		if hit {
			return true, nil
		}
	}
	return false, nil
}

func textModule() Module {
	str := func(name string, f func(string) any) Function {
		return fn(name, func(c *Call) ([]any, error) {
			s, err := c.OptString(0, "")
			if err != nil {
				return nil, err
			}
			return []any{f(s)}, nil
		}, param("text", TypeText))
	}
	pair := func(name string, f func(a, b string) bool) Function {
		return fn(name, func(c *Call) ([]any, error) {
			a, err := c.OptString(0, "")
			if err != nil {
				return nil, err
			}
			b, err := c.String(1)
			if err != nil {
				return nil, err
			}
			return []any{f(strings.ToLower(a), strings.ToLower(b))}, nil
		}, param("text", TypeText), param("value", TypeString))
	}
	op := param("op", TypeEnum)
	op.Options = textOps()
	fns := []Function{
		fn("test", func(c *Call) ([]any, error) {
			op, err := c.String(1)
			if err != nil {
				return nil, err
			}
			value, err := c.OptString(2, "")
			if err != nil {
				return nil, err
			}
			ok, err := testText(c, c.Arg(0), op, value)
			return []any{ok}, err
		}, param("text", TypeText), op, param("value", TypeString)),
		pair("contains", strings.Contains),
		pair("starts", strings.HasPrefix),
		pair("ends", strings.HasSuffix),
		str("lower", func(s string) any { return strings.ToLower(s) }),
		str("upper", func(s string) any { return strings.ToUpper(s) }),
		str("trim", func(s string) any { return strings.TrimSpace(s) }),
		str("escape", func(s string) any { return regexp.QuoteMeta(s) }),
		fn("split", func(c *Call) ([]any, error) {
			s, err := c.OptString(0, "")
			if err != nil {
				return nil, err
			}
			sep, err := c.OptString(1, ",")
			if err != nil {
				return nil, err
			}
			out := []any{}
			if sep == "" {
				return []any{out}, nil
			}
			for _, part := range strings.Split(s, sep) {
				if part = strings.TrimSpace(part); part != "" {
					out = append(out, part)
				}
			}
			return []any{out}, nil
		}, param("text", TypeText), param("separator", TypeString)),
		fn("matches", func(c *Call) ([]any, error) {
			s, re, err := textAndRegexp(c)
			if err != nil {
				return nil, err
			}
			return []any{re.MatchString(s)}, nil
		}, param("text", TypeText), param("pattern", TypeRegex)),
		fn("match", func(c *Call) ([]any, error) {
			s, re, err := textAndRegexp(c)
			if err != nil {
				return nil, err
			}
			m := re.FindStringSubmatch(s)
			if m == nil {
				return []any{nil}, nil
			}
			if len(m) == 1 {
				return []any{m[0]}, nil
			}
			out := make([]any, 0, len(m)-1)
			for _, g := range m[1:] {
				out = append(out, g)
			}
			return out, nil
		}, param("text", TypeText), param("pattern", TypeRegex)),
		fn("find", func(c *Call) ([]any, error) {
			s, re, err := textAndRegexp(c)
			if err != nil {
				return nil, err
			}
			loc := re.FindStringIndex(s)
			if loc == nil {
				return []any{nil}, nil
			}
			return []any{loc[0] + 1, loc[1]}, nil
		}, param("text", TypeText), param("pattern", TypeRegex)),
		fn("find_all", func(c *Call) ([]any, error) {
			s, re, err := textAndRegexp(c)
			if err != nil {
				return nil, err
			}
			out := []any{}
			for _, m := range re.FindAllString(s, 1000) {
				out = append(out, m)
			}
			return []any{out}, nil
		}, param("text", TypeText), param("pattern", TypeRegex)),
		fn("replace", func(c *Call) ([]any, error) {
			s, re, err := textAndRegexp(c)
			if err != nil {
				return nil, err
			}
			with, err := c.OptString(2, "")
			if err != nil {
				return nil, err
			}
			out, err := boundedReplace(re, s, with, maxBuiltString)
			if err != nil {
				return nil, err
			}
			return []any{out}, nil
		}, param("text", TypeText), param("pattern", TypeRegex), param("with", TypeString)),
	}
	for i := range fns {
		fns[i].DescKey = modDesc("text", fns[i].Name)
	}
	return Module{Meta: meta("module", "text", "utility"), Functions: fns}
}

// boundedReplace is re.ReplaceAllString that gives up as soon as the result
// passes limit bytes, instead of building it first.
func boundedReplace(re *regexp.Regexp, s, with string, limit int) (string, error) {
	tooLong := fmt.Errorf("text.replace would build more than %d bytes", limit)
	if with == "" {
		return re.ReplaceAllString(s, ""), nil
	}
	matches := re.FindAllStringSubmatchIndex(s, limit+1)
	if len(matches) > limit {
		return "", tooLong
	}
	var out []byte
	last := 0
	for _, m := range matches {
		out = append(out, s[last:m[0]]...)
		out = re.ExpandString(out, with, s, m)
		last = m[1]
		if len(out) > limit {
			return "", tooLong
		}
	}
	out = append(out, s[last:]...)
	if len(out) > limit {
		return "", tooLong
	}
	return string(out), nil
}

func textAndRegexp(c *Call) (string, *regexp.Regexp, error) {
	s, err := c.OptString(0, "")
	if err != nil {
		return "", nil, err
	}
	pattern, err := c.String(1)
	if err != nil {
		return "", nil, err
	}
	re, err := c.Regexp(pattern)
	return s, re, err
}
