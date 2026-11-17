package scripting

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"time"
)

// Call is one host function call as its handler sees it.
type Call struct {
	Ctx context.Context
	// Args are the Lua arguments converted to Go: nil, bool, float64,
	// string, []any or map[string]any.
	Args []any
	// Dry is set in a dry run. Handlers of calls with an Effect never run in
	// one; a read may use it to answer for the sample message.
	Dry   bool
	Hosts *Hosts
	// Function is the full Lua name being called.
	Function string
	st       *runState
}

// ScriptID is the id of the running script, 0 for an unsaved one.
func (c *Call) ScriptID() int64 {
	return c.st.req.Script.ID
}

// ScriptName is the name of the running script.
func (c *Call) ScriptName() string {
	return c.st.req.Script.Name
}

// Arg returns argument i (from 0), nil when it was not given.
func (c *Call) Arg(i int) any {
	if i < 0 || i >= len(c.Args) {
		return nil
	}
	return c.Args[i]
}

func (c *Call) argError(i int, want string) error {
	return fmt.Errorf("%s: argument %d must be %s", c.Function, i+1, want)
}

// String returns argument i as text; numbers are formatted.
func (c *Call) String(i int) (string, error) {
	switch v := c.Arg(i).(type) {
	case string:
		return v, nil
	case float64:
		return fmt.Sprint(v), nil
	}
	return "", c.argError(i, "text")
}

// OptString returns argument i as text, or def when it is nil.
func (c *Call) OptString(i int, def string) (string, error) {
	if c.Arg(i) == nil {
		return def, nil
	}
	return c.String(i)
}

// Number returns argument i as a number.
func (c *Call) Number(i int) (float64, error) {
	if v, ok := c.Arg(i).(float64); ok {
		return v, nil
	}
	return 0, c.argError(i, "a number")
}

// Int returns argument i as a whole number.
func (c *Call) Int(i int) (int64, error) {
	f, err := c.Number(i)
	if err != nil || f != math.Trunc(f) {
		return 0, c.argError(i, "a whole number")
	}
	return int64(f), nil
}

// Bool returns argument i by Lua truth, with def when it is nil.
func (c *Call) Bool(i int, def bool) bool {
	switch v := c.Arg(i).(type) {
	case nil:
		return def
	case bool:
		return v
	}
	return true
}

// Table returns argument i as a table with string keys, or nil.
func (c *Call) Table(i int) map[string]any {
	m, _ := c.Arg(i).(map[string]any)
	return m
}

// List returns argument i as a list of text; a single string is a list of
// one.
func (c *Call) List(i int) ([]string, error) {
	switch v := c.Arg(i).(type) {
	case nil:
		return nil, nil
	case string:
		return []string{v}, nil
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, c.argError(i, "a list of text")
			}
			out = append(out, s)
		}
		return out, nil
	case map[string]any:
		if len(v) == 0 {
			return nil, nil
		}
	}
	return nil, c.argError(i, "a list of text")
}

// MessageID returns argument i as a message id: a message table or a number.
func (c *Call) MessageID(i int) (int64, error) {
	switch v := c.Arg(i).(type) {
	case float64:
		return int64(v), nil
	case map[string]any:
		if id, ok := v["id"].(float64); ok {
			return int64(id), nil
		}
	}
	return 0, c.argError(i, "a message")
}

// Setting returns one of the script's settings, or its declared default.
func (c *Call) Setting(name string) (any, bool) {
	return c.st.req.Script.setting(name)
}

// Settings returns every setting with its value or default.
func (c *Call) Settings() map[string]any {
	out := map[string]any{}
	for _, p := range c.st.req.Script.SettingsSchema {
		if p.Default != nil {
			out[p.Name] = p.Default
		}
	}
	for k, v := range c.st.req.Script.Settings {
		out[k] = v
	}
	return out
}

// KV returns the script's own key value storage, nil when there is none.
func (c *Call) KV() KVStore {
	return c.st.e.KV
}

// Log adds a line to the run log.
func (c *Call) Log(level, text string) {
	c.st.log(level, text)
}

// Cancel stops what started the run once the run ends, e.g. a send.
func (c *Call) Cancel(reason string) {
	c.st.mu.Lock()
	defer c.st.mu.Unlock()
	c.st.run.Cancelled = true
	c.st.run.CancelReason = reason
}

// Now is the current time by the hosts' clock.
func (c *Call) Now() time.Time {
	return c.Hosts.now()
}

// maxPattern caps a regular expression's length; RE2 runs in linear time, so
// only the compile cost needs a bound.
const maxPattern = 2000

// maxRegexps caps how many distinct expressions one run compiles.
const maxRegexps = 200

type regexpEntry struct {
	re *regexp.Regexp
}

// Regexp compiles a regular expression once per run.
func (c *Call) Regexp(pattern string) (*regexp.Regexp, error) {
	if len(pattern) > maxPattern {
		return nil, fmt.Errorf("%s: the regular expression is longer than %d characters", c.Function, maxPattern)
	}
	c.st.mu.Lock()
	defer c.st.mu.Unlock()
	if e, ok := c.st.regexps[pattern]; ok {
		return e.re, nil
	}
	if len(c.st.regexps) >= maxRegexps {
		return nil, fmt.Errorf("%s: too many different regular expressions in one run", c.Function)
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("%s: invalid regular expression: %w", c.Function, err)
	}
	c.st.regexps[pattern] = &regexpEntry{re: re}
	return re, nil
}
