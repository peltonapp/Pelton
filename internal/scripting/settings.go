package scripting

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/yuin/gopher-lua/ast"
	"github.com/yuin/gopher-lua/parse"
)

// SettingsGlobal is the global a Lua script declares its settings in:
//
//	SETTINGS = {
//	  { name = "folder", type = "folder", label = "Target folder", default = "Archive" },
//	  { name = "mode", type = "enum", options = { "fast", "slow" }, default = "fast" },
//	}
//
// Each entry takes name, type, label, description, default, required and,
// for an enum, options as values or { value = ..., label = ... } tables.
const SettingsGlobal = "SETTINGS"

// LuaSettings reads the settings a Lua script declares in its top level
// SETTINGS table, without running anything: the table must be written out
// as constants. A script without the table has no settings.
func LuaSettings(src string) ([]Param, error) {
	stmts, err := parse.Parse(strings.NewReader(src), "script")
	if err != nil {
		return nil, nil
	}
	var expr ast.Expr
	for _, s := range stmts {
		switch st := s.(type) {
		case *ast.AssignStmt:
			for i, lhs := range st.Lhs {
				if id, ok := lhs.(*ast.IdentExpr); ok && id.Value == SettingsGlobal && i < len(st.Rhs) {
					expr = st.Rhs[i]
				}
			}
		case *ast.LocalAssignStmt:
			for i, name := range st.Names {
				if name == SettingsGlobal && i < len(st.Exprs) {
					expr = st.Exprs[i]
				}
			}
		}
		if expr != nil {
			break
		}
	}
	if expr == nil {
		return nil, nil
	}
	v, err := constValue(expr, 0)
	if err != nil {
		return nil, fmt.Errorf("scripting: %s: %w", SettingsGlobal, err)
	}
	list, ok := v.([]any)
	if !ok {
		if m, isMap := v.(map[string]any); isMap && len(m) == 0 {
			return []Param{}, nil
		}
		return nil, fmt.Errorf("scripting: %s must be a list of settings", SettingsGlobal)
	}
	out := make([]Param, 0, len(list))
	for i, item := range list {
		p, err := settingParam(item)
		if err != nil {
			return nil, fmt.Errorf("scripting: %s entry %d: %w", SettingsGlobal, i+1, err)
		}
		out = append(out, p)
	}
	return out, nil
}

func settingParam(item any) (Param, error) {
	m, ok := item.(map[string]any)
	if !ok {
		return Param{}, fmt.Errorf("a setting is a table with a name and a type")
	}
	for k := range m {
		if !slices.Contains([]string{"name", "type", "label", "description", "default", "required", "options"}, k) {
			return Param{}, fmt.Errorf("unknown field %q", k)
		}
	}
	name, _ := m["name"].(string)
	typ, _ := m["type"].(string)
	if name == "" || typ == "" {
		return Param{}, fmt.Errorf("a setting needs a name and a type")
	}
	if !slices.Contains(settingTypes, ParamType(typ)) {
		return Param{}, fmt.Errorf("%q is not a type a setting can have", typ)
	}
	label, _ := m["label"].(string)
	desc, _ := m["description"].(string)
	req, _ := m["required"].(bool)
	p := Param{Name: name, Type: ParamType(typ), Label: label, Description: desc, Required: req, Default: m["default"]}
	if raw, ok := m["options"]; ok {
		opts, ok := raw.([]any)
		if !ok {
			return Param{}, fmt.Errorf("options must be a list")
		}
		for _, o := range opts {
			switch x := o.(type) {
			case string:
				p.Options = append(p.Options, Option{Value: x, Label: x})
			case map[string]any:
				value, _ := x["value"].(string)
				olabel, _ := x["label"].(string)
				if value == "" {
					return Param{}, fmt.Errorf("an option needs a value")
				}
				if olabel == "" {
					olabel = value
				}
				p.Options = append(p.Options, Option{Value: value, Label: olabel})
			default:
				return Param{}, fmt.Errorf("an option is text or a table with a value")
			}
		}
	}
	if !luaName.MatchString(p.Name) {
		return Param{}, fmt.Errorf("%q is not a valid setting name", p.Name)
	}
	if err := p.validate(); err != nil {
		return Param{}, err
	}
	return p, nil
}

// constValue turns a constant Lua expression into Go: text, numbers,
// booleans, nil and tables of those. Anything else, a call or a variable,
// is refused, so reading it can never run code.
func constValue(e ast.Expr, depth int) (any, error) {
	if depth > 8 {
		return nil, fmt.Errorf("nested too deep")
	}
	switch x := e.(type) {
	case *ast.StringExpr:
		return x.Value, nil
	case *ast.NumberExpr:
		f, err := strconv.ParseFloat(x.Value, 64)
		if err != nil {
			n, ierr := strconv.ParseInt(x.Value, 0, 64)
			if ierr != nil {
				return nil, fmt.Errorf("line %d: %q is not a number", x.Line(), x.Value)
			}
			f = float64(n)
		}
		return f, nil
	case *ast.UnaryMinusOpExpr:
		v, err := constValue(x.Expr, depth+1)
		if err != nil {
			return nil, err
		}
		f, ok := v.(float64)
		if !ok {
			return nil, fmt.Errorf("line %d: minus needs a number", x.Line())
		}
		return -f, nil
	case *ast.TrueExpr:
		return true, nil
	case *ast.FalseExpr:
		return false, nil
	case *ast.NilExpr:
		return nil, nil
	case *ast.TableExpr:
		var list []any
		m := map[string]any{}
		for _, f := range x.Fields {
			v, err := constValue(f.Value, depth+1)
			if err != nil {
				return nil, err
			}
			if f.Key == nil {
				list = append(list, v)
				continue
			}
			k, ok := f.Key.(*ast.StringExpr)
			if !ok {
				return nil, fmt.Errorf("line %d: only names can be keys", x.Line())
			}
			m[k.Value] = v
		}
		if len(list) > 0 && len(m) > 0 {
			return nil, fmt.Errorf("line %d: a table is either a list or has names", x.Line())
		}
		if len(list) > 0 {
			return list, nil
		}
		return m, nil
	}
	return nil, fmt.Errorf("line %d: only constants are allowed here", e.Line())
}

// CheckSettings checks setting values against a schema and returns them
// cleaned: a nil value is dropped, so the setting falls back to its default.
func CheckSettings(schema []Param, values map[string]any) (map[string]any, error) {
	out := map[string]any{}
	for name, v := range values {
		i := slices.IndexFunc(schema, func(p Param) bool { return p.Name == name })
		if i < 0 {
			return nil, fmt.Errorf("scripting: the script has no setting %q", name)
		}
		if v == nil {
			continue
		}
		if _, err := literalLua(schema[i], v); err != nil {
			return nil, fmt.Errorf("scripting: setting %q: %w", name, err)
		}
		out[name] = v
	}
	return out, nil
}
