package storage

import (
	"context"
	"testing"
)

func newBook(t *testing.T, db *DB, name string) AddressBook {
	t.Helper()
	book := AddressBook{Name: name, URL: "https://cloud.example.com", CollectionPath: "/books/" + name}
	if _, err := db.CreateAddressBook(context.Background(), &book); err != nil {
		t.Fatalf("create address book: %v", err)
	}
	return book
}

func saveContact(t *testing.T, db *DB, bookID int64, path, name string, emails ...string) Contact {
	t.Helper()
	values := make([]ContactValue, 0, len(emails))
	for i, e := range emails {
		label := "home"
		if i > 0 {
			label = "work"
		}
		values = append(values, ContactValue{Value: e, Label: label})
	}
	contact := Contact{BookID: bookID, Path: path, ETag: "v1", UID: path, FullName: name, Emails: values}
	if err := db.SaveContact(context.Background(), &contact); err != nil {
		t.Fatalf("save contact %q: %v", name, err)
	}
	return contact
}

// SaveContact is the sync's one write, so the same path arriving again has to
// land as an update rather than a second contact.
func TestSaveContactUpsertsByPath(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := context.Background()
	book := newBook(t, db, "personal")

	saveContact(t, db, book.ID, "/books/personal/a.vcf", "Arne Kock", "arne@example.com")
	saveContact(t, db, book.ID, "/books/personal/a.vcf", "Arne K.", "arne@example.com", "arne@work.example")

	contacts, err := db.ListContacts(ctx, book.ID)
	if err != nil {
		t.Fatalf("list contacts: %v", err)
	}
	if len(contacts) != 1 {
		t.Fatalf("got %d contacts, want 1", len(contacts))
	}
	if contacts[0].FullName != "Arne K." {
		t.Errorf("FullName = %q, want the updated one", contacts[0].FullName)
	}
	if len(contacts[0].Emails) != 2 {
		t.Fatalf("emails = %+v, want both", contacts[0].Emails)
	}
	if contacts[0].Emails[0].Label != "home" {
		t.Errorf("first email label = %q", contacts[0].Emails[0].Label)
	}
}

// an address removed from a card has to disappear locally too, or autocomplete
// keeps offering an address the contact no longer has.
func TestSaveContactReplacesAddresses(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := context.Background()
	book := newBook(t, db, "personal")

	saveContact(t, db, book.ID, "/books/personal/a.vcf", "Arne", "one@example.com", "two@example.com")
	saveContact(t, db, book.ID, "/books/personal/a.vcf", "Arne", "one@example.com")

	matches, err := db.SearchContacts(ctx, "two@", 8)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(matches) != 0 {
		t.Errorf("removed address still found: %+v", matches)
	}
}

func TestSearchContactsMatchesNameOrAddress(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := context.Background()
	book := newBook(t, db, "personal")
	saveContact(t, db, book.ID, "/books/personal/a.vcf", "Arne Kock", "arne@example.com")
	saveContact(t, db, book.ID, "/books/personal/b.vcf", "Potato Co-op", "harvest@potato.example")

	byName, err := db.SearchContacts(ctx, "kock", 8)
	if err != nil {
		t.Fatalf("search by name: %v", err)
	}
	if len(byName) != 1 || byName[0].Email != "arne@example.com" {
		t.Errorf("search by name = %+v", byName)
	}

	byAddress, err := db.SearchContacts(ctx, "potato", 8)
	if err != nil {
		t.Fatalf("search by address: %v", err)
	}
	if len(byAddress) != 1 || byAddress[0].Name != "Potato Co-op" {
		t.Errorf("search by address = %+v", byAddress)
	}
}

// ContactNames is what lets a synced contact's name win over whatever a sender
// once put in a From header.
func TestContactNames(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := context.Background()
	book := newBook(t, db, "personal")
	saveContact(t, db, book.ID, "/books/personal/a.vcf", "Arne Kock", "arne@example.com")

	names, err := db.ContactNames(ctx, []string{"ARNE@example.com", "nobody@example.com"})
	if err != nil {
		t.Fatalf("contact names: %v", err)
	}
	if names["arne@example.com"] != "Arne Kock" {
		t.Errorf("names = %+v, want the address matched case-insensitively", names)
	}
	if _, ok := names["nobody@example.com"]; ok {
		t.Error("an address no contact has came back with a name")
	}
}

// Removing a book removes what it held here. The server keeps its own copy:
// this is Pelton forgetting the book, not deleting the user's contacts.
func TestDeleteAddressBookTakesItsContacts(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := context.Background()
	book := newBook(t, db, "personal")
	saveContact(t, db, book.ID, "/books/personal/a.vcf", "Arne", "arne@example.com")

	if err := db.DeleteAddressBook(ctx, book.ID); err != nil {
		t.Fatalf("delete book: %v", err)
	}
	contacts, err := db.ListContacts(ctx, 0)
	if err != nil {
		t.Fatalf("list contacts: %v", err)
	}
	if len(contacts) != 0 {
		t.Errorf("book deleted but %d contacts remain", len(contacts))
	}
	matches, err := db.SearchContacts(ctx, "arne", 8)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(matches) != 0 {
		t.Errorf("addresses of a deleted book still searchable: %+v", matches)
	}
}

// A sync failure must not throw away the token: the next attempt resumes from
// where it was rather than reading the whole book again.
func TestRecordAddressBookSyncKeepsTokenOnFailure(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := context.Background()
	book := newBook(t, db, "personal")

	if err := db.RecordAddressBookSync(ctx, book.ID, "token-1", ""); err != nil {
		t.Fatalf("record success: %v", err)
	}
	if err := db.RecordAddressBookSync(ctx, book.ID, "", "the server said no"); err != nil {
		t.Fatalf("record failure: %v", err)
	}

	stored, err := db.GetAddressBook(ctx, book.ID)
	if err != nil {
		t.Fatalf("get book: %v", err)
	}
	if stored.SyncToken != "token-1" {
		t.Errorf("SyncToken = %q, want it kept across a failure", stored.SyncToken)
	}
	if stored.LastError == "" {
		t.Error("the failure was not recorded")
	}
	if stored.LastSyncAt == "" {
		t.Error("the earlier success was forgotten")
	}

	if err := db.RecordAddressBookSync(ctx, book.ID, "token-2", ""); err != nil {
		t.Fatalf("record second success: %v", err)
	}
	stored, err = db.GetAddressBook(ctx, book.ID)
	if err != nil {
		t.Fatalf("get book: %v", err)
	}
	if stored.LastError != "" {
		t.Errorf("LastError = %q, want it cleared by a success", stored.LastError)
	}
}
