package desktop

import (
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

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
	// DraftMessageID is the Message-ID of the server copy of this draft, empty
	// until one has been uploaded. It is only for replacing that copy: a sent
	// message gets a Message-ID of its own.
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
// Request is left empty, and Locked says the passphrase is needed to read it
// back. Ordinary drafts are unchanged.
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
	// message the user chose to encrypt. Request is empty when it is set.
	Sealed     string `json:"sealed,omitempty"`
	AccountID  int64  `json:"accountId"`
	Protection string `json:"protection"`
}

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
func (a *App) SaveDraft(id int64, req ComposeRequest) (int64, error) {
	if err := a.ready(); err != nil {
		return 0, err
	}
	if req.DraftMessageID != "" {
		if err := checkMessageID(req.DraftMessageID); err != nil {
			return 0, err
		}
	}
	entry := storedDraft{
		ID:         id,
		SavedAt:    time.Now().UTC().Format(time.RFC3339),
		Request:    req,
		AccountID:  req.AccountID,
		Protection: req.Protection,
	}
	// a draft of a message set to encrypt is sealed to the sender's own key
	// rather than written out as it stands. Failing to seal fails the save: the
	// alternative is quietly storing in the clear the one thing the user asked
	// not to be.
	if encrypts(req.Protection) {
		sealed, err := a.sealDraft(req)
		if err != nil {
			return 0, err
		}
		entry.Sealed = sealed
		// the Message-ID stays in the clear: it is what finds a plaintext copy
		// an earlier save put on the server, so a failed removal can be retried.
		entry.Request = ComposeRequest{AccountID: req.AccountID, Protection: req.Protection, DraftMessageID: req.DraftMessageID}
	} else if req.DraftMessageID == "" {
		// the Message-ID is fixed at the first save, so every server copy of
		// this draft carries the same one, whatever the composer still knows.
		acc, err := a.store.GetAccount(a.ctx, req.AccountID)
		if err != nil {
			return 0, err
		}
		if entry.Request.DraftMessageID, err = smtp.NewMessageID(acc.Email); err != nil {
			return 0, err
		}
	}

	a.draftsMu.Lock()
	defer a.draftsMu.Unlock()
	drafts, err := a.loadDrafts()
	if err != nil {
		return 0, err
	}
	if id == 0 {
		entry.ID = nextDraftID(drafts)
		id = entry.ID
		drafts = append(drafts, entry)
	} else {
		i := slices.IndexFunc(drafts, func(d storedDraft) bool { return d.ID == id })
		if i < 0 {
			return 0, ErrDraftNotFound
		}
		if kept := drafts[i].Request.DraftMessageID; kept != "" {
			entry.Request.DraftMessageID = kept
		}
		drafts[i] = entry
	}
	if err := a.store.SetJSON(a.ctx, draftsKey, drafts); err != nil {
		return 0, err
	}
	// a draft now sealed must leave no plaintext copy on the server. Every
	// sealed save tries again, and DeleteDraft once more on send or delete.
	if messageID := entry.Request.DraftMessageID; entry.Sealed != "" && messageID != "" {
		goSafe("removing the plaintext copy of an encrypted draft", func() {
			if err := a.RemoveServerDraft(entry.AccountID, messageID); err != nil {
				a.log.Error("remove plaintext draft from the server", "err", err)
			}
		})
	}
	return id, nil
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

// DeleteDraft removes a local draft by id, and its copy in the server Drafts
// folder in the background.
func (a *App) DeleteDraft(id int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	a.draftsMu.Lock()
	defer a.draftsMu.Unlock()
	drafts, err := a.loadDrafts()
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
	if err := a.store.SetJSON(a.ctx, draftsKey, kept); err != nil {
		return err
	}
	// the server copy goes too, found by the Message-ID stored here, since the
	// composer may never have learned it. A failure costs a stale server copy.
	if messageID := gone.Request.DraftMessageID; messageID != "" {
		goSafe("removing a deleted draft", func() {
			if err := a.RemoveServerDraft(gone.AccountID, messageID); err != nil {
				a.log.Error("remove deleted draft from the server", "err", err)
			}
		})
	}
	return nil
}

// loadDrafts reads the drafts json, treating an unset key as an empty list. It
// may write the list back, so the caller holds draftsMu.
func (a *App) loadDrafts() ([]storedDraft, error) {
	var drafts []storedDraft
	err := a.store.GetJSON(a.ctx, draftsKey, &drafts)
	if err != nil {
		if isSettingMissing(err) {
			return nil, nil
		}
		return nil, err
	}
	if renumberUnsafeDraftIDs(drafts) {
		if err := a.store.SetJSON(a.ctx, draftsKey, drafts); err != nil {
			return nil, err
		}
	}
	return drafts, nil
}

// ErrProtectedDraftStaysLocal is returned by UploadDraft for a draft of a
// message to be encrypted. Such a draft is never put on the server in the
// clear; the frontend does not ask, and this is the boundary if it does.
var ErrProtectedDraftStaysLocal = errors.New("pelton: a draft of an encrypted message stays on this device")

// UploadDraft puts the local draft id in its account's Drafts folder on the
// server, replacing the copy an earlier upload put there, and returns the
// Message-ID that links the two, fixed by SaveDraft. A sign-only draft goes up
// unsigned, since signing happens at send. An unknown id is ErrDraftNotFound, a draft to be encrypted is
// ErrProtectedDraftStaysLocal, and an account with no server uploads nothing
// and returns "".
func (a *App) UploadDraft(id int64) (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	a.draftsMu.Lock()
	drafts, err := a.loadDrafts()
	a.draftsMu.Unlock()
	if err != nil {
		return "", err
	}
	i := slices.IndexFunc(drafts, func(d storedDraft) bool { return d.ID == id })
	if i < 0 {
		return "", ErrDraftNotFound
	}
	draft := drafts[i]
	if draft.Sealed != "" || encrypts(draft.Protection) {
		return "", ErrProtectedDraftStaysLocal
	}
	account, err := a.store.GetAccount(a.ctx, draft.AccountID)
	if err != nil {
		return "", err
	}
	if account.Local {
		return "", nil
	}

	msg, err := a.buildMessage(draft.Request)
	if err != nil {
		return "", err
	}
	msg.MessageID = draft.Request.DraftMessageID
	raw, messageID, err := smtp.BuildDraft(msg)
	if err != nil {
		return "", err
	}
	mailbox, err := a.draftsFolder(account.ID)
	if err != nil {
		return "", err
	}
	cfg, err := a.resolveIMAP(*account)
	if err != nil {
		return "", err
	}
	// no syncMu: a sync can hold it for minutes, and saving a draft must not
	// wait for that. A second short login is the cheaper cost.
	if err := a.withIMAPSession(cfg, func(client mailClient) error {
		return client.ReplaceDraft(mailbox, raw, messageID)
	}); err != nil {
		return "", err
	}
	a.queueSync(*account)
	return messageID, nil
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
	account, err := a.store.GetAccount(a.ctx, accountID)
	if err != nil {
		return err
	}
	if account.Local {
		return nil
	}
	mailbox, err := a.draftsFolder(account.ID)
	if err != nil {
		return err
	}
	cfg, err := a.resolveIMAP(*account)
	if err != nil {
		return err
	}
	// no syncMu, for the same reason as UploadDraft.
	if err := a.withIMAPSession(cfg, func(client mailClient) error {
		return client.RemoveDraft(mailbox, messageID)
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

// checkMessageID keeps legacy draft keys injection-safe. Reply tokens use a
// stricter syntax and length policy; applying it here would orphan old drafts.
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

// draftLookupID removes one optional bracket pair without rewriting the key.
func draftLookupID(id string) (string, bool) {
	if strings.HasPrefix(id, "<") && strings.HasSuffix(id, ">") {
		id = id[1 : len(id)-1]
	}
	return id, id != "" && !strings.ContainsAny(id, "<>")
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
	want, ok := draftLookupID(m.MessageID)
	if !ok {
		return DraftDTO{}, nil
	}
	a.draftsMu.Lock()
	drafts, err := a.loadDrafts()
	a.draftsMu.Unlock()
	if err != nil {
		return DraftDTO{}, err
	}
	for _, d := range drafts {
		candidate, ok := draftLookupID(d.Request.DraftMessageID)
		if ok && candidate == want {
			return a.toDraftDTO(d), nil
		}
	}
	return DraftDTO{}, nil
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
