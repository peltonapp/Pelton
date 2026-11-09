package sync

import (
	"context"
	"fmt"
	"strings"

	"github.com/emersion/go-imap/v2"

	"github.com/peltonapp/Pelton/internal/storage"
)

// labelKeywords maps color indices 1..8 to the Thunderbird-style imap keywords
// Pelton uses to sync flag colors across clients.
var labelKeywords = []imap.Flag{
	"$Label1", "$Label2", "$Label3", "$Label4",
	"$Label5", "$Label6", "$Label7", "$Label8",
}

// colorFromFlags returns the color index (1..8) encoded by a message's keyword
// flags, or 0 when none is present.
func colorFromFlags(flags []imap.Flag) int {
	for _, f := range flags {
		for i, lf := range labelKeywords {
			if strings.EqualFold(string(f), string(lf)) {
				return i + 1
			}
		}
	}
	return 0
}

// FolderSyncState is the per-folder state that makes the next sync cheaper:
// the UIDVALIDITY we last saw, the highest uid we have processed and the sync
// window's floor. The set of pending local operations is derived from the
// message rows themselves, not stored here, so there is a single source of
// truth.
type FolderSyncState struct {
	StoredUIDValidity uint32
	LastSeenUID       uint32
	// SyncFloorUID is the lowest uid the cache covers; 0 means the folder is
	// cached in full. See window.go.
	SyncFloorUID uint32
}

// loadFolderSyncState reads the stored sync state for a folder.
func loadFolderSyncState(ctx context.Context, store *storage.DB, folder storage.Folder) (FolderSyncState, error) {
	lastSeen, err := store.FolderLastSeenUID(ctx, folder.ID)
	if err != nil {
		return FolderSyncState{}, err
	}
	floor, err := store.FolderSyncFloorUID(ctx, folder.ID)
	if err != nil {
		return FolderSyncState{}, err
	}
	return FolderSyncState{
		StoredUIDValidity: folder.UIDValidity,
		LastSeenUID:       lastSeen,
		SyncFloorUID:      floor,
	}, nil
}

// localView turns storage message states into reconcile inputs and an id lookup
// keyed by uid, which the executor needs to act on the right row.
func localView(states []storage.MessageState) ([]LocalMessage, map[uint32]storage.MessageState) {
	locals := make([]LocalMessage, 0, len(states))
	byUID := make(map[uint32]storage.MessageState, len(states))
	for _, s := range states {
		locals = append(locals, LocalMessage{
			UID:           s.UID,
			Flags:         s.Flags,
			PendingFlags:  s.PendingFlags,
			PendingMask:   s.PendingMask,
			PendingDelete: s.PendingDelete,
		})
		byUID[s.UID] = s
	}
	return locals, byUID
}

// highestUID returns the largest uid among server messages, used to advance the
// folder high water mark after a sync.
func highestUID(servers []ServerMessage) uint32 {
	var max uint32
	for _, s := range servers {
		if s.UID > max {
			max = s.UID
		}
	}
	return max
}

// loadServerView fetches the uid+flags of every message in the selected mailbox
// and converts them to reconcile inputs. It also returns the color label encoded
// in each message's keyword flags (uid -> color, 0 when none), so the caller can
// adopt server-side color changes when color syncing is on.
func loadServerView(client mailClient) ([]ServerMessage, map[uint32]int, error) {
	headers, err := client.FetchAllFlags()
	if err != nil {
		return nil, nil, fmt.Errorf("sync: load server view: %w", err)
	}
	servers := make([]ServerMessage, 0, len(headers))
	colors := make(map[uint32]int, len(headers))
	for _, h := range headers {
		servers = append(servers, ServerMessage{
			UID:   uint32(h.UID),
			Flags: imapFlagsToStorage(h.Flags),
		})
		colors[uint32(h.UID)] = colorFromFlags(h.Flags)
	}
	return servers, colors, nil
}
