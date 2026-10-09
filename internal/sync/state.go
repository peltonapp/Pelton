package sync

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/peltonapp/Pelton/internal/storage"
)

// FolderSyncState is the per-folder state that makes the next sync cheaper:
// the generation we last saw, the highest legacy uid we have processed and the
// sync window's floor. The set of pending local operations is derived from the
// message rows themselves, not stored here, so there is a single source of
// truth.
type FolderSyncState struct {
	StoredUIDValidity uint32
	LastSeenUID       uint32
	// SyncFloorUID is the lowest legacy uid the cache covers; 0 means the
	// folder is cached in full or the protocol does not use numeric floors.
	SyncFloorUID uint32
	// SyncFloorID is the opaque remote-id floor of the newest-first window.
	SyncFloorID string
	// SyncInitialized is true once a folder has completed an initial sync,
	// including an empty first result.
	SyncInitialized bool
}

// loadFolderSyncState reads the stored sync state for a folder.
func loadFolderSyncState(ctx context.Context, store *storage.DB, folder storage.Folder) (FolderSyncState, error) {
	fresh, err := store.GetFolder(ctx, folder.ID)
	if err != nil {
		return FolderSyncState{}, err
	}
	lastSeen, err := store.FolderLastSeenUID(ctx, folder.ID)
	if err != nil {
		return FolderSyncState{}, err
	}
	floor, err := store.FolderSyncFloorUID(ctx, folder.ID)
	if err != nil {
		return FolderSyncState{}, err
	}
	return FolderSyncState{
		StoredUIDValidity: fresh.UIDValidity,
		LastSeenUID:       lastSeen,
		SyncFloorUID:      floor,
		SyncFloorID:       fresh.SyncFloorID,
		SyncInitialized:   fresh.SyncInitialized,
	}, nil
}

// localView turns storage message states into reconcile inputs and an id lookup
// keyed by remote id, which the executor needs to act on the right row.
func localView(states []storage.MessageState) ([]LocalMessage, map[string]storage.MessageState) {
	locals := make([]LocalMessage, 0, len(states))
	byID := make(map[string]storage.MessageState, len(states))
	for _, s := range states {
		locals = append(locals, LocalMessage{
			RemoteID:      s.RemoteID,
			Flags:         s.Flags,
			PendingFlags:  s.PendingFlags,
			PendingDelete: s.PendingDelete,
		})
		byID[s.RemoteID] = s
	}
	return locals, byID
}

// storedGeneration is the decimal form of a folder's uid_validity, or "" when
// the folder has never recorded one (including an empty generation stored as
// 0).
func storedGeneration(uidValidity uint32) string {
	if uidValidity == 0 {
		return ""
	}
	return strconv.FormatUint(uint64(uidValidity), 10)
}

// generationToUIDValidity parses a decimal generation into uid_validity.
// Empty and "0" both mean no generation.
func generationToUIDValidity(generation string) uint32 {
	if generation == "" || generation == "0" {
		return 0
	}
	n, err := strconv.ParseUint(generation, 10, 32)
	if err != nil {
		return 0
	}
	return uint32(n)
}

// generationsEqual implements plan choice 2: "" and "0" are the same empty
// generation and do not purge.
func generationsEqual(stored, server string) bool {
	return normalizeGeneration(stored) == normalizeGeneration(server)
}

func normalizeGeneration(g string) string {
	if g == "" || g == "0" {
		return ""
	}
	return g
}

// headersToServers converts ListMessages headers into reconcile inputs.
func headersToServers(headers []Header) []ServerMessage {
	out := make([]ServerMessage, 0, len(headers))
	for _, h := range headers {
		out = append(out, ServerMessage{RemoteID: h.RemoteID, Flags: h.Flags})
	}
	return out
}

// selectable reports whether a folder can be opened on the server. A
// \Noselect (or \NonExistent) folder is a container in the hierarchy, not a
// mailbox: sync has to leave it alone.
func selectable(f storage.Folder) bool {
	for _, attr := range f.Attributes {
		switch strings.ToLower(strings.TrimPrefix(attr, "\\")) {
		case "noselect", "nonexistent":
			return false
		}
	}
	return true
}

// highestLegacyUID returns the largest non-zero LegacyUID among headers.
func highestLegacyUID(headers []Header) uint32 {
	var max uint32
	for _, h := range headers {
		if h.LegacyUID > max {
			max = h.LegacyUID
		}
	}
	return max
}

// legacyFloorUID returns the LegacyUID of the header whose RemoteID matches
// floorID, or 0 when the floor is empty or the protocol does not supply one.
func legacyFloorUID(headers []Header, floorID string) uint32 {
	if floorID == "" {
		return 0
	}
	for _, h := range headers {
		if h.RemoteID == floorID {
			return h.LegacyUID
		}
	}
	return 0
}

// refreshFolder reloads a folder row so sync state tokens and remote ids are
// current even when the caller passed a stale value.
func refreshFolder(ctx context.Context, store *storage.DB, folder storage.Folder) (storage.Folder, error) {
	fresh, err := store.GetFolder(ctx, folder.ID)
	if err != nil {
		return folder, fmt.Errorf("sync: reload folder %d: %w", folder.ID, err)
	}
	return *fresh, nil
}

// pendingRemoteIDs lists cached ids with a local flag change or delete that is
// not on the server yet.
func pendingRemoteIDs(states []storage.MessageState) []string {
	var out []string
	for _, s := range states {
		if s.RemoteID != "" && (s.PendingFlags || s.PendingDelete) {
			out = append(out, s.RemoteID)
		}
	}
	return out
}
