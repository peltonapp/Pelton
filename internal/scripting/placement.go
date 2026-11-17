package scripting

import (
	"context"
	"fmt"
	"slices"
	"strconv"
)

// The surfaces a placement can be on. The frontend decides how each looks.
const (
	// SurfaceMenu is a context menu of an item, e.g. a mail row.
	SurfaceMenu = "menu"
	// SurfacePalette is the command palette.
	SurfacePalette = "palette"
	// SurfaceToolbar is a toolbar button.
	SurfaceToolbar = "toolbar"
	// SurfaceShortcut is a keyboard shortcut.
	SurfaceShortcut = "shortcut"
	// SurfaceRow is a mail row's quick buttons and swipes.
	SurfaceRow = "row"
)

// Placement is a place in the app a script can be started from by hand: a
// context menu, the command palette, a toolbar button, a shortcut or a row
// action. Each hands the script the value it was used on as the manual
// trigger's event, so a script is only offered where its trigger takes that
// value.
type Placement struct {
	Meta
	// Surface is the kind of place, one of the Surface constants or another
	// the frontend knows.
	Surface string `json:"surface"`
	// ValueType is the value type the place hands over, e.g. "message",
	// empty for none. With Optional set it hands one over only when there is
	// one, e.g. the palette with nothing selected.
	ValueType string `json:"valueType,omitempty"`
	Optional  bool   `json:"optional,omitempty"`
	// List names the event field a selection of several arrives in, e.g.
	// "messages", empty when the place hands over one value.
	List string `json:"list,omitempty"`
	// Permission is what a script must hold, approved, to be offered here.
	Permission string `json:"permission,omitempty"`
	// Options are what a script sets for the place, e.g. the icon and label
	// of a toolbar button.
	Options []Param `json:"options,omitempty"`
	// Load fetches the value behind an id the frontend sends. Nil takes the
	// fields the frontend sends, keeping those the value type declares.
	Load func(c *PrepareContext, id string) (Object, error) `json:"-"`
}

// PlacementConfig is a place a script is offered at, with its options.
type PlacementConfig struct {
	ID      string         `json:"id"`
	Options map[string]any `json:"options,omitempty"`
}

// PlacedItem is one value a place hands a script: an id to load, or the
// value's fields.
type PlacedItem struct {
	ID     string         `json:"id"`
	Fields map[string]any `json:"fields,omitempty"`
}

// PlacedScript is a script offered at a place.
type PlacedScript struct {
	ScriptID    int64          `json:"scriptId"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Options     map[string]any `json:"options"`
}

// maxPlacedItems caps the values one placed run gets.
const maxPlacedItems = 500

// RegisterPlacement adds a place scripts can be started from. A placement
// that hands over a value adds that value type to the manual trigger's
// scopes and its event, so scripts can declare they take it.
func (r *Registry) RegisterPlacement(p Placement) error {
	if err := checkMeta(p.Meta); err != nil {
		return err
	}
	if p.Surface == "" {
		return fmt.Errorf("scripting: placement %q needs a surface", p.ID)
	}
	if p.List != "" && (p.ValueType == "" || !luaName.MatchString(p.List)) {
		return fmt.Errorf("scripting: placement %q has an invalid list field %q", p.ID, p.List)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.checkParams(p.ID, p.Options); err != nil {
		return err
	}
	if err := r.checkPermission(p.Permission, p.ID); err != nil {
		return err
	}
	if p.ValueType != "" {
		if _, ok := r.valueTypes.byID[p.ValueType]; !ok {
			return fmt.Errorf("scripting: placement %q hands over %q, which is not a value type", p.ID, p.ValueType)
		}
	}
	if err := r.placements.add(p.ID, p); err != nil {
		return err
	}
	r.acceptOnManual(p)
	return nil
}

// acceptOnManual lets the manual trigger take a placement's value: a scope
// option and an event field named after the value type, and the list field.
func (r *Registry) acceptOnManual(p Placement) {
	t, ok := r.triggers.byID[TriggerManual]
	if !ok || p.ValueType == "" {
		return
	}
	t.Params = slices.Clone(t.Params)
	if i := slices.IndexFunc(t.Params, func(x Param) bool { return x.Name == "scope" }); i >= 0 {
		scope := t.Params[i]
		if !slices.ContainsFunc(scope.Options, func(o Option) bool { return o.Value == p.ValueType }) {
			vt := r.valueTypes.byID[p.ValueType]
			scope.Options = append(slices.Clone(scope.Options), Option{Value: p.ValueType, LabelKey: vt.LabelKey, Label: vt.Label})
			t.Params[i] = scope
		}
	}
	addField := func(f Field) {
		if !slices.ContainsFunc(t.Event, func(x Field) bool { return x.Name == f.Name }) {
			t.Event = append(slices.Clone(t.Event), f)
		}
	}
	addField(NewField(p.ValueType, p.ValueType))
	if p.List != "" {
		f := NewField(p.List, string(TypeList))
		f.Of = p.ValueType
		addField(f)
	}
	r.triggers.byID[TriggerManual] = t
}

// Placement returns a registered placement.
func (r *Registry) Placement(id string) (Placement, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.placements.byID[id]
	return p, ok
}

// scriptScope is the value a manual script takes, "none" for none.
func scriptScope(sc *Script) string {
	if s, ok := sc.Trigger.Params["scope"].(string); ok && s != "" {
		return s
	}
	return "none"
}

// fits reports whether a script's trigger takes what a placement hands over.
func (p Placement) fits(sc *Script) bool {
	if sc.Trigger.Type != TriggerManual {
		return false
	}
	scope := scriptScope(sc)
	switch {
	case p.ValueType == "":
		return scope == "none"
	case p.Optional:
		return scope == "none" || scope == p.ValueType
	}
	return scope == p.ValueType
}

// offered reports whether a script shows at a placement: switched on, placed
// there, taking its value and holding its permissions.
func (p Placement) offered(sc *Script) (PlacementConfig, bool) {
	if !sc.Enabled || !p.fits(sc) || !sc.granted(p.Permission, false) || len(sc.Unapproved()) > 0 {
		return PlacementConfig{}, false
	}
	i := slices.IndexFunc(sc.Placements, func(c PlacementConfig) bool { return c.ID == p.ID })
	if i < 0 {
		return PlacementConfig{}, false
	}
	return sc.Placements[i], true
}

// checkPlacements validates a script's placements against the registry.
func (r *Registry) checkPlacements(sc *Script) error {
	seen := map[string]bool{}
	for _, c := range sc.Placements {
		p, ok := r.Placement(c.ID)
		if !ok {
			return fmt.Errorf("scripting: unknown placement %q", c.ID)
		}
		if seen[c.ID] {
			return fmt.Errorf("scripting: placement %q is listed twice", c.ID)
		}
		seen[c.ID] = true
		if !p.fits(sc) {
			return fmt.Errorf("scripting: the script's trigger does not take what %q hands over", c.ID)
		}
		for name, v := range c.Options {
			i := slices.IndexFunc(p.Options, func(o Param) bool { return o.Name == name })
			if i < 0 {
				return fmt.Errorf("scripting: placement %q has no option %q", c.ID, name)
			}
			if v == nil {
				continue
			}
			if _, err := literalLua(p.Options[i], v); err != nil {
				return fmt.Errorf("scripting: placement %q option %q: %w", c.ID, name, err)
			}
		}
	}
	return nil
}

// Placed lists, per placement, the scripts offered there in name order.
func (s *Service) Placed() map[string][]PlacedScript {
	s.mu.Lock()
	list := make([]*Script, 0, len(s.scripts))
	for _, sc := range s.scripts {
		list = append(list, sc)
	}
	s.mu.Unlock()
	slices.SortFunc(list, func(a, b *Script) int {
		if a.Name != b.Name {
			if a.Name < b.Name {
				return -1
			}
			return 1
		}
		return int(a.ID - b.ID)
	})
	out := map[string][]PlacedScript{}
	for _, p := range s.reg.Catalog().Placements {
		for _, sc := range list {
			c, ok := p.offered(sc)
			if !ok {
				continue
			}
			opts := c.Options
			if opts == nil {
				opts = map[string]any{}
			}
			out[p.ID] = append(out[p.ID], PlacedScript{ScriptID: sc.ID, Name: sc.Name, Description: sc.Description, Options: opts})
		}
	}
	return out
}

// SetPlacements sets where a script is offered.
func (s *Service) SetPlacements(ctx context.Context, id int64, list []PlacementConfig) (*Script, error) {
	sc, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	sc.Placements = list
	if err := s.reg.checkPlacements(sc); err != nil {
		return nil, err
	}
	if err := s.save(ctx, sc); err != nil {
		return nil, err
	}
	return s.store.Get(ctx, id)
}

// placedEvent builds the raw event a placement hands a script: the first
// value under the value type's name, and every value in the list field.
func (s *Service) placedEvent(ctx context.Context, p Placement, items []PlacedItem) (map[string]any, error) {
	data := map[string]any{}
	if p.ValueType == "" || len(items) == 0 {
		return data, nil
	}
	if len(items) > maxPlacedItems {
		return nil, fmt.Errorf("scripting: a script takes at most %d items at once", maxPlacedItems)
	}
	if p.List == "" {
		items = items[:1]
	}
	vt, _ := s.reg.ValueType(p.ValueType)
	objs := make([]any, 0, len(items))
	for _, it := range items {
		var obj Object
		if p.Load != nil {
			o, err := p.Load(&PrepareContext{Ctx: ctx, Hosts: s.cfg.Hosts}, it.ID)
			if err != nil {
				return nil, err
			}
			obj = o
		} else {
			fields := map[string]any{}
			for _, f := range vt.Fields {
				if v, ok := it.Fields[f.Name]; ok {
					fields[f.Name] = v
				}
			}
			obj = Object{Type: p.ValueType, Fields: fields}
		}
		objs = append(objs, obj)
	}
	data[p.ValueType] = objs[0]
	if p.List != "" {
		data[p.List] = objs
	}
	return data, nil
}

// RunPlaced runs a script from a placement on the values it was used on,
// and waits for the result. The script must be offered there.
func (s *Service) RunPlaced(ctx context.Context, id int64, placement string, items []PlacedItem) (*Run, error) {
	p, ok := s.reg.Placement(placement)
	if !ok {
		return nil, fmt.Errorf("scripting: unknown placement %q", placement)
	}
	sc, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if _, ok := p.offered(sc); !ok {
		return nil, fmt.Errorf("scripting: %q is not offered here", sc.Name)
	}
	data, err := s.placedEvent(ctx, p, items)
	if err != nil {
		return nil, err
	}
	t, _ := s.reg.Trigger(sc.Trigger.Type)
	event, err := s.prepare(ctx, t, data)
	if err != nil {
		return nil, err
	}
	w := s.workerFor(id)
	w.mu.Lock()
	defer w.mu.Unlock()
	return s.run(ctx, sc, sc.Trigger.Type, event, EventDepth(ctx), false, true)
}

// Value loads a value of a value type by its id, through a placement that
// hands such values over, and returns its fields as plain data. It is how the
// editor reads an item to build a rule from it.
func (s *Service) Value(ctx context.Context, valueType, id string) (map[string]any, error) {
	for _, p := range s.reg.Catalog().Placements {
		if p.ValueType != valueType || p.Load == nil {
			continue
		}
		obj, err := p.Load(&PrepareContext{Ctx: ctx, Hosts: s.cfg.Hosts}, id)
		if err != nil {
			return nil, err
		}
		fields, _ := plainValue(obj.Fields).(map[string]any)
		return fields, nil
	}
	return nil, fmt.Errorf("scripting: no way to load a %q", valueType)
}

// MessageLoad loads a message placement's value by its id.
func MessageLoad(c *PrepareContext, id string) (Object, error) {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return Object{}, fmt.Errorf("scripting: %q is not a message id", id)
	}
	if c.Hosts == nil || c.Hosts.Mail == nil {
		return Object{}, ErrUnavailable
	}
	m, err := c.Hosts.Mail.Message(c.Ctx, n)
	if err != nil {
		return Object{}, fmt.Errorf("scripting: load message %d: %w", n, err)
	}
	return MessageObject(m), nil
}

func registerMailPlacements(r *Registry) error {
	label := Required(NewParam("label", TypeString))
	icon := Required(NewParam("icon", TypeIcon))
	button := []Param{icon, label}
	for _, p := range []Placement{
		{Meta: meta("place", "mail.row", "mail"), Surface: SurfaceMenu, ValueType: "message", List: "messages", Load: MessageLoad},
		{Meta: meta("place", "mail.message", "mail"), Surface: SurfaceMenu, ValueType: "message", Load: MessageLoad},
		{Meta: meta("place", "palette", "app"), Surface: SurfacePalette, ValueType: "message", Optional: true, List: "messages", Load: MessageLoad},
		{Meta: meta("place", "toolbar.message", "mail"), Surface: SurfaceToolbar, ValueType: "message", Options: button, Load: MessageLoad},
		{Meta: meta("place", "toolbar.list", "mail"), Surface: SurfaceToolbar, ValueType: "message", List: "messages", Options: button, Load: MessageLoad},
		{Meta: meta("place", "shortcut", "app"), Surface: SurfaceShortcut, ValueType: "message", Optional: true, List: "messages", Load: MessageLoad},
		{Meta: meta("place", "row.hover", "mail"), Surface: SurfaceRow, ValueType: "message", Options: []Param{icon}, Load: MessageLoad},
		{Meta: meta("place", "row.swipe", "mail"), Surface: SurfaceRow, ValueType: "message", Options: []Param{icon}, Load: MessageLoad},
	} {
		if err := r.RegisterPlacement(p); err != nil {
			return err
		}
	}
	return nil
}
