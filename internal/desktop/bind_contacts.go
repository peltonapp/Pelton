// bind_contacts.go is the CardDAV address book (#168): contacts the user keeps
// on their own server, read into Pelton, edited here and written back.
//
// The harvested address book is a separate thing and stays: it is every address
// seen in mail, which is useful for autocomplete and is not an address book.
// These are the contacts the user actually maintains, so where the two carry
// the same address the contact's name is the one shown.
package desktop

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"
	"uuid"

	pcarddav "github.com/peltonapp/Pelton/internal/carddav"
	"github.com/peltonapp/Pelton/internal/credentials"
	"github.com/peltonapp/Pelton/internal/storage"
)

// contactsTimeout bounds one address book request. Contact sync is small next
// to mail, and a server that has not answered in half a minute is down.
const contactsTimeout = 30 * time.Second

var (
	errBookNeedsURL      = errors.New("pelton: an address book needs a server address")
	errBookReadOnly      = errors.New("pelton: this address book is read-only")
	errContactNeedsBook  = errors.New("pelton: choose an address book for this contact")
	errContactNeedsField = errors.New("pelton: a contact needs a name or an address")
)

// AddressBookDTO is one configured CardDAV address book for the ui.
type AddressBookDTO struct {
	ID int64 `json:"id"`
	// AccountID is the mail account this book was discovered from, 0 for one
	// added by hand.
	AccountID      int64  `json:"accountId"`
	Name           string `json:"name"`
	URL            string `json:"url"`
	CollectionPath string `json:"collectionPath"`
	Username       string `json:"username"`
	ReadOnly       bool   `json:"readOnly"`
	// LastSync is rfc3339, empty when it has never synced. LastError is the
	// last failure in the server's own words, empty once one succeeds.
	LastSync  string `json:"lastSync"`
	LastError string `json:"lastError"`
	// ContactCount is how many contacts this book holds locally.
	ContactCount int `json:"contactCount"`
	// HasPassword is false for a book whose keyring entry is missing, which
	// cannot sync until one is entered.
	HasPassword bool `json:"hasPassword"`
}

// ContactDTO is one contact for the list and the editor.
type ContactDTO struct {
	ID     int64 `json:"id"`
	BookID int64 `json:"bookId"`
	// BookName saves the ui joining books to contacts to label a row.
	BookName     string            `json:"bookName"`
	UID          string            `json:"uid"`
	FullName     string            `json:"fullName"`
	Organization string            `json:"organization"`
	Title        string            `json:"title"`
	Note         string            `json:"note"`
	Emails       []ContactValueDTO `json:"emails"`
	Phones       []ContactValueDTO `json:"phones"`
	ReadOnly     bool              `json:"readOnly"`
	Updated      string            `json:"updated"`
	// Extra lists the vCard properties Pelton has no field for but keeps on
	// every write, so the editor can say the card carries more than this.
	Extra []string `json:"extra"`
}

// ContactValueDTO is one address or number with its label.
type ContactValueDTO struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// ContactConflictDTO is what a refused write returns: the contact as the server
// now holds it, next to the version the user tried to save. The ui shows both
// and the user picks (#168).
type ContactConflictDTO struct {
	Conflict bool       `json:"conflict"`
	Server   ContactDTO `json:"server"`
	Mine     ContactDTO `json:"mine"`
	// Saved is the stored contact when there was no conflict.
	Saved ContactDTO `json:"saved"`
}

// AddressBookRequest creates or updates a book. An empty Password on update
// leaves the stored one alone.
type AddressBookRequest struct {
	ID             int64  `json:"id"`
	AccountID      int64  `json:"accountId"`
	Name           string `json:"name"`
	URL            string `json:"url"`
	CollectionPath string `json:"collectionPath"`
	Username       string `json:"username"`
	Password       string `json:"password"`
}

// DiscoveredBookDTO is one address book offered by discovery, before the user
// has decided to add it.
type DiscoveredBookDTO struct {
	Name           string `json:"name"`
	URL            string `json:"url"`
	CollectionPath string `json:"collectionPath"`
	// Exists is true when this collection is already configured, so the ui can
	// grey it rather than offering a duplicate.
	Exists bool `json:"exists"`
}

// ListAddressBooks returns every configured address book.
func (a *App) ListAddressBooks() ([]AddressBookDTO, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	books, err := a.store.ListAddressBooks(a.ctx)
	if err != nil {
		return nil, err
	}
	out := make([]AddressBookDTO, 0, len(books))
	for _, b := range books {
		contacts, err := a.store.ListContacts(a.ctx, b.ID)
		if err != nil {
			return nil, err
		}
		password, err := credentials.LoadAddressBookPassword(b.ID)
		if err != nil {
			a.log.Warn("read address book password", "book", b.ID, "err", err)
		}
		out = append(out, AddressBookDTO{
			ID:             b.ID,
			AccountID:      b.AccountID,
			Name:           b.Name,
			URL:            b.URL,
			CollectionPath: b.CollectionPath,
			Username:       b.Username,
			ReadOnly:       b.ReadOnly,
			LastSync:       b.LastSyncAt,
			LastError:      b.LastError,
			ContactCount:   len(contacts),
			HasPassword:    password != "",
		})
	}
	return out, nil
}

// DiscoverAddressBooks asks a server what address books the given login has.
// The url may be a bare domain, in which case .well-known/carddav resolves it;
// an account id instead uses that mailbox's address to find its domain, which
// is what makes "add contacts for this mailbox" one click on servers that host
// both.
func (a *App) DiscoverAddressBooks(req AddressBookRequest) ([]DiscoveredBookDTO, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	target := strings.TrimSpace(req.URL)
	username := strings.TrimSpace(req.Username)
	httpClient, err := a.contactsHTTPClient(req.AccountID)
	if err != nil {
		return nil, err
	}
	if req.AccountID != 0 {
		account, err := a.store.GetAccount(a.ctx, req.AccountID)
		if err != nil {
			return nil, err
		}
		if target == "" {
			ctx, cancel := context.WithTimeout(a.ctx, contactsTimeout)
			defer cancel()
			discovered, err := pcarddav.Discover(ctx, httpClient, account.Email)
			if err != nil {
				return nil, err
			}
			target = discovered
		}
		if username == "" {
			username = loginName(*account)
		}
	}
	if target == "" {
		return nil, errBookNeedsURL
	}

	ctx, cancel := context.WithTimeout(a.ctx, contactsTimeout)
	defer cancel()
	client, err := pcarddav.Connect(ctx, pcarddav.Config{
		URL:      target,
		Username: username,
		Password: req.Password,
		HTTP:     httpClient,
	})
	if err != nil {
		return nil, err
	}
	found, err := client.Books(ctx)
	if err != nil {
		return nil, err
	}

	existing, err := a.store.ListAddressBooks(a.ctx)
	if err != nil {
		return nil, err
	}
	have := make(map[string]bool, len(existing))
	for _, b := range existing {
		have[b.URL+"|"+b.CollectionPath] = true
	}

	out := make([]DiscoveredBookDTO, 0, len(found))
	for _, b := range found {
		out = append(out, DiscoveredBookDTO{
			Name:           b.Name,
			URL:            target,
			CollectionPath: b.Path,
			Exists:         have[target+"|"+b.Path],
		})
	}
	return out, nil
}

// AddAddressBook stores a book and syncs it once, so the contacts are there
// before the user goes looking for them.
func (a *App) AddAddressBook(req AddressBookRequest) (AddressBookDTO, error) {
	if err := a.ready(); err != nil {
		return AddressBookDTO{}, err
	}
	if strings.TrimSpace(req.URL) == "" {
		return AddressBookDTO{}, errBookNeedsURL
	}
	book := storage.AddressBook{
		AccountID:      req.AccountID,
		Name:           strings.TrimSpace(req.Name),
		URL:            strings.TrimSpace(req.URL),
		CollectionPath: strings.TrimSpace(req.CollectionPath),
		Username:       strings.TrimSpace(req.Username),
	}
	id, err := a.store.CreateAddressBook(a.ctx, &book)
	if err != nil {
		return AddressBookDTO{}, err
	}
	if err := credentials.StoreAddressBookPassword(id, req.Password); err != nil {
		// a book nobody can log into is worse than no book, so it is rolled back.
		_ = a.store.DeleteAddressBook(a.ctx, id)
		return AddressBookDTO{}, err
	}
	if err := a.syncAddressBook(a.sessionCtx(), book); err != nil {
		a.log.Warn("first contact sync", "book", id, "err", err)
	}
	return a.addressBookDTO(id)
}

// UpdateAddressBook saves a book's name, location and login. An empty password
// leaves the stored one alone, so renaming a book does not mean retyping it.
func (a *App) UpdateAddressBook(req AddressBookRequest) (AddressBookDTO, error) {
	if err := a.ready(); err != nil {
		return AddressBookDTO{}, err
	}
	book, err := a.store.GetAddressBook(a.ctx, req.ID)
	if err != nil {
		return AddressBookDTO{}, err
	}
	if strings.TrimSpace(req.URL) == "" {
		return AddressBookDTO{}, errBookNeedsURL
	}
	book.Name = strings.TrimSpace(req.Name)
	book.URL = strings.TrimSpace(req.URL)
	book.CollectionPath = strings.TrimSpace(req.CollectionPath)
	book.Username = strings.TrimSpace(req.Username)
	if err := a.store.UpdateAddressBook(a.ctx, *book); err != nil {
		return AddressBookDTO{}, err
	}
	if req.Password != "" {
		if err := credentials.StoreAddressBookPassword(book.ID, req.Password); err != nil {
			return AddressBookDTO{}, err
		}
	}
	return a.addressBookDTO(book.ID)
}

// RemoveAddressBook forgets a book and the contacts it held here. The server is
// not touched: this removes Pelton's copy, not the user's address book.
func (a *App) RemoveAddressBook(id int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	if err := a.store.DeleteAddressBook(a.ctx, id); err != nil {
		return err
	}
	if err := credentials.DeleteAddressBookPassword(id); err != nil {
		a.log.Warn("delete address book password", "book", id, "err", err)
	}
	return nil
}

// ListContacts returns the contacts in a book, or in every book when bookID is
// 0.
func (a *App) ListContacts(bookID int64) ([]ContactDTO, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	contacts, err := a.store.ListContacts(a.ctx, bookID)
	if err != nil {
		return nil, err
	}
	books, err := a.bookIndex()
	if err != nil {
		return nil, err
	}
	out := make([]ContactDTO, 0, len(contacts))
	for _, c := range contacts {
		out = append(out, toContactDTO(c, books))
	}
	return out, nil
}

// SyncContacts refreshes every address book now, for the button in the ui. It
// returns when the last book is done, since the user pressed it and is waiting
// on the answer.
func (a *App) SyncContacts() error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.syncAllAddressBooks(a.sessionCtx())
}

// syncContactsInBackground kicks off a contacts refresh alongside a mail sync,
// but only when there is an address book to refresh. An install with none, which
// is most of them, starts no goroutine at all rather than one that wakes up to
// find nothing to do.
func (a *App) syncContactsInBackground() {
	books, err := a.store.ListAddressBooks(a.ctx)
	if err != nil || len(books) == 0 {
		if err != nil {
			a.log.Warn("list address books", "err", err)
		}
		return
	}
	goSafe("syncing contacts", func() {
		if err := a.syncAllAddressBooks(a.sessionCtx()); err != nil {
			a.log.Warn("contact sync", "err", err)
		}
	})
}

// syncAllAddressBooks refreshes every book, recording each outcome. One book
// failing does not stop the others: they are separate servers.
//
// It runs on the profile's session context, so switching profile cancels a
// contacts sync the way it cancels a mail sync: the books belong to the profile
// that was open, and finishing them into a store that has moved on is worse
// than stopping.
func (a *App) syncAllAddressBooks(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	books, err := a.store.ListAddressBooks(ctx)
	if err != nil {
		return err
	}
	var failed error
	for _, book := range books {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := a.syncAddressBook(ctx, book); err != nil {
			a.log.Warn("contact sync", "book", book.ID, "err", err)
			failed = err
		}
	}
	return failed
}

// syncAddressBook pulls one book's changes and records the outcome. A server
// that rejects its own sync token is answered with a full read, which is the
// only recovery it offers.
func (a *App) syncAddressBook(ctx context.Context, book storage.AddressBook) error {
	err := a.syncAddressBookOnce(ctx, book, book.SyncToken)
	if err != nil && book.SyncToken != "" && ctx.Err() == nil {
		// the token may simply have expired, which is not a failure worth
		// showing the user until a token-free read fails too.
		if err := a.store.ClearBookContacts(ctx, book.ID); err != nil {
			return err
		}
		err = a.syncAddressBookOnce(ctx, book, "")
	}
	if err != nil && ctx.Err() == nil {
		if recordErr := a.store.RecordAddressBookSync(ctx, book.ID, "", err.Error()); recordErr != nil {
			return recordErr
		}
	}
	return err
}

func (a *App) syncAddressBookOnce(ctx context.Context, book storage.AddressBook, token string) error {
	client, err := a.addressBookClient(ctx, book)
	if err != nil {
		return err
	}
	reqCtx, cancel := context.WithTimeout(ctx, contactsTimeout)
	defer cancel()

	changes, err := client.Sync(reqCtx, a.collectionOf(book), token)
	if err != nil {
		return err
	}
	for _, contact := range changes.Updated {
		if err := a.storeContact(ctx, book.ID, contact); err != nil {
			return err
		}
	}
	for _, gone := range changes.Deleted {
		if err := a.store.DeleteContactByPath(ctx, book.ID, gone); err != nil {
			return err
		}
	}
	return a.store.RecordAddressBookSync(ctx, book.ID, changes.Token, "")
}

// storeContact writes one synced contact, keeping its card whole.
func (a *App) storeContact(ctx context.Context, bookID int64, contact pcarddav.Contact) error {
	card, err := pcarddav.EncodeCard(contact.Card)
	if err != nil {
		return err
	}
	stored := storage.Contact{
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
	return a.store.SaveContact(a.ctx, &stored)
}

// addressBookClient opens a connection for a book, reading its password from
// the keyring.
func (a *App) addressBookClient(ctx context.Context, book storage.AddressBook) (*pcarddav.Client, error) {
	password, err := credentials.LoadAddressBookPassword(book.ID)
	if err != nil {
		return nil, err
	}
	httpClient, err := a.contactsHTTPClient(book.AccountID)
	if err != nil {
		return nil, err
	}
	connectCtx, cancel := context.WithTimeout(ctx, contactsTimeout)
	defer cancel()
	return pcarddav.Connect(connectCtx, pcarddav.Config{
		URL:      book.URL,
		Username: book.Username,
		Password: password,
		HTTP:     httpClient,
	})
}

// collectionOf is the path sync and writes address. A book added from a url
// that already pointed at the collection has no separate path.
func (a *App) collectionOf(book storage.AddressBook) string {
	if book.CollectionPath != "" {
		return book.CollectionPath
	}
	return "/"
}

// contactPath is where a new contact is written: its uid under the collection,
// which is what every other client does and what makes the file recognisable
// on the server.
func contactPath(collection, uid string) string {
	return path.Join(collection, uid+".vcf")
}

// newContactUID is the identity a contact keeps for life, independent of where
// it is stored.
func newContactUID() string {
	return uuid.NewV4().String()
}

func toStoredValues(values []pcarddav.Labelled) []storage.ContactValue {
	out := make([]storage.ContactValue, 0, len(values))
	for _, v := range values {
		out = append(out, storage.ContactValue{Value: v.Value, Label: v.Label})
	}
	return out
}

func toDTOValues(values []storage.ContactValue) []ContactValueDTO {
	out := make([]ContactValueDTO, 0, len(values))
	for _, v := range values {
		out = append(out, ContactValueDTO{Value: v.Value, Label: v.Label})
	}
	return out
}

func fromDTOValues(values []ContactValueDTO) []pcarddav.Labelled {
	out := make([]pcarddav.Labelled, 0, len(values))
	for _, v := range values {
		if strings.TrimSpace(v.Value) == "" {
			continue
		}
		out = append(out, pcarddav.Labelled{Value: strings.TrimSpace(v.Value), Label: v.Label})
	}
	return out
}

// bookIndex maps book ids to their names and whether they take writes.
func (a *App) bookIndex() (map[int64]storage.AddressBook, error) {
	books, err := a.store.ListAddressBooks(a.ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]storage.AddressBook, len(books))
	for _, b := range books {
		out[b.ID] = b
	}
	return out, nil
}

func (a *App) addressBookDTO(id int64) (AddressBookDTO, error) {
	books, err := a.ListAddressBooks()
	if err != nil {
		return AddressBookDTO{}, err
	}
	for _, b := range books {
		if b.ID == id {
			return b, nil
		}
	}
	return AddressBookDTO{}, fmt.Errorf("pelton: address book %d disappeared", id)
}

func toContactDTO(c storage.Contact, books map[int64]storage.AddressBook) ContactDTO {
	book := books[c.BookID]
	return ContactDTO{
		ID:           c.ID,
		BookID:       c.BookID,
		BookName:     book.Name,
		UID:          c.UID,
		FullName:     c.FullName,
		Organization: c.Organization,
		Title:        c.Title,
		Note:         c.Note,
		Emails:       toDTOValues(c.Emails),
		Phones:       toDTOValues(c.Phones),
		ReadOnly:     book.ReadOnly,
		Updated:      c.UpdatedAt,
		Extra:        extraProperties(c.Card),
	}
}

// extraProperties lists the vCard property names on a card that Pelton has no
// field for. The editor shows them so it is clear the card holds more than the
// form, and that saving keeps them.
func extraProperties(card string) []string {
	parsed, err := pcarddav.ParseCard(card)
	if err != nil || parsed.Card == nil {
		return nil
	}
	var out []string
	for name := range parsed.Card {
		if knownProperty(name) {
			continue
		}
		out = append(out, name)
	}
	return out
}

// knownProperty reports whether the editor has a field for a vCard property,
// or whether it is structural.
func knownProperty(name string) bool {
	switch strings.ToUpper(name) {
	case "VERSION", "UID", "FN", "N", "EMAIL", "TEL", "ORG", "TITLE", "NOTE",
		"PRODID", "REV":
		return true
	}
	return false
}
