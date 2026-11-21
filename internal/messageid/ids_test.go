package messageid

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	valid := []string{"Ab.c+tag@Example.COM", "a@[127.0.0.1]", "a@[IPv6:2001:db8::1]", "a@[]", "!#$%&'*+-/=?^_`{|}~@x"}
	for _, bare := range valid {
		for _, value := range []string{bare, "<" + bare + ">"} {
			t.Run(value, func(t *testing.T) {
				if got, ok := Normalize(value); !ok || got != bare {
					t.Fatalf("Normalize(%q) = %q, %v; want %q, true", value, got, ok, bare)
				}
				token, ok := Bracket(value)
				if !ok || token != "<"+bare+">" || len(token) != len(bare)+2 {
					t.Fatalf("Bracket(%q) = %q, %v", value, token, ok)
				}
			})
		}
	}
	invalid := []string{"", "a", "@x", "a@", "a@b@c", ".a@x", "a.@x", "a..b@x", "a@.x", "a@x.", "a@x..y", `"a"@x`, "<a@x", "a@x>", "<<a@x>>", "<a@x>>", "a<b@x", "a@x>y", " a@x", "a@x ", "a b@x", "a\t@x", "a@x\r\nBcc: injected", "a\x00@x", "a\x7f@x", "é@x", "a@é", "a@[x y]", `a@[x\y]`, "a@[x[y]", "a@[x]y", "a@[x@y]", "a@[<x>]", "a(x)@y", "a@x:y"}
	for _, value := range invalid {
		t.Run(fmt.Sprintf("invalid/%q", value), func(t *testing.T) {
			if got, ok := Normalize(value); ok || got != "" {
				t.Fatalf("Normalize(%q) = %q, %v; want empty, false", value, got, ok)
			}
			if got, ok := Bracket(value); ok || got != "" {
				t.Fatalf("Bracket(%q) = %q, %v; want empty, false", value, got, ok)
			}
		})
	}
}

func TestNormalizeTokenLimit(t *testing.T) {
	bare978 := strings.Repeat("a", 976) + "@x"
	bare979 := strings.Repeat("a", 977) + "@x"
	for _, tc := range []struct {
		bare string
		ok   bool
	}{{bare978, true}, {bare979, false}} {
		for _, value := range []string{tc.bare, "<" + tc.bare + ">"} {
			got, ok := Normalize(value)
			if ok != tc.ok || (ok && got != tc.bare) || (!ok && got != "") {
				t.Fatalf("Normalize length %d = length %d, %v; want %v", len(value), len(got), ok, tc.ok)
			}
			token, ok := Bracket(value)
			if ok != tc.ok || (ok && (token != "<"+tc.bare+">" || len(token) != len(tc.bare)+2)) || (!ok && token != "") {
				t.Fatalf("Bracket length %d = length %d, %v", len(value), len(token), ok)
			}
		}
	}
}

func TestReferences(t *testing.T) {
	cases := []struct {
		name      string
		ancestors []string
		parent    string
		want      []string
	}{
		{"normalize dedupe relocate", []string{"<root@x>", "root@x", "<parent@x>", "bad", "ROOT@x", "new@x", "parent@x"}, "<parent@x>", []string{"root@x", "ROOT@x", "new@x", "parent@x"}},
		{"parent only", nil, "<parent@x>", []string{"parent@x"}},
		{"missing parent", []string{"root@x"}, "", []string{}},
		{"invalid parent", []string{"root@x"}, "<bad>", []string{}},
		{"only parent in ancestry", []string{"parent@x", "<parent@x>"}, "parent@x", []string{"parent@x"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := append([]string(nil), tc.ancestors...)
			got := References(tc.ancestors, tc.parent)
			if got == nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("References = %#v; want %#v", got, tc.want)
			}
			if !reflect.DeepEqual(tc.ancestors, before) {
				t.Fatal("input mutated")
			}
			if len(got) > 0 {
				got[0] = "changed@x"
				if !reflect.DeepEqual(tc.ancestors, before) {
					t.Fatal("result aliases input")
				}
			}
		})
	}
	t.Run("count cap then length floor", func(t *testing.T) {
		ids := referenceIDs(21, 300)
		want := []string{ids[0], ids[18], ids[19], ids[20]}
		if got := References(ids[:20], ids[20]); !reflect.DeepEqual(got, want) {
			t.Fatalf("References = %v; want exact root and last three %v", got, want)
		}
	})
	for _, n := range []int{20, 21, 100} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			ids := referenceIDs(n, 6)
			want := append([]string{}, ids...)
			if n == 21 {
				want = append([]string{ids[0]}, ids[2:21]...)
			}
			if n == 100 {
				want = append([]string{ids[0]}, ids[81:100]...)
			}
			before := append([]string{}, ids...)
			got := References(ids[:n-1], ids[n-1])
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("References = %v; want %v", got, want)
			}
			got[0] = "changed@x"
			if !reflect.DeepEqual(ids, before) {
				t.Fatal("input/backing array mutated")
			}
		})
	}
}

func TestReferencesLengthGuard(t *testing.T) {
	for _, tc := range []struct {
		name    string
		n, size int
		indices []int
		length  int
	}{
		{"trim oldest after root", 20, 60, []int{0, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19}, 956},
		{"four IDs above limit", 4, 300, []int{0, 1, 2, 3}, 1223},
		{"three IDs above limit", 3, 400, []int{0, 1, 2}, 1220},
		{"length floor after trimming", 20, 300, []int{0, 17, 18, 19}, 1223},
		{"exactly 998", 7, 138, []int{0, 1, 2, 3, 4, 5, 6}, 998},
		{"just above 998", 7, 139, []int{0, 2, 3, 4, 5, 6}, 863},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ids := referenceIDs(tc.n, tc.size)
			want := make([]string, 0, len(tc.indices))
			for _, i := range tc.indices {
				want = append(want, ids[i])
			}
			got := References(ids[:len(ids)-1], ids[len(ids)-1])
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("References = %v; want %v", got, want)
			}
			unfolded := "References: <" + strings.Join(got, "> <") + ">"
			if len(unfolded) != tc.length {
				t.Fatalf("unfolded length = %d; want %d", len(unfolded), tc.length)
			}
		})
	}
}

func referenceIDs(n, size int) []string {
	ids := make([]string, n)
	for i := range ids {
		prefix := fmt.Sprintf("%03d", i)
		ids[i] = prefix + strings.Repeat("a", size-len(prefix)-2) + "@x"
	}
	return ids
}
