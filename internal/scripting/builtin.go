package scripting

import (
	"fmt"
	"slices"
	"time"
)

// The permissions this package registers. Other packages register their own
// through Registry.RegisterPermission.
const (
	PermMailRead    = "mail.read"
	PermMailModify  = "mail.modify"
	PermMailDelete  = "mail.delete"
	PermMailCompose = "mail.compose"
	PermMailSend    = "mail.send"
	PermNotify      = "notify"
)

// The triggers this package registers.
const (
	TriggerManual       = "manual"
	TriggerAppStart     = "app.start"
	TriggerEvery        = "schedule.every"
	TriggerDaily        = "schedule.daily"
	TriggerWeekdays     = "schedule.weekdays"
	TriggerMailArrived  = "mail.arrived"
	TriggerBeforeSend   = "mail.before_send"
	TriggerAfterSend    = "mail.after_send"
	TriggerMailMoved    = "mail.moved"
	TriggerMailFlagged  = "mail.flagged"
	TriggerMailReadMark = "mail.read_changed"
)

var (
	meta        = EntryMeta
	param       = NewParam
	required    = Required
	withDefault = WithDefault
	field       = NewField
	options     = NewOptions
)

func fn(name string, h Handler, params ...Param) Function {
	return Function{Name: name, Handler: h, Params: params}
}

// RegisterBuiltins registers everything this package provides, through the
// same methods any other package uses.
func RegisterBuiltins(r *Registry) error {
	steps := []func(*Registry) error{
		registerPermissions,
		registerValueTypes,
		registerCoreTriggers,
		registerUtilModules,
		registerUtilActions,
		registerMail,
		registerInteraction,
		registerMailPlacements,
	}
	for _, step := range steps {
		if err := step(r); err != nil {
			return err
		}
	}
	return nil
}

func registerPermissions(r *Registry) error {
	perms := []Permission{
		{Meta: meta("perm", PermMailRead, "mail"), Risk: "low"},
		{Meta: meta("perm", PermMailModify, "mail"), Risk: "medium"},
		{Meta: meta("perm", PermMailDelete, "mail"), Risk: "high"},
		{Meta: meta("perm", PermMailCompose, "mail"), Risk: "low"},
		{Meta: meta("perm", PermMailSend, "mail"), Risk: "high"},
		{Meta: meta("perm", PermNotify, "app"), Risk: "low"},
	}
	for _, p := range perms {
		if err := r.RegisterPermission(p); err != nil {
			return err
		}
	}
	return nil
}

func registerValueTypes(r *Registry) error {
	message := ValueType{
		Meta: meta("type", "message", "mail"),
		Fields: []Field{
			field("id", "number"), field("account_id", "account"), field("account", "address"),
			field("folder", "folder"), field("folder_role", "string"), field("subject", "string"),
			field("from", "address"), field("from_name", "string"), field("to", "string"),
			field("cc", "string"), field("reply_to", "address"), field("date", "number"),
			field("size", "number"), field("has_attachments", "bool"), field("read", "bool"),
			field("flagged", "bool"), field("pinned", "bool"), field("color", "color"),
			field("headers", "table"), field("body", "text"),
		},
		Lazy: map[string]string{"body": "mail.body"},
		Conditions: []FieldCondition{
			{Field: "from", Condition: "mail.from", Param: "value", LabelKey: "scripts.blocks.rule.from"},
			{Field: "subject", Condition: "mail.subject", Param: "value", LabelKey: "scripts.blocks.rule.subject"},
			{Field: "headers.list-id", Condition: "mail.header", Param: "value", Args: map[string]any{"name": "List-Id"}, LabelKey: "scripts.blocks.rule.list"},
			{Field: "to", Condition: "mail.to", Param: "value", Split: ",", LabelKey: "scripts.blocks.rule.to"},
		},
	}
	addresses := func(name string) Field {
		f := field(name, "list")
		f.Of = string(TypeAddress)
		return f
	}
	draft := ValueType{
		Meta: meta("type", "draft", "mail"),
		Fields: []Field{
			field("account_id", "account"), addresses("to"), addresses("cc"),
			addresses("bcc"), field("subject", "string"), field("body", "text"),
		},
	}
	for _, v := range []ValueType{message, draft} {
		if err := r.RegisterValueType(v); err != nil {
			return err
		}
	}
	return nil
}

var weekdayNames = []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

// clockToday returns today's occurrence of the "HH:MM" in params["at"].
func clockToday(params map[string]any, now time.Time) (time.Time, bool) {
	at, _ := params["at"].(string)
	h, m, err := ParseClock(at)
	if err != nil {
		return time.Time{}, false
	}
	return time.Date(now.Year(), now.Month(), now.Day(), h, m, 0, 0, now.Location()), true
}

// dueAt reports whether a daily time came due: it is past today's time, the
// trigger did not fire since, and it was set up before then, so enabling a
// script at noon does not fire its morning schedule straight away.
func dueAt(params map[string]any, last, since, now time.Time) bool {
	at, ok := clockToday(params, now)
	if !ok || now.Before(at) {
		return false
	}
	return last.Before(at) && since.Before(at)
}

func registerCoreTriggers(r *Registry) error {
	scope := withDefault(param("scope", TypeEnum), "none")
	scope.Options = options("none", "message")
	minutes := required(withDefault(param("minutes", TypeNumber), float64(60)))
	at := required(withDefault(param("at", TypeTime), "08:00"))
	days := withDefault(param("days", TypeList), []any{"mon", "tue", "wed", "thu", "fri"})
	days.Of = TypeEnum
	days.Options = options(weekdayNames...)

	triggers := []Trigger{
		{
			Meta:    meta("trigger", TriggerManual, "manual"),
			Params:  []Param{scope},
			Event:   []Field{field("message", "message")},
			Prepare: prepareMessage,
		},
		{Meta: meta("trigger", TriggerAppStart, "app")},
		{
			Meta:   meta("trigger", TriggerEvery, "schedule"),
			Params: []Param{minutes},
			Due: func(params map[string]any, last, since, now time.Time) bool {
				n, _ := toFloat(params["minutes"])
				if n < 1 {
					n = 1
				}
				base := last
				if base.IsZero() {
					base = since
				}
				return now.Sub(base) >= time.Duration(n*float64(time.Minute))
			},
		},
		{Meta: meta("trigger", TriggerDaily, "schedule"), Params: []Param{at}, Due: dueAt},
		{
			Meta:   meta("trigger", TriggerWeekdays, "schedule"),
			Params: []Param{at, days},
			Due: func(params map[string]any, last, since, now time.Time) bool {
				list, _ := params["days"].([]any)
				if !slices.Contains(list, any(weekdayNames[now.Weekday()])) {
					return false
				}
				return dueAt(params, last, since, now)
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

// prepareMessage loads the message behind a "message_id" into "message",
// unless the caller sent the message along already.
func prepareMessage(c *PrepareContext, data map[string]any) (map[string]any, error) {
	out := map[string]any{}
	for k, v := range data {
		out[k] = v
	}
	if _, ok := out["message"]; ok {
		return out, nil
	}
	id, ok := out["message_id"].(int64)
	if !ok {
		return out, nil
	}
	if c.Hosts == nil || c.Hosts.Mail == nil {
		return nil, ErrUnavailable
	}
	m, err := c.Hosts.Mail.Message(c.Ctx, id)
	if err != nil {
		return nil, fmt.Errorf("scripting: load message %d: %w", id, err)
	}
	out["message"] = MessageObject(m)
	delete(out, "message_id")
	return out, nil
}

func registerUtilActions(r *Registry) error {
	text := required(param("text", TypeText))
	level := withDefault(param("level", TypeEnum), "info")
	level.Options = options("debug", "info", "warn", "error")
	toastLevel := withDefault(param("level", TypeEnum), "info")
	toastLevel.Options = options("info", "success", "warning", "error")
	title := required(param("title", TypeString))
	key := required(param("key", TypeString))
	value := param("value", TypeString)

	actions := []Action{
		{Meta: meta("action", "log.write", "utility"), Params: []Param{text, level}, Lua: "log[{{level}}]({{text}})"},
		{Meta: meta("action", "notify.toast", "utility"), Params: []Param{text, toastLevel}, Lua: "notify.toast({{text}}, {{level}})"},
		{Meta: meta("action", "notify.system", "utility"), Params: []Param{title, param("body", TypeText)}, Permission: PermNotify, Lua: "notify.system({{title}}, {{body}})"},
		{Meta: meta("action", "store.set", "utility"), Params: []Param{key, value}, Lua: "store.set({{key}}, {{value}})"},
	}
	for _, a := range actions {
		if err := r.RegisterAction(a); err != nil {
			return err
		}
	}
	return nil
}
