package messageid

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseHeader(t *testing.T) {
	limitID := strings.Repeat("a", 976) + "@x"
	cases := []struct {
		name  string
		value string
		want  []string
		valid bool
	}{
		{"empty", "", []string{}, true},
		{"CFWS only", " \t(root (nested))\r\n\t", []string{}, true},
		{"folded comments", "(root (nested)) <a@example.com>\r\n\t<b@example.com> (tail)", []string{"a@example.com", "b@example.com"}, true},
		{"escaped comments", `(root \(escaped\) \\ (nested)) <a@example.com> (tail)`, []string{"a@example.com"}, true},
		{"case and duplicates", "<A@Example.COM> <A@Example.COM> <a@example.com>", []string{"A@Example.COM", "A@Example.COM", "a@example.com"}, true},
		{"duplicate parents", "<a@example.com> <a@example.com>", []string{"a@example.com", "a@example.com"}, true},
		{"Normalize domain literals", "<a@[]> <a@[IPv6:2001:db8::1]>", []string{"a@[]", "a@[IPv6:2001:db8::1]"}, true},
		{"adjacent tokens", "<a@x><b@x>", []string{"a@x", "b@x"}, true},
		{"invalid closed token recovery", "<a@x> <bad> <b@x>", []string{"a@x", "b@x"}, false},
		{"unclosed token clears recovery", "<a@x> <b@x", []string{}, false},
		{"junk clears recovery", "<a@x> junk <b@x>", []string{}, false},
		{"internal space", "<a@ example.com>", []string{}, false},
		{"internal folding", "<a@\r\n\texample.com>", []string{}, false},
		{"internal comment", "<a@(comment)example.com>", []string{}, false},
		{"bare ID", "a@example.com", []string{}, false},
		{"nested brackets", "<a@x> <<b@x>>", []string{}, false},
		{"stray bracket", "<a@x> > <b@x>", []string{}, false},
		{"unterminated comment", "<a@x> (tail", []string{}, false},
		{"unterminated escape", "<a@x> (tail\\", []string{}, false},
		{"stray comment end", "<a@x> )", []string{}, false},
		{"unfolded CRLF injection", "<a@x>\r\nBcc: victim@example.com", []string{}, false},
		{"bare CR", "<a@x>\r <b@x>", []string{}, false},
		{"bare LF", "<a@x>\n <b@x>", []string{}, false},
		{"control in token", "<a@x> <b\x00@x>", []string{}, false},
		{"control in comment", "<a@x> (bad\x7f)", []string{}, false},
		{"escaped newline", "<a@x> (bad\\\r\n )", []string{}, false},
		{"non-ASCII ID", "<a@éxample.com>", []string{}, false},
		{"encoded word", "=?UTF-8?Q?<a@x>?=", []string{}, false},
		{"token byte limit", "<" + limitID + ">", []string{limitID}, true},
		{"oversized token recovery", "<a@x> <a" + limitID + "> <b@x>", []string{"a@x", "b@x"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, valid := ParseHeader(tc.value)
			if !reflect.DeepEqual(got, tc.want) || valid != tc.valid {
				t.Fatalf("ParseHeader(%q) = %#v, %v; want %#v, %v", tc.value, got, valid, tc.want, tc.valid)
			}
		})
	}
}
