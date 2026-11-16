// bind_contacts_merge.go finds contacts that are probably the same person and
// merges them. The merged card is written to the user's own CardDAV server and
// the others are deleted there, conditionally, through the same address book
// clients the editor uses; internal/contactmerge holds the logic.
package desktop

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	pcarddav "github.com/peltonapp/Pelton/internal/carddav"
	"github.com/peltonapp/Pelton/internal/contactmerge"
	"github.com/peltonapp/Pelton/internal/storage"
)

// mergeTimeout bounds one merge or undo, which is a handful of requests to
// each server involved.
const mergeTimeout = 2 * time.Minute

// mergeHistoryLimit is how many merges can still be undone in a session.
const mergeHistoryLimit = 10

var (
	errMergeChanged = errors.New("pelton: a contact changed on its server since it was last synced. Sync your contacts and try again")
	errMergeTarget  = errors.New("pelton: choose the address book the merged contact goes in")
	errMergeRead    = errors.New("pelton: an address book involved is read-only")
	errMergeUndone  = errors.New("pelton: this merge can no longer be undone")
)

// DuplicateReasonDTO is one thing a group of contacts has in common.
type DuplicateReasonDTO struct {
	// Kind is "email", "phone" or "name".
	Kind string `json:"kind"`
	// Value is the shared address, number or name.
	Value string `json:"value"`
	// Overlap backs up a shared name: "organization", "domain" or "empty".
	Overlap []string `json:"overlap"`
}

// DuplicateGroupDTO is a set of contacts that look like one person.
type DuplicateGroupDTO struct {
	Contacts []ContactDTO `json:"contacts"`
	// Score is 60 to 100: how sure the match is.
	Score   int                  `json:"score"`
	Reasons []DuplicateReasonDTO `json:"reasons"`
}

// DuplicatesDTO is the answer of FindDuplicateContacts.
type DuplicatesDTO struct {
	Groups []DuplicateGroupDTO `json:"groups"`
	// Dismissed is how many pairs the user marked as not duplicates, which
	// ResetDismissedDuplicates forgets.
	Dismissed int `json:"dismissed"`
}

// MergeContactsRequest is one merge: MergeIDs are folded into SurvivorID.
type MergeContactsRequest struct {
	SurvivorID int64   `json:"survivorId"`
	MergeIDs   []int64 `json:"mergeIds"`
	// TargetBookID is where the merged contact ends up. It is required when the
	// contacts are in more than one address book.
	TargetBookID int64 `json:"targetBookId"`
	// Choices maps "name", "organization", "title" and "note" to the id of the
	// contact the value comes from; -1 keeps both notes.
	Choices map[string]int64 `json:"choices"`
}

// MergeResultDTO is what a merge did.
type MergeResultDTO struct {
	Contact ContactDTO `json:"contact"`
	// Token undoes this merge with UndoContactMerge, for the rest of the
	// session.
	Token string `json:"token"`
	// Kept is how many merged contacts are still on their server because it
	// refused their deletion: they changed since the last sync. The merged
	// contact already holds their details.
	Kept int `json:"kept"`
}

// mergeHistory keeps the records of this session's merges, newest last, so one
// of them can be undone. It is memory only: after a restart the originals are
// no longer offered back.
type mergeHistory struct {
	mu      sync.Mutex
	seq     int
	order   []string
	records map[string]contactmerge.Record
}

func (h *mergeHistory) add(rec contactmerge.Record) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.records == nil {
		h.records = map[string]contactmerge.Record{}
	}
	h.seq++
	token := strconv.Itoa(h.seq)
	h.records[token] = rec
	h.order = append(h.order, token)
	for len(h.order) > mergeHistoryLimit {
		delete(h.records, h.order[0])
		h.order = h.order[1:]
	}
	return token
}

func (h *mergeHistory) take(token string) (contactmerge.Record, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	rec, ok := h.records[token]
	return rec, ok
}

func (h *mergeHistory) drop(token string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.records, token)
	for i, t := range h.order {
		if t == token {
			h.order = append(h.order[:i], h.order[i+1:]...)
			break
		}
	}
}

// FindDuplicateContacts lists the groups of contacts that look like one person,
// the surest first, leaving out pairs the user dismissed.
func (a *App) FindDuplicateContacts() (DuplicatesDTO, error) {
	if err := a.ready(); err != nil {
		return DuplicatesDTO{}, err
	}
	contacts, err := a.store.ListContacts(a.ctx, 0)
	if err != nil {
		return DuplicatesDTO{}, err
	}
	dismissed, err := a.store.DismissedContactPairs(a.ctx)
	if err != nil {
		return DuplicatesDTO{}, err
	}
	books, err := a.bookIndex()
	if err != nil {
		return DuplicatesDTO{}, err
	}
	byID := make(map[int64]storage.Contact, len(contacts))
	for _, c := range contacts {
		byID[c.ID] = c
	}
	out := DuplicatesDTO{Groups: []DuplicateGroupDTO{}, Dismissed: len(dismissed)}
	for _, g := range contactmerge.FindDuplicates(contacts, dismissed) {
		group := DuplicateGroupDTO{Score: g.Score, Contacts: []ContactDTO{}, Reasons: []DuplicateReasonDTO{}}
		for _, id := range g.IDs {
			group.Contacts = append(group.Contacts, toContactDTO(byID[id], books))
		}
		for _, r := range g.Reasons {
			reason := DuplicateReasonDTO{Kind: string(r.Kind), Value: r.Value, Overlap: []string{}}
			for _, o := range r.Overlap {
				reason.Overlap = append(reason.Overlap, string(o))
			}
			group.Reasons = append(group.Reasons, reason)
		}
		out.Groups = append(out.Groups, group)
	}
	return out, nil
}

// MergeContacts merges contacts into one: the merged card is written to the
// server first, then the others are deleted there and here. See
// contactmerge.Run for the order and what a failure part way leaves behind.
func (a *App) MergeContacts(req MergeContactsRequest) (MergeResultDTO, error) {
	if err := a.ready(); err != nil {
		return MergeResultDTO{}, err
	}
	ctx, cancel := context.WithTimeout(a.sessionCtx(), mergeTimeout)
	defer cancel()

	choices := make(contactmerge.Choices, len(req.Choices))
	for field, id := range req.Choices {
		choices[contactmerge.Field(field)] = id
	}
	res, err := contactmerge.Run(ctx, a.mergeEnv(), contactmerge.Request{
		SurvivorID:   req.SurvivorID,
		MergeIDs:     req.MergeIDs,
		TargetBookID: req.TargetBookID,
		Choices:      choices,
	})
	if err != nil {
		return MergeResultDTO{}, mergeError(err)
	}
	books, err := a.bookIndex()
	if err != nil {
		return MergeResultDTO{}, err
	}
	return MergeResultDTO{
		Contact: toContactDTO(res.Contact, books),
		Token:   a.merges.add(res.Record),
		Kept:    len(res.Record.Kept),
	}, nil
}

// UndoContactMerge puts the contacts of a merge back, on the servers and here,
// and returns them. It works until the app restarts. A contact that was
// deleted from a synced book is written there again as a new resource: other
// devices see it come back, and changes they made to the merged card in the
// meantime stop the undo rather than being overwritten.
func (a *App) UndoContactMerge(token string) ([]ContactDTO, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	rec, ok := a.merges.take(token)
	if !ok {
		return nil, errMergeUndone
	}
	ctx, cancel := context.WithTimeout(a.sessionCtx(), mergeTimeout)
	defer cancel()
	restored, err := contactmerge.Undo(ctx, a.mergeEnv(), rec)
	if err != nil {
		return nil, mergeError(err)
	}
	a.merges.drop(token)
	books, err := a.bookIndex()
	if err != nil {
		return nil, err
	}
	out := make([]ContactDTO, 0, len(restored))
	for _, c := range restored {
		out = append(out, toContactDTO(c, books))
	}
	return out, nil
}

// DismissDuplicateContacts marks every pair among the given contacts as not
// duplicates, for good: the duplicates view stops offering them.
func (a *App) DismissDuplicateContacts(ids []int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	var keys []string
	for _, id := range ids {
		c, err := a.store.GetContact(a.ctx, id)
		if err != nil {
			return err
		}
		keys = append(keys, storage.ContactKey(*c))
	}
	var pairs []storage.ContactPair
	for i := range keys {
		for j := i + 1; j < len(keys); j++ {
			pairs = append(pairs, storage.NewContactPair(keys[i], keys[j]))
		}
	}
	return a.store.DismissContactPairs(a.ctx, pairs)
}

// ResetDismissedDuplicates forgets every "not duplicates" answer, so those
// groups show again. It returns how many pairs were forgotten.
func (a *App) ResetDismissedDuplicates() (int, error) {
	if err := a.ready(); err != nil {
		return 0, err
	}
	return a.store.ResetDismissedContactPairs(a.ctx)
}

// mergeEnv wires a merge to the store and to the address book clients, which
// read their passwords from the keyring like every other contact write.
func (a *App) mergeEnv() contactmerge.Env {
	return contactmerge.Env{
		Store: a.store,
		Open: func(ctx context.Context, book storage.AddressBook) (contactmerge.Remote, error) {
			client, err := a.addressBookClient(ctx, book)
			if err != nil {
				return nil, err
			}
			return client, nil
		},
		Collection: a.collectionOf,
	}
}

// mergeError turns the package's errors into the sentences the user sees.
func mergeError(err error) error {
	switch {
	case errors.Is(err, contactmerge.ErrServerChanged):
		return errMergeChanged
	case errors.Is(err, contactmerge.ErrNeedsTarget):
		return errMergeTarget
	case errors.Is(err, contactmerge.ErrReadOnly):
		return fmt.Errorf("%w (%v)", errMergeRead, err)
	case errors.Is(err, storage.ErrContactNotFound):
		return errors.New("pelton: a contact in this merge no longer exists. Reload and try again")
	case errors.Is(err, pcarddav.ErrConflict):
		return errMergeChanged
	}
	return err
}
