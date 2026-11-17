package spaces

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/scripting"
)

// The chat permissions.
const (
	PermChatRead = "chat.read"
	PermChatSend = "chat.send"
)

// TriggerChatMessage fires for each chat message someone else sends.
const TriggerChatMessage = "chat.message_received"

// ActionChatSend is the block action that sends a chat message.
const ActionChatSend = "chat.send"

// ChatOff is why the chat entries are unavailable while chat is off.
const ChatOff = "chat is switched off in the settings"

// maxChatText caps a message a script sends.
const maxChatText = 16 << 10

// ChatMessage is a chat message as scripts see it.
type ChatMessage struct {
	RoomID  string
	Room    string
	EventID string
	Sender  string
	Body    string
	Direct  bool
	Mention bool
}

// ChatRoom is a room the user is in.
type ChatRoom struct {
	ID     string
	Name   string
	Direct bool
}

// ChatHost is the chat a script reads and sends to.
type ChatHost interface {
	// Enabled reports whether chat is switched on.
	Enabled() bool
	Rooms(ctx context.Context) ([]ChatRoom, error)
	// Send sends text to a room, named by its id or its name, and returns
	// the message's id.
	Send(ctx context.Context, room, text string) (string, error)
}

// ChatMessageObject turns a chat message into the value scripts get.
func ChatMessageObject(m ChatMessage) scripting.Object {
	return scripting.Object{Type: "chat_message", Fields: map[string]any{
		"room_id": m.RoomID, "room": m.Room, "id": m.EventID, "sender": m.Sender,
		"body": m.Body, "direct": m.Direct, "mention": m.Mention,
	}}
}

func sampleChatMessage(time.Time) map[string]any {
	return ChatMessageObject(ChatMessage{RoomID: "!room:example.org", Room: "Team", Sender: "@lead:example.org", Body: "Can you send the report?", Mention: true}).Fields
}

// SetChatAvailable marks the chat trigger and send action available or not,
// for when the user switches chat on or off.
func SetChatAvailable(r *scripting.Registry, on bool) {
	reason := ""
	if !on {
		reason = ChatOff
	}
	r.SetMissing(TriggerChatMessage, reason)
	r.SetMissing(ActionChatSend, reason)
}

func chatEntries(h ChatHost) scripting.Entries {
	host := func() (ChatHost, error) {
		if h == nil {
			return nil, unavailable("chat")
		}
		if !h.Enabled() {
			return nil, fmt.Errorf("%w: %s", scripting.ErrUnavailable, ChatOff)
		}
		return h, nil
	}
	room := required(param("room", scripting.TypeString))
	text := required(param("text", scripting.TypeText))

	rooms := fnDef("chat", "rooms", PermChatRead, "read", func(c *scripting.Call) ([]any, error) {
		hh, err := host()
		if err != nil {
			return nil, err
		}
		out, err := hh.Rooms(c.Ctx)
		if err != nil {
			return nil, err
		}
		return []any{list(out, func(r ChatRoom) scripting.Object {
			return scripting.Object{Type: "chat_room", Fields: map[string]any{"id": r.ID, "name": r.Name, "direct": r.Direct}}
		})}, nil
	}, []scripting.Field{field("rooms", "list")})
	send := fnDef("chat", "send", PermChatSend, "effect", func(c *scripting.Call) ([]any, error) {
		hh, err := host()
		if err != nil {
			return nil, err
		}
		r, err := c.String(0)
		if err != nil {
			return nil, err
		}
		body, err := c.String(1)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(body) == "" || len(body) > maxChatText {
			return nil, fmt.Errorf("%s: a message is 1 to %d bytes", c.Function, maxChatText)
		}
		id, err := hh.Send(c.Ctx, strings.TrimSpace(r), body)
		if err != nil {
			return nil, err
		}
		return []any{map[string]any{"id": id}}, nil
	}, []scripting.Field{field("id", "string")}, room, text)
	send.Risky = true
	send.Dry = func(*scripting.Call) ([]any, error) { return []any{map[string]any{"id": ""}}, nil }

	chatRef := scripting.RefParam("chat", "chat_message", "event.chat")
	only := required(withDef(param("only", scripting.TypeEnum), "any"))
	only.Options = scripting.NewOptions("any", "direct", "mention")
	roomName := param("room", scripting.TypeString)

	return scripting.Entries{
		Permissions: []scripting.Permission{
			{Meta: meta("perm", PermChatRead, "chat"), Risk: "medium"},
			{Meta: meta("perm", PermChatSend, "chat"), Risk: "high"},
		},
		ValueTypes: []scripting.ValueType{
			{Meta: meta("type", "chat_message", "chat"), Sample: sampleChatMessage, Fields: fields(
				"room_id:string", "room:string", "id:string", "sender:string", "body:text", "direct:bool", "mention:bool")},
			{Meta: meta("type", "chat_room", "chat"), Fields: fields("id:string", "name:string", "direct:bool")},
		},
		Triggers: []scripting.Trigger{{
			Meta: meta("trigger", TriggerChatMessage, "chat"), Params: []scripting.Param{roomName, only},
			Event: []scripting.Field{field("chat", "chat_message")}, Permission: PermChatRead,
			Match: func(params, event map[string]any) bool {
				m := objectFields(event, "chat")
				if m == nil {
					return false
				}
				name, _ := m["room"].(string)
				id, _ := m["room_id"].(string)
				if !matchText(params, "room", name) && !matchText(params, "room", id) {
					return false
				}
				switch params["only"] {
				case "direct":
					return m["direct"] == true
				case "mention":
					return m["mention"] == true
				}
				return true
			},
		}},
		Modules: []scripting.Module{{Meta: meta("module", "chat", "chat"), Functions: []scripting.Function{rooms, send}}},
		Conditions: []scripting.Condition{
			scripting.TextCondition(meta("cond", "chat.body", "chat"), "{{chat}}.body", chatRef),
			scripting.TextCondition(meta("cond", "chat.sender", "chat"), "{{chat}}.sender", chatRef),
		},
		Actions: []scripting.Action{{
			Meta: meta("action", ActionChatSend, "chat"), Params: []scripting.Param{room, text},
			Permission: PermChatSend, Risky: true, Lua: "chat.send({{room}}, {{text}})",
		}},
	}
}
