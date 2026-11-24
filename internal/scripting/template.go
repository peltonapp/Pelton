package scripting

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

// Template is a ready made script the user copies from the gallery. It is
// held as the text of a .peltonscript file; its name and description come
// from the file unless the frontend has strings under its keys
// ("scripts.template.<id>" and ".desc").
type Template struct {
	Meta
	Kind     Kind          `json:"kind"`
	Trigger  TriggerConfig `json:"trigger"`
	Blocks   *Tree         `json:"blocks,omitempty"`
	Source   string        `json:"source,omitempty"`
	Settings []Param       `json:"settings,omitempty"`
	// Permissions are what the template declares together with what its
	// blocks need, and AI says whether one of them sends data to an AI
	// provider.
	Permissions []string `json:"permissions"`
	AI          bool     `json:"ai"`
	// Requires names the libraries the template loads.
	Requires []string `json:"requires,omitempty"`
	// Missing says why the template would not compile with what this app
	// offers now, e.g. an AI step without a provider; empty when it does.
	Missing string `json:"missing,omitempty"`

	data []byte
}

// RegisterTemplate adds a template from the text of a .peltonscript file,
// under id, in a gallery category. The file is read here; whether its
// triggers and blocks exist is worked out when the templates are listed, so
// a template may use entries another package registers later.
func (r *Registry) RegisterTemplate(id, category string, data []byte) error {
	f, err := readFile(data)
	if err != nil {
		return fmt.Errorf("scripting: template %q: %w", id, err)
	}
	if f.Kind != KindLua && f.Kind != KindBlocks {
		return fmt.Errorf("scripting: template %q is not a script", id)
	}
	m := EntryMeta("template", id, category)
	m.Label, m.Description = f.Name, f.Description
	if err := checkMeta(m); err != nil {
		return err
	}
	t := Template{
		Meta: m, Kind: f.Kind, Trigger: f.Trigger, Blocks: f.Blocks, Source: f.Source,
		Settings: f.Settings, Permissions: nonNil(f.Permissions), Requires: fileRequires(f),
		data: slices.Clone(data),
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.templates.add(id, t)
}

// Templates lists the templates in registration order, with what each needs
// worked out against the registry as it is now.
func (r *Registry) Templates() []Template {
	r.mu.RLock()
	list := r.templates.list()
	r.mu.RUnlock()
	for i := range list {
		r.settle(&list[i])
	}
	return list
}

// Template returns one template.
func (r *Registry) Template(id string) (Template, bool) {
	r.mu.RLock()
	t, ok := r.templates.byID[id]
	r.mu.RUnlock()
	if ok {
		r.settle(&t)
	}
	return t, ok
}

// settle fills a template's permissions from its blocks, whether it uses AI
// and what keeps it from compiling.
func (r *Registry) settle(t *Template) {
	t.Missing = ""
	perms := slices.Clone(t.Permissions)
	if t.Kind == KindBlocks && t.Blocks != nil {
		res := Compile(r, *t.Blocks, t.Trigger.Type, false)
		if len(res.Errors) > 0 {
			t.Missing = strings.TrimPrefix(res.Errors[0].Error(), "scripting: ")
		}
		for _, p := range res.Permissions {
			if !slices.Contains(perms, p) {
				perms = append(perms, p)
			}
		}
	}
	if _, ok := r.Trigger(t.Trigger.Type); !ok && t.Missing == "" {
		t.Missing = fmt.Sprintf("there is no trigger %q", t.Trigger.Type)
	}
	t.Permissions = perms
	t.AI = slices.ContainsFunc(perms, func(p string) bool {
		def, ok := r.Permission(p)
		return ok && def.Category == "ai"
	})
}

// FromTemplate stores a copy of a template as a new script of the user's,
// switched off, to be looked over and changed like any other.
func (s *Service) FromTemplate(ctx context.Context, id string) (*Script, error) {
	r := s.reg
	r.mu.RLock()
	t, ok := r.templates.byID[id]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("scripting: there is no template %q", id)
	}
	sc, err := ParseFile(r, t.data)
	if err != nil {
		return nil, err
	}
	sc.Origin = OriginUser
	if sc.Kind == KindBlocks && sc.Blocks != nil {
		res := Compile(r, *sc.Blocks, sc.Trigger.Type, false)
		for _, p := range res.Permissions {
			if !slices.Contains(sc.Permissions, p) {
				sc.Permissions = append(sc.Permissions, p)
			}
		}
	}
	return s.Create(ctx, sc)
}
