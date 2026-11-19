package desktop

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/emersion/go-imap/v2"

	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/storage"
)

// The attachments browser: every attachment across the profile's mail in one
// paged list, and the per-sender summary the reader's sender panel shows.

// AttachmentQueryRequest is one page of the browser. Kinds holds storage kind
// names; Since and Until are YYYY-MM-DD, Until inclusive. Zero values leave a
// filter off. WithTotal asks for the match count as well, which the list only
// needs on its first page.
type AttachmentQueryRequest struct {
	Kinds         []string `json:"kinds"`
	AccountID     int64    `json:"accountId"`
	Sender        string   `json:"sender"`
	SenderAddress string   `json:"senderAddress"`
	Since         string   `json:"since"`
	Until         string   `json:"until"`
	Name          string   `json:"name"`
	Limit         int      `json:"limit"`
	Offset        int      `json:"offset"`
	WithTotal     bool     `json:"withTotal"`
}

// AttachmentEntryDTO is one row of the browser. Downloaded is false when the
// bytes are not in the local store and have to come from the server first.
type AttachmentEntryDTO struct {
	ID          int64  `json:"id"`
	MessageID   int64  `json:"messageId"`
	AccountID   int64  `json:"accountId"`
	FolderID    int64  `json:"folderId"`
	ThreadID    int64  `json:"threadId"`
	Filename    string `json:"filename"`
	ContentType string `json:"contentType"`
	SizeBytes   int64  `json:"sizeBytes"`
	Kind        string `json:"kind"`
	Subject     string `json:"subject"`
	FromName    string `json:"fromName"`
	FromAddress string `json:"fromAddress"`
	Date        string `json:"date"`
	Downloaded  bool   `json:"downloaded"`
}

// AttachmentPageDTO is a page of entries; Total is -1 unless it was asked for.
type AttachmentPageDTO struct {
	Entries []AttachmentEntryDTO `json:"entries"`
	Total   int                  `json:"total"`
}

// AttachmentRefDTO names one attachment of one message.
type AttachmentRefDTO struct {
	MessageID    int64 `json:"messageId"`
	AttachmentID int64 `json:"attachmentId"`
}

// maxAttachmentPage caps one browser page.
const maxAttachmentPage = 500

// QueryAttachments returns a page of the attachments browser, newest first.
// Trash and junk are left out, and so are tiny images (see
// storage.TinyImageBytes).
func (a *App) QueryAttachments(req AttachmentQueryRequest) (AttachmentPageDTO, error) {
	if err := a.ready(); err != nil {
		return AttachmentPageDTO{}, err
	}
	q, err := a.attachmentQuery(a.ctx, req)
	if err != nil {
		return AttachmentPageDTO{}, err
	}
	entries, err := a.store.QueryAttachments(a.ctx, q)
	if err != nil {
		return AttachmentPageDTO{}, err
	}
	page := AttachmentPageDTO{Entries: make([]AttachmentEntryDTO, 0, len(entries)), Total: -1}
	for _, e := range entries {
		page.Entries = append(page.Entries, a.attachmentEntryDTO(e))
	}
	if req.WithTotal {
		if page.Total, err = a.store.CountAttachments(a.ctx, q); err != nil {
			return AttachmentPageDTO{}, err
		}
	}
	return page, nil
}

func (a *App) attachmentEntryDTO(e storage.AttachmentEntry) AttachmentEntryDTO {
	return AttachmentEntryDTO{
		ID:          e.ID,
		MessageID:   e.MessageID,
		AccountID:   e.AccountID,
		FolderID:    e.FolderID,
		ThreadID:    e.ThreadID,
		Filename:    e.Filename,
		ContentType: e.ContentType,
		SizeBytes:   e.SizeBytes,
		Kind:        string(e.Kind),
		Subject:     e.Subject,
		FromName:    e.FromName,
		FromAddress: e.FromAddress,
		Date:        formatDate(e.Date),
		Downloaded:  a.store.AttachmentOnDisk(e.DiskPath),
	}
}

// attachmentQuery turns a request into the storage query over the folders the
// browser covers.
func (a *App) attachmentQuery(ctx context.Context, req AttachmentQueryRequest) (storage.AttachmentQuery, error) {
	folders, err := a.browsableFolders(ctx)
	if err != nil {
		return storage.AttachmentQuery{}, err
	}
	limit := req.Limit
	if limit <= 0 || limit > maxAttachmentPage {
		limit = maxAttachmentPage
	}
	q := storage.AttachmentQuery{
		FolderIDs:     folders,
		AccountID:     req.AccountID,
		Sender:        req.Sender,
		SenderAddress: bareAddress(req.SenderAddress),
		Name:          req.Name,
		Limit:         limit,
		Offset:        max(req.Offset, 0),
	}
	for _, k := range req.Kinds {
		q.Kinds = append(q.Kinds, storage.AttachmentKind(k))
	}
	if req.Since != "" {
		since, err := time.ParseInLocation(time.DateOnly, req.Since, time.Local)
		if err != nil {
			return storage.AttachmentQuery{}, fmt.Errorf("pelton: invalid date %q: %w", req.Since, err)
		}
		q.Since = since
	}
	if req.Until != "" {
		until, err := time.ParseInLocation(time.DateOnly, req.Until, time.Local)
		if err != nil {
			return storage.AttachmentQuery{}, fmt.Errorf("pelton: invalid date %q: %w", req.Until, err)
		}
		q.Until = until.AddDate(0, 0, 1)
	}
	return q, nil
}

// browsableFolders is every folder of the profile's accounts except trash and
// junk, imported archives included: the browser is about what you have.
func (a *App) browsableFolders(ctx context.Context) ([]int64, error) {
	accounts, err := a.store.ListAccounts(ctx)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for _, acc := range accounts {
		folders, err := a.store.ListFolders(ctx, acc.ID)
		if err != nil {
			return nil, err
		}
		for _, f := range folders {
			if role := folderRole(f); role != roleTrash && role != roleJunk {
				ids = append(ids, f.ID)
			}
		}
	}
	return ids, nil
}

// FetchAttachment makes sure an attachment's bytes are in the local store,
// fetching its message from the server when they are not. It returns the
// entry as it now stands.
func (a *App) FetchAttachment(attachmentID int64) (AttachmentEntryDTO, error) {
	if err := a.ready(); err != nil {
		return AttachmentEntryDTO{}, err
	}
	entry, err := a.store.GetAttachmentEntry(a.ctx, attachmentID)
	if err != nil {
		return AttachmentEntryDTO{}, err
	}
	att, err := a.localAttachment(entry.Attachment)
	if err != nil {
		return AttachmentEntryDTO{}, err
	}
	entry.Attachment = *att
	return a.attachmentEntryDTO(*entry), nil
}

// errAttachmentGone means the server no longer has the part a missing file
// would be fetched from.
var errAttachmentGone = errors.New("pelton: the server no longer has this attachment")

// localAttachment returns att with its bytes on disk, fetching the message
// from the server first when they are missing.
func (a *App) localAttachment(att storage.Attachment) (*storage.Attachment, error) {
	if a.store.AttachmentOnDisk(att.DiskPath) {
		return &att, nil
	}
	m, err := a.store.GetMessage(a.mailCtx(), att.MessageID)
	if err != nil {
		return nil, err
	}
	account, err := a.store.GetAccount(a.ctx, m.AccountID)
	if err != nil {
		return nil, err
	}
	if account.Local {
		return nil, storage.ErrAttachmentNotLocal
	}
	folder, err := a.store.GetFolder(a.ctx, m.FolderID)
	if err != nil {
		return nil, err
	}
	content, err := a.fetchPart(*account, folder.IMAPPath, m.UID, att)
	if err != nil {
		return nil, err
	}
	return a.store.RestoreAttachmentFile(a.mailCtx(), m.AccountID, att, bytes.NewReader(content))
}

// fetchPart fetches one message and returns the content of the part that
// matches att: the part at the same position among the message's stored
// attachments when the counts agree, else the first with the same name.
func (a *App) fetchPart(account storage.Account, mailbox string, uid uint32, att storage.Attachment) ([]byte, error) {
	cfg, err := a.resolveIMAP(account)
	if err != nil {
		return nil, err
	}
	stored, err := a.store.ListAttachments(a.mailCtx(), att.MessageID)
	if err != nil {
		return nil, err
	}
	syncMu.Lock()
	defer syncMu.Unlock()
	client, err := a.connectIMAP(cfg)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	if err := client.Login(); err != nil {
		return nil, err
	}
	defer client.Logout()
	if _, err := client.Select(mailbox); err != nil {
		return nil, err
	}
	msg, err := client.FetchMessage(imap.UID(uid))
	if err != nil {
		return nil, err
	}
	part := matchPart(msg.Attachments, stored, att)
	if part == nil {
		return nil, errAttachmentGone
	}
	return part.Content, nil
}

func matchPart(parts []pimap.Attachment, stored []storage.Attachment, att storage.Attachment) *pimap.Attachment {
	if len(parts) == len(stored) {
		for i := range stored {
			if stored[i].ID == att.ID {
				return &parts[i]
			}
		}
	}
	for i := range parts {
		if filepath.Base(parts[i].Filename) == att.Filename {
			return &parts[i]
		}
	}
	return nil
}

// OpenAttachmentFile copies an attachment to a temporary folder and opens it
// with the application the system has for its type.
func (a *App) OpenAttachmentFile(messageID, attachmentID int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	target, err := a.findAttachment(messageID, attachmentID)
	if err != nil {
		return err
	}
	att, err := a.localAttachment(*target)
	if err != nil {
		return err
	}
	dir := filepath.Join(os.TempDir(), "pelton-attachments", strconv.FormatInt(att.ID, 10))
	if m, err := a.store.GetMessage(a.mailCtx(), att.MessageID); err == nil && m.VaultID != 0 {
		if dir, err = a.mailVaultTempFor(att.ID); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("pelton: open attachment: %w", err)
	}
	dest := filepath.Join(dir, filepath.Base(att.Filename))
	if err := a.copyAttachmentProgress(*att, dest, 0, 1); err != nil {
		return err
	}
	return openPath(dest)
}

// SaveAttachmentsTo prompts for a folder and writes the chosen attachments
// there, fetching any that are not downloaded and emitting progress. It
// returns the folder, empty when the user cancelled.
func (a *App) SaveAttachmentsTo(refs []AttachmentRefDTO) (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	if len(refs) == 0 {
		return "", nil
	}
	dir, err := a.openDirectoryDialog(openDialogOptions{Title: "Save attachments to folder"})
	if err != nil || dir == "" {
		return "", err
	}
	total := len(refs)
	defer a.emit(EventAttachmentProgress, AttachmentProgressEvent{Running: false, FilesDone: total, FilesTotal: total})
	for i, ref := range refs {
		target, err := a.findAttachment(ref.MessageID, ref.AttachmentID)
		if err == nil {
			target, err = a.localAttachment(*target)
		}
		if err == nil {
			err = a.copyAttachmentProgress(*target, uniqueDestPath(dir, target.Filename), i, total)
		}
		if err != nil {
			a.emit(EventAttachmentProgress, AttachmentProgressEvent{Running: false, Error: err.Error(), FilesDone: i, FilesTotal: total})
			return "", err
		}
	}
	return dir, nil
}
