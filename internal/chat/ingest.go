package chat

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

// RoomsChange is the payload of EventRooms.
type RoomsChange struct {
	RoomIDs []string `json:"roomIds"`
}

// TimelineChange is the payload of EventTimeline.
type TimelineChange struct {
	RoomID string `json:"roomId"`
}

// TypingChange is the payload of EventTyping.
type TypingChange struct {
	RoomID string   `json:"roomId"`
	Users  []Member `json:"users"`
}

// ingest writes what a sync brought into the store and tells the frontend
// what changed. since is the token the sync was made with; empty means the
// first sync, which never notifies.
func (s *session) ingest(ctx context.Context, resp *mautrix.RespSync, since string) {
	s.ingestMu.Lock()
	defer s.ingestMu.Unlock()

	changed := map[id.RoomID]bool{}
	timeline := map[id.RoomID]bool{}
	receipts := map[id.RoomID]bool{}
	var incoming []incomingMessage

	for _, evt := range resp.AccountData.Events {
		if evt.Type.Type == event.AccountDataDirectChats.Type {
			var direct event.DirectChatsEventContent
			if err := json.Unmarshal(evt.Content.VeryRaw, &direct); err == nil {
				if err := s.st.setDirect(ctx, direct); err != nil {
					s.log.Warn("chat: store direct chats", "err", err)
				}
				for _, rooms := range direct {
					for _, r := range rooms {
						changed[r] = true
					}
				}
			}
		}
	}

	for roomID, jr := range resp.Rooms.Join {
		r, _, err := s.st.room(ctx, roomID)
		if err != nil {
			s.log.Warn("chat: read room", "err", err)
			continue
		}
		r.ID = roomID
		r.Membership = string(event.MembershipJoin)
		r.Inviter = ""
		r.InviteDirect = false

		stateEvents := jr.State.Events
		if jr.StateAfter != nil {
			stateEvents = jr.StateAfter.Events
		}
		for _, evt := range stateEvents {
			evt.RoomID = roomID
			s.applyState(ctx, &r, evt)
		}
		if len(jr.Summary.Heroes) > 0 {
			r.Heroes = jr.Summary.Heroes
		}
		if jr.Summary.JoinedMemberCount != nil {
			r.JoinedCount = *jr.Summary.JoinedMemberCount
		}
		if jr.Summary.InvitedMemberCount != nil {
			r.InvitedCount = *jr.Summary.InvitedMemberCount
		}
		if jr.UnreadNotifications != nil {
			r.NotifCount = jr.UnreadNotifications.NotificationCount
			r.HighlightCount = jr.UnreadNotifications.HighlightCount
		}

		if len(jr.Timeline.Events) > 0 || jr.Timeline.Limited {
			if jr.Timeline.Limited {
				if err := s.st.clearTimeline(ctx, roomID); err != nil {
					s.log.Warn("chat: clear timeline", "err", err)
				}
				s.cache.dropRoom(roomID)
				r.PrevBatch = jr.Timeline.PrevBatch
				r.HistoryDone = false
				r.LastEvent = ""
			} else if r.PrevBatch == "" && !r.HistoryDone {
				if lo, hi, _ := s.st.seqBounds(ctx, roomID); lo == 0 && hi == 0 {
					r.PrevBatch = jr.Timeline.PrevBatch
				}
			}
			_, hi, _ := s.st.seqBounds(ctx, roomID)
			for i, evt := range jr.Timeline.Events {
				evt.RoomID = roomID
				parseEvent(evt)
				if evt.StateKey != nil && jr.StateAfter == nil {
					s.applyState(ctx, &r, evt)
				}
				inserted := s.storeEvent(ctx, &r, evt, hi+int64(i)+1, true)
				if inserted && since != "" && evt.Sender != s.cli.UserID && isMessageLike(evt) && relType(evt) == "" {
					incoming = append(incoming, incomingMessage{room: roomID, evt: evt})
				}
			}
			timeline[roomID] = true
		}

		for _, evt := range jr.Ephemeral.Events {
			parseEvent(evt)
			switch evt.Type.Type {
			case event.EphemeralEventReceipt.Type:
				if s.applyReceipts(ctx, roomID, evt) {
					receipts[roomID] = true
				}
			case event.EphemeralEventTyping.Type:
				if content, ok := evt.Content.Parsed.(*event.TypingEventContent); ok {
					s.setTyping(ctx, roomID, content.UserIDs)
				}
			}
		}
		for _, evt := range jr.AccountData.Events {
			parseEvent(evt)
			switch evt.Type.Type {
			case event.AccountDataFullyRead.Type:
				if content, ok := evt.Content.Parsed.(*event.FullyReadEventContent); ok {
					r.FullyRead = content.EventID
				}
			case event.AccountDataRoomTags.Type:
				if content, ok := evt.Content.Parsed.(*event.TagEventContent); ok {
					r.Tags = r.Tags[:0]
					for tag := range content.Tags {
						r.Tags = append(r.Tags, string(tag))
					}
					slices.Sort(r.Tags)
				}
			}
		}
		if err := s.st.putRoom(ctx, r); err != nil {
			s.log.Warn("chat: store room", "err", err)
		}
		changed[roomID] = true
	}

	for roomID, ir := range resp.Rooms.Invite {
		r, _, _ := s.st.room(ctx, roomID)
		r.ID = roomID
		r.Membership = string(event.MembershipInvite)
		for _, evt := range ir.State.Events {
			evt.RoomID = roomID
			parseEvent(evt)
			s.applyState(ctx, &r, evt)
			if evt.Type == event.StateMember && evt.GetStateKey() == s.cli.UserID.String() {
				r.Inviter = evt.Sender
				if content, ok := evt.Content.Parsed.(*event.MemberEventContent); ok {
					r.InviteDirect = content.IsDirect
				}
				if r.LastTS == 0 {
					r.LastTS = evt.Timestamp
				}
			}
		}
		if err := s.st.putRoom(ctx, r); err != nil {
			s.log.Warn("chat: store invite", "err", err)
		}
		changed[roomID] = true
	}

	for roomID := range resp.Rooms.Leave {
		if err := s.st.deleteRoom(ctx, roomID); err != nil {
			s.log.Warn("chat: forget room", "err", err)
		}
		s.cache.dropRoom(roomID)
		changed[roomID] = true
	}

	if len(changed) > 0 {
		s.m.emit(EventRooms, RoomsChange{RoomIDs: roomIDStrings(changed)})
	}
	for roomID := range timeline {
		s.m.emit(EventTimeline, TimelineChange{RoomID: roomID.String()})
	}
	for roomID := range receipts {
		s.m.emit(EventReceipts, TimelineChange{RoomID: roomID.String()})
	}

	s.mu.Lock()
	first := !s.synced
	s.synced = true
	s.mu.Unlock()
	if !first && len(incoming) > 0 {
		s.notify(ctx, incoming)
	}
}

func roomIDStrings(set map[id.RoomID]bool) []string {
	out := make([]string, 0, len(set))
	for r := range set {
		out = append(out, r.String())
	}
	slices.Sort(out)
	return out
}

// parseEvent fills in an event's parsed content. Events read back from the
// store or a page arrive unparsed; ones from the syncer already are.
func parseEvent(evt *event.Event) {
	if evt.Content.Parsed != nil {
		return
	}
	switch {
	case evt.StateKey != nil:
		evt.Type.Class = event.StateEventType
	case evt.Type.Class == event.UnknownEventType:
		evt.Type.Class = event.MessageEventType
	}
	_ = evt.Content.ParseRaw(evt.Type)
}

// applyState folds one state event into the stored room.
func (s *session) applyState(ctx context.Context, r *roomRow, evt *event.Event) {
	parseEvent(evt)
	switch evt.Type.Type {
	case event.StateRoomName.Type:
		if c, ok := evt.Content.Parsed.(*event.RoomNameEventContent); ok {
			r.Name = c.Name
		}
	case event.StateTopic.Type:
		if c, ok := evt.Content.Parsed.(*event.TopicEventContent); ok {
			r.Topic = c.Topic
		}
	case event.StateRoomAvatar.Type:
		if c, ok := evt.Content.Parsed.(*event.RoomAvatarEventContent); ok {
			r.Avatar = string(c.URL)
		}
	case event.StateCanonicalAlias.Type:
		if c, ok := evt.Content.Parsed.(*event.CanonicalAliasEventContent); ok {
			r.CanonicalAlias = c.Alias.String()
		}
	case event.StateEncryption.Type:
		r.Encrypted = true
	case event.StateCreate.Type:
		if c, ok := evt.Content.Parsed.(*event.CreateEventContent); ok {
			r.IsSpace = c.Type == event.RoomTypeSpace
		}
	case event.StateSpaceChild.Type:
		child := id.RoomID(evt.GetStateKey())
		if child != "" {
			via, _ := evt.Content.Raw["via"].([]any)
			if err := s.st.setSpaceChild(ctx, r.ID, child, len(via) > 0); err != nil {
				s.log.Warn("chat: store space child", "err", err)
			}
		}
	}
}

// relation reads which event an event relates to, and how. A redaction
// relates to the event it removes.
func relation(evt *event.Event) (string, id.EventID) {
	if evt.Type.Type == event.EventRedaction.Type {
		if evt.Redacts != "" {
			return "", evt.Redacts
		}
		if target, ok := evt.Content.Raw["redacts"].(string); ok {
			return "", id.EventID(target)
		}
		return "", ""
	}
	rel, ok := evt.Content.Raw["m.relates_to"].(map[string]any)
	if !ok {
		return "", ""
	}
	relType, _ := rel["rel_type"].(string)
	target, _ := rel["event_id"].(string)
	if relType == "" {
		return "", ""
	}
	return relType, id.EventID(target)
}

func relType(evt *event.Event) string {
	t, _ := relation(evt)
	return t
}

// isMessageLike reports whether an event is something a person said or sent.
func isMessageLike(evt *event.Event) bool {
	switch evt.Type.Type {
	case event.EventMessage.Type, event.EventEncrypted.Type, event.EventSticker.Type:
		return true
	}
	return false
}

// encodeEvent serialises an event with its content exactly as the server sent
// it.
func encodeEvent(evt *event.Event) ([]byte, error) {
	cp := *evt
	cp.Content = event.Content{VeryRaw: evt.Content.VeryRaw}
	if cp.Content.VeryRaw == nil {
		raw, err := json.Marshal(&evt.Content)
		if err != nil {
			return nil, err
		}
		cp.Content.VeryRaw = raw
	}
	return json.Marshal(&cp)
}

// decodeEvent reads a stored event back.
func decodeEvent(row eventRow) (*event.Event, error) {
	var evt event.Event
	if err := json.Unmarshal(row.Raw, &evt); err != nil {
		return nil, err
	}
	evt.RoomID = row.RoomID
	parseEvent(&evt)
	return &evt, nil
}

// storeEvent writes one timeline event. forward is true for events from a
// sync, which move the room's last activity; backwards pages do not. It
// reports whether the event was new.
func (s *session) storeEvent(ctx context.Context, r *roomRow, evt *event.Event, seq int64, forward bool) bool {
	raw, err := encodeEvent(evt)
	if err != nil {
		return false
	}
	rt, target := relation(evt)
	row := eventRow{
		RoomID: r.ID, EventID: evt.ID, Seq: seq, TS: evt.Timestamp, Sender: evt.Sender,
		Type: evt.Type.Type, StateKey: evt.StateKey, RelType: rt, RelatesTo: target, Raw: raw,
	}
	if evt.Unsigned.RedactedBecause != nil {
		row.RedactedBy = evt.Unsigned.RedactedBecause.ID
		if row.RedactedBy == "" {
			row.RedactedBy = "redacted"
		}
	} else if by, err := s.st.redactionOf(ctx, r.ID, evt.ID); err == nil && by != "" {
		row.RedactedBy = by
	}
	inserted, err := s.st.insertEvent(ctx, row)
	if err != nil {
		s.log.Warn("chat: store event", "err", err)
		return false
	}
	if !inserted {
		return false
	}
	if evt.Type.Type == event.EventRedaction.Type && target != "" {
		if err := s.st.markRedacted(ctx, r.ID, target, evt.ID); err != nil {
			s.log.Warn("chat: apply redaction", "err", err)
		}
		s.cache.drop(target)
	}
	if forward {
		if isMessageLike(evt) && rt != string(event.RelReplace) && rt != string(event.RelAnnotation) {
			r.LastEvent = evt.ID
			r.LastTS = max(r.LastTS, evt.Timestamp)
		} else if r.LastTS == 0 {
			r.LastTS = evt.Timestamp
		}
	}
	return true
}

// applyReceipts stores the read receipts of an m.receipt event. It reports
// whether any changed.
func (s *session) applyReceipts(ctx context.Context, roomID id.RoomID, evt *event.Event) bool {
	content, ok := evt.Content.Parsed.(*event.ReceiptEventContent)
	if !ok {
		return false
	}
	any := false
	for eventID, receipts := range *content {
		for rtype, users := range receipts {
			if rtype != event.ReceiptTypeRead && rtype != event.ReceiptTypeReadPrivate {
				continue
			}
			for user, rr := range users {
				if rr.ThreadID != "" && rr.ThreadID != event.ReadReceiptThreadMain {
					continue
				}
				if err := s.st.putReceipt(ctx, roomID, user, eventID, rr.Timestamp.UnixMilli()); err == nil {
					any = true
				}
			}
		}
	}
	return any
}

// setTyping records who is typing in a room and tells the frontend.
func (s *session) setTyping(ctx context.Context, roomID id.RoomID, users []id.UserID) {
	others := make([]id.UserID, 0, len(users))
	for _, u := range users {
		if u != s.cli.UserID {
			others = append(others, u)
		}
	}
	s.mu.Lock()
	s.typing[roomID] = others
	s.mu.Unlock()
	members := make([]Member, 0, len(others))
	for _, u := range others {
		members = append(members, s.member(ctx, roomID, u))
	}
	s.m.emit(EventTyping, TypingChange{RoomID: roomID.String(), Users: members})
}

// decryptCache keeps decrypted events by id, so a timeline redrawn on every
// new message does not decrypt the same rows again. Only successes are kept.
type decryptCache struct {
	mu    sync.Mutex
	limit int
	items map[id.EventID]*event.Event
	order []id.EventID
}

func newDecryptCache(limit int) *decryptCache {
	return &decryptCache{limit: limit, items: map[id.EventID]*event.Event{}}
}

func (c *decryptCache) get(eventID id.EventID) *event.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.items[eventID]
}

func (c *decryptCache) put(evt *event.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.items[evt.ID]; ok {
		return
	}
	c.items[evt.ID] = evt
	c.order = append(c.order, evt.ID)
	for len(c.order) > c.limit {
		delete(c.items, c.order[0])
		c.order = c.order[1:]
	}
}

func (c *decryptCache) drop(eventID id.EventID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.items, eventID)
}

func (c *decryptCache) dropRoom(roomID id.RoomID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, v := range c.items {
		if v.RoomID == roomID {
			delete(c.items, k)
		}
	}
}

// decrypt decrypts an encrypted event, from the cache when it can. Events
// that are not encrypted come back as they are.
func (s *session) decrypt(ctx context.Context, evt *event.Event) (*event.Event, error) {
	if evt.Type.Type != event.EventEncrypted.Type {
		return evt, nil
	}
	if dec := s.cache.get(evt.ID); dec != nil {
		return dec, nil
	}
	parseEvent(evt)
	dec, err := s.mach.DecryptMegolmEvent(ctx, evt)
	if err != nil {
		return nil, err
	}
	if dec.Content.Parsed == nil {
		parseEvent(dec)
	}
	dec.RoomID = evt.RoomID
	s.cache.put(dec)
	return dec, nil
}

// errMissingKeys marks an event whose room key this device does not have.
var errMissingKeys = errors.New("chat: missing room key")

// isMissingKeys reports whether a decryption failed for want of the key,
// which a key request or the backup can still fix.
func isMissingKeys(err error) bool {
	return err != nil && (errors.Is(err, errMissingKeys) || strings.Contains(err.Error(), "no session with given ID found") ||
		strings.Contains(err.Error(), "failed to decrypt megolm event: no session"))
}
