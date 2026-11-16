package chat

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

// Room is one room, direct chat, space or invite in the room list.
type Room struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Topic string `json:"topic"`
	// Avatar is the room's mxc picture; for a direct chat without one, the
	// other person's.
	Avatar string `json:"avatar"`
	Alias  string `json:"alias"`
	// Kind is direct, room or space.
	Kind string `json:"kind"`
	// DirectUser is the other person of a direct chat.
	DirectUser string `json:"directUser"`
	Encrypted  bool   `json:"encrypted"`
	// Membership is join or invite.
	Membership  string `json:"membership"`
	InviterID   string `json:"inviterId"`
	InviterName string `json:"inviterName"`
	Favourite   bool   `json:"favourite"`
	LowPriority bool   `json:"lowPriority"`
	Unread      int    `json:"unread"`
	Highlight   int    `json:"highlight"`
	// LastTS is the time of the latest message, in Unix milliseconds.
	LastTS        int64  `json:"lastTs"`
	Preview       string `json:"preview"`
	PreviewSender string `json:"previewSender"`
	// PreviewKind is the Kind of the latest message.
	PreviewKind string   `json:"previewKind"`
	Spaces      []string `json:"spaces"`
	MemberCount int      `json:"memberCount"`
	FullyRead   string   `json:"fullyRead"`
}

// Member is a person in a room.
type Member struct {
	UserID     string `json:"userId"`
	Name       string `json:"name"`
	Avatar     string `json:"avatar"`
	Membership string `json:"membership"`
	PowerLevel int    `json:"powerLevel"`
}

// member returns how a user shows in a room: their room display name, or the
// user name of their Matrix ID.
func (s *session) member(ctx context.Context, roomID id.RoomID, user id.UserID) Member {
	m := Member{UserID: user.String(), Name: user.Localpart()}
	if m.Name == "" {
		m.Name = user.String()
	}
	if content, err := s.stateStore.TryGetMember(ctx, roomID, user); err == nil && content != nil {
		if content.Displayname != "" {
			m.Name = content.Displayname
		}
		m.Avatar = string(content.AvatarURL)
		m.Membership = string(content.Membership)
	}
	return m
}

// Rooms lists every joined room and pending invite, most recent activity
// first.
func (m *Manager) Rooms(ctx context.Context) ([]Room, error) {
	s, err := m.active()
	if err != nil {
		return nil, err
	}
	return s.rooms(ctx)
}

func (s *session) rooms(ctx context.Context) ([]Room, error) {
	rows, err := s.st.rooms(ctx)
	if err != nil {
		return nil, err
	}
	directs, err := s.st.directs(ctx)
	if err != nil {
		return nil, err
	}
	parents, err := s.st.spaceParents(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Room, 0, len(rows))
	for _, r := range rows {
		out = append(out, s.toRoom(ctx, r, directs, parents))
	}
	slices.SortStableFunc(out, func(a, b Room) int {
		switch {
		case a.LastTS > b.LastTS:
			return -1
		case a.LastTS < b.LastTS:
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
	return out, nil
}

// Room returns one room.
func (m *Manager) Room(ctx context.Context, roomID string) (Room, error) {
	s, err := m.active()
	if err != nil {
		return Room{}, err
	}
	r, ok, err := s.st.room(ctx, id.RoomID(roomID))
	if err != nil {
		return Room{}, err
	}
	if !ok {
		return Room{}, fmt.Errorf("chat: room %s is not known", roomID)
	}
	directs, _ := s.st.directs(ctx)
	parents, _ := s.st.spaceParents(ctx)
	return s.toRoom(ctx, r, directs, parents), nil
}

func (s *session) toRoom(ctx context.Context, r roomRow, directs map[id.RoomID]id.UserID, parents map[id.RoomID][]id.RoomID) Room {
	room := Room{
		ID: r.ID.String(), Name: r.Name, Topic: r.Topic, Avatar: r.Avatar, Alias: r.CanonicalAlias,
		Kind: "room", Encrypted: r.Encrypted, Membership: r.Membership, Unread: r.NotifCount,
		Highlight: r.HighlightCount, LastTS: r.LastTS, FullyRead: r.FullyRead.String(),
		MemberCount: r.JoinedCount, Spaces: []string{},
	}
	for _, p := range parents[r.ID] {
		room.Spaces = append(room.Spaces, p.String())
	}
	for _, tag := range r.Tags {
		switch event.RoomTag(tag) {
		case event.RoomTagFavourite:
			room.Favourite = true
		case event.RoomTagLowPriority:
			room.LowPriority = true
		}
	}
	if r.IsSpace {
		room.Kind = "space"
	}
	other, direct := directs[r.ID]
	if r.Membership == string(event.MembershipInvite) && r.InviteDirect {
		other, direct = r.Inviter, true
	}
	if direct && !r.IsSpace {
		room.Kind = "direct"
		room.DirectUser = other.String()
	}
	if r.Inviter != "" {
		inv := s.member(ctx, r.ID, r.Inviter)
		room.InviterID = r.Inviter.String()
		room.InviterName = inv.Name
	}
	if room.Name == "" {
		room.Name = s.fallbackName(ctx, r, room)
	}
	if room.Avatar == "" && direct {
		room.Avatar = s.member(ctx, r.ID, other).Avatar
	}
	if r.Membership == string(event.MembershipJoin) {
		s.fillPreview(ctx, r, &room)
	}
	return room
}

// fallbackName names a room that has no name of its own: by its alias, or by
// the people in it, as the spec describes.
func (s *session) fallbackName(ctx context.Context, r roomRow, room Room) string {
	if r.CanonicalAlias != "" {
		return r.CanonicalAlias
	}
	if room.DirectUser != "" {
		return s.member(ctx, r.ID, id.UserID(room.DirectUser)).Name
	}
	var names []string
	for _, h := range r.Heroes {
		if h != s.cli.UserID {
			names = append(names, s.member(ctx, r.ID, h).Name)
		}
	}
	if len(names) == 0 && r.Inviter != "" {
		return room.InviterName
	}
	if len(names) > 3 {
		names = names[:3]
	}
	return strings.Join(names, ", ")
}

// fillPreview sets the room list preview from the room's latest message.
func (s *session) fillPreview(ctx context.Context, r roomRow, room *Room) {
	row, ok, err := s.st.latestMessage(ctx, r.ID)
	if err != nil || !ok {
		return
	}
	room.LastTS = max(room.LastTS, row.TS)
	if row.Sender == s.cli.UserID {
		room.PreviewSender = ""
	} else {
		room.PreviewSender = s.member(ctx, r.ID, row.Sender).Name
	}
	if row.RedactedBy != "" {
		room.PreviewKind = "redacted"
		return
	}
	evt, err := decodeEvent(row)
	if err != nil {
		return
	}
	dec, err := s.decrypt(ctx, evt)
	if err != nil {
		room.PreviewKind = "encrypted"
		return
	}
	c, ok := dec.Content.Parsed.(*event.MessageEventContent)
	if !ok {
		return
	}
	var msg Message
	fillContent(&msg, dec.Type, c)
	room.PreviewKind = msg.Kind
	c.RemoveReplyFallback()
	room.Preview = previewText(dec.Type, c)
}

// Members lists the joined and invited members of a room, by power level
// and then name. The full list is loaded from the server the first time.
func (m *Manager) Members(ctx context.Context, roomID string) ([]Member, error) {
	s, err := m.active()
	if err != nil {
		return nil, err
	}
	rid := id.RoomID(roomID)
	if err := s.ensureMembers(ctx, rid); err != nil {
		return nil, err
	}
	all, err := s.stateStore.GetRoomMembers(ctx, rid, event.MembershipJoin, event.MembershipInvite)
	if err != nil {
		return nil, err
	}
	levels, _ := s.stateStore.GetPowerLevels(ctx, rid)
	out := make([]Member, 0, len(all))
	for uid, c := range all {
		mem := Member{UserID: uid.String(), Name: c.Displayname, Avatar: string(c.AvatarURL), Membership: string(c.Membership)}
		if mem.Name == "" {
			mem.Name = uid.Localpart()
		}
		if levels != nil {
			mem.PowerLevel = levels.GetUserLevel(uid)
		}
		out = append(out, mem)
	}
	slices.SortFunc(out, func(a, b Member) int {
		if a.PowerLevel != b.PowerLevel {
			return b.PowerLevel - a.PowerLevel
		}
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return out, nil
}

// CreateRoomRequest describes a new room.
type CreateRoomRequest struct {
	Name   string   `json:"name"`
	Topic  string   `json:"topic"`
	Invite []string `json:"invite"`
	// Encrypted turns end to end encryption on from the start. It cannot be
	// turned off later.
	Encrypted bool `json:"encrypted"`
	// Public lets anyone find and join the room.
	Public bool `json:"public"`
}

// CreateRoom creates a room and returns its id.
func (m *Manager) CreateRoom(ctx context.Context, req CreateRoomRequest) (string, error) {
	s, err := m.active()
	if err != nil {
		return "", err
	}
	create := &mautrix.ReqCreateRoom{Name: strings.TrimSpace(req.Name), Topic: strings.TrimSpace(req.Topic), Preset: "private_chat"}
	if req.Public {
		create.Preset = "public_chat"
	}
	for _, u := range req.Invite {
		uid, err := parseUserID(u)
		if err != nil {
			return "", err
		}
		create.Invite = append(create.Invite, uid)
	}
	if req.Encrypted {
		create.InitialState = append(create.InitialState, encryptionState())
	}
	resp, err := s.cli.CreateRoom(ctx, create)
	if err != nil {
		return "", fmt.Errorf("chat: create room: %w", err)
	}
	return resp.RoomID.String(), nil
}

func encryptionState() *event.Event {
	return &event.Event{
		Type:     event.StateEncryption,
		StateKey: new(string),
		Content:  event.Content{Parsed: &event.EncryptionEventContent{Algorithm: id.AlgorithmMegolmV1}},
	}
}

// OpenDirect returns the direct chat with a person, creating it when there is
// none. A new direct chat is end to end encrypted.
func (m *Manager) OpenDirect(ctx context.Context, userID string) (string, error) {
	s, err := m.active()
	if err != nil {
		return "", err
	}
	uid, err := parseUserID(userID)
	if err != nil {
		return "", err
	}
	if uid == s.cli.UserID {
		return "", errors.New("chat: that is your own Matrix ID")
	}
	directs, err := s.st.directMap(ctx)
	if err != nil {
		return "", err
	}
	for _, rid := range directs[uid] {
		if r, ok, _ := s.st.room(ctx, rid); ok && r.Membership == string(event.MembershipJoin) {
			return rid.String(), nil
		}
	}
	resp, err := s.cli.CreateRoom(ctx, &mautrix.ReqCreateRoom{
		Preset:       "trusted_private_chat",
		IsDirect:     true,
		Invite:       []id.UserID{uid},
		InitialState: []*event.Event{encryptionState()},
	})
	if err != nil {
		return "", fmt.Errorf("chat: start direct chat: %w", err)
	}
	directs[uid] = append(directs[uid], resp.RoomID)
	if err := s.cli.SetAccountData(ctx, event.AccountDataDirectChats.Type, directs); err != nil {
		return "", fmt.Errorf("chat: mark direct chat: %w", err)
	}
	if err := s.st.setDirect(ctx, directs); err != nil {
		return "", err
	}
	return resp.RoomID.String(), nil
}

// FindDirect returns the joined direct chat with a person, or empty.
func (m *Manager) FindDirect(ctx context.Context, userID string) (string, error) {
	s, err := m.active()
	if err != nil {
		return "", err
	}
	directs, err := s.st.directMap(ctx)
	if err != nil {
		return "", err
	}
	for _, rid := range directs[id.UserID(userID)] {
		if r, ok, _ := s.st.room(ctx, rid); ok && r.Membership == string(event.MembershipJoin) {
			return rid.String(), nil
		}
	}
	return "", nil
}

// joinTarget reads a room id, alias or matrix.to link, with the servers a
// link names to join through.
func joinTarget(input string) (string, []string, error) {
	input = strings.TrimSpace(input)
	var via []string
	if strings.HasPrefix(input, "https://matrix.to/#/") {
		rest := strings.TrimPrefix(input, "https://matrix.to/#/")
		target, query, _ := strings.Cut(rest, "?")
		if q, err := url.ParseQuery(query); err == nil {
			via = q["via"]
		}
		unescaped, err := url.PathUnescape(target)
		if err != nil {
			return "", nil, fmt.Errorf("chat: %q is not a room link", input)
		}
		input = unescaped
	}
	if strings.HasPrefix(input, "#") || strings.HasPrefix(input, "!") {
		if !strings.Contains(input, ":") {
			return "", nil, fmt.Errorf("chat: %q needs a server, like #room:example.org", input)
		}
		return input, via, nil
	}
	return "", nil, fmt.Errorf("chat: %q is not a room address", input)
}

// Join joins a room by id, alias or matrix.to link and returns its id. An
// invite is accepted the same way.
func (m *Manager) Join(ctx context.Context, target string) (string, error) {
	s, err := m.active()
	if err != nil {
		return "", err
	}
	room, via, err := joinTarget(target)
	if err != nil {
		return "", err
	}
	resp, err := s.cli.JoinRoom(ctx, room, &mautrix.ReqJoinRoom{Via: via})
	if err != nil {
		return "", fmt.Errorf("chat: join %s: %w", room, err)
	}
	return resp.RoomID.String(), nil
}

// Leave leaves a room, or declines an invite.
func (m *Manager) Leave(ctx context.Context, roomID string) error {
	s, err := m.active()
	if err != nil {
		return err
	}
	if _, err := s.cli.LeaveRoom(ctx, id.RoomID(roomID)); err != nil {
		return fmt.Errorf("chat: leave room: %w", err)
	}
	s.ingestMu.Lock()
	defer s.ingestMu.Unlock()
	if err := s.st.deleteRoom(ctx, id.RoomID(roomID)); err != nil {
		return err
	}
	m.emit(EventRooms, RoomsChange{RoomIDs: []string{roomID}})
	return nil
}

// Invite invites a person to a room.
func (m *Manager) Invite(ctx context.Context, roomID, userID string) error {
	s, err := m.active()
	if err != nil {
		return err
	}
	uid, err := parseUserID(userID)
	if err != nil {
		return err
	}
	if _, err := s.cli.InviteUser(ctx, id.RoomID(roomID), &mautrix.ReqInviteUser{UserID: uid}); err != nil {
		return fmt.Errorf("chat: invite: %w", err)
	}
	return nil
}

// SetFavourite adds a room to or removes it from the favourites.
func (m *Manager) SetFavourite(ctx context.Context, roomID string, favourite bool) error {
	s, err := m.active()
	if err != nil {
		return err
	}
	rid := id.RoomID(roomID)
	if favourite {
		err = s.cli.AddTag(ctx, rid, event.RoomTagFavourite, 0.5)
	} else {
		err = s.cli.RemoveTag(ctx, rid, event.RoomTagFavourite)
	}
	if err != nil {
		return fmt.Errorf("chat: change favourite: %w", err)
	}
	s.ingestMu.Lock()
	defer s.ingestMu.Unlock()
	r, ok, err := s.st.room(ctx, rid)
	if err != nil || !ok {
		return err
	}
	r.Tags = slices.DeleteFunc(r.Tags, func(t string) bool { return t == string(event.RoomTagFavourite) })
	if favourite {
		r.Tags = append(r.Tags, string(event.RoomTagFavourite))
	}
	if err := s.st.putRoom(ctx, r); err != nil {
		return err
	}
	m.emit(EventRooms, RoomsChange{RoomIDs: []string{roomID}})
	return nil
}

// Profile is a person's public name and picture.
type Profile struct {
	UserID string `json:"userId"`
	Name   string `json:"name"`
	Avatar string `json:"avatar"`
}

// LookupProfile reads a person's profile from their homeserver, to show who a
// Matrix ID belongs to before starting a chat.
func (m *Manager) LookupProfile(ctx context.Context, userID string) (Profile, error) {
	s, err := m.active()
	if err != nil {
		return Profile{}, err
	}
	uid, err := parseUserID(userID)
	if err != nil {
		return Profile{}, err
	}
	resp, err := s.cli.GetProfile(ctx, uid)
	if err != nil {
		return Profile{}, fmt.Errorf("chat: look up %s: %w", uid, err)
	}
	p := Profile{UserID: uid.String(), Name: resp.DisplayName}
	if !resp.AvatarURL.IsEmpty() {
		p.Avatar = resp.AvatarURL.String()
	}
	return p, nil
}

var (
	timeNow   = time.Now
	timeSince = time.Since
)

// keyRequestInterval is how often one missing room key is asked for at most.
const keyRequestInterval = 10 * time.Minute
