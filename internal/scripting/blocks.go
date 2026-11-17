package scripting

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// TreeVersion is the version of the block tree format this package writes.
const TreeVersion = 1

// Tree is a block script: a list of blocks run in order.
type Tree struct {
	Version int     `json:"version"`
	Blocks  []Block `json:"blocks"`
}

// The block kinds.
const (
	// BlockAction runs the registered action Type with Args.
	BlockAction = "action"
	// BlockIf runs the body of the first branch whose condition holds, or
	// Else. The first branch is the "if", the rest are "else if".
	BlockIf = "if"
	// BlockForEach runs Body once per item of List, with the item in the
	// variable As.
	BlockForEach = "foreach"
	// BlockSet stores Value in the variable As.
	BlockSet = "set"
	// BlockStop ends the run.
	BlockStop = "stop"
	// BlockLua runs Code as Lua.
	BlockLua = "lua"
)

// Block is one step of a block script. Which fields apply depends on Kind.
type Block struct {
	// ID is unique within the tree; later blocks refer to an action's
	// outputs as step:<id>.<field>.
	ID       string         `json:"id"`
	Kind     string         `json:"kind"`
	Type     string         `json:"type,omitempty"`
	Args     map[string]Arg `json:"args,omitempty"`
	Branches []Branch       `json:"branches,omitempty"`
	Else     []Block        `json:"else,omitempty"`
	List     *Arg           `json:"list,omitempty"`
	As       string         `json:"as,omitempty"`
	Value    *Arg           `json:"value,omitempty"`
	Body     []Block        `json:"body,omitempty"`
	Code     string         `json:"code,omitempty"`
}

// Branch is one condition and the blocks it guards.
type Branch struct {
	When Cond    `json:"when"`
	Body []Block `json:"body"`
}

// The condition kinds.
const (
	CondAll       = "all"
	CondAny       = "any"
	CondNot       = "not"
	CondCondition = "condition"
)

// Cond is a condition: a registered condition Type with Args, or a group of
// conditions that must all, any or not hold. A "not" has one item.
type Cond struct {
	Kind  string         `json:"kind"`
	Type  string         `json:"type,omitempty"`
	Args  map[string]Arg `json:"args,omitempty"`
	Items []Cond         `json:"items,omitempty"`
}

// Arg is a parameter value: a literal Value, or a Ref to another value
// ("event.message", "var:item", "step:b3.id" or "setting:folder").
type Arg struct {
	Value any    `json:"value,omitempty"`
	Ref   string `json:"ref,omitempty"`
}

var blockIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// ParseTree reads a block tree from JSON.
func ParseTree(data []byte) (Tree, error) {
	var t Tree
	if err := json.Unmarshal(data, &t); err != nil {
		return Tree{}, fmt.Errorf("scripting: read blocks: %w", err)
	}
	if t.Version == 0 {
		t.Version = TreeVersion
	}
	if t.Version > TreeVersion {
		return Tree{}, fmt.Errorf("scripting: blocks are format %d, newer than this app reads", t.Version)
	}
	return t, nil
}

// CompileError is a problem with one block.
type CompileError struct {
	Block   string `json:"block"`
	Message string `json:"message"`
}

func (e CompileError) Error() string {
	if e.Block == "" {
		return "scripting: " + e.Message
	}
	return fmt.Sprintf("scripting: block %s: %s", e.Block, e.Message)
}

// CompileResult is the Lua compiled from a tree, and every problem found.
// Lua is empty when there are errors.
type CompileResult struct {
	Lua    string         `json:"lua"`
	Errors []CompileError `json:"errors"`
	// Permissions are those the blocks need, so the editor can declare them.
	Permissions []string `json:"permissions"`
}

type compiler struct {
	r       *Registry
	trigger string
	trace   bool
	b       strings.Builder
	errs    []CompileError
	ids     map[string]bool
	outputs map[string][]Field
	perms   []string
}

// generatedHeader starts every compiled script.
const generatedHeader = "-- Generated from blocks. Edit the blocks, or convert the script to Lua.\n"

// Compile turns a block tree into Lua. Every action and condition compiles
// through its registry entry's template, so the compiler knows nothing about
// any of them. trigger is the script's trigger, which some actions require.
//
// With trace set, the Lua also reports which block runs and how each
// condition came out, for the run log; that is the form a run executes.
// Without it the Lua is what the editor shows and "Convert to Lua" keeps.
func Compile(r *Registry, t Tree, trigger string, trace bool) CompileResult {
	c := &compiler{r: r, trigger: trigger, trace: trace, ids: map[string]bool{}, outputs: map[string][]Field{}}
	c.collect(t.Blocks)
	if trace {
		c.b.WriteString(generatedHeader)
	}
	c.b.WriteString("local vars, steps = {}, {}\n")
	c.blocks(t.Blocks, 0)
	res := CompileResult{Errors: c.errs, Permissions: c.perms}
	if res.Errors == nil {
		res.Errors = []CompileError{}
		res.Lua = c.b.String()
	}
	if res.Permissions == nil {
		res.Permissions = []string{}
	}
	return res
}

func (c *compiler) fail(block, format string, args ...any) {
	c.errs = append(c.errs, CompileError{Block: block, Message: fmt.Sprintf(format, args...)})
}

// collect checks the ids and records which blocks have outputs, so a
// reference to a step can be checked wherever it appears.
func (c *compiler) collect(blocks []Block) {
	for _, b := range blocks {
		if !blockIDPattern.MatchString(b.ID) {
			c.fail(b.ID, "invalid block id %q", b.ID)
		} else if c.ids[b.ID] {
			c.fail(b.ID, "the block id is used twice")
		}
		c.ids[b.ID] = true
		if b.Kind == BlockAction {
			if a, ok := c.r.Action(b.Type); ok && len(a.Outputs) > 0 {
				c.outputs[b.ID] = a.Outputs
			}
		}
		for _, br := range b.Branches {
			c.collect(br.Body)
		}
		c.collect(b.Else)
		c.collect(b.Body)
	}
}

func (c *compiler) line(depth int, s string) {
	c.b.WriteString(strings.Repeat("  ", depth))
	c.b.WriteString(s)
	c.b.WriteByte('\n')
}

// mark reports the block about to run, in traced code.
func (c *compiler) mark(depth int, id string) {
	if c.trace {
		c.line(depth, "__block("+LuaString(id)+")")
	}
}

func (c *compiler) need(perm string) {
	if perm != "" && !slices.Contains(c.perms, perm) {
		c.perms = append(c.perms, perm)
	}
}

func (c *compiler) blocks(blocks []Block, depth int) {
	for _, b := range blocks {
		c.block(b, depth)
	}
}

func (c *compiler) block(b Block, depth int) {
	switch b.Kind {
	case BlockAction:
		a, ok := c.r.Action(b.Type)
		if !ok {
			c.fail(b.ID, "unknown action %q", b.Type)
			return
		}
		if a.Missing != "" {
			c.fail(b.ID, "%s is not available: %s", b.Type, a.Missing)
			return
		}
		if len(a.Triggers) > 0 && !slices.Contains(a.Triggers, c.trigger) {
			c.fail(b.ID, "%s only works with the trigger %s", b.Type, strings.Join(a.Triggers, ", "))
			return
		}
		c.need(a.Permission)
		expr, ok := c.fill(b.ID, a.Lua, a.Params, b.Args)
		if !ok {
			return
		}
		c.mark(depth, b.ID)
		if len(a.Outputs) > 0 {
			c.line(depth, "steps["+LuaString(b.ID)+"] = "+expr)
		} else {
			c.line(depth, expr)
		}
	case BlockIf:
		if len(b.Branches) == 0 {
			c.fail(b.ID, "an if block needs a condition")
			return
		}
		for i, br := range b.Branches {
			check := c.cond(b.ID, br.When)
			if c.trace {
				check = "__check(" + LuaString(b.ID) + ", " + check + ")"
			}
			if i == 0 {
				c.line(depth, "if "+check+" then")
			} else {
				c.line(depth, "elseif "+check+" then")
			}
			c.blocks(br.Body, depth+1)
		}
		if len(b.Else) > 0 {
			c.line(depth, "else")
			c.blocks(b.Else, depth+1)
		}
		c.line(depth, "end")
	case BlockForEach:
		if !refSegment.MatchString(b.As) {
			c.fail(b.ID, "a for each block needs a variable name")
			return
		}
		if b.List == nil {
			c.fail(b.ID, "a for each block needs a list")
			return
		}
		list, ok := c.arg(b.ID, Param{Name: "list", Type: TypeList, Of: TypeString}, *b.List)
		if !ok {
			return
		}
		c.line(depth, "for _, item in ipairs("+list+" or {}) do")
		c.line(depth+1, "vars["+LuaString(b.As)+"] = item")
		c.blocks(b.Body, depth+1)
		c.line(depth, "end")
	case BlockSet:
		if !refSegment.MatchString(b.As) {
			c.fail(b.ID, "a set block needs a variable name")
			return
		}
		value := "nil"
		if b.Value != nil {
			v, ok := c.anyArg(b.ID, *b.Value)
			if !ok {
				return
			}
			value = v
		}
		c.line(depth, "vars["+LuaString(b.As)+"] = "+value)
	case BlockStop:
		c.line(depth, "stop()")
	case BlockLua:
		c.mark(depth, b.ID)
		c.line(depth, "do")
		for _, l := range strings.Split(strings.TrimRight(b.Code, "\n"), "\n") {
			c.line(depth+1, l)
		}
		c.line(depth, "end")
	default:
		c.fail(b.ID, "unknown block kind %q", b.Kind)
	}
}

func (c *compiler) cond(block string, k Cond) string {
	switch k.Kind {
	case CondAll, CondAny:
		if len(k.Items) == 0 {
			if k.Kind == CondAll {
				return "true"
			}
			return "false"
		}
		op := " and "
		if k.Kind == CondAny {
			op = " or "
		}
		parts := make([]string, 0, len(k.Items))
		for _, item := range k.Items {
			parts = append(parts, c.cond(block, item))
		}
		return "(" + strings.Join(parts, op) + ")"
	case CondNot:
		if len(k.Items) != 1 {
			c.fail(block, "a not group takes exactly one condition")
			return "false"
		}
		return "(not " + c.cond(block, k.Items[0]) + ")"
	case CondCondition:
		def, ok := c.r.Condition(k.Type)
		if !ok {
			c.fail(block, "unknown condition %q", k.Type)
			return "false"
		}
		c.need(def.Permission)
		expr, ok := c.fill(block, def.Lua, def.Params, k.Args)
		if !ok {
			return "false"
		}
		return "(" + expr + ")"
	}
	c.fail(block, "unknown condition kind %q", k.Kind)
	return "false"
}

// fill puts the compiled arguments into a template.
func (c *compiler) fill(block, tmpl string, params []Param, args map[string]Arg) (string, bool) {
	for name := range args {
		if !slices.ContainsFunc(params, func(p Param) bool { return p.Name == name }) {
			c.fail(block, "unknown parameter %q", name)
			return "", false
		}
	}
	values := map[string]string{}
	for _, p := range params {
		arg, given := args[p.Name]
		var (
			expr string
			ok   = true
		)
		switch {
		case given && (arg.Ref != "" || arg.Value != nil):
			expr, ok = c.arg(block, p, arg)
		case p.DefaultRef != "":
			expr, ok = c.arg(block, p, Arg{Ref: p.DefaultRef})
		case p.Default != nil:
			expr, ok = c.arg(block, p, Arg{Value: p.Default})
		case p.Required:
			c.fail(block, "%s is required", p.Name)
			ok = false
		default:
			expr = "nil"
		}
		if !ok {
			return "", false
		}
		values[p.Name] = expr
	}
	return holeRE.ReplaceAllStringFunc(tmpl, func(m string) string {
		return values[m[2:len(m)-2]]
	}), true
}

func (c *compiler) arg(block string, p Param, a Arg) (string, bool) {
	if a.Ref != "" {
		return c.ref(block, a.Ref)
	}
	s, err := literalLua(p, a.Value)
	if err != nil {
		c.fail(block, "%s: %v", p.Name, err)
		return "", false
	}
	return s, true
}

// anyArg compiles a value of no declared type, as a set block stores.
func (c *compiler) anyArg(block string, a Arg) (string, bool) {
	if a.Ref != "" {
		return c.ref(block, a.Ref)
	}
	switch v := a.Value.(type) {
	case nil:
		return "nil", true
	case string:
		return LuaString(v), true
	case bool:
		return fmt.Sprint(v), true
	case float64:
		s, err := luaNumber(v)
		if err != nil {
			c.fail(block, "%v", err)
			return "", false
		}
		return s, true
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := c.anyArg(block, Arg{Value: item})
			if !ok {
				return "", false
			}
			parts = append(parts, s)
		}
		return "{" + strings.Join(parts, ", ") + "}", true
	}
	c.fail(block, "a value of type %T cannot be stored", a.Value)
	return "", false
}

func (c *compiler) ref(block, ref string) (string, bool) {
	if strings.HasPrefix(ref, "step:") {
		id, _, _ := strings.Cut(strings.TrimPrefix(ref, "step:"), ".")
		if _, ok := c.outputs[id]; !ok {
			c.fail(block, "%s refers to a block without a result", ref)
			return "", false
		}
	}
	s, err := refLua(ref)
	if err != nil {
		c.fail(block, "%v", err)
		return "", false
	}
	return s, true
}
