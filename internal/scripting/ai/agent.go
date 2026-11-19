package ai

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/scripting"
)

// The caps of a tool use step: their defaults and the most a step may set.
const (
	DefaultAgentSteps   = 8
	MaxAgentSteps       = 32
	DefaultAgentTokens  = 30000
	MaxAgentTokens      = 500000
	DefaultAgentSeconds = 120
	// maxCallsPerTurn is how many tool calls of one answer are made; the
	// rest are answered with an error.
	maxCallsPerTurn = 16
	agentTokens     = 2048
)

// agentRules is the system text of a tool use step.
const agentRules = `You act inside Pelton, a mail app, through the tools you are given. Each tool call is checked against what the user allowed, and some ask the user first. Use the fewest calls that do the task, and finish with a short plain answer saying what you did.`

// tool is one function the model may call.
type tool struct {
	full string
	fn   scripting.Function
}

// tools builds the tool list from the registry: the module functions the
// script holds the permission for, without the ai module itself. With want
// set, only those functions, or every function of a module named alone.
// Without it, only functions that need a permission, since the rest are
// helpers a script uses on values it already has.
func (m *module) tools(c *scripting.Call, want []string) (map[string]tool, []Tool, error) {
	byName := map[string]tool{}
	var list []Tool
	matched := map[string]bool{}
	for _, mod := range m.reg.Modules() {
		if mod.ID == "ai" {
			continue
		}
		for _, f := range mod.Functions {
			full := mod.ID + "." + f.Name
			if want != nil {
				hit := ""
				for _, w := range want {
					if w == full || w == mod.ID {
						hit = w
					}
				}
				if hit == "" {
					continue
				}
				matched[hit] = true
			} else if f.Permission == "" {
				continue
			}
			if !c.Granted(f.Permission) {
				continue
			}
			name := strings.ReplaceAll(full, ".", "_")
			if _, dup := byName[name]; dup {
				continue
			}
			byName[name] = tool{full: full, fn: f}
			list = append(list, Tool{Name: name, Description: toolDescription(full, f), Parameters: paramsSchema(f.Params)})
		}
	}
	for _, w := range want {
		if !matched[w] {
			return nil, nil, fmt.Errorf("%s: there is no function or module %q to offer as a tool", c.Function, w)
		}
	}
	return byName, list, nil
}

func toolDescription(full string, f scripting.Function) string {
	d := f.Description
	if d == "" {
		names := make([]string, 0, len(f.Params))
		for _, p := range f.Params {
			names = append(names, p.Name)
		}
		d = fmt.Sprintf("The Pelton function %s(%s).", full, strings.Join(names, ", "))
	}
	if f.Effect {
		d += " It changes something."
	}
	if f.Risky {
		d += " The user is asked first."
	}
	return d
}

func paramsSchema(params []scripting.Param) map[string]any {
	props := map[string]any{}
	var required []any
	for _, p := range params {
		s := paramSchema(p.Type, p)
		if p.Label != "" {
			s["description"] = p.Label
		}
		props[p.Name] = s
		if p.Required {
			required = append(required, p.Name)
		}
	}
	out := map[string]any{"type": "object", "properties": props}
	if required != nil {
		out["required"] = required
	}
	return out
}

func paramSchema(t scripting.ParamType, p scripting.Param) map[string]any {
	switch t {
	case scripting.TypeNumber, scripting.TypeAccount:
		return map[string]any{"type": "number"}
	case scripting.TypeColor:
		return map[string]any{"type": "integer", "minimum": 0, "maximum": 8}
	case scripting.TypeBool:
		return map[string]any{"type": "boolean"}
	case scripting.TypeEnum:
		values := make([]any, len(p.Options))
		for i, o := range p.Options {
			values[i] = o.Value
		}
		return map[string]any{"type": "string", "enum": values}
	case scripting.TypeList:
		return map[string]any{"type": "array", "items": paramSchema(p.Of, p)}
	case scripting.TypeDuration:
		return map[string]any{"type": "string", "description": "a span such as 15m, 2h or 3d"}
	case scripting.TypeTime:
		return map[string]any{"type": "string", "description": "a time of day as HH:MM"}
	case scripting.TypeFolder:
		return map[string]any{"type": "string", "description": "a folder path, or a role: inbox, archive, trash, junk, sent, drafts"}
	case scripting.TypeRef:
		if p.ValueType == "message" {
			return map[string]any{"type": "number", "description": "the id of a message"}
		}
		return map[string]any{"type": "object", "description": "a table of values"}
	}
	return map[string]any{"type": "string"}
}

// callTool makes one tool call through the engine, as a script's own call
// would be made. A failure the model can work with comes back as an error
// result; one that ends the run is passed on.
func (m *module) callTool(c *scripting.Call, t tool, args map[string]any) (string, bool, error) {
	pos := make([]any, len(t.fn.Params))
	for i, p := range t.fn.Params {
		pos[i] = args[p.Name]
	}
	res, err := c.Invoke(t.full, pos)
	if err != nil {
		if c.Ending() {
			return "", true, err
		}
		return fence("tool error", err.Error()), true, nil
	}
	var v any = res
	if len(res) == 1 {
		v = res[0]
	}
	b, jerr := json.Marshal(plain(v))
	if jerr != nil {
		return fence("tool error", "the result cannot be shown"), true, nil
	}
	return fence("tool result", string(b)), false, nil
}

func (m *module) agent(c *scripting.Call) ([]any, error) {
	prompt, err := c.String(0)
	if err != nil {
		return nil, err
	}
	o, err := readOpts(c, c.Table(1))
	if err != nil {
		return nil, err
	}
	maxSteps, err := optNumber(c, o.raw, "max_steps", DefaultAgentSteps, 1, MaxAgentSteps)
	if err != nil {
		return nil, err
	}
	maxTotal, err := optNumber(c, o.raw, "max_total_tokens", DefaultAgentTokens, 100, MaxAgentTokens)
	if err != nil {
		return nil, err
	}
	wall := o.timeout
	if wall <= 0 {
		wall = DefaultAgentSeconds * time.Second
	}
	var want []string
	if v := o.raw["tools"]; v != nil {
		want, err = toolList(c, v)
		if err != nil {
			return nil, err
		}
	}
	data, label, err := input(c, o.raw["input"])
	if err != nil {
		return nil, err
	}
	byName, tools, err := m.tools(c, want)
	if err != nil {
		return nil, err
	}
	s, err := m.open(c, o)
	if err != nil {
		return nil, err
	}
	toolCalls := 0
	finish := func(text string, err error) []any {
		r := s.result(text, err)
		r["model_calls"] = s.calls
		r["tool_calls"] = toolCalls
		return []any{r}
	}
	if s.placeholder {
		return finish(dryText, nil), nil
	}
	text := prompt
	if data != "" {
		text += "\n\n" + fence(label, data)
	}
	sys := agentRules
	if o.system != "" {
		sys = o.system + "\n\n" + agentRules
	}
	req := Request{System: system(sys), Messages: []Message{{Role: "user", Text: text}}, Tools: tools, MaxTokens: tokens(o, agentTokens)}
	deadline := time.Now().Add(wall)
	for {
		if s.calls >= int(maxSteps) {
			return finish("", fmt.Errorf("stopped after %d model calls", int(maxSteps))), nil
		}
		if s.inTokens+s.outTokens >= int(maxTotal) {
			return finish("", fmt.Errorf("stopped after %d tokens", s.inTokens+s.outTokens)), nil
		}
		left := time.Until(deadline)
		if left <= 0 {
			return finish("", fmt.Errorf("stopped after %s", wall)), nil
		}
		resp, err := s.chat(req, min(left, RequestTimeout))
		if err != nil {
			return finish("", err), nil
		}
		if len(resp.ToolCalls) == 0 {
			return finish(resp.Text, nil), nil
		}
		req.Messages = append(req.Messages, Message{Role: "assistant", Text: resp.Text, ToolCalls: resp.ToolCalls})
		for i, tc := range resp.ToolCalls {
			answer := Message{Role: "tool", ToolCallID: tc.ID, ToolName: tc.Name}
			t, known := byName[tc.Name]
			switch {
			case i >= maxCallsPerTurn:
				answer.Text, answer.ToolError = fmt.Sprintf("Not called: at most %d calls at once.", maxCallsPerTurn), true
			case !known:
				answer.Text, answer.ToolError = "There is no tool "+tc.Name+".", true
			default:
				toolCalls++
				out, failed, err := m.callTool(c, t, tc.Args)
				if err != nil {
					return nil, err
				}
				answer.Text, answer.ToolError = out, failed
			}
			req.Messages = append(req.Messages, answer)
		}
	}
}

// toolList reads the tools option: a list of function or module names, or
// one name.
func toolList(c *scripting.Call, v any) ([]string, error) {
	var names []string
	switch x := v.(type) {
	case string:
		names = []string{x}
	case []any:
		for _, item := range x {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("%s: the tools option is a list of function names", c.Function)
			}
			names = append(names, s)
		}
	case map[string]any:
		if len(x) != 0 {
			return nil, fmt.Errorf("%s: the tools option is a list of function names", c.Function)
		}
	default:
		return nil, fmt.Errorf("%s: the tools option is a list of function names", c.Function)
	}
	out := []string{}
	for _, n := range names {
		if n = strings.TrimSpace(n); n != "" {
			out = append(out, n)
		}
	}
	return out, nil
}
