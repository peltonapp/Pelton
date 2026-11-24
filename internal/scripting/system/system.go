// Package system gives scripts the clipboard and lets them open a web link
// in the browser. Reading the clipboard has its own permission, since it may
// hold anything the user copied, and opening a link asks first unless the
// user lets the script do it without asking.
package system

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/peltonapp/Pelton/internal/scripting"
)

// The permissions this package registers.
const (
	PermClipboardWrite = "clipboard.write"
	PermClipboardRead  = "clipboard.read"
	PermOpenLink       = "links.open"
)

// maxClipboard caps the text a script puts on or reads from the clipboard.
const maxClipboard = 1 << 20

// Host is the desktop a script reaches.
type Host interface {
	SetClipboard(ctx context.Context, text string) error
	Clipboard(ctx context.Context) (string, error)
	// OpenLink opens an http or https address in the default browser.
	OpenLink(ctx context.Context, link string) error
}

// CheckLink refuses anything but an http or https address with a host.
func CheckLink(link string) error {
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("links.open: only http and https addresses can be opened")
	}
	return nil
}

// Register adds the clipboard and links modules, their permissions and block
// actions.
func Register(r *scripting.Registry, h Host) error {
	text := scripting.Required(scripting.NewParam("text", scripting.TypeText))
	link := scripting.Required(scripting.NewParam("url", scripting.TypeString))

	set := scripting.NewFunction("clipboard", "write", func(c *scripting.Call) ([]any, error) {
		s, err := c.String(0)
		if err != nil {
			return nil, err
		}
		if len(s) > maxClipboard {
			return nil, fmt.Errorf("clipboard.write: at most %d bytes", maxClipboard)
		}
		if h == nil {
			return nil, scripting.ErrUnavailable
		}
		return nil, h.SetClipboard(c.Ctx, s)
	}, text)
	set.Permission, set.Effect = PermClipboardWrite, true

	get := scripting.NewFunction("clipboard", "read", func(c *scripting.Call) ([]any, error) {
		if h == nil {
			return nil, scripting.ErrUnavailable
		}
		s, err := h.Clipboard(c.Ctx)
		if err != nil {
			return nil, err
		}
		if len(s) > maxClipboard {
			s = s[:maxClipboard]
		}
		return []any{s}, nil
	})
	get.Permission = PermClipboardRead
	get.Returns = []scripting.Field{scripting.NewField("text", "text")}

	open := scripting.NewFunction("links", "open", func(c *scripting.Call) ([]any, error) {
		s, err := c.String(0)
		if err != nil {
			return nil, err
		}
		if err := CheckLink(s); err != nil {
			return nil, err
		}
		if h == nil {
			return nil, scripting.ErrUnavailable
		}
		return nil, h.OpenLink(c.Ctx, strings.TrimSpace(s))
	}, link)
	open.Permission, open.Effect, open.Risky = PermOpenLink, true, true

	return r.Register(scripting.Entries{
		Permissions: []scripting.Permission{
			{Meta: scripting.EntryMeta("perm", PermClipboardWrite, "system"), Risk: "low"},
			{Meta: scripting.EntryMeta("perm", PermClipboardRead, "system"), Risk: "high"},
			{Meta: scripting.EntryMeta("perm", PermOpenLink, "system"), Risk: "medium"},
		},
		Modules: []scripting.Module{
			{Meta: scripting.EntryMeta("module", "clipboard", "system"), Functions: []scripting.Function{set, get}},
			{Meta: scripting.EntryMeta("module", "links", "system"), Functions: []scripting.Function{open}},
		},
		Actions: []scripting.Action{
			{Meta: scripting.EntryMeta("action", "clipboard.write", "system"), Params: []scripting.Param{text}, Permission: PermClipboardWrite, Lua: "clipboard.write({{text}})"},
			{Meta: scripting.EntryMeta("action", "links.open", "system"), Params: []scripting.Param{link}, Permission: PermOpenLink, Risky: true, Lua: "links.open({{url}})"},
		},
	})
}
