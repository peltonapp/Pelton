package contactmerge

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/emersion/go-vcard"

	pcarddav "github.com/peltonapp/Pelton/internal/carddav"
	"github.com/peltonapp/Pelton/internal/storage"
)

var (
	// ErrServerChanged means a contact changed on its server since the last
	// sync, so a conditional write or delete was refused. Nothing was
	// overwritten; sync the address book and merge again.
	ErrServerChanged = errors.New("contactmerge: a contact changed on the server since it was last synced")
	// ErrNeedsTarget means the contacts are in more than one address book and
	// no target book was chosen.
	ErrNeedsTarget = errors.New("contactmerge: choose the address book the merged contact goes in")
	// ErrReadOnly means an address book involved does not take writes.
	ErrReadOnly = errors.New("contactmerge: an address book involved is read-only")
	// ErrBadRequest means the request names no survivor, nothing to merge, or
	// the same contact twice.
	ErrBadRequest = errors.New("contactmerge: a merge needs a survivor and at least one other contact")
)

// Remote is the part of a CardDAV client a merge writes through.
// *carddav.Client satisfies it.
type Remote interface {
	Put(ctx context.Context, path string, card vcard.Card, pre pcarddav.Precondition, etag string) (string, error)
	Delete(ctx context.Context, path, ifMatch string) error
	Get(ctx context.Context, path string) (pcarddav.Contact, error)
}

// Env is what a merge runs against.
type Env struct {
	Store *storage.DB
	// Open connects to a book's server.
	Open func(ctx context.Context, book storage.AddressBook) (Remote, error)
	// Collection is the path new contacts of a book are written under.
	Collection func(book storage.AddressBook) string
}

// Request is one merge. The contacts in MergeIDs are folded into SurvivorID.
type Request struct {
	SurvivorID int64
	MergeIDs   []int64
	// TargetBookID is the book the merged contact ends up in. It is required
	// when the contacts are in more than one book, and 0 otherwise means the
	// survivor's own. A target that is not the survivor's book writes the
	// merged contact there as a new resource and deletes the survivor's.
	TargetBookID int64
	Choices      Choices
}

// Record is what an undo needs: the cards as they were before the merge and
// where the merged contact went.
type Record struct {
	// Originals are the survivor first, then each merged contact, as stored
	// before the merge.
	Originals []storage.Contact
	// Merged is where the combined contact was written.
	MergedBookID int64
	MergedPath   string
	// Moved is true when the survivor's own resource was deleted because the
	// merged contact went to another book or path.
	Moved bool
	// Kept are the ids of merged contacts whose delete the server refused;
	// they still exist and need no restoring.
	Kept []int64
}

// Result is what a merge did.
type Result struct {
	// Contact is the merged contact as stored.
	Contact storage.Contact
	// Record is what Undo takes. Record.Kept lists merged contacts that are
	// still on the server because it refused their deletion: they changed since
	// the last sync, and the merged contact already holds their details.
	Record Record
}

// Run carries out a merge. The merged card is written to the server first, with
// a precondition so a card changed elsewhere is never overwritten, and only
// then are the others deleted there and locally, each conditionally too. A
// failure part way therefore leaves duplicates behind, never a lost contact.
func Run(ctx context.Context, env Env, req Request) (Result, error) {
	loaded, err := load(ctx, env, req)
	if err != nil {
		return Result{}, err
	}
	survivor, others := loaded.survivor, loaded.others
	target := loaded.target

	combined, err := Combine(survivor, others, req.Choices)
	if err != nil {
		return Result{}, err
	}

	moved := target.ID != survivor.BookID
	pre, etag := pcarddav.PreconditionMatch, quoteETag(survivor.ETag)
	if moved {
		combined.Path = path.Join(env.Collection(target), combined.UID+".vcf")
		combined.ETag = ""
		pre, etag = pcarddav.PreconditionNew, ""
	}
	if combined.UID == "" {
		return Result{}, fmt.Errorf("contactmerge: %q has no UID to write under", survivor.FullName)
	}

	remote, err := loaded.remote(target.ID)
	if err != nil {
		return Result{}, err
	}
	newETag, err := remote.Put(ctx, combined.Path, combined.ToCard(), pre, etag)
	if errors.Is(err, pcarddav.ErrConflict) {
		return Result{}, ErrServerChanged
	}
	if err != nil {
		return Result{}, err
	}
	combined.ETag = strings.Trim(newETag, `"`)
	if combined.ETag == "" {
		if fetched, err := remote.Get(ctx, combined.Path); err == nil {
			combined.ETag = fetched.ETag
		}
	}
	merged, err := store(ctx, env.Store, target.ID, combined)
	if err != nil {
		return Result{}, err
	}

	record := Record{
		Originals:    append([]storage.Contact{survivor}, others...),
		MergedBookID: target.ID,
		MergedPath:   combined.Path,
		Moved:        moved,
	}
	gone := others
	if moved {
		gone = append([]storage.Contact{survivor}, others...)
	}
	for _, c := range gone {
		if err := loaded.deleteRemote(ctx, c); err != nil {
			if errors.Is(err, ErrServerChanged) {
				record.Kept = append(record.Kept, c.ID)
				continue
			}
			return Result{}, err
		}
		if err := env.Store.DeleteContact(ctx, c.ID); err != nil && !errors.Is(err, storage.ErrContactNotFound) {
			return Result{}, err
		}
	}
	return Result{Contact: merged, Record: record}, nil
}

// Undo puts the contacts of a merge back. The merged contact is returned to
// the survivor's original card, or deleted when it had moved books, and each
// merged contact is written back at its old path. A contact whose delete was
// refused during the merge is still there and is left alone. It returns the
// restored contacts as stored.
//
// The restored cards are new resources to a server that has deleted them: other
// devices see them come back, with new etags. If the merged contact changed on
// the server since the merge, undo is refused rather than overwriting it.
func Undo(ctx context.Context, env Env, rec Record) ([]storage.Contact, error) {
	if len(rec.Originals) < 2 {
		return nil, ErrBadRequest
	}
	remotes := map[int64]Remote{}
	open := func(id int64) (Remote, error) {
		if r, ok := remotes[id]; ok {
			return r, nil
		}
		book, err := env.Store.GetAddressBook(ctx, id)
		if err != nil {
			return nil, err
		}
		if book.ReadOnly {
			return nil, ErrReadOnly
		}
		r, err := env.Open(ctx, *book)
		if err != nil {
			return nil, err
		}
		remotes[id] = r
		return r, nil
	}

	current, err := currentMerged(ctx, env.Store, rec)
	if err != nil {
		return nil, err
	}

	var restored []storage.Contact
	if rec.Moved {
		remote, err := open(rec.MergedBookID)
		if err != nil {
			return nil, err
		}
		if err := deleteWith(ctx, remote, rec.MergedPath, current.ETag); err != nil {
			return nil, err
		}
		if err := env.Store.DeleteContact(ctx, current.ID); err != nil && !errors.Is(err, storage.ErrContactNotFound) {
			return nil, err
		}
	}
	kept := map[int64]bool{}
	for _, id := range rec.Kept {
		kept[id] = true
	}
	for i, orig := range rec.Originals {
		if kept[orig.ID] {
			continue
		}
		remote, err := open(orig.BookID)
		if err != nil {
			return restored, err
		}
		card, err := pcarddav.ParseCard(orig.Card)
		if err != nil {
			return restored, fmt.Errorf("contactmerge: restore %q: %w", orig.FullName, err)
		}
		pre, etag := pcarddav.PreconditionNew, ""
		if i == 0 && !rec.Moved {
			pre, etag = pcarddav.PreconditionMatch, quoteETag(current.ETag)
		}
		newETag, err := remote.Put(ctx, orig.Path, card.Card, pre, etag)
		if errors.Is(err, pcarddav.ErrConflict) {
			if i == 0 && !rec.Moved {
				return restored, ErrServerChanged
			}
			// something is at the path already, which is the contact itself
			// or a newer card: either way it is not overwritten.
			continue
		}
		if err != nil {
			return restored, err
		}
		orig.ETag = strings.Trim(newETag, `"`)
		if orig.ETag == "" {
			if fetched, err := remote.Get(ctx, orig.Path); err == nil {
				orig.ETag = fetched.ETag
			}
		}
		orig.ID = 0
		if err := env.Store.SaveContact(ctx, &orig); err != nil {
			return restored, err
		}
		restored = append(restored, orig)
	}
	return restored, nil
}

type loadedMerge struct {
	survivor storage.Contact
	others   []storage.Contact
	target   storage.AddressBook
	books    map[int64]storage.AddressBook
	remotes  map[int64]Remote
	env      Env
	ctx      context.Context
}

// load reads and checks everything a merge touches: the contacts, their books,
// that each takes writes, and that the target is chosen when needed.
func load(ctx context.Context, env Env, req Request) (*loadedMerge, error) {
	if req.SurvivorID == 0 || len(req.MergeIDs) == 0 {
		return nil, ErrBadRequest
	}
	seen := map[int64]bool{req.SurvivorID: true}
	for _, id := range req.MergeIDs {
		if seen[id] {
			return nil, ErrBadRequest
		}
		seen[id] = true
	}
	l := &loadedMerge{env: env, ctx: ctx, books: map[int64]storage.AddressBook{}, remotes: map[int64]Remote{}}
	survivor, err := env.Store.GetContact(ctx, req.SurvivorID)
	if err != nil {
		return nil, err
	}
	l.survivor = *survivor
	for _, id := range req.MergeIDs {
		c, err := env.Store.GetContact(ctx, id)
		if err != nil {
			return nil, err
		}
		l.others = append(l.others, *c)
	}
	bookIDs := map[int64]bool{survivor.BookID: true}
	for _, c := range l.others {
		bookIDs[c.BookID] = true
	}
	targetID := req.TargetBookID
	if targetID == 0 {
		if len(bookIDs) > 1 {
			return nil, ErrNeedsTarget
		}
		targetID = survivor.BookID
	}
	bookIDs[targetID] = true
	for id := range bookIDs {
		book, err := env.Store.GetAddressBook(ctx, id)
		if err != nil {
			return nil, err
		}
		if book.ReadOnly {
			return nil, fmt.Errorf("%w: %s", ErrReadOnly, book.Name)
		}
		l.books[id] = *book
	}
	l.target = l.books[targetID]
	return l, nil
}

func (l *loadedMerge) remote(bookID int64) (Remote, error) {
	if r, ok := l.remotes[bookID]; ok {
		return r, nil
	}
	r, err := l.env.Open(l.ctx, l.books[bookID])
	if err != nil {
		return nil, err
	}
	l.remotes[bookID] = r
	return r, nil
}

// deleteRemote deletes a contact on its own server, conditionally. A contact
// the server no longer has counts as deleted.
func (l *loadedMerge) deleteRemote(ctx context.Context, c storage.Contact) error {
	remote, err := l.remote(c.BookID)
	if err != nil {
		return err
	}
	return deleteWith(ctx, remote, c.Path, c.ETag)
}

func deleteWith(ctx context.Context, remote Remote, path, etag string) error {
	err := remote.Delete(ctx, path, quoteETag(etag))
	switch {
	case errors.Is(err, pcarddav.ErrConflict):
		return ErrServerChanged
	case errors.Is(err, pcarddav.ErrNotFound):
		return nil
	}
	return err
}

// currentMerged finds the merged contact as it is stored now.
func currentMerged(ctx context.Context, db *storage.DB, rec Record) (storage.Contact, error) {
	contacts, err := db.ListContacts(ctx, rec.MergedBookID)
	if err != nil {
		return storage.Contact{}, err
	}
	for _, c := range contacts {
		if c.Path == rec.MergedPath {
			return c, nil
		}
	}
	return storage.Contact{}, storage.ErrContactNotFound
}

// store writes a combined contact to the local copy of its book.
func store(ctx context.Context, db *storage.DB, bookID int64, c pcarddav.Contact) (storage.Contact, error) {
	card, err := pcarddav.EncodeCard(c.Card)
	if err != nil {
		return storage.Contact{}, err
	}
	stored := storage.Contact{
		BookID:       bookID,
		Path:         c.Path,
		ETag:         c.ETag,
		UID:          c.UID,
		FullName:     c.FullName,
		Organization: c.Organization,
		Title:        c.Title,
		Note:         c.Note,
		Card:         card,
	}
	for _, e := range c.Emails {
		stored.Emails = append(stored.Emails, storage.ContactValue{Value: e.Value, Label: e.Label})
	}
	for _, p := range c.Phones {
		stored.Phones = append(stored.Phones, storage.ContactValue{Value: p.Value, Label: p.Label})
	}
	if err := db.SaveContact(ctx, &stored); err != nil {
		return storage.Contact{}, err
	}
	return stored, nil
}

// quoteETag restores the quotes an etag is compared with on the wire.
func quoteETag(etag string) string {
	if etag == "" || strings.HasPrefix(etag, `"`) || strings.HasPrefix(etag, "W/") {
		return etag
	}
	return `"` + etag + `"`
}
