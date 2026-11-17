package scripting

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// ParamType is the type of a parameter, which decides the editor control and
// how a literal compiles to Lua.
type ParamType string

// The parameter types.
const (
	// TypeString is one line of text, TypeText several.
	TypeString ParamType = "string"
	TypeText   ParamType = "text"
	TypeNumber ParamType = "number"
	TypeBool   ParamType = "bool"
	// TypeEnum is one of Param.Options.
	TypeEnum ParamType = "enum"
	// TypeRegex is an RE2 regular expression, checked when compiled.
	TypeRegex ParamType = "regex"
	// TypeDuration is a span like "15m", "2h", "3d" or "1w", or a number of
	// seconds; it compiles to seconds.
	TypeDuration ParamType = "duration"
	// TypeTime is a time of day, "HH:MM".
	TypeTime ParamType = "time"
	// TypeFolder names a folder by path or by role (inbox, archive, trash,
	// junk, sent, drafts), resolved in the message's own account, so a
	// script carries no account specific ids.
	TypeFolder ParamType = "folder"
	// TypeAccount is a local account id. It is personal, so an export
	// leaves it out.
	TypeAccount ParamType = "account"
	// TypeColor is a colour label, 0 for none or 1 to 8.
	TypeColor ParamType = "color"
	// TypeAddress is an email address or part of one.
	TypeAddress ParamType = "address"
	// TypeList is a list of Param.Of values.
	TypeList ParamType = "list"
	// TypeRef only takes a reference to a value: a trigger field, a variable,
	// an earlier step's output or a script setting.
	TypeRef ParamType = "ref"
	// TypeIcon names an icon of the app's script icon set.
	TypeIcon ParamType = "icon"
)

var paramTypes = []ParamType{
	TypeString, TypeText, TypeNumber, TypeBool, TypeEnum, TypeRegex, TypeDuration,
	TypeTime, TypeFolder, TypeAccount, TypeColor, TypeAddress, TypeList, TypeRef, TypeIcon,
}

// settingTypes are the types a script setting may have. None holds a secret:
// settings are stored and shown in plain text.
var settingTypes = []ParamType{
	TypeString, TypeText, TypeNumber, TypeBool, TypeEnum, TypeFolder, TypeAccount, TypeColor,
}

// Param describes one parameter of a trigger, condition, action, function or
// script setting.
type Param struct {
	Name     string    `json:"name"`
	Type     ParamType `json:"type"`
	LabelKey string    `json:"labelKey"`
	Label    string    `json:"label,omitempty"`
	DescKey  string    `json:"descKey,omitempty"`
	// Description is the English text shown when DescKey has no string.
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
	// Default is the literal used when no value is given.
	Default any `json:"default,omitempty"`
	// DefaultRef is the reference used when no value is given, e.g.
	// "event.message".
	DefaultRef string `json:"defaultRef,omitempty"`
	// Options are the choices of an enum.
	Options []Option `json:"options,omitempty"`
	// Of is the element type of a list.
	Of ParamType `json:"of,omitempty"`
	// ValueType is the value type a reference should point at, e.g.
	// "message", so the editor offers only fitting references.
	ValueType string `json:"valueType,omitempty"`
}

// Option is one choice of an enum parameter.
type Option struct {
	Value    string `json:"value"`
	LabelKey string `json:"labelKey"`
	Label    string `json:"label,omitempty"`
	// Lua, when set, is inserted into the compiled code as is in place of the
	// quoted value, e.g. "<" for a comparison.
	Lua string `json:"lua,omitempty"`
}

func (p Param) validate() error {
	if !slices.Contains(paramTypes, p.Type) {
		return fmt.Errorf("unknown type %q", p.Type)
	}
	switch p.Type {
	case TypeEnum:
		if len(p.Options) == 0 {
			return errors.New("an enum needs options")
		}
	case TypeList:
		if p.Of == "" || p.Of == TypeList || p.Of == TypeRef || !slices.Contains(paramTypes, p.Of) {
			return fmt.Errorf("a list needs a plain element type, not %q", p.Of)
		}
	case TypeRef:
		if p.Default != nil {
			return errors.New("a reference takes a default reference, not a literal")
		}
	}
	if p.DefaultRef != "" {
		if _, err := refLua(p.DefaultRef); err != nil {
			return err
		}
	}
	if p.Default != nil {
		if _, err := literalLua(p, p.Default); err != nil {
			return fmt.Errorf("default: %w", err)
		}
	}
	return nil
}

// ParseDuration reads a span such as "90s", "15m", "2h", "3d" or "1w", or a
// plain number of seconds, and returns it in seconds.
func ParseDuration(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("empty duration")
	}
	if n, err := strconv.ParseFloat(s, 64); err == nil {
		return n, nil
	}
	unit := map[byte]float64{'d': 86400, 'w': 7 * 86400}
	if mult, ok := unit[s[len(s)-1]]; ok {
		n, err := strconv.ParseFloat(s[:len(s)-1], 64)
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q", s)
		}
		return n * mult, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q", s)
	}
	return d.Seconds(), nil
}

// ParseClock reads "HH:MM" and returns the hour and minute.
func ParseClock(s string) (int, int, error) {
	t, err := time.Parse("15:04", strings.TrimSpace(s))
	if err != nil {
		return 0, 0, fmt.Errorf("invalid time of day %q, want HH:MM", s)
	}
	return t.Hour(), t.Minute(), nil
}

// LuaString quotes s as a Lua string literal. Every byte outside printable
// ASCII is written as a decimal escape, so the result is plain ASCII and no
// input can end the literal early.
func LuaString(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"' || c == '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		case c == '\n':
			b.WriteString(`\n`)
		case c == '\t':
			b.WriteString(`\t`)
		case c < 0x20 || c >= 0x7f:
			fmt.Fprintf(&b, `\%03d`, c)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func luaNumber(f float64) (string, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return "", errors.New("not a finite number")
	}
	return strconv.FormatFloat(f, 'g', -1, 64), nil
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case int32:
		return float64(n), true
	}
	return 0, false
}

// literalLua compiles a literal value of parameter p to Lua.
func literalLua(p Param, v any) (string, error) {
	return typedLiteral(p.Type, p, v)
}

func typedLiteral(t ParamType, p Param, v any) (string, error) {
	switch t {
	case TypeString, TypeText, TypeAddress, TypeFolder, TypeIcon:
		s, ok := v.(string)
		if !ok {
			return "", fmt.Errorf("want text, got %T", v)
		}
		return LuaString(s), nil
	case TypeRegex:
		s, ok := v.(string)
		if !ok {
			return "", fmt.Errorf("want a regular expression, got %T", v)
		}
		if _, err := regexp.Compile(s); err != nil {
			return "", fmt.Errorf("invalid regular expression: %w", err)
		}
		return LuaString(s), nil
	case TypeTime:
		s, ok := v.(string)
		if !ok {
			return "", fmt.Errorf("want a time of day, got %T", v)
		}
		if _, _, err := ParseClock(s); err != nil {
			return "", err
		}
		return LuaString(s), nil
	case TypeNumber, TypeAccount:
		f, ok := toFloat(v)
		if !ok {
			return "", fmt.Errorf("want a number, got %T", v)
		}
		return luaNumber(f)
	case TypeColor:
		f, ok := toFloat(v)
		if !ok || f != math.Trunc(f) || f < 0 || f > 8 {
			return "", fmt.Errorf("want a colour from 0 to 8, got %v", v)
		}
		return luaNumber(f)
	case TypeBool:
		b, ok := v.(bool)
		if !ok {
			return "", fmt.Errorf("want true or false, got %T", v)
		}
		return strconv.FormatBool(b), nil
	case TypeDuration:
		if f, ok := toFloat(v); ok {
			return luaNumber(f)
		}
		s, ok := v.(string)
		if !ok {
			return "", fmt.Errorf("want a duration, got %T", v)
		}
		secs, err := ParseDuration(s)
		if err != nil {
			return "", err
		}
		return luaNumber(secs)
	case TypeEnum:
		s, ok := v.(string)
		if !ok {
			return "", fmt.Errorf("want one of the options, got %T", v)
		}
		i := slices.IndexFunc(p.Options, func(o Option) bool { return o.Value == s })
		if i < 0 {
			return "", fmt.Errorf("%q is not one of the options", s)
		}
		if p.Options[i].Lua != "" {
			return p.Options[i].Lua, nil
		}
		return LuaString(s), nil
	case TypeList:
		items, ok := v.([]any)
		if !ok {
			return "", fmt.Errorf("want a list, got %T", v)
		}
		parts := make([]string, 0, len(items))
		for _, item := range items {
			s, err := typedLiteral(p.Of, p, item)
			if err != nil {
				return "", err
			}
			parts = append(parts, s)
		}
		return "{" + strings.Join(parts, ", ") + "}", nil
	case TypeRef:
		return "", errors.New("needs a reference, not a literal")
	}
	return "", fmt.Errorf("unknown type %q", t)
}

var refSegment = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// refLua compiles a value reference to a Lua expression. The forms are
// "event.<path>", "var:<name>[.<path>]", "step:<block id>[.<path>]" and
// "setting:<name>".
func refLua(ref string) (string, error) {
	bad := fmt.Errorf("invalid reference %q", ref)
	path := func(head string, rest []string) (string, error) {
		out := head
		for _, seg := range rest {
			if !refSegment.MatchString(seg) {
				return "", bad
			}
			out += "." + seg
		}
		return out, nil
	}
	switch {
	case strings.HasPrefix(ref, "event.") || ref == "event":
		return path("event", strings.Split(ref, ".")[1:])
	case strings.HasPrefix(ref, "var:"):
		parts := strings.Split(strings.TrimPrefix(ref, "var:"), ".")
		if !refSegment.MatchString(parts[0]) {
			return "", bad
		}
		return path("vars["+LuaString(parts[0])+"]", parts[1:])
	case strings.HasPrefix(ref, "step:"):
		parts := strings.Split(strings.TrimPrefix(ref, "step:"), ".")
		if !blockIDPattern.MatchString(parts[0]) {
			return "", bad
		}
		return path("steps["+LuaString(parts[0])+"]", parts[1:])
	case strings.HasPrefix(ref, "setting:"):
		name := strings.TrimPrefix(ref, "setting:")
		if !refSegment.MatchString(name) {
			return "", bad
		}
		return "settings.get(" + LuaString(name) + ")", nil
	}
	return "", bad
}
