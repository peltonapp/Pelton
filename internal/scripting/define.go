package scripting

// The helpers below build registry entries with the localization keys the
// built-ins use, so another package's entries are named the same way:
// "scripts.<kind>.<id>" and "scripts.<kind>.<id>.desc" for an entry,
// "scripts.param.<name>", "scripts.field.<name>" and "scripts.option.<value>"
// for its parts, and "scripts.fn.<module>.<name>" for a function.

// EntryMeta returns the meta of an entry of kind ("trigger", "cond",
// "action", "module", "type" or "perm") in a picker category.
func EntryMeta(kind, id, category string) Meta {
	return Meta{
		ID:       id,
		LabelKey: "scripts." + kind + "." + id,
		DescKey:  "scripts." + kind + "." + id + ".desc",
		Category: category,
	}
}

// NewParam returns an optional parameter.
func NewParam(name string, t ParamType) Param {
	return Param{Name: name, Type: t, LabelKey: "scripts.param." + name}
}

// RefParam returns a parameter that takes a reference to a value of a value
// type, defaulting to ref when it is not empty, e.g. "event.task".
func RefParam(name, valueType, ref string) Param {
	p := NewParam(name, TypeRef)
	p.ValueType = valueType
	p.DefaultRef = ref
	return p
}

// Required marks a parameter as required.
func Required(p Param) Param {
	p.Required = true
	return p
}

// WithDefault gives a parameter a literal default.
func WithDefault(p Param, v any) Param {
	p.Default = v
	return p
}

// NewField returns a named, typed field.
func NewField(name, typ string) Field {
	return Field{Name: name, Type: typ, LabelKey: "scripts.field." + name}
}

// NewOptions returns enum options for the values.
func NewOptions(values ...string) []Option {
	out := make([]Option, 0, len(values))
	for _, v := range values {
		out = append(out, Option{Value: v, LabelKey: "scripts.option." + v})
	}
	return out
}

// NewFunction returns a module function with its description key.
func NewFunction(module, name string, h Handler, params ...Param) Function {
	return Function{Name: name, DescKey: "scripts.fn." + module + "." + name, Handler: h, Params: params}
}

// Register adds every entry of an Entries set in the order permissions,
// value types, triggers, modules, conditions, actions, which is the order
// their references to each other need.
func (r *Registry) Register(e Entries) error {
	for _, p := range e.Permissions {
		if err := r.RegisterPermission(p); err != nil {
			return err
		}
	}
	for _, v := range e.ValueTypes {
		if err := r.RegisterValueType(v); err != nil {
			return err
		}
	}
	for _, t := range e.Triggers {
		if err := r.RegisterTrigger(t); err != nil {
			return err
		}
	}
	for _, m := range e.Modules {
		if err := r.RegisterModule(m); err != nil {
			return err
		}
	}
	for _, c := range e.Conditions {
		if err := r.RegisterCondition(c); err != nil {
			return err
		}
	}
	for _, a := range e.Actions {
		if err := r.RegisterAction(a); err != nil {
			return err
		}
	}
	return nil
}

// Entries is a set of registry entries an area registers together.
type Entries struct {
	Permissions []Permission
	ValueTypes  []ValueType
	Triggers    []Trigger
	Modules     []Module
	Conditions  []Condition
	Actions     []Action
}
