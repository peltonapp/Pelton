package storage

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func newTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := openMigrated(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.RunMigrations(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestViewCRUD(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := context.Background()

	v := &View{
		Name:        "Invoices",
		Icon:        "file-invoice",
		Color:       "amber",
		QueryText:   "invoice",
		QueryFrom:   "acme.com",
		WithinDays:  30,
		UnreadOnly:  true,
		FlaggedOnly: false,
		AccountID:   0,
	}
	id, err := db.CreateView(ctx, v)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if id == 0 || v.Position != 1 {
		t.Fatalf("expected id and position 1, got id=%d pos=%d", id, v.Position)
	}

	got, err := db.GetView(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "Invoices" || got.QueryText != "invoice" || !got.UnreadOnly || got.WithinDays != 30 {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if got.FlaggedOnly {
		t.Fatalf("flaggedOnly should be false")
	}

	got.Name = "Bills"
	got.UnreadOnly = false
	got.HasAttachment = true
	if err := db.UpdateView(ctx, got); err != nil {
		t.Fatalf("update: %v", err)
	}
	reloaded, err := db.GetView(ctx, id)
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if reloaded.Name != "Bills" || reloaded.UnreadOnly || !reloaded.HasAttachment {
		t.Fatalf("update not persisted: %+v", reloaded)
	}

	if err := db.DeleteView(ctx, id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := db.GetView(ctx, id); !errors.Is(err, ErrViewNotFound) {
		t.Fatalf("expected ErrViewNotFound, got %v", err)
	}
}

func TestViewReorder(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := context.Background()

	var ids []int64
	for _, name := range []string{"A", "B", "C"} {
		v := &View{Name: name}
		id, err := db.CreateView(ctx, v)
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		ids = append(ids, id)
	}

	// reverse the order.
	reversed := []int64{ids[2], ids[1], ids[0]}
	if err := db.SetViewPositions(ctx, reversed); err != nil {
		t.Fatalf("reorder: %v", err)
	}

	list, err := db.ListViews(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 views, got %d", len(list))
	}
	if list[0].ID != ids[2] || list[1].ID != ids[1] || list[2].ID != ids[0] {
		t.Fatalf("order not applied: %d,%d,%d", list[0].ID, list[1].ID, list[2].ID)
	}
}
