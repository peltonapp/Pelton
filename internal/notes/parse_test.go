package notes

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseFrontMatter(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		bodyStart int
		tags      []string
		aliases   []string
	}{
		{name: "none", text: "# Title\n", bodyStart: 0},
		{
			name:      "block lists",
			text:      "---\ntags:\n  - work\n  - project/alpha\naliases:\n  - Alpha\n---\nbody #extra",
			bodyStart: len("---\ntags:\n  - work\n  - project/alpha\naliases:\n  - Alpha\n---\n"),
			tags:      []string{"work", "project/alpha", "extra"},
			aliases:   []string{"Alpha"},
		},
		{
			name:      "inline list and crlf",
			text:      "---\r\ntags: [a, \"b\"]\r\naliases: [\"Bee\"]\r\n---\r\ntext",
			bodyStart: len("---\r\ntags: [a, \"b\"]\r\naliases: [\"Bee\"]\r\n---\r\n"),
			tags:      []string{"a", "b"},
			aliases:   []string{"Bee"},
		},
		{
			name:      "tags as a string",
			text:      "---\ntags: one, #two three\n---\n",
			bodyStart: len("---\ntags: one, #two three\n---\n"),
			tags:      []string{"one", "two", "three"},
		},
		{
			name:      "byte order mark",
			text:      bom + "---\ntag: x\n---\nhi",
			bodyStart: len(bom + "---\ntag: x\n---\n"),
			tags:      []string{"x"},
		},
		{name: "unterminated is body", text: "---\ntags: [a]\nno end", bodyStart: 0},
		{name: "rule later is not front matter", text: "text\n---\ntags: [a]\n---\n", bodyStart: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Parse(tt.text)
			if p.BodyStart != tt.bodyStart {
				t.Errorf("BodyStart = %d, want %d", p.BodyStart, tt.bodyStart)
			}
			if !reflect.DeepEqual(p.Tags, tt.tags) {
				t.Errorf("Tags = %q, want %q", p.Tags, tt.tags)
			}
			if !reflect.DeepEqual(p.Aliases, tt.aliases) {
				t.Errorf("Aliases = %q, want %q", p.Aliases, tt.aliases)
			}
		})
	}
}

func TestParseHeadingsAndTags(t *testing.T) {
	text := strings.Join([]string{
		"# One",
		"Some #tag and #nested/tag, not#this, not #123 but #2026x",
		"## Two ##",
		"```",
		"# not a heading #nottag [[NotLink]]",
		"```",
		"`#code [[code]]` and #Tag again",
		"###### Six",
		"####### seven is text",
	}, "\n")
	p := Parse(text)
	wantHeadings := []Heading{{1, "One", 0}, {2, "Two", 2}, {6, "Six", 7}}
	if !reflect.DeepEqual(p.Headings, wantHeadings) {
		t.Errorf("Headings = %+v, want %+v", p.Headings, wantHeadings)
	}
	wantTags := []string{"tag", "nested/tag", "2026x"}
	if !reflect.DeepEqual(p.Tags, wantTags) {
		t.Errorf("Tags = %q, want %q", p.Tags, wantTags)
	}
	if len(p.Links) != 0 {
		t.Errorf("links in code were read: %+v", p.Links)
	}
}

func TestParseLinks(t *testing.T) {
	text := "See [[Note]], [[Folder/Other#Part|shown]] and ![[pic.png]].\n" +
		"A [md link](sub/My%20Note.md#Head), an ![image](<img/a b.png>), [web](https://example.com)\n" +
		"and [mail](pelton://mail/abc%40host) plus [[#Local]] | [[Table\\|alias]]"
	p := Parse(text)
	type want struct {
		kind    LinkKind
		embed   bool
		target  string
		subpath string
		display string
		raw     string
		line    int
	}
	wants := []want{
		{LinkWiki, false, "Note", "", "", "Note", 0},
		{LinkWiki, false, "Folder/Other", "Part", "shown", "Folder/Other", 0},
		{LinkWiki, true, "pic.png", "", "", "pic.png", 0},
		{LinkMarkdown, false, "sub/My Note.md", "Head", "md link", "sub/My%20Note.md", 1},
		{LinkMarkdown, true, "img/a b.png", "", "image", "img/a b.png", 1},
		{LinkExternal, false, "https://example.com", "", "web", "https://example.com", 1},
		{LinkPelton, false, "pelton://mail/abc%40host", "", "mail", "pelton://mail/abc%40host", 2},
		{LinkWiki, false, "", "Local", "", "", 2},
		{LinkWiki, false, "Table", "", "alias", "Table", 2},
	}
	if len(p.Links) != len(wants) {
		t.Fatalf("got %d links, want %d: %+v", len(p.Links), len(wants), p.Links)
	}
	for i, w := range wants {
		l := p.Links[i]
		got := want{l.Kind, l.Embed, l.Target, l.Subpath, l.Display, text[l.TStart:l.TEnd], l.Line}
		if got != w {
			t.Errorf("link %d = %+v, want %+v", i, got, w)
		}
		if l.Embed && text[l.Start] != '!' {
			t.Errorf("link %d: embed span does not start at the bang", i)
		}
	}
}

func TestSnippet(t *testing.T) {
	body := "# Heading\n\n- [ ] Call **Ann** about [[Project X|the project]]\n```\ncode\n```\n> [!note] A [link](x.md)"
	got := Snippet(body)
	want := "Heading Call Ann about the project A link"
	if got != want {
		t.Errorf("Snippet = %q, want %q", got, want)
	}
}
