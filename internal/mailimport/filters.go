package mailimport

import (
	"bufio"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/peltonapp/Pelton/internal/scripting"
)

// Thunderbird keeps each account's message filters in msgFilterRules.dat in
// that account's mail directory: a flat list of key="value" lines, where a
// name= line starts the next filter. They become Pelton automations, always
// created switched off for the user to look over; whatever has no Pelton
// counterpart is named in the report rather than guessed at.

// Filter is one Thunderbird message filter.
type Filter struct {
	Key     string
	Name    string
	Account string
	Enabled bool
	// Type is Thunderbird's bitmask of when the filter runs.
	Type      int
	Condition string
	Actions   []FilterAction
}

// FilterAction is one thing a filter does, with its value.
type FilterAction struct {
	Type  string
	Value string
}

// Thunderbird's filter type bits.
const (
	tbFilterIncoming   = 0x1
	tbFilterManual     = 0x10
	tbFilterPostPlugin = 0x20
)

// readFilterRules parses one msgFilterRules.dat.
func readFilterRules(path, account string) ([]Filter, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parseFilterRules(io.LimitReader(f, maxTextFile), path, account)
}

func parseFilterRules(r io.Reader, origin, account string) ([]Filter, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4<<20)
	var (
		out []Filter
		cur *Filter
	)
	for sc.Scan() {
		key, value, ok := filterLine(sc.Text())
		if !ok {
			continue
		}
		if key == "name" {
			out = append(out, Filter{Key: fmt.Sprintf("filter:%s:%d", origin, len(out)), Name: value, Account: account})
			cur = &out[len(out)-1]
			continue
		}
		if cur == nil {
			continue
		}
		switch key {
		case "enabled":
			cur.Enabled = value == "yes"
		case "type":
			cur.Type, _ = strconv.Atoi(value)
		case "condition":
			cur.Condition = value
		case "action":
			cur.Actions = append(cur.Actions, FilterAction{Type: value})
		case "actionValue":
			if n := len(cur.Actions); n > 0 {
				cur.Actions[n-1].Value = value
			}
		}
	}
	return out, sc.Err()
}

// filterLine reads one key="value" line, undoing the \" and \\ escapes.
func filterLine(line string) (string, string, bool) {
	key, rest, ok := strings.Cut(strings.TrimSpace(line), "=")
	if !ok || len(rest) < 2 || rest[0] != '"' || rest[len(rest)-1] != '"' {
		return "", "", false
	}
	inner := rest[1 : len(rest)-1]
	var b strings.Builder
	for i := 0; i < len(inner); i++ {
		if inner[i] == '\\' && i+1 < len(inner) {
			i++
		}
		b.WriteByte(inner[i])
	}
	return key, b.String(), true
}

// FilterMapping is a filter turned into an automation, or the reason it
// could not be.
type FilterMapping struct {
	Tree    scripting.Tree
	Trigger string
	// Dropped lists the parts of the filter left out, in Thunderbird's own
	// words, for the report.
	Dropped []string
	// Unmappable is set when the filter cannot become an automation at all.
	Unmappable bool
}

// MapFilter turns a Thunderbird filter into a block script. accountID, when
// not 0, limits it to the Pelton account the filter's account became.
func MapFilter(f Filter, accountID int64) FilterMapping {
	var m FilterMapping
	switch {
	case f.Type&(tbFilterIncoming|tbFilterPostPlugin) != 0:
		m.Trigger = scripting.TriggerMailArrived
	case f.Type&tbFilterManual != 0:
		m.Trigger = scripting.TriggerManual
	default:
		m.Dropped = append(m.Dropped, "type="+strconv.Itoa(f.Type))
		m.Unmappable = true
		return m
	}
	cond, ok, problem := mapCondition(f.Condition)
	if !ok {
		m.Dropped = append(m.Dropped, problem)
		m.Unmappable = true
		return m
	}
	var body []scripting.Block
	for i, a := range f.Actions {
		block, ok := mapAction(a, fmt.Sprintf("a%d", i+1))
		if !ok {
			text := a.Type
			if a.Value != "" {
				text += ": " + a.Value
			}
			m.Dropped = append(m.Dropped, text)
			continue
		}
		body = append(body, block)
	}
	if len(body) == 0 {
		m.Unmappable = true
		return m
	}
	if m.Trigger == scripting.TriggerManual {
		accountID = 0
	}
	if accountID != 0 {
		account := scripting.Cond{Kind: scripting.CondCondition, Type: "mail.account",
			Args: map[string]scripting.Arg{"account": {Value: float64(accountID)}}}
		if cond == nil {
			cond = &account
		} else {
			cond = &scripting.Cond{Kind: scripting.CondAll, Items: []scripting.Cond{account, *cond}}
		}
	}
	if cond == nil {
		m.Tree = scripting.Tree{Version: scripting.TreeVersion, Blocks: body}
		return m
	}
	m.Tree = scripting.Tree{Version: scripting.TreeVersion, Blocks: []scripting.Block{{
		ID: "if1", Kind: scripting.BlockIf,
		Branches: []scripting.Branch{{When: *cond, Body: body}},
	}}}
	return m
}

// mapCondition reads Thunderbird's condition string ("AND (from,contains,x)
// AND (subject,is,y)", or "ALL") into one condition. A nil condition with ok
// means every message matches.
func mapCondition(text string) (*scripting.Cond, bool, string) {
	text = strings.TrimSpace(text)
	if text == "" || strings.EqualFold(text, "ALL") {
		return nil, true, ""
	}
	terms, joins, err := splitTerms(text)
	if err != nil || len(terms) == 0 {
		return nil, false, "condition: " + text
	}
	for _, j := range joins[1:] {
		if j != joins[0] {
			return nil, false, "condition mixes AND and OR: " + text
		}
	}
	var items []scripting.Cond
	for _, t := range terms {
		c, ok := mapTerm(t)
		if !ok {
			return nil, false, "condition: " + strings.Join(t[:], ",")
		}
		items = append(items, c)
	}
	if len(items) == 1 {
		return &items[0], true, ""
	}
	kind := scripting.CondAll
	if joins[0] == "OR" {
		kind = scripting.CondAny
	}
	return &scripting.Cond{Kind: kind, Items: items}, true, ""
}

// splitTerms cuts a condition string into its (attribute,operator,value)
// terms and the AND or OR before each. A value may be quoted to hold commas
// and parentheses.
func splitTerms(text string) ([][3]string, []string, error) {
	var (
		terms [][3]string
		joins []string
	)
	i := 0
	for i < len(text) {
		for i < len(text) && text[i] == ' ' {
			i++
		}
		if i >= len(text) {
			break
		}
		join := ""
		for _, w := range []string{"AND", "OR"} {
			if strings.HasPrefix(text[i:], w) {
				join = w
				i += len(w)
				break
			}
		}
		if join == "" {
			return nil, nil, fmt.Errorf("expected AND or OR at %d", i)
		}
		for i < len(text) && text[i] == ' ' {
			i++
		}
		if i >= len(text) || text[i] != '(' {
			return nil, nil, fmt.Errorf("expected ( at %d", i)
		}
		i++
		var parts []string
		var cur strings.Builder
		quoted := false
		for ; i < len(text); i++ {
			c := text[i]
			switch {
			case c == '\\' && i+1 < len(text):
				i++
				cur.WriteByte(text[i])
			case c == '"':
				quoted = !quoted
			case c == ',' && !quoted && len(parts) < 2:
				parts = append(parts, cur.String())
				cur.Reset()
			case c == ')' && !quoted:
				parts = append(parts, cur.String())
				goto closed
			default:
				cur.WriteByte(c)
			}
		}
		return nil, nil, fmt.Errorf("unclosed term")
	closed:
		i++
		if len(parts) != 3 {
			return nil, nil, fmt.Errorf("term with %d parts", len(parts))
		}
		terms = append(terms, [3]string{strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), parts[2]})
		joins = append(joins, join)
	}
	return terms, joins, nil
}

// tbBuiltinAttr are Thunderbird's own one-word search attributes with no
// Pelton counterpart, so they are not read as header names.
var tbBuiltinAttr = map[string]bool{
	"date": true, "priority": true, "age": true, "tag": true, "keywords": true, "junk": true,
	"label": true, "bcc": true,
}

func textCond(typ, op, value string, extra map[string]scripting.Arg) scripting.Cond {
	args := map[string]scripting.Arg{"op": {Value: op}, "value": {Value: value}}
	for k, v := range extra {
		args[k] = v
	}
	return scripting.Cond{Kind: scripting.CondCondition, Type: typ, Args: args}
}

func negate(c scripting.Cond) scripting.Cond {
	return scripting.Cond{Kind: scripting.CondNot, Items: []scripting.Cond{c}}
}

// mapTerm maps one term onto a registered condition.
func mapTerm(t [3]string) (scripting.Cond, bool) {
	attr, op, value := strings.ToLower(t[0]), strings.ToLower(t[1]), t[2]
	var (
		typ   string
		extra map[string]scripting.Arg
	)
	switch {
	case attr == "from":
		typ = "mail.from"
	case attr == "to", attr == "cc", attr == "to or cc":
		typ = "mail.to"
	case attr == "subject":
		typ = "mail.subject"
	case attr == "body":
		typ = "mail.body"
	case attr == "size":
		kb, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil {
			return scripting.Cond{}, false
		}
		cmp := map[string]string{"is greater than": "gt", "is less than": "lt"}[op]
		if cmp == "" {
			return scripting.Cond{}, false
		}
		return scripting.Cond{Kind: scripting.CondCondition, Type: "mail.size",
			Args: map[string]scripting.Arg{"cmp": {Value: cmp}, "kb": {Value: kb}}}, true
	case attr == "has attachment status":
		c := scripting.Cond{Kind: scripting.CondCondition, Type: "mail.has_attachment"}
		yes := strings.EqualFold(strings.TrimSpace(value), "true")
		switch op {
		case "is":
		case "isn't":
			yes = !yes
		default:
			return scripting.Cond{}, false
		}
		if !yes {
			return negate(c), true
		}
		return c, true
	case attr == "status":
		var typ string
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "read":
			typ = "mail.is_read"
		case "flagged":
			typ = "mail.is_flagged"
		default:
			return scripting.Cond{}, false
		}
		state := map[string]string{"is": "on", "isn't": "off"}[op]
		if state == "" {
			return scripting.Cond{}, false
		}
		return scripting.Cond{Kind: scripting.CondCondition, Type: typ,
			Args: map[string]scripting.Arg{"state": {Value: state}}}, true
	case attr != "" && !strings.ContainsAny(attr, " \t") && !tbBuiltinAttr[attr]:
		// any other single word is a custom header, which Thunderbird
		// writes by its name.
		typ = "mail.header"
		extra = map[string]scripting.Arg{"name": {Value: strings.Trim(t[0], `"`)}}
	default:
		return scripting.Cond{}, false
	}
	switch op {
	case "contains":
		return textCond(typ, scripting.OpContains, value, extra), true
	case "doesn't contain":
		return negate(textCond(typ, scripting.OpContains, value, extra)), true
	case "is":
		return textCond(typ, scripting.OpEquals, value, extra), true
	case "isn't":
		return negate(textCond(typ, scripting.OpEquals, value, extra)), true
	case "begins with":
		return textCond(typ, scripting.OpStarts, value, extra), true
	case "ends with":
		return textCond(typ, scripting.OpEnds, value, extra), true
	}
	return scripting.Cond{}, false
}

func actionBlock(id, typ string, args map[string]scripting.Arg) scripting.Block {
	return scripting.Block{ID: id, Kind: scripting.BlockAction, Type: typ, Args: args}
}

// mapAction maps one filter action onto a registered action.
func mapAction(a FilterAction, id string) (scripting.Block, bool) {
	on := func(v bool) map[string]scripting.Arg { return map[string]scripting.Arg{"on": {Value: v}} }
	switch a.Type {
	case "Move to folder", "Copy to folder":
		folder, ok := folderFromURI(a.Value)
		if !ok {
			return scripting.Block{}, false
		}
		typ := "mail.move"
		if a.Type == "Copy to folder" {
			typ = "mail.copy"
		}
		return actionBlock(id, typ, map[string]scripting.Arg{"folder": {Value: folder}}), true
	case "Mark read":
		return actionBlock(id, "mail.mark_read", on(true)), true
	case "Mark unread":
		return actionBlock(id, "mail.mark_read", on(false)), true
	case "Mark flagged":
		return actionBlock(id, "mail.flag", on(true)), true
	case "Mark unflagged":
		return actionBlock(id, "mail.flag", on(false)), true
	case "Delete":
		return actionBlock(id, "mail.delete", nil), true
	case "Stop execution":
		return scripting.Block{ID: id, Kind: scripting.BlockStop}, true
	}
	return scripting.Block{}, false
}

// folderFromURI reads the folder path out of a Thunderbird folder URI such
// as imap://me%40example.org@imap.example.org/INBOX/News. Folders of Local
// Folders (mailbox://nobody@Local%20Folders/...) live in another account
// than the mail the filter runs on, which an automation cannot move to.
func folderFromURI(uri string) (string, bool) {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "imap" {
		return "", false
	}
	path := strings.Trim(u.Path, "/")
	return path, path != ""
}
