package managesieve

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// maxLiteral bounds a literal the server sends, so a broken or hostile server
// cannot make the client allocate without limit. Sieve scripts are small; the
// largest servers allow is in the hundreds of kilobytes.
const maxLiteral = 16 << 20

// maxLine bounds one response line outside literals.
const maxLine = 64 << 10

type tokenKind int

const (
	tokAtom tokenKind = iota
	tokString
	tokOpen
	tokClose
)

// token is one item of a response line: an atom, a quoted or literal string,
// or a parenthesis of a response code.
type token struct {
	kind  tokenKind
	value string
}

// response is a final OK, NO or BYE line.
type response struct {
	status  string
	code    string
	codeArg string
	text    string
}

// reader parses server output.
type reader struct {
	br *bufio.Reader
}

// readLine reads one logical line, literals included, as tokens.
func (r *reader) readLine() ([]token, error) {
	var out []token
	read := 0
	for {
		b, err := r.br.ReadByte()
		if err != nil {
			return nil, err
		}
		read++
		if read > maxLine {
			return nil, errors.New("managesieve: response line too long")
		}
		switch {
		case b == ' ':
		case b == '\r':
			nb, err := r.br.ReadByte()
			if err != nil {
				return nil, err
			}
			if nb != '\n' {
				return nil, fmt.Errorf("managesieve: bare CR in response")
			}
			return out, nil
		case b == '\n':
			return out, nil
		case b == '(':
			out = append(out, token{kind: tokOpen})
		case b == ')':
			out = append(out, token{kind: tokClose})
		case b == '"':
			s, err := r.readQuoted()
			if err != nil {
				return nil, err
			}
			out = append(out, token{kind: tokString, value: s})
		case b == '{':
			s, err := r.readLiteral()
			if err != nil {
				return nil, err
			}
			out = append(out, token{kind: tokString, value: s})
		default:
			var sb strings.Builder
			sb.WriteByte(b)
			for {
				nb, err := r.br.ReadByte()
				if err != nil {
					return nil, err
				}
				if nb == ' ' || nb == '\r' || nb == '\n' || nb == '(' || nb == ')' {
					_ = r.br.UnreadByte()
					break
				}
				sb.WriteByte(nb)
				read++
				if read > maxLine {
					return nil, errors.New("managesieve: response line too long")
				}
			}
			out = append(out, token{kind: tokAtom, value: sb.String()})
		}
	}
}

// readQuoted reads a quoted string after its opening quote.
func (r *reader) readQuoted() (string, error) {
	var sb strings.Builder
	for {
		b, err := r.br.ReadByte()
		if err != nil {
			return "", err
		}
		switch b {
		case '"':
			return sb.String(), nil
		case '\\':
			nb, err := r.br.ReadByte()
			if err != nil {
				return "", err
			}
			sb.WriteByte(nb)
		case '\r', '\n':
			return "", errors.New("managesieve: line break inside a quoted string")
		default:
			sb.WriteByte(b)
		}
		if sb.Len() > maxLine {
			return "", errors.New("managesieve: quoted string too long")
		}
	}
}

// readLiteral reads "{n}" or "{n+}" followed by CRLF and n octets, after the
// opening brace.
func (r *reader) readLiteral() (string, error) {
	spec, err := r.br.ReadString('}')
	if err != nil {
		return "", err
	}
	spec = strings.TrimSuffix(strings.TrimSuffix(spec, "}"), "+")
	n, err := strconv.Atoi(spec)
	if err != nil || n < 0 {
		return "", fmt.Errorf("managesieve: bad literal length %q", spec)
	}
	if n > maxLiteral {
		return "", fmt.Errorf("managesieve: literal of %d bytes is too large", n)
	}
	cr, err := r.br.ReadByte()
	if err != nil {
		return "", err
	}
	if cr == '\r' {
		if cr, err = r.br.ReadByte(); err != nil {
			return "", err
		}
	}
	if cr != '\n' {
		return "", errors.New("managesieve: literal length not followed by a line break")
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r.br, buf); err != nil {
		return "", err
	}
	return string(buf), nil
}

// asResponse reads a token line as a final response, or reports ok=false for a
// data line.
func asResponse(line []token) (response, bool) {
	if len(line) == 0 || line[0].kind != tokAtom {
		return response{}, false
	}
	status := strings.ToUpper(line[0].value)
	if status != "OK" && status != "NO" && status != "BYE" {
		return response{}, false
	}
	resp := response{status: status}
	rest := line[1:]
	if len(rest) > 0 && rest[0].kind == tokOpen {
		end := 1
		for end < len(rest) && rest[end].kind != tokClose {
			end++
		}
		var parts []string
		for _, tk := range rest[1:end] {
			parts = append(parts, tk.value)
		}
		if len(parts) > 0 {
			resp.code = strings.ToUpper(parts[0])
			resp.codeArg = strings.Join(parts[1:], " ")
		}
		if end < len(rest) {
			rest = rest[end+1:]
		} else {
			rest = nil
		}
	}
	if len(rest) > 0 {
		resp.text = rest[0].value
	}
	return resp, true
}

// quote encodes s as a protocol string: quoted when it is short and plain,
// otherwise a non-synchronizing literal (RFC 5804 section 4).
func quote(s string) string {
	if len(s) <= 1024 && !strings.ContainsAny(s, "\r\n\x00") {
		return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
	}
	return literal(s)
}

// literal always encodes s as a non-synchronizing literal.
func literal(s string) string {
	return "{" + strconv.Itoa(len(s)) + "+}\r\n" + s
}
