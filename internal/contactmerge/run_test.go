package contactmerge

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/emersion/go-vcard"

	pcarddav "github.com/peltonapp/Pelton/internal/carddav"
	"github.com/peltonapp/Pelton/internal/storage"
)

// fakeServer is one address book collection held in memory. It enforces the
// preconditions the real client sends, which is what the merge relies on.
type fakeServer struct {
	cards   map[string]vcard.Card
	etags   map[string]string
	version int
	ops     []string
	// refuseDelete lists paths whose delete answers 412.
	refuseDelete map[string]bool
	// stale makes every conditional write answer 412, as if another device had
	// changed the card.
	stale bool
}

func newFakeServer() *fakeServer {
	return &fakeServer{cards: map[string]vcard.Card{}, etags: map[string]string{}, refuseDelete: map[string]bool{}}
}

func (f *fakeServer) seed(c storage.Contact) {
	parsed, _ := pcarddav.ParseCard(c.Card)
	f.cards[c.Path] = parsed.Card
	f.etags[c.Path] = c.ETag
}

func (f *fakeServer) Put(_ context.Context, path string, card vcard.Card, pre pcarddav.Precondition, etag string) (string, error) {
	f.ops = append(f.ops, fmt.Sprintf("PUT %s pre=%d if=%s", path, pre, etag))
	_, exists := f.etags[path]
	switch {
	case f.stale && pre != pcarddav.PreconditionNone:
		return "", pcarddav.ErrConflict
	case pre == pcarddav.PreconditionNew && exists:
		return "", pcarddav.ErrConflict
	case pre == pcarddav.PreconditionMatch && (!exists || `"`+f.etags[path]+`"` != etag):
		return "", pcarddav.ErrConflict
	}
	f.version++
	f.cards[path] = card
	f.etags[path] = fmt.Sprintf("v%d", f.version)
	return `"` + f.etags[path] + `"`, nil
}

func (f *fakeServer) Delete(_ context.Context, path, ifMatch string) error {
	f.ops = append(f.ops, fmt.Sprintf("DELETE %s if=%s", path, ifMatch))
	if _, ok := f.etags[path]; !ok {
		return pcarddav.ErrNotFound
	}
	if f.refuseDelete[path] || (ifMatch != "" && `"`+f.etags[path]+`"` != ifMatch) {
		return pcarddav.ErrConflict
	}
	delete(f.cards, path)
	delete(f.etags, path)
	return nil
}

func (f *fakeServer) Get(_ context.Context, path string) (pcarddav.Contact, error) {
	card, ok := f.cards[path]
	if !ok {
		return pcarddav.Contact{}, pcarddav.ErrNotFound
	}
	c := pcarddav.FromCard(card)
	c.Path, c.ETag = path, f.etags[path]
	return c, nil
}

func (f *fakeServer) opsText() string { return strings.Join(f.ops, "\n") }

type fixture struct {
	db      *storage.DB
	env     Env
	servers map[int64]*fakeServer
	books   map[string]storage.AddressBook
}

func newFixture(t *testing.T, bookNames ...string) *fixture {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.RunMigrations(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	f := &fixture{db: db, servers: map[int64]*fakeServer{}, books: map[string]storage.AddressBook{}}
	for _, name := range bookNames {
		book := storage.AddressBook{Name: name, URL: "https://" + name + ".example", CollectionPath: "/" + name + "/"}
		if _, err := db.CreateAddressBook(context.Background(), &book); err != nil {
			t.Fatal(err)
		}
		f.books[name] = book
		f.servers[book.ID] = newFakeServer()
	}
	f.env = Env{
		Store: db,
		Open: func(_ context.Context, book storage.AddressBook) (Remote, error) {
			return f.servers[book.ID], nil
		},
		Collection: func(book storage.AddressBook) string { return book.CollectionPath },
	}
	return f
}

// add stores a contact locally and seeds it on its book's server.
func (f *fixture) add(t *testing.T, book string, path, body string) storage.Contact {
	t.Helper()
	b := f.books[book]
	c := stored(t, 0, b.ID, path, body)
	if err := f.db.SaveContact(context.Background(), &c); err != nil {
		t.Fatal(err)
	}
	f.servers[b.ID].seed(c)
	return c
}

func (f *fixture) ids(t *testing.T) []string {
	t.Helper()
	list, err := f.db.ListContacts(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, c := range list {
		out = append(out, c.Path)
	}
	return out
}

func TestRunMergesInPlaceAndDeletesTheOthers(t *testing.T) {
	f := newFixture(t, "personal")
	ctx := context.Background()
	a := f.add(t, "personal", "/personal/a.vcf", "UID:u1\nFN:Ada Lovelace\nEMAIL:ada@work.example\nX-KEEP:yes")
	b := f.add(t, "personal", "/personal/b.vcf", "UID:u2\nFN:Ada L\nEMAIL:ada@home.example\nTEL:+31 6 1234 5678\nNICKNAME:Countess")
	f.add(t, "personal", "/personal/c.vcf", "UID:u3\nFN:Grace Hopper\nEMAIL:grace@example.com")
	server := f.servers[a.BookID]

	res, err := Run(ctx, f.env, Request{SurvivorID: storedID(t, f, a), MergeIDs: []int64{storedID(t, f, b)}})
	if err != nil {
		t.Fatal(err)
	}

	if ops := server.opsText(); !strings.Contains(ops, `PUT /personal/a.vcf pre=`+fmt.Sprint(pcarddav.PreconditionMatch)+` if="e/personal/a.vcf"`) {
		t.Errorf("survivor was not written in place under its etag:\n%s", ops)
	}
	if !strings.Contains(server.opsText(), `DELETE /personal/b.vcf if="e/personal/b.vcf"`) {
		t.Errorf("merged contact was not deleted with its etag:\n%s", server.opsText())
	}
	if _, ok := server.cards["/personal/b.vcf"]; ok {
		t.Error("merged contact is still on the server")
	}
	card := server.cards["/personal/a.vcf"]
	if card.Value("X-KEEP") != "yes" || card.Value("NICKNAME") != "Countess" {
		t.Errorf("survivor's card lost or did not gain properties: %v", card)
	}
	if got := f.ids(t); strings.Join(got, ",") != "/personal/a.vcf,/personal/c.vcf" {
		t.Errorf("stored contacts = %v", got)
	}
	if len(res.Contact.Emails) != 2 || len(res.Contact.Phones) != 1 {
		t.Errorf("merged contact = %+v", res.Contact)
	}
	if res.Contact.ETag != server.etags["/personal/a.vcf"] {
		t.Errorf("stored etag %q does not match the server's %q", res.Contact.ETag, server.etags["/personal/a.vcf"])
	}
}

func storedID(t *testing.T, f *fixture, c storage.Contact) int64 {
	t.Helper()
	list, err := f.db.ListContacts(context.Background(), c.BookID)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range list {
		if x.Path == c.Path {
			return x.ID
		}
	}
	t.Fatalf("contact %s not stored", c.Path)
	return 0
}

// every other feature looks a contact up by address, so the merged contact
// has to answer for each address the originals held.
func TestRunKeepsEveryAddressResolvingToTheSurvivor(t *testing.T) {
	f := newFixture(t, "personal")
	ctx := context.Background()
	a := f.add(t, "personal", "/personal/a.vcf", "UID:u1\nFN:Ada Lovelace\nEMAIL:ada@work.example")
	b := f.add(t, "personal", "/personal/b.vcf", "UID:u2\nFN:Ada L\nEMAIL:ada@home.example\nEMAIL:ada@old.example")

	if _, err := Run(ctx, f.env, Request{SurvivorID: storedID(t, f, a), MergeIDs: []int64{storedID(t, f, b)}}); err != nil {
		t.Fatal(err)
	}
	addresses := []string{"ada@work.example", "ada@home.example", "ada@old.example"}
	names, err := f.db.ContactNames(ctx, addresses)
	if err != nil {
		t.Fatal(err)
	}
	for _, addr := range addresses {
		if names[addr] != "Ada Lovelace" {
			t.Errorf("%s resolves to %q, want the survivor", addr, names[addr])
		}
	}
	matches, err := f.db.SearchContacts(ctx, "ada@old", 5)
	if err != nil || len(matches) != 1 || matches[0].Name != "Ada Lovelace" {
		t.Errorf("autocomplete = %+v, %v", matches, err)
	}
}

func TestRunAcrossBooksNeedsATargetAndMovesTheSurvivor(t *testing.T) {
	f := newFixture(t, "personal", "work")
	ctx := context.Background()
	a := f.add(t, "personal", "/personal/a.vcf", "UID:u1\nFN:Ada Lovelace\nEMAIL:ada@work.example")
	b := f.add(t, "work", "/work/b.vcf", "UID:u2\nFN:Ada L\nEMAIL:ada@home.example")
	req := Request{SurvivorID: storedID(t, f, a), MergeIDs: []int64{storedID(t, f, b)}}

	if _, err := Run(ctx, f.env, req); !errors.Is(err, ErrNeedsTarget) {
		t.Fatalf("err = %v, want ErrNeedsTarget", err)
	}
	if len(f.servers[a.BookID].ops)+len(f.servers[b.BookID].ops) != 0 {
		t.Error("a merge without a target touched a server")
	}

	req.TargetBookID = f.books["work"].ID
	res, err := Run(ctx, f.env, req)
	if err != nil {
		t.Fatal(err)
	}
	work, personal := f.servers[f.books["work"].ID], f.servers[f.books["personal"].ID]
	if res.Contact.BookID != req.TargetBookID || res.Contact.Path != "/work/u1.vcf" {
		t.Errorf("merged contact = %s in book %d", res.Contact.Path, res.Contact.BookID)
	}
	if !strings.Contains(work.opsText(), "PUT /work/u1.vcf pre=0") {
		t.Errorf("the new resource must be written only if absent:\n%s", work.opsText())
	}
	if len(personal.cards) != 0 {
		t.Error("the survivor was not deleted from its own book")
	}
	if _, ok := work.cards["/work/b.vcf"]; ok {
		t.Error("the merged contact was not deleted")
	}
	if got := f.ids(t); strings.Join(got, ",") != "/work/u1.vcf" {
		t.Errorf("stored contacts = %v", got)
	}
	if !res.Record.Moved {
		t.Error("record does not say the survivor moved")
	}
}

func TestRunRefusesReadOnlyBooks(t *testing.T) {
	f := newFixture(t, "personal", "shared")
	ctx := context.Background()
	a := f.add(t, "personal", "/personal/a.vcf", "UID:u1\nFN:Ada")
	b := f.add(t, "shared", "/shared/b.vcf", "UID:u2\nFN:Ada")
	shared := f.books["shared"]
	shared.ReadOnly = true
	if err := f.db.UpdateAddressBook(ctx, shared); err != nil {
		t.Fatal(err)
	}
	_, err := Run(ctx, f.env, Request{SurvivorID: storedID(t, f, a), MergeIDs: []int64{storedID(t, f, b)}, TargetBookID: f.books["personal"].ID})
	if !errors.Is(err, ErrReadOnly) {
		t.Fatalf("err = %v, want ErrReadOnly", err)
	}
	if len(f.servers[a.BookID].ops) != 0 {
		t.Error("a refused merge wrote to a server")
	}
}

func TestRunStopsWhenTheServerCopyChanged(t *testing.T) {
	f := newFixture(t, "personal")
	ctx := context.Background()
	a := f.add(t, "personal", "/personal/a.vcf", "UID:u1\nFN:Ada")
	b := f.add(t, "personal", "/personal/b.vcf", "UID:u2\nFN:Ada")
	f.servers[a.BookID].stale = true

	_, err := Run(ctx, f.env, Request{SurvivorID: storedID(t, f, a), MergeIDs: []int64{storedID(t, f, b)}})
	if !errors.Is(err, ErrServerChanged) {
		t.Fatalf("err = %v, want ErrServerChanged", err)
	}
	if got := f.ids(t); len(got) != 2 {
		t.Errorf("a refused merge changed the local contacts: %v", got)
	}
	if strings.Contains(f.servers[a.BookID].opsText(), "DELETE") {
		t.Error("nothing may be deleted when the survivor could not be written")
	}
}

func TestRunKeepsAContactTheServerWillNotDelete(t *testing.T) {
	f := newFixture(t, "personal")
	ctx := context.Background()
	a := f.add(t, "personal", "/personal/a.vcf", "UID:u1\nFN:Ada")
	b := f.add(t, "personal", "/personal/b.vcf", "UID:u2\nFN:Ada")
	c := f.add(t, "personal", "/personal/c.vcf", "UID:u3\nFN:Ada")
	f.servers[a.BookID].refuseDelete["/personal/b.vcf"] = true
	bID := storedID(t, f, b)

	res, err := Run(ctx, f.env, Request{SurvivorID: storedID(t, f, a), MergeIDs: []int64{bID, storedID(t, f, c)}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Record.Kept) != 1 || res.Record.Kept[0] != bID {
		t.Errorf("kept = %v, want [%d]", res.Record.Kept, bID)
	}
	if got := strings.Join(f.ids(t), ","); got != "/personal/a.vcf,/personal/b.vcf" {
		t.Errorf("stored = %s: the refused one stays, the other goes", got)
	}
}

func TestRunRejectsBadRequests(t *testing.T) {
	f := newFixture(t, "personal")
	a := f.add(t, "personal", "/personal/a.vcf", "UID:u1\nFN:Ada")
	id := storedID(t, f, a)
	for name, req := range map[string]Request{
		"no survivor":  {MergeIDs: []int64{id}},
		"nothing else": {SurvivorID: id},
		"itself":       {SurvivorID: id, MergeIDs: []int64{id}},
	} {
		if _, err := Run(context.Background(), f.env, req); !errors.Is(err, ErrBadRequest) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestUndoRestoresTheOriginalsInPlace(t *testing.T) {
	f := newFixture(t, "personal")
	ctx := context.Background()
	a := f.add(t, "personal", "/personal/a.vcf", "UID:u1\nFN:Ada Lovelace\nEMAIL:ada@work.example\nBDAY:1815-12-10")
	b := f.add(t, "personal", "/personal/b.vcf", "UID:u2\nFN:Ada L\nEMAIL:ada@home.example\nX-KEEP:yes")
	server := f.servers[a.BookID]
	res, err := Run(ctx, f.env, Request{SurvivorID: storedID(t, f, a), MergeIDs: []int64{storedID(t, f, b)}})
	if err != nil {
		t.Fatal(err)
	}

	restored, err := Undo(ctx, f.env, res.Record)
	if err != nil {
		t.Fatal(err)
	}
	if len(restored) != 2 {
		t.Fatalf("restored %d contacts, want 2", len(restored))
	}
	list, _ := f.db.ListContacts(ctx, 0)
	if len(list) != 2 {
		t.Fatalf("stored = %d contacts", len(list))
	}
	byPath := map[string]storage.Contact{}
	for _, c := range list {
		byPath[c.Path] = c
	}
	if got := byPath["/personal/a.vcf"]; got.FullName != "Ada Lovelace" || len(got.Emails) != 1 {
		t.Errorf("survivor not restored: %+v", got)
	}
	if got := byPath["/personal/b.vcf"]; got.FullName != "Ada L" || got.Emails[0].Value != "ada@home.example" {
		t.Errorf("merged contact not restored: %+v", got)
	}
	if v := server.cards["/personal/b.vcf"].Value("X-KEEP"); v != "yes" {
		t.Errorf("restored card lost its own properties: %q", v)
	}
	if v := server.cards["/personal/a.vcf"].Value("NICKNAME"); v != "" {
		t.Errorf("survivor still carries the merge: %q", v)
	}
	if byPath["/personal/b.vcf"].ETag != server.etags["/personal/b.vcf"] {
		t.Error("restored contact does not hold the server's etag")
	}
}

func TestUndoAfterAMoveRemovesTheMergedCopy(t *testing.T) {
	f := newFixture(t, "personal", "work")
	ctx := context.Background()
	a := f.add(t, "personal", "/personal/a.vcf", "UID:u1\nFN:Ada Lovelace\nEMAIL:ada@work.example")
	b := f.add(t, "work", "/work/b.vcf", "UID:u2\nFN:Ada L\nEMAIL:ada@home.example")
	res, err := Run(ctx, f.env, Request{SurvivorID: storedID(t, f, a), MergeIDs: []int64{storedID(t, f, b)}, TargetBookID: f.books["work"].ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Undo(ctx, f.env, res.Record); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(f.ids(t), ","); got != "/work/b.vcf,/personal/a.vcf" {
		t.Errorf("stored = %s", got)
	}
	if _, ok := f.servers[f.books["work"].ID].cards["/work/u1.vcf"]; ok {
		t.Error("the merged copy is still on the server")
	}
	if _, ok := f.servers[f.books["personal"].ID].cards["/personal/a.vcf"]; !ok {
		t.Error("the survivor was not put back in its book")
	}
}

func TestUndoIsRefusedWhenTheMergedContactChanged(t *testing.T) {
	f := newFixture(t, "personal")
	ctx := context.Background()
	a := f.add(t, "personal", "/personal/a.vcf", "UID:u1\nFN:Ada")
	b := f.add(t, "personal", "/personal/b.vcf", "UID:u2\nFN:Ada")
	res, err := Run(ctx, f.env, Request{SurvivorID: storedID(t, f, a), MergeIDs: []int64{storedID(t, f, b)}})
	if err != nil {
		t.Fatal(err)
	}
	f.servers[a.BookID].etags["/personal/a.vcf"] = "changed-elsewhere"
	if _, err := Undo(ctx, f.env, res.Record); !errors.Is(err, ErrServerChanged) {
		t.Fatalf("err = %v, want ErrServerChanged", err)
	}
	if _, ok := f.servers[a.BookID].cards["/personal/b.vcf"]; ok {
		t.Error("undo restored a contact although it was refused")
	}
}

func TestUndoLeavesAKeptContactAlone(t *testing.T) {
	f := newFixture(t, "personal")
	ctx := context.Background()
	a := f.add(t, "personal", "/personal/a.vcf", "UID:u1\nFN:Ada")
	b := f.add(t, "personal", "/personal/b.vcf", "UID:u2\nFN:Ada")
	f.servers[a.BookID].refuseDelete["/personal/b.vcf"] = true
	res, err := Run(ctx, f.env, Request{SurvivorID: storedID(t, f, a), MergeIDs: []int64{storedID(t, f, b)}})
	if err != nil {
		t.Fatal(err)
	}
	restored, err := Undo(ctx, f.env, res.Record)
	if err != nil {
		t.Fatal(err)
	}
	if len(restored) != 1 || restored[0].Path != "/personal/a.vcf" {
		t.Errorf("restored = %+v, want only the survivor", restored)
	}
	for _, op := range f.servers[a.BookID].ops {
		if strings.HasPrefix(op, "PUT /personal/b.vcf") {
			t.Errorf("the contact the server kept was written again: %s", op)
		}
	}
}
