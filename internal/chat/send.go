package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/format"
	"maunium.net/go/mautrix/id"
)

// Mention is a person named in a message being sent.
type Mention struct {
	UserID string `json:"userId"`
	Name   string `json:"name"`
}

// SendRequest is a text message to send.
type SendRequest struct {
	RoomID string `json:"roomId"`
	Text   string `json:"text"`
	// Markdown renders the text as Markdown into the formatted body.
	Markdown bool `json:"markdown"`
	// Emote sends it as an action ("/me").
	Emote bool `json:"emote"`
	// ReplyTo answers this message.
	ReplyTo string `json:"replyTo"`
	// Edit replaces the text of this earlier message of the user's.
	Edit     string    `json:"edit"`
	Mentions []Mention `json:"mentions"`
}

// buildContent renders a message's content from what the user typed.
// Mentions become links to the person when Markdown is on, and are listed in
// the content either way, so the people named are notified.
func buildContent(req SendRequest) (*event.MessageEventContent, error) {
	text := strings.TrimRight(req.Text, " \t\r\n")
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("chat: the message is empty")
	}
	var content event.MessageEventContent
	if req.Markdown {
		md := text
		for _, men := range req.Mentions {
			uid := id.UserID(men.UserID)
			if men.Name == "" || !strings.Contains(md, "@"+men.Name) {
				continue
			}
			md = strings.ReplaceAll(md, "@"+men.Name, format.MarkdownMentionWithName(men.Name, uid))
		}
		content = format.RenderMarkdown(md, true, false)
		if content.Format == event.FormatHTML {
			// the plain body keeps the names as typed, not the link syntax.
			content.Body = text
		}
	} else {
		content = format.TextToContent(text)
	}
	if req.Emote {
		content.MsgType = event.MsgEmote
	}
	content.Mentions = &event.Mentions{}
	for _, men := range req.Mentions {
		if uid, err := parseUserID(men.UserID); err == nil {
			content.Mentions.Add(uid)
		}
	}
	return &content, nil
}

// Send sends a text message, a reply or an edit, and returns the new event's
// id. In an encrypted room it is encrypted first.
func (m *Manager) Send(ctx context.Context, req SendRequest) (string, error) {
	s, err := m.active()
	if err != nil {
		return "", err
	}
	content, err := buildContent(req)
	if err != nil {
		return "", err
	}
	roomID := id.RoomID(req.RoomID)
	if req.ReplyTo != "" {
		content.RelatesTo = (&event.RelatesTo{}).SetReplyTo(id.EventID(req.ReplyTo))
		if row, ok, _ := s.st.event(ctx, roomID, id.EventID(req.ReplyTo)); ok && row.Sender != s.cli.UserID {
			content.Mentions.Add(row.Sender)
		}
	}
	if req.Edit != "" {
		row, ok, err := s.st.event(ctx, roomID, id.EventID(req.Edit))
		if err != nil {
			return "", err
		}
		if !ok || row.Sender != s.cli.UserID {
			return "", errors.New("chat: only your own messages can be edited")
		}
		content.SetEdit(id.EventID(req.Edit))
	}
	resp, err := s.cli.SendMessageEvent(ctx, roomID, event.EventMessage, content)
	if err != nil {
		return "", fmt.Errorf("chat: send: %w", err)
	}
	return resp.EventID.String(), nil
}

// React adds a reaction to a message. In an encrypted room the reaction is
// encrypted too, so the server does not learn it.
func (m *Manager) React(ctx context.Context, roomID, eventID, key string) (string, error) {
	s, err := m.active()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(key) == "" {
		return "", errors.New("chat: empty reaction")
	}
	rid := id.RoomID(roomID)
	content := &event.ReactionEventContent{RelatesTo: event.RelatesTo{Type: event.RelAnnotation, EventID: id.EventID(eventID), Key: key}}
	evtType := event.EventReaction
	var payload any = content
	if enc, _ := s.stateStore.IsEncrypted(ctx, rid); enc {
		encrypted, err := s.Encrypt(ctx, rid, event.EventReaction, content)
		if err != nil {
			return "", fmt.Errorf("chat: encrypt reaction: %w", err)
		}
		evtType, payload = event.EventEncrypted, encrypted
	}
	resp, err := s.cli.SendMessageEvent(ctx, rid, evtType, payload)
	if err != nil {
		return "", fmt.Errorf("chat: react: %w", err)
	}
	return resp.EventID.String(), nil
}

// Redact removes a message (or takes back a reaction) for everyone.
func (m *Manager) Redact(ctx context.Context, roomID, eventID, reason string) error {
	s, err := m.active()
	if err != nil {
		return err
	}
	if _, err := s.cli.RedactEvent(ctx, id.RoomID(roomID), id.EventID(eventID), mautrix.ReqRedact{Reason: reason}); err != nil {
		return fmt.Errorf("chat: remove message: %w", err)
	}
	return nil
}

// typingTimeout is how long a typing notice lasts unless renewed.
const typingTimeout = 20 * time.Second

// Typing tells a room the user is typing, or stopped. It does nothing when
// the user turned typing notices off.
func (m *Manager) Typing(ctx context.Context, roomID string, typing bool) error {
	s, err := m.active()
	if err != nil {
		return err
	}
	if !m.opts.Prefs().SendTyping {
		return nil
	}
	_, err = s.cli.UserTyping(ctx, id.RoomID(roomID), typing, typingTimeout)
	return err
}

// MarkRead moves the user's read marker in a room to an event. The public
// read receipt is only sent when the user allows it; otherwise a private one
// keeps the user's other devices in step without telling the room.
func (m *Manager) MarkRead(ctx context.Context, roomID, eventID string) error {
	s, err := m.active()
	if err != nil {
		return err
	}
	rid, eid := id.RoomID(roomID), id.EventID(eventID)
	markers := map[string]id.EventID{"m.fully_read": eid}
	if m.opts.Prefs().SendReceipts {
		markers[string(event.ReceiptTypeRead)] = eid
	} else {
		markers[string(event.ReceiptTypeReadPrivate)] = eid
	}
	if err := s.cli.SetReadMarkers(ctx, rid, markers); err != nil {
		return fmt.Errorf("chat: mark read: %w", err)
	}
	s.ingestMu.Lock()
	r, ok, err := s.st.room(ctx, rid)
	if err == nil && ok {
		r.FullyRead = eid
		r.NotifCount = 0
		r.HighlightCount = 0
		err = s.st.putRoom(ctx, r)
	}
	s.ingestMu.Unlock()
	if err != nil {
		return err
	}
	m.emit(EventRooms, RoomsChange{RoomIDs: []string{roomID}})
	return nil
}

// Typists returns who was typing in a room at the last sync.
func (m *Manager) Typists(ctx context.Context, roomID string) []Member {
	s, err := m.active()
	if err != nil {
		return []Member{}
	}
	s.mu.Lock()
	users := append([]id.UserID(nil), s.typing[id.RoomID(roomID)]...)
	s.mu.Unlock()
	out := make([]Member, 0, len(users))
	for _, u := range users {
		out = append(out, s.member(ctx, id.RoomID(roomID), u))
	}
	return out
}
