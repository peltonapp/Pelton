package spaces

import (
	"context"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/scripting"
)

// The contact permissions.
const (
	PermContactsRead  = "contacts.read"
	PermContactsWrite = "contacts.write"
)

// TriggerContactAdded fires when a contact is added.
const TriggerContactAdded = "contacts.added"

// Contact is a contact as scripts see it.
type Contact struct {
	ID           int64
	BookID       int64
	Book         string
	Name         string
	Organization string
	Title        string
	Note         string
	Emails       []string
	Phones       []string
}

// ContactInput is a new contact. BookID 0 picks the first address book that
// can be written.
type ContactInput struct {
	BookID       int64
	Name         string
	Email        string
	Organization string
	Note         string
}

// ContactsHost is the address book a script reads and adds to.
type ContactsHost interface {
	// Contacts lists contacts whose name, organization or address contains
	// query, at most limit.
	Contacts(ctx context.Context, query string, limit int) ([]Contact, error)
	Contact(ctx context.Context, id int64) (Contact, error)
	// FindByEmail returns the contact with this address, false when none.
	FindByEmail(ctx context.Context, email string) (Contact, bool, error)
	CreateContact(ctx context.Context, in ContactInput) (Contact, error)
}

// ContactObject turns a contact into the value scripts get.
func ContactObject(c Contact) scripting.Object {
	email := ""
	if len(c.Emails) > 0 {
		email = c.Emails[0]
	}
	return scripting.Object{Type: "contact", Fields: map[string]any{
		"id": c.ID, "book_id": c.BookID, "book": c.Book, "name": c.Name, "organization": c.Organization,
		"title": c.Title, "note": c.Note, "email": email, "emails": texts(c.Emails), "phones": texts(c.Phones),
	}}
}

func sampleContact(time.Time) map[string]any {
	return ContactObject(Contact{Book: "Contacts", Name: "Example Billing", Organization: "Example", Emails: []string{"billing@example.com"}}).Fields
}

// cleanEmail reads an address, with or without a display name.
func cleanEmail(c *scripting.Call, s string) (string, string, error) {
	a, err := mail.ParseAddress(strings.TrimSpace(s))
	if err != nil {
		return "", "", fmt.Errorf("%s: %q is not an email address", c.Function, s)
	}
	return strings.ToLower(a.Address), a.Name, nil
}

func contactsEntries(h ContactsHost) scripting.Entries {
	host := func() (ContactsHost, error) {
		if h == nil {
			return nil, unavailable("contacts")
		}
		return h, nil
	}
	contactField := field("contact", "contact")
	load := func(c *scripting.PrepareContext, id int64) (scripting.Object, error) {
		hh, err := host()
		if err != nil {
			return scripting.Object{}, err
		}
		ct, err := hh.Contact(c.Ctx, id)
		if err != nil {
			return scripting.Object{}, fmt.Errorf("spaces: load contact %d: %w", id, err)
		}
		return ContactObject(ct), nil
	}
	add := func(c *scripting.Call, hh ContactsHost, in ContactInput) ([]any, error) {
		email, name, err := cleanEmail(c, in.Email)
		if err != nil {
			return nil, err
		}
		in.Email = email
		if in.Name == "" {
			in.Name = name
		}
		if found, ok, err := hh.FindByEmail(c.Ctx, email); err != nil {
			return nil, err
		} else if ok {
			return []any{ContactObject(found)}, nil
		}
		ct, err := hh.CreateContact(c.Ctx, in)
		if err != nil {
			return nil, err
		}
		return []any{ContactObject(ct)}, nil
	}
	query := required(param("query", scripting.TypeString))
	email := required(param("email", scripting.TypeAddress))
	msg := param("message", scripting.TypeRef)
	msg.ValueType = "message"

	fns := []scripting.Function{
		fnDef("contacts", "search", PermContactsRead, "read", func(c *scripting.Call) ([]any, error) {
			hh, err := host()
			if err != nil {
				return nil, err
			}
			q, err := c.String(0)
			if err != nil {
				return nil, err
			}
			n, err := limitArg(c, 1)
			if err != nil {
				return nil, err
			}
			out, err := hh.Contacts(c.Ctx, q, n)
			if err != nil {
				return nil, err
			}
			return []any{list(out, ContactObject)}, nil
		}, []scripting.Field{listOf("contacts", "contact")}, query, param("limit", scripting.TypeNumber)),
		fnDef("contacts", "get", PermContactsRead, "read", func(c *scripting.Call) ([]any, error) {
			hh, err := host()
			if err != nil {
				return nil, err
			}
			id, err := idOf(c, 0, "a contact")
			if err != nil {
				return nil, err
			}
			ct, err := hh.Contact(c.Ctx, id)
			if err != nil {
				return []any{nil, err.Error()}, nil
			}
			return []any{ContactObject(ct)}, nil
		}, []scripting.Field{contactField}, param("id", scripting.TypeNumber)),
		fnDef("contacts", "find", PermContactsRead, "read", func(c *scripting.Call) ([]any, error) {
			hh, err := host()
			if err != nil {
				return nil, err
			}
			s, err := c.String(0)
			if err != nil {
				return nil, err
			}
			addr, _, err := cleanEmail(c, s)
			if err != nil {
				return []any{nil}, nil
			}
			ct, ok, err := hh.FindByEmail(c.Ctx, addr)
			if err != nil || !ok {
				return []any{nil}, err
			}
			return []any{ContactObject(ct)}, nil
		}, []scripting.Field{contactField}, email),
		withDry(fnDef("contacts", "add", PermContactsWrite, "effect", func(c *scripting.Call) ([]any, error) {
			hh, err := host()
			if err != nil {
				return nil, err
			}
			t, err := tableArg(c, 0, "an email")
			if err != nil {
				return nil, err
			}
			return add(c, hh, ContactInput{
				BookID: numberOf(t, "book_id"), Name: strings.TrimSpace(textOf(t, "name")), Email: textOf(t, "email"),
				Organization: textOf(t, "organization"), Note: textOf(t, "note"),
			})
		}, []scripting.Field{contactField}, param("contact", scripting.TypeRef))),
		withDry(fnDef("contacts", "add_sender", PermContactsWrite, "effect", func(c *scripting.Call) ([]any, error) {
			hh, err := host()
			if err != nil {
				return nil, err
			}
			m, err := tableArg(c, 0, "a message")
			if err != nil {
				return nil, err
			}
			from := textOf(m, "from")
			if from == "" {
				return nil, fmt.Errorf("%s: the message has no sender", c.Function)
			}
			return add(c, hh, ContactInput{Name: textOf(m, "from_name"), Email: from})
		}, []scripting.Field{contactField}, msg)),
	}

	msgRef := scripting.RefParam("message", "message", "event.message")
	contactRef := scripting.RefParam("contact", "contact", "event.contact")
	conds := []scripting.Condition{
		{
			Meta: meta("cond", "contacts.known_sender", "contacts"), Params: []scripting.Param{msgRef},
			Permission: PermContactsRead, Lua: "contacts.find({{message}}.from) ~= nil",
		},
		scripting.TextCondition(meta("cond", "contacts.name", "contacts"), "{{contact}}.name", contactRef),
		scripting.TextCondition(meta("cond", "contacts.organization", "contacts"), "{{contact}}.organization", contactRef),
	}
	act := func(id, lua string, params ...scripting.Param) scripting.Action {
		return scripting.Action{Meta: meta("action", id, "contacts"), Params: params, Permission: PermContactsWrite,
			Outputs: []scripting.Field{field("id", "number")}, Lua: lua}
	}
	actions := []scripting.Action{
		act("contacts.add_sender", "contacts.add_sender({{message}})", msgRef),
		act("contacts.add", "contacts.add({name = {{name}}, email = {{email}}})", param("name", scripting.TypeString), email),
	}

	return scripting.Entries{
		Permissions: []scripting.Permission{
			{Meta: meta("perm", PermContactsRead, "contacts"), Risk: "low"},
			{Meta: meta("perm", PermContactsWrite, "contacts"), Risk: "medium"},
		},
		ValueTypes: []scripting.ValueType{{Meta: meta("type", "contact", "contacts"), Sample: sampleContact, Fields: fields(
			"id:number", "book_id:number", "book:string", "name:string", "organization:string", "title:string",
			"note:text", "email:address", "emails:list:address", "phones:list:string")}},
		Triggers: []scripting.Trigger{{
			Meta: meta("trigger", TriggerContactAdded, "contacts"), Event: []scripting.Field{field("contact", "contact")},
			Permission: PermContactsRead,
			Prepare:    prepareObject("contact", "contact_id", load),
		}},
		Modules:    []scripting.Module{{Meta: meta("module", "contacts", "contacts"), Functions: fns}},
		Conditions: conds,
		Actions:    actions,
		InputKinds: []scripting.InputKind{{
			Meta: meta("input", "contact", "contacts"), Control: "contact", Value: "contact",
			Check: func(c *scripting.Call, answer any, _ map[string]any) (any, error) {
				id, ok := answer.(float64)
				if !ok {
					return nil, fmt.Errorf("ui: the answer is not a contact")
				}
				if err := c.Require(PermContactsRead); err != nil {
					return nil, err
				}
				return load(&scripting.PrepareContext{Ctx: c.Ctx, Hosts: c.Hosts}, int64(id))
			},
		}},
		Placements: []scripting.Placement{menu("contacts.contact", "contacts", "contact", PermContactsRead, intLoad(load))},
	}
}
