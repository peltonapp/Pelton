package notes

import "testing"

func TestRewriteLinks(t *testing.T) {
	files := []string{"Source.md", "Old Name.md", "dir/Deep.md", "dir/Other.md", "img/pic.png"}
	tests := []struct {
		name       string
		text       string
		noteBefore string
		noteAfter  string
		moves      map[string]string
		want       string
		count      int
	}{
		{
			name:       "bare name keeps alias and heading",
			text:       "a [[Old Name]] b [[old name#Part|shown]] c",
			noteBefore: "Source.md", noteAfter: "Source.md",
			moves: map[string]string{"Old Name.md": "New Name.md"},
			want:  "a [[New Name]] b [[New Name#Part|shown]] c",
			count: 2,
		},
		{
			name:       "path form stays a path",
			text:       "[[dir/Deep]] and [[dir/Deep.md|x]]",
			noteBefore: "Source.md", noteAfter: "Source.md",
			moves: map[string]string{"dir/Deep.md": "elsewhere/Deep.md"},
			want:  "[[elsewhere/Deep]] and [[elsewhere/Deep.md|x]]",
			count: 2,
		},
		{
			name:       "markdown link gets a relative escaped path",
			text:       "see [x](Old%20Name.md#h) and [y](<dir/Deep.md>)",
			noteBefore: "Source.md", noteAfter: "Source.md",
			moves: map[string]string{"Old Name.md": "sub/New Name.md", "dir/Deep.md": "dir/Moved.md"},
			want:  "see [x](sub/New%20Name.md#h) and [y](<dir/Moved.md>)",
			count: 2,
		},
		{
			name:       "moving the note fixes its own relative links",
			text:       "[o](dir/Other.md) [[Old Name]]",
			noteBefore: "Source.md", noteAfter: "dir/Source.md",
			moves: map[string]string{"Source.md": "dir/Source.md"},
			want:  "[o](Other.md) [[Old Name]]",
			count: 1,
		},
		{
			name:       "embeds of attachments",
			text:       "![[pic.png]] ![p](img/pic.png)",
			noteBefore: "Source.md", noteAfter: "Source.md",
			moves: map[string]string{"img/pic.png": "img/photo.png"},
			want:  "![[photo.png]] ![p](img/photo.png)",
			count: 2,
		},
		{
			name:       "unrelated and code untouched, crlf kept",
			text:       "---\r\ntitle: x\r\n---\r\n[[Other]] `[[Old Name]]`\r\n```\r\n[[Old Name]]\r\n```\r\n[[Old Name]]\r\n",
			noteBefore: "Source.md", noteAfter: "Source.md",
			moves: map[string]string{"Old Name.md": "Renamed.md"},
			want:  "---\r\ntitle: x\r\n---\r\n[[Other]] `[[Old Name]]`\r\n```\r\n[[Old Name]]\r\n```\r\n[[Renamed]]\r\n",
			count: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := NewResolver(files)
			var after []string
			for _, f := range files {
				if m, ok := tt.moves[f]; ok {
					f = m
				}
				after = append(after, f)
			}
			got, n := RewriteLinks(tt.text, tt.noteBefore, tt.noteAfter, before, NewResolver(after), tt.moves)
			if got != tt.want || n != tt.count {
				t.Errorf("RewriteLinks = %q (%d), want %q (%d)", got, n, tt.want, tt.count)
			}
		})
	}
}

func TestRewriteUsesPathWhenNameBecomesAmbiguous(t *testing.T) {
	files := []string{"a/Note.md", "b/Other.md", "Source.md"}
	moves := map[string]string{"b/Other.md": "b/Note.md"}
	after := NewResolver([]string{"a/Note.md", "b/Note.md", "Source.md"})
	got, _ := RewriteLinks("[[Other]]", "Source.md", "Source.md", NewResolver(files), after, moves)
	if got != "[[b/Note]]" {
		t.Errorf("got %q, want [[b/Note]]", got)
	}
}

func TestRelPath(t *testing.T) {
	tests := []struct{ dir, to, want string }{
		{".", "a/b.md", "a/b.md"},
		{"a", "a/b.md", "b.md"},
		{"a/x", "a/b.md", "../b.md"},
		{"c", "a/b.md", "../a/b.md"},
	}
	for _, tt := range tests {
		if got := relPath(tt.dir, tt.to); got != tt.want {
			t.Errorf("relPath(%q, %q) = %q, want %q", tt.dir, tt.to, got, tt.want)
		}
	}
}
