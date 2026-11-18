package scripting

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/yuin/gopher-lua/parse"
)

// CheckLua reports a syntax error in Lua source, without running it.
func CheckLua(src string) error {
	if _, err := parse.Parse(strings.NewReader(src), "script"); err != nil {
		return fmt.Errorf("scripting: %w", err)
	}
	return nil
}

// normalize fills the defaults a script may leave out: the catch-up mode and
// cap, and the trigger parameters' defaults.
func (s *Service) normalize(sc *Script) {
	if sc.CatchUp == "" {
		sc.CatchUp = CatchUpLive
	}
	if sc.CatchUpCap <= 0 {
		sc.CatchUpCap = DefaultCatchUpCap
	}
	if sc.Origin == "" {
		sc.Origin = OriginUser
	}
	if t, ok := s.reg.Trigger(sc.Trigger.Type); ok {
		if sc.Trigger.Params == nil {
			sc.Trigger.Params = map[string]any{}
		}
		for _, p := range t.Params {
			if _, set := sc.Trigger.Params[p.Name]; !set && p.Default != nil {
				sc.Trigger.Params[p.Name] = p.Default
			}
		}
	}
	sc.Approved = slices.DeleteFunc(slices.Clone(sc.Approved), func(p string) bool {
		return !slices.Contains(sc.Permissions, p)
	})
	sc.Placements = slices.DeleteFunc(slices.Clone(sc.Placements), func(c PlacementConfig) bool {
		p, ok := s.reg.Placement(c.ID)
		return !ok || !p.fits(sc)
	})
	for name := range sc.Settings {
		if !slices.ContainsFunc(sc.SettingsSchema, func(p Param) bool { return p.Name == name }) {
			delete(sc.Settings, name)
		}
	}
}

// deriveSettings reads a Lua script's settings from its SETTINGS table. A
// script without one has no settings.
func deriveSettings(sc *Script) error {
	if sc.Kind != KindLua {
		return nil
	}
	schema, err := LuaSettings(sc.Source)
	if err != nil {
		return err
	}
	sc.SettingsSchema = schema
	return nil
}

// Create validates and stores a new script, switched off. It returns the
// stored script.
func (s *Service) Create(ctx context.Context, sc *Script) (*Script, error) {
	sc.ID = 0
	sc.Enabled = false
	sc.Approved = nil
	if err := deriveSettings(sc); err != nil {
		return nil, err
	}
	s.normalize(sc)
	if err := sc.Validate(s.reg); err != nil {
		return nil, err
	}
	id, err := s.store.Create(ctx, sc)
	if err != nil {
		return nil, err
	}
	return s.reloaded(ctx, id)
}

func (s *Service) reloaded(ctx context.Context, id int64) (*Script, error) {
	if err := s.load(ctx); err != nil {
		return nil, err
	}
	if s.cfg.OnChange != nil {
		s.cfg.OnChange()
	}
	return s.store.Get(ctx, id)
}

func (s *Service) save(ctx context.Context, sc *Script) error {
	if err := s.store.Update(ctx, sc); err != nil {
		return err
	}
	_, err := s.reloaded(ctx, sc.ID)
	return err
}

// ResetsApproval reports whether changing cur into next changes what the
// user approved: the kind, the code, the trigger or the declared
// permissions. The libraries a script requires are approved with it too; a
// change to one of them resets the scripts that load it.
func ResetsApproval(cur, next *Script) bool {
	if cur.Kind != next.Kind || cur.Source != next.Source || cur.Trigger.Type != next.Trigger.Type {
		return true
	}
	if blocksJSON(cur.Blocks) != blocksJSON(next.Blocks) {
		return true
	}
	a, b := slices.Clone(cur.Permissions), slices.Clone(next.Permissions)
	slices.Sort(a)
	slices.Sort(b)
	return !slices.Equal(slices.Compact(a), slices.Compact(b))
}

// revoke switches a script off and takes back its approvals and allowances.
func revoke(sc *Script) {
	sc.Enabled, sc.Approved, sc.AllowUnasked = false, nil, nil
}

// Update replaces a script's content: name, description, kind, code,
// trigger, declared permissions and settings schema. Its origin stays. A
// change to the code, the trigger or the permissions switches it off and
// takes back its approvals and allowances, so the user approves the new
// version; other changes keep them.
func (s *Service) Update(ctx context.Context, next *Script) (*Script, error) {
	cur, err := s.store.Get(ctx, next.ID)
	if err != nil {
		return nil, err
	}
	if ResetsApproval(cur, next) {
		revoke(cur)
	}
	cur.Name, cur.Description, cur.Kind = next.Name, next.Description, next.Kind
	cur.Source, cur.Blocks, cur.Trigger = next.Source, next.Blocks, next.Trigger
	cur.Permissions, cur.SettingsSchema = next.Permissions, next.SettingsSchema
	if next.CatchUp != "" {
		cur.CatchUp = next.CatchUp
	}
	if next.CatchUpCap > 0 {
		cur.CatchUpCap = next.CatchUpCap
	}
	if err := deriveSettings(cur); err != nil {
		return nil, err
	}
	s.normalize(cur)
	if err := cur.Validate(s.reg); err != nil {
		return nil, err
	}
	if cur.Enabled && len(cur.Unapproved()) > 0 {
		cur.Enabled = false
	}
	if cur.Enabled {
		if err := s.checkRunnable(cur); err != nil {
			cur.Enabled = false
		}
	}
	if err := s.save(ctx, cur); err != nil {
		return nil, err
	}
	return s.store.Get(ctx, cur.ID)
}

// checkRunnable makes sure the script compiles, before it is switched on.
func (s *Service) checkRunnable(sc *Script) error {
	code, err := sc.Lua(s.reg)
	if err != nil {
		return err
	}
	return CheckLua(code)
}

// SetEnabled switches a script on or off. Switching on approves the given
// permissions and needs every declared one approved, and the code to
// compile.
func (s *Service) SetEnabled(ctx context.Context, id int64, enabled bool, approve []string) (*Script, error) {
	sc, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !enabled {
		sc.Enabled = false
		if err := s.save(ctx, sc); err != nil {
			return nil, err
		}
		return s.store.Get(ctx, id)
	}
	for _, p := range approve {
		if slices.Contains(sc.Permissions, p) && !slices.Contains(sc.Approved, p) {
			sc.Approved = append(sc.Approved, p)
		}
	}
	if missing := sc.Unapproved(); len(missing) > 0 {
		return nil, fmt.Errorf("scripting: approve %s first", strings.Join(missing, ", "))
	}
	if err := s.checkRunnable(sc); err != nil {
		return nil, err
	}
	if !sc.Enabled {
		now := s.now()
		sc.Enabled = true
		sc.EnabledAt = now
		sc.AliveAt = now
		s.mu.Lock()
		s.watermarks[id] = now
		s.mu.Unlock()
	}
	if err := s.save(ctx, sc); err != nil {
		return nil, err
	}
	return s.store.Get(ctx, id)
}

// Delete removes a script, its runs and its storage.
func (s *Service) Delete(ctx context.Context, id int64) error {
	if err := s.store.Delete(ctx, id); err != nil {
		return err
	}
	s.mu.Lock()
	if w := s.workers[id]; w != nil && s.running {
		close(w.jobs)
	}
	delete(s.workers, id)
	delete(s.pending, id)
	delete(s.lastFired, id)
	delete(s.watermarks, id)
	delete(s.rate, id)
	s.mu.Unlock()
	_, err := s.reloaded(ctx, id)
	if err == ErrNotFound {
		return nil
	}
	return err
}

// SetAllowUnasked lets a script call a risky function without asking, or
// takes that back.
func (s *Service) SetAllowUnasked(ctx context.Context, id int64, function string, allow bool) (*Script, error) {
	f, ok := s.reg.Function(function)
	if !ok || !f.Risky {
		return nil, fmt.Errorf("scripting: %q is not a function that asks first", function)
	}
	sc, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	sc.AllowUnasked = slices.DeleteFunc(sc.AllowUnasked, func(x string) bool { return x == function })
	if allow {
		sc.AllowUnasked = append(sc.AllowUnasked, function)
	}
	if err := s.save(ctx, sc); err != nil {
		return nil, err
	}
	return s.store.Get(ctx, id)
}

// SetSettings stores the values of a script's settings, checked against its
// schema. Names the script does not declare are refused; a nil value goes
// back to the default.
func (s *Service) SetSettings(ctx context.Context, id int64, values map[string]any) (*Script, error) {
	sc, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	clean, err := CheckSettings(sc.SettingsSchema, values)
	if err != nil {
		return nil, err
	}
	sc.Settings = clean
	if err := s.save(ctx, sc); err != nil {
		return nil, err
	}
	return s.store.Get(ctx, id)
}

// SetCatchUp sets what a script does with mail that arrived while the app was
// closed, and how many messages it runs over at most.
func (s *Service) SetCatchUp(ctx context.Context, id int64, mode CatchUpMode, limit int) (*Script, error) {
	sc, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	sc.CatchUp = mode
	if limit > 0 {
		sc.CatchUpCap = limit
	}
	if err := sc.Validate(s.reg); err != nil {
		return nil, err
	}
	if err := s.save(ctx, sc); err != nil {
		return nil, err
	}
	return s.store.Get(ctx, id)
}

// ConvertToLua turns a block script into a Lua script for good: its source
// becomes the compiled blocks and the blocks are dropped.
func (s *Service) ConvertToLua(ctx context.Context, id int64) (*Script, error) {
	sc, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if sc.Kind != KindBlocks || sc.Blocks == nil {
		return nil, fmt.Errorf("scripting: %q is not a block script", sc.Name)
	}
	res := Compile(s.reg, *sc.Blocks, sc.Trigger.Type, false)
	if len(res.Errors) > 0 {
		return nil, res.Errors[0]
	}
	sc.Kind, sc.Source, sc.Blocks = KindLua, res.Lua, nil
	if err := s.save(ctx, sc); err != nil {
		return nil, err
	}
	return s.store.Get(ctx, id)
}

// Scripts lists every script.
func (s *Service) Scripts(ctx context.Context) ([]*Script, error) {
	return s.store.List(ctx)
}

// Script returns one script.
func (s *Service) Script(ctx context.Context, id int64) (*Script, error) {
	return s.store.Get(ctx, id)
}
