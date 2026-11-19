package scripting

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"

	lua "github.com/yuin/gopher-lua"
)

// maxValueDepth bounds how deep a table is converted between Lua and Go, and
// maxValueItems how many entries, so a self-referencing or huge table cannot
// hang a host call.
const (
	maxValueDepth = 32
	maxValueItems = 100000
)

// fromLua converts a Lua value to Go: nil, bool, float64, string, []any for a
// sequence and map[string]any for any other table. Functions and other
// values become nil.
func fromLua(v lua.LValue) any {
	items := 0
	return fromLuaDepth(v, 0, &items)
}

func fromLuaDepth(v lua.LValue, depth int, items *int) any {
	switch x := v.(type) {
	case lua.LBool:
		return bool(x)
	case lua.LNumber:
		return float64(x)
	case lua.LString:
		return string(x)
	case *lua.LTable:
		if depth >= maxValueDepth {
			return nil
		}
		return tableFromLua(x, depth, items)
	}
	return nil
}

func tableFromLua(t *lua.LTable, depth int, items *int) any {
	n := t.Len()
	isList := n > 0
	if isList {
		count := 0
		t.ForEach(func(k, _ lua.LValue) {
			count++
		})
		isList = count == n
	}
	if isList {
		out := make([]any, 0, n)
		for i := 1; i <= n && *items < maxValueItems; i++ {
			*items++
			out = append(out, fromLuaDepth(t.RawGetInt(i), depth+1, items))
		}
		return out
	}
	out := map[string]any{}
	t.ForEach(func(k, val lua.LValue) {
		if *items >= maxValueItems {
			return
		}
		*items++
		var key string
		switch kk := k.(type) {
		case lua.LString:
			key = string(kk)
		case lua.LNumber:
			key = strconv.FormatFloat(float64(kk), 'g', -1, 64)
		default:
			return
		}
		out[key] = fromLuaDepth(val, depth+1, items)
	})
	return out
}

// toLua converts a Go value to Lua. Objects get their value type's metatable
// through meta, which may be nil.
func toLua(L *lua.LState, v any, meta func(typ string) *lua.LTable) lua.LValue {
	return toLuaDepth(L, v, meta, 0)
}

func toLuaDepth(L *lua.LState, v any, meta func(typ string) *lua.LTable, depth int) lua.LValue {
	if depth >= maxValueDepth {
		return lua.LNil
	}
	switch x := v.(type) {
	case nil:
		return lua.LNil
	case lua.LValue:
		return x
	case bool:
		return lua.LBool(x)
	case string:
		return lua.LString(x)
	case int:
		return lua.LNumber(x)
	case int32:
		return lua.LNumber(x)
	case int64:
		return lua.LNumber(x)
	case uint32:
		return lua.LNumber(x)
	case float32:
		return lua.LNumber(x)
	case float64:
		return lua.LNumber(x)
	case time.Time:
		return lua.LNumber(x.Unix())
	case []string:
		t := L.CreateTable(len(x), 0)
		for _, s := range x {
			t.Append(lua.LString(s))
		}
		return t
	case []any:
		t := L.CreateTable(len(x), 0)
		for _, item := range x {
			t.Append(toLuaDepth(L, item, meta, depth+1))
		}
		return t
	case map[string]any:
		t := L.CreateTable(0, len(x))
		for _, k := range sortedKeys(x) {
			t.RawSetString(k, toLuaDepth(L, x[k], meta, depth+1))
		}
		return t
	case Object:
		t := toLuaDepth(L, x.Fields, meta, depth).(*lua.LTable)
		if meta != nil {
			if mt := meta(x.Type); mt != nil {
				L.SetMetatable(t, mt)
			}
		}
		return t
	case []Object:
		t := L.CreateTable(len(x), 0)
		for _, item := range x {
			t.Append(toLuaDepth(L, item, meta, depth+1))
		}
		return t
	}
	return lua.LString(fmt.Sprint(v))
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// plainValue turns Objects into their fields, so a value can be encoded as
// JSON or compared in a test.
func plainValue(v any) any {
	switch x := v.(type) {
	case Object:
		return plainValue(x.Fields)
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[k] = plainValue(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = plainValue(val)
		}
		return out
	case []Object:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = plainValue(val)
		}
		return out
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return nil
		}
	}
	return v
}
