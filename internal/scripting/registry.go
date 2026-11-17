// Package scripting runs the user's automations: Lua scripts, and block
// scripts that compile to Lua, started by triggers such as mail arriving or a
// schedule.
//
// Everything a script can react to or do is declared as data in a Registry:
// triggers, conditions, actions, value types, permissions and the Lua API
// modules with their Go handlers. The block palette, the API reference and the
// editor's pickers are all built from that data, and the built-ins register
// through the same public methods any other package uses, so a new trigger or
// action never needs an edit here.
//
// The package does not know the app. It reaches mail, notifications and the
// confirmation dialog through the small interfaces in hosts.go.
package scripting

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// Meta is what every registry entry carries to be shown and found.
type Meta struct {
	// ID is unique within its kind, e.g. "mail.move".
	ID string `json:"id"`
	// LabelKey and DescKey are localization keys. Label and Description are
	// the English text shown when the frontend has no string under the key,
	// which is how a package without its own catalog entries names things.
	LabelKey    string `json:"labelKey"`
	DescKey     string `json:"descKey"`
	Label       string `json:"label,omitempty"`
	Description string `json:"description,omitempty"`
	// Category groups entries in pickers, e.g. "mail" or "schedule".
	Category string `json:"category"`
}

// Field is a named, typed value: an event field, an action output or a
// function result. Type is a ParamType or a value type id.
type Field struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	LabelKey string `json:"labelKey"`
	Label    string `json:"label,omitempty"`
	// Of is the element type of a list field, a ParamType or a value type
	// id, so a loop over it knows what each item is.
	Of string `json:"of,omitempty"`
}

// FieldCondition says which condition tests a field of a value type, so a
// rule can be made from a value without knowing condition ids.
type FieldCondition struct {
	// Field is the field's path, e.g. "subject" or "headers.list-id".
	Field string `json:"field"`
	// Condition is the condition id and Param the parameter the field's value
	// goes into; Args are fixed arguments, e.g. a header name.
	Condition string         `json:"condition"`
	Param     string         `json:"param"`
	Args      map[string]any `json:"args,omitempty"`
	// Split, when set, separates a field holding several values, e.g. a list
	// of addresses; a rule matches on the first.
	Split string `json:"split,omitempty"`
	// LabelKey and Label name the match, e.g. "Sender".
	LabelKey string `json:"labelKey"`
	Label    string `json:"label,omitempty"`
}

// Trigger is something that starts a script.
type Trigger struct {
	Meta
	// Params configure the trigger, e.g. the interval of a schedule.
	Params []Param `json:"params"`
	// Event lists the fields of the `event` table a run receives.
	Event []Field `json:"event"`
	// Permission is what a script needs approved to be started by this
	// trigger, empty when none.
	Permission string `json:"permission,omitempty"`
	// Mutable means the script may change the event (event.draft for a
	// message about to be sent) and the caller reads it back.
	Mutable bool `json:"mutable,omitempty"`
	// Missing names the app hook that would deliver this trigger when the app
	// has none yet. Such a trigger is listed but never fires.
	Missing string `json:"missing,omitempty"`

	// Prepare turns the raw event data into the event table, e.g. loading
	// the message behind a message id. Nil passes the data through.
	Prepare func(c *PrepareContext, data map[string]any) (map[string]any, error) `json:"-"`
	// Match reports whether a prepared event concerns a script configured
	// with params. Nil matches every event.
	Match func(params map[string]any, event map[string]any) bool `json:"-"`
	// Due reports whether a scheduled trigger should fire at now, given when
	// it last fired (zero when never) and when it was set up. Nil for
	// triggers the app delivers as events.
	Due func(params map[string]any, last, since, now time.Time) bool `json:"-"`
}

// Condition is a yes or no question a block script can ask.
type Condition struct {
	Meta
	Params     []Param `json:"params"`
	Permission string  `json:"permission,omitempty"`
	// Permissions lists every permission the condition needs. Registration
	// puts Permission first in it, so either field may be used to declare.
	Permissions []string `json:"permissions,omitempty"`
	// Lua is the expression the condition compiles to, with {{param}}
	// placeholders, e.g. `text.test({{message}}.subject, {{op}}, {{value}})`.
	Lua string `json:"lua"`
	// Field names the message field a text condition tests, e.g. "subject",
	// "body" or "headers" (with the header in a "name" argument), so the
	// match tester can show where its pattern matched. Empty for others.
	Field string `json:"field,omitempty"`
}

// Action is a step a block script can take.
type Action struct {
	Meta
	Params []Param `json:"params"`
	// Outputs are the fields of the action's result, which later blocks can
	// refer to as step:<block id>.<field>.
	Outputs []Field `json:"outputs,omitempty"`
	// Permission is what the action needs, so the editor can add it to the
	// script's declared permissions. The host function enforces it.
	Permission string `json:"permission,omitempty"`
	// Permissions lists every permission the action needs, for an action
	// that calls more than one function. Registration puts Permission first
	// in it.
	Permissions []string `json:"permissions,omitempty"`
	Risky       bool     `json:"risky,omitempty"`
	// Lua is the call the action compiles to, with {{param}} placeholders.
	Lua string `json:"lua"`
	// Triggers limits the action to scripts started by these triggers, e.g.
	// stopping a send only means something before a send. Empty allows all.
	Triggers []string `json:"triggers,omitempty"`
	// Missing names what the app lacks for this action, which is listed but
	// refused by the compiler.
	Missing string `json:"missing,omitempty"`
}

// Module is a table of Go functions exposed to Lua under Meta.ID, e.g. mail.
type Module struct {
	Meta
	Functions []Function `json:"functions"`
}

// Function is one host function of a module.
type Function struct {
	// Name is the Lua name within the module, e.g. "move".
	Name        string  `json:"name"`
	DescKey     string  `json:"descKey"`
	Description string  `json:"description,omitempty"`
	Params      []Param `json:"params"`
	Returns     []Field `json:"returns,omitempty"`
	// Permission must be declared and approved for the call to run.
	Permission string `json:"permission,omitempty"`
	// Permissions lists every permission the call needs; all must be held.
	// Registration puts Permission first in it.
	Permissions []string `json:"permissions,omitempty"`
	// Risky calls ask the user first unless the script is allowed to make
	// them without asking.
	Risky bool `json:"risky,omitempty"`
	// Effect marks a call that changes something outside the run. A dry run
	// records it as a planned step instead of making it.
	Effect bool `json:"effect,omitempty"`

	Handler Handler `json:"-"`
	// Dry, when set, answers the call in a dry run in place of a plain
	// planned step, so a script that uses the result can keep going.
	Dry Handler `json:"-"`
	// Summary, when set, describes the call in the run log and in a
	// confirmation in place of its arguments, for arguments that must not be
	// logged.
	Summary func(args []any) string `json:"-"`
}

// Handler implements a host function. It returns the Lua results.
type Handler func(c *Call) ([]any, error)

// ValueType describes a structured value a script handles, e.g. a message.
type ValueType struct {
	Meta
	Fields []Field `json:"fields"`
	// Lazy maps a field that is fetched on first use to the module function
	// that fetches it, e.g. "body" to "mail.body".
	Lazy map[string]string `json:"lazy,omitempty"`
	// Conditions are the conditions that test the type's fields.
	Conditions []FieldCondition `json:"conditions,omitempty"`
	// Sample returns the fields of an example value, which a test run with
	// nothing picked hands a script for a trigger field of this type.
	Sample func(now time.Time) map[string]any `json:"-"`
}

// Permission is something a script must declare and the user approve.
type Permission struct {
	Meta
	// Risk is "low", "medium" or "high", for how the approval dialog warns.
	Risk string `json:"risk"`
}

// Catalog is the whole registry as data, in registration order.
type Catalog struct {
	Triggers    []Trigger    `json:"triggers"`
	Conditions  []Condition  `json:"conditions"`
	Actions     []Action     `json:"actions"`
	Modules     []Module     `json:"modules"`
	ValueTypes  []ValueType  `json:"valueTypes"`
	Permissions []Permission `json:"permissions"`
	ParamTypes  []ParamType  `json:"paramTypes"`
	// SettingTypes are the parameter types a script setting may have.
	SettingTypes  []ParamType    `json:"settingTypes"`
	InputKinds    []InputKind    `json:"inputKinds"`
	ResultActions []ResultAction `json:"resultActions"`
	Placements    []Placement    `json:"placements"`
	Templates     []Template     `json:"templates"`
}

// Registry holds everything scripts can use. It is safe for concurrent use;
// entries are normally registered at startup and only read afterwards.
type Registry struct {
	mu            sync.RWMutex
	triggers      entries[Trigger]
	conditions    entries[Condition]
	actions       entries[Action]
	modules       entries[Module]
	valueTypes    entries[ValueType]
	permissions   entries[Permission]
	inputKinds    entries[InputKind]
	resultActions entries[ResultAction]
	placements    entries[Placement]
	templates     entries[Template]
}

type entries[T any] struct {
	order []string
	byID  map[string]T
}

func (e *entries[T]) add(id string, v T) error {
	if e.byID == nil {
		e.byID = map[string]T{}
	}
	if _, dup := e.byID[id]; dup {
		return fmt.Errorf("scripting: %q is already registered", id)
	}
	e.byID[id] = v
	e.order = append(e.order, id)
	return nil
}

func (e *entries[T]) list() []T {
	out := make([]T, 0, len(e.order))
	for _, id := range e.order {
		out = append(out, e.byID[id])
	}
	return out
}

// NewRegistry returns an empty registry. NewService fills its own with the
// built-ins; tests and other callers can start from nothing.
func NewRegistry() *Registry {
	return &Registry{}
}

var (
	idPattern  = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$`)
	luaName    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	permPatten = regexp.MustCompile(`^[a-z][a-z0-9_.]*(:[A-Za-z0-9_.*-]+)?$`)
	holeRE     = regexp.MustCompile(`\{\{([A-Za-z_][A-Za-z0-9_]*)\}\}`)
)

// luaKeywords cannot name a module or function.
var luaKeywords = []string{
	"and", "break", "do", "else", "elseif", "end", "false", "for", "function", "if", "in",
	"local", "nil", "not", "or", "repeat", "return", "then", "true", "until", "while",
}

// reservedGlobals are names the engine itself defines in a run.
var reservedGlobals = []string{"event", "script", "stop", "vars", "steps", "print", "require"}

func checkMeta(m Meta) error {
	if !idPattern.MatchString(m.ID) {
		return fmt.Errorf("scripting: %q is not a valid id", m.ID)
	}
	if m.LabelKey == "" && m.Label == "" {
		return fmt.Errorf("scripting: %q needs a label key or a label", m.ID)
	}
	return nil
}

func (r *Registry) checkPermission(id, owner string) error {
	if id == "" {
		return nil
	}
	base, _, _ := strings.Cut(id, ":")
	if _, ok := r.permissions.byID[base]; !ok {
		return fmt.Errorf("scripting: %q needs permission %q, which is not registered", owner, id)
	}
	return nil
}

// permList returns single followed by the entries of list, each once.
func permList(single string, list []string) []string {
	var out []string
	for _, p := range append([]string{single}, list...) {
		if p != "" && !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	return out
}

func (r *Registry) checkPermissions(ids []string, owner string) error {
	for _, id := range ids {
		if err := r.checkPermission(id, owner); err != nil {
			return err
		}
	}
	return nil
}

func (r *Registry) checkParams(owner string, params []Param) error {
	seen := map[string]bool{}
	for _, p := range params {
		if !luaName.MatchString(p.Name) {
			return fmt.Errorf("scripting: %q has a parameter with the invalid name %q", owner, p.Name)
		}
		if seen[p.Name] {
			return fmt.Errorf("scripting: %q declares parameter %q twice", owner, p.Name)
		}
		seen[p.Name] = true
		if err := p.validate(); err != nil {
			return fmt.Errorf("scripting: %q parameter %q: %w", owner, p.Name, err)
		}
	}
	return nil
}

func checkTemplate(owner, tmpl string, params []Param) error {
	if strings.TrimSpace(tmpl) == "" {
		return fmt.Errorf("scripting: %q has no Lua", owner)
	}
	for _, m := range holeRE.FindAllStringSubmatch(tmpl, -1) {
		if !slices.ContainsFunc(params, func(p Param) bool { return p.Name == m[1] }) {
			return fmt.Errorf("scripting: %q uses {{%s}}, which is not a parameter", owner, m[1])
		}
	}
	return nil
}

// RegisterPermission adds a permission. Its id is the base a script declares,
// optionally followed by ":" and a target, e.g. "net" for "net:example.org".
func (r *Registry) RegisterPermission(p Permission) error {
	if err := checkMeta(p.Meta); err != nil {
		return err
	}
	switch p.Risk {
	case "low", "medium", "high":
	default:
		return fmt.Errorf("scripting: permission %q has risk %q, want low, medium or high", p.ID, p.Risk)
	}
	englishMeta(&p.Meta)
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.permissions.add(p.ID, p)
}

// RegisterValueType adds a value type.
func (r *Registry) RegisterValueType(v ValueType) error {
	if err := checkMeta(v.Meta); err != nil {
		return err
	}
	englishMeta(&v.Meta)
	v.Fields = englishFields(v.Fields)
	v.Conditions = slices.Clone(v.Conditions)
	for i := range v.Conditions {
		if v.Conditions[i].Label == "" {
			v.Conditions[i].Label = englishFor(v.Conditions[i].LabelKey)
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.valueTypes.add(v.ID, v)
}

// RegisterTrigger adds a trigger.
func (r *Registry) RegisterTrigger(t Trigger) error {
	if err := checkMeta(t.Meta); err != nil {
		return err
	}
	englishMeta(&t.Meta)
	t.Params = englishParams(t.Params)
	t.Event = englishFields(t.Event)
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.checkParams(t.ID, t.Params); err != nil {
		return err
	}
	if err := r.checkPermission(t.Permission, t.ID); err != nil {
		return err
	}
	return r.triggers.add(t.ID, t)
}

// RegisterCondition adds a block condition.
func (r *Registry) RegisterCondition(c Condition) error {
	if err := checkMeta(c.Meta); err != nil {
		return err
	}
	c.Permissions = permList(c.Permission, c.Permissions)
	englishMeta(&c.Meta)
	c.Params = englishParams(c.Params)
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.checkParams(c.ID, c.Params); err != nil {
		return err
	}
	if err := r.checkPermissions(c.Permissions, c.ID); err != nil {
		return err
	}
	if err := checkTemplate(c.ID, c.Lua, c.Params); err != nil {
		return err
	}
	return r.conditions.add(c.ID, c)
}

// RegisterAction adds a block action.
func (r *Registry) RegisterAction(a Action) error {
	if err := checkMeta(a.Meta); err != nil {
		return err
	}
	a.Permissions = permList(a.Permission, a.Permissions)
	englishMeta(&a.Meta)
	a.Params = englishParams(a.Params)
	a.Outputs = englishFields(a.Outputs)
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.checkParams(a.ID, a.Params); err != nil {
		return err
	}
	if err := r.checkPermissions(a.Permissions, a.ID); err != nil {
		return err
	}
	if err := checkTemplate(a.ID, a.Lua, a.Params); err != nil {
		return err
	}
	return r.actions.add(a.ID, a)
}

// RegisterModule adds a Lua module. Its id is its global name in Lua, so it
// must be a plain identifier that no other module or the engine uses.
func (r *Registry) RegisterModule(m Module) error {
	if err := checkMeta(m.Meta); err != nil {
		return err
	}
	if !luaName.MatchString(m.ID) || strings.HasPrefix(m.ID, "__") || slices.Contains(luaKeywords, m.ID) || slices.Contains(reservedGlobals, m.ID) || slices.Contains(sandboxGlobals, m.ID) {
		return fmt.Errorf("scripting: %q cannot be a module name", m.ID)
	}
	englishMeta(&m.Meta)
	m.Functions = slices.Clone(m.Functions)
	for i := range m.Functions {
		f := &m.Functions[i]
		f.Permissions = permList(f.Permission, f.Permissions)
		if f.Description == "" {
			f.Description = englishFor(f.DescKey)
		}
		f.Params = englishParams(f.Params)
		f.Returns = englishFields(f.Returns)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	names := map[string]bool{}
	for _, f := range m.Functions {
		owner := m.ID + "." + f.Name
		if !luaName.MatchString(f.Name) || slices.Contains(luaKeywords, f.Name) {
			return fmt.Errorf("scripting: %q is not a valid function name", owner)
		}
		if names[f.Name] {
			return fmt.Errorf("scripting: %q is registered twice", owner)
		}
		names[f.Name] = true
		if f.Handler == nil {
			return fmt.Errorf("scripting: %q has no handler", owner)
		}
		if err := r.checkParams(owner, f.Params); err != nil {
			return err
		}
		if err := r.checkPermissions(f.Permissions, owner); err != nil {
			return err
		}
	}
	return r.modules.add(m.ID, m)
}

// SetMissing marks the trigger and the action registered under id as not
// available, with reason, or as available again for "". It is for a feature
// the user can switch off while the app runs, such as chat: the entries stay
// listed, the compiler refuses the action and the trigger never fires. It
// reports whether id names a trigger or an action.
func (r *Registry) SetMissing(id, reason string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	found := false
	if t, ok := r.triggers.byID[id]; ok {
		t.Missing = reason
		r.triggers.byID[id] = t
		found = true
	}
	if a, ok := r.actions.byID[id]; ok {
		a.Missing = reason
		r.actions.byID[id] = a
		found = true
	}
	return found
}

// Trigger returns a registered trigger.
func (r *Registry) Trigger(id string) (Trigger, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.triggers.byID[id]
	return t, ok
}

// Condition returns a registered condition.
func (r *Registry) Condition(id string) (Condition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.conditions.byID[id]
	return c, ok
}

// Action returns a registered action.
func (r *Registry) Action(id string) (Action, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.actions.byID[id]
	return a, ok
}

// ValueType returns a registered value type.
func (r *Registry) ValueType(id string) (ValueType, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.valueTypes.byID[id]
	return v, ok
}

// Permission returns a registered permission by its base id; "net:x.org"
// finds "net".
func (r *Registry) Permission(id string) (Permission, bool) {
	base, _, _ := strings.Cut(id, ":")
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.permissions.byID[base]
	return p, ok
}

// Modules returns every module in registration order.
func (r *Registry) Modules() []Module {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.modules.list()
}

// Function finds a host function by its full Lua name, e.g. "mail.move".
func (r *Registry) Function(full string) (Function, bool) {
	mod, name, ok := strings.Cut(full, ".")
	if !ok {
		return Function{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	m, ok := r.modules.byID[mod]
	if !ok {
		return Function{}, false
	}
	for _, f := range m.Functions {
		if f.Name == name {
			return f, true
		}
	}
	return Function{}, false
}

// Catalog returns the registry as data for the editor and the references.
func (r *Registry) Catalog() Catalog {
	c := r.entriesCatalog()
	c.Templates = r.Templates()
	return c
}

func (r *Registry) entriesCatalog() Catalog {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return Catalog{
		Triggers:      r.triggers.list(),
		Conditions:    r.conditions.list(),
		Actions:       r.actions.list(),
		Modules:       r.modules.list(),
		ValueTypes:    r.valueTypes.list(),
		Permissions:   r.permissions.list(),
		ParamTypes:    slices.Clone(paramTypes),
		SettingTypes:  slices.Clone(settingTypes),
		InputKinds:    r.inputKinds.list(),
		ResultActions: r.resultActions.list(),
		Placements:    r.placements.list(),
	}
}

// ValidPermission reports whether id names a registered permission in a form
// a script may declare.
func (r *Registry) ValidPermission(id string) bool {
	if !permPatten.MatchString(id) {
		return false
	}
	_, ok := r.Permission(id)
	return ok
}
