package ai

import (
	"github.com/peltonapp/Pelton/internal/scripting"
)

var (
	meta     = scripting.EntryMeta
	param    = scripting.NewParam
	field    = scripting.NewField
	required = scripting.Required
	withDef  = scripting.WithDefault
)

// actionIDs are the block actions, which SetAvailable marks missing while no
// provider is set up.
var actionIDs = []string{
	"ai.ask", "ai.check", "ai.classify", "ai.summarise", "ai.extract",
	"ai.draft_reply", "ai.rewrite", "ai.translate", "ai.agent",
}

// SetAvailable marks the block actions as available or not, by whether the
// user has set up a provider.
func SetAvailable(r *scripting.Registry, hasProvider bool) {
	reason := ""
	if !hasProvider {
		reason = "no AI provider is set up; add one in Settings"
	}
	for _, id := range actionIDs {
		r.SetMissing(id, reason)
	}
}

func fields(specs ...string) []scripting.Field {
	out := make([]scripting.Field, 0, len(specs))
	for _, s := range specs {
		name, typ := s, "string"
		for i := range s {
			if s[i] == ':' {
				name, typ = s[:i], s[i+1:]
				break
			}
		}
		out = append(out, field(name, typ))
	}
	return out
}

// Register adds the ai permissions, the ai module and its block actions and
// conditions. Nothing is contacted until a script calls the module.
func Register(r *scripting.Registry, cfg Config) error {
	m := &module{cfg: cfg, reg: r}

	options := param("options", scripting.TypeRef)
	prompt := required(param("prompt", scripting.TypeText))
	question := required(param("question", scripting.TypeText))
	inputRef := scripting.RefParam("input", "", "")
	inputMsg := scripting.RefParam("input", "", "event.message")
	message := required(scripting.RefParam("message", "message", "event.message"))
	categories := required(param("categories", scripting.TypeList))
	categories.Of = scripting.TypeString
	fieldList := required(param("fields", scripting.TypeList))
	fieldList.Of = scripting.TypeString
	textParam := required(param("text", scripting.TypeText))
	instructions := param("instructions", scripting.TypeText)
	language := required(param("language", scripting.TypeString))
	optLanguage := param("language", scripting.TypeString)
	systemParam := param("system", scripting.TypeText)
	provider := param("provider", scripting.TypeString)
	model := param("model", scripting.TypeString)
	length := required(withDef(param("length", scripting.TypeEnum), "short"))
	length.Options = scripting.NewOptions("short", "medium", "long")
	toolNames := param("tools", scripting.TypeList)
	toolNames.Of = scripting.TypeString
	maxSteps := withDef(param("max_steps", scripting.TypeNumber), float64(DefaultAgentSteps))
	maxTotal := withDef(param("max_total_tokens", scripting.TypeNumber), float64(DefaultAgentTokens))
	maxSeconds := withDef(param("max_seconds", scripting.TypeDuration), "2m")
	all := withDef(param("all", scripting.TypeBool), false)
	category := required(param("category", scripting.TypeString))

	cost := []string{"provider", "model", "input_tokens:number", "output_tokens:number"}
	returns := func(extra ...string) []scripting.Field {
		return fields(append(append([]string{"ok:bool", "text:text", "error"}, extra...), cost...)...)
	}
	fn := func(name string, h scripting.Handler, opt int, ret []scripting.Field, params ...scripting.Param) scripting.Function {
		f := scripting.NewFunction("ai", name, h, params...)
		f.Effect = true
		f.Dry = h
		f.Summary = summary(opt)
		f.Returns = ret
		return f
	}
	fns := []scripting.Function{
		fn("ask", m.ask, 1, returns(), prompt, options),
		fn("check", m.check, 2, returns("yes:bool", "label"), question, inputRef, options),
		fn("classify", m.classify, 2, returns("label"), inputRef, categories, options),
		fn("summarise", m.summarise, 1, returns(), inputRef, options),
		fn("extract", m.extract, 2, returns("data:table"), inputRef, fieldList, options),
		fn("draft_reply", m.draftReply, 1, returns(), message, options),
		fn("rewrite", m.rewrite, 2, returns(), textParam, required(instructions), options),
		fn("translate", m.translate, 2, returns(), textParam, language, options),
		fn("agent", m.agent, 1, returns("model_calls:number", "tool_calls:number"), prompt, options),
	}

	const pick = "provider = {{provider}}, model = {{model}}"
	action := func(id, lua string, outputs []string, params ...scripting.Param) scripting.Action {
		return scripting.Action{
			Meta:       meta("action", id, "ai"),
			Params:     append(params, provider, model),
			Outputs:    fields(append([]string{"ok:bool", "error"}, outputs...)...),
			Permission: PermCloud,
			Lua:        lua,
		}
	}
	withPerms := func(a scripting.Action, perms ...string) scripting.Action {
		a.Permissions = append([]string{a.Permission}, perms...)
		return a
	}
	actions := []scripting.Action{
		action("ai.ask", "ai.ask({{prompt}}, {input = {{input}}, system = {{system}}, "+pick+"})",
			[]string{"text:text"}, prompt, inputRef, systemParam),
		action("ai.check", "ai.check({{question}}, {{input}}, {"+pick+"})",
			[]string{"yes:bool"}, question, inputMsg),
		action("ai.classify", "ai.classify({{input}}, {{categories}}, {instructions = {{instructions}}, "+pick+"})",
			[]string{"label"}, inputMsg, categories, instructions),
		action("ai.summarise", "ai.summarise({{input}}, {length = {{length}}, language = {{language}}, "+pick+"})",
			[]string{"text:text"}, inputMsg, length, optLanguage),
		action("ai.extract", "ai.extract({{input}}, {{fields}}, {"+pick+"})",
			[]string{"data:table"}, inputMsg, fieldList),
		withPerms(action("ai.draft_reply",
			"(function() local r = ai.draft_reply({{message}}, {instructions = {{instructions}}, language = {{language}}, "+pick+"}) "+
				"if r.ok then r.draft_id = mail.reply({{message}}, {body = r.text, all = {{all}}}).id end return r end)()",
			[]string{"text:text", "draft_id:number"}, message, instructions, optLanguage, all), scripting.PermMailCompose),
		action("ai.rewrite", "ai.rewrite({{text}}, {{instructions}}, {"+pick+"})",
			[]string{"text:text"}, textParam, required(instructions)),
		action("ai.translate", "ai.translate({{text}}, {{language}}, {"+pick+"})",
			[]string{"text:text"}, textParam, language),
		action("ai.agent",
			"ai.agent({{prompt}}, {input = {{input}}, system = {{system}}, tools = {{tools}}, max_steps = {{max_steps}}, "+
				"max_total_tokens = {{max_total_tokens}}, max_seconds = {{max_seconds}}, "+pick+"})",
			[]string{"text:text", "tool_calls:number"}, prompt, inputRef, systemParam, toolNames, maxSteps, maxTotal, maxSeconds),
	}
	conditions := []scripting.Condition{
		{
			Meta:       meta("cond", "ai.says_yes", "ai"),
			Params:     []scripting.Param{question, inputMsg, provider, model},
			Permission: PermCloud,
			Lua:        "ai.check({{question}}, {{input}}, {" + pick + "}).yes == true",
		},
		{
			Meta:       meta("cond", "ai.category_is", "ai"),
			Params:     []scripting.Param{inputMsg, categories, category, provider, model},
			Permission: PermCloud,
			Lua:        "ai.classify({{input}}, {{categories}}, {" + pick + "}).label == {{category}}",
		},
	}
	return r.Register(scripting.Entries{
		Permissions: []scripting.Permission{
			{Meta: meta("perm", PermCloud, "ai"), Risk: "high"},
			{Meta: meta("perm", PermLocal, "ai"), Risk: "medium"},
		},
		Modules:    []scripting.Module{{Meta: meta("module", "ai", "ai"), Functions: fns}},
		Conditions: conditions,
		Actions:    actions,
	})
}
