package scripting

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
)

// InputKind is a question a running script can put to the user: a line of
// text, a pick from a list, a folder. The frontend draws the question by its
// Control, so a kind another package registers with an existing control
// needs no frontend change.
type InputKind struct {
	Meta
	// Control names the frontend control that asks, e.g. "text" or "choice".
	Control string `json:"control"`
	// Value is the type of the answer, a ParamType or a value type id.
	Value string `json:"value"`
	// Params are what the question takes besides its prompt and default, e.g.
	// the options of a choice.
	Params []Param `json:"params,omitempty"`
	// Check turns the answer the control gave into the value the script
	// gets, or refuses it. Nil passes it through.
	Check func(c *Call, answer any, params map[string]any) (any, error) `json:"-"`
}

// ResultAction is a button of the result panel a script's output shows in,
// e.g. "copy" or "save as note".
type ResultAction struct {
	Meta
	// Icon names an icon of the app's script icon set.
	Icon string `json:"icon"`
	// Client names the frontend handler of an action only the window can do,
	// such as copying; it is empty for an action Run carries out.
	Client string `json:"client,omitempty"`
	// Permission is a permission the script that showed the result must hold
	// for the action to be offered.
	Permission string `json:"permission,omitempty"`
	// Run carries out the action on a result and returns a short message for
	// the user.
	Run func(ctx context.Context, r Result) (string, error) `json:"-"`
}

// Result is longer output a script shows in the result panel.
type Result struct {
	ScriptID   int64  `json:"scriptId"`
	ScriptName string `json:"scriptName"`
	Title      string `json:"title"`
	Text       string `json:"text"`
	// Format is "text" or "markdown".
	Format string `json:"format"`
	// Actions are the ids of the result actions on offer.
	Actions []string `json:"actions"`
	Dry     bool     `json:"dry"`
}

// AskRequest is a question a running script puts to the user.
type AskRequest struct {
	ScriptID   int64  `json:"scriptId"`
	ScriptName string `json:"scriptName"`
	// Kind is the input kind and Control the control that asks.
	Kind    string         `json:"kind"`
	Control string         `json:"control"`
	Prompt  string         `json:"prompt"`
	Params  map[string]any `json:"params"`
	Default any            `json:"default"`
	Dry     bool           `json:"dry"`
	// Attended is set when the user started the run; otherwise the question
	// gives up after Timeout.
	Attended bool          `json:"attended"`
	Timeout  time.Duration `json:"-"`
}

// AskAnswer is the user's answer. Cancelled ends the run.
type AskAnswer struct {
	Value     any
	Cancelled bool
}

// Asker puts a script's question to the user. It blocks until they answer or
// ctx ends.
type Asker interface {
	Ask(ctx context.Context, req AskRequest) (AskAnswer, error)
}

// ResultHost shows a script's result panel.
type ResultHost interface {
	Show(ctx context.Context, r Result) error
}

// ErrRunCancelled is the cause of a run the user cancelled.
var ErrRunCancelled = errors.New("scripting: cancelled")

// How long a question of a run nobody started waits: by default, and the
// shortest and longest the setting or a question's own timeout may set.
const (
	DefaultAskTimeout = 2 * time.Minute
	MinAskTimeout     = 10 * time.Second
	MaxAskTimeout     = time.Hour
)

// ClampAskTimeout keeps d within MinAskTimeout and MaxAskTimeout.
func ClampAskTimeout(d time.Duration) time.Duration {
	return min(max(d, MinAskTimeout), MaxAskTimeout)
}

// maxResultText caps the text of one result, and maxPromptBytes a question.
const (
	maxResultText  = 256 << 10
	maxPromptBytes = 2000
)

// RegisterInputKind adds an input kind, and with it the block action
// "ui.<id>" that asks it and outputs the answer as "value".
func (r *Registry) RegisterInputKind(k InputKind) error {
	if err := checkMeta(k.Meta); err != nil {
		return err
	}
	if !luaName.MatchString(k.ID) {
		return fmt.Errorf("scripting: %q cannot name an input kind", k.ID)
	}
	if k.Control == "" || k.Value == "" {
		return fmt.Errorf("scripting: input kind %q needs a control and a value type", k.ID)
	}
	englishMeta(&k.Meta)
	k.Params = englishParams(k.Params)
	reserved := []string{"prompt", "default", "timeout"}
	for _, p := range k.Params {
		if slices.Contains(reserved, p.Name) {
			return fmt.Errorf("scripting: input kind %q cannot have a parameter %q", k.ID, p.Name)
		}
	}
	r.mu.Lock()
	err := r.checkParams(k.ID, k.Params)
	if err == nil {
		err = r.inputKinds.add(k.ID, k)
	}
	r.mu.Unlock()
	if err != nil {
		return err
	}
	return r.RegisterAction(askAction(k))
}

// askAction is the block action that asks an input kind.
func askAction(k InputKind) Action {
	prompt := Required(NewParam("prompt", TypeString))
	params := []Param{prompt}
	params = append(params, k.Params...)
	opts := make([]string, 0, len(k.Params)+2)
	for _, p := range k.Params {
		opts = append(opts, p.Name+" = {{"+p.Name+"}}")
	}
	if slices.Contains(paramTypes, ParamType(k.Value)) && ParamType(k.Value) != TypeRef && ParamType(k.Value) != TypeList {
		def := NewParam("default", ParamType(k.Value))
		params = append(params, def)
		opts = append(opts, "default = {{default}}")
	}
	params = append(params, NewParam("timeout", TypeDuration))
	opts = append(opts, "timeout = {{timeout}}")
	return Action{
		Meta: Meta{
			ID:          "ui." + k.ID,
			LabelKey:    "scripts.action.ui." + k.ID,
			DescKey:     "scripts.action.ui." + k.ID + ".desc",
			Label:       cmp.Or(englishFor("scripts.action.ui."+k.ID), k.Label),
			Description: cmp.Or(englishFor("scripts.action.ui."+k.ID+".desc"), k.Description),
			Category:    "ui",
		},
		Params:  params,
		Outputs: []Field{{Name: "value", Type: k.Value, LabelKey: "scripts.field.value"}},
		Lua:     "{ value = ui.ask(" + LuaString(k.ID) + ", {{prompt}}, { " + strings.Join(opts, ", ") + " }) }",
	}
}

// RegisterResultAction adds a button of the result panel.
func (r *Registry) RegisterResultAction(a ResultAction) error {
	if err := checkMeta(a.Meta); err != nil {
		return err
	}
	if (a.Client == "") == (a.Run == nil) {
		return fmt.Errorf("scripting: result action %q needs either a client handler or Run", a.ID)
	}
	englishMeta(&a.Meta)
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.checkPermission(a.Permission, a.ID); err != nil {
		return err
	}
	return r.resultActions.add(a.ID, a)
}

// InputKind returns a registered input kind.
func (r *Registry) InputKind(id string) (InputKind, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	k, ok := r.inputKinds.byID[id]
	return k, ok
}

// ResultAction returns a registered result action.
func (r *Registry) ResultAction(id string) (ResultAction, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.resultActions.byID[id]
	return a, ok
}

// resultActionsFor lists the result actions a script may offer.
func (r *Registry) resultActionsFor(sc *Script, dry bool) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []string{}
	for _, a := range r.resultActions.list() {
		if sc.granted(a.Permission, dry) {
			out = append(out, a.ID)
		}
	}
	return out
}

// ask puts a question to the user with the run's clock stopped. A question
// of a run nobody started gives up after its timeout and takes the default,
// or stops the run when there is none. Cancelling ends the run as cancelled.
func (st *runState) ask(c *Call, k InputKind, prompt string, opts map[string]any) (any, error) {
	params := map[string]any{}
	for _, p := range k.Params {
		v, ok := opts[p.Name]
		if !ok || v == nil {
			if p.Default != nil {
				v = p.Default
			} else if p.Required {
				return nil, fmt.Errorf("ui.ask: %s needs %q", k.ID, p.Name)
			}
		}
		if v != nil {
			params[p.Name] = v
		}
	}
	def := opts["default"]
	timeout := DefaultAskTimeout
	if st.e.AskTimeout != nil {
		if d := st.e.AskTimeout(); d > 0 {
			timeout = d
		}
	}
	if secs, ok := toFloat(opts["timeout"]); ok && secs > 0 {
		timeout = time.Duration(min(secs, MaxAskTimeout.Seconds()) * float64(time.Second))
	}
	timeout = ClampAskTimeout(timeout)
	summary := clip(prompt, 120)
	hosts := st.e.Hosts
	if hosts == nil || hosts.Ask == nil {
		if def != nil {
			st.addStep(Step{Kind: StepAsk, Function: "ui." + k.ID, Summary: summary + " -> " + display(def), Error: "nobody to ask"})
			return def, nil
		}
		st.addStep(Step{Kind: StepAsk, Function: "ui." + k.ID, Summary: summary, Error: "nobody to ask"})
		return nil, st.setFatal(StatusStopped, "nobody to ask")
	}
	req := AskRequest{
		ScriptID: st.req.Script.ID, ScriptName: st.req.Script.Name,
		Kind: k.ID, Control: k.Control, Prompt: prompt, Params: params, Default: def,
		Dry: st.req.Dry, Attended: st.req.Attended,
	}
	ctx := st.ctx
	if !st.req.Attended {
		req.Timeout = timeout
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	stopped := st.pauseClock()
	st.setWaiting(true)
	ans, err := hosts.Ask.Ask(ctx, req)
	st.setWaiting(false)
	if stopped {
		st.resumeClock()
	}
	if st.ctx.Err() != nil {
		return nil, st.ctxFatal()
	}
	if ans.Cancelled {
		st.addStep(Step{Kind: StepAsk, Function: "ui." + k.ID, Summary: summary, Error: "cancelled"})
		return nil, st.setFatal(StatusCancelled, "cancelled")
	}
	if err != nil {
		if def != nil {
			st.addStep(Step{Kind: StepAsk, Function: "ui." + k.ID, Summary: summary + " -> " + display(def), Error: "no answer, took the default"})
			return def, nil
		}
		st.addStep(Step{Kind: StepAsk, Function: "ui." + k.ID, Summary: summary, Error: "no answer"})
		return nil, st.setFatal(StatusStopped, "no answer")
	}
	value := ans.Value
	if k.Check != nil {
		value, err = k.Check(c, value, params)
		if err != nil {
			return nil, err
		}
	}
	st.addStep(Step{Kind: StepAsk, Function: "ui." + k.ID, Summary: summary + " -> " + clip(display(value), 120)})
	return value, nil
}

// show hands a result to the result panel.
func (st *runState) show(c *Call, title, text, format string) error {
	hosts := st.e.Hosts
	if hosts == nil || hosts.Result == nil {
		return ErrUnavailable
	}
	if len(text) > maxResultText {
		text = clip(text, maxResultText)
	}
	if format != "markdown" {
		format = "text"
	}
	sc := st.req.Script
	st.addStep(Step{Kind: StepLog, Level: "info", Function: "ui.show", Summary: "showed " + clip(firstLine(title, text), 80)})
	return hosts.Result.Show(c.Ctx, Result{
		ScriptID: sc.ID, ScriptName: sc.Name, Title: title, Text: text, Format: format,
		Actions: st.e.Registry.resultActionsFor(sc, st.req.Dry), Dry: st.req.Dry,
	})
}

func firstLine(title, text string) string {
	if title != "" {
		return title
	}
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	return line
}

func uiModule() Module {
	ask := func(c *Call, kind string, prompt any, opts map[string]any) ([]any, error) {
		k, ok := c.st.e.Registry.InputKind(kind)
		if !ok {
			return nil, fmt.Errorf("ui.ask: there is no question kind %q", kind)
		}
		text, _ := prompt.(string)
		text = clip(text, maxPromptBytes)
		if opts == nil {
			opts = map[string]any{}
		}
		v, err := c.st.ask(c, k, text, opts)
		if err != nil {
			return nil, err
		}
		return []any{v}, nil
	}
	generic := doc(fn("ask", func(c *Call) ([]any, error) {
		kind, err := c.String(0)
		if err != nil {
			return nil, err
		}
		return ask(c, kind, c.Arg(1), c.Table(2))
	}, required(param("kind", TypeString)), required(param("prompt", TypeString)), param("options", TypeRef)), modDesc("ui", "ask"))
	short := func(name, kind string) Function {
		return doc(fn(name, func(c *Call) ([]any, error) {
			return ask(c, kind, c.Arg(0), c.Table(1))
		}, required(param("prompt", TypeString)), param("options", TypeRef)), modDesc("ui", name))
	}
	choose := doc(fn("choose", func(c *Call) ([]any, error) {
		opts := c.Table(2)
		if opts == nil {
			opts = map[string]any{}
		}
		opts["options"] = c.Arg(1)
		return ask(c, "choice", c.Arg(0), opts)
	}, required(param("prompt", TypeString)), required(param("options", TypeRef)), param("settings", TypeRef)), modDesc("ui", "choose"))
	show := doc(fn("show", func(c *Call) ([]any, error) {
		text, err := c.String(0)
		if err != nil {
			return nil, err
		}
		opts := c.Table(1)
		title, _ := opts["title"].(string)
		format, _ := opts["format"].(string)
		return nil, c.st.show(c, clip(title, 200), text, format)
	}, required(param("text", TypeText)), param("options", TypeRef)), modDesc("ui", "show"))
	return Module{
		Meta: meta("module", "ui", "ui"),
		Functions: []Function{
			generic, short("text", "text"), short("textarea", "textarea"), choose, short("confirm", "confirm"),
			short("folder", "folder"), short("account", "account"), short("datetime", "datetime"), show,
		},
	}
}

func checkText(_ *Call, v any, _ map[string]any) (any, error) {
	s, ok := v.(string)
	if !ok {
		return nil, errors.New("ui: the answer is not text")
	}
	return s, nil
}

func checkChoice(_ *Call, v any, params map[string]any) (any, error) {
	s, ok := v.(string)
	if !ok {
		return nil, errors.New("ui: the answer is not one of the options")
	}
	list, _ := params["options"].([]any)
	if !slices.Contains(list, any(s)) {
		return nil, fmt.Errorf("ui: %q is not one of the options", s)
	}
	return s, nil
}

func checkBool(_ *Call, v any, _ map[string]any) (any, error) {
	b, ok := v.(bool)
	if !ok {
		return nil, errors.New("ui: the answer is not yes or no")
	}
	return b, nil
}

func checkNumber(_ *Call, v any, _ map[string]any) (any, error) {
	f, ok := toFloat(v)
	if !ok || math.IsNaN(f) || math.IsInf(f, 0) {
		return nil, errors.New("ui: the answer is not a number")
	}
	return f, nil
}

func registerInteraction(r *Registry) error {
	if err := r.RegisterModule(uiModule()); err != nil {
		return err
	}
	choices := Required(NewParam("options", TypeList))
	choices.Of = TypeString
	kinds := []InputKind{
		{Meta: meta("input", "text", "ui"), Control: "text", Value: string(TypeString), Check: checkText},
		{Meta: meta("input", "textarea", "ui"), Control: "textarea", Value: string(TypeText), Check: checkText},
		{Meta: meta("input", "choice", "ui"), Control: "choice", Value: string(TypeString), Params: []Param{choices}, Check: checkChoice},
		{Meta: meta("input", "confirm", "ui"), Control: "confirm", Value: string(TypeBool), Check: checkBool},
		{Meta: meta("input", "folder", "ui"), Control: "folder", Value: string(TypeFolder), Check: checkText},
		{Meta: meta("input", "account", "ui"), Control: "account", Value: string(TypeAccount), Check: checkNumber},
		{Meta: meta("input", "datetime", "ui"), Control: "datetime", Value: string(TypeNumber), Check: checkNumber},
	}
	for _, k := range kinds {
		if err := r.RegisterInputKind(k); err != nil {
			return err
		}
	}
	format := withDefault(param("format", TypeEnum), "text")
	format.Options = options("text", "markdown")
	if err := r.RegisterAction(Action{
		Meta:   meta("action", "ui.show", "ui"),
		Params: []Param{required(param("text", TypeText)), param("title", TypeString), format},
		Lua:    "ui.show({{text}}, { title = {{title}}, format = {{format}} })",
	}); err != nil {
		return err
	}
	for _, a := range []ResultAction{
		{Meta: meta("result", "copy", "ui"), Icon: "copy", Client: "copy"},
		{Meta: meta("result", "reply", "ui"), Icon: "reply", Client: "reply"},
	} {
		if err := r.RegisterResultAction(a); err != nil {
			return err
		}
	}
	return nil
}
