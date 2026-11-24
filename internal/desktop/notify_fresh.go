package desktop

import (
	"context"
	"maps"
	"slices"
	"time"

	"github.com/peltonapp/Pelton/internal/storage"
)

// New-mail notifications come only for mail that is new in the list that
// found it ("fresh"): a list records the ids it found that were not
// incomplete stubs already, and the body fetch that stores one of them
// notifies. Bodies stored for anything else (a backfill, a raised limit, an
// old stub refilled, a full check healing the cache, an opened message) only
// update the list.

// freshTTL is how long a fresh id waits for its body. One whose body never
// lands (gone from the server) is forgotten after it.
const freshTTL = time.Hour

// addFresh records ids of a folder as new mail.
func (rt *accountSync) addFresh(folderID int64, ids []string) {
	if len(ids) == 0 {
		return
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.fresh == nil {
		rt.fresh = make(map[int64]map[string]time.Time)
	}
	now := time.Now()
	for fid, set := range rt.fresh {
		maps.DeleteFunc(set, func(_ string, at time.Time) bool { return now.Sub(at) > freshTTL })
		if len(set) == 0 {
			delete(rt.fresh, fid)
		}
	}
	set := rt.fresh[folderID]
	if set == nil {
		set = make(map[string]time.Time, len(ids))
		rt.fresh[folderID] = set
	}
	for _, id := range ids {
		set[id] = now
	}
}

// takeFresh returns the fresh ids among ids and forgets them.
func (rt *accountSync) takeFresh(folderID int64, ids []string) []string {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	set := rt.fresh[folderID]
	var out []string
	for _, id := range ids {
		if _, ok := set[id]; ok {
			out = append(out, id)
			delete(set, id)
		}
	}
	return out
}

// incompleteStubs is the folder's stubs still waiting for a body inside the
// sync window, read before a list: a list returns them again, and they are
// not new mail.
func (a *App) incompleteStubs(ctx context.Context, folder storage.Folder) (map[string]struct{}, error) {
	floorUID, err := a.store.FolderSyncFloorUID(ctx, folder.ID)
	if err != nil {
		return nil, err
	}
	ids, err := a.store.RemoteIDsNeedingBodyNewest(ctx, folder.ID, floorUID, a.syncMessageLimit())
	if err != nil {
		return nil, err
	}
	set := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		set[id] = struct{}{}
	}
	return set, nil
}

// freshIDs is the ids of toFetch not in before.
func freshIDs(toFetch []string, before map[string]struct{}) []string {
	return slices.DeleteFunc(slices.Clone(toFetch), func(id string) bool {
		_, old := before[id]
		return old
	})
}

// notifyFresh runs the new-mail work for the fresh ids among a fetched batch
// that the fetch stored, and forgets every fresh id of the batch. The work
// needs the whole message, so it runs here, once, when the body is in, never
// for a stub. notify false leaves out the OS notification only (an opened
// message is not news); the scripts, promises and privacy shield still hear
// of it.
func (a *App) notifyFresh(ctx context.Context, folder storage.Folder, batch []string, stored []int64, notify bool) {
	rt := a.accountSync(folder.AccountID)
	if rt == nil {
		return
	}
	fresh := rt.takeFresh(folder.ID, batch)
	if len(fresh) == 0 || len(stored) == 0 {
		return
	}
	var ids []int64
	for _, remoteID := range fresh {
		id, err := a.store.MessageIDByRemoteID(ctx, folder.ID, remoteID)
		if err == nil && slices.Contains(stored, id) {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return
	}
	if notify {
		goSafe("announcing new mail", func() { a.notifyNewMail(folder, ids) })
	}
	a.afterArrival(folder, ids)
}

// afterArrival hands new mail whose body is stored to the scripts, the
// promise scan and the privacy shield. Mail in a mail vault is left out by
// each of them.
func (a *App) afterArrival(folder storage.Folder, ids []int64) {
	a.scriptMailArrived(ids, a.firstScriptSync(folder.AccountID))
	a.promisesAfterSync(folder, ids)
	a.privacyAfterSync(folder, ids)
}
