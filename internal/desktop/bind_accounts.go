package desktop

import (
	"context"

	"github.com/peltonapp/Pelton/internal/storage"
)

// ListAccounts returns all configured accounts. Accounts are created by the cli
// tools today; an in-app add-account flow is the next step and would call a new
// CreateAccount binding here plus a keyring write for credentials.
func (a *App) ListAccounts() ([]AccountDTO, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	accounts, err := a.store.ListAccounts(a.ctx)
	if err != nil {
		return nil, err
	}
	out := make([]AccountDTO, 0, len(accounts))
	for _, acc := range accounts {
		out = append(out, toAccountDTO(acc))
	}
	return out, nil
}

// ListFolders returns one account's full mailbox tree with per folder unread and
// total counts. The frontend builds the collapsible hierarchy from ParentID and
// respects the per server Delimiter for display.
func (a *App) ListFolders(accountID int64) ([]FolderDTO, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	folders, err := a.store.ListFolders(a.ctx, accountID)
	if err != nil {
		return nil, err
	}
	out := make([]FolderDTO, 0, len(folders))
	for _, f := range folders {
		dto := toFolderDTO(f)
		total, unread, err := a.store.FolderCounts(a.ctx, f.ID)
		if err != nil {
			return nil, err
		}
		dto.TotalCount = total
		dto.UnreadCount = unread
		out = append(out, dto)
	}
	return out, nil
}

// unifiedView is one entry in the built-in cross-account group. The label is a
// short English fallback so the message-list pane reads "Inbox", "Sent" and so
// on; the frontend localizes by key.
type unifiedView struct {
	key   string
	label string
}

// unifiedViewOrder is the thunderbird-style unified group in its built-in
// order: one entry per folder role, merged across every account. The user can
// rearrange it (#187), which is stored as a list of keys, so this stays the
// canonical set and the fallback order.
var unifiedViewOrder = []unifiedView{
	{viewInbox, "Inbox"},
	{viewFlagged, "Flagged"},
	{viewDrafts, "Drafts"},
	{viewSent, "Sent"},
	{viewArchive, "Archive"},
	{viewJunk, "Junk"},
	{viewTrash, "Bin"},
}

// ListUnifiedViews returns the cross account views shown at the top of the
// sidebar with aggregate counts, in the order the user arranged them. Unified
// Inbox is the default startup view; the others appear with their counts.
func (a *App) ListUnifiedViews() ([]UnifiedViewDTO, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}

	views := a.sortedUnifiedViews()
	out := make([]UnifiedViewDTO, 0, len(views))
	for _, v := range views {
		q, err := a.viewQuery(a.ctx, v.key)
		if err != nil {
			return nil, err
		}
		total, err := a.store.CountMessages(a.ctx, q)
		if err != nil {
			return nil, err
		}
		unread, err := a.store.UnreadCount(a.ctx, q.FolderIDs)
		if err != nil {
			return nil, err
		}
		out = append(out, UnifiedViewDTO{
			Key:         v.key,
			Label:       v.label,
			TotalCount:  total,
			UnreadCount: unread,
		})
	}
	return out, nil
}

// unified view keys, shared with the frontend.
const (
	viewInbox   = "inbox"
	viewFlagged = "flagged"
	viewSent    = "sent"
	viewDrafts  = "drafts"
	viewArchive = "archive"
	viewJunk    = "junk"
	viewTrash   = "trash"
)

// viewQuery resolves a unified view key into the storage query that backs it:
// the set of folder ids across all accounts whose role matches, plus the flag
// filter for the flagged view (which spans every selectable folder).
func (a *App) viewQuery(ctx context.Context, key string) (storage.MessageQuery, error) {
	accounts, err := a.store.ListAccounts(ctx)
	if err != nil {
		return storage.MessageQuery{}, err
	}

	var (
		folderIDs []int64
		require   storage.Flag
	)
	for _, acc := range accounts {
		// the unified views merge the mail your accounts are receiving. An
		// imported archive is not that, and folding it in would put years of
		// old mail into the unified inbox on the day of the import.
		if acc.Local {
			continue
		}
		folders, err := a.store.ListFolders(ctx, acc.ID)
		if err != nil {
			return storage.MessageQuery{}, err
		}
		for _, f := range folders {
			role := folderRole(f)
			switch key {
			case viewInbox:
				if role == roleInbox {
					folderIDs = append(folderIDs, f.ID)
				}
			case viewSent:
				if role == roleSent {
					folderIDs = append(folderIDs, f.ID)
				}
			case viewDrafts:
				if role == roleDrafts {
					folderIDs = append(folderIDs, f.ID)
				}
			case viewArchive:
				if role == roleArchive {
					folderIDs = append(folderIDs, f.ID)
				}
			case viewJunk:
				if role == roleJunk {
					folderIDs = append(folderIDs, f.ID)
				}
			case viewTrash:
				if role == roleTrash {
					folderIDs = append(folderIDs, f.ID)
				}
			case viewFlagged:
				// flagged spans everything except trash and junk.
				if role != roleTrash && role != roleJunk {
					folderIDs = append(folderIDs, f.ID)
				}
			}
		}
	}
	if key == viewFlagged {
		require = storage.FlagFlagged
	}
	return storage.MessageQuery{FolderIDs: folderIDs, RequireFlags: require, OneCopy: key == viewFlagged}, nil
}
