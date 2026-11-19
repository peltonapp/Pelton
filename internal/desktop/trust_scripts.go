package desktop

import (
	"context"

	"github.com/peltonapp/Pelton/internal/scripting"
	"github.com/peltonapp/Pelton/internal/storage"
)

// What scripts see of the sender passport and the privacy shield: conditions
// on a message's sender and trackers, and unsubscribing, which asks first.

const (
	permTrustRead        = "trust.read"
	permTrustUnsubscribe = "trust.unsubscribe"
)

// trustScriptHost is what the scripts reach the passport and the shield
// through.
type trustScriptHost interface {
	Passport(ctx context.Context, id int64) (map[string]any, error)
	Shield(ctx context.Context, id int64) (map[string]any, error)
	Unsubscribe(ctx context.Context, id int64) error
}

func trustScriptEntries(h trustScriptHost) scripting.Entries {
	meta := scripting.EntryMeta
	msg := scripting.RefParam("message", "message", "event.message")
	read := func(name string, get func(context.Context, int64) (map[string]any, error)) scripting.Function {
		f := scripting.NewFunction("trust", name, func(c *scripting.Call) ([]any, error) {
			id, err := c.MessageID(0)
			if err != nil {
				return nil, err
			}
			out, err := get(c.Ctx, id)
			if err != nil {
				return nil, err
			}
			return []any{out}, nil
		}, msg)
		f.Permission = permTrustRead
		return f
	}
	unsubscribe := scripting.NewFunction("trust", "unsubscribe", func(c *scripting.Call) ([]any, error) {
		id, err := c.MessageID(0)
		if err != nil {
			return nil, err
		}
		return nil, h.Unsubscribe(c.Ctx, id)
	}, msg)
	unsubscribe.Permission, unsubscribe.Risky, unsubscribe.Effect = permTrustUnsubscribe, true, true

	is := func(id, lua string) scripting.Condition {
		return scripting.Condition{Meta: meta("cond", id, "mail"), Params: []scripting.Param{msg}, Permission: permTrustRead, Lua: lua}
	}
	return scripting.Entries{
		Permissions: []scripting.Permission{
			{Meta: meta("perm", permTrustRead, "mail"), Risk: "low"},
			{Meta: meta("perm", permTrustUnsubscribe, "mail"), Risk: "medium"},
		},
		Modules: []scripting.Module{{Meta: meta("module", "trust", "mail"), Functions: []scripting.Function{
			read("passport", h.Passport), read("shield", h.Shield), unsubscribe,
		}}},
		Conditions: []scripting.Condition{
			is("trust.sender_verified", `trust.passport({{message}}).verified == true`),
			is("trust.first_contact", `trust.passport({{message}}).first == true`),
			is("trust.looks_like_contact", `trust.passport({{message}}).lookalike == true`),
			is("trust.has_trackers", `(trust.shield({{message}}).trackers or 0) > 0`),
			is("trust.still_sending", `trust.shield({{message}}).still_sending == true`),
		},
		Actions: []scripting.Action{{
			Meta: meta("action", "trust.unsubscribe", "mail"), Params: []scripting.Param{msg},
			Permission: permTrustUnsubscribe, Risky: true, Lua: "trust.unsubscribe({{message}})",
		}},
	}
}

// registerScriptTrust adds the passport and shield entries to the scripts'
// registry.
func (a *App) registerScriptTrust(r *scripting.Registry) error {
	return r.Register(trustScriptEntries(scriptTrust{app: a}))
}

type scriptTrust struct {
	app *App
}

// reach applies the script audience to a trust condition or action. Mail in
// a vault has no passport or shield record, so even a vault that admits
// scripts answers as if the message were not there.
func (s scriptTrust) reach(id int64) error {
	if !s.app.scriptSeesMessage(id) || s.app.inMailVaultMessage(id) {
		return storage.ErrMessageNotFound
	}
	return nil
}

func (s scriptTrust) Passport(_ context.Context, id int64) (map[string]any, error) {
	if err := s.reach(id); err != nil {
		return nil, err
	}
	p, err := s.app.GetSenderPassport(id)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"state": p.State, "verified": p.State == passportVerified, "first": p.First && p.Mark != storage.SenderKnown,
		"lookalike": p.Lookalike != nil, "domain": p.Domain,
	}, nil
}

func (s scriptTrust) Shield(_ context.Context, id int64) (map[string]any, error) {
	if err := s.reach(id); err != nil {
		return nil, err
	}
	m, err := s.app.listedMessage(id)
	if err != nil {
		return nil, err
	}
	trust, err := s.app.store.GetMessageTrust(s.app.ctx, id)
	if err != nil {
		return nil, err
	}
	dto := buildShield(*m, trust, false, true)
	still := false
	if u, ok, err := s.app.store.UnsubscribeFor(s.app.ctx, bareAddress(m.FromAddress), m.ListID); err == nil && ok {
		still = unsubState(u).StillSending
	}
	return map[string]any{"trackers": dto.Trackers, "links": dto.Links, "still_sending": still}, nil
}

func (s scriptTrust) Unsubscribe(_ context.Context, id int64) error {
	if err := s.reach(id); err != nil {
		return err
	}
	return s.app.Unsubscribe(id)
}
