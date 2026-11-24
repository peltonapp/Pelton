package desktop

import (
	"bytes"
	"encoding/json"
	"errors"
	"regexp"
)

// the most bytes of icon geometry a project stores, and the most nodes in it.
// the longest icon in the tabler set is about 1.4 KB.
const (
	maxProjectIconBytes = 4096
	maxProjectIconNodes = 32
)

var errProjectIconNodes = errors.New("pelton: a project icon is a short list of svg shapes")

// projectIconTags are the svg shapes an icon may draw.
var projectIconTags = map[string]bool{
	"path": true, "circle": true, "ellipse": true, "rect": true,
	"line": true, "polyline": true, "polygon": true,
}

var (
	iconPathData = regexp.MustCompile(`^[0-9a-zA-Z .,+-]*$`)
	iconNumber   = regexp.MustCompile(`^-?[0-9]*\.?[0-9]+$`)
	iconPoints   = regexp.MustCompile(`^[0-9 .,+-]*$`)
)

// projectIconAttrs maps each attribute an icon may carry to what its value
// must look like. Attributes outside this list, event handlers included, are
// refused.
var projectIconAttrs = map[string]func(string) bool{
	"d":               iconPathData.MatchString,
	"points":          iconPoints.MatchString,
	"cx":              iconNumber.MatchString,
	"cy":              iconNumber.MatchString,
	"r":               iconNumber.MatchString,
	"rx":              iconNumber.MatchString,
	"ry":              iconNumber.MatchString,
	"x":               iconNumber.MatchString,
	"y":               iconNumber.MatchString,
	"x1":              iconNumber.MatchString,
	"y1":              iconNumber.MatchString,
	"x2":              iconNumber.MatchString,
	"y2":              iconNumber.MatchString,
	"width":           iconNumber.MatchString,
	"height":          iconNumber.MatchString,
	"opacity":         iconNumber.MatchString,
	"stroke-width":    iconNumber.MatchString,
	"fill":            iconOneOf("none", "currentColor"),
	"stroke":          iconOneOf("none", "currentColor"),
	"stroke-linecap":  iconOneOf("round", "butt", "square"),
	"stroke-linejoin": iconOneOf("round", "miter", "bevel"),
}

func iconOneOf(values ...string) func(string) bool {
	return func(s string) bool {
		for _, v := range values {
			if s == v {
				return true
			}
		}
		return false
	}
}

// checkProjectIconNodes returns the icon geometry in its compact form, or
// errProjectIconNodes when it is not a json array of [tag, attributes] nodes
// made only of the shapes and attributes tabler icons use. The geometry ends
// up as svg in the page, so anything else is refused rather than cleaned. An
// empty string is no geometry.
func checkProjectIconNodes(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	if len(raw) > maxProjectIconBytes {
		return "", errProjectIconNodes
	}
	var nodes []json.RawMessage
	if err := decodeStrict(raw, &nodes); err != nil || len(nodes) == 0 || len(nodes) > maxProjectIconNodes {
		return "", errProjectIconNodes
	}
	for _, n := range nodes {
		var pair []json.RawMessage
		if err := decodeStrict(string(n), &pair); err != nil || len(pair) != 2 {
			return "", errProjectIconNodes
		}
		var tag string
		var attrs map[string]any
		if decodeStrict(string(pair[0]), &tag) != nil || !projectIconTags[tag] {
			return "", errProjectIconNodes
		}
		if decodeStrict(string(pair[1]), &attrs) != nil || len(attrs) == 0 {
			return "", errProjectIconNodes
		}
		for name, value := range attrs {
			valid, ok := projectIconAttrs[name]
			if !ok {
				return "", errProjectIconNodes
			}
			var text string
			switch v := value.(type) {
			case string:
				text = v
			case json.Number:
				text = v.String()
			default:
				return "", errProjectIconNodes
			}
			if !valid(text) {
				return "", errProjectIconNodes
			}
		}
	}
	var out bytes.Buffer
	if err := json.Compact(&out, []byte(raw)); err != nil {
		return "", errProjectIconNodes
	}
	return out.String(), nil
}

// decodeStrict decodes one json value and nothing after it, keeping numbers
// as json.Number.
func decodeStrict(raw string, v any) error {
	dec := json.NewDecoder(bytes.NewReader([]byte(raw)))
	dec.UseNumber()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return errProjectIconNodes
	}
	return nil
}
