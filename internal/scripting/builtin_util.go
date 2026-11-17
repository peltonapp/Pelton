package scripting

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Limits of the store module, per script.
const (
	maxKVKey   = 200
	maxKVValue = 64 << 10
	maxJSON    = 1 << 20
)

func doc(f Function, desc string) Function {
	f.DescKey = desc
	return f
}

func registerUtilModules(r *Registry) error {
	for _, m := range []Module{logModule(), notifyModule(), timeModule(), jsonModule(), textModule(), settingsModule(), storeModule()} {
		if err := r.RegisterModule(m); err != nil {
			return err
		}
	}
	return nil
}

func modDesc(module, name string) string {
	return "scripts.fn." + module + "." + name
}

func logModule() Module {
	level := func(name string) Function {
		return doc(fn(name, func(c *Call) ([]any, error) {
			parts := make([]string, 0, len(c.Args))
			for _, a := range c.Args {
				parts = append(parts, display(a))
			}
			c.Log(name, strings.Join(parts, " "))
			return nil, nil
		}, param("text", TypeText)), modDesc("log", name))
	}
	return Module{
		Meta:      meta("module", "log", "utility"),
		Functions: []Function{level("debug"), level("info"), level("warn"), level("error")},
	}
}

// display renders a value for a log line the way Lua's tostring would for
// plain values, and as JSON for tables.
func display(v any) string {
	switch x := v.(type) {
	case nil:
		return "nil"
	case string:
		return x
	case float64:
		return fmt.Sprint(x)
	case bool:
		return fmt.Sprint(x)
	}
	b, err := json.Marshal(plainValue(v))
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

func notifyModule() Module {
	toast := fn("toast", func(c *Call) ([]any, error) {
		text, err := c.String(0)
		if err != nil {
			return nil, err
		}
		level, err := c.OptString(1, "info")
		if err != nil {
			return nil, err
		}
		if c.Hosts == nil || c.Hosts.Notify == nil {
			return nil, ErrUnavailable
		}
		c.Hosts.Notify.Toast(c.Ctx, c.ScriptName(), clip(text, 500), level)
		return nil, nil
	}, param("text", TypeText), param("level", TypeString))
	toast.Effect = true
	system := fn("system", func(c *Call) ([]any, error) {
		title, err := c.String(0)
		if err != nil {
			return nil, err
		}
		body, err := c.OptString(1, "")
		if err != nil {
			return nil, err
		}
		if c.Hosts == nil || c.Hosts.Notify == nil {
			return nil, ErrUnavailable
		}
		return nil, c.Hosts.Notify.System(c.Ctx, clip(title, 200), clip(body, 1000))
	}, param("title", TypeString), param("body", TypeText))
	system.Effect = true
	system.Permission = PermNotify
	return Module{
		Meta:      meta("module", "notify", "utility"),
		Functions: []Function{doc(toast, modDesc("notify", "toast")), doc(system, modDesc("notify", "system"))},
	}
}

// timeLayouts are the named layouts time.format and time.parse take besides
// a Go layout.
var timeLayouts = map[string]string{
	"date":     "2006-01-02",
	"time":     "15:04",
	"datetime": "2006-01-02 15:04",
	"iso":      time.RFC3339,
}

func layoutArg(c *Call, i int) (string, error) {
	l, err := c.OptString(i, "datetime")
	if err != nil {
		return "", err
	}
	if named, ok := timeLayouts[l]; ok {
		return named, nil
	}
	return l, nil
}

func unixArg(c *Call, i int) (time.Time, error) {
	if c.Arg(i) == nil {
		return c.Now(), nil
	}
	f, err := c.Number(i)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(int64(f), 0).In(c.Now().Location()), nil
}

func timeModule() Module {
	fns := []Function{
		fn("now", func(c *Call) ([]any, error) {
			return []any{c.Now().Unix()}, nil
		}),
		fn("format", func(c *Call) ([]any, error) {
			t, err := unixArg(c, 0)
			if err != nil {
				return nil, err
			}
			layout, err := layoutArg(c, 1)
			if err != nil {
				return nil, err
			}
			return []any{t.Format(layout)}, nil
		}, param("time", TypeNumber), param("layout", TypeString)),
		fn("parse", func(c *Call) ([]any, error) {
			s, err := c.String(0)
			if err != nil {
				return nil, err
			}
			layout, err := layoutArg(c, 1)
			if err != nil {
				return nil, err
			}
			t, err := time.ParseInLocation(layout, s, c.Now().Location())
			if err != nil {
				return []any{nil, err.Error()}, nil
			}
			return []any{t.Unix()}, nil
		}, param("text", TypeString), param("layout", TypeString)),
		fn("duration", func(c *Call) ([]any, error) {
			s, err := c.String(0)
			if err != nil {
				return nil, err
			}
			secs, err := ParseDuration(s)
			if err != nil {
				return nil, fmt.Errorf("time.duration: %w", err)
			}
			return []any{secs}, nil
		}, param("text", TypeDuration)),
		fn("date", func(c *Call) ([]any, error) {
			t, err := unixArg(c, 0)
			if err != nil {
				return nil, err
			}
			return []any{map[string]any{
				"year": t.Year(), "month": int(t.Month()), "day": t.Day(),
				"hour": t.Hour(), "minute": t.Minute(), "second": t.Second(),
				"weekday": weekdayNames[t.Weekday()],
			}}, nil
		}, param("time", TypeNumber)),
	}
	for i := range fns {
		fns[i].DescKey = modDesc("time", fns[i].Name)
	}
	return Module{Meta: meta("module", "time", "utility"), Functions: fns}
}

func jsonModule() Module {
	encode := fn("encode", func(c *Call) ([]any, error) {
		b, err := json.Marshal(plainValue(c.Arg(0)))
		if err != nil {
			return nil, fmt.Errorf("json.encode: %w", err)
		}
		if len(b) > maxJSON {
			return nil, fmt.Errorf("json.encode: more than %d bytes", maxJSON)
		}
		return []any{string(b)}, nil
	}, param("value", TypeRef))
	decode := fn("decode", func(c *Call) ([]any, error) {
		s, err := c.String(0)
		if err != nil {
			return nil, err
		}
		if len(s) > maxJSON {
			return nil, fmt.Errorf("json.decode: more than %d bytes", maxJSON)
		}
		var v any
		if err := json.Unmarshal([]byte(s), &v); err != nil {
			return []any{nil, err.Error()}, nil
		}
		return []any{v}, nil
	}, param("text", TypeText))
	return Module{
		Meta:      meta("module", "json", "utility"),
		Functions: []Function{doc(encode, modDesc("json", "encode")), doc(decode, modDesc("json", "decode"))},
	}
}

func settingsModule() Module {
	get := fn("get", func(c *Call) ([]any, error) {
		name, err := c.String(0)
		if err != nil {
			return nil, err
		}
		v, _ := c.Setting(name)
		return []any{v}, nil
	}, param("name", TypeString))
	all := fn("all", func(c *Call) ([]any, error) {
		return []any{c.Settings()}, nil
	})
	return Module{
		Meta:      meta("module", "settings", "utility"),
		Functions: []Function{doc(get, modDesc("settings", "get")), doc(all, modDesc("settings", "all"))},
	}
}

func kvKey(c *Call) (string, KVStore, error) {
	key, err := c.String(0)
	if err != nil {
		return "", nil, err
	}
	if key == "" || len(key) > maxKVKey {
		return "", nil, fmt.Errorf("%s: a key is 1 to %d bytes", c.Function, maxKVKey)
	}
	kv := c.KV()
	if kv == nil || c.ScriptID() == 0 {
		return "", nil, fmt.Errorf("%s: only a saved script has storage", c.Function)
	}
	return key, kv, nil
}

func storeModule() Module {
	get := fn("get", func(c *Call) ([]any, error) {
		key, kv, err := kvKey(c)
		if err != nil {
			if c.ScriptID() == 0 {
				return []any{nil}, nil
			}
			return nil, err
		}
		raw, ok, err := kv.KVGet(c.Ctx, c.ScriptID(), key)
		if err != nil || !ok {
			return []any{nil}, err
		}
		var v any
		if err := json.Unmarshal([]byte(raw), &v); err != nil {
			return []any{nil}, nil
		}
		return []any{v}, nil
	}, param("key", TypeString))
	set := fn("set", func(c *Call) ([]any, error) {
		key, kv, err := kvKey(c)
		if err != nil {
			return nil, err
		}
		if c.Arg(1) == nil {
			return nil, kv.KVDelete(c.Ctx, c.ScriptID(), key)
		}
		b, err := json.Marshal(plainValue(c.Arg(1)))
		if err != nil {
			return nil, fmt.Errorf("store.set: %w", err)
		}
		if len(b) > maxKVValue {
			return nil, fmt.Errorf("store.set: a value is at most %d bytes", maxKVValue)
		}
		return nil, kv.KVSet(c.Ctx, c.ScriptID(), key, string(b))
	}, param("key", TypeString), param("value", TypeRef))
	set.Effect = true
	del := fn("delete", func(c *Call) ([]any, error) {
		key, kv, err := kvKey(c)
		if err != nil {
			return nil, err
		}
		return nil, kv.KVDelete(c.Ctx, c.ScriptID(), key)
	}, param("key", TypeString))
	del.Effect = true
	keys := fn("keys", func(c *Call) ([]any, error) {
		kv := c.KV()
		if kv == nil || c.ScriptID() == 0 {
			return []any{[]any{}}, nil
		}
		list, err := kv.KVKeys(c.Ctx, c.ScriptID())
		if err != nil {
			return nil, err
		}
		out := make([]any, len(list))
		for i, k := range list {
			out[i] = k
		}
		return []any{out}, nil
	})
	return Module{
		Meta: meta("module", "store", "utility"),
		Functions: []Function{
			doc(get, modDesc("store", "get")), doc(set, modDesc("store", "set")),
			doc(del, modDesc("store", "delete")), doc(keys, modDesc("store", "keys")),
		},
	}
}
