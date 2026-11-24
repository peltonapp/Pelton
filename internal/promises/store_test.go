package promises

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/storage"
)

func testStore(t *testing.T) (*Store, int64) {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.RunMigrations(context.Background()); err != nil {
		t.Fatal(err)
	}
	id, err := db.CreateAccount(context.Background(), &storage.Account{DisplayName: "Me", Email: "me@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	return NewStore(db.Handle()), id
}

func TestStoreKeepsOneRowPerSentence(t *testing.T) {
	s, acc := testStore(t)
	ctx := context.Background()
	p := Promise{AccountID: acc, MailID: "m1@example.com", Direction: Theirs, PersonEmail: "anna@example.com",
		Title: "Send the contract", Quote: "I'll send the contract by Friday.", Due: "2026-11-20", Source: "rules", MailDate: time.Now()}
	added, err := s.Add(ctx, &p)
	if err != nil || !added || p.ID == 0 {
		t.Fatalf("add: %v %v %d", added, err, p.ID)
	}
	p.State = StateDismissed
	if err := s.Save(ctx, p); err != nil {
		t.Fatal(err)
	}
	again := Promise{AccountID: acc, MailID: "m1@example.com", Direction: Theirs, Title: "x", Quote: "I'll send  the contract\nby Friday.", Source: "rules"}
	added, err = s.Add(ctx, &again)
	if err != nil || added || again.ID != p.ID {
		t.Fatalf("a resync added a second row: %v %v %d", added, err, again.ID)
	}
	got, err := s.Get(ctx, p.ID)
	if err != nil || got.State != StateDismissed {
		t.Fatalf("a dismissed promise came back: %+v %v", got, err)
	}
	if ok, _ := s.Scanned(ctx, acc, "m1@example.com"); ok {
		t.Fatal("scanned before it was marked")
	}
	if err := s.MarkScanned(ctx, acc, "m1@example.com", "rules"); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkScanned(ctx, acc, "m1@example.com", "ai"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.Scanned(ctx, acc, "m1@example.com"); !ok {
		t.Fatal("not marked scanned")
	}
}

func TestStoreQuietSender(t *testing.T) {
	s, acc := testStore(t)
	ctx := context.Background()
	for i, q := range []string{"Can you send it by Friday?", "I'll call you tomorrow."} {
		p := Promise{AccountID: acc, MailID: "m@example.com", Direction: Mine, PersonEmail: "Anna@Example.com", Title: q, Quote: q, Source: "rules"}
		if _, err := s.Add(ctx, &p); err != nil {
			t.Fatal(i, err)
		}
	}
	other := Promise{AccountID: acc, MailID: "n@example.com", Direction: Mine, PersonEmail: "bob@example.com", Title: "x", Quote: "x", Source: "rules"}
	if _, err := s.Add(ctx, &other); err != nil {
		t.Fatal(err)
	}
	ids, err := s.Quiet(ctx, "anna@example.com")
	if err != nil || len(ids) != 2 {
		t.Fatalf("quiet: %v %v", ids, err)
	}
	if q, _ := s.IsQuiet(ctx, "ANNA@example.com"); !q {
		t.Fatal("not quiet")
	}
	if left, _ := s.List(ctx, StateSuggested); len(left) != 1 || left[0].ID != other.ID {
		t.Fatalf("left %+v", left)
	}
	if err := s.Unquiet(ctx, "anna@example.com", ids); err != nil {
		t.Fatal(err)
	}
	if left, _ := s.List(ctx, StateSuggested); len(left) != 3 {
		t.Fatalf("undo left %d suggested", len(left))
	}
}
