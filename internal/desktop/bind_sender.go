package desktop

import (
	"strings"

	"github.com/peltonapp/Pelton/internal/storage"
)

// SenderOverviewDTO is what the reader's sender panel shows about one address,
// all from the local store. Recent holds one message per conversation, newest
// first. Automated is a guess from the address alone (noreply and the like).
type SenderOverviewDTO struct {
	Address    string               `json:"address"`
	Count      int                  `json:"count"`
	First      string               `json:"first"`
	Last       string               `json:"last"`
	Recent     []MessageSummaryDTO  `json:"recent"`
	Files      []AttachmentEntryDTO `json:"files"`
	FilesTotal int                  `json:"filesTotal"`
	Automated  bool                 `json:"automated"`
}

const (
	senderRecentScan = 60
	senderRecentShow = 8
	senderFilesShow  = 12
)

// automatedLocalParts are mailbox names that belong to machines, not people.
var automatedLocalParts = []string{
	"noreply", "no-reply", "no_reply", "donotreply", "do-not-reply", "do_not_reply",
	"mailer-daemon", "postmaster", "bounce", "bounces", "notification", "notifications",
	"notify", "alerts", "newsletter", "automated",
}

// looksAutomated reports whether an address reads as a machine sender.
func looksAutomated(address string) bool {
	local, _, ok := strings.Cut(address, "@")
	if !ok {
		return false
	}
	local = strings.ToLower(local)
	if base, _, found := strings.Cut(local, "+"); found {
		local = base
	}
	for _, name := range automatedLocalParts {
		if local == name {
			return true
		}
	}
	return strings.HasPrefix(local, "noreply") || strings.HasPrefix(local, "no-reply")
}

// SenderOverview returns the sender panel's summary of one address.
func (a *App) SenderOverview(address string) (SenderOverviewDTO, error) {
	if err := a.ready(); err != nil {
		return SenderOverviewDTO{}, err
	}
	addr := bareAddress(address)
	out := SenderOverviewDTO{
		Address:   addr,
		Recent:    []MessageSummaryDTO{},
		Files:     []AttachmentEntryDTO{},
		Automated: looksAutomated(addr),
	}
	if addr == "" {
		return out, nil
	}
	folders, err := a.browsableFolders(a.ctx)
	if err != nil {
		return SenderOverviewDTO{}, err
	}
	stats, err := a.store.SenderStatsFor(a.ctx, addr, folders)
	if err != nil {
		return SenderOverviewDTO{}, err
	}
	out.Count, out.First, out.Last = stats.Count, formatDate(stats.First), formatDate(stats.Last)

	messages, err := a.store.SenderMessages(a.ctx, addr, folders, senderRecentScan)
	if err != nil {
		return SenderOverviewDTO{}, err
	}
	if out.Recent, err = a.buildSummaries(a.ctx, onePerThread(messages, senderRecentShow)); err != nil {
		return SenderOverviewDTO{}, err
	}

	q := storage.AttachmentQuery{FolderIDs: folders, SenderAddress: addr, Limit: senderFilesShow}
	files, err := a.store.QueryAttachments(a.ctx, q)
	if err != nil {
		return SenderOverviewDTO{}, err
	}
	for _, f := range files {
		out.Files = append(out.Files, a.attachmentEntryDTO(f))
	}
	if out.FilesTotal, err = a.store.CountAttachments(a.ctx, q); err != nil {
		return SenderOverviewDTO{}, err
	}
	return out, nil
}

// onePerThread keeps the newest message of each conversation, up to limit.
// Messages not threaded yet stand on their own.
func onePerThread(messages []storage.Message, limit int) []storage.Message {
	seen := make(map[int64]bool)
	out := make([]storage.Message, 0, limit)
	for _, m := range messages {
		if len(out) == limit {
			break
		}
		if m.ThreadID != 0 {
			if seen[m.ThreadID] {
				continue
			}
			seen[m.ThreadID] = true
		}
		out = append(out, m)
	}
	return out
}
