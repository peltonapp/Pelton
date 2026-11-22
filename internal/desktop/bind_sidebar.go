package desktop

import (
	"errors"
	"fmt"
	"strings"
)

// errCrossGroupReorder is returned when a folder reorder mixes accounts or
// parents. Dragging a folder out of its own sibling group would mean an imap
// move, not a display change, so the ui does not offer it and this rejects it
// even if a caller tries.
var errCrossGroupReorder = errors.New("folders can only be reordered among their own siblings")

// ReorderFolders persists a new sidebar order for one group of sibling folders.
// Every id must belong to the same account and the same parent; the order of the
// slice becomes the display order. Folders outside the group are untouched.
func (a *App) ReorderFolders(orderedIDs []int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	if len(orderedIDs) < 2 {
		return nil
	}
	if err := a.sameFolderGroup(orderedIDs); err != nil {
		return err
	}
	return a.store.SetFolderPositions(a.ctx, orderedIDs)
}

// ReorderAccounts persists a new order for the sidebar's account sections.
func (a *App) ReorderAccounts(orderedIDs []int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	if len(orderedIDs) < 2 {
		return nil
	}
	return a.store.SetAccountPositions(a.ctx, orderedIDs)
}

// ListPinnedFolders returns the folders the user pinned, across every account,
// in the order of the sidebar's Pinned group. A pinned folder is mirrored there
// and still appears in its own account's tree.
func (a *App) ListPinnedFolders() ([]FolderDTO, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	folders, err := a.store.ListPinnedFolders(a.ctx)
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

// SetFolderPinned pins a folder to the sidebar's Pinned group or unpins it.
// Pinning appends to the end of the group and never removes the folder from its
// own tree. Pinning a folder that is already pinned does nothing.
func (a *App) SetFolderPinned(folderID int64, pinned bool) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.store.SetFolderPinned(a.ctx, folderID, pinned)
}

// SetFolderSyncExcluded turns syncing for one folder off or on. Excluding a
// folder does not delete what is already cached: it stops being updated and
// stays readable offline, so a mistaken uncheck costs nothing but a resync
// (#173).
func (a *App) SetFolderSyncExcluded(folderID int64, excluded bool) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.store.SetFolderSyncExcluded(a.ctx, folderID, excluded)
}

// ReorderPinnedFolders persists a new order for the Pinned group. Ids that are
// not pinned are ignored rather than pinned implicitly.
func (a *App) ReorderPinnedFolders(orderedIDs []int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	if len(orderedIDs) < 2 {
		return nil
	}
	return a.store.SetPinnedFolderPositions(a.ctx, orderedIDs)
}

// ReorderUnifiedViews persists a new order for the unified views block. The
// views are computed, not stored, so their order is a settings key holding the
// view keys. Unknown keys are rejected and any known key the caller left out is
// appended in its built-in position, so the stored order always covers all of
// them.
func (a *App) ReorderUnifiedViews(keys []string) error {
	if err := a.ready(); err != nil {
		return err
	}
	seen := make(map[string]bool, len(keys))
	for _, k := range keys {
		if !isUnifiedViewKey(k) {
			return fmt.Errorf("unknown unified view %q", k)
		}
		if seen[k] {
			return fmt.Errorf("unified view %q listed twice", k)
		}
		seen[k] = true
	}
	full := append([]string(nil), keys...)
	for _, v := range unifiedViewOrder {
		if !seen[v.key] {
			full = append(full, v.key)
		}
	}
	return a.store.Set(a.ctx, settingUnifiedViewOrder, strings.Join(full, ","))
}

// sameFolderGroup reports whether every id names a folder in one account under
// one parent, which is the only grouping a reorder may span.
func (a *App) sameFolderGroup(ids []int64) error {
	var (
		accountID int64
		parentID  int64
	)
	for i, id := range ids {
		f, err := a.store.GetFolder(a.ctx, id)
		if err != nil {
			return err
		}
		// a root folder has no parent; 0 is not a valid folder id, so it stands
		// in for "root" without needing a second nullable comparison.
		var parent int64
		if f.ParentID != nil {
			parent = *f.ParentID
		}
		if i == 0 {
			accountID, parentID = f.AccountID, parent
			continue
		}
		if f.AccountID != accountID || parent != parentID {
			return errCrossGroupReorder
		}
	}
	return nil
}

// sortedUnifiedViews returns the unified views in the user's stored order, with
// any view missing from that order (a key added by a later version, or a stored
// order written before it existed) kept in its built-in position.
func (a *App) sortedUnifiedViews() []unifiedView {
	return orderUnifiedViews(a.stringSetting(settingUnifiedViewOrder, ""))
}

// orderUnifiedViews applies a stored comma-separated key order to the built-in
// view list. Unknown and repeated keys are dropped; views the stored order does
// not mention keep their built-in position at the end.
func orderUnifiedViews(stored string) []unifiedView {
	if stored == "" {
		return unifiedViewOrder
	}
	byKey := make(map[string]unifiedView, len(unifiedViewOrder))
	for _, v := range unifiedViewOrder {
		byKey[v.key] = v
	}
	out := make([]unifiedView, 0, len(unifiedViewOrder))
	for key := range strings.SplitSeq(stored, ",") {
		if v, ok := byKey[key]; ok {
			out = append(out, v)
			delete(byKey, key)
		}
	}
	for _, v := range unifiedViewOrder {
		if _, ok := byKey[v.key]; ok {
			out = append(out, v)
		}
	}
	return out
}

// isUnifiedViewKey reports whether a key names one of the built-in views.
func isUnifiedViewKey(key string) bool {
	for _, v := range unifiedViewOrder {
		if v.key == key {
			return true
		}
	}
	return false
}
