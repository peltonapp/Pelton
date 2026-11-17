package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/scripting"
)

// The permissions a call needs, targeted at the provider's id.
const (
	// PermCloud is for a provider that may leave this device: "ai:<id>".
	PermCloud = "ai"
	// PermLocal is for a provider on this device or the local network:
	// "ai_local:<id>". A provider whose address stops being local needs
	// PermCloud again.
	PermLocal = "ai_local"
)

// ScriptOptions are the AI switches of one script.
type ScriptOptions struct {
	// LogText puts the text sent to the model and its answers into the run
	// log. Off, the log has the provider, model, token counts and duration
	// only.
	LogText bool `json:"logText,omitempty"`
}

type askModelKey struct{}

// WithModelInTest marks a test run's context so its AI calls really ask the
// model, which a branch on the answer needs to be tried. Changes the script
// or the model asks for stay planned steps either way.
func WithModelInTest(ctx context.Context) context.Context {
	return context.WithValue(ctx, askModelKey{}, true)
}

func modelInTest(ctx context.Context) bool {
	on, _ := ctx.Value(askModelKey{}).(bool)
	return on
}

// Config is how the ai module reaches the providers.
type Config struct {
	Network Network
	// Providers returns the providers the user set up, nil for none.
	Providers func(ctx context.Context) ([]Provider, error)
	// Key returns a provider's API key, "" when none is stored.
	Key func(id string) (string, error)
	// Options returns a script's switches; nil leaves every switch off.
	Options func(scriptID int64) ScriptOptions
}

// dryText is what a call answers in a test run that does not ask the model.
const dryText = "[test run: the model was not asked]"

// callOpts are the options every call takes in its last argument.
type callOpts struct {
	provider  string
	model     string
	system    string
	maxTokens int
	timeout   time.Duration
	raw       map[string]any
}

func readOpts(c *scripting.Call, t map[string]any) (callOpts, error) {
	o := callOpts{raw: t}
	if t == nil {
		o.raw = map[string]any{}
		return o, nil
	}
	str := func(key string) (string, error) {
		switch v := t[key].(type) {
		case nil:
			return "", nil
		case string:
			return strings.TrimSpace(v), nil
		}
		return "", fmt.Errorf("%s: the option %q must be text", c.Function, key)
	}
	var err error
	if o.provider, err = str("provider"); err != nil {
		return o, err
	}
	if o.model, err = str("model"); err != nil {
		return o, err
	}
	if o.system, err = str("system"); err != nil {
		return o, err
	}
	n, err := optNumber(c, t, "max_tokens", 0, 1, maxOutputTokens)
	if err != nil {
		return o, err
	}
	o.maxTokens = int(n)
	secs, err := optNumber(c, t, "max_seconds", 0, 1, MaxTimeout.Seconds())
	if err != nil {
		return o, err
	}
	o.timeout = time.Duration(secs * float64(time.Second))
	return o, nil
}

// optNumber reads a number option, def when it is not given, and refuses one
// outside lo to hi.
func optNumber(c *scripting.Call, t map[string]any, key string, def, lo, hi float64) (float64, error) {
	v, ok := t[key]
	if !ok || v == nil {
		return def, nil
	}
	n, ok := v.(float64)
	if !ok || math.IsNaN(n) || n < lo || n > hi {
		return 0, fmt.Errorf("%s: the option %q must be a number from %g to %g", c.Function, key, lo, hi)
	}
	return n, nil
}

// maxOutputTokens is the most output one request may ask for.
const maxOutputTokens = 32768

// session is one step's provider and model, ready to send.
type session struct {
	m       *module
	c       *scripting.Call
	p       Provider
	kind    Kind
	model   string
	ep      *Endpoint
	timeout time.Duration
	// placeholder is set in a test run that does not ask the model.
	placeholder bool
	logText     bool
	calls       int
	inTokens    int
	outTokens   int
}

// open picks the provider and model, checks the permission for where the
// data goes and connects. Nothing is sent yet.
func (m *module) open(c *scripting.Call, o callOpts) (*session, error) {
	var list []Provider
	if m.cfg.Providers != nil {
		var err error
		if list, err = m.cfg.Providers(c.Ctx); err != nil {
			return nil, fmt.Errorf("%s: %w", c.Function, err)
		}
	}
	p, err := Pick(list, o.provider)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", c.Function, err)
	}
	kind, ok := KindByID(p.Kind)
	if !ok {
		return nil, fmt.Errorf("%s: the provider %q is of a kind this app does not know", c.Function, p.Name)
	}
	model := o.model
	if model == "" {
		model = p.Model
	}
	if model == "" {
		model = kind.Model
	}
	if model == "" {
		return nil, fmt.Errorf("%s: pick a model for %q, in Settings or in the step", c.Function, p.Name)
	}
	var opts ScriptOptions
	if m.cfg.Options != nil {
		opts = m.cfg.Options(c.ScriptID())
	}
	s := &session{m: m, c: c, p: p, kind: kind, model: model, timeout: o.timeout, logText: opts.LogText}
	if c.Dry && !modelInTest(c.Ctx) {
		s.placeholder = true
		return s, m.require(c, p, Local(p.BaseURL))
	}
	if !c.Granted(PermCloud+":"+p.ID) && !c.Granted(PermLocal+":"+p.ID) {
		return nil, m.require(c, p, Local(p.BaseURL))
	}
	t, err := m.cfg.Network.Resolve(c.Ctx, p)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", c.Function, err)
	}
	if err := m.require(c, p, t.Local); err != nil {
		return nil, err
	}
	key := ""
	if m.cfg.Key != nil {
		if key, err = m.cfg.Key(p.ID); err != nil {
			return nil, fmt.Errorf("%s: read the key of %q: %w", c.Function, p.Name, err)
		}
	}
	if kind.Key == KeyRequired && key == "" {
		return nil, fmt.Errorf("%s: %q has no API key; add it in Settings", c.Function, p.Name)
	}
	s.ep = m.cfg.Network.Connect(p, key, t, s.timeout)
	return s, nil
}

// require checks the permission for sending to p: the cloud one always
// does, the local one only for a provider that is local.
func (m *module) require(c *scripting.Call, p Provider, local bool) error {
	cloud := PermCloud + ":" + p.ID
	if c.Granted(cloud) {
		return nil
	}
	if local {
		return c.Require(PermLocal + ":" + p.ID)
	}
	return c.Require(cloud)
}

func redactKey(s, key string) string {
	if key == "" {
		return s
	}
	return strings.ReplaceAll(s, key, "[key]")
}

// chat sends one request with the run's clock stopped, since the request has
// its own time limit, and logs what it cost.
func (s *session) chat(req Request, timeout time.Duration) (Response, error) {
	req.Model = s.model
	if timeout <= 0 {
		timeout = s.timeout
	}
	if timeout <= 0 {
		timeout = RequestTimeout
	}
	if s.logText {
		if n := len(req.Messages); n > 0 {
			s.c.Log("info", "ai sent: "+req.Messages[n-1].Text)
		}
	}
	var (
		resp Response
		err  error
	)
	start := time.Now()
	s.c.Untimed(func() {
		ctx, cancel := context.WithTimeout(s.c.Ctx, timeout)
		defer cancel()
		resp, err = s.kind.Driver.Chat(ctx, s.ep, req)
	})
	took := time.Since(start)
	s.calls++
	s.inTokens += resp.InputTokens
	s.outTokens += resp.OutputTokens
	line := fmt.Sprintf("ai: %s, model %s, %d tokens in, %d out, %s", s.p.Name, s.model, resp.InputTokens, resp.OutputTokens, took.Round(time.Millisecond))
	if err != nil {
		line += ", failed"
	}
	s.c.Log("info", line)
	if s.logText && err == nil {
		answer := resp.Text
		for _, tc := range resp.ToolCalls {
			answer += fmt.Sprintf("\n[calls %s %s]", tc.Name, argsJSON(tc.Args))
		}
		s.c.Log("info", "ai answered: "+answer)
	}
	if err != nil {
		return Response{}, errors.New(redactKey(err.Error(), s.ep.Key))
	}
	return resp, nil
}

// result is the table every call returns: whether it worked, the text and
// what it cost.
func (s *session) result(text string, err error) map[string]any {
	r := map[string]any{
		"ok":            err == nil,
		"text":          text,
		"provider":      s.p.ID,
		"model":         s.model,
		"input_tokens":  s.inTokens,
		"output_tokens": s.outTokens,
	}
	if err != nil {
		r["error"] = err.Error()
		r["text"] = ""
	}
	if s.placeholder {
		r["dry_run"] = true
	}
	return r
}

// input turns what a call was given to look at into text for the model, and
// says what it is. A message is read with its body, through mail.body, so
// the script needs the permission to read mail.
func input(c *scripting.Call, v any) (string, string, error) {
	switch x := v.(type) {
	case nil:
		return "", "", nil
	case string:
		return x, "data", nil
	case float64:
		return fmt.Sprint(x), "data", nil
	case scripting.Object:
		return input(c, x.Fields)
	case map[string]any:
		if _, isMessage := x["subject"]; isMessage {
			if _, hasID := x["id"]; hasID {
				return messageText(c, x)
			}
		}
	}
	b, err := json.Marshal(plain(v))
	if err != nil {
		return "", "", fmt.Errorf("%s: cannot hand this value to the model", c.Function)
	}
	return string(b), "data", nil
}

func messageText(c *scripting.Call, m map[string]any) (string, string, error) {
	body, ok := m["body"].(string)
	if !ok {
		res, err := c.Invoke("mail.body", []any{m})
		if err != nil {
			return "", "", err
		}
		if len(res) > 0 {
			body, _ = res[0].(string)
		}
	}
	var b strings.Builder
	str := func(k string) string { s, _ := m[k].(string); return s }
	from := str("from")
	if name := str("from_name"); name != "" {
		from = name + " <" + from + ">"
	}
	fmt.Fprintf(&b, "From: %s\n", from)
	if to := str("to"); to != "" {
		fmt.Fprintf(&b, "To: %s\n", to)
	}
	if d, ok := m["date"].(float64); ok && d > 0 {
		fmt.Fprintf(&b, "Date: %s\n", time.Unix(int64(d), 0).Format("2006-01-02 15:04"))
	}
	fmt.Fprintf(&b, "Subject: %s\n\n%s", str("subject"), body)
	return b.String(), "email", nil
}

// plain turns a value a host function returned into one JSON can encode.
func plain(v any) any {
	switch x := v.(type) {
	case scripting.Object:
		return plain(x.Fields)
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, item := range x {
			out[k] = plain(item)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = plain(item)
		}
		return out
	case []string:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = item
		}
		return out
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return nil
		}
	}
	return v
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
