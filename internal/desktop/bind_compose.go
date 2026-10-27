package desktop

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/peltonapp/Pelton/internal/crypto"
	"github.com/peltonapp/Pelton/internal/smtp"
	"github.com/peltonapp/Pelton/internal/storage"
)

// ErrSendAtInvalid is returned when a ComposeRequest.SendAt string does not
// parse as RFC3339.
var ErrSendAtInvalid = fmt.Errorf("pelton: sendAt is not a valid RFC3339 timestamp")

// ErrSendAtPast is returned when a ComposeRequest.SendAt is not in the future.
var ErrSendAtPast = fmt.Errorf("pelton: sendAt must be in the future")

// AddressDTO is one mail address with an optional display name.
type AddressDTO struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// ComposeAttachment is a file the user attached. Content is base64 so it crosses
// the bindings boundary as a plain string. Inline marks an image referenced from
// the html body by ContentID.
type ComposeAttachment struct {
	Filename      string `json:"filename"`
	ContentType   string `json:"contentType"`
	ContentBase64 string `json:"contentBase64"`
	Inline        bool   `json:"inline"`
	ContentID     string `json:"contentId"`
}

// ComposeRequest is the full input to send or save a message. The frontend
// produces both Text and HTML: for markdown mode it renders markdown to html
// itself and sends the markdown source as Text; for plaintext mode HTML is
// empty. Threading fields are copied from the message being replied to.
type ComposeRequest struct {
	AccountID   int64               `json:"accountId"`
	To          []AddressDTO        `json:"to"`
	Cc          []AddressDTO        `json:"cc"`
	Bcc         []AddressDTO        `json:"bcc"`
	Subject     string              `json:"subject"`
	Text        string              `json:"text"`
	HTML        string              `json:"html"`
	InReplyTo   string              `json:"inReplyTo"`
	References  []string            `json:"references"`
	Attachments []ComposeAttachment `json:"attachments"`
	// SendAt is an optional RFC3339 timestamp for a user-scheduled send ("send
	// later"). Empty means send immediately, subject to the undo-send delay
	// below. When set it must be in the future.
	SendAt string `json:"sendAt"`
	// Protection is the pgp treatment for this one message: "none", "sign",
	// "encrypt" or "signencrypt". Empty means none. The compose window resolves
	// it from the account default and what the keys actually allow, and the
	// send refuses rather than falling back to plaintext.
	Protection string `json:"protection"`
	// DraftMessageID is the Message-ID of the newest server copy of this
	// draft, empty until one has been uploaded. Each upload gets a new one, and
	// a sent message gets a Message-ID of its own.
	DraftMessageID string `json:"draftMessageId"`
}

// SendMessage builds the mime message and enqueues it in the durable outbox. The
// background worker transmits it when smtp credentials are configured. This call
// returns once the message is safely queued, so the ui can confirm immediately.
//
// Signing and encryption happen here, at enqueue time, not when the worker
// transmits: the queue holds the finished bytes, so a message sits in the
// outbox already protected and a passphrase is only ever needed while the user
// is still in front of the send button. Anything that would stop the protection
// from being applied fails the send outright, because the alternative is
// quietly putting a message the user marked as protected on the wire in
// plaintext.
func (a *App) SendMessage(req ComposeRequest) (int64, error) {
	if err := a.ready(); err != nil {
		return 0, err
	}

	msg, err := a.buildMessage(req)
	if err != nil {
		return 0, err
	}

	notBefore, err := resolveNotBefore(req.SendAt, a.intSetting(settingSendDelay, 0), time.Now().UTC())
	if err != nil {
		return 0, err
	}

	account, err := a.store.GetAccount(a.ctx, req.AccountID)
	if err != nil {
		return 0, err
	}
	mode, opts, engine, err := a.protectionOptions(*account, req)
	if err != nil {
		return 0, err
	}

	id, err := smtp.Enqueue(a.ctx, a.queue, req.AccountID, msg, engine, mode, opts, notBefore)
	if err != nil {
		return 0, err
	}
	a.emit(EventOutboxChanged, nil)

	// harvest every recipient into the address book so autocomplete learns from
	// who the user writes to. best effort: a failure here must not fail the send.
	// Turned off, nothing is remembered and autocomplete offers only contacts.
	for _, group := range harvestGroups(a.harvestAddresses(), req) {
		for _, addr := range group {
			if err := a.store.RecordAddress(a.ctx, addr.Email, addr.Name); err != nil {
				a.log.Error("record recipient address", "email", addr.Email, "err", err)
			}
		}
	}
	return id, nil
}

// harvestGroups is the recipient lists to learn from, or none when learning
// from mail is off.
func harvestGroups(harvest bool, req ComposeRequest) [][]AddressDTO {
	if !harvest {
		return nil
	}
	return [][]AddressDTO{req.To, req.Cc, req.Bcc}
}

// CancelSend pulls a still-queued message back out of the outbox, returning
// whether it was cancelled. It only succeeds while the message is waiting in its
// undo-send delay window; once the worker has claimed it for sending it cannot be
// recalled. The frontend reopens the original draft after a successful cancel.
func (a *App) CancelSend(id int64) (bool, error) {
	if err := a.ready(); err != nil {
		return false, err
	}
	cancelled, err := a.queue.Cancel(a.ctx, id)
	if err != nil {
		return false, err
	}
	if cancelled {
		a.emit(EventOutboxChanged, nil)
	}
	return cancelled, nil
}

// RetrySend puts a failed message back in the send queue, returning whether it
// was requeued. False means it is no longer failed, which the ui treats as
// "already handled" rather than an error.
func (a *App) RetrySend(id int64) (bool, error) {
	if err := a.ready(); err != nil {
		return false, err
	}
	retried, err := a.queue.Retry(a.ctx, id)
	if err != nil {
		return false, err
	}
	if retried {
		a.emit(EventOutboxChanged, nil)
	}
	return retried, nil
}

// DiscardFailedSend removes a failed message from the outbox, returning whether
// it was removed. The message is not recoverable afterwards, so the ui confirms
// before calling this.
func (a *App) DiscardFailedSend(id int64) (bool, error) {
	if err := a.ready(); err != nil {
		return false, err
	}
	discarded, err := a.queue.Discard(a.ctx, id)
	if err != nil {
		return false, err
	}
	if discarded {
		a.emit(EventOutboxChanged, nil)
	}
	return discarded, nil
}

// ClearSentOutbox removes rows already marked sent. The ui calls it after showing
// the brief "sent" confirmation so the queue does not keep completed messages.
func (a *App) ClearSentOutbox() error {
	if err := a.ready(); err != nil {
		return err
	}
	n, err := a.queue.PruneSent(a.ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		a.emit(EventOutboxChanged, nil)
	}
	return nil
}

// resolveNotBefore decides when an outbox message becomes eligible to send. An
// explicit sendAt (RFC3339) takes precedence over the undo-send delay: a
// message the user deliberately scheduled shouldn't also sit through the short
// undo window on top of its scheduled time. sendAt must parse and be strictly
// after now, or an error is returned. With sendAt empty, the undo-send delay
// (delaySeconds, 0 meaning none) is applied as before.
func resolveNotBefore(sendAt string, delaySeconds int, now time.Time) (time.Time, error) {
	if sendAt == "" {
		if delaySeconds > 0 {
			return now.Add(time.Duration(delaySeconds) * time.Second), nil
		}
		return time.Time{}, nil
	}
	when, err := time.Parse(time.RFC3339, sendAt)
	if err != nil {
		return time.Time{}, ErrSendAtInvalid
	}
	if !when.After(now) {
		return time.Time{}, ErrSendAtPast
	}
	return when.UTC(), nil
}

// buildMessage assembles an smtp.Message from a compose request, resolving the
// from address from the account and decoding attachment bytes.
func (a *App) buildMessage(req ComposeRequest) (*smtp.Message, error) {
	acc, err := a.store.GetAccount(a.ctx, req.AccountID)
	if err != nil {
		return nil, err
	}

	atts, err := decodeAttachments(req.Attachments)
	if err != nil {
		return nil, err
	}

	return &smtp.Message{
		From:        smtp.Address{Name: acc.DisplayName, Email: acc.Email},
		To:          toBuilderAddresses(req.To),
		Cc:          toBuilderAddresses(req.Cc),
		Bcc:         toBuilderAddresses(req.Bcc),
		Subject:     req.Subject,
		Text:        req.Text,
		HTML:        req.HTML,
		Attachments: atts,
		InReplyTo:   req.InReplyTo,
		References:  req.References,
	}, nil
}

// DraftDTO is a locally saved unsent draft. Drafts are stored in the settings
// table as json. UploadDraft also puts a copy in the account's Drafts folder on
// the server; the local one stays the copy Pelton edits, since it keeps what
// the server copy cannot be trusted to bring back.
// A draft of a message the user chose to encrypt is not stored as it stands:
// the point of encrypting it was that its contents are not to sit around
// readable, and an unsent draft is no different from a sent one in that
// respect. Sealed holds the whole request encrypted to the sender's own key,
// Request keeps only the Message-ID that links it to its encrypted server copy,
// and Locked says the passphrase is needed to read it back. Ordinary drafts
// are unchanged.
type DraftDTO struct {
	ID      int64          `json:"id"`
	SavedAt string         `json:"savedAt"`
	Request ComposeRequest `json:"request"`
	// Locked is true when this draft is sealed and could not be opened with a
	// passphrase Pelton currently holds.
	Locked bool `json:"locked"`
	// AccountID and Protection survive sealing so the drafts list can still say
	// which mailbox a locked draft belongs to and that it is protected.
	AccountID  int64  `json:"accountId"`
	Protection string `json:"protection"`
}

// storedDraft is a draft as it sits in the settings table. It is kept separate
// from DraftDTO so the sealed ciphertext has somewhere to live that the
// frontend never sees: the ui gets Locked and the metadata, never the armor.
type storedDraft struct {
	ID      int64          `json:"id"`
	SavedAt string         `json:"savedAt"`
	Request ComposeRequest `json:"request"`
	// Sealed is the armored, encrypted request, set only for a draft of a
	// message the user chose to encrypt. Request then holds nothing but the
	// account, the protection and the Message-ID of its newest server copy.
	Sealed     string `json:"sealed,omitempty"`
	AccountID  int64  `json:"accountId"`
	Protection string `json:"protection"`
	// Token tells this draft apart from any other, its id being reused once it
	// is deleted and the same in every profile.
	Token string `json:"token,omitempty"`
	// ServerCopies are the copies of this draft that may be on a server, kept
	// in the clear so they can be removed without the passphrase. The one
	// Request.DraftMessageID names is among them.
	ServerCopies []serverCopy `json:"serverCopies,omitempty"`
}

// draftCopy names one server copy of a draft: the account and folder it went
// up to and its Message-ID. Mailbox is empty for a copy recorded before
// folders were.
type draftCopy struct {
	AccountID int64  `json:"accountId"`
	MessageID string `json:"messageId"`
	Mailbox   string `json:"mailbox,omitempty"`
}

// draftKey names a draft in memory: the settings scope of the profile it
// belongs to and its token.
type draftKey struct {
	scope int64
	token string
}

// newDraftToken returns a random token for a new draft.
func newDraftToken() string {
	return rand.Text()
}

// serverCopy is a draftCopy that may be on the server, and whether it went up
// encrypted.
type serverCopy struct {
	draftCopy
	Encrypted bool `json:"encrypted"`
}

// draftRemovalsKey is the settings key holding the json array of draft copies
// to remove from their servers, kept until each removal has worked. It lives
// in the main profile's settings whatever profile the app is in, so every
// profile drains it and deleting a profile does not lose it.
const draftRemovalsKey = "draft_server_removals"

// draftsKey is the settings key holding the json array of local drafts.
const draftsKey = "local_drafts"

// maxSafeDraftID is the largest id a JavaScript number holds exactly. Draft ids
// travel to the frontend as numbers, and a larger one comes back rounded to a
// different id.
const maxSafeDraftID int64 = 1<<53 - 1

// ErrDraftNotFound is returned when a draft is saved under an id that is not in
// the list, instead of reporting a save that wrote nothing.
var ErrDraftNotFound = errors.New("pelton: this draft no longer exists")

// nextDraftID is one above the highest id in use.
func nextDraftID(drafts []storedDraft) int64 {
	var highest int64
	for _, d := range drafts {
		highest = max(highest, d.ID)
	}
	return highest + 1
}

// renumberUnsafeDraftIDs gives every draft whose id a JavaScript number cannot
// hold a fresh small id, and reports whether it changed anything.
func renumberUnsafeDraftIDs(drafts []storedDraft) bool {
	var next int64
	for _, d := range drafts {
		if d.ID <= maxSafeDraftID {
			next = max(next, d.ID)
		}
	}
	changed := false
	for i := range drafts {
		if drafts[i].ID > maxSafeDraftID {
			next++
			drafts[i].ID = next
			changed = true
		}
	}
	return changed
}

// SaveDraft stores a compose request as a local draft and returns its id. An id
// of 0 in the request creates a new draft; a non zero id replaces that draft.
// The Message-ID of its server copy is UploadDraft's to set; one the composer
// brings for a new draft is recorded as a copy that may be on the server.
func (a *App) SaveDraft(id int64, req ComposeRequest) (int64, error) {
	if err := a.ready(); err != nil {
		return 0, err
	}
	if req.DraftMessageID != "" {
		if err := checkMessageID(req.DraftMessageID); err != nil {
			return 0, err
		}
	}
	a.draftsMu.Lock()
	defer a.draftsMu.Unlock()
	scope := a.store.SettingsScope()
	drafts, err := a.loadDraftsIn(scope)
	if err != nil {
		return 0, err
	}
	at := -1
	token := newDraftToken()
	var copies []serverCopy
	if id != 0 {
		if at = slices.IndexFunc(drafts, func(d storedDraft) bool { return d.ID == id }); at < 0 {
			return 0, ErrDraftNotFound
		}
		// the server copies are known here, whatever the composer still knows.
		token, copies = drafts[at].Token, drafts[at].ServerCopies
		req.DraftMessageID = drafts[at].Request.DraftMessageID
	} else if req.DraftMessageID != "" {
		// taken for plaintext, the side to err on.
		copies = []serverCopy{{draftCopy{AccountID: req.AccountID, MessageID: req.DraftMessageID}, false}}
	}
	// a draft set to encrypt has its plaintext copies queued for removal in
	// this same write, so none outlives a quit before the next upload.
	var gone []draftCopy
	if encrypts(req.Protection) {
		var kept []serverCopy
		for _, c := range copies {
			if c.Encrypted {
				kept = append(kept, c)
			} else {
				gone = append(gone, c.draftCopy)
			}
		}
		copies = kept
		req.DraftMessageID = linkedCopy(copies, req.DraftMessageID)
	}
	entry := storedDraft{
		ID:           id,
		SavedAt:      time.Now().UTC().Format(time.RFC3339),
		Request:      req,
		AccountID:    req.AccountID,
		Protection:   req.Protection,
		Token:        token,
		ServerCopies: copies,
	}
	// a draft of a message set to encrypt is sealed to the sender's own key
	// rather than written out as it stands. Failing to seal fails the save: the
	// alternative is quietly storing in the clear the one thing the user asked
	// not to be. The Message-ID stays outside the seal: the server copy carries
	// it in the clear anyway.
	if encrypts(req.Protection) {
		sealed, err := a.sealDraft(req)
		if err != nil {
			return 0, err
		}
		entry.Sealed = sealed
		entry.Request = ComposeRequest{AccountID: req.AccountID, Protection: req.Protection, DraftMessageID: req.DraftMessageID}
	}

	if at < 0 {
		entry.ID = nextDraftID(drafts)
		drafts = append(drafts, entry)
	} else {
		drafts[at] = entry
	}
	if err := a.writeDrafts(scope, drafts, gone); err != nil {
		return 0, err
	}
	if len(gone) > 0 {
		goSafe("removing the plaintext copies of an encrypted draft", a.drainDraftRemovals)
	}
	key := draftKey{scope, entry.Token}
	if entry.Sealed == "" {
		delete(a.sealedDrafts, key)
	} else {
		if a.sealedDrafts == nil {
			a.sealedDrafts = make(map[draftKey]ComposeRequest)
		}
		a.sealedDrafts[key] = req
	}
	return entry.ID, nil
}

// ListDrafts returns the locally saved drafts, newest first by save time. A
// sealed draft is opened when a passphrase for its key is already held, and
// otherwise comes back marked Locked with no content, for UnsealDraft to open.
func (a *App) ListDrafts() ([]DraftDTO, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	a.draftsMu.Lock()
	drafts, err := a.loadDrafts()
	a.draftsMu.Unlock()
	if err != nil {
		return nil, err
	}
	out := make([]DraftDTO, 0, len(drafts))
	for _, draft := range drafts {
		out = append(out, a.toDraftDTO(draft))
	}
	return out, nil
}

// toDraftDTO opens a sealed draft when a passphrase for it is already held, and
// otherwise hands back an empty one marked Locked. Nothing prompts from here:
// listing drafts must not put a passphrase dialog in front of anybody.
func (a *App) toDraftDTO(draft storedDraft) DraftDTO {
	dto := DraftDTO{
		ID:         draft.ID,
		SavedAt:    draft.SavedAt,
		Request:    draft.Request,
		AccountID:  draft.AccountID,
		Protection: draft.Protection,
	}
	if draft.Sealed == "" {
		return dto
	}
	req, err := a.unsealDraft(draft, nil)
	if err != nil {
		dto.Locked = true
		return dto
	}
	dto.Request = req
	return dto
}

// UnsealDraft opens a locked draft with a passphrase and returns it. The
// passphrase is held for the rest of the session, so a user working through
// several protected drafts is asked once.
func (a *App) UnsealDraft(id int64, passphrase string) (DraftDTO, error) {
	if err := a.ready(); err != nil {
		return DraftDTO{}, err
	}
	a.draftsMu.Lock()
	drafts, err := a.loadDrafts()
	a.draftsMu.Unlock()
	if err != nil {
		return DraftDTO{}, err
	}
	for _, draft := range drafts {
		if draft.ID != id {
			continue
		}
		if draft.Sealed == "" {
			return a.toDraftDTO(draft), nil
		}
		req, err := a.unsealDraft(draft, []byte(passphrase))
		if err != nil {
			return DraftDTO{}, err
		}
		dto := a.toDraftDTO(draft)
		dto.Request = req
		dto.Locked = false
		return dto, nil
	}
	return DraftDTO{}, fmt.Errorf("pelton: draft %d not found", id)
}

// DeleteDraft removes a local draft by id, and queues its server copies for
// removal in the same write, removing them in the background.
func (a *App) DeleteDraft(id int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	a.draftsMu.Lock()
	defer a.draftsMu.Unlock()
	scope := a.store.SettingsScope()
	drafts, err := a.loadDraftsIn(scope)
	if err != nil {
		return err
	}
	var gone storedDraft
	kept := drafts[:0]
	for _, d := range drafts {
		if d.ID != id {
			kept = append(kept, d)
		} else {
			gone = d
		}
	}
	delete(a.sealedDrafts, draftKey{scope, gone.Token})
	// the server copies go too, found by what is stored here, since the
	// composer may never have learned them. Queued with the delete, a removal
	// that fails now is tried again later.
	var queued []draftCopy
	for _, c := range gone.ServerCopies {
		queued = append(queued, c.draftCopy)
	}
	if err := a.writeDrafts(scope, kept, queued); err != nil {
		return err
	}
	goSafe("removing a deleted draft", a.drainDraftRemovals)
	return nil
}

// loadDrafts is loadDraftsIn the profile the app is in now.
func (a *App) loadDrafts() ([]storedDraft, error) {
	return a.loadDraftsIn(a.store.SettingsScope())
}

// loadDraftsIn reads the drafts json of settings scope, treating an unset key
// as an empty list. It may write the list back, so the caller holds draftsMu.
func (a *App) loadDraftsIn(scope int64) ([]storedDraft, error) {
	var drafts []storedDraft
	err := a.store.GetJSONIn(a.ctx, scope, draftsKey, &drafts)
	if err != nil {
		if isSettingMissing(err) {
			return nil, nil
		}
		return nil, err
	}
	changed := renumberUnsafeDraftIDs(drafts)
	for i, d := range drafts {
		// a draft stored before drafts had tokens gets one, kept from here on.
		if d.Token == "" {
			drafts[i].Token, changed = newDraftToken(), true
		}
		// a draft stored before server copies were recorded has its one
		// Message-ID for all of them, taken for plaintext: the copy may be from
		// before the draft was set to encrypt, and the next upload replaces it
		// either way.
		if d.ServerCopies == nil && d.Request.DraftMessageID != "" {
			drafts[i].ServerCopies = []serverCopy{{draftCopy{AccountID: d.AccountID, MessageID: d.Request.DraftMessageID}, false}}
		}
	}
	if changed {
		if err := a.writeDrafts(scope, drafts, nil); err != nil {
			return nil, err
		}
	}
	return drafts, nil
}

// queueDraftCopiesOf queues for removal the server copies of every draft in
// settings scope, about to be deleted with its profile, and forgets what this
// session held for them.
func (a *App) queueDraftCopiesOf(scope int64) error {
	a.draftsMu.Lock()
	defer a.draftsMu.Unlock()
	drafts, err := a.loadDraftsIn(scope)
	if err != nil {
		return err
	}
	var gone []draftCopy
	for _, d := range drafts {
		delete(a.sealedDrafts, draftKey{scope, d.Token})
		for _, c := range d.ServerCopies {
			gone = append(gone, c.draftCopy)
		}
	}
	return a.writeDrafts(scope, nil, gone)
}

// linkedCopy is the Message-ID a draft with these server copies links to:
// current when it is still among them, else the newest of them, else none.
func linkedCopy(copies []serverCopy, current string) string {
	if slices.ContainsFunc(copies, func(c serverCopy) bool { return c.MessageID == current }) {
		return current
	}
	if len(copies) == 0 {
		return ""
	}
	return copies[len(copies)-1].MessageID
}

// UploadDraft puts the local draft id in its account's Drafts folder on the
// server and returns the Message-ID of the copy. Every upload goes up under a
// Message-ID of its own, so a server keeping one message per Message-ID still
// takes every edit, and once it is up the draft's other copies are removed. A
// sign-only draft goes up unsigned, since signing happens at send. A draft to
// be encrypted goes up encrypted to the account's own key alone, without its
// Bcc, so plaintext never leaves the device; after a restart that takes the
// passphrase being held, since the draft has to be unsealed first. When such
// an upload fails, the draft's plaintext copies are removed all the same,
// while its earlier encrypted ones stay. An unknown id is ErrDraftNotFound,
// and an account with no server uploads nothing and returns "".
func (a *App) UploadDraft(id int64) (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	// the upload stays with the profile it started in, and with the draft id
	// named then, whatever switch or delete comes meanwhile.
	scope := a.store.SettingsScope()
	first, _, _, err := a.draftSnapshot(scope, func(d storedDraft) bool { return d.ID == id })
	if err != nil {
		return "", err
	}
	// uploads of one draft take turns, each reading the draft once its turn
	// comes, so the last to finish is the newest version.
	defer a.lockDraftUploads(draftKey{scope, first.Token})()
	draft, req, held, err := a.draftSnapshot(scope, func(d storedDraft) bool { return d.Token == first.Token })
	if err != nil {
		return "", err
	}
	account, err := a.store.GetAccount(a.ctx, draft.AccountID)
	if err != nil {
		return "", err
	}
	if account.Local {
		return "", nil
	}
	messageID, err := a.putDraft(scope, *account, draft, req, held)
	goSafe("removing replaced draft copies", a.drainDraftRemovals)
	if err != nil {
		return "", err
	}
	a.queueSync(*account)
	return messageID, nil
}

// putDraft builds the server copy of draft under a new Message-ID, encrypted
// when the draft is to be, records it among the draft's server copies and
// puts it up, then settles the copies by how that went. req is the sealed
// draft's request when held.
func (a *App) putDraft(scope int64, account storage.Account, draft storedDraft, req ComposeRequest, held bool) (string, error) {
	encrypted := draft.Sealed != "" || encrypts(draft.Protection)
	raw, messageID, err := a.buildDraftCopy(account, draft, req, held, encrypted)
	if err != nil {
		return "", errors.Join(err, a.settleUpload(scope, draft.Token, nil, false, encrypted))
	}
	mailbox, err := a.draftsFolder(account.ID)
	if err != nil {
		return "", errors.Join(err, a.settleUpload(scope, draft.Token, nil, false, encrypted))
	}
	cfg, err := a.resolveIMAP(account)
	if err != nil {
		return "", errors.Join(err, a.settleUpload(scope, draft.Token, nil, false, encrypted))
	}
	// the copy is recorded before it goes up: an upload whose answer is lost
	// may still have stored it, and a recorded copy is one that gets removed.
	sent := serverCopy{draftCopy{account.ID, messageID, mailbox}, encrypted}
	if err := a.recordServerCopy(scope, draft.Token, sent); err != nil {
		return "", err
	}
	// no syncMu: a sync can hold it for minutes, and saving a draft must not
	// wait for that. A second short login is the cheaper cost.
	err = a.withIMAPSession(cfg, func(client mailClient) error {
		return client.ReplaceDraft(mailbox, raw, messageID)
	})
	if serr := a.settleUpload(scope, draft.Token, &sent, err == nil, encrypted); serr != nil {
		return "", errors.Join(err, serr)
	}
	if err != nil {
		return "", err
	}
	return messageID, nil
}

// buildDraftCopy builds the server copy of draft under a new Message-ID.
func (a *App) buildDraftCopy(account storage.Account, draft storedDraft, req ComposeRequest, held, encrypted bool) ([]byte, string, error) {
	switch {
	case draft.Sealed == "":
		req = draft.Request
	case !held:
		var err error
		if req, err = a.unsealDraft(draft, nil); err != nil {
			return nil, "", fmt.Errorf("pelton: unlock your key to put this encrypted draft on the server: %w", err)
		}
	}
	msg, err := a.buildMessage(req)
	if err != nil {
		return nil, "", err
	}
	if msg.MessageID, err = smtp.NewMessageID(account.Email); err != nil {
		return nil, "", err
	}
	if encrypted {
		return a.encryptDraft(msg, account.Email)
	}
	return smtp.BuildDraft(msg)
}

// recordServerCopy adds a copy about to go up to the server copies of the
// draft with token in settings scope, and marks it as being uploaded, so no
// removal runs before the upload is done. A draft gone meanwhile is
// ErrDraftNotFound.
func (a *App) recordServerCopy(scope int64, token string, c serverCopy) error {
	a.draftsMu.Lock()
	defer a.draftsMu.Unlock()
	drafts, err := a.loadDraftsIn(scope)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(drafts, func(d storedDraft) bool { return d.Token == token })
	if i < 0 {
		return ErrDraftNotFound
	}
	drafts[i].ServerCopies = append(drafts[i].ServerCopies, c)
	if err := a.writeDrafts(scope, drafts, nil); err != nil {
		return err
	}
	if a.uploadingCopies == nil {
		a.uploadingCopies = make(map[string]struct{})
	}
	a.uploadingCopies[c.MessageID] = struct{}{}
	return nil
}

// settleUpload queues for removal the copies of draft id an upload made
// stale. When the upload of sent worked, that is every other copy, and sent
// becomes the one the draft links to. When it failed, or never got as far as
// sending (sent nil), that is sent itself, which may have been stored all the
// same, and for a draft to be encrypted also every plaintext copy, while its
// earlier encrypted ones stay. A draft deleted meanwhile has had its copies
// queued already, and sent is queued again in case it was never recorded.
func (a *App) settleUpload(scope int64, token string, sent *serverCopy, ok, encrypted bool) error {
	a.draftsMu.Lock()
	defer a.draftsMu.Unlock()
	if sent != nil {
		delete(a.uploadingCopies, sent.MessageID)
	}
	drafts, err := a.loadDraftsIn(scope)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(drafts, func(d storedDraft) bool { return d.Token == token })
	if i < 0 || (sent != nil && !slices.Contains(drafts[i].ServerCopies, *sent)) {
		if sent == nil {
			return nil
		}
		return a.writeDrafts(scope, nil, []draftCopy{sent.draftCopy})
	}
	d := &drafts[i]
	stale := func(c serverCopy) bool {
		if sent != nil && c == *sent {
			return !ok
		}
		return ok || (encrypted && !c.Encrypted)
	}
	var keep []serverCopy
	var gone []draftCopy
	for _, c := range d.ServerCopies {
		if stale(c) {
			gone = append(gone, c.draftCopy)
		} else {
			keep = append(keep, c)
		}
	}
	if len(gone) == 0 && !ok {
		return nil
	}
	d.ServerCopies = keep
	if ok {
		d.Request.DraftMessageID = sent.MessageID
	} else {
		d.Request.DraftMessageID = linkedCopy(keep, d.Request.DraftMessageID)
	}
	return a.writeDrafts(scope, drafts, gone)
}

// writeDrafts writes drafts to settings scope, unless drafts is nil, and adds
// gone to the removal queue in the same write, so a copy is never in neither.
// The caller holds draftsMu.
func (a *App) writeDrafts(scope int64, drafts []storedDraft, gone []draftCopy) error {
	values := map[storage.ScopedSetting]any{}
	if drafts != nil {
		values[storage.ScopedSetting{Scope: scope, Key: draftsKey}] = drafts
	}
	if len(gone) > 0 {
		at, queue, err := a.loadDraftRemovals()
		if err != nil {
			return err
		}
		values[storage.ScopedSetting{Scope: at, Key: draftRemovalsKey}] = append(queue, gone...)
	}
	if len(values) == 0 {
		return nil
	}
	return a.store.SetJSONsIn(a.ctx, values)
}

// loadDraftRemovals reads the removal queue, treating an unset key as empty,
// and returns it with the scope it lives in, the main profile's. The caller
// holds draftsMu.
func (a *App) loadDraftRemovals() (int64, []draftCopy, error) {
	main, err := a.store.MainProfile(a.ctx)
	if err != nil {
		return 0, nil, err
	}
	var queue []draftCopy
	if err := a.store.GetJSONIn(a.ctx, main.ID, draftRemovalsKey, &queue); err != nil && !isSettingMissing(err) {
		return 0, nil, err
	}
	return main.ID, queue, nil
}

// drainDraftRemovals removes from the server every queued copy not being
// uploaded right now, each on its own account, and takes off the queue the
// ones that went. A copy already gone counts as removed, and so does one on an
// account that no longer exists; any other failure leaves it for next time.
// It runs after every upload and delete, and at start.
func (a *App) drainDraftRemovals() {
	a.draftRemovalsMu.Lock()
	defer a.draftRemovalsMu.Unlock()
	a.draftsMu.Lock()
	_, queue, err := a.loadDraftRemovals()
	busy := maps.Clone(a.uploadingCopies)
	a.draftsMu.Unlock()
	if err != nil {
		a.log.Error("read the draft removal queue", "err", err)
		return
	}
	var done []draftCopy
	for _, c := range queue {
		if _, uploading := busy[c.MessageID]; uploading || slices.Contains(done, c) {
			continue
		}
		err := checkMessageID(c.MessageID)
		if err == nil {
			err = a.removeDraftCopy(c)
		}
		if err != nil && !errors.Is(err, storage.ErrAccountNotFound) && !errors.Is(err, errBadMessageID) {
			a.log.Error("remove a draft copy from the server", "err", err)
			continue
		}
		done = append(done, c)
	}
	if len(done) == 0 {
		return
	}
	a.draftsMu.Lock()
	defer a.draftsMu.Unlock()
	at, queue, err := a.loadDraftRemovals()
	if err == nil {
		queue = slices.DeleteFunc(queue, func(c draftCopy) bool { return slices.Contains(done, c) })
		err = a.store.SetJSONsIn(a.ctx, map[storage.ScopedSetting]any{{Scope: at, Key: draftRemovalsKey}: queue})
	}
	if err != nil {
		a.log.Error("update the draft removal queue", "err", err)
	}
}

// draftSnapshot returns the draft of settings scope that is matches and, for a
// sealed one saved this session, its request and true. None is
// ErrDraftNotFound.
func (a *App) draftSnapshot(scope int64, is func(storedDraft) bool) (storedDraft, ComposeRequest, bool, error) {
	a.draftsMu.Lock()
	defer a.draftsMu.Unlock()
	drafts, err := a.loadDraftsIn(scope)
	if err != nil {
		return storedDraft{}, ComposeRequest{}, false, err
	}
	i := slices.IndexFunc(drafts, is)
	if i < 0 {
		return storedDraft{}, ComposeRequest{}, false, ErrDraftNotFound
	}
	req, held := a.sealedDrafts[draftKey{scope, drafts[i].Token}]
	return drafts[i], req, held, nil
}

// lockDraftUploads waits for the uploads of a draft already under way and
// returns the unlock. The locks are never dropped: one per draft saved this
// session is small, and dropping one in use would split it in two.
func (a *App) lockDraftUploads(key draftKey) func() {
	lock, _ := a.draftUploadLocks.LoadOrStore(key, &sync.Mutex{})
	mu := lock.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// encryptDraft builds the server copy of an encrypted draft, encrypted to the
// key of owner, the account's own address: the key sealDraft seals the local
// draft to, whichever address the draft is from.
func (a *App) encryptDraft(msg *smtp.Message, owner string) ([]byte, string, error) {
	keys, err := a.keyStore()
	if err != nil {
		return nil, "", errNoProtectionKeys
	}
	raw, messageID, err := smtp.BuildEncryptedDraft(msg, owner, crypto.NewPGP(keys))
	if err != nil {
		return nil, "", fmt.Errorf("pelton: this draft cannot go to the server because there is no key for %s to encrypt it to: %w", owner, err)
	}
	return raw, messageID, nil
}

// ErrNoDraftsFolder means the account has no folder Pelton uses as Drafts.
var ErrNoDraftsFolder = errors.New(`pelton: this account has no Drafts folder. Create one, or pick "Use as…" and Drafts on an existing folder`)

// draftsFolder is the path of the folder the folder list shows as the
// account's Drafts: one the user picked by hand first, then the server's
// \Drafts folder or one named Drafts.
func (a *App) draftsFolder(accountID int64) (string, error) {
	folders, err := a.store.ListFolders(a.ctx, accountID)
	if err != nil {
		return "", err
	}
	if i := slices.IndexFunc(folders, func(f storage.Folder) bool { return f.RoleOverride == roleDrafts }); i >= 0 {
		return folders[i].IMAPPath, nil
	}
	if i := slices.IndexFunc(folders, func(f storage.Folder) bool { return folderRole(f) == roleDrafts }); i >= 0 {
		return folders[i].IMAPPath, nil
	}
	return "", ErrNoDraftsFolder
}

// RemoveServerDraft deletes the server copy of a draft by its Message-ID. An
// empty Message-ID, or an account with no server, does nothing; one that is
// not a single <local@domain> is errBadMessageID.
func (a *App) RemoveServerDraft(accountID int64, messageID string) error {
	if strings.Trim(strings.TrimSpace(messageID), "<>") == "" {
		return nil
	}
	if err := checkMessageID(messageID); err != nil {
		return err
	}
	if err := a.ready(); err != nil {
		return err
	}
	return a.removeDraftCopy(draftCopy{AccountID: accountID, MessageID: messageID})
}

// removeDraftCopy deletes a draft copy from its account, in one session: from
// the folder it went up to while the account still has that folder, and from
// the folder used as Drafts now, in case that is where it went.
func (a *App) removeDraftCopy(c draftCopy) error {
	account, err := a.store.GetAccount(a.ctx, c.AccountID)
	if err != nil {
		return err
	}
	if account.Local {
		return nil
	}
	var mailboxes []string
	if c.Mailbox != "" {
		folders, err := a.store.ListFolders(a.ctx, account.ID)
		if err != nil {
			return err
		}
		if slices.ContainsFunc(folders, func(f storage.Folder) bool { return f.IMAPPath == c.Mailbox }) {
			mailboxes = append(mailboxes, c.Mailbox)
		}
	}
	current, err := a.draftsFolder(account.ID)
	if err == nil && !slices.Contains(mailboxes, current) {
		mailboxes = append(mailboxes, current)
	}
	if len(mailboxes) == 0 {
		return err
	}
	cfg, err := a.resolveIMAP(*account)
	if err != nil {
		return err
	}
	// no syncMu, for the same reason as UploadDraft.
	if err := a.withIMAPSession(cfg, func(client mailClient) error {
		for _, mailbox := range mailboxes {
			if err := client.RemoveDraft(mailbox, c.MessageID); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	a.queueSync(*account)
	return nil
}

// errBadMessageID is returned for a draft Message-ID from the frontend that is
// not a single <local@domain>. It goes into a header of the server copy, where
// a line break would start a header of its own.
var errBadMessageID = errors.New("pelton: a draft Message-ID must have the form <local@domain>")

// checkMessageID returns errBadMessageID unless id is <local@domain> with no
// whitespace, control characters, brackets or second @ inside.
func checkMessageID(id string) error {
	inner, ok := strings.CutPrefix(id, "<")
	if ok {
		inner, ok = strings.CutSuffix(inner, ">")
	}
	local, domain, _ := strings.Cut(inner, "@")
	invalid := func(r rune) bool { return r <= ' ' || r == 0x7f || strings.ContainsRune("<>@", r) }
	if !ok || local == "" || domain == "" || strings.ContainsFunc(local+domain, invalid) {
		return fmt.Errorf("%w: %q", errBadMessageID, id)
	}
	return nil
}

// OpenDraft returns the local draft behind a message in a Drafts folder, given
// the message's storage id, so the composer can edit it. ID is 0 when the
// message is not a draft Pelton saved on this device; such a draft is shown
// read-only, since rebuilding it from the cached message would drop
// attachments, Bcc and remote content.
func (a *App) OpenDraft(messageID int64) (DraftDTO, error) {
	if err := a.ready(); err != nil {
		return DraftDTO{}, err
	}
	m, err := a.store.GetMessage(a.ctx, messageID)
	if err != nil {
		return DraftDTO{}, err
	}
	// sync stores the Message-ID without its angle brackets, the builder
	// writes it with them.
	want := strings.Trim(m.MessageID, "<>")
	if want == "" {
		return DraftDTO{}, nil
	}
	a.draftsMu.Lock()
	drafts, err := a.loadDrafts()
	a.draftsMu.Unlock()
	if err != nil {
		return DraftDTO{}, err
	}
	// any of a draft's server copies opens it, on the account it went up on.
	matches := func(c serverCopy) bool {
		return c.AccountID == m.AccountID && strings.Trim(c.MessageID, "<>") == want
	}
	for _, d := range drafts {
		if slices.ContainsFunc(d.ServerCopies, matches) {
			return a.toDraftDTO(d), nil
		}
	}
	return DraftDTO{}, nil
}

// DraftSubjects returns the subjects of the account's sealed drafts, keyed by
// the Message-ID of each of their server copies on the account, without its
// angle brackets. Their server copies are titled "...",
// so the drafts list shows these instead. Only drafts readable without asking
// are included: one this session saved, or one a passphrase already held
// opens. Nothing prompts from here and nothing is stored.
func (a *App) DraftSubjects(accountID int64) (map[string]string, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	a.draftsMu.Lock()
	scope := a.store.SettingsScope()
	drafts, err := a.loadDraftsIn(scope)
	held := maps.Clone(a.sealedDrafts)
	a.draftsMu.Unlock()
	if err != nil {
		return nil, err
	}
	out := make(map[string]string)
	for _, d := range drafts {
		onAccount := slices.ContainsFunc(d.ServerCopies, func(c serverCopy) bool { return c.AccountID == accountID })
		if d.Sealed == "" || !onAccount {
			continue
		}
		req, ok := held[draftKey{scope, d.Token}]
		if !ok {
			if req, err = a.unsealDraft(d, nil); err != nil {
				continue
			}
		}
		for _, c := range d.ServerCopies {
			if c.AccountID == accountID {
				out[strings.Trim(c.MessageID, "<>")] = req.Subject
			}
		}
	}
	return out, nil
}

// OutboxRowDTO is one queued or failed message for the outbox view.
type OutboxRowDTO struct {
	ID            int64    `json:"id"`
	AccountID     int64    `json:"accountId"`
	Recipients    []string `json:"recipients"`
	State         string   `json:"state"`
	Attempts      int      `json:"attempts"`
	LastError     string   `json:"lastError"`
	NextAttemptAt string   `json:"nextAttemptAt"`
	CreatedAt     string   `json:"createdAt"`
}

// ListOutbox returns the current outbox, so the ui can show queued, sending and
// failed messages and surface send failures explicitly.
func (a *App) ListOutbox() ([]OutboxRowDTO, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	rows, err := a.queue.List(a.ctx)
	if err != nil {
		return nil, err
	}
	out := make([]OutboxRowDTO, 0, len(rows))
	for _, r := range rows {
		out = append(out, OutboxRowDTO{
			ID:            r.ID,
			AccountID:     r.AccountID,
			Recipients:    r.Recipients,
			State:         r.State,
			Attempts:      r.Attempts,
			LastError:     r.LastError,
			NextAttemptAt: formatDate(r.NextAttemptAt),
			CreatedAt:     formatDate(r.CreatedAt),
		})
	}
	return out, nil
}

// decodeAttachments base64-decodes compose attachments into builder attachments.
func decodeAttachments(in []ComposeAttachment) ([]smtp.Attachment, error) {
	out := make([]smtp.Attachment, 0, len(in))
	for _, att := range in {
		data, err := base64.StdEncoding.DecodeString(att.ContentBase64)
		if err != nil {
			return nil, fmt.Errorf("pelton: decode attachment %q: %w", att.Filename, err)
		}
		out = append(out, smtp.Attachment{
			Filename:    att.Filename,
			ContentType: att.ContentType,
			Content:     data,
			Inline:      att.Inline,
			ContentID:   att.ContentID,
		})
	}
	return out, nil
}

// toBuilderAddresses converts address dtos to builder addresses.
func toBuilderAddresses(in []AddressDTO) []smtp.Address {
	out := make([]smtp.Address, 0, len(in))
	for _, a := range in {
		out = append(out, smtp.Address{Name: a.Name, Email: a.Email})
	}
	return out
}
