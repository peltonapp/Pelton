package desktop

import (
	"sort"
	"strings"

	goimap "github.com/emersion/go-imap/v2"

	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/storage"
)

// refreshFolders brings the stored folder tree in line with the server's list:
// a mailbox created elsewhere is added, and one deleted elsewhere is dropped
// with its cached mail. The tree used to be read once at setup, so a folder
// removed in webmail failed every sync after (#484).
//
// It reports whether anything changed, so the caller knows the sidebar needs
// reloading.
func (a *App) refreshFolders(client mailClient, accountID int64) (bool, error) {
	server, err := client.ListFolders()
	if err != nil {
		return false, err
	}
	onServer := make(map[string]bool, len(server))
	listed := make([]pimap.Folder, 0, len(server))
	hasInbox := false
	for _, f := range server {
		if f.HasAttr(goimap.MailboxAttrNonExistent) {
			continue
		}
		onServer[f.Name] = true
		listed = append(listed, f)
		if strings.EqualFold(f.Name, "INBOX") {
			hasInbox = true
		}
	}
	// every account has an INBOX. A list without one is a server answering
	// badly, and acting on it would drop folders that still exist.
	if !hasInbox {
		return false, nil
	}

	stored, err := a.store.ListFolders(a.ctx, accountID)
	if err != nil {
		return false, err
	}
	byPath := make(map[string]int64, len(stored))
	var gone []storage.Folder
	for _, f := range stored {
		byPath[f.IMAPPath] = f.ID
		if !onServer[f.IMAPPath] && !hasListedDescendant(f, listed) {
			gone = append(gone, f)
		}
	}

	// deepest first, so a parent is never deleted with a child still holding
	// attachment files on disk.
	sort.SliceStable(gone, func(i, j int) bool {
		return len(gone[i].IMAPPath) > len(gone[j].IMAPPath)
	})
	for _, f := range gone {
		if _, err := a.store.PurgeFolderMessages(a.ctx, accountID, f.ID); err != nil {
			return false, err
		}
		if err := a.store.DeleteFolder(a.ctx, f.ID); err != nil {
			return false, err
		}
		delete(byPath, f.IMAPPath)
		a.log.Info("folder gone from the server, dropped", "folder", f.IMAPPath)
	}

	var added []pimap.Folder
	for _, f := range listed {
		if _, ok := byPath[f.Name]; !ok {
			added = append(added, f)
		}
	}
	if err := a.createFolderTree(accountID, added, byPath); err != nil {
		return false, err
	}
	for _, f := range added {
		a.log.Info("folder new on the server, added", "folder", f.Name)
	}
	return len(gone) > 0 || len(added) > 0, nil
}

// hasListedDescendant reports whether the server still lists a folder nested
// under f. Some servers leave a bare parent out of the list while listing its
// children, and deleting the parent row would take the children with it.
func hasListedDescendant(f storage.Folder, listed []pimap.Folder) bool {
	if f.Delimiter == "" {
		return false
	}
	prefix := f.IMAPPath + f.Delimiter
	for _, l := range listed {
		if strings.HasPrefix(l.Name, prefix) {
			return true
		}
	}
	return false
}
