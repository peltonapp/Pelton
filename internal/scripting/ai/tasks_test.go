package ai

import (
	"reflect"
	"strings"
	"testing"
)

func TestMatchLabel(t *testing.T) {
	cats := []string{"Bills", "Friends", "yes"}
	cases := map[string]string{
		"Bills":                                "Bills",
		" bills. ":                             "Bills",
		`"Friends"`:                            "Friends",
		`{"label":"friends"}`:                  "Friends",
		"```json\n{\"label\": \"Bills\"}\n```": "Bills",
		"**Yes**":                              "yes",
		"Bills or Friends":                     "",
		"Spam":                                 "",
		`{"label":"Spam"}`:                     "",
	}
	for in, want := range cases {
		got, ok := matchLabel(in, cats)
		if got != want || ok != (want != "") {
			t.Errorf("%q: got %q %v, want %q", in, got, ok, want)
		}
	}
}

func TestParseFields(t *testing.T) {
	got, err := ParseFields([]any{"amount:number", "due:date", "paid:yes_no", "note"})
	if err != nil {
		t.Fatal(err)
	}
	want := []FieldSpec{{"amount", FieldNumber}, {"due", FieldDate}, {"paid", FieldYesNo}, {"note", FieldText}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
	got, err = ParseFields(map[string]any{"b": "bool", "a": "text"})
	if err != nil || !reflect.DeepEqual(got, []FieldSpec{{"a", FieldText}, {"b", FieldYesNo}}) {
		t.Fatalf("table: %v %v", got, err)
	}
	for _, bad := range []any{nil, []any{}, []any{"a b"}, []any{"x:colour"}, []any{"a", "a"}, []any{1.0}} {
		if _, err := ParseFields(bad); err == nil {
			t.Errorf("%v: no error", bad)
		}
	}
}

func TestCheckExtracted(t *testing.T) {
	fields := []FieldSpec{{"n", FieldNumber}, {"d", FieldDate}, {"y", FieldYesNo}, {"s", FieldText}}
	got, err := CheckExtracted(`{"n": "1,200.5", "d": "2026-03-04T10:30:00Z", "y": "yes", "s": 7, "other": true}`, fields)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"n": 1200.5, "d": "2026-03-04 10:30", "y": true, "s": "7"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
	got, err = CheckExtracted(`{"n": null, "d": "2026-03-04"}`, fields)
	if err != nil || !reflect.DeepEqual(got, map[string]any{"d": "2026-03-04"}) {
		t.Fatalf("nulls: %v %v", got, err)
	}
	for _, bad := range []string{`not json`, `{"n": "lots"}`, `{"d": "next week"}`, `{"y": "perhaps"}`, `{"s": {}}`, `[1]`} {
		if _, err := CheckExtracted(bad, fields); err == nil {
			t.Errorf("%s: no error", bad)
		}
	}
	schema := extractSchema(fields)
	if schema["additionalProperties"] != false || len(schema["required"].([]any)) != 4 {
		t.Fatalf("schema %v", schema)
	}
}

func TestLocal(t *testing.T) {
	cases := map[string]bool{
		"http://localhost:11434":     true,
		"http://127.0.0.1:8000/v1":   true,
		"http://[::1]:8000":          true,
		"http://192.168.1.5:8000/v1": true,
		"http://10.0.0.2":            true,
		"https://api.openai.com/v1":  false,
		"http://gpu-box.lan:11434":   false,
		"http://8.8.8.8":             false,
		"ftp://127.0.0.1":            false,
	}
	for u, want := range cases {
		if got := Local(u); got != want {
			t.Errorf("%s: got %v", u, got)
		}
	}
}

func TestNewIDAndPick(t *testing.T) {
	if got := NewID("My OpenAI!", nil); got != "my-openai" {
		t.Fatalf("got %q", got)
	}
	if got := NewID("My OpenAI", []string{"my-openai", "my-openai-2"}); got != "my-openai-3" {
		t.Fatalf("got %q", got)
	}
	if got := NewID("!!!", nil); got != "provider" {
		t.Fatalf("got %q", got)
	}
	if got := NewID(strings.Repeat("a", 80), nil); len(got) > maxIDLen {
		t.Fatalf("got %q", got)
	}
	list := []Provider{{ID: "a", Name: "Alpha"}, {ID: "b", Name: "Beta", Default: true}}
	for want, name := range map[string]string{"b": "", "a": "alpha"} {
		p, err := Pick(list, name)
		if err != nil || p.ID != want {
			t.Fatalf("%q: %v %v", name, p, err)
		}
	}
	if _, err := Pick(list, "gamma"); err == nil {
		t.Fatal("picked a provider that does not exist")
	}
	if _, err := Pick(nil, ""); err != ErrNoProvider {
		t.Fatalf("got %v", err)
	}
}

func TestValidate(t *testing.T) {
	good := Provider{Name: "x", Kind: "ollama", BaseURL: "http://localhost:11434"}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, p := range []Provider{
		{Kind: "ollama", BaseURL: "http://localhost"},
		{Name: "x", Kind: "nope", BaseURL: "http://localhost"},
		{Name: "x", Kind: "ollama", BaseURL: "file:///etc"},
		{Name: "x", Kind: "ollama", BaseURL: "http://user:pw@localhost"},
		{Name: "x", Kind: "ollama", BaseURL: "http://localhost?key=1"},
	} {
		if err := p.Validate(); err == nil {
			t.Errorf("%+v: no error", p)
		}
	}
}

func TestFence(t *testing.T) {
	a, b := fence("email", "hi"), fence("email", "hi")
	if a == b || !strings.HasPrefix(a, "----- BEGIN UNTRUSTED EMAIL [") || !strings.HasSuffix(a, "-----") {
		t.Fatalf("fence %q", a)
	}
	long := fence("data", strings.Repeat("x", maxInputBytes+10))
	if !strings.Contains(long, "[cut:") || len(long) > maxInputBytes+200 {
		t.Fatal("long data is not cut")
	}
}
