package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

// Device is one of the user's sessions.
type Device struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// LastSeen is the last activity the server saw, in Unix milliseconds.
	LastSeen int64 `json:"lastSeen"`
	// Current is true for the session this app runs.
	Current bool `json:"current"`
	// Verified is true when the device is signed by the user's own
	// cross-signing key.
	Verified bool `json:"verified"`
}

// Devices lists the user's sessions, this one first.
func (m *Manager) Devices(ctx context.Context) ([]Device, error) {
	s, err := m.active()
	if err != nil {
		return nil, err
	}
	resp, err := s.cli.GetDevicesInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("chat: list sessions: %w", err)
	}
	keys, _ := s.mach.FetchKeys(ctx, []id.UserID{s.cli.UserID}, true)
	out := make([]Device, 0, len(resp.Devices))
	for _, d := range resp.Devices {
		dev := Device{ID: d.DeviceID.String(), Name: d.DisplayName, LastSeen: d.LastSeenTS, Current: d.DeviceID == s.cli.DeviceID}
		known := keys[s.cli.UserID][d.DeviceID]
		if known == nil {
			known, _ = s.cryptoStore.GetDevice(ctx, s.cli.UserID, d.DeviceID)
		}
		if known != nil {
			if trust, err := s.mach.ResolveTrustContext(ctx, known); err == nil {
				dev.Verified = trust >= id.TrustStateCrossSignedTOFU
			}
		}
		out = append(out, dev)
	}
	slices.SortStableFunc(out, func(a, b Device) int {
		switch {
		case a.Current:
			return -1
		case b.Current:
			return 1
		case a.LastSeen > b.LastSeen:
			return -1
		case a.LastSeen < b.LastSeen:
			return 1
		}
		return 0
	})
	return out, nil
}

// ErrPasswordRequired is returned when the server wants the account password
// to confirm an action.
var ErrPasswordRequired = errors.New("chat: the server wants your account password to confirm this")

// SignOutDevice ends another of the user's sessions. The server usually asks
// for the account password first.
func (m *Manager) SignOutDevice(ctx context.Context, deviceID, password string) error {
	s, err := m.active()
	if err != nil {
		return err
	}
	if id.DeviceID(deviceID) == s.cli.DeviceID {
		return errors.New("chat: sign out of this device from its own settings")
	}
	err = s.cli.DeleteDevice(ctx, id.DeviceID(deviceID), &mautrix.ReqDeleteDevice[any]{})
	var httpErr mautrix.HTTPError
	if err == nil || !errors.As(err, &httpErr) || !httpErr.IsStatus(http.StatusUnauthorized) {
		return err
	}
	var uia mautrix.RespUserInteractive
	if jerr := json.Unmarshal([]byte(httpErr.ResponseBody), &uia); jerr != nil || uia.Session == "" {
		return err
	}
	if password == "" {
		return ErrPasswordRequired
	}
	err = s.cli.DeleteDevice(ctx, id.DeviceID(deviceID), &mautrix.ReqDeleteDevice[any]{Auth: &mautrix.ReqUIAuthLogin{
		BaseAuthData: mautrix.BaseAuthData{Type: mautrix.AuthTypePassword, Session: uia.Session},
		User:         s.cli.UserID.String(),
		Password:     password,
	}})
	if err != nil {
		return fmt.Errorf("chat: sign out session: %w", err)
	}
	return nil
}

// UserTrust tells whether the user has verified a person.
func (m *Manager) UserTrust(ctx context.Context, userID string) (bool, error) {
	s, err := m.active()
	if err != nil {
		return false, err
	}
	uid, err := parseUserID(userID)
	if err != nil {
		return false, err
	}
	if _, err := s.mach.FetchKeys(ctx, []id.UserID{uid}, false); err != nil {
		return false, err
	}
	return s.mach.IsUserTrusted(ctx, uid)
}

// notify hands new messages to Options.Notify with what decides whether they
// raise a notification: a direct chat, or a mention of the user.
func (s *session) notify(ctx context.Context, list []incomingMessage) {
	if s.m.opts.Notify == nil {
		return
	}
	directs, _ := s.st.directs(ctx)
	out := make([]Notification, 0, len(list))
	for _, in := range list {
		n := Notification{RoomID: in.room.String(), EventID: in.evt.ID.String()}
		_, n.Direct = directs[in.room]
		r, _, _ := s.st.room(ctx, in.room)
		room := s.toRoom(ctx, r, directs, nil)
		n.RoomName = room.Name
		n.Sender = s.member(ctx, in.room, in.evt.Sender).Name
		if dec, err := s.decrypt(ctx, in.evt); err == nil {
			if c, ok := dec.Content.Parsed.(*event.MessageEventContent); ok {
				stripReplyFallback(c)
				n.Body = previewText(dec.Type, c)
				n.Mention = s.mentions(c)
			}
		}
		out = append(out, n)
	}
	s.m.opts.Notify(out)
}

// incomingMessage is a new message from someone else, found in a sync.
type incomingMessage struct {
	room id.RoomID
	evt  *event.Event
}

func jsonMarshal(v any) (json.RawMessage, error) {
	return json.Marshal(v)
}
