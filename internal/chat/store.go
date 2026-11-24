package chat

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.mau.fi/util/dbutil"
	_ "modernc.org/sqlite"

	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

// store is the chat database of one account: the rooms and timeline events
// Pelton shows, next to the state and crypto tables mautrix keeps in the same
// file. It never holds the access token or the pickle key, and encrypted
// events are kept as they arrived, so message text from an encrypted room is
// not written to disk in the clear.
type store struct {
	db  *sql.DB
	mdb *dbutil.Database
}

// schema creates the tables this package owns. They carry a pc_ prefix so they
// can never meet the tables mautrix creates in the same database.
const schema = `
CREATE TABLE IF NOT EXISTS pc_account (
	id INTEGER PRIMARY KEY CHECK (id = 1),
	homeserver TEXT NOT NULL,
	user_id TEXT NOT NULL,
	device_id TEXT NOT NULL,
	display_name TEXT NOT NULL DEFAULT '',
	avatar TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS pc_rooms (
	room_id TEXT PRIMARY KEY,
	membership TEXT NOT NULL DEFAULT 'join',
	name TEXT NOT NULL DEFAULT '',
	topic TEXT NOT NULL DEFAULT '',
	avatar TEXT NOT NULL DEFAULT '',
	canonical_alias TEXT NOT NULL DEFAULT '',
	is_space INTEGER NOT NULL DEFAULT 0,
	encrypted INTEGER NOT NULL DEFAULT 0,
	heroes TEXT NOT NULL DEFAULT '[]',
	joined_count INTEGER NOT NULL DEFAULT 0,
	invited_count INTEGER NOT NULL DEFAULT 0,
	notif_count INTEGER NOT NULL DEFAULT 0,
	highlight_count INTEGER NOT NULL DEFAULT 0,
	tags TEXT NOT NULL DEFAULT '[]',
	fully_read TEXT NOT NULL DEFAULT '',
	prev_batch TEXT NOT NULL DEFAULT '',
	history_complete INTEGER NOT NULL DEFAULT 0,
	inviter TEXT NOT NULL DEFAULT '',
	invite_direct INTEGER NOT NULL DEFAULT 0,
	last_event TEXT NOT NULL DEFAULT '',
	last_ts INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS pc_events (
	room_id TEXT NOT NULL,
	event_id TEXT NOT NULL,
	seq INTEGER NOT NULL,
	ts INTEGER NOT NULL,
	sender TEXT NOT NULL,
	type TEXT NOT NULL,
	state_key TEXT,
	rel_type TEXT NOT NULL DEFAULT '',
	relates_to TEXT NOT NULL DEFAULT '',
	redacted_by TEXT NOT NULL DEFAULT '',
	raw TEXT NOT NULL,
	PRIMARY KEY (room_id, event_id)
);
CREATE INDEX IF NOT EXISTS pc_events_seq ON pc_events (room_id, seq);
CREATE INDEX IF NOT EXISTS pc_events_rel ON pc_events (room_id, relates_to);
CREATE TABLE IF NOT EXISTS pc_receipts (
	room_id TEXT NOT NULL,
	user_id TEXT NOT NULL,
	event_id TEXT NOT NULL,
	ts INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (room_id, user_id)
);
CREATE TABLE IF NOT EXISTS pc_space_children (
	space_id TEXT NOT NULL,
	child_id TEXT NOT NULL,
	PRIMARY KEY (space_id, child_id)
);
CREATE TABLE IF NOT EXISTS pc_direct (
	user_id TEXT NOT NULL,
	room_id TEXT NOT NULL,
	PRIMARY KEY (user_id, room_id)
);
`

// openStore opens (creating it if needed) the chat database at path.
func openStore(path string) (*store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("chat: create data dir: %w", err)
	}
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(15000)&_txlock=immediate", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("chat: open database: %w", err)
	}
	db.SetMaxOpenConns(4)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("chat: create tables: %w", err)
	}
	mdb, err := dbutil.NewWithDB(db, "sqlite3")
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("chat: wrap database: %w", err)
	}
	return &store{db: db, mdb: mdb}, nil
}

func (s *store) close() error {
	return s.db.Close()
}

// accountRow is the non-secret half of a signed-in account.
type accountRow struct {
	Homeserver  string
	UserID      id.UserID
	DeviceID    id.DeviceID
	DisplayName string
	Avatar      string
}

func (s *store) saveAccount(ctx context.Context, a accountRow) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO pc_account (id, homeserver, user_id, device_id, display_name, avatar) VALUES (1, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET homeserver=excluded.homeserver, user_id=excluded.user_id,
			device_id=excluded.device_id, display_name=excluded.display_name, avatar=excluded.avatar`,
		a.Homeserver, a.UserID, a.DeviceID, a.DisplayName, a.Avatar)
	return err
}

// loadAccount returns the stored account, or ok false when there is none.
func (s *store) loadAccount(ctx context.Context) (accountRow, bool, error) {
	var a accountRow
	err := s.db.QueryRowContext(ctx, `SELECT homeserver, user_id, device_id, display_name, avatar FROM pc_account WHERE id = 1`).
		Scan(&a.Homeserver, &a.UserID, &a.DeviceID, &a.DisplayName, &a.Avatar)
	if errors.Is(err, sql.ErrNoRows) {
		return a, false, nil
	}
	return a, err == nil, err
}

func (s *store) setProfile(ctx context.Context, name, avatar string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE pc_account SET display_name = ?, avatar = ? WHERE id = 1`, name, avatar)
	return err
}

// roomRow is one room as stored.
type roomRow struct {
	ID             id.RoomID
	Membership     string
	Name           string
	Topic          string
	Avatar         string
	CanonicalAlias string
	IsSpace        bool
	Encrypted      bool
	Heroes         []id.UserID
	JoinedCount    int
	InvitedCount   int
	NotifCount     int
	HighlightCount int
	Tags           []string
	FullyRead      id.EventID
	PrevBatch      string
	HistoryDone    bool
	Inviter        id.UserID
	InviteDirect   bool
	LastEvent      id.EventID
	LastTS         int64
}

const roomColumns = `room_id, membership, name, topic, avatar, canonical_alias, is_space, encrypted, heroes,
	joined_count, invited_count, notif_count, highlight_count, tags, fully_read, prev_batch, history_complete,
	inviter, invite_direct, last_event, last_ts`

type scanner interface {
	Scan(dest ...any) error
}

func scanRoom(row scanner) (roomRow, error) {
	var r roomRow
	var heroes, tags string
	err := row.Scan(&r.ID, &r.Membership, &r.Name, &r.Topic, &r.Avatar, &r.CanonicalAlias, &r.IsSpace, &r.Encrypted,
		&heroes, &r.JoinedCount, &r.InvitedCount, &r.NotifCount, &r.HighlightCount, &tags, &r.FullyRead,
		&r.PrevBatch, &r.HistoryDone, &r.Inviter, &r.InviteDirect, &r.LastEvent, &r.LastTS)
	if err != nil {
		return r, err
	}
	_ = json.Unmarshal([]byte(heroes), &r.Heroes)
	_ = json.Unmarshal([]byte(tags), &r.Tags)
	return r, nil
}

func (s *store) rooms(ctx context.Context) ([]roomRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+roomColumns+` FROM pc_rooms`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []roomRow
	for rows.Next() {
		r, err := scanRoom(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// room returns one room, or ok false when it is not known.
func (s *store) room(ctx context.Context, roomID id.RoomID) (roomRow, bool, error) {
	r, err := scanRoom(s.db.QueryRowContext(ctx, `SELECT `+roomColumns+` FROM pc_rooms WHERE room_id = ?`, roomID))
	if errors.Is(err, sql.ErrNoRows) {
		return r, false, nil
	}
	return r, err == nil, err
}

func (s *store) putRoom(ctx context.Context, r roomRow) error {
	heroes, _ := json.Marshal(nonNil(r.Heroes))
	tags, _ := json.Marshal(nonNil(r.Tags))
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO pc_rooms (`+roomColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (room_id) DO UPDATE SET membership=excluded.membership, name=excluded.name, topic=excluded.topic,
			avatar=excluded.avatar, canonical_alias=excluded.canonical_alias, is_space=excluded.is_space,
			encrypted=excluded.encrypted, heroes=excluded.heroes, joined_count=excluded.joined_count,
			invited_count=excluded.invited_count, notif_count=excluded.notif_count,
			highlight_count=excluded.highlight_count, tags=excluded.tags, fully_read=excluded.fully_read,
			prev_batch=excluded.prev_batch, history_complete=excluded.history_complete, inviter=excluded.inviter,
			invite_direct=excluded.invite_direct, last_event=excluded.last_event, last_ts=excluded.last_ts`,
		r.ID, r.Membership, r.Name, r.Topic, r.Avatar, r.CanonicalAlias, r.IsSpace, r.Encrypted, string(heroes),
		r.JoinedCount, r.InvitedCount, r.NotifCount, r.HighlightCount, string(tags), r.FullyRead, r.PrevBatch,
		r.HistoryDone, r.Inviter, r.InviteDirect, r.LastEvent, r.LastTS)
	return err
}

// deleteRoom forgets a room and everything stored for it.
func (s *store) deleteRoom(ctx context.Context, roomID id.RoomID) error {
	for _, q := range []string{
		`DELETE FROM pc_events WHERE room_id = ?`,
		`DELETE FROM pc_receipts WHERE room_id = ?`,
		`DELETE FROM pc_space_children WHERE space_id = ?`,
		`DELETE FROM pc_rooms WHERE room_id = ?`,
	} {
		if _, err := s.db.ExecContext(ctx, q, roomID); err != nil {
			return err
		}
	}
	return nil
}

// clearTimeline drops the stored events of a room. A sync that reports a gap
// replaces them, since events on both sides of a gap would read as one
// conversation.
func (s *store) clearTimeline(ctx context.Context, roomID id.RoomID) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM pc_events WHERE room_id = ?`, roomID)
	return err
}

// eventRow is one timeline event as stored. Raw is the event JSON exactly as
// the server sent it, still encrypted when it was.
type eventRow struct {
	RoomID     id.RoomID
	EventID    id.EventID
	Seq        int64
	TS         int64
	Sender     id.UserID
	Type       string
	StateKey   *string
	RelType    string
	RelatesTo  id.EventID
	RedactedBy id.EventID
	Raw        []byte
}

const eventColumns = `room_id, event_id, seq, ts, sender, type, state_key, rel_type, relates_to, redacted_by, raw`

func scanEvent(row scanner) (eventRow, error) {
	var e eventRow
	var raw string
	var stateKey sql.NullString
	err := row.Scan(&e.RoomID, &e.EventID, &e.Seq, &e.TS, &e.Sender, &e.Type, &stateKey, &e.RelType, &e.RelatesTo, &e.RedactedBy, &raw)
	if stateKey.Valid {
		e.StateKey = &stateKey.String
	}
	e.Raw = []byte(raw)
	return e, err
}

// seqBounds returns the lowest and highest sequence numbers stored for a room.
func (s *store) seqBounds(ctx context.Context, roomID id.RoomID) (lo, hi int64, err error) {
	var nlo, nhi sql.NullInt64
	err = s.db.QueryRowContext(ctx, `SELECT MIN(seq), MAX(seq) FROM pc_events WHERE room_id = ?`, roomID).Scan(&nlo, &nhi)
	return nlo.Int64, nhi.Int64, err
}

// insertEvent stores one event. It reports false when the event was already
// there, which a sync replayed after a crash or a page that overlaps the
// stored timeline both produce.
func (s *store) insertEvent(ctx context.Context, e eventRow) (bool, error) {
	var stateKey any
	if e.StateKey != nil {
		stateKey = *e.StateKey
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO pc_events (`+eventColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (room_id, event_id) DO NOTHING`,
		e.RoomID, e.EventID, e.Seq, e.TS, e.Sender, e.Type, stateKey, e.RelType, e.RelatesTo, e.RedactedBy, string(e.Raw))
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// markRedacted records that an event was redacted, dropping its content. The
// redaction itself stays as its own row, so a redaction that arrives before
// the event it removes (backwards pagination) still applies.
func (s *store) markRedacted(ctx context.Context, roomID id.RoomID, target, by id.EventID) error {
	_, err := s.db.ExecContext(ctx, `UPDATE pc_events SET redacted_by = ? WHERE room_id = ? AND event_id = ?`, by, roomID, target)
	return err
}

// redactionOf returns the redaction already stored for an event, if any.
func (s *store) redactionOf(ctx context.Context, roomID id.RoomID, target id.EventID) (id.EventID, error) {
	var by id.EventID
	err := s.db.QueryRowContext(ctx, `SELECT event_id FROM pc_events WHERE room_id = ? AND type = 'm.room.redaction' AND relates_to = ? LIMIT 1`, roomID, target).Scan(&by)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return by, err
}

func (s *store) event(ctx context.Context, roomID id.RoomID, eventID id.EventID) (eventRow, bool, error) {
	e, err := scanEvent(s.db.QueryRowContext(ctx, `SELECT `+eventColumns+` FROM pc_events WHERE room_id = ? AND event_id = ?`, roomID, eventID))
	if errors.Is(err, sql.ErrNoRows) {
		return e, false, nil
	}
	return e, err == nil, err
}

// timelineTypes are the event types that show as rows in a timeline. Edits,
// reactions and redactions are stored too but fold into the row they change.
var timelineTypes = []string{
	event.EventMessage.Type, event.EventEncrypted.Type, event.EventSticker.Type,
	event.StateMember.Type, event.StateRoomName.Type, event.StateTopic.Type, event.StateRoomAvatar.Type,
	event.StateEncryption.Type, event.StateCreate.Type,
}

func typeList() string {
	quoted := make([]string, len(timelineTypes))
	for i, t := range timelineTypes {
		quoted[i] = "'" + t + "'"
	}
	return strings.Join(quoted, ",")
}

// page returns up to limit timeline rows older than beforeSeq (the newest
// when it is nil), newest first.
func (s *store) page(ctx context.Context, roomID id.RoomID, beforeSeq *int64, limit int) ([]eventRow, error) {
	q := `SELECT ` + eventColumns + ` FROM pc_events WHERE room_id = ? AND type IN (` + typeList() + `) AND rel_type NOT IN ('m.replace', 'm.annotation')`
	args := []any{roomID}
	if beforeSeq != nil {
		q += ` AND seq < ?`
		args = append(args, *beforeSeq)
	}
	q += ` ORDER BY seq DESC LIMIT ?`
	args = append(args, limit)
	return s.queryEvents(ctx, q, args...)
}

// relations returns every stored event that relates to one of targets.
func (s *store) relations(ctx context.Context, roomID id.RoomID, targets []id.EventID) ([]eventRow, error) {
	if len(targets) == 0 {
		return nil, nil
	}
	args := []any{roomID}
	marks := make([]string, len(targets))
	for i, t := range targets {
		marks[i] = "?"
		args = append(args, t)
	}
	q := `SELECT ` + eventColumns + ` FROM pc_events WHERE room_id = ? AND relates_to IN (` + strings.Join(marks, ",") + `) ORDER BY seq`
	return s.queryEvents(ctx, q, args...)
}

// latestMessage returns the newest message-like event of a room, for the
// room list preview.
func (s *store) latestMessage(ctx context.Context, roomID id.RoomID) (eventRow, bool, error) {
	e, err := scanEvent(s.db.QueryRowContext(ctx, `SELECT `+eventColumns+` FROM pc_events
		WHERE room_id = ? AND type IN ('m.room.message', 'm.room.encrypted', 'm.sticker') AND rel_type NOT IN ('m.replace', 'm.annotation')
		ORDER BY seq DESC LIMIT 1`, roomID))
	if errors.Is(err, sql.ErrNoRows) {
		return e, false, nil
	}
	return e, err == nil, err
}

func (s *store) queryEvents(ctx context.Context, q string, args ...any) ([]eventRow, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []eventRow
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *store) putReceipt(ctx context.Context, roomID id.RoomID, userID id.UserID, eventID id.EventID, ts int64) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO pc_receipts (room_id, user_id, event_id, ts) VALUES (?, ?, ?, ?)
		ON CONFLICT (room_id, user_id) DO UPDATE SET event_id=excluded.event_id, ts=excluded.ts
		WHERE excluded.ts >= pc_receipts.ts`, roomID, userID, eventID, ts)
	return err
}

// receipts returns the event each user has read up to in a room.
func (s *store) receipts(ctx context.Context, roomID id.RoomID) (map[id.UserID]id.EventID, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT user_id, event_id FROM pc_receipts WHERE room_id = ?`, roomID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[id.UserID]id.EventID{}
	for rows.Next() {
		var u id.UserID
		var e id.EventID
		if err := rows.Scan(&u, &e); err != nil {
			return nil, err
		}
		out[u] = e
	}
	return out, rows.Err()
}

func (s *store) setSpaceChild(ctx context.Context, space, child id.RoomID, present bool) error {
	var err error
	if present {
		_, err = s.db.ExecContext(ctx, `INSERT INTO pc_space_children (space_id, child_id) VALUES (?, ?) ON CONFLICT DO NOTHING`, space, child)
	} else {
		_, err = s.db.ExecContext(ctx, `DELETE FROM pc_space_children WHERE space_id = ? AND child_id = ?`, space, child)
	}
	return err
}

// spaceParents maps each room to the spaces that list it.
func (s *store) spaceParents(ctx context.Context) (map[id.RoomID][]id.RoomID, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT space_id, child_id FROM pc_space_children`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[id.RoomID][]id.RoomID{}
	for rows.Next() {
		var sp, ch id.RoomID
		if err := rows.Scan(&sp, &ch); err != nil {
			return nil, err
		}
		out[ch] = append(out[ch], sp)
	}
	return out, rows.Err()
}

// setDirect replaces the m.direct map: which rooms are direct chats with whom.
func (s *store) setDirect(ctx context.Context, direct map[id.UserID][]id.RoomID) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM pc_direct`); err != nil {
		return err
	}
	for user, rooms := range direct {
		for _, room := range rooms {
			if _, err := tx.ExecContext(ctx, `INSERT INTO pc_direct (user_id, room_id) VALUES (?, ?) ON CONFLICT DO NOTHING`, user, room); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// directs maps each direct chat room to the person it is with.
func (s *store) directs(ctx context.Context) (map[id.RoomID]id.UserID, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT user_id, room_id FROM pc_direct`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[id.RoomID]id.UserID{}
	for rows.Next() {
		var u id.UserID
		var r id.RoomID
		if err := rows.Scan(&u, &r); err != nil {
			return nil, err
		}
		out[r] = u
	}
	return out, rows.Err()
}

// directMap is the m.direct content as stored, for writing it back with one
// room added.
func (s *store) directMap(ctx context.Context) (map[id.UserID][]id.RoomID, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT user_id, room_id FROM pc_direct ORDER BY user_id, room_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[id.UserID][]id.RoomID{}
	for rows.Next() {
		var u id.UserID
		var r id.RoomID
		if err := rows.Scan(&u, &r); err != nil {
			return nil, err
		}
		out[u] = append(out[u], r)
	}
	return out, rows.Err()
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
