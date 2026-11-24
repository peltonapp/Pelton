package system

import (
	"context"
	"testing"

	"github.com/peltonapp/Pelton/internal/scripting"
	"github.com/peltonapp/Pelton/internal/scripting/scriptingtest"
)

type fakeHost struct {
	clip   string
	opened []string
}

func (f *fakeHost) SetClipboard(_ context.Context, s string) error { f.clip = s; return nil }
func (f *fakeHost) Clipboard(context.Context) (string, error)      { return f.clip, nil }
func (f *fakeHost) OpenLink(_ context.Context, s string) error {
	f.opened = append(f.opened, s)
	return nil
}

func setup(t *testing.T) (*scripting.Registry, *fakeHost) {
	h := &fakeHost{clip: "copied"}
	return scriptingtest.Registry(t, func(r *scripting.Registry) error { return Register(r, h) }), h
}

func TestLocalized(t *testing.T) {
	r, _ := setup(t)
	scriptingtest.Localized(t, r)
}

func TestClipboardAndLinks(t *testing.T) {
	r, h := setup(t)
	run := func(src string, perms ...string) *scripting.Run {
		return scriptingtest.Run(t, r, scriptingtest.Script(1, src, perms...), false)
	}
	if got := run(`print(clipboard.read())`, PermClipboardWrite); got.Status != scripting.StatusDenied {
		t.Fatalf("read with write only: %s", got.Status)
	}
	got := run(`clipboard.write("x") print(clipboard.read())`, PermClipboardWrite, PermClipboardRead)
	if got.Status != scripting.StatusOK || scriptingtest.Logged(got)[0] != "x" {
		t.Fatalf("%s %v", got.Status, scriptingtest.Logged(got))
	}
	for _, link := range []string{"file:///etc/passwd", "javascript:alert(1)", "mailto:a@b.c", "x"} {
		if got := run(`links.open("`+link+`")`, PermOpenLink); got.Status != scripting.StatusError {
			t.Fatalf("%s: %s", link, got.Status)
		}
	}
	if got := run(`links.open("https://example.org/a")`, PermOpenLink); got.Status != scripting.StatusOK {
		t.Fatalf("%s %s", got.Status, got.Error)
	}
	if len(h.opened) != 1 {
		t.Fatalf("opened %v", h.opened)
	}
	dry := scriptingtest.Run(t, r, scriptingtest.Script(1, `clipboard.write("dry") links.open("https://example.org")`, PermClipboardWrite, PermOpenLink), true)
	if dry.Status != scripting.StatusOK || dry.Changes() != 2 || h.clip != "x" || len(h.opened) != 1 {
		t.Fatalf("dry: %s %+v", dry.Status, dry.Steps)
	}
}
