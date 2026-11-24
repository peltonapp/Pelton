package mailimport

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
)

// Apple keeps settings and per-message state in property lists, as XML (the
// trailer of an .emlx file) or in the binary bplist00 form (most files under
// MailData). parsePlist reads either into plain Go values: map[string]any,
// []any, string, int64, float64, bool, time.Time and []byte.

var errBadPlist = errors.New("mailimport: unreadable property list")

func parsePlist(data []byte) (any, error) {
	if bytes.HasPrefix(data, []byte("bplist00")) {
		return parseBinaryPlist(data)
	}
	return parseXMLPlist(data)
}

func parseXMLPlist(data []byte) (any, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, errBadPlist
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local != "plist" {
			return xmlPlistValue(dec, se, 0)
		}
	}
}

const maxPlistDepth = 64

func xmlPlistValue(dec *xml.Decoder, se xml.StartElement, depth int) (any, error) {
	if depth > maxPlistDepth {
		return nil, errBadPlist
	}
	switch se.Name.Local {
	case "dict":
		out := map[string]any{}
		key := ""
		for {
			tok, err := dec.Token()
			if err != nil {
				return nil, errBadPlist
			}
			switch t := tok.(type) {
			case xml.EndElement:
				return out, nil
			case xml.StartElement:
				if t.Name.Local == "key" {
					var k string
					if err := dec.DecodeElement(&k, &t); err != nil {
						return nil, errBadPlist
					}
					key = k
					continue
				}
				v, err := xmlPlistValue(dec, t, depth+1)
				if err != nil {
					return nil, err
				}
				out[key] = v
			}
		}
	case "array":
		var out []any
		for {
			tok, err := dec.Token()
			if err != nil {
				return nil, errBadPlist
			}
			switch t := tok.(type) {
			case xml.EndElement:
				return out, nil
			case xml.StartElement:
				v, err := xmlPlistValue(dec, t, depth+1)
				if err != nil {
					return nil, err
				}
				out = append(out, v)
			}
		}
	case "true", "false":
		if err := dec.Skip(); err != nil {
			return nil, errBadPlist
		}
		return se.Name.Local == "true", nil
	}
	var text string
	if err := dec.DecodeElement(&text, &se); err != nil {
		return nil, errBadPlist
	}
	text = strings.TrimSpace(text)
	switch se.Name.Local {
	case "integer":
		n, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return nil, errBadPlist
		}
		return n, nil
	case "real":
		f, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return nil, errBadPlist
		}
		return f, nil
	case "date":
		d, err := time.Parse(time.RFC3339, text)
		if err != nil {
			return nil, errBadPlist
		}
		return d, nil
	}
	return text, nil
}

// appleEpoch is where binary plist dates count from.
var appleEpoch = time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)

type bplist struct {
	data       []byte
	offsets    []uint64
	refSize    int
	depth      int
	objectsMax int
}

func parseBinaryPlist(data []byte) (any, error) {
	if len(data) < 40 {
		return nil, errBadPlist
	}
	trailer := data[len(data)-32:]
	offSize := int(trailer[6])
	refSize := int(trailer[7])
	numObjects := binary.BigEndian.Uint64(trailer[8:16])
	top := binary.BigEndian.Uint64(trailer[16:24])
	tableAt := binary.BigEndian.Uint64(trailer[24:32])
	if offSize < 1 || offSize > 8 || refSize < 1 || refSize > 8 || numObjects == 0 ||
		numObjects > uint64(len(data)) || top >= numObjects ||
		tableAt+numObjects*uint64(offSize) > uint64(len(data)-32) {
		return nil, errBadPlist
	}
	p := &bplist{data: data, refSize: refSize, objectsMax: len(data)}
	p.offsets = make([]uint64, numObjects)
	for i := range p.offsets {
		at := tableAt + uint64(i*offSize)
		p.offsets[i] = readUint(data[at : at+uint64(offSize)])
	}
	return p.object(top)
}

func readUint(b []byte) uint64 {
	var n uint64
	for _, c := range b {
		n = n<<8 | uint64(c)
	}
	return n
}

func (p *bplist) object(ref uint64) (any, error) {
	if ref >= uint64(len(p.offsets)) || p.depth > maxPlistDepth {
		return nil, errBadPlist
	}
	at := p.offsets[ref]
	if at >= uint64(len(p.data)) {
		return nil, errBadPlist
	}
	marker := p.data[at]
	kind, info := marker>>4, int(marker&0x0f)
	body := at + 1
	switch kind {
	case 0x0:
		switch info {
		case 0x8:
			return false, nil
		case 0x9:
			return true, nil
		}
		return nil, nil
	case 0x1:
		size := uint64(1) << info
		b, err := p.slice(body, size)
		if err != nil {
			return nil, err
		}
		return int64(readUint(b)), nil
	case 0x2:
		size := uint64(1) << info
		b, err := p.slice(body, size)
		if err != nil {
			return nil, err
		}
		if size == 4 {
			return float64(math.Float32frombits(uint32(readUint(b)))), nil
		}
		return math.Float64frombits(readUint(b)), nil
	case 0x3:
		b, err := p.slice(body, 8)
		if err != nil {
			return nil, err
		}
		secs := math.Float64frombits(readUint(b))
		return appleEpoch.Add(time.Duration(secs * float64(time.Second))), nil
	}
	count, body, err := p.length(info, body)
	if err != nil {
		return nil, err
	}
	switch kind {
	case 0x4:
		return p.slice(body, count)
	case 0x5:
		b, err := p.slice(body, count)
		if err != nil {
			return nil, err
		}
		runes := make([]rune, len(b))
		for i, c := range b {
			runes[i] = rune(c)
		}
		return string(runes), nil
	case 0x6:
		b, err := p.slice(body, count*2)
		if err != nil {
			return nil, err
		}
		units := make([]uint16, count)
		for i := range units {
			units[i] = binary.BigEndian.Uint16(b[i*2:])
		}
		return string(utf16.Decode(units)), nil
	case 0xA:
		refs, err := p.refs(body, count)
		if err != nil {
			return nil, err
		}
		out := make([]any, 0, len(refs))
		p.depth++
		defer func() { p.depth-- }()
		for _, r := range refs {
			v, err := p.object(r)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	case 0xD:
		refs, err := p.refs(body, count*2)
		if err != nil {
			return nil, err
		}
		out := map[string]any{}
		p.depth++
		defer func() { p.depth-- }()
		for i := uint64(0); i < count; i++ {
			k, err := p.object(refs[i])
			if err != nil {
				return nil, err
			}
			key, ok := k.(string)
			if !ok {
				return nil, errBadPlist
			}
			v, err := p.object(refs[count+i])
			if err != nil {
				return nil, err
			}
			out[key] = v
		}
		return out, nil
	}
	return nil, fmt.Errorf("%w: object kind %x", errBadPlist, kind)
}

// length reads an object's element count, which is in the marker's low four
// bits or, when those are all set, in an integer object that follows.
func (p *bplist) length(info int, body uint64) (uint64, uint64, error) {
	if info != 0x0f {
		return uint64(info), body, nil
	}
	if body >= uint64(len(p.data)) || p.data[body]>>4 != 0x1 {
		return 0, 0, errBadPlist
	}
	size := uint64(1) << (p.data[body] & 0x0f)
	b, err := p.slice(body+1, size)
	if err != nil {
		return 0, 0, err
	}
	n := readUint(b)
	if n > uint64(p.objectsMax) {
		return 0, 0, errBadPlist
	}
	return n, body + 1 + size, nil
}

func (p *bplist) refs(body, count uint64) ([]uint64, error) {
	b, err := p.slice(body, count*uint64(p.refSize))
	if err != nil {
		return nil, err
	}
	out := make([]uint64, count)
	for i := range out {
		out[i] = readUint(b[i*p.refSize : (i+1)*p.refSize])
	}
	return out, nil
}

func (p *bplist) slice(at, size uint64) ([]byte, error) {
	if size > uint64(len(p.data)) || at > uint64(len(p.data))-size {
		return nil, io.ErrUnexpectedEOF
	}
	return p.data[at : at+size], nil
}

func plistString(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

func plistPort(m map[string]any, key string) int {
	n := plistInt(m, key)
	if n < 1 || n > 65535 {
		return 0
	}
	return int(n)
}

func plistInt(m map[string]any, key string) int64 {
	switch v := m[key].(type) {
	case int64:
		return v
	case float64:
		return int64(v)
	case string:
		n, _ := strconv.ParseInt(v, 10, 64)
		return n
	}
	return 0
}
