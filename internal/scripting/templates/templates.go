// Package templates holds the scripts Pelton ships for the Automations
// gallery, as .peltonscript files, and registers them. The file name less its
// extension is the template's id.
package templates

import (
	"embed"

	"github.com/peltonapp/Pelton/internal/scripting"
)

//go:embed *.peltonscript
var content embed.FS

// builtin lists the templates in gallery order, each with its category.
var builtin = []struct{ id, category string }{
	{"newsletters", "mail"},
	{"sender_domain", "mail"},
	{"flagged_task", "tasks"},
	{"no_reply", "mail"},
	{"old_notifications", "schedule"},
	{"unread_digest", "schedule"},
	{"reply_contacts", "contacts"},
	{"ai_summary", "ai"},
	{"ai_sort", "ai"},
	{"ai_reply", "ai"},
}

// Register adds the built-in templates to r.
func Register(r *scripting.Registry) error {
	for _, t := range builtin {
		data, err := content.ReadFile(t.id + scripting.FileExtension)
		if err != nil {
			return err
		}
		if err := r.RegisterTemplate(t.id, t.category, data); err != nil {
			return err
		}
	}
	return nil
}
