// Package ical reads and writes iCalendar (RFC 5545) data: the component tree,
// the VEVENT fields a calendar shows and edits, recurrence rules, time zones
// and the expansion of a recurring event into occurrences.
//
// Parsing keeps every component and property in order, including the ones
// Pelton has no field for, so an event written by another client survives an
// edit here with its X- properties, its attachments and its conferencing
// links intact. Writing an Event back only touches the properties it owns.
package ical

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// MIMEType is the media type of iCalendar data.
const MIMEType = "text/calendar"

// Component is one BEGIN/END block: VCALENDAR, VEVENT, VALARM, VTIMEZONE.
type Component struct {
	Name     string
	Props    []*Prop
	Children []*Component
}

// Prop is one content line.
type Prop struct {
	Name   string
	Params []Param
	// Value is the raw value as written on the wire, escapes included. Text
	// values are read through Text and written through SetText.
	Value string
}

// Param is one property parameter. Values keeps the list a parameter like
// MEMBER or DELEGATED-TO can carry.
type Param struct {
	Name   string
	Values []string
}

// Param returns the first value of a parameter, or "".
func (p *Prop) Param(name string) string {
	for _, param := range p.Params {
		if strings.EqualFold(param.Name, name) && len(param.Values) > 0 {
			return param.Values[0]
		}
	}
	return ""
}

// SetParam replaces a parameter, removing it when value is empty.
func (p *Prop) SetParam(name, value string) {
	out := p.Params[:0]
	for _, param := range p.Params {
		if !strings.EqualFold(param.Name, name) {
			out = append(out, param)
		}
	}
	p.Params = out
	if value != "" {
		p.Params = append(p.Params, Param{Name: strings.ToUpper(name), Values: []string{value}})
	}
}

// Text returns the value with TEXT escapes resolved.
func (p *Prop) Text() string {
	return unescapeText(p.Value)
}

// Prop returns the first property with the given name, or nil.
func (c *Component) Prop(name string) *Prop {
	for _, p := range c.Props {
		if strings.EqualFold(p.Name, name) {
			return p
		}
	}
	return nil
}

// PropsNamed returns every property with the given name, in order.
func (c *Component) PropsNamed(name string) []*Prop {
	var out []*Prop
	for _, p := range c.Props {
		if strings.EqualFold(p.Name, name) {
			out = append(out, p)
		}
	}
	return out
}

// Text returns the unescaped text of the first property with the name.
func (c *Component) Text(name string) string {
	if p := c.Prop(name); p != nil {
		return p.Text()
	}
	return ""
}

// Remove drops every property with the given name.
func (c *Component) Remove(name string) {
	out := c.Props[:0]
	for _, p := range c.Props {
		if !strings.EqualFold(p.Name, name) {
			out = append(out, p)
		}
	}
	c.Props = out
}

// Set replaces every property with the name by one carrying the raw value,
// keeping the position of the first one so a rewrite does not reorder the
// component. An empty value removes the property.
func (c *Component) Set(name, value string, params ...Param) {
	if value == "" {
		c.Remove(name)
		return
	}
	prop := &Prop{Name: strings.ToUpper(name), Params: params, Value: value}
	for i, p := range c.Props {
		if strings.EqualFold(p.Name, name) {
			c.Props[i] = prop
			rest := c.Props[i+1:]
			kept := c.Props[:i+1]
			for _, q := range rest {
				if !strings.EqualFold(q.Name, name) {
					kept = append(kept, q)
				}
			}
			c.Props = kept
			return
		}
	}
	c.Props = append(c.Props, prop)
}

// SetText is Set for a TEXT value, escaping it.
func (c *Component) SetText(name, value string) {
	c.Set(name, escapeText(value))
}

// Add appends a property.
func (c *Component) Add(p *Prop) {
	c.Props = append(c.Props, p)
}

// ChildrenNamed returns the direct children with the given name.
func (c *Component) ChildrenNamed(name string) []*Component {
	var out []*Component
	for _, child := range c.Children {
		if strings.EqualFold(child.Name, name) {
			out = append(out, child)
		}
	}
	return out
}

// RemoveChildren drops the direct children with the given name.
func (c *Component) RemoveChildren(name string) {
	out := c.Children[:0]
	for _, child := range c.Children {
		if !strings.EqualFold(child.Name, name) {
			out = append(out, child)
		}
	}
	c.Children = out
}

// Clone returns a deep copy.
func (c *Component) Clone() *Component {
	out := &Component{Name: c.Name}
	for _, p := range c.Props {
		cp := &Prop{Name: p.Name, Value: p.Value}
		for _, param := range p.Params {
			cp.Params = append(cp.Params, Param{Name: param.Name, Values: append([]string(nil), param.Values...)})
		}
		out.Props = append(out.Props, cp)
	}
	for _, child := range c.Children {
		out.Children = append(out.Children, child.Clone())
	}
	return out
}

// maxLine caps one unfolded content line. A legitimate line, even one carrying
// an inline attachment, stays well under this.
const maxLine = 16 << 20

// Parse reads every top-level component in r. A stream normally holds one
// VCALENDAR, but an .ics export can concatenate several.
func Parse(r io.Reader) ([]*Component, error) {
	lines, err := unfold(r)
	if err != nil {
		return nil, err
	}
	var roots []*Component
	var stack []*Component
	for n, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		prop, err := parseLine(line)
		if err != nil {
			return nil, fmt.Errorf("ical: line %d: %w", n+1, err)
		}
		switch strings.ToUpper(prop.Name) {
		case "BEGIN":
			comp := &Component{Name: strings.ToUpper(strings.TrimSpace(prop.Value))}
			if len(stack) > 0 {
				parent := stack[len(stack)-1]
				parent.Children = append(parent.Children, comp)
			} else {
				roots = append(roots, comp)
			}
			stack = append(stack, comp)
		case "END":
			name := strings.ToUpper(strings.TrimSpace(prop.Value))
			if len(stack) == 0 || stack[len(stack)-1].Name != name {
				return nil, fmt.Errorf("ical: line %d: END:%s does not close an open component", n+1, name)
			}
			stack = stack[:len(stack)-1]
		default:
			if len(stack) == 0 {
				return nil, fmt.Errorf("ical: line %d: property outside a component", n+1)
			}
			cur := stack[len(stack)-1]
			cur.Props = append(cur.Props, prop)
		}
	}
	if len(stack) > 0 {
		return nil, fmt.Errorf("ical: %s is never closed", stack[len(stack)-1].Name)
	}
	if len(roots) == 0 {
		return nil, errors.New("ical: no calendar data")
	}
	return roots, nil
}

// ParseCalendar reads a single VCALENDAR. Several concatenated calendars are
// merged into the first, which is what an import of an exported file wants.
func ParseCalendar(r io.Reader) (*Component, error) {
	roots, err := Parse(r)
	if err != nil {
		return nil, err
	}
	var cal *Component
	for _, root := range roots {
		if root.Name != "VCALENDAR" {
			continue
		}
		if cal == nil {
			cal = root
			continue
		}
		cal.Children = append(cal.Children, root.Children...)
	}
	if cal == nil {
		return nil, errors.New("ical: no VCALENDAR")
	}
	return cal, nil
}

// unfold joins continuation lines. CRLF and bare LF both end a line, since
// plenty of producers get the line ending wrong.
func unfold(r io.Reader) ([]string, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), maxLine)
	var lines []string
	for sc.Scan() {
		line := strings.TrimSuffix(sc.Text(), "\r")
		if len(lines) == 0 {
			line = strings.TrimPrefix(line, string(rune(0xFEFF)))
		}
		if (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) && len(lines) > 0 {
			lines[len(lines)-1] += line[1:]
			continue
		}
		lines = append(lines, line)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("ical: read: %w", err)
	}
	return lines, nil
}

// parseLine splits name;param=value:value, honouring quoted parameter values
// which may contain ':' and ';'.
func parseLine(line string) (*Prop, error) {
	prop := &Prop{}
	i := strings.IndexAny(line, ";:")
	if i <= 0 {
		return nil, fmt.Errorf("no property name in %q", truncate(line))
	}
	prop.Name = strings.ToUpper(line[:i])
	for line[i] == ';' {
		i++
		eq := strings.IndexByte(line[i:], '=')
		if eq < 0 {
			return nil, fmt.Errorf("parameter without a value in %q", truncate(line))
		}
		param := Param{Name: strings.ToUpper(line[i : i+eq])}
		i += eq + 1
		for {
			var value string
			if i < len(line) && line[i] == '"' {
				end := strings.IndexByte(line[i+1:], '"')
				if end < 0 {
					return nil, fmt.Errorf("unterminated quote in %q", truncate(line))
				}
				value = line[i+1 : i+1+end]
				i += end + 2
			} else {
				end := strings.IndexAny(line[i:], ",;:")
				if end < 0 {
					return nil, fmt.Errorf("no value in %q", truncate(line))
				}
				value = line[i : i+end]
				i += end
			}
			param.Values = append(param.Values, unescapeParam(value))
			if i >= len(line) {
				return nil, fmt.Errorf("no value in %q", truncate(line))
			}
			if line[i] != ',' {
				break
			}
			i++
		}
		prop.Params = append(prop.Params, param)
	}
	if line[i] != ':' {
		return nil, fmt.Errorf("no value in %q", truncate(line))
	}
	prop.Value = line[i+1:]
	return prop, nil
}

func truncate(s string) string {
	if len(s) > 40 {
		return s[:40] + "..."
	}
	return s
}

// Encode writes components as iCalendar text with CRLF line endings, folding
// lines at 75 octets without splitting a UTF-8 sequence.
func Encode(w io.Writer, comps ...*Component) error {
	bw := bufio.NewWriter(w)
	for _, c := range comps {
		encodeComponent(bw, c)
	}
	return bw.Flush()
}

// String renders one component.
func (c *Component) String() string {
	var b strings.Builder
	_ = Encode(&b, c)
	return b.String()
}

func encodeComponent(w *bufio.Writer, c *Component) {
	writeFolded(w, "BEGIN:"+c.Name)
	for _, p := range c.Props {
		writeFolded(w, encodeProp(p))
	}
	for _, child := range c.Children {
		encodeComponent(w, child)
	}
	writeFolded(w, "END:"+c.Name)
}

func encodeProp(p *Prop) string {
	var b strings.Builder
	b.WriteString(p.Name)
	for _, param := range p.Params {
		b.WriteByte(';')
		b.WriteString(param.Name)
		b.WriteByte('=')
		for i, v := range param.Values {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(quoteParam(v))
		}
	}
	b.WriteByte(':')
	b.WriteString(p.Value)
	return b.String()
}

func writeFolded(w *bufio.Writer, line string) {
	const limit = 75
	first := true
	for len(line) > 0 {
		max := limit
		if !first {
			max = limit - 1
		}
		if len(line) <= max {
			if !first {
				w.WriteByte(' ')
			}
			w.WriteString(line)
			break
		}
		cut := max
		for cut > 0 && !utf8.RuneStart(line[cut]) {
			cut--
		}
		if !first {
			w.WriteByte(' ')
		}
		w.WriteString(line[:cut])
		w.WriteString("\r\n")
		line = line[cut:]
		first = false
	}
	w.WriteString("\r\n")
}

// quoteParam quotes a parameter value that contains a character with meaning
// in the content line. Double quotes and newlines use RFC 6868's caret
// escapes, since a quoted value cannot hold either.
func quoteParam(v string) string {
	v = strings.NewReplacer("^", "^^", "\n", "^n", `"`, "^'").Replace(v)
	if strings.ContainsAny(v, ":;,") {
		return `"` + v + `"`
	}
	return v
}

func unescapeParam(v string) string {
	if !strings.Contains(v, "^") {
		return v
	}
	return strings.NewReplacer("^^", "^", "^n", "\n", "^'", `"`).Replace(v)
}

// escapeText escapes a TEXT value.
func escapeText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.NewReplacer(`\`, `\\`, ";", `\;`, ",", `\,`, "\n", `\n`).Replace(s)
}

// unescapeText resolves TEXT escapes. An unknown escape keeps the character
// after the backslash, which is what most readers do with the "\:" some
// producers emit.
func unescapeText(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i == len(s)-1 {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 'n', 'N':
			b.WriteByte('\n')
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// splitList splits a comma separated value, honouring escaped commas in TEXT.
func splitList(raw string) []string {
	var out []string
	var cur strings.Builder
	for i := 0; i < len(raw); i++ {
		if raw[i] == '\\' && i+1 < len(raw) {
			cur.WriteByte(raw[i])
			cur.WriteByte(raw[i+1])
			i++
			continue
		}
		if raw[i] == ',' {
			out = append(out, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteByte(raw[i])
	}
	out = append(out, cur.String())
	return out
}
