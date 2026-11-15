package sieve

import (
	"fmt"
	"strings"
)

// ArgKind tells the kinds of command argument apart.
type ArgKind int

const (
	// ArgTag is a ":name" tag.
	ArgTag ArgKind = iota
	// ArgNumber is a number with an optional K, M or G quantifier.
	ArgNumber
	// ArgStrings is a string or a string list.
	ArgStrings
)

// Arg is one positional or tagged argument.
type Arg struct {
	Kind ArgKind
	// Value is the tag name without its colon, or the number as written.
	Value string
	// Strings holds the strings of an ArgStrings argument.
	Strings []string
	// List is true when the strings were written as a [list].
	List bool
	// Multiline is true for a single string written in the text: form.
	Multiline bool
}

// Test is a test with its arguments and nested tests.
type Test struct {
	Name  string
	Args  []Arg
	Tests []Test
}

// Command is a command, its arguments, the test of a control command, its
// block, and the comment lines directly above it.
type Command struct {
	Name     string
	Args     []Arg
	Tests    []Test
	Block    []Command
	HasBlock bool
	Comments []string
}

// SyntaxError is a script that does not follow the sieve grammar.
type SyntaxError struct {
	Line int
	Msg  string
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("sieve: line %d: %s", e.Line, e.Msg)
}

type tokKind int

const (
	tEOF tokKind = iota
	tIdent
	tTag
	tNumber
	tString
	tText
	tComment
	tPunct
)

type tok struct {
	kind  tokKind
	value string
	line  int
}

// lex splits a script (with LF line ends) into tokens, comments included.
func lex(src string) ([]tok, error) {
	var out []tok
	line := 1
	i := 0
	errAt := func(msg string) error { return &SyntaxError{Line: line, Msg: msg} }
	for i < len(src) {
		c := src[i]
		switch {
		case c == '\n':
			line++
			i++
		case c == ' ' || c == '\t' || c == '\r':
			i++
		case c == '#':
			end := strings.IndexByte(src[i:], '\n')
			if end < 0 {
				end = len(src) - i
			}
			out = append(out, tok{kind: tComment, value: src[i : i+end], line: line})
			i += end
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			end := strings.Index(src[i+2:], "*/")
			if end < 0 {
				return nil, errAt("unterminated comment")
			}
			body := src[i : i+2+end+2]
			line += strings.Count(body, "\n")
			i += len(body)
		case strings.ContainsRune("[](){},;", rune(c)):
			out = append(out, tok{kind: tPunct, value: string(c), line: line})
			i++
		case c == '"':
			var sb strings.Builder
			start := line
			i++
			for {
				if i >= len(src) {
					return nil, &SyntaxError{Line: start, Msg: "unterminated string"}
				}
				ch := src[i]
				if ch == '"' {
					i++
					break
				}
				if ch == '\\' && i+1 < len(src) {
					i++
					ch = src[i]
				}
				if ch == '\n' {
					line++
				}
				sb.WriteByte(ch)
				i++
			}
			out = append(out, tok{kind: tString, value: sb.String(), line: start})
		case c == ':':
			j := i + 1
			for j < len(src) && isIdentChar(src[j], j == i+1) {
				j++
			}
			if j == i+1 {
				return nil, errAt("empty tag")
			}
			out = append(out, tok{kind: tTag, value: src[i+1 : j], line: line})
			i = j
		case c >= '0' && c <= '9':
			j := i
			for j < len(src) && src[j] >= '0' && src[j] <= '9' {
				j++
			}
			if j < len(src) && strings.ContainsRune("KMGkmg", rune(src[j])) {
				j++
			}
			out = append(out, tok{kind: tNumber, value: src[i:j], line: line})
			i = j
		case isIdentChar(c, true):
			j := i
			for j < len(src) && isIdentChar(src[j], j == i) {
				j++
			}
			word := src[i:j]
			if strings.EqualFold(word, "text") && j < len(src) && src[j] == ':' {
				text, consumed, lines, err := readText(src[j+1:], line)
				if err != nil {
					return nil, err
				}
				out = append(out, tok{kind: tText, value: text, line: line})
				line += lines
				i = j + 1 + consumed
				continue
			}
			out = append(out, tok{kind: tIdent, value: word, line: line})
			i = j
		default:
			return nil, errAt(fmt.Sprintf("unexpected character %q", c))
		}
	}
	out = append(out, tok{kind: tEOF, line: line})
	return out, nil
}

// readText reads the body of a text: string after the colon: the rest of the
// line (blanks and an optional comment), then lines up to a lone ".", with a
// leading doubled dot unstuffed. It returns the value, the bytes consumed and
// the line breaks crossed.
func readText(src string, line int) (string, int, int, error) {
	nl := strings.IndexByte(src, '\n')
	if nl < 0 {
		return "", 0, 0, &SyntaxError{Line: line, Msg: "unterminated text: string"}
	}
	head := strings.TrimLeft(src[:nl], " \t\r")
	if head != "" && !strings.HasPrefix(head, "#") {
		return "", 0, 0, &SyntaxError{Line: line, Msg: "text: must end its line"}
	}
	pos := nl + 1
	lines := 1
	var sb strings.Builder
	for {
		end := strings.IndexByte(src[pos:], '\n')
		if end < 0 {
			return "", 0, 0, &SyntaxError{Line: line, Msg: "unterminated text: string"}
		}
		l := strings.TrimSuffix(src[pos:pos+end], "\r")
		pos += end + 1
		lines++
		if l == "." {
			return sb.String(), pos, lines, nil
		}
		if strings.HasPrefix(l, "..") {
			l = l[1:]
		}
		sb.WriteString(l)
		sb.WriteByte('\n')
	}
}

func isIdentChar(c byte, first bool) bool {
	if c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
		return true
	}
	return !first && c >= '0' && c <= '9'
}

type parser struct {
	toks []tok
	pos  int
}

// parseCommands parses a whole script into its commands.
func parseCommands(src string) ([]Command, error) {
	toks, err := lex(normalize(src))
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	cmds, err := p.commands(false)
	if err != nil {
		return nil, err
	}
	return cmds, nil
}

func (p *parser) peek() tok { return p.toks[p.pos] }

func (p *parser) next() tok {
	t := p.toks[p.pos]
	if t.kind != tEOF {
		p.pos++
	}
	return t
}

func (p *parser) fail(msg string) error {
	return &SyntaxError{Line: p.peek().line, Msg: msg}
}

func (p *parser) isPunct(v string) bool {
	t := p.peek()
	return t.kind == tPunct && t.value == v
}

func (p *parser) commands(inBlock bool) ([]Command, error) {
	var out []Command
	var comments []string
	for {
		t := p.peek()
		switch {
		case t.kind == tComment:
			comments = append(comments, p.next().value)
		case t.kind == tEOF:
			if inBlock {
				return nil, p.fail("missing }")
			}
			return out, nil
		case inBlock && p.isPunct("}"):
			return out, nil
		case t.kind == tIdent:
			cmd, err := p.command()
			if err != nil {
				return nil, err
			}
			cmd.Comments = comments
			comments = nil
			out = append(out, cmd)
		default:
			return nil, p.fail(fmt.Sprintf("expected a command, found %q", t.value))
		}
	}
}

func (p *parser) skipComments() {
	for p.peek().kind == tComment {
		p.next()
	}
}

func (p *parser) command() (Command, error) {
	cmd := Command{Name: strings.ToLower(p.next().value)}
	args, tests, err := p.arguments()
	if err != nil {
		return cmd, err
	}
	cmd.Args, cmd.Tests = args, tests
	p.skipComments()
	switch {
	case p.isPunct(";"):
		p.next()
	case p.isPunct("{"):
		p.next()
		block, err := p.commands(true)
		if err != nil {
			return cmd, err
		}
		p.next()
		cmd.Block, cmd.HasBlock = block, true
	default:
		return cmd, p.fail(fmt.Sprintf("expected ; or { after %s", cmd.Name))
	}
	return cmd, nil
}

// arguments reads *argument [test / test-list].
func (p *parser) arguments() ([]Arg, []Test, error) {
	var args []Arg
	for {
		p.skipComments()
		t := p.peek()
		switch {
		case t.kind == tTag:
			p.next()
			args = append(args, Arg{Kind: ArgTag, Value: strings.ToLower(t.value)})
		case t.kind == tNumber:
			p.next()
			args = append(args, Arg{Kind: ArgNumber, Value: strings.ToUpper(t.value)})
		case t.kind == tString || t.kind == tText:
			p.next()
			args = append(args, Arg{Kind: ArgStrings, Strings: []string{t.value}, Multiline: t.kind == tText})
		case p.isPunct("["):
			p.next()
			list, err := p.stringList()
			if err != nil {
				return nil, nil, err
			}
			args = append(args, Arg{Kind: ArgStrings, Strings: list, List: true})
		case t.kind == tIdent:
			test, err := p.test()
			if err != nil {
				return nil, nil, err
			}
			return args, []Test{test}, nil
		case p.isPunct("("):
			tests, err := p.testList()
			if err != nil {
				return nil, nil, err
			}
			return args, tests, nil
		default:
			return args, nil, nil
		}
	}
}

func (p *parser) stringList() ([]string, error) {
	var out []string
	for {
		p.skipComments()
		t := p.next()
		if t.kind != tString && t.kind != tText {
			return nil, &SyntaxError{Line: t.line, Msg: "expected a string in the list"}
		}
		out = append(out, t.value)
		p.skipComments()
		if p.isPunct(",") {
			p.next()
			continue
		}
		if p.isPunct("]") {
			p.next()
			return out, nil
		}
		return nil, p.fail("expected , or ] in a string list")
	}
}

func (p *parser) test() (Test, error) {
	t := Test{Name: strings.ToLower(p.next().value)}
	args, tests, err := p.arguments()
	if err != nil {
		return t, err
	}
	t.Args, t.Tests = args, tests
	return t, nil
}

func (p *parser) testList() ([]Test, error) {
	p.next()
	var out []Test
	for {
		p.skipComments()
		if p.peek().kind != tIdent {
			return nil, p.fail("expected a test")
		}
		t, err := p.test()
		if err != nil {
			return nil, err
		}
		out = append(out, t)
		p.skipComments()
		if p.isPunct(",") {
			p.next()
			continue
		}
		if p.isPunct(")") {
			p.next()
			return out, nil
		}
		return nil, p.fail("expected , or ) in a test list")
	}
}

// normalize turns CRLF line ends into LF.
func normalize(src string) string {
	return strings.ReplaceAll(src, "\r\n", "\n")
}
