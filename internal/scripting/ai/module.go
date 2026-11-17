package ai

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/peltonapp/Pelton/internal/scripting"
)

type module struct {
	cfg Config
	reg *scripting.Registry
}

// Output caps of the single step calls, when the step sets none.
const (
	askTokens      = 2048
	classifyTokens = 512
	extractTokens  = 1024
)

func (m *module) ask(c *scripting.Call) ([]any, error) {
	prompt, err := c.String(0)
	if err != nil {
		return nil, err
	}
	o, err := readOpts(c, c.Table(1))
	if err != nil {
		return nil, err
	}
	data, label, err := input(c, o.raw["input"])
	if err != nil {
		return nil, err
	}
	s, err := m.open(c, o)
	if err != nil {
		return nil, err
	}
	if s.placeholder {
		return []any{s.result(dryText, nil)}, nil
	}
	text := prompt
	if data != "" {
		text += "\n\n" + fence(label, data)
	}
	resp, err := s.chat(Request{System: system(o.system), Messages: []Message{{Role: "user", Text: text}}, MaxTokens: tokens(o, askTokens)}, 0)
	return []any{s.result(resp.Text, err)}, nil
}

func tokens(o callOpts, def int) int {
	if o.maxTokens > 0 {
		return o.maxTokens
	}
	return def
}

// categories reads the list of categories at argument i.
func categories(c *scripting.Call, i int) ([]string, error) {
	list, err := c.List(i)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, s := range list {
		s = strings.TrimSpace(s)
		if s != "" && !slices.ContainsFunc(out, func(o string) bool { return strings.EqualFold(o, s) }) {
			out = append(out, s)
		}
	}
	if len(out) < 2 {
		return nil, fmt.Errorf("%s: name at least two categories", c.Function)
	}
	if len(out) > maxCategories {
		return nil, fmt.Errorf("%s: at most %d categories", c.Function, maxCategories)
	}
	return out, nil
}

// errNoLabel is what classify returns when the model twice answered with
// something that is not a category.
var errNoLabel = errors.New("the model did not answer with one of the categories")

// pick asks the model to put data into one of cats, once more when the
// answer is not one of them.
func (s *session) pick(task, data, label string, cats []string, o callOpts) (string, error) {
	var list strings.Builder
	for _, c := range cats {
		fmt.Fprintf(&list, "- %s\n", c)
	}
	prompt := task + "\n\nThe categories:\n" + list.String() +
		"\nAnswer with a JSON object of the form {\"label\": \"<category>\"}, where <category> is exactly one of the categories, and nothing else."
	if data != "" {
		prompt += "\n\n" + fence(label, data)
	}
	req := Request{
		System:    system("You sort data into categories. You always answer with exactly one of the categories you are given."),
		Messages:  []Message{{Role: "user", Text: prompt}},
		MaxTokens: tokens(o, classifyTokens),
		Schema:    classifySchema(cats),
	}
	for attempt := 0; attempt < 2; attempt++ {
		resp, err := s.chat(req, 0)
		if err != nil {
			return "", err
		}
		if got, ok := matchLabel(resp.Text, cats); ok {
			return got, nil
		}
		req.Messages = append(req.Messages,
			Message{Role: "assistant", Text: resp.Text},
			Message{Role: "user", Text: "That is not one of the categories. Answer with {\"label\": \"<category>\"} where <category> is exactly one of: " + strings.Join(cats, ", ") + "."})
	}
	return "", errNoLabel
}

func (m *module) classify(c *scripting.Call) ([]any, error) {
	data, label, err := input(c, c.Arg(0))
	if err != nil {
		return nil, err
	}
	cats, err := categories(c, 1)
	if err != nil {
		return nil, err
	}
	o, err := readOpts(c, c.Table(2))
	if err != nil {
		return nil, err
	}
	s, err := m.open(c, o)
	if err != nil {
		return nil, err
	}
	if s.placeholder {
		r := s.result(dryText, nil)
		r["label"] = cats[0]
		return []any{r}, nil
	}
	task := "Put the data below into exactly one of the categories."
	if hint, _ := o.raw["instructions"].(string); strings.TrimSpace(hint) != "" {
		task += "\n" + strings.TrimSpace(hint)
	}
	got, err := s.pick(task, data, label, cats, o)
	r := s.result(got, err)
	if err == nil {
		r["label"] = got
	}
	return []any{r}, nil
}

func (m *module) check(c *scripting.Call) ([]any, error) {
	question, err := c.String(0)
	if err != nil {
		return nil, err
	}
	data, label, err := input(c, c.Arg(1))
	if err != nil {
		return nil, err
	}
	o, err := readOpts(c, c.Table(2))
	if err != nil {
		return nil, err
	}
	s, err := m.open(c, o)
	if err != nil {
		return nil, err
	}
	if s.placeholder {
		r := s.result(dryText, nil)
		r["label"], r["yes"] = "yes", true
		return []any{r}, nil
	}
	got, err := s.pick("Answer this question about the data below with yes or no.\nThe question: "+question, data, label, []string{"yes", "no"}, o)
	r := s.result(got, err)
	if err == nil {
		r["label"], r["yes"] = got, got == "yes"
	} else {
		r["yes"] = false
	}
	return []any{r}, nil
}

func (m *module) summarise(c *scripting.Call) ([]any, error) {
	data, label, err := input(c, c.Arg(0))
	if err != nil {
		return nil, err
	}
	o, err := readOpts(c, c.Table(1))
	if err != nil {
		return nil, err
	}
	length, _ := o.raw["length"].(string)
	if length == "" {
		length = "short"
	}
	size, ok := summaryLength[length]
	if !ok {
		return nil, fmt.Errorf("%s: the length is short, medium or long", c.Function)
	}
	s, err := m.open(c, o)
	if err != nil {
		return nil, err
	}
	if s.placeholder {
		return []any{s.result(dryText, nil)}, nil
	}
	task := "Summarise the data below " + size.ask + ". Write only the summary."
	if lang, _ := o.raw["language"].(string); strings.TrimSpace(lang) != "" {
		task += " Write it in " + strings.TrimSpace(lang) + "."
	}
	resp, err := s.chat(Request{
		System:    system(o.system),
		Messages:  []Message{{Role: "user", Text: task + "\n\n" + fence(label, data)}},
		MaxTokens: tokens(o, size.tokens),
	}, 0)
	return []any{s.result(resp.Text, err)}, nil
}

func (m *module) extract(c *scripting.Call) ([]any, error) {
	data, label, err := input(c, c.Arg(0))
	if err != nil {
		return nil, err
	}
	fields, err := ParseFields(c.Arg(1))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", c.Function, err)
	}
	o, err := readOpts(c, c.Table(2))
	if err != nil {
		return nil, err
	}
	s, err := m.open(c, o)
	if err != nil {
		return nil, err
	}
	if s.placeholder {
		r := s.result(dryText, nil)
		r["data"] = map[string]any{}
		return []any{r}, nil
	}
	prompt := "Find these fields in the data below:\n" + describeFields(fields) +
		"\nAnswer with a JSON object that has exactly these fields, and nothing else. Use null for a field the data has no value for; do not guess." +
		"\n\n" + fence(label, data)
	req := Request{
		System:    system("You read data and pull out the fields you are asked for, as JSON."),
		Messages:  []Message{{Role: "user", Text: prompt}},
		MaxTokens: tokens(o, extractTokens),
		Schema:    extractSchema(fields),
	}
	var out map[string]any
	for attempt := 0; attempt < 2; attempt++ {
		resp, cerr := s.chat(req, 0)
		if cerr != nil {
			err = cerr
			break
		}
		if out, err = CheckExtracted(resp.Text, fields); err == nil {
			break
		}
		req.Messages = append(req.Messages,
			Message{Role: "assistant", Text: resp.Text},
			Message{Role: "user", Text: "That answer does not fit: " + err.Error() + ". Answer again with only the JSON object."})
		err = fmt.Errorf("the model's answer did not fit the fields: %w", err)
	}
	r := s.result("", err)
	if err == nil {
		r["data"] = out
	}
	return []any{r}, nil
}

func (m *module) draftReply(c *scripting.Call) ([]any, error) {
	msg, ok := c.Arg(0).(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: argument 1 must be a message", c.Function)
	}
	data, label, err := input(c, msg)
	if err != nil {
		return nil, err
	}
	o, err := readOpts(c, c.Table(1))
	if err != nil {
		return nil, err
	}
	s, err := m.open(c, o)
	if err != nil {
		return nil, err
	}
	if s.placeholder {
		return []any{s.result(dryText, nil)}, nil
	}
	task := "Write the body of a reply to the email below, for the user to send. Write only the body text: no subject line, no explanation."
	if hint, _ := o.raw["instructions"].(string); strings.TrimSpace(hint) != "" {
		task += "\nThe user's instructions for the reply: " + strings.TrimSpace(hint)
	}
	if lang, _ := o.raw["language"].(string); strings.TrimSpace(lang) != "" {
		task += "\nWrite it in " + strings.TrimSpace(lang) + "."
	}
	resp, err := s.chat(Request{
		System:    system(o.system),
		Messages:  []Message{{Role: "user", Text: task + "\n\n" + fence(label, data)}},
		MaxTokens: tokens(o, askTokens),
	}, 0)
	return []any{s.result(resp.Text, err)}, nil
}

// transform asks for the text at argument 0 changed as task says, with the
// text fenced as data.
func (m *module) transform(c *scripting.Call, task string, o callOpts) ([]any, error) {
	src, err := c.String(0)
	if err != nil {
		return nil, err
	}
	s, err := m.open(c, o)
	if err != nil {
		return nil, err
	}
	if s.placeholder {
		return []any{s.result(dryText, nil)}, nil
	}
	resp, err := s.chat(Request{
		System:    system(o.system),
		Messages:  []Message{{Role: "user", Text: task + " Write only the new text, no explanation.\n\n" + fence("text", src)}},
		MaxTokens: tokens(o, askTokens),
	}, 0)
	return []any{s.result(resp.Text, err)}, nil
}

func (m *module) rewrite(c *scripting.Call) ([]any, error) {
	how, err := c.String(1)
	if err != nil {
		return nil, err
	}
	o, err := readOpts(c, c.Table(2))
	if err != nil {
		return nil, err
	}
	return m.transform(c, "Rewrite the text below as follows: "+strings.TrimSpace(how)+".", o)
}

func (m *module) translate(c *scripting.Call) ([]any, error) {
	lang, err := c.String(1)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(lang) == "" {
		return nil, fmt.Errorf("%s: name the language", c.Function)
	}
	o, err := readOpts(c, c.Table(2))
	if err != nil {
		return nil, err
	}
	return m.transform(c, "Translate the text below into "+strings.TrimSpace(lang)+", keeping its meaning, tone and layout.", o)
}

// summary describes a call for the run log by its provider and model only,
// so the text sent is not logged; opt is the index of the options argument.
func summary(opt int) func(args []any) string {
	return func(args []any) string {
		t := map[string]any{}
		if opt < len(args) {
			t, _ = args[opt].(map[string]any)
		}
		provider, _ := t["provider"].(string)
		if provider == "" {
			provider = "default"
		}
		out := "provider " + provider
		if model, _ := t["model"].(string); model != "" {
			out += ", model " + model
		}
		return out
	}
}
