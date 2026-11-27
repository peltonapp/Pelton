package desktop

import (
	"context"
	"errors"
	"strings"

	pcarddav "github.com/peltonapp/Pelton/internal/carddav"
	"github.com/peltonapp/Pelton/internal/storage"
)

// Writing contacts back (#168).
//
// Every write is conditional. A create sends If-None-Match so it cannot land on
// top of a contact another client made at the same path; an update sends
// If-Match with the etag the local copy came from, so it cannot overwrite a
// change made on a phone since the last sync. A server refusing either comes
// back as a conflict the user resolves, rather than as a silent overwrite.

// ContactRequest is the editor's contact. An ID of 0 creates, and then BookID
// says where: there is no default book, because a contact filed in the wrong
// address book is a contact on the wrong device.
type ContactRequest struct {
	ID           int64             `json:"id"`
	BookID       int64             `json:"bookId"`
	FullName     string            `json:"fullName"`
	Organization string            `json:"organization"`
	Title        string            `json:"title"`
	Note         string            `json:"note"`
	Emails       []ContactValueDTO `json:"emails"`
	Phones       []ContactValueDTO `json:"phones"`
	// MatrixID is the person's Matrix user id; empty removes it.
	MatrixID string `json:"matrixId"`
	// Force resolves a conflict the user has already been shown: write anyway,
	// over whatever the server now holds. It is only ever set by the "keep
	// mine" button on the conflict dialog.
	Force bool `json:"force"`
}

// SaveContact creates or updates a contact and writes it to the server. A
// refused write returns Conflict with both versions and changes nothing, local
// or remote, so the user decides which one survives.
func (a *App) SaveContact(req ContactRequest) (ContactConflictDTO, error) {
	res, err := a.saveContact(req)
	if err == nil && req.ID == 0 && res.Saved.ID != 0 {
		a.scriptContactAdded(a.ctx, res.Saved.ID)
	}
	return res, err
}

func (a *App) saveContact(req ContactRequest) (ContactConflictDTO, error) {
	if err := a.ready(); err != nil {
		return ContactConflictDTO{}, err
	}
	if strings.TrimSpace(req.FullName) == "" && len(fromDTOValues(req.Emails)) == 0 {
		return ContactConflictDTO{}, errContactNeedsField
	}
	if req.ID == 0 && req.BookID == 0 {
		return ContactConflictDTO{}, errContactNeedsBook
	}

	existing, book, err := a.contactTarget(req)
	if err != nil {
		return ContactConflictDTO{}, err
	}
	if book.ReadOnly {
		return ContactConflictDTO{}, errBookReadOnly
	}

	contact := a.contactFromRequest(req, existing)
	if book.Local() {
		return a.saveLocalContact(book.ID, contact)
	}
	client, err := a.addressBookClient(a.ctx, *book)
	if err != nil {
		return ContactConflictDTO{}, err
	}
	ctx, cancel := context.WithTimeout(a.ctx, contactsTimeout)
	defer cancel()

	pre, etagHeader := precondition(req.Force, existing != nil, contact.ETag)
	etag, err := client.Put(ctx, contact.Path, contact.Card, pre, etagHeader)
	if errors.Is(err, pcarddav.ErrConflict) {
		return a.contactConflict(ctx, client, *book, contact)
	}
	if err != nil {
		return ContactConflictDTO{}, err
	}

	contact.ETag = strings.Trim(etag, `"`)
	if contact.ETag == "" {
		// a server that sends no etag on write is one we have to ask, or the
		// next edit would carry a stale precondition and be refused.
		if fetched, err := client.Get(ctx, contact.Path); err == nil {
			contact = fetched
		}
	}
	if err := a.storeContact(a.ctx, book.ID, contact); err != nil {
		return ContactConflictDTO{}, err
	}
	saved, err := a.contactByPath(book.ID, contact.Path)
	if err != nil {
		return ContactConflictDTO{}, err
	}
	return ContactConflictDTO{Saved: saved}, nil
}

// saveLocalContact stores a contact of a book that lives only here, such as
// an imported one: there is no server to write to first.
func (a *App) saveLocalContact(bookID int64, contact pcarddav.Contact) (ContactConflictDTO, error) {
	if err := a.storeContact(a.ctx, bookID, contact); err != nil {
		return ContactConflictDTO{}, err
	}
	saved, err := a.contactByPath(bookID, contact.Path)
	if err != nil {
		return ContactConflictDTO{}, err
	}
	return ContactConflictDTO{Saved: saved}, nil
}

// DeleteContact removes a contact here and on the server. Like a save it is
// conditional, so a contact edited elsewhere since the last sync reports a
// conflict rather than disappearing.
func (a *App) DeleteContact(id int64, force bool) (ContactConflictDTO, error) {
	if err := a.ready(); err != nil {
		return ContactConflictDTO{}, err
	}
	stored, err := a.store.GetContact(a.ctx, id)
	if err != nil {
		return ContactConflictDTO{}, err
	}
	book, err := a.store.GetAddressBook(a.ctx, stored.BookID)
	if err != nil {
		return ContactConflictDTO{}, err
	}
	if book.ReadOnly {
		return ContactConflictDTO{}, errBookReadOnly
	}
	if book.Local() {
		return ContactConflictDTO{}, a.store.DeleteContact(a.ctx, id)
	}

	client, err := a.addressBookClient(a.ctx, *book)
	if err != nil {
		return ContactConflictDTO{}, err
	}
	ctx, cancel := context.WithTimeout(a.ctx, contactsTimeout)
	defer cancel()

	ifMatch := stored.ETag
	if force {
		ifMatch = ""
	}
	err = client.Delete(ctx, stored.Path, quoteETag(ifMatch))
	switch {
	case errors.Is(err, pcarddav.ErrConflict):
		mine, err := a.contactByPath(book.ID, stored.Path)
		if err != nil {
			return ContactConflictDTO{}, err
		}
		return a.conflictWithServer(ctx, client, *book, stored.Path, mine)
	case errors.Is(err, pcarddav.ErrNotFound):
		// already gone on the server, so the local copy is what is stale.
	case err != nil:
		return ContactConflictDTO{}, err
	}
	return ContactConflictDTO{}, a.store.DeleteContact(a.ctx, id)
}

// contactTarget resolves what a request is editing: the stored contact when
// there is one, and the book it belongs to.
func (a *App) contactTarget(req ContactRequest) (*storage.Contact, *storage.AddressBook, error) {
	if req.ID == 0 {
		book, err := a.store.GetAddressBook(a.ctx, req.BookID)
		return nil, book, err
	}
	existing, err := a.store.GetContact(a.ctx, req.ID)
	if err != nil {
		return nil, nil, err
	}
	book, err := a.store.GetAddressBook(a.ctx, existing.BookID)
	if err != nil {
		return nil, nil, err
	}
	return existing, book, nil
}

// contactFromRequest builds the contact to write. An edit starts from the
// stored card so properties Pelton has no field for survive; a new contact
// starts empty and gets a uid and a path.
func (a *App) contactFromRequest(req ContactRequest, existing *storage.Contact) pcarddav.Contact {
	contact := pcarddav.Contact{
		FullName:     strings.TrimSpace(req.FullName),
		Organization: strings.TrimSpace(req.Organization),
		Title:        strings.TrimSpace(req.Title),
		Note:         strings.TrimSpace(req.Note),
		Emails:       fromDTOValues(req.Emails),
		Phones:       fromDTOValues(req.Phones),
		MatrixID:     strings.TrimSpace(req.MatrixID),
	}
	if existing != nil {
		contact.Path = existing.Path
		contact.ETag = existing.ETag
		contact.UID = existing.UID
		if parsed, err := pcarddav.ParseCard(existing.Card); err == nil {
			contact.Card = parsed.Card
		}
		contact.Card = contact.ToCard()
		return contact
	}
	book, err := a.store.GetAddressBook(a.ctx, req.BookID)
	collection := "/"
	if err == nil {
		collection = a.collectionOf(*book)
	}
	contact.UID = newContactUID()
	contact.Path = contactPath(collection, contact.UID)
	contact.Card = contact.ToCard()
	return contact
}

// contactConflict builds the answer for a refused save: the server's copy and
// the user's, both as the editor understands them.
func (a *App) contactConflict(ctx context.Context, client *pcarddav.Client, book storage.AddressBook, mine pcarddav.Contact) (ContactConflictDTO, error) {
	books, err := a.bookIndex()
	if err != nil {
		return ContactConflictDTO{}, err
	}
	theirs, err := client.Get(ctx, mine.Path)
	if err != nil {
		// the server refused the write and will not show what is there, which
		// leaves nothing to compare. Report it rather than pretending.
		return ContactConflictDTO{}, err
	}
	return ContactConflictDTO{
		Conflict: true,
		Server:   toContactDTO(storedFrom(book.ID, theirs), books),
		Mine:     toContactDTO(storedFrom(book.ID, mine), books),
	}, nil
}

// conflictWithServer is the delete path's version: the user's copy is already
// stored, only the server's has to be fetched.
func (a *App) conflictWithServer(ctx context.Context, client *pcarddav.Client, book storage.AddressBook, path string, mine ContactDTO) (ContactConflictDTO, error) {
	books, err := a.bookIndex()
	if err != nil {
		return ContactConflictDTO{}, err
	}
	theirs, err := client.Get(ctx, path)
	if err != nil {
		return ContactConflictDTO{}, err
	}
	return ContactConflictDTO{
		Conflict: true,
		Server:   toContactDTO(storedFrom(book.ID, theirs), books),
		Mine:     mine,
	}, nil
}

// contactByPath reads back a stored contact so the ui gets ids and timestamps
// rather than what was sent.
func (a *App) contactByPath(bookID int64, path string) (ContactDTO, error) {
	contacts, err := a.store.ListContacts(a.ctx, bookID)
	if err != nil {
		return ContactDTO{}, err
	}
	books, err := a.bookIndex()
	if err != nil {
		return ContactDTO{}, err
	}
	for _, c := range contacts {
		if c.Path == path {
			return toContactDTO(c, books), nil
		}
	}
	return ContactDTO{}, storage.ErrContactNotFound
}

// storedFrom adapts a live contact to the stored shape the dto builder takes,
// so the conflict dialog renders both sides through one path.
func storedFrom(bookID int64, contact pcarddav.Contact) storage.Contact {
	card, err := pcarddav.EncodeCard(contact.Card)
	if err != nil {
		card = ""
	}
	return storage.Contact{
		BookID:       bookID,
		Path:         contact.Path,
		ETag:         contact.ETag,
		UID:          contact.UID,
		FullName:     contact.FullName,
		Organization: contact.Organization,
		Title:        contact.Title,
		Note:         contact.Note,
		Card:         card,
		Emails:       toStoredValues(contact.Emails),
		Phones:       toStoredValues(contact.Phones),
	}
}

// precondition picks what a save requires of the server: nothing when the user
// has seen the conflict and chose their version, the stored etag when updating,
// and "must not exist" when creating.
func precondition(force, updating bool, etag string) (pcarddav.Precondition, string) {
	switch {
	case force:
		return pcarddav.PreconditionNone, ""
	case updating:
		return pcarddav.PreconditionMatch, quoteETag(etag)
	default:
		return pcarddav.PreconditionNew, ""
	}
}

// quoteETag restores the quotes an etag is compared with on the wire. They are
// stripped on the way in so the stored value is the tag itself.
func quoteETag(etag string) string {
	if etag == "" || strings.HasPrefix(etag, `"`) || strings.HasPrefix(etag, "W/") {
		return etag
	}
	return `"` + etag + `"`
}
