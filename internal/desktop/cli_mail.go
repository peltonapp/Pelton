package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/cliproto"
	"github.com/peltonapp/Pelton/internal/cliserver"
	"github.com/peltonapp/Pelton/internal/mailexport"
	"github.com/peltonapp/Pelton/internal/storage"
)

// cliListLimit is how many messages list and search return unless asked;
// cliListMax is the most they return.
const (
	cliListLimit = 50
	cliListMax   = 5000
	cliPage      = 200
)

// cliExportBatch is how many messages one connection fetches for an export.
const cliExportBatch = 50

func cliLimit(n int) int {
	if n <= 0 {
		return cliListLimit
	}
	return min(n, cliListMax)
}

// cliAccount resolves an account by id, address or name. An empty spec is
// nil, meaning every account.
func (a *App) cliAccount(ctx context.Context, spec string) (*storage.Account, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, nil
	}
	accounts, err := a.store.ListAccounts(ctx)
	if err != nil {
		return nil, err
	}
	id, numeric := strconv.ParseInt(spec, 10, 64)
	for i := range accounts {
		acc := &accounts[i]
		if (numeric == nil && acc.ID == id) || strings.EqualFold(acc.Email, spec) || strings.EqualFold(acc.Label(), spec) {
			return acc, nil
		}
	}
	return nil, cliproto.Errorf(cliproto.CodeNotFound, "no account %q", spec)
}

// cliAccounts is the accounts a request covers: the one named, or all.
func (a *App) cliAccounts(ctx context.Context) (cliserver.Response, error) {
	accounts, err := a.store.ListAccounts(ctx)
	if err != nil {
		return cliserver.Response{}, err
	}
	out := make([]cliproto.Account, 0, len(accounts))
	for _, acc := range accounts {
		out = append(out, cliproto.Account{ID: acc.ID, Email: acc.Email, Name: acc.Label()})
	}
	return cliserver.Response{Data: out, Touched: "accounts"}, nil
}

// cliSees reports whether the command may see a folder: any folder outside
// a vault, and a vault's only while it is unlocked and allows the command.
func (a *App) cliSees(ctx context.Context, folderID int64) bool {
	vault, err := a.store.FolderVault(ctx, folderID)
	if err != nil {
		return false
	}
	return vault == 0 || (mailVaultGate{a}).Visible(vault, storage.VaultAudienceCLI)
}

// cliVisibleFolders lists the folders of one account, or every account,
// that the command may see.
func (a *App) cliVisibleFolders(ctx context.Context, account *storage.Account) ([]storage.Folder, error) {
	var accounts []storage.Account
	if account != nil {
		accounts = []storage.Account{*account}
	} else {
		var err error
		if accounts, err = a.store.ListAccounts(ctx); err != nil {
			return nil, err
		}
	}
	var out []storage.Folder
	for _, acc := range accounts {
		folders, err := a.store.ListFolders(ctx, acc.ID)
		if err != nil {
			return nil, err
		}
		for _, f := range folders {
			if a.cliSees(ctx, f.ID) {
				out = append(out, f)
			}
		}
	}
	return out, nil
}

func (a *App) cliFolderList(ctx context.Context, req cliproto.FolderRequest) (cliserver.Response, error) {
	account, err := a.cliAccount(ctx, req.Account)
	if err != nil {
		return cliserver.Response{}, err
	}
	folders, err := a.cliVisibleFolders(ctx, account)
	if err != nil {
		return cliserver.Response{}, err
	}
	out := make([]cliproto.Folder, 0, len(folders))
	for _, f := range folders {
		out = append(out, cliFolder(f))
	}
	touched := "folders"
	if account != nil {
		touched = "folders of " + account.Email
	}
	return cliserver.Response{Data: out, Touched: touched}, nil
}

func cliFolder(f storage.Folder) cliproto.Folder {
	role := folderRole(f)
	if role == roleNormal {
		role = ""
	}
	return cliproto.Folder{ID: f.ID, AccountID: f.AccountID, Name: f.Name, Path: f.IMAPPath, Role: role}
}

// cliRoles are the folder roles a folder can be named by.
var cliRoles = []string{roleInbox, roleSent, roleDrafts, roleArchive, roleTrash, roleJunk}

// cliResolveFolders finds the folders a spec names: a folder id, a role, a
// path or a name, in one account or every one. An empty spec is the inbox.
// Without an account, a role skips imported mail as the unified views do.
func (a *App) cliResolveFolders(ctx context.Context, spec string, account *storage.Account) ([]storage.Folder, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		spec = roleInbox
	}
	folders, err := a.cliVisibleFolders(ctx, account)
	if err != nil {
		return nil, err
	}
	if id, err := strconv.ParseInt(spec, 10, 64); err == nil {
		for _, f := range folders {
			if f.ID == id {
				return []storage.Folder{f}, nil
			}
		}
	}
	var local map[int64]bool
	if account == nil {
		local = map[int64]bool{}
		if accounts, err := a.store.ListAccounts(ctx); err == nil {
			for _, acc := range accounts {
				local[acc.ID] = acc.Local
			}
		}
	}
	matchers := []func(storage.Folder) bool{
		func(f storage.Folder) bool { return f.IMAPPath == spec },
		func(f storage.Folder) bool {
			return strings.EqualFold(f.Name, spec) || strings.EqualFold(f.IMAPPath, spec)
		},
	}
	if slices.Contains(cliRoles, strings.ToLower(spec)) {
		role := strings.ToLower(spec)
		matchers = append([]func(storage.Folder) bool{func(f storage.Folder) bool {
			return folderRole(f) == role && !local[f.AccountID]
		}}, matchers...)
	}
	for _, match := range matchers {
		var out []storage.Folder
		for _, f := range folders {
			if match(f) {
				out = append(out, f)
			}
		}
		if len(out) > 0 {
			return out, nil
		}
	}
	return nil, cliproto.Errorf(cliproto.CodeNotFound, "no folder %q", spec)
}

// cliMessages projects stored messages, naming each folder once.
func (a *App) cliMessages(ctx context.Context, msgs []storage.Message) []cliproto.Message {
	names := map[int64]string{}
	out := make([]cliproto.Message, 0, len(msgs))
	for _, m := range msgs {
		name, ok := names[m.FolderID]
		if !ok {
			if f, err := a.store.GetFolder(ctx, m.FolderID); err == nil {
				name = f.Name
			}
			names[m.FolderID] = name
		}
		out = append(out, cliMessage(m, name))
	}
	return out
}

func cliMessage(m storage.Message, folder string) cliproto.Message {
	return cliproto.Message{
		ID: m.ID, AccountID: m.AccountID, FolderID: m.FolderID, Folder: folder,
		Date:        m.Date.UTC().Format(time.RFC3339),
		From:        cliproto.Address{Name: m.FromName, Email: m.FromAddress},
		Subject:     m.Subject,
		Unread:      m.Flags&storage.FlagSeen == 0,
		Flagged:     m.Flags&storage.FlagFlagged != 0,
		Attachments: m.HasAttachments,
	}
}

func (a *App) cliList(ctx context.Context, req cliproto.ListRequest) (cliserver.Response, error) {
	account, err := a.cliAccount(ctx, req.Account)
	if err != nil {
		return cliserver.Response{}, err
	}
	folders, err := a.cliResolveFolders(ctx, req.Folder, account)
	if err != nil {
		return cliserver.Response{}, err
	}
	var since time.Time
	if req.Since != "" {
		if since, err = time.Parse(time.RFC3339, req.Since); err != nil {
			return cliserver.Response{}, cliproto.Errorf(cliproto.CodeBadRequest, "since is not an RFC 3339 time")
		}
	}
	q := storage.MessageQuery{OneCopy: len(folders) > 1}
	for _, f := range folders {
		q.FolderIDs = append(q.FolderIDs, f.ID)
	}
	if req.Unread {
		q.ExcludeFlags = storage.FlagSeen
	}
	limit := cliLimit(req.Limit)
	var kept []storage.Message
	for offset := 0; len(kept) < limit; offset += cliPage {
		q.Limit, q.Offset = cliPage, offset
		page, err := a.store.QueryMessages(ctx, q)
		if err != nil {
			return cliserver.Response{}, err
		}
		done := len(page) < cliPage
		for _, m := range page {
			if !since.IsZero() && m.Date.Before(since) {
				done = true
				break
			}
			kept = append(kept, m)
			if len(kept) == limit {
				break
			}
		}
		if done {
			break
		}
	}
	names := make([]string, 0, len(folders))
	for _, f := range folders {
		names = append(names, f.Name)
	}
	return cliserver.Response{Data: a.cliMessages(ctx, kept), Touched: "list of " + strings.Join(names, ", ")}, nil
}

func (a *App) cliSearch(ctx context.Context, req cliproto.SearchRequest) (cliserver.Response, error) {
	account, err := a.cliAccount(ctx, req.Account)
	if err != nil {
		return cliserver.Response{}, err
	}
	search := parseCLISearch(req.Query, time.Now(), func(name string) (int64, bool) {
		folders, err := a.cliResolveFolders(ctx, name, account)
		if err != nil || len(folders) == 0 {
			return 0, false
		}
		return folders[0].ID, true
	})
	if account != nil {
		search.AccountID = account.ID
	}
	search.Limit = cliLimit(req.Limit)
	msgs, _, err := a.searchMessages(ctx, search)
	if err != nil {
		return cliserver.Response{}, err
	}
	visible := msgs[:0]
	for _, m := range msgs {
		if a.cliSees(ctx, m.FolderID) {
			visible = append(visible, m)
		}
	}
	return cliserver.Response{Data: a.cliMessages(ctx, visible), Touched: fmt.Sprintf("search, %d results", len(visible))}, nil
}

// cliReach reads a message the command may see, or says it was not found:
// one in a vault the command may not read is not found either, so a guessed
// id reads and changes nothing.
func (a *App) cliReach(ctx context.Context, id int64) (*storage.Message, error) {
	m, err := a.store.GetMessage(ctx, id)
	if err != nil {
		return nil, cliNotFound(err, fmt.Sprintf("message %d", id))
	}
	if !a.cliSees(ctx, m.FolderID) {
		return nil, cliproto.Errorf(cliproto.CodeNotFound, "message %d was not found", id)
	}
	return m, nil
}

func (a *App) cliShow(ctx context.Context, req cliproto.ShowRequest) (cliserver.Response, error) {
	touched := fmt.Sprintf("message %d", req.ID)
	m, err := a.cliReach(ctx, req.ID)
	if err != nil {
		return cliserver.Response{Touched: touched}, err
	}
	folder := ""
	if f, err := a.store.GetFolder(ctx, m.FolderID); err == nil {
		folder = f.Name
	}
	out := cliproto.MessageDetail{
		Message: cliMessage(*m, folder),
		To:      m.ToAddresses, Cc: m.CcAddresses, ReplyTo: m.ReplyTo, MessageID: m.MessageID,
		Text: m.BodyPlain, HTML: m.BodyHTML, VaultSealed: m.VaultID != 0,
	}
	if out.AttachList, err = a.cliAttachmentList(ctx, m.ID); err != nil {
		return cliserver.Response{Touched: touched}, err
	}
	if req.Headers || req.Raw {
		src := a.fetchEMLSources([]*storage.Message{m})[0]
		if src.err != nil {
			return cliserver.Response{Touched: touched}, src.err
		}
		if req.Raw {
			out.Raw = string(src.raw)
		}
		if req.Headers {
			out.Headers = headerBlock(src.raw)
		}
	}
	return cliserver.Response{Data: out, Touched: touched}, nil
}

// headerBlock is the header section of a message source.
func headerBlock(raw []byte) string {
	text := string(raw)
	for _, sep := range []string{"\r\n\r\n", "\n\n"} {
		if i := strings.Index(text, sep); i >= 0 {
			return text[:i+len(sep)/2]
		}
	}
	return text
}

func (a *App) cliAttachmentList(ctx context.Context, id int64) ([]cliproto.Attachment, error) {
	atts, err := a.store.ListAttachments(ctx, id)
	if err != nil {
		return nil, err
	}
	out := make([]cliproto.Attachment, 0, len(atts))
	for _, at := range atts {
		out = append(out, cliproto.Attachment{
			ID: at.ID, Name: at.Filename, ContentType: at.ContentType, Size: at.SizeBytes, Inline: at.ContentID != "",
		})
	}
	return out, nil
}

func (a *App) cliAttachments(ctx context.Context, req cliproto.IDRequest) (cliserver.Response, error) {
	touched := fmt.Sprintf("attachments of message %d", req.ID)
	if _, err := a.cliReach(ctx, req.ID); err != nil {
		return cliserver.Response{Touched: touched}, err
	}
	list, err := a.cliAttachmentList(ctx, req.ID)
	return cliserver.Response{Data: list, Touched: touched}, err
}

// cliAttachment returns one attachment's bytes, fetching them from the
// server first when they are not stored.
func (a *App) cliAttachment(ctx context.Context, req cliproto.AttachmentRequest) (cliserver.Response, error) {
	touched := fmt.Sprintf("an attachment of message %d", req.ID)
	if _, err := a.cliReach(ctx, req.ID); err != nil {
		return cliserver.Response{Touched: touched}, err
	}
	atts, err := a.store.ListAttachments(ctx, req.ID)
	if err != nil {
		return cliserver.Response{Touched: touched}, err
	}
	var target *storage.Attachment
	for i := range atts {
		if atts[i].Filename == req.Name || strconv.FormatInt(atts[i].ID, 10) == req.Name {
			target = &atts[i]
			break
		}
	}
	if target == nil {
		return cliserver.Response{Touched: touched}, cliproto.Errorf(cliproto.CodeNotFound, "message %d has no attachment %q", req.ID, req.Name)
	}
	local, err := a.localAttachment(*target)
	if err != nil {
		return cliserver.Response{Touched: touched}, err
	}
	r, err := a.store.OpenAttachment(ctx, *local)
	if err != nil {
		return cliserver.Response{Touched: touched}, err
	}
	defer r.Close()
	data, err := io.ReadAll(r)
	if err != nil {
		return cliserver.Response{Touched: touched}, err
	}
	if data == nil {
		data = []byte{}
	}
	return cliserver.Response{Bytes: data, ContentType: "application/octet-stream", Touched: touched}, nil
}

// cliAct applies one action to every message, answering for each. Moving
// to the trash a message that is already there, or whose account has no
// trash, deletes it for good, and that asks first.
func (a *App) cliAct(ctx context.Context, req cliproto.ActRequest) (cliserver.Response, error) {
	touched := req.Action + " " + touchedIDs("messages", req.IDs)
	if len(req.IDs) == 0 {
		return cliserver.Response{Touched: touched}, cliproto.Errorf(cliproto.CodeBadRequest, "no message ids")
	}
	if req.Action == cliproto.ActMove && strings.TrimSpace(req.To) == "" {
		return cliserver.Response{Touched: touched}, cliproto.Errorf(cliproto.CodeBadRequest, "move needs a folder to move to")
	}
	results := make([]cliproto.Result, 0, len(req.IDs))
	msgs := map[int64]*storage.Message{}
	var permanent []int64
	for _, id := range req.IDs {
		m, err := a.cliReach(ctx, id)
		if err != nil {
			results = append(results, cliResult(id, err))
			continue
		}
		msgs[id] = m
		if req.Action == cliproto.ActTrash && a.deletesForGood(ctx, m) {
			permanent = append(permanent, id)
		}
	}
	if len(permanent) > 0 {
		summary := fmt.Sprintf("Delete %d message(s) permanently. They are in the trash already, or their account has no trash, so they cannot be brought back.\n\nMessage ids: %s",
			len(permanent), strings.TrimPrefix(touchedIDs("", permanent), " "))
		if err := a.cliConfirm(ctx, cliproto.CapDeleteUnasked, "cli.delete", summary); err != nil {
			for _, id := range permanent {
				results = append(results, cliResult(id, err))
				delete(msgs, id)
			}
		}
	}
	for _, id := range req.IDs {
		m, ok := msgs[id]
		if !ok {
			continue
		}
		results = append(results, cliResult(id, a.cliApply(ctx, req, m)))
	}
	slices.SortStableFunc(results, func(x, y cliproto.Result) int {
		return slices.Index(req.IDs, x.ID) - slices.Index(req.IDs, y.ID)
	})
	return cliserver.Response{Data: results, Touched: touched}, nil
}

func (a *App) cliApply(ctx context.Context, req cliproto.ActRequest, m *storage.Message) error {
	switch req.Action {
	case cliproto.ActFlag, cliproto.ActUnflag:
		return a.SetFlagged(m.ID, req.Action == cliproto.ActFlag)
	case cliproto.ActRead, cliproto.ActUnread:
		return a.SetSeen(m.ID, req.Action == cliproto.ActRead)
	case cliproto.ActArchive:
		_, err := a.ArchiveMessage(m.ID)
		return err
	case cliproto.ActTrash:
		return a.DeleteMessage(m.ID)
	case cliproto.ActMove:
		account, err := a.store.GetAccount(ctx, m.AccountID)
		if err != nil {
			return err
		}
		dest, err := a.cliResolveFolders(ctx, req.To, account)
		if err != nil {
			return err
		}
		if len(dest) != 1 {
			return cliproto.Errorf(cliproto.CodeBadRequest, "%q names %d folders; give the folder's path or id", req.To, len(dest))
		}
		_, err = a.MoveMessage(m.ID, dest[0].ID)
		return err
	}
	return cliproto.Errorf(cliproto.CodeBadRequest, "unknown action %q", req.Action)
}

// deletesForGood reports whether deleting m removes it rather than moving
// it to the trash.
func (a *App) deletesForGood(ctx context.Context, m *storage.Message) bool {
	account, err := a.store.GetAccount(ctx, m.AccountID)
	if err != nil || account.Local {
		return true
	}
	trash, ok := a.findTrashFolder(m.AccountID)
	return !ok || trash.ID == m.FolderID
}

func cliResult(id int64, err error) cliproto.Result {
	if err == nil {
		return cliproto.Result{ID: id, OK: true}
	}
	e := cliproto.AsError(err)
	return cliproto.Result{ID: id, Code: e.Code, Error: e.Message}
}

// cliExport streams the sources of messages, one JSON line each, a batch
// per server connection. A message that cannot be read is a line with its
// error, and the rest still go.
func (a *App) cliExport(ctx context.Context, req cliproto.ExportRequest) (cliserver.Response, error) {
	var msgs []*storage.Message
	var touched string
	if len(req.IDs) > 0 {
		touched = touchedIDs("messages", req.IDs)
		for _, id := range req.IDs {
			m, err := a.cliReach(ctx, id)
			if err != nil {
				return cliserver.Response{Touched: touched}, err
			}
			msgs = append(msgs, m)
		}
	} else {
		account, err := a.cliAccount(ctx, req.Account)
		if err != nil {
			return cliserver.Response{}, err
		}
		folders, err := a.cliResolveFolders(ctx, req.Folder, account)
		if err != nil {
			return cliserver.Response{}, err
		}
		if len(folders) != 1 {
			return cliserver.Response{}, cliproto.Errorf(cliproto.CodeBadRequest, "%q names %d folders; give the account too", req.Folder, len(folders))
		}
		touched = "folder " + folders[0].Name
		for offset := 0; ; offset += cliPage {
			page, err := a.store.QueryMessages(ctx, storage.MessageQuery{FolderIDs: []int64{folders[0].ID}, Limit: cliPage, Offset: offset})
			if err != nil {
				return cliserver.Response{Touched: touched}, err
			}
			for i := range page {
				msgs = append(msgs, &page[i])
			}
			if len(page) < cliPage {
				break
			}
		}
	}
	stream := func(w io.Writer) error {
		enc := json.NewEncoder(w)
		flush := func() {
			if f, ok := w.(interface{ Flush() }); ok {
				f.Flush()
			}
		}
		groups := map[int64][]*storage.Message{}
		var order []int64
		for _, m := range msgs {
			if _, seen := groups[m.FolderID]; !seen {
				order = append(order, m.FolderID)
			}
			groups[m.FolderID] = append(groups[m.FolderID], m)
		}
		for _, folder := range order {
			for batch := range slices.Chunk(groups[folder], cliExportBatch) {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				sources := a.fetchEMLSources(batch)
				for i, m := range batch {
					item := cliproto.ExportItem{
						ID:   m.ID,
						Name: mailexport.FileName(exportMeta(m), a.emlOptions("", m).Template),
						Date: m.Date.UTC().Format(time.RFC3339),
					}
					if err := sources[i].err; err != nil {
						item.Error = err.Error()
					} else if len(sources[i].raw) == 0 {
						item.Error = errors.New("the message has no content to export").Error()
					} else {
						item.Raw = sources[i].raw
					}
					if err := enc.Encode(item); err != nil {
						return err
					}
				}
				flush()
			}
		}
		return enc.Encode(cliproto.ExportItem{End: true})
	}
	return cliserver.Response{Stream: stream, ContentType: "application/x-ndjson", Touched: fmt.Sprintf("%s, %d messages", touched, len(msgs))}, nil
}
