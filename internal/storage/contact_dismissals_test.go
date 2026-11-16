package storage

import (
	"context"
	"testing"
)

func TestContactKeyUsesUIDThenPath(t *testing.T) {
	withUID := Contact{BookID: 2, UID: "u1", Path: "/a.vcf"}
	if got := ContactKey(withUID); got != "uid:2:u1" {
		t.Errorf("key = %q", got)
	}
	moved := withUID
	moved.Path = "/elsewhere.vcf"
	if ContactKey(moved) != ContactKey(withUID) {
		t.Error("a contact that moved paths must keep its key")
	}
	if got := ContactKey(Contact{BookID: 2, Path: "/a.vcf"}); got != "path:2:/a.vcf" {
		t.Errorf("key without a uid = %q", got)
	}
}

func TestDismissedContactPairsRoundTrip(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	pair := NewContactPair("uid:1:b", "uid:1:a")
	if pair != NewContactPair("uid:1:a", "uid:1:b") {
		t.Fatal("a pair must not depend on the order it was named in")
	}
	if err := db.DismissContactPairs(ctx, []ContactPair{pair, pair}); err != nil {
		t.Fatal(err)
	}
	got, err := db.DismissedContactPairs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !got[pair] {
		t.Fatalf("pairs = %v", got)
	}

	n, err := db.ResetDismissedContactPairs(ctx)
	if err != nil || n != 1 {
		t.Fatalf("reset = %d, %v", n, err)
	}
	if got, _ := db.DismissedContactPairs(ctx); len(got) != 0 {
		t.Errorf("pairs after reset = %v", got)
	}
}

// a full resync empties a book and writes its contacts back with new row ids,
// so a dismissal has to be keyed by something that survives that.
func TestDismissalSurvivesAFullResync(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	book := newBook(t, db, "personal")
	a := saveContact(t, db, book.ID, "/a.vcf", "Ada")
	b := saveContact(t, db, book.ID, "/b.vcf", "Ada L")
	if err := db.DismissContactPairs(ctx, []ContactPair{NewContactPair(ContactKey(a), ContactKey(b))}); err != nil {
		t.Fatal(err)
	}

	if err := db.ClearBookContacts(ctx, book.ID); err != nil {
		t.Fatal(err)
	}
	a2 := saveContact(t, db, book.ID, "/a.vcf", "Ada")
	b2 := saveContact(t, db, book.ID, "/b.vcf", "Ada L")
	if a2.ID == a.ID {
		t.Fatal("expected new ids after a resync")
	}
	got, _ := db.DismissedContactPairs(ctx)
	if !got[NewContactPair(ContactKey(a2), ContactKey(b2))] {
		t.Error("the dismissal did not survive the resync")
	}
}
