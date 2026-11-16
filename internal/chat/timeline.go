package chat

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/crypto"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

// Message is one row of a room's timeline, with its edits, reactions and
// redaction already applied.
type Message struct {
	ID           string `json:"id"`
	Sender       string `json:"sender"`
	SenderName   string `json:"senderName"`
	SenderAvatar string `json:"senderAvatar"`
	// TS is when the server received it, in Unix milliseconds.
	TS int64 `json:"ts"`
	// Kind is text, notice, emote, image, video, audio, file, sticker,
	// location, state, redacted, encrypted (could not be read) or unknown.
	Kind string `json:"kind"`
	Body string `json:"body"`
	// HTML is the sender's formatted body, unsanitised. The frontend runs it
	// through the app's sanitiser before it is shown.
	HTML    string `json:"html"`
	Edited  bool   `json:"edited"`
	Own     bool   `json:"own"`
	ReplyTo *Reply `json:"replyTo"`
	// Reactions are grouped by key, in the order they were first given.
	Reactions []Reaction `json:"reactions"`
	Media     *Media     `json:"media"`
	State     *StateChange `json:"state"`
	// Encrypted is true for an event that was sent encrypted.
	Encrypted bool `json:"encrypted"`
	// DecryptError says why an encrypted event could not be read.
	DecryptError string `json:"decryptError"`
	// MissingKeys is true when the room key may still arrive, so a retry
	// can help.
	MissingKeys bool `json:"missingKeys"`
	// Mentioned is true when the message names the user.
	Mentioned bool `json:"mentioned"`
	// ReadBy are the people whose read marker rests on this message.
	ReadBy []Member `json:"readBy"`
}

// Reply is the message a reply answers.
type Reply struct {
	ID         string `json:"id"`
	Sender     string `json:"sender"`
	SenderName string `json:"senderName"`
	Body       string `json:"body"`
	// Missing is true when the original is not stored here.
	Missing bool `json:"missing"`
}

// Reaction is one reaction key on a message.
type Reaction struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
	// Mine is the id of the user's own reaction with this key, for taking it
	// back; empty when the user did not react with it.
	Mine    string   `json:"mine"`
	Senders []string `json:"senders"`
}

// Media describes the file of an image, video, audio, file or sticker
// message. The bytes are fetched separately.
type Media struct {
	Name      string `json:"name"`
	Mime      string `json:"mime"`
	Size      int64  `json:"size"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Duration  int    `json:"duration"`
	Thumbnail bool   `json:"thumbnail"`
	Encrypted bool   `json:"encrypted"`
}

// StateChange describes a membership or room change, for the frontend to
// phrase.
type StateChange struct {
	// Kind is joined, left, invited, kicked, banned, unbanned, rejected,
	// renamed, avatar, name, topic, roomAvatar, encryption or created.
	Kind       string `json:"kind"`
	Target     string `json:"target"`
	TargetName string `json:"targetName"`
	Value      string `json:"value"`
}

// TimelinePage is a slice of a room's timeline, oldest first.
type TimelinePage struct {
	Messages []Message `json:"messages"`
	// HasMore is true when older messages exist, here or on the server.
	HasMore bool `json:"hasMore"`
	// FullyRead is the event the user has read up to.
	FullyRead string `json:"fullyRead"`
}

// paginateBatch is how many events one backwards request asks for.
const paginateBatch = 50

// Timeline returns up to limit messages of a room older than before (the
// newest when before is empty). When the store runs short it pages backwards
// on the server first.
func (m *Manager) Timeline(ctx context.Context, roomID, before string, limit int) (TimelinePage, error) {
	s, err := m.active()
	if err != nil {
		return TimelinePage{}, err
	}
	return s.timeline(ctx, id.RoomID(roomID), id.EventID(before), limit)
}

func (s *session) timeline(ctx context.Context, roomID id.RoomID, before id.EventID, limit int) (TimelinePage, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	var beforeSeq *int64
	if before != "" {
		row, ok, err := s.st.event(ctx, roomID, before)
		if err != nil {
			return TimelinePage{}, err
		}
		if !ok {
			return TimelinePage{}, fmt.Errorf("chat: message %s is not loaded", before)
		}
		beforeSeq = &row.Seq
	}
	rows, err := s.st.page(ctx, roomID, beforeSeq, limit)
	if err != nil {
		return TimelinePage{}, err
	}
	// a page of nothing but reactions and state can come back from the
	// server, so a few requests may be needed to fill one screen.
	for tries := 0; len(rows) < limit && tries < 4; tries++ {
		added, err := s.paginate(ctx, roomID)
		if err != nil {
			if len(rows) > 0 {
				break
			}
			return TimelinePage{}, err
		}
		if added == 0 {
			break
		}
		if rows, err = s.st.page(ctx, roomID, beforeSeq, limit); err != nil {
			return TimelinePage{}, err
		}
	}
	r, _, err := s.st.room(ctx, roomID)
	if err != nil {
		return TimelinePage{}, err
	}
	slices.Reverse(rows)
	msgs, err := s.buildMessages(ctx, roomID, rows)
	if err != nil {
		return TimelinePage{}, err
	}
	return TimelinePage{
		Messages:  msgs,
		HasMore:   len(rows) == limit || (!r.HistoryDone && r.PrevBatch != ""),
		FullyRead: r.FullyRead.String(),
	}, nil
}

// paginate fetches one batch of older events from the server into the store.
// It returns how many events were new.
func (s *session) paginate(ctx context.Context, roomID id.RoomID) (int, error) {
	s.ingestMu.Lock()
	defer s.ingestMu.Unlock()
	r, ok, err := s.st.room(ctx, roomID)
	if err != nil || !ok || r.HistoryDone || r.PrevBatch == "" {
		return 0, err
	}
	resp, err := s.cli.Messages(ctx, roomID, r.PrevBatch, "", mautrix.DirectionBackward, &mautrix.FilterPart{LazyLoadMembers: true}, paginateBatch)
	if err != nil {
		return 0, fmt.Errorf("chat: load older messages: %w", err)
	}
	for _, evt := range resp.State {
		evt.RoomID = roomID
		parseEvent(evt)
		mautrix.UpdateStateStore(ctx, s.stateStore, evt)
	}
	lo, hi, err := s.st.seqBounds(ctx, roomID)
	if err != nil {
		return 0, err
	}
	seq := lo - 1
	if lo == 0 && hi == 0 {
		seq = -1
	}
	added := 0
	for _, evt := range resp.Chunk {
		evt.RoomID = roomID
		parseEvent(evt)
		if s.storeEvent(ctx, &r, evt, seq, false) {
			added++
			seq--
		}
	}
	if resp.End == "" || len(resp.Chunk) == 0 {
		r.HistoryDone = true
		r.PrevBatch = ""
	} else {
		r.PrevBatch = resp.End
	}
	return added, s.st.putRoom(ctx, r)
}

// buildMessages turns stored rows, oldest first, into timeline messages.
func (s *session) buildMessages(ctx context.Context, roomID id.RoomID, rows []eventRow) ([]Message, error) {
	ids := make([]id.EventID, len(rows))
	for i, r := range rows {
		ids[i] = r.EventID
	}
	rels, err := s.st.relations(ctx, roomID, ids)
	if err != nil {
		return nil, err
	}
	edits := map[id.EventID]*event.Event{}
	reactions := map[id.EventID][]*event.Event{}
	for _, rel := range rels {
		if rel.RedactedBy != "" || (rel.RelType != string(event.RelReplace) && rel.RelType != string(event.RelAnnotation)) {
			continue
		}
		evt, err := decodeEvent(rel)
		if err != nil {
			continue
		}
		dec, err := s.decrypt(ctx, evt)
		if err != nil {
			continue
		}
		switch rel.RelType {
		case string(event.RelReplace):
			edits[rel.RelatesTo] = dec
		case string(event.RelAnnotation):
			reactions[rel.RelatesTo] = append(reactions[rel.RelatesTo], dec)
		}
	}
	receipts, err := s.st.receipts(ctx, roomID)
	if err != nil {
		return nil, err
	}
	readBy := map[id.EventID][]Member{}
	for user, eventID := range receipts {
		if user != s.cli.UserID {
			readBy[eventID] = append(readBy[eventID], s.member(ctx, roomID, user))
		}
	}

	out := make([]Message, 0, len(rows))
	for _, row := range rows {
		evt, err := decodeEvent(row)
		if err != nil {
			continue
		}
		msg := s.toMessage(ctx, roomID, row, evt, edits, reactions)
		if msg == nil {
			continue
		}
		if who := readBy[row.EventID]; len(who) > 0 {
			slices.SortFunc(who, func(a, b Member) int { return strings.Compare(a.Name, b.Name) })
			msg.ReadBy = who
		}
		out = append(out, *msg)
	}
	return out, nil
}

// toMessage builds one message. It returns nil for a row that should not show.
func (s *session) toMessage(ctx context.Context, roomID id.RoomID, row eventRow, evt *event.Event,
	edits map[id.EventID]*event.Event, reactions map[id.EventID][]*event.Event) *Message {
	sender := s.member(ctx, roomID, evt.Sender)
	msg := &Message{
		ID: evt.ID.String(), Sender: evt.Sender.String(), SenderName: sender.Name, SenderAvatar: sender.Avatar,
		TS: evt.Timestamp, Own: evt.Sender == s.cli.UserID, Reactions: []Reaction{}, ReadBy: []Member{},
		Encrypted: evt.Type.Type == event.EventEncrypted.Type,
	}
	if row.RedactedBy != "" {
		msg.Kind = "redacted"
		return msg
	}
	if evt.StateKey != nil {
		msg.Kind = "state"
		msg.State = s.stateChange(ctx, roomID, evt)
		if msg.State == nil {
			return nil
		}
		return msg
	}

	dec, err := s.decrypt(ctx, evt)
	if err != nil {
		msg.Kind = "encrypted"
		msg.MissingKeys = errors.Is(err, crypto.ErrNoSessionFound) || isMissingKeys(err)
		msg.DecryptError = err.Error()
		if msg.MissingKeys {
			go s.requestKeys(context.WithoutCancel(ctx), evt, false)
		}
		return msg
	}
	content, ok := dec.Content.Parsed.(*event.MessageEventContent)
	if !ok {
		if dec.Type.Type == event.EventMessage.Type || dec.Type.Type == event.EventSticker.Type {
			msg.Kind = "unknown"
			return msg
		}
		// an encrypted reaction or edit with no visible row of its own.
		return nil
	}
	if strings.HasPrefix(string(content.MsgType), "m.key.verification") {
		return nil
	}
	if replyTo := content.RelatesTo.GetNonFallbackReplyTo(); replyTo != "" {
		msg.ReplyTo = s.replyPreview(ctx, roomID, replyTo)
	}
	if edit := edits[evt.ID]; edit != nil && edit.Sender == evt.Sender {
		if ec, ok := edit.Content.Parsed.(*event.MessageEventContent); ok && ec.NewContent != nil {
			content = ec.NewContent
			msg.Edited = true
		}
	}
	content.RemoveReplyFallback()
	fillContent(msg, dec.Type, content)
	msg.Mentioned = !msg.Own && s.mentions(content)

	if list := reactions[evt.ID]; len(list) > 0 {
		msg.Reactions = s.groupReactions(ctx, roomID, list)
	}
	return msg
}

// fillContent copies the visible parts of a message's content.
func fillContent(msg *Message, evtType event.Type, content *event.MessageEventContent) {
	msg.Body = content.Body
	if content.Format == event.FormatHTML {
		msg.HTML = content.FormattedBody
	}
	switch {
	case evtType.Type == event.EventSticker.Type:
		msg.Kind = "sticker"
	case content.MsgType == event.MsgText:
		msg.Kind = "text"
	case content.MsgType == event.MsgNotice:
		msg.Kind = "notice"
	case content.MsgType == event.MsgEmote:
		msg.Kind = "emote"
	case content.MsgType == event.MsgImage:
		msg.Kind = "image"
	case content.MsgType == event.MsgVideo:
		msg.Kind = "video"
	case content.MsgType == event.MsgAudio:
		msg.Kind = "audio"
	case content.MsgType == event.MsgFile:
		msg.Kind = "file"
	case content.MsgType == event.MsgLocation:
		msg.Kind = "location"
	default:
		msg.Kind = "unknown"
	}
	if msg.Kind == "image" || msg.Kind == "video" || msg.Kind == "audio" || msg.Kind == "file" || msg.Kind == "sticker" {
		media := &Media{Name: content.GetFileName(), Encrypted: content.File != nil}
		if info := content.Info; info != nil {
			media.Mime = info.MimeType
			media.Size = int64(info.Size)
			media.Width = info.Width
			media.Height = info.Height
			media.Duration = info.Duration
			media.Thumbnail = info.ThumbnailURL != "" || info.ThumbnailFile != nil
		}
		if media.Name == "" {
			media.Name = content.Body
		}
		msg.Media = media
		// a caption is a body that differs from the file name.
		if content.FileName == "" || content.Body == content.FileName {
			msg.Body = ""
			msg.HTML = ""
		}
	}
}

// replyPreview looks up the message a reply answers.
func (s *session) replyPreview(ctx context.Context, roomID id.RoomID, target id.EventID) *Reply {
	rep := &Reply{ID: target.String()}
	row, ok, err := s.st.event(ctx, roomID, target)
	if err != nil || !ok {
		rep.Missing = true
		return rep
	}
	evt, err := decodeEvent(row)
	if err != nil {
		rep.Missing = true
		return rep
	}
	who := s.member(ctx, roomID, evt.Sender)
	rep.Sender = evt.Sender.String()
	rep.SenderName = who.Name
	if row.RedactedBy != "" {
		return rep
	}
	if dec, err := s.decrypt(ctx, evt); err == nil {
		if c, ok := dec.Content.Parsed.(*event.MessageEventContent); ok {
			c.RemoveReplyFallback()
			rep.Body = previewText(dec.Type, c)
		}
	}
	return rep
}

// previewText is a one-line rendering of a message, for replies and the room
// list. Media show as their file name.
func previewText(evtType event.Type, c *event.MessageEventContent) string {
	body := c.Body
	if c.MsgType.IsMedia() || evtType.Type == event.EventSticker.Type {
		if name := c.GetFileName(); name != "" {
			body = name
		}
	}
	body = strings.Join(strings.Fields(body), " ")
	if len([]rune(body)) > 200 {
		body = string([]rune(body)[:200])
	}
	return body
}

// groupReactions folds reaction events into one entry per key.
func (s *session) groupReactions(ctx context.Context, roomID id.RoomID, list []*event.Event) []Reaction {
	var out []Reaction
	index := map[string]int{}
	seen := map[string]bool{}
	for _, evt := range list {
		c, ok := evt.Content.Parsed.(*event.ReactionEventContent)
		if !ok {
			continue
		}
		key := c.RelatesTo.Key
		if key == "" {
			continue
		}
		// one person reacting twice with the same key counts once.
		if seen[key+"\x00"+evt.Sender.String()] {
			continue
		}
		seen[key+"\x00"+evt.Sender.String()] = true
		i, ok := index[key]
		if !ok {
			i = len(out)
			index[key] = i
			out = append(out, Reaction{Key: key, Senders: []string{}})
		}
		out[i].Count++
		out[i].Senders = append(out[i].Senders, s.member(ctx, roomID, evt.Sender).Name)
		if evt.Sender == s.cli.UserID {
			out[i].Mine = evt.ID.String()
		}
	}
	return out
}

// mentions reports whether a message names the user: through its mentions
// list, or by display name or user name in the text.
func (s *session) mentions(c *event.MessageEventContent) bool {
	if c.Mentions != nil && (c.Mentions.Has(s.cli.UserID) || c.Mentions.Room) {
		return true
	}
	body := strings.ToLower(c.Body)
	local := strings.ToLower(s.cli.UserID.Localpart())
	if local != "" && containsWord(body, local) {
		return true
	}
	name := strings.ToLower(strings.TrimSpace(s.m.Status().DisplayName))
	return name != "" && containsWord(body, name)
}

// containsWord reports whether needle appears in hay with no letter or digit
// right before or after it.
func containsWord(hay, needle string) bool {
	for start := 0; ; {
		i := strings.Index(hay[start:], needle)
		if i < 0 {
			return false
		}
		i += start
		end := i + len(needle)
		before := i == 0 || !isWordByte(hay[i-1])
		after := end >= len(hay) || !isWordByte(hay[end])
		if before && after {
			return true
		}
		start = i + 1
	}
}

func isWordByte(b byte) bool {
	return b == '_' || (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || b >= 0x80
}

// stateChange describes a state event in the timeline, or nil for one not
// worth a row.
func (s *session) stateChange(ctx context.Context, roomID id.RoomID, evt *event.Event) *StateChange {
	switch evt.Type.Type {
	case event.StateMember.Type:
		c, ok := evt.Content.Parsed.(*event.MemberEventContent)
		if !ok {
			return nil
		}
		target := id.UserID(evt.GetStateKey())
		ch := &StateChange{Target: target.String(), TargetName: c.Displayname}
		if ch.TargetName == "" {
			ch.TargetName = s.member(ctx, roomID, target).Name
		}
		prev := event.MembershipLeave
		var prevContent *event.MemberEventContent
		if evt.Unsigned.PrevContent != nil {
			_ = evt.Unsigned.PrevContent.ParseRaw(event.StateMember)
			if pc, ok := evt.Unsigned.PrevContent.Parsed.(*event.MemberEventContent); ok {
				prev = pc.Membership
				prevContent = pc
			}
		}
		kicked := evt.Sender.String() != evt.GetStateKey()
		switch {
		case c.Membership == event.MembershipJoin && prev == event.MembershipJoin:
			if prevContent != nil && prevContent.Displayname != c.Displayname {
				ch.Kind = "renamed"
				ch.Value = c.Displayname
				if prevContent.Displayname != "" {
					ch.TargetName = prevContent.Displayname
				}
			} else if prevContent != nil && prevContent.AvatarURL != c.AvatarURL {
				ch.Kind = "avatar"
			} else {
				return nil
			}
		case c.Membership == event.MembershipJoin:
			ch.Kind = "joined"
		case c.Membership == event.MembershipInvite:
			ch.Kind = "invited"
		case c.Membership == event.MembershipBan:
			ch.Kind = "banned"
		case c.Membership == event.MembershipLeave && prev == event.MembershipBan:
			ch.Kind = "unbanned"
		case c.Membership == event.MembershipLeave && prev == event.MembershipInvite && !kicked:
			ch.Kind = "rejected"
		case c.Membership == event.MembershipLeave && kicked:
			ch.Kind = "kicked"
		case c.Membership == event.MembershipLeave:
			ch.Kind = "left"
		default:
			return nil
		}
		return ch
	case event.StateRoomName.Type:
		c, _ := evt.Content.Parsed.(*event.RoomNameEventContent)
		if c == nil {
			return nil
		}
		return &StateChange{Kind: "name", Value: c.Name}
	case event.StateTopic.Type:
		c, _ := evt.Content.Parsed.(*event.TopicEventContent)
		if c == nil {
			return nil
		}
		return &StateChange{Kind: "topic", Value: c.Topic}
	case event.StateRoomAvatar.Type:
		return &StateChange{Kind: "roomAvatar"}
	case event.StateEncryption.Type:
		return &StateChange{Kind: "encryption"}
	case event.StateCreate.Type:
		return &StateChange{Kind: "created"}
	}
	return nil
}

// RetryDecryption asks again for the keys of the undecryptable messages of a
// room: from the key backup when this device can read it, and from the
// sender's and the user's other devices.
func (m *Manager) RetryDecryption(ctx context.Context, roomID string, eventIDs []string) error {
	s, err := m.active()
	if err != nil {
		return err
	}
	rid := id.RoomID(roomID)
	for _, eid := range eventIDs {
		row, ok, err := s.st.event(ctx, rid, id.EventID(eid))
		if err != nil || !ok {
			continue
		}
		evt, err := decodeEvent(row)
		if err != nil {
			continue
		}
		s.requestKeys(ctx, evt, true)
	}
	m.emit(EventTimeline, TimelineChange{RoomID: roomID})
	return nil
}

// requestKeys tries to get the room key of an undecryptable event: from the
// backup first, then from other devices. Unless force is set, one session is
// asked for at most once every ten minutes.
func (s *session) requestKeys(ctx context.Context, evt *event.Event, force bool) {
	c, ok := evt.Content.Parsed.(*event.EncryptedEventContent)
	if !ok {
		return
	}
	s.mu.Lock()
	last, asked := s.keyRequests[c.SessionID]
	if asked && !force && timeSince(last) < keyRequestInterval {
		s.mu.Unlock()
		return
	}
	s.keyRequests[c.SessionID] = timeNow()
	s.mu.Unlock()
	if s.restoreSessionFromBackup(ctx, evt.RoomID, c.SessionID) {
		return
	}
	s.RequestSession(ctx, evt.RoomID, c.SenderKey, c.SessionID, evt.Sender, c.DeviceID)
}
