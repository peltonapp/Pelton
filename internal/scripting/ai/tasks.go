package ai

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// maxCategories and maxFields bound what one classify or extract asks.
const (
	maxCategories = 50
	maxFields     = 30
)

// classifySchema asks for one label out of the categories.
func classifySchema(categories []string) map[string]any {
	enum := make([]any, len(categories))
	for i, c := range categories {
		enum[i] = c
	}
	return map[string]any{
		"type":                 "object",
		"properties":           map[string]any{"label": map[string]any{"type": "string", "enum": enum}},
		"required":             []any{"label"},
		"additionalProperties": false,
	}
}

// matchLabel finds the category an answer names: a JSON object's "label", or
// the plain text, ignoring case, quotes and a closing full stop. It reports
// false for anything that is not exactly one of the categories.
func matchLabel(answer string, categories []string) (string, bool) {
	s := strings.TrimSpace(answer)
	var obj struct {
		Label string `json:"label"`
	}
	if json.Unmarshal([]byte(stripFence(s)), &obj) == nil && obj.Label != "" {
		s = obj.Label
	}
	s = strings.TrimSpace(strings.Trim(strings.TrimSpace(s), "\"'`*."))
	for _, c := range categories {
		if strings.EqualFold(s, strings.TrimSpace(c)) {
			return c, true
		}
	}
	return "", false
}

// stripFence removes a Markdown code fence a model may put around JSON.
func stripFence(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	s = strings.TrimPrefix(s, "```")
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "```"))
}

// The field types extract knows.
const (
	FieldText   = "text"
	FieldNumber = "number"
	FieldDate   = "date"
	FieldYesNo  = "yes_no"
)

// FieldSpec is one field extract asks for.
type FieldSpec struct {
	Name string
	Type string
}

var fieldName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)

// fieldType reads a field type, with the obvious other spellings.
func fieldType(s string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "text", "string":
		return FieldText, true
	case "number", "num", "float", "int", "integer":
		return FieldNumber, true
	case "date", "time", "datetime":
		return FieldDate, true
	case "yes_no", "yesno", "yes/no", "bool", "boolean":
		return FieldYesNo, true
	}
	return "", false
}

// ParseFields reads the fields extract asks for: a table of name to type, or
// a list of "name" or "name:type" entries.
func ParseFields(v any) ([]FieldSpec, error) {
	var out []FieldSpec
	add := func(name, typ string) error {
		name = strings.TrimSpace(name)
		if !fieldName.MatchString(name) {
			return fmt.Errorf("%q is not a field name; use letters, digits and _", name)
		}
		t, ok := fieldType(typ)
		if !ok {
			return fmt.Errorf("field %q has the unknown type %q; use text, number, date or yes_no", name, typ)
		}
		for _, f := range out {
			if f.Name == name {
				return fmt.Errorf("field %q is named twice", name)
			}
		}
		out = append(out, FieldSpec{Name: name, Type: t})
		return nil
	}
	switch x := v.(type) {
	case []any:
		for _, item := range x {
			s, ok := item.(string)
			if !ok {
				return nil, errors.New("each field is text such as \"amount:number\"")
			}
			name, typ, _ := strings.Cut(s, ":")
			if err := add(name, typ); err != nil {
				return nil, err
			}
		}
	case map[string]any:
		for _, name := range sortedKeys(x) {
			typ, _ := x[name].(string)
			if err := add(name, typ); err != nil {
				return nil, err
			}
		}
	case string:
		for _, s := range strings.Split(x, ",") {
			if strings.TrimSpace(s) == "" {
				continue
			}
			name, typ, _ := strings.Cut(s, ":")
			if err := add(name, typ); err != nil {
				return nil, err
			}
		}
	}
	if len(out) == 0 {
		return nil, errors.New("name at least one field to extract")
	}
	if len(out) > maxFields {
		return nil, fmt.Errorf("at most %d fields", maxFields)
	}
	return out, nil
}

// extractSchema asks for an object with every field, null where the text has
// no value for it.
func extractSchema(fields []FieldSpec) map[string]any {
	props := map[string]any{}
	required := make([]any, 0, len(fields))
	for _, f := range fields {
		var p map[string]any
		switch f.Type {
		case FieldNumber:
			p = map[string]any{"type": []any{"number", "null"}}
		case FieldYesNo:
			p = map[string]any{"type": []any{"boolean", "null"}}
		case FieldDate:
			p = map[string]any{"type": []any{"string", "null"}, "description": "a date as YYYY-MM-DD, or YYYY-MM-DD HH:MM with a time"}
		default:
			p = map[string]any{"type": []any{"string", "null"}}
		}
		props[f.Name] = p
		required = append(required, f.Name)
	}
	return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
}

// describeFields lists the fields for the prompt.
func describeFields(fields []FieldSpec) string {
	var b strings.Builder
	for _, f := range fields {
		hint := map[string]string{
			FieldText:   "text",
			FieldNumber: "a number",
			FieldDate:   "a date as YYYY-MM-DD, or YYYY-MM-DD HH:MM with a time",
			FieldYesNo:  "true or false",
		}[f.Type]
		fmt.Fprintf(&b, "- %s: %s\n", f.Name, hint)
	}
	return b.String()
}

// CheckExtracted validates a model's answer against the fields: a JSON
// object whose values have the asked types. A missing or null field is left
// out of the result; a field nobody asked for is dropped. The errors say
// what was wrong, to tell the model on a retry.
func CheckExtracted(answer string, fields []FieldSpec) (map[string]any, error) {
	var raw map[string]any
	if err := json.Unmarshal([]byte(stripFence(answer)), &raw); err != nil {
		return nil, errors.New("the answer is not a JSON object")
	}
	out := map[string]any{}
	var problems []string
	for _, f := range fields {
		v, ok := raw[f.Name]
		if !ok || v == nil {
			continue
		}
		got, err := convertField(f.Type, v)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", f.Name, err))
			continue
		}
		out[f.Name] = got
	}
	if len(problems) > 0 {
		return nil, errors.New(strings.Join(problems, "; "))
	}
	return out, nil
}

var dateLayouts = []string{"2006-01-02", "2006-01-02 15:04", "2006-01-02T15:04", "2006-01-02T15:04:05Z07:00", "2006-01-02T15:04:05"}

func convertField(typ string, v any) (any, error) {
	switch typ {
	case FieldNumber:
		switch n := v.(type) {
		case float64:
			return n, nil
		case string:
			f, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(n), ",", ""), 64)
			if err != nil {
				return nil, fmt.Errorf("%q is not a number", n)
			}
			return f, nil
		}
		return nil, errors.New("want a number")
	case FieldYesNo:
		switch b := v.(type) {
		case bool:
			return b, nil
		case string:
			switch strings.ToLower(strings.TrimSpace(b)) {
			case "yes", "true":
				return true, nil
			case "no", "false":
				return false, nil
			}
		}
		return nil, errors.New("want true or false")
	case FieldDate:
		s, ok := v.(string)
		if !ok {
			return nil, errors.New("want a date as YYYY-MM-DD")
		}
		s = strings.TrimSpace(s)
		for _, layout := range dateLayouts {
			if t, err := time.Parse(layout, s); err == nil {
				if layout == "2006-01-02" {
					return t.Format("2006-01-02"), nil
				}
				return t.Format("2006-01-02 15:04"), nil
			}
		}
		return nil, fmt.Errorf("%q is not a date as YYYY-MM-DD", s)
	default:
		switch x := v.(type) {
		case string:
			return x, nil
		case float64:
			return strconv.FormatFloat(x, 'f', -1, 64), nil
		case bool:
			return strconv.FormatBool(x), nil
		}
		return nil, errors.New("want text")
	}
}

// summaryLength is how long each summary length asks the summary to be, and
// the output cap that goes with it.
var summaryLength = map[string]struct {
	ask    string
	tokens int
}{
	"short":  {"in one or two sentences", 512},
	"medium": {"in one paragraph", 1024},
	"long":   {"in a few paragraphs, covering every point that matters", 2048},
}
