package chat

import (
	"context"
	"path/filepath"
	"testing"

	"go.mau.fi/util/dbutil"
	"maunium.net/go/mautrix/crypto"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
	"maunium.net/go/mautrix/sqlstatestore"
)

func openTestStore(t *testing.T) *store {
	t.Helper()
	s, err := openStore(filepath.Join(t.TempDir(), "chat.db"))
	if err != nil {
		t.Fatalf("openStore: %v", err)
	}
	t.Cleanup(func() { s.close() })
	return s
}

// The mautrix stores have to run their migrations on the pure Go driver the
// app already ships, since cgo sqlite is not linked in.
func TestMautrixStoresUpgradeOnTheAppDriver(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	state := sqlstatestore.NewSQLStateStore(s.mdb, dbutil.NoopLogger, false)
	if err := state.Upgrade(ctx); err != nil {
		t.Fatalf("state store upgrade: %v", err)
	}
	cs := crypto.NewSQLCryptoStore(s.mdb, dbutil.NoopLogger, "acc", "DEVICE", []byte("pickle key for the test"))
	if err := cs.DB.Upgrade(ctx); err != nil {
		t.Fatalf("crypto store upgrade: %v", err)
	}

	if err := cs.PutSecret(ctx, id.SecretMegolmBackupV1, "secret value"); err != nil {
		t.Fatalf("PutSecret: %v", err)
	}
	got, err := cs.GetSecret(ctx, id.SecretMegolmBackupV1)
	if err != nil || got != "secret value" {
		t.Fatalf("GetSecret = %q, %v", got, err)
	}

	room := id.RoomID("!r:example.org")
	if err := state.SetMembership(ctx, room, "@a:example.org", event.MembershipJoin); err != nil {
		t.Fatalf("SetMembership: %v", err)
	}
	members, err := state.GetRoomJoinedOrInvitedMembers(ctx, room)
	if err != nil || len(members) != 1 {
		t.Fatalf("members = %v, %v", members, err)
	}
	if err := cs.PutNextBatch(ctx, "s123"); err != nil {
		t.Fatalf("PutNextBatch: %v", err)
	}
	if nb, err := cs.GetNextBatch(ctx); err != nil || nb != "s123" {
		t.Fatalf("GetNextBatch = %q, %v", nb, err)
	}
}

func TestStoreRoomsAndEvents(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	room := id.RoomID("!r:example.org")

	if err := s.putRoom(ctx, roomRow{ID: room, Membership: "join", Name: "Garden", Tags: []string{"m.favourite"}}); err != nil {
		t.Fatalf("putRoom: %v", err)
	}
	got, ok, err := s.room(ctx, room)
	if err != nil || !ok || got.Name != "Garden" || len(got.Tags) != 1 {
		t.Fatalf("room = %+v, %v, %v", got, ok, err)
	}

	for i, eid := range []id.EventID{"$a", "$b", "$c"} {
		inserted, err := s.insertEvent(ctx, eventRow{RoomID: room, EventID: eid, Seq: int64(i + 1), TS: int64(i), Sender: "@a:x", Type: "m.room.message", Raw: []byte(`{}`)})
		if err != nil || !inserted {
			t.Fatalf("insertEvent %s: %v %v", eid, inserted, err)
		}
	}
	if inserted, _ := s.insertEvent(ctx, eventRow{RoomID: room, EventID: "$b", Seq: 9, Type: "m.room.message", Raw: []byte(`{}`)}); inserted {
		t.Fatal("a duplicate event was inserted twice")
	}
	if _, err := s.insertEvent(ctx, eventRow{RoomID: room, EventID: "$edit", Seq: 4, Type: "m.room.message", RelType: "m.replace", RelatesTo: "$a", Raw: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}

	page, err := s.page(ctx, room, nil, 2)
	if err != nil || len(page) != 2 || page[0].EventID != "$c" || page[1].EventID != "$b" {
		t.Fatalf("first page = %+v, %v", page, err)
	}
	older, err := s.page(ctx, room, &page[1].Seq, 10)
	if err != nil || len(older) != 1 || older[0].EventID != "$a" {
		t.Fatalf("older page = %+v, %v", older, err)
	}
	rels, err := s.relations(ctx, room, []id.EventID{"$a"})
	if err != nil || len(rels) != 1 || rels[0].EventID != "$edit" {
		t.Fatalf("relations = %+v, %v", rels, err)
	}
	lo, hi, err := s.seqBounds(ctx, room)
	if err != nil || lo != 1 || hi != 4 {
		t.Fatalf("seqBounds = %d %d %v", lo, hi, err)
	}

	if err := s.deleteRoom(ctx, room); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.room(ctx, room); ok {
		t.Fatal("room survived deleteRoom")
	}
	if page, _ := s.page(ctx, room, nil, 10); len(page) != 0 {
		t.Fatal("events survived deleteRoom")
	}
}

func TestStoreDirectAndReceipts(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	if err := s.setDirect(ctx, map[id.UserID][]id.RoomID{"@b:x": {"!dm:x"}}); err != nil {
		t.Fatal(err)
	}
	d, err := s.directs(ctx)
	if err != nil || d["!dm:x"] != "@b:x" {
		t.Fatalf("directs = %v, %v", d, err)
	}
	if err := s.putReceipt(ctx, "!dm:x", "@b:x", "$2", 20); err != nil {
		t.Fatal(err)
	}
	// an older receipt arriving late must not move the marker back.
	if err := s.putReceipt(ctx, "!dm:x", "@b:x", "$1", 10); err != nil {
		t.Fatal(err)
	}
	r, err := s.receipts(ctx, "!dm:x")
	if err != nil || r["@b:x"] != "$2" {
		t.Fatalf("receipts = %v, %v", r, err)
	}
}
