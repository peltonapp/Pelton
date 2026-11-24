package notes

import "testing"

func TestResolve(t *testing.T) {
	r := NewResolver([]string{
		"Inbox.md",
		"Projects/Alpha.md",
		"Archive/Alpha.md",
		"Archive/Old/Alpha.md",
		"People/Ann Lee.md",
		"img/pic.png",
		"Deep/a/Shared.md",
		"Deep/b/Shared.md",
	})
	tests := []struct {
		target, from, want string
	}{
		{"Inbox", "x.md", "Inbox.md"},
		{"inbox.md", "x.md", "Inbox.md"},
		{"ann lee", "x.md", "People/Ann Lee.md"},
		// same folder first, then the shortest path.
		{"Alpha", "Archive/Note.md", "Archive/Alpha.md"},
		{"Alpha", "Projects/Note.md", "Projects/Alpha.md"},
		{"Alpha", "Other/Note.md", "Archive/Alpha.md"},
		{"Archive/Old/Alpha", "x.md", "Archive/Old/Alpha.md"},
		{"Old/Alpha", "x.md", "Archive/Old/Alpha.md"},
		{"pic.png", "x.md", "img/pic.png"},
		{"pic", "x.md", ""},
		{"Missing", "x.md", ""},
		{"", "self.md", "self.md"},
		{"a/Shared", "x.md", "Deep/a/Shared.md"},
	}
	for _, tt := range tests {
		if got := r.Resolve(tt.target, tt.from); got != tt.want {
			t.Errorf("Resolve(%q, %q) = %q, want %q", tt.target, tt.from, got, tt.want)
		}
	}
}

func TestResolveMarkdown(t *testing.T) {
	r := NewResolver([]string{"a/b/Note.md", "a/Other.md", "Top.md"})
	tests := []struct {
		target, from, want string
	}{
		{"../Other.md", "a/b/Note.md", "a/Other.md"},
		{"Note.md", "a/b/x.md", "a/b/Note.md"},
		{"Top.md", "a/b/x.md", "Top.md"},
		{"/a/Other.md", "a/b/x.md", "a/Other.md"},
	}
	for _, tt := range tests {
		if got := r.ResolveMarkdown(tt.target, tt.from); got != tt.want {
			t.Errorf("ResolveMarkdown(%q, %q) = %q, want %q", tt.target, tt.from, got, tt.want)
		}
	}
}

func TestLinkText(t *testing.T) {
	r := NewResolver([]string{"One.md", "a/Two.md", "b/Two.md", "img/x.png"})
	tests := map[string]string{
		"One.md":    "One",
		"a/Two.md":  "a/Two",
		"img/x.png": "x.png",
	}
	for p, want := range tests {
		if got := r.LinkText(p); got != want {
			t.Errorf("LinkText(%q) = %q, want %q", p, got, want)
		}
	}
}
