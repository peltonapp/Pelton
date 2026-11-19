// Package sieve models the part of the Sieve mail filtering language (RFC
// 5228) that Pelton edits: rules made of header, address, envelope, size and
// exists tests combined with allof, anyof and not, running fileinto,
// redirect, keep, discard, stop and addflag (RFC 5232); an out of office reply
// with the vacation extension (RFC 5230) bounded by start and end days with
// the date extension (RFC 5260); and includes of other scripts (RFC 6609).
//
// Parse only accepts a script in exactly the form Generate writes. Anything
// else, a script another program wrote or one edited by hand, is reported as
// ErrForeign and is never rewritten: the caller shows it as text instead.
package sieve

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Header is the first line of every script Pelton writes.
const Header = "# Pelton sieve script. Change it in Pelton's settings, or replace it as a whole."

const (
	vacationComment = "# Pelton: out of office"
	ruleComment     = "# Pelton rule: "
	includeComment  = "# Pelton: other script"
)

// ErrForeign means the script is not one Pelton can edit without changing
// what it says.
var ErrForeign = errors.New("sieve: the script is not in the form Pelton writes")

// Extensions the model can require.
const (
	ExtFileInto   = "fileinto"
	ExtIMAP4Flags = "imap4flags"
	ExtVacation   = "vacation"
	ExtDate       = "date"
	ExtRelational = "relational"
	ExtEnvelope   = "envelope"
	ExtInclude    = "include"
)

// Condition fields.
const (
	FieldFrom    = "from"
	FieldTo      = "to"
	FieldSubject = "subject"
	FieldHeader  = "header"
	FieldSize    = "size"
	FieldEnvFrom = "envfrom"
	FieldEnvTo   = "envto"
)

// Condition operators. Over and under apply to size, exists to a header.
const (
	OpContains = "contains"
	OpIs       = "is"
	OpMatches  = "matches"
	OpExists   = "exists"
	OpOver     = "over"
	OpUnder    = "under"
)

// Action kinds.
const (
	ActionFileInto = "fileinto"
	ActionRedirect = "redirect"
	ActionKeep     = "keep"
	ActionDiscard  = "discard"
	ActionStop     = "stop"
	ActionAddFlag  = "addflag"
)

// Script is everything Pelton keeps in its own sieve script.
type Script struct {
	// Vacation is the out of office reply, nil when none was ever set.
	Vacation *Vacation `json:"vacation"`
	Rules    []Rule    `json:"rules"`
	// Includes are other scripts on the server that keep running after
	// Pelton's own rules.
	Includes []string `json:"includes"`
}

// Vacation is an out of office reply.
type Vacation struct {
	Enabled bool `json:"enabled"`
	// Start and End bound the reply by day, YYYY-MM-DD, both inclusive. Empty
	// means unbounded on that side.
	Start string `json:"start"`
	End   string `json:"end"`
	// Days is how often one sender gets a reply; 0 leaves it to the server.
	Days    int    `json:"days"`
	Subject string `json:"subject"`
	From    string `json:"from"`
	// Addresses are the account's addresses the reply answers for.
	Addresses []string `json:"addresses"`
	Body      string   `json:"body"`
}

// Rule is one filter: when its conditions hold, its actions run.
type Rule struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	// MatchAny is true when one condition is enough, false when all must hold.
	MatchAny   bool        `json:"matchAny"`
	Conditions []Condition `json:"conditions"`
	Actions    []Action    `json:"actions"`
}

// Condition is one test of a rule.
type Condition struct {
	Field string `json:"field"`
	// Header names the header of a FieldHeader condition.
	Header string `json:"header"`
	Op     string `json:"op"`
	Negate bool   `json:"negate"`
	// Value is the text to compare with, or for size a number with an
	// optional K, M or G.
	Value string `json:"value"`
}

// Action is one thing a rule does.
type Action struct {
	Kind string `json:"kind"`
	// Arg is the folder of fileinto, the address of redirect and the flag of
	// addflag.
	Arg string `json:"arg"`
}

// Requires lists the extensions the script needs, sorted.
func (s *Script) Requires() []string {
	set := map[string]bool{}
	if v := s.Vacation; v != nil {
		set[ExtVacation] = true
		if v.Start != "" || v.End != "" {
			set[ExtDate] = true
			set[ExtRelational] = true
		}
	}
	for _, r := range s.Rules {
		for _, c := range r.Conditions {
			if c.Field == FieldEnvFrom || c.Field == FieldEnvTo {
				set[ExtEnvelope] = true
			}
		}
		for _, a := range r.Actions {
			switch a.Kind {
			case ActionFileInto:
				set[ExtFileInto] = true
			case ActionAddFlag:
				set[ExtIMAP4Flags] = true
			}
		}
	}
	if len(s.Includes) > 0 {
		set[ExtInclude] = true
	}
	out := make([]string, 0, len(set))
	for ext := range set {
		out = append(out, ext)
	}
	slices.Sort(out)
	return out
}

// UnsupportedError means the script needs an extension the server lacks.
type UnsupportedError struct {
	Extension string
}

func (e *UnsupportedError) Error() string {
	return fmt.Sprintf("sieve: the server does not support the %q extension", e.Extension)
}

// InvalidError names a field of the model that cannot be written.
type InvalidError struct {
	// Rule is the index of the rule at fault, or -1 for the out of office.
	Rule  int
	Field string
	Msg   string
}

func (e *InvalidError) Error() string {
	if e.Rule < 0 {
		return fmt.Sprintf("sieve: out of office %s: %s", e.Field, e.Msg)
	}
	return fmt.Sprintf("sieve: rule %d %s: %s", e.Rule+1, e.Field, e.Msg)
}

var (
	dayRe  = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	sizeRe = regexp.MustCompile(`^\d+[KMG]?$`)
)

// Validate checks that the script can be written and that the server, which
// supports the extensions has reports true for, can run it.
func (s *Script) Validate(has func(ext string) bool) error {
	for _, ext := range s.Requires() {
		if !has(ext) {
			return &UnsupportedError{Extension: ext}
		}
	}
	if v := s.Vacation; v != nil {
		bad := func(field, msg string) error { return &InvalidError{Rule: -1, Field: field, Msg: msg} }
		if strings.TrimSpace(v.Body) == "" {
			return bad("message", "is empty")
		}
		if v.Start != "" && !dayRe.MatchString(v.Start) {
			return bad("start", "is not a YYYY-MM-DD day")
		}
		if v.End != "" && !dayRe.MatchString(v.End) {
			return bad("end", "is not a YYYY-MM-DD day")
		}
		if v.Start != "" && v.End != "" && v.End < v.Start {
			return bad("end", "is before the start")
		}
		if v.Days < 0 {
			return bad("days", "is negative")
		}
		for _, a := range v.Addresses {
			if !strings.Contains(a, "@") {
				return bad("addresses", fmt.Sprintf("%q is not an address", a))
			}
		}
	}
	for i, r := range s.Rules {
		bad := func(field, msg string) error { return &InvalidError{Rule: i, Field: field, Msg: msg} }
		if strings.ContainsAny(r.Name, "\r\n") {
			return bad("name", "has a line break")
		}
		if len(r.Actions) == 0 {
			return bad("actions", "are empty")
		}
		for _, c := range r.Conditions {
			if err := validCondition(c); err != "" {
				return bad("condition", err)
			}
		}
		for _, a := range r.Actions {
			switch a.Kind {
			case ActionFileInto:
				if a.Arg == "" {
					return bad("action", "has no folder")
				}
			case ActionRedirect:
				if !strings.Contains(a.Arg, "@") {
					return bad("action", "has no address to forward to")
				}
			case ActionAddFlag:
				if a.Arg == "" {
					return bad("action", "has no flag")
				}
			case ActionKeep, ActionDiscard, ActionStop:
			default:
				return bad("action", fmt.Sprintf("kind %q is unknown", a.Kind))
			}
			if strings.ContainsAny(a.Arg, "\r\n") {
				return bad("action", "has a line break")
			}
		}
	}
	for _, name := range s.Includes {
		if name == "" || strings.ContainsAny(name, "\r\n") {
			return &InvalidError{Rule: -1, Field: "include", Msg: "has no usable name"}
		}
	}
	return nil
}

func validCondition(c Condition) string {
	if strings.ContainsAny(c.Value+c.Header, "\r\n") {
		return "has a line break"
	}
	switch c.Field {
	case FieldSize:
		if c.Op != OpOver && c.Op != OpUnder {
			return "size takes over or under"
		}
		if !sizeRe.MatchString(c.Value) {
			return "size is not a number"
		}
		return ""
	case FieldHeader:
		if strings.TrimSpace(c.Header) == "" {
			return "has no header name"
		}
		if c.Op == OpExists {
			return ""
		}
	case FieldFrom, FieldTo, FieldSubject, FieldEnvFrom, FieldEnvTo:
	default:
		return fmt.Sprintf("field %q is unknown", c.Field)
	}
	switch c.Op {
	case OpContains, OpIs, OpMatches:
	default:
		return fmt.Sprintf("operator %q does not apply", c.Op)
	}
	if c.Value == "" {
		return "has no value"
	}
	return ""
}
