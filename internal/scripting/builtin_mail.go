package scripting

import (
	"fmt"
	"strings"
	"time"
)

func registerMail(r *Registry) error {
	steps := []func(*Registry) error{registerMailTriggers, registerMailModules, registerMailConditions, registerMailActions}
	for _, step := range steps {
		if err := step(r); err != nil {
			return err
		}
	}
	return nil
}

// folderMatches reports whether a message's folder is the one a script named,
// by path or by role, ignoring case. An empty name matches any folder.
func folderMatches(msg map[string]any, name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return true
	}
	path, _ := msg["folder"].(string)
	role, _ := msg["folder_role"].(string)
	return strings.EqualFold(path, name) || (role != "" && strings.EqualFold(role, name))
}

func eventMessage(event map[string]any) map[string]any {
	switch m := event["message"].(type) {
	case Object:
		return m.Fields
	case map[string]any:
		return m
	}
	return nil
}

func matchAccount(params, msg map[string]any) bool {
	want, ok := toFloat(params["account"])
	if !ok || want == 0 {
		return true
	}
	got, _ := toFloat(msg["account_id"])
	return got == want
}

// matchState checks a trigger's "state" param (any, on or off) against the
// event's boolean field.
func matchState(params, event map[string]any, key string) bool {
	state, _ := params["state"].(string)
	on, _ := event[key].(bool)
	switch state {
	case "on":
		return on
	case "off":
		return !on
	}
	return true
}

func registerMailTriggers(r *Registry) error {
	account := param("account", TypeAccount)
	folder := withDefault(param("folder", TypeFolder), "inbox")
	anyFolder := param("folder", TypeFolder)
	state := withDefault(param("state", TypeEnum), "any")
	state.Options = options("any", "on", "off")
	message := field("message", "message")

	triggers := []Trigger{
		{
			Meta:       meta("trigger", TriggerMailArrived, "mail"),
			Params:     []Param{account, folder},
			Event:      []Field{message},
			Permission: PermMailRead,
			Prepare:    prepareMessage,
			Match: func(params, event map[string]any) bool {
				msg := eventMessage(event)
				f, _ := params["folder"].(string)
				return msg != nil && matchAccount(params, msg) && folderMatches(msg, f)
			},
		},
		{
			Meta:       meta("trigger", TriggerBeforeSend, "mail"),
			Params:     []Param{account},
			Event:      []Field{field("draft", "draft")},
			Permission: PermMailSend,
			Mutable:    true,
			Match: func(params, event map[string]any) bool {
				d, _ := event["draft"].(map[string]any)
				return d != nil && matchAccount(params, d)
			},
		},
		{
			Meta:       meta("trigger", TriggerAfterSend, "mail"),
			Params:     []Param{account},
			Event:      []Field{field("draft", "draft")},
			Permission: PermMailRead,
			Match: func(params, event map[string]any) bool {
				d, _ := event["draft"].(map[string]any)
				return d != nil && matchAccount(params, d)
			},
		},
		{
			Meta:       meta("trigger", TriggerMailMoved, "mail"),
			Params:     []Param{account, anyFolder},
			Event:      []Field{message, field("to_folder", "folder")},
			Permission: PermMailRead,
			Prepare:    prepareMessage,
			Match: func(params, event map[string]any) bool {
				msg := eventMessage(event)
				f, _ := params["folder"].(string)
				to, _ := event["to_folder"].(string)
				role, _ := event["to_role"].(string)
				return msg != nil && matchAccount(params, msg) &&
					(f == "" || strings.EqualFold(f, to) || (role != "" && strings.EqualFold(f, role)))
			},
		},
		{
			Meta:       meta("trigger", TriggerMailFlagged, "mail"),
			Params:     []Param{account, state},
			Event:      []Field{message, field("flagged", "bool")},
			Permission: PermMailRead,
			Prepare:    prepareMessage,
			Match: func(params, event map[string]any) bool {
				msg := eventMessage(event)
				return msg != nil && matchAccount(params, msg) && matchState(params, event, "flagged")
			},
		},
		{
			Meta:       meta("trigger", TriggerMailReadMark, "mail"),
			Params:     []Param{account, state},
			Event:      []Field{message, field("read", "bool")},
			Permission: PermMailRead,
			Prepare:    prepareMessage,
			Match: func(params, event map[string]any) bool {
				msg := eventMessage(event)
				return msg != nil && matchAccount(params, msg) && matchState(params, event, "read")
			},
		},
	}
	for _, t := range triggers {
		if err := r.RegisterTrigger(t); err != nil {
			return err
		}
	}
	return nil
}

func messageParam() Param {
	p := param("message", TypeRef)
	p.ValueType = "message"
	p.DefaultRef = "event.message"
	return p
}

func registerMailConditions(r *Registry) error {
	msg := messageParam()
	op := required(withDefault(param("op", TypeEnum), OpContains))
	op.Options = textOps()
	value := required(param("value", TypeString))
	cmp := required(withDefault(param("cmp", TypeEnum), "gt"))
	cmp.Options = []Option{
		{Value: "gt", LabelKey: "scripts.option.gt", Lua: ">"},
		{Value: "lt", LabelKey: "scripts.option.lt", Lua: "<"},
	}
	age := required(withDefault(param("age", TypeEnum), "older"))
	age.Options = []Option{
		{Value: "older", LabelKey: "scripts.option.older", Lua: ">"},
		{Value: "newer", LabelKey: "scripts.option.newer", Lua: "<"},
	}
	state := required(withDefault(param("state", TypeEnum), "on"))
	state.Options = options("on", "off")
	text := func(id, field, subject string) Condition {
		return Condition{
			Meta:   meta("cond", id, "mail"),
			Params: []Param{msg, op, value},
			Lua:    "text.test(" + subject + ", {{op}}, {{value}})",
			Field:  field,
		}
	}
	conds := []Condition{
		text("mail.from", "from", "{ {{message}}.from, {{message}}.from_name }"),
		text("mail.to", "to", "text.split(({{message}}.to or \"\") .. \",\" .. ({{message}}.cc or \"\"), \",\")"),
		text("mail.subject", "subject", "{{message}}.subject"),
		{
			Meta:       meta("cond", "mail.body", "mail"),
			Params:     []Param{msg, op, value},
			Permission: PermMailRead,
			Lua:        "text.test(mail.body({{message}}), {{op}}, {{value}})",
			Field:      "body",
		},
		{
			Meta:   meta("cond", "mail.header", "mail"),
			Params: []Param{msg, required(param("name", TypeString)), op, value},
			Lua:    "text.test(mail.header({{message}}, {{name}}), {{op}}, {{value}})",
			Field:  "headers",
		},
		{Meta: meta("cond", "mail.has_attachment", "mail"), Params: []Param{msg}, Lua: "{{message}}.has_attachments == true"},
		{
			Meta:   meta("cond", "mail.size", "mail"),
			Params: []Param{msg, cmp, required(withDefault(param("kb", TypeNumber), float64(1024)))},
			Lua:    "({{message}}.size or 0) {{cmp}} {{kb}} * 1024",
		},
		{
			Meta:   meta("cond", "mail.account", "mail"),
			Params: []Param{msg, required(param("account", TypeAccount))},
			Lua:    "{{message}}.account_id == {{account}}",
		},
		{
			Meta:   meta("cond", "mail.folder", "mail"),
			Params: []Param{msg, required(param("folder", TypeFolder))},
			Lua:    "mail.in_folder({{message}}, {{folder}})",
		},
		{
			Meta:   meta("cond", "mail.age", "mail"),
			Params: []Param{msg, age, required(withDefault(param("duration", TypeDuration), "7d"))},
			Lua:    "time.now() - ({{message}}.date or 0) {{age}} {{duration}}",
		},
		{Meta: meta("cond", "mail.is_read", "mail"), Params: []Param{msg, state}, Lua: "({{message}}.read == true) == ({{state}} == \"on\")"},
		{Meta: meta("cond", "mail.is_flagged", "mail"), Params: []Param{msg, state}, Lua: "({{message}}.flagged == true) == ({{state}} == \"on\")"},
		TextCondition(meta("cond", "text.value", "utility"), "{{text}}", required(RefParam("text", "", ""))),
	}
	for _, c := range conds {
		if err := r.RegisterCondition(c); err != nil {
			return err
		}
	}
	return nil
}

func registerMailActions(r *Registry) error {
	msg := messageParam()
	folder := required(param("folder", TypeFolder))
	on := withDefault(param("on", TypeBool), true)
	color := required(withDefault(param("color", TypeColor), float64(1)))
	duration := required(withDefault(param("duration", TypeDuration), "1d"))
	to := required(param("to", TypeList))
	to.Of = TypeAddress
	cc := param("cc", TypeList)
	cc.Of = TypeAddress
	subject := param("subject", TypeString)
	body := param("body", TypeText)
	all := withDefault(param("all", TypeBool), false)
	reason := param("reason", TypeString)
	draftField := required(withDefault(param("field", TypeEnum), "subject"))
	draftField.Options = options("subject", "body")
	recipient := required(withDefault(param("field", TypeEnum), "to"))
	recipient.Options = options("to", "cc", "bcc")
	address := required(param("address", TypeAddress))
	idOut := []Field{field("id", "number")}

	act := func(id, perm, lua string, params ...Param) Action {
		return Action{Meta: meta("action", id, "mail"), Params: params, Permission: perm, Lua: lua}
	}
	risky := func(a Action) Action {
		a.Risky = true
		return a
	}
	outputs := func(a Action) Action {
		a.Outputs = idOut
		return a
	}
	beforeSend := func(a Action) Action {
		a.Triggers = []string{TriggerBeforeSend}
		return a
	}
	listFolder := required(withDefault(param("folder", TypeFolder), "inbox"))
	limit := required(withDefault(param("limit", TypeNumber), float64(defaultListLimit)))
	unread := withDefault(param("unread", TypeBool), false)
	messages := field("messages", "list")
	messages.Of = "message"
	list := Action{
		Meta:       meta("action", "mail.list", "mail"),
		Params:     []Param{listFolder, limit, unread},
		Outputs:    []Field{messages},
		Permission: PermMailRead,
		Lua:        "{ messages = mail.list({{folder}}, {limit = {{limit}}, unread = {{unread}}}) }",
	}
	actions := []Action{
		list,
		act("mail.move", PermMailModify, "mail.move({{message}}, {{folder}})", msg, folder),
		act("mail.copy", PermMailModify, "mail.copy({{message}}, {{folder}})", msg, folder),
		act("mail.archive", PermMailModify, "mail.archive({{message}})", msg),
		act("mail.delete", PermMailDelete, "mail.delete({{message}})", msg),
		act("mail.mark_read", PermMailModify, "mail.mark_read({{message}}, {{on}})", msg, on),
		act("mail.flag", PermMailModify, "mail.flag({{message}}, {{on}})", msg, on),
		act("mail.color", PermMailModify, "mail.set_color({{message}}, {{color}})", msg, color),
		act("mail.snooze", PermMailModify, "mail.snooze({{message}}, time.now() + {{duration}})", msg, duration),
		act("mail.pin", PermMailModify, "mail.pin({{message}}, {{on}})", msg, on),
		outputs(act("mail.create_draft", PermMailCompose, "mail.draft({to = {{to}}, cc = {{cc}}, subject = {{subject}}, body = {{body}}})", to, cc, subject, body)),
		outputs(act("mail.reply", PermMailCompose, "mail.reply({{message}}, {body = {{body}}, all = {{all}}})", msg, body, all)),
		outputs(risky(act("mail.reply_send", PermMailSend, "mail.send_reply({{message}}, {body = {{body}}, all = {{all}}})", msg, body, all))),
		outputs(act("mail.forward", PermMailCompose, "mail.forward({{message}}, {to = {{to}}, body = {{body}}})", msg, to, body)),
		outputs(risky(act("mail.forward_send", PermMailSend, "mail.send_forward({{message}}, {to = {{to}}, body = {{body}}})", msg, to, body))),
		outputs(risky(act("mail.send", PermMailSend, "mail.send({to = {{to}}, cc = {{cc}}, subject = {{subject}}, body = {{body}}})", to, cc, subject, body))),
		beforeSend(act("outgoing.cancel", "", "outgoing.cancel({{reason}})", reason)),
		beforeSend(act("outgoing.set", "", "event.draft[{{field}}] = {{value}}", draftField, param("value", TypeText))),
		beforeSend(act("outgoing.add_recipient", "", "table.insert(event.draft[{{field}}], {{address}})", recipient, address)),
	}
	for _, a := range actions {
		if err := r.RegisterAction(a); err != nil {
			return err
		}
	}
	return nil
}

// mailHost returns the mail host or ErrUnavailable.
func mailHost(c *Call) (MailHost, error) {
	if c.Hosts == nil || c.Hosts.Mail == nil {
		return nil, ErrUnavailable
	}
	return c.Hosts.Mail, nil
}

// onMessage builds a handler that acts on the message in argument 0.
func onMessage(do func(c *Call, h MailHost, id int64) error) Handler {
	return func(c *Call) ([]any, error) {
		id, err := c.MessageID(0)
		if err != nil {
			return nil, err
		}
		h, err := mailHost(c)
		if err != nil {
			return nil, err
		}
		return nil, do(c, h, id)
	}
}

// draftArg reads a draft table: to, cc, bcc, subject, body and account_id.
func draftArg(c *Call, i int) (Draft, error) {
	t := c.Table(i)
	if t == nil {
		return Draft{}, fmt.Errorf("%s: argument %d must be a table", c.Function, i+1)
	}
	sub := &Call{Ctx: c.Ctx, Function: c.Function, Args: []any{t["to"], t["cc"], t["bcc"]}}
	var d Draft
	var err error
	if d.To, err = sub.List(0); err != nil {
		return Draft{}, fmt.Errorf("%s: to must be a list of addresses", c.Function)
	}
	if d.Cc, err = sub.List(1); err != nil {
		return Draft{}, fmt.Errorf("%s: cc must be a list of addresses", c.Function)
	}
	if d.Bcc, err = sub.List(2); err != nil {
		return Draft{}, fmt.Errorf("%s: bcc must be a list of addresses", c.Function)
	}
	d.Subject, _ = t["subject"].(string)
	d.Body, _ = t["body"].(string)
	if acc, ok := t["account_id"].(float64); ok {
		d.AccountID = int64(acc)
	}
	return d, nil
}

// compose builds a draft handler. answer reads the message being answered
// or forwarded from argument 0 and the draft table from argument 1.
func compose(send bool, kind string) Handler {
	return func(c *Call) ([]any, error) {
		h, err := mailHost(c)
		if err != nil {
			return nil, err
		}
		var d Draft
		switch kind {
		case "new":
			if d, err = draftArg(c, 0); err != nil {
				return nil, err
			}
			if len(d.To)+len(d.Cc)+len(d.Bcc) == 0 && send {
				return nil, fmt.Errorf("%s: a message needs a recipient", c.Function)
			}
		case "reply", "forward":
			id, err := c.MessageID(0)
			if err != nil {
				return nil, err
			}
			if c.Arg(1) != nil {
				if d, err = draftArg(c, 1); err != nil {
					return nil, err
				}
			}
			if kind == "reply" {
				d.ReplyTo = id
				d.ReplyAll, _ = c.Table(1)["all"].(bool)
			} else {
				d.Forward = id
				if len(d.To) == 0 && send {
					return nil, fmt.Errorf("%s: a forward needs a recipient", c.Function)
				}
			}
		}
		id, err := h.Compose(c.Ctx, d, send)
		if err != nil {
			return nil, err
		}
		return []any{map[string]any{"id": id}}, nil
	}
}

// The number of messages mail.list returns unless told otherwise, and the
// most it returns.
const (
	defaultListLimit = 50
	maxListLimit     = 500
)

// listMail is mail.list: the newest messages of a folder, optionally only
// the unread ones and only one account's. A dry run without a mail host
// lists the sample message.
func listMail(c *Call) ([]any, error) {
	folder, err := c.OptString(0, "inbox")
	if err != nil {
		return nil, err
	}
	opts := c.Table(1)
	limit := defaultListLimit
	if n, ok := opts["limit"].(float64); ok {
		if n < 1 || n > maxListLimit {
			return nil, fmt.Errorf("mail.list: the limit is 1 to %d", maxListLimit)
		}
		limit = int(n)
	}
	account, _ := opts["account"].(float64)
	unread, _ := opts["unread"].(bool)
	var lister MailLister
	if c.Hosts != nil {
		lister, _ = c.Hosts.Mail.(MailLister)
	}
	if lister == nil {
		if c.Dry {
			return []any{[]any{MessageObject(SampleMessage(c.Now()))}}, nil
		}
		return nil, ErrUnavailable
	}
	fetch := limit
	if unread {
		fetch = maxListLimit
	}
	list, err := lister.Recent(c.Ctx, folder, int64(account), fetch)
	if err != nil {
		return nil, fmt.Errorf("mail.list: %w", err)
	}
	out := []any{}
	for _, m := range list {
		if len(out) == limit {
			break
		}
		if unread && m.Read {
			continue
		}
		out = append(out, MessageObject(m))
	}
	return []any{out}, nil
}

func dryDraft(*Call) ([]any, error) {
	return []any{map[string]any{"id": 0}}, nil
}

func registerMailModules(r *Registry) error {
	msg := param("message", TypeRef)
	msg.ValueType = "message"
	folder := param("folder", TypeFolder)
	on := param("on", TypeBool)
	draft := param("draft", TypeRef)
	draft.ValueType = "draft"

	read := func(f Function) Function {
		f.Permission = PermMailRead
		return f
	}
	effect := func(perm string, f Function) Function {
		f.Permission = perm
		f.Effect = true
		return f
	}
	risky := func(f Function) Function {
		f.Risky = true
		return f
	}
	drafted := func(f Function) Function {
		f.Returns = []Field{field("id", "number")}
		f.Dry = dryDraft
		return f
	}

	fns := []Function{
		read(fn("get", func(c *Call) ([]any, error) {
			id, err := c.Int(0)
			if err != nil {
				return nil, err
			}
			if id == 0 && c.Dry {
				return []any{MessageObject(SampleMessage(c.Now()))}, nil
			}
			h, err := mailHost(c)
			if err != nil {
				return nil, err
			}
			m, err := h.Message(c.Ctx, id)
			if err != nil {
				return []any{nil, err.Error()}, nil
			}
			return []any{MessageObject(m)}, nil
		}, param("id", TypeNumber))),
		read(fn("body", func(c *Call) ([]any, error) {
			id, err := c.MessageID(0)
			if err != nil {
				return nil, err
			}
			if id == 0 && c.Dry {
				return []any{sampleBody}, nil
			}
			h, err := mailHost(c)
			if err != nil {
				return nil, err
			}
			body, err := h.Body(c.Ctx, id)
			if err != nil {
				return nil, err
			}
			return []any{body}, nil
		}, msg)),
		read(fn("list", listMail, folder, param("options", TypeRef))),
		fn("header", func(c *Call) ([]any, error) {
			m := c.Table(0)
			name, err := c.String(1)
			if err != nil {
				return nil, err
			}
			headers, _ := m["headers"].(map[string]any)
			v, _ := headers[strings.ToLower(name)].(string)
			return []any{v}, nil
		}, msg, param("name", TypeString)),
		fn("in_folder", func(c *Call) ([]any, error) {
			name, err := c.String(1)
			if err != nil {
				return nil, err
			}
			m := c.Table(0)
			return []any{m != nil && folderMatches(m, name)}, nil
		}, msg, folder),
		effect(PermMailModify, fn("move", onMessage(func(c *Call, h MailHost, id int64) error {
			f, err := c.String(1)
			if err != nil {
				return err
			}
			return h.Move(c.Ctx, id, f)
		}), msg, folder)),
		effect(PermMailModify, fn("copy", onMessage(func(c *Call, h MailHost, id int64) error {
			f, err := c.String(1)
			if err != nil {
				return err
			}
			return h.Copy(c.Ctx, id, f)
		}), msg, folder)),
		effect(PermMailModify, fn("archive", onMessage(func(c *Call, h MailHost, id int64) error {
			return h.Archive(c.Ctx, id)
		}), msg)),
		effect(PermMailDelete, fn("delete", onMessage(func(c *Call, h MailHost, id int64) error {
			return h.Delete(c.Ctx, id)
		}), msg)),
		effect(PermMailModify, fn("mark_read", onMessage(func(c *Call, h MailHost, id int64) error {
			return h.SetRead(c.Ctx, id, c.Bool(1, true))
		}), msg, on)),
		effect(PermMailModify, fn("flag", onMessage(func(c *Call, h MailHost, id int64) error {
			return h.SetFlagged(c.Ctx, id, c.Bool(1, true))
		}), msg, on)),
		effect(PermMailModify, fn("set_color", onMessage(func(c *Call, h MailHost, id int64) error {
			n, err := c.Int(1)
			if err != nil || n < 0 || n > 8 {
				return fmt.Errorf("mail.set_color: the colour is 0 (none) to 8")
			}
			return h.SetColor(c.Ctx, id, int(n))
		}), msg, param("color", TypeColor))),
		effect(PermMailModify, fn("pin", onMessage(func(c *Call, h MailHost, id int64) error {
			return h.SetPinned(c.Ctx, id, c.Bool(1, true))
		}), msg, on)),
		effect(PermMailModify, fn("snooze", onMessage(func(c *Call, h MailHost, id int64) error {
			var until time.Time
			switch v := c.Arg(1).(type) {
			case float64:
				until = time.Unix(int64(v), 0)
			case string:
				secs, err := ParseDuration(v)
				if err != nil {
					return fmt.Errorf("mail.snooze: %w", err)
				}
				until = c.Now().Add(time.Duration(secs * float64(time.Second)))
			default:
				return fmt.Errorf("mail.snooze: give a time or a duration such as \"2h\"")
			}
			if !until.After(c.Now()) {
				return fmt.Errorf("mail.snooze: the time has passed")
			}
			return h.Snooze(c.Ctx, id, until)
		}), msg, param("until", TypeNumber))),
		drafted(effect(PermMailCompose, fn("draft", compose(false, "new"), draft))),
		drafted(risky(effect(PermMailSend, fn("send", compose(true, "new"), draft)))),
		drafted(effect(PermMailCompose, fn("reply", compose(false, "reply"), msg, draft))),
		drafted(risky(effect(PermMailSend, fn("send_reply", compose(true, "reply"), msg, draft)))),
		drafted(effect(PermMailCompose, fn("forward", compose(false, "forward"), msg, draft))),
		drafted(risky(effect(PermMailSend, fn("send_forward", compose(true, "forward"), msg, draft)))),
	}
	for i := range fns {
		fns[i].DescKey = modDesc("mail", fns[i].Name)
	}
	cancel := fn("cancel", func(c *Call) ([]any, error) {
		reason, err := c.OptString(0, "")
		if err != nil {
			return nil, err
		}
		c.Cancel(reason)
		return nil, nil
	}, param("reason", TypeString))
	cancel.DescKey = modDesc("outgoing", "cancel")

	for _, m := range []Module{
		{Meta: meta("module", "mail", "mail"), Functions: fns},
		{Meta: meta("module", "outgoing", "mail"), Functions: []Function{cancel}},
	} {
		if err := r.RegisterModule(m); err != nil {
			return err
		}
	}
	return nil
}
