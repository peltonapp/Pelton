package desktop

import (
	"errors"
	"testing"

	"github.com/peltonapp/Pelton/internal/contactmerge"
	"github.com/peltonapp/Pelton/internal/storage"
)

func seedDuplicates(t *testing.T, a *App) []int64 {
	t.Helper()
	book := storage.AddressBook{Name: "personal", URL: "https://cloud.example.com", CollectionPath: "/books/personal"}
	if _, err := a.store.CreateAddressBook(a.ctx, &book); err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, c := range []storage.Contact{
		{Path: "/a.vcf", UID: "a", FullName: "Ada Lovelace", Emails: []storage.ContactValue{{Value: "ada@example.com"}}},
		{Path: "/b.vcf", UID: "b", FullName: "A. Lovelace", Emails: []storage.ContactValue{{Value: "ADA@example.com"}}},
		{Path: "/c.vcf", UID: "c", FullName: "Grace Hopper", Emails: []storage.ContactValue{{Value: "grace@example.com"}}},
	} {
		c.BookID = book.ID
		if err := a.store.SaveContact(a.ctx, &c); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, c.ID)
	}
	return ids
}

func TestFindAndDismissDuplicateContacts(t *testing.T) {
	a := newVIPTestApp(t)
	ids := seedDuplicates(t, a)

	found, err := a.FindDuplicateContacts()
	if err != nil {
		t.Fatal(err)
	}
	if len(found.Groups) != 1 || len(found.Groups[0].Contacts) != 2 {
		t.Fatalf("groups = %+v", found.Groups)
	}
	g := found.Groups[0]
	if g.Score != 100 || len(g.Reasons) != 1 || g.Reasons[0].Kind != "email" {
		t.Errorf("group = %+v", g)
	}
	if g.Contacts[0].BookName != "personal" {
		t.Errorf("contact lacks its book name: %+v", g.Contacts[0])
	}

	if err := a.DismissDuplicateContacts(ids[:2]); err != nil {
		t.Fatal(err)
	}
	found, _ = a.FindDuplicateContacts()
	if len(found.Groups) != 0 || found.Dismissed != 1 {
		t.Fatalf("after dismissing: %+v", found)
	}

	n, err := a.ResetDismissedDuplicates()
	if err != nil || n != 1 {
		t.Fatalf("reset = %d, %v", n, err)
	}
	if found, _ = a.FindDuplicateContacts(); len(found.Groups) != 1 {
		t.Errorf("the group should be back: %+v", found)
	}
}

func TestMergeContactsRejectsAMergeAcrossBooksWithoutATarget(t *testing.T) {
	a := newVIPTestApp(t)
	ids := seedDuplicates(t, a)
	other := storage.AddressBook{Name: "work", URL: "https://work.example.com", CollectionPath: "/books/work"}
	if _, err := a.store.CreateAddressBook(a.ctx, &other); err != nil {
		t.Fatal(err)
	}
	moved := storage.Contact{BookID: other.ID, Path: "/d.vcf", UID: "d", FullName: "Ada L", Emails: []storage.ContactValue{{Value: "ada@example.com"}}}
	if err := a.store.SaveContact(a.ctx, &moved); err != nil {
		t.Fatal(err)
	}
	_, err := a.MergeContacts(MergeContactsRequest{SurvivorID: ids[0], MergeIDs: []int64{moved.ID}})
	if !errors.Is(err, errMergeTarget) {
		t.Fatalf("err = %v, want the choose-a-book error", err)
	}
}

func TestMergeHistoryKeepsTheLatestMerges(t *testing.T) {
	var h mergeHistory
	var tokens []string
	for i := 0; i < mergeHistoryLimit+2; i++ {
		tokens = append(tokens, h.add(contactmerge.Record{MergedPath: "/p"}))
	}
	if _, ok := h.take(tokens[0]); ok {
		t.Error("the oldest merge should have been forgotten")
	}
	if _, ok := h.take(tokens[len(tokens)-1]); !ok {
		t.Error("the newest merge should be kept")
	}
	h.drop(tokens[len(tokens)-1])
	if _, ok := h.take(tokens[len(tokens)-1]); ok {
		t.Error("a dropped merge should be gone")
	}
}

func TestUndoContactMergeWithAnUnknownToken(t *testing.T) {
	a := newVIPTestApp(t)
	if _, err := a.UndoContactMerge("nope"); !errors.Is(err, errMergeUndone) {
		t.Fatalf("err = %v", err)
	}
}
