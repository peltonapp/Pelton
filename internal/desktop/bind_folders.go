package desktop

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/peltonapp/Pelton/internal/storage"
	psync "github.com/peltonapp/Pelton/internal/sync"
)

// Mailbox management (#132): create, rename and delete folders from the
// sidebar. Each one changes the server first and only then the local cache, so
// a failed command leaves the cache untouched rather than describing a
// mailbox that is not there.
//
// The local tree is updated directly rather than rediscovered, because the
// caller knows exactly what changed. A rename in particular is indistinguishable
// from a delete plus a create when seen through a fresh LIST, and rediscovering
// it that way would throw out the folder's cached mail.

// errFolderProtected is returned for an attempt to rename or delete a mailbox
// the app itself depends on.
var errFolderProtected = errors.New("this folder is used by Pelton and cannot be renamed or deleted")

// errUnknownRole is returned for a role the ui should never have sent.
var errUnknownRole = errors.New("pelton: unknown folder role")

// SetFolderRole records the role a user assigned to a mailbox by hand,
// overriding both detection steps. An empty role clears the assignment and
// hands the folder back to automatic detection.
//
// This exists because detection cannot be made to work everywhere: a server
// that reports no \Special-Use attribute and does not name its mailboxes with
// the english defaults leaves that mail cached but missing from the unified
// views, and no name list would ever cover every server (#186).
//
// Nothing is sent to the server. The role is local classification, which is why
// assigning one never renames or moves the mailbox.
func (a *App) SetFolderRole(folderID int64, role string) error {
	if err := a.ready(); err != nil {
		return err
	}
	if role != "" && !validFolderRole(role) {
		return fmt.Errorf("%w: %s", errUnknownRole, role)
	}
	return a.store.SetFolderRoleOverride(a.ctx, folderID, role)
}

// CreateFolderRequest names a new mailbox. ParentID 0 creates it at the root of
// the account; otherwise it is created as a child of that folder.
type CreateFolderRequest struct {
	AccountID int64  `json:"accountId"`
	ParentID  int64  `json:"parentId"`
	Name      string `json:"name"`
}

// CreateFolder creates a mailbox on the server and adds it to the local tree.
// The name is a single level: any hierarchy comes from ParentID, so a name
// containing the server's delimiter is rejected rather than silently creating
// something nested.
func (a *App) CreateFolder(req CreateFolderRequest) (FolderDTO, error) {
	if err := a.ready(); err != nil {
		return FolderDTO{}, err
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return FolderDTO{}, errors.New("folder name cannot be empty")
	}

	account, err := a.store.GetAccount(a.ctx, req.AccountID)
	if err != nil {
		return FolderDTO{}, err
	}

	var (
		parent   *storage.Folder
		delim    string
		parentID string
		path     = name
	)
	if req.ParentID != 0 {
		p, err := a.store.GetFolder(a.ctx, req.ParentID)
		if err != nil {
			return FolderDTO{}, err
		}
		if p.AccountID != req.AccountID {
			return FolderDTO{}, errors.New("parent folder belongs to a different account")
		}
		parent = p
		delim = p.Delimiter
		parentID = p.RemoteID
		if parentID == "" {
			parentID = p.IMAPPath
		}
		path, err = a.protocolFor(*account).folderPath(*p, name)
		if err != nil {
			return FolderDTO{}, err
		}
	} else {
		delim = a.accountDelimiter(req.AccountID)
	}
	if delim != "" && strings.Contains(name, delim) {
		return FolderDTO{}, fmt.Errorf("a folder name cannot contain %q", delim)
	}

	var created psync.Mailbox
	if err := a.withAccountAdapter(req.AccountID, func(adapter psync.Adapter) error {
		var err error
		created, err = adapter.CreateMailbox(a.ctx, name, parentID)
		return err
	}); err != nil {
		return FolderDTO{}, err
	}

	remoteID := created.RemoteID
	if remoteID == "" {
		remoteID = path
	}
	imapPath := a.protocolFor(*account).createdPath(path, remoteID)

	folder := storage.Folder{
		AccountID: req.AccountID,
		Name:      name,
		IMAPPath:  imapPath,
		RemoteID:  remoteID,
		Delimiter: delim,
	}
	if parent != nil {
		folder.ParentID = &parent.ID
	}
	if _, err := a.store.CreateFolder(a.ctx, &folder); err != nil {
		return FolderDTO{}, err
	}
	return toFolderDTO(folder), nil
}

// RenameFolder renames a mailbox in place, keeping its cached messages. The
// name is again a single level: the folder stays where it is in the hierarchy,
// so only the last path segment changes. Special mailboxes are refused.
func (a *App) RenameFolder(id int64, name string) error {
	if err := a.ready(); err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("folder name cannot be empty")
	}

	folder, err := a.store.GetFolder(a.ctx, id)
	if err != nil {
		return err
	}
	if err := protectSpecialFolder(*folder); err != nil {
		return err
	}
	delim := folder.Delimiter
	if delim != "" && strings.Contains(name, delim) {
		return fmt.Errorf("a folder name cannot contain %q", delim)
	}
	if name == folder.Name {
		return nil
	}

	account, err := a.store.GetAccount(a.ctx, folder.AccountID)
	if err != nil {
		return err
	}

	remoteID := folder.RemoteID
	if remoteID == "" {
		remoteID = folder.IMAPPath
	}
	newPath := renamedPath(folder.IMAPPath, name, delim)

	if err := a.withAccountAdapter(folder.AccountID, func(adapter psync.Adapter) error {
		return adapter.RenameMailbox(a.ctx, remoteID, name)
	}); err != nil {
		return err
	}

	// a driver whose remote id is not the path keeps the stored path; IMAP
	// rewrites it.
	if a.protocolFor(*account).renameKeepsPath(*folder, remoteID) {
		return a.store.RenameFolder(a.ctx, id, name, folder.IMAPPath)
	}

	// the server moved the children along with the parent, so their stored paths
	// have to follow. do the subtree first: if the process dies between the two,
	// a stale parent path is recoverable by renaming again, whereas orphaned
	// children whose parent already moved are not findable by prefix any more.
	if delim != "" {
		if _, err := a.store.RenameFolderSubtree(a.ctx,
			folder.AccountID, folder.IMAPPath+delim, newPath+delim); err != nil {
			return err
		}
	}
	return a.store.RenameFolder(a.ctx, id, name, newPath)
}

// DeleteFolder deletes a mailbox on the server and drops it, its descendants and
// their cached messages locally. Special mailboxes are refused. The caller is
// expected to have confirmed with the user: this destroys mail on the server.
func (a *App) DeleteFolder(id int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	folder, err := a.store.GetFolder(a.ctx, id)
	if err != nil {
		return err
	}
	if err := protectSpecialFolder(*folder); err != nil {
		return err
	}

	// descendants first, deepest last from the query, so each mailbox is gone
	// from the server before its parent. a server that refuses to delete a
	// mailbox with children then never sees that case.
	targets, err := a.subtreeFolders(*folder)
	if err != nil {
		return err
	}
	if err := a.withAccountAdapter(folder.AccountID, func(adapter psync.Adapter) error {
		for _, target := range slices.Backward(targets) {
			rid := target.RemoteID
			if rid == "" {
				rid = target.IMAPPath
			}
			if err := adapter.DeleteMailbox(a.ctx, rid); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}

	for _, target := range slices.Backward(targets) {
		if _, err := a.store.PurgeFolderMessages(a.ctx, target.AccountID, target.ID); err != nil {
			return err
		}
		if err := a.store.DeleteFolder(a.ctx, target.ID); err != nil {
			return err
		}
	}
	goSafe("counting unread mail", a.refreshViewCounts)
	return nil
}

// errNotTrash is returned when EmptyTrash is pointed at a folder that is not
// the account's trash. Emptying is unrecoverable, so the role is checked here
// rather than trusted from the caller.
var errNotTrash = errors.New("pelton: that folder is not a trash folder")

// EmptyTrash deletes every message in a trash folder and returns how many it
// marked. It is the same delete the per-message action performs, applied to the
// whole folder: the rows are marked pending (which takes them out of the lists
// at once) and the server expunge is pushed by a folder sync started in the
// background, so a failed or offline push is retried by the next regular sync
// instead of being lost.
//
// Nothing is moved anywhere. Messages in the trash have already been deleted
// once; this is the second, permanent one.
func (a *App) EmptyTrash(folderID int64) (int, error) {
	if err := a.ready(); err != nil {
		return 0, err
	}
	folder, err := a.store.GetFolder(a.ctx, folderID)
	if err != nil {
		return 0, err
	}
	if folderRole(*folder) != roleTrash {
		return 0, errNotTrash
	}

	marked, err := a.store.MarkFolderDeletePending(a.ctx, folderID)
	if err != nil {
		return 0, err
	}
	if marked == 0 {
		return 0, nil
	}

	goSafe("counting unread mail", a.refreshViewCounts)
	goSafe("emptying the trash", func() {
		if err := a.withAccountAdapter(folder.AccountID, func(adapter psync.Adapter) error {
			return a.syncOneFolder(adapter, *folder)
		}); err != nil {
			a.log.Error("push emptied trash", "folder", folder.ID, "err", err)
		}
	})
	return marked, nil
}

// subtreeFolders returns the folder and everything nested under it, shallowest
// first. On a flat server a folder has no descendants by definition.
func (a *App) subtreeFolders(folder storage.Folder) ([]storage.Folder, error) {
	out := []storage.Folder{folder}
	if folder.Delimiter == "" {
		return out, nil
	}
	children, err := a.store.FolderDescendants(a.ctx, folder.AccountID, folder.IMAPPath+folder.Delimiter)
	if err != nil {
		return nil, err
	}
	return append(out, children...), nil
}

// accountDelimiter reports the hierarchy delimiter the account's folders use,
// read off any folder that has one. Empty means a flat server, or an account
// with no folders discovered yet.
func (a *App) accountDelimiter(accountID int64) string {
	folders, err := a.store.ListFolders(a.ctx, accountID)
	if err != nil {
		return ""
	}
	for _, f := range folders {
		if f.Delimiter != "" {
			return f.Delimiter
		}
	}
	return ""
}

// renamedPath swaps the last segment of a mailbox path for a new name, keeping
// the folder at the same level of the hierarchy.
func renamedPath(path, name, delim string) string {
	if delim == "" {
		return name
	}
	if parent, _, ok := strings.CutLast(path, delim); ok {
		return parent + delim + name
	}
	return name
}

// protectSpecialFolder refuses to touch INBOX or a mailbox mapped to one of the
// roles the app routes mail through. Renaming Sent out from under the app would
// leave sent mail with nowhere to land, and the server's own special-use
// attribute does not move with a rename on every server.
func protectSpecialFolder(f storage.Folder) error {
	if folderRole(f) != roleNormal {
		return fmt.Errorf("%w: %s", errFolderProtected, f.Name)
	}
	return nil
}

// withAccountAdapter runs fn against a sync adapter for an account, taking the
// same lock sync uses so a folder operation never races a sync on the same
// account.
func (a *App) withAccountAdapter(accountID int64, fn func(psync.Adapter) error) error {
	account, err := a.store.GetAccount(a.ctx, accountID)
	if err != nil {
		return err
	}

	accountMu := a.accountLock(account.ID)
	accountMu.Lock()
	defer accountMu.Unlock()

	return a.protocolFor(*account).withAdapter(account, fn)
}
