package desktop

import (
	"bytes"
	"context"
	"net/mail"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/outbox"
	"github.com/peltonapp/Pelton/internal/storage"
)

// maxThreadParticipants caps the names a conversation row carries.
const maxThreadParticipants = 4

// threadBackfillBatch is how many messages one backfill transaction threads.
// Small enough that a sync waiting for the write lock is not held up long.
const threadBackfillBatch = 200

// unsentReplyAge is how long a reply record waits for its message to go out
// before it is treated as a cancelled send. Scheduled sends can sit queued for
// a long time, so it is generous.
const unsentReplyAge = 400 * 24 * time.Hour

// ThreadSummaryDTO is one conversation row in the threaded list. Head is the
// newest message of the conversation in the listed folders, which is also what
// the list is ordered by. Count, UnreadCount, Participants, HasAttachments and
// Flagged cover every member, including the user's replies in other folders of
// the account; Trash and Junk count only when they are what is listed.
// Answered and Forwarded are the head's. MessageIDs are the members' ids in
// date order, oldest first, each message once however many folders hold it.
// ThreadID is 0 for a message the threading pass has not reached yet, which is
// then the only member.
type ThreadSummaryDTO struct {
	ThreadID       int64             `json:"threadId"`
	Head           MessageSummaryDTO `json:"head"`
	Count          int               `json:"count"`
	UnreadCount    int               `json:"unreadCount"`
	Participants   []string          `json:"participants"`
	HasAttachments bool              `json:"hasAttachments"`
	Flagged        bool              `json:"flagged"`
	Answered       bool              `json:"answered"`
	Forwarded      bool              `json:"forwarded"`
	MessageIDs     []int64           `json:"messageIds"`
}

// ThreadListDTO is a page of conversations plus the total for paging. HasOlder
// means the same as on MessageListDTO.
type ThreadListDTO struct {
	Threads  []ThreadSummaryDTO `json:"threads"`
	Total    int                `json:"total"`
	HasOlder bool               `json:"hasOlder"`
}

// ThreadMessageDTO is one message of an open conversation. FolderRole is the
// role of the folder it sits in ("inbox", "sent", "archive", "normal" and so
// on) and FromMe is true when the user sent it.
type ThreadMessageDTO struct {
	MessageSummaryDTO
	FolderRole string `json:"folderRole"`
	FromMe     bool   `json:"fromMe"`
}

// ThreadDTO is an open conversation, its messages in date order, oldest first.
type ThreadDTO struct {
	ThreadID int64              `json:"threadId"`
	Messages []ThreadMessageDTO `json:"messages"`
}

// ListThreads returns a page of conversations for a folder, a unified view or a
// saved view, taking the same request as ListMessages. Paging counts
// conversations, not messages. A conversation is listed where at least one of
// its messages is.
func (a *App) ListThreads(req ListMessagesRequest) (ThreadListDTO, error) {
	if err := a.ready(); err != nil {
		return ThreadListDTO{}, err
	}
	members, err := a.threadMemberFolders(a.ctx)
	if err != nil {
		return ThreadListDTO{}, err
	}

	if req.Kind == "savedView" {
		return a.listSavedViewThreads(a.ctx, req, members)
	}

	q, err := a.requestQuery(a.ctx, req)
	if err != nil {
		return ThreadListDTO{}, err
	}
	q.Limit = req.Limit
	q.Offset = req.Offset
	tq := storage.ThreadQuery{Query: q, MemberFolderIDs: members}

	pages, err := a.store.QueryThreads(a.ctx, tq)
	if err != nil {
		return ThreadListDTO{}, err
	}
	total, err := a.store.CountThreads(a.ctx, tq)
	if err != nil {
		return ThreadListDTO{}, err
	}
	threads, err := a.threadSummaries(a.ctx, pages)
	if err != nil {
		return ThreadListDTO{}, err
	}
	hasOlder, err := a.store.AnyFolderHasOlder(a.ctx, q.FolderIDs)
	if err != nil {
		return ThreadListDTO{}, err
	}
	return ThreadListDTO{Threads: threads, Total: total, HasOlder: hasOlder}, nil
}

func (a *App) listSavedViewThreads(ctx context.Context, req ListMessagesRequest, members []int64) (ThreadListDTO, error) {
	v, err := a.store.GetView(ctx, req.ViewID)
	if err != nil {
		return ThreadListDTO{}, err
	}
	matches, err := a.runViewMatches(ctx, *v)
	if err != nil {
		return ThreadListDTO{}, err
	}
	pages, err := a.store.GroupThreads(ctx, matches, nil, members)
	if err != nil {
		return ThreadListDTO{}, err
	}
	total := len(pages)
	offset := min(max(req.Offset, 0), total)
	end := total
	if req.Limit > 0 && offset+req.Limit < end {
		end = offset + req.Limit
	}
	threads, err := a.threadSummaries(ctx, pages[offset:end])
	if err != nil {
		return ThreadListDTO{}, err
	}
	return ThreadListDTO{Threads: threads, Total: total}, nil
}

// threadMemberFolders is every folder whose mail belongs to a conversation:
// all of them except Trash and Junk, which a conversation only shows when they
// are what is being listed.
func (a *App) threadMemberFolders(ctx context.Context) ([]int64, error) {
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

func (a *App) threadSummaries(ctx context.Context, pages []storage.ThreadPage) ([]ThreadSummaryDTO, error) {
	heads := make([]storage.Message, 0, len(pages))
	for _, p := range pages {
		heads = append(heads, p.Head)
	}
	summaries, err := a.buildSummaries(ctx, heads)
	if err != nil {
		return nil, err
	}
	out := make([]ThreadSummaryDTO, 0, len(pages))
	for i, p := range pages {
		out = append(out, threadSummary(p, summaries[i]))
	}
	return out, nil
}

// threadSummary folds a conversation's members into its list row.
func threadSummary(p storage.ThreadPage, head MessageSummaryDTO) ThreadSummaryDTO {
	dto := ThreadSummaryDTO{
		ThreadID:     p.ThreadID,
		Head:         head,
		Count:        len(p.Members),
		Participants: []string{},
		Answered:     head.Answered,
		Forwarded:    head.Forwarded,
		MessageIDs:   make([]int64, 0, len(p.Members)),
	}
	for _, m := range p.Members {
		dto.MessageIDs = append(dto.MessageIDs, m.ID)
		if !m.Flags.Has(storage.FlagSeen) {
			dto.UnreadCount++
		}
		dto.Flagged = dto.Flagged || m.Flags.Has(storage.FlagFlagged)
		dto.HasAttachments = dto.HasAttachments || m.HasAttachments
	}
	for i := len(p.Members) - 1; i >= 0 && len(dto.Participants) < maxThreadParticipants; i-- {
		name := senderName(p.Members[i].FromName, p.Members[i].FromAddress)
		if name != "" && !containsFold(dto.Participants, name) {
			dto.Participants = append(dto.Participants, name)
		}
	}
	return dto
}

// senderName is the display name of a sender, or the bare address when the
// message gave none. Synced mail keeps "Name <address>" in the address field.
func senderName(name, address string) string {
	if name = strings.TrimSpace(name); name != "" {
		return name
	}
	address = strings.TrimSpace(address)
	if lt := strings.LastIndex(address, "<"); lt > 0 {
		if n := strings.Trim(strings.TrimSpace(address[:lt]), `"`); n != "" {
			return n
		}
	}
	return bareAddress(address)
}

func containsFold(list []string, s string) bool {
	for _, v := range list {
		if strings.EqualFold(v, s) {
			return true
		}
	}
	return false
}

// GetThread returns one conversation's messages in date order, oldest first,
// for the reader: the user's own replies from the Sent folder included, each
// marked with its folder role and whether the user sent it. A message held in
// several folders is returned once, preferring the copy in the inbox, then
// Sent. Messages in Trash and Junk are left out unless nothing else is left.
// Bodies are fetched per message through GetMessage.
func (a *App) GetThread(threadID int64) (ThreadDTO, error) {
	if err := a.ready(); err != nil {
		return ThreadDTO{}, err
	}
	msgs, err := a.store.ThreadMessages(a.ctx, threadID, nil)
	if err != nil {
		return ThreadDTO{}, err
	}

	roles := map[int64]string{}
	for _, m := range msgs {
		if _, ok := roles[m.FolderID]; ok {
			continue
		}
		roles[m.FolderID] = roleNormal
		if f, err := a.store.GetFolder(a.ctx, m.FolderID); err == nil {
			roles[m.FolderID] = folderRole(*f)
		}
	}
	kept := pickThreadCopies(msgs, roles)

	own, err := a.ownAddresses(a.ctx)
	if err != nil {
		return ThreadDTO{}, err
	}
	summaries, err := a.buildSummaries(a.ctx, kept)
	if err != nil {
		return ThreadDTO{}, err
	}
	out := ThreadDTO{ThreadID: threadID, Messages: make([]ThreadMessageDTO, 0, len(kept))}
	for i, m := range kept {
		role := roles[m.FolderID]
		out.Messages = append(out.Messages, ThreadMessageDTO{
			MessageSummaryDTO: summaries[i],
			FolderRole:        role,
			FromMe:            role == roleSent || own[bareAddress(m.FromAddress)],
		})
	}
	return out, nil
}

// copyRank orders the folders a duplicate message may be shown from.
var copyRank = map[string]int{roleInbox: 5, roleSent: 4, roleDrafts: 3, roleNormal: 2, roleArchive: 1}

// pickThreadCopies drops Trash and Junk members unless they are all there is,
// and keeps one copy of each Message-ID, keeping date order.
func pickThreadCopies(msgs []storage.Message, roles map[int64]string) []storage.Message {
	binned := func(m storage.Message) bool {
		r := roles[m.FolderID]
		return r == roleTrash || r == roleJunk
	}
	allBinned := true
	for _, m := range msgs {
		if !binned(m) {
			allBinned = false
			break
		}
	}
	out := make([]storage.Message, 0, len(msgs))
	seen := map[string]int{}
	for _, m := range msgs {
		if binned(m) && !allBinned {
			continue
		}
		if m.MessageID == "" {
			out = append(out, m)
			continue
		}
		if i, ok := seen[m.MessageID]; ok {
			if copyRank[roles[m.FolderID]] > copyRank[roles[out[i].FolderID]] {
				out[i] = m
			}
			continue
		}
		seen[m.MessageID] = len(out)
		out = append(out, m)
	}
	return out
}

// ownAddresses is the set of the user's account addresses, lower-cased.
func (a *App) ownAddresses(ctx context.Context) (map[string]bool, error) {
	accounts, err := a.store.ListAccounts(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(accounts))
	for _, acc := range accounts {
		out[bareAddress(acc.Email)] = true
	}
	return out, nil
}

// recordReplySources notes what an outgoing message answers and forwards, so
// those messages can be marked once it is sent. The record goes under the
// account that holds the original, which need not be the sending one.
func (a *App) recordReplySources(req ComposeRequest, outgoingID string) error {
	if req.InReplyTo != "" {
		account, ok, err := a.store.MessageAccount(a.ctx, req.InReplyTo, req.AccountID)
		if err != nil {
			return err
		}
		if !ok {
			account = req.AccountID
		}
		if err := a.store.RecordOutgoingReply(a.ctx, account, outgoingID, req.InReplyTo, storage.ReplyKindReply); err != nil {
			return err
		}
	}
	if req.ForwardOf != 0 {
		m, err := a.store.GetMessage(a.ctx, req.ForwardOf)
		if err != nil {
			return err
		}
		if err := a.store.RecordOutgoingReply(a.ctx, m.AccountID, outgoingID, m.MessageID, storage.ReplyKindForward); err != nil {
			return err
		}
	}
	return nil
}

// afterSent marks what a message the server just accepted answered or
// forwarded, and tells open lists to reload. It runs on the outbox worker's
// goroutine and never fails the send.
func (a *App) afterSent(ctx context.Context, m outbox.Message) {
	parsed, err := mail.ReadMessage(bytes.NewReader(m.Raw))
	if err != nil {
		a.log.Warn("read sent message headers", "id", m.ID, "err", err)
		return
	}
	changed, err := a.store.MarkReplySent(ctx, parsed.Header.Get("Message-ID"))
	if err != nil {
		a.log.Error("mark replied messages", "id", m.ID, "err", err)
		return
	}
	a.announceReplyState(changed)
}

// announceReplyState emits the list reload event for rows whose answered or
// forwarded state changed outside a sync.
func (a *App) announceReplyState(changed []storage.ReplyStateChange) {
	if len(changed) == 0 {
		return
	}
	ev := MailRepairedEvent{AccountID: changed[0].AccountID, FolderID: changed[0].FolderID, Count: len(changed)}
	for _, c := range changed[1:] {
		if c.AccountID != ev.AccountID {
			ev.AccountID = 0
		}
		if c.FolderID != ev.FolderID {
			ev.FolderID = 0
		}
	}
	a.emit(EventMailRepaired, ev)
}

// backfillThreads threads the mail cached before threading existed, a batch at
// a time, and clears out reply records of sends that never happened. It picks
// up where an interrupted run left off and does nothing once every message has
// a thread. Open lists reload once at the end.
func (a *App) backfillThreads() {
	if n, err := a.store.PruneUnsentReplies(a.ctx, unsentReplyAge); err != nil {
		a.log.Error("prune unsent reply records", "err", err)
	} else if n > 0 {
		a.log.Info("pruned unsent reply records", "count", n)
	}

	total := 0
	for a.ctx.Err() == nil {
		n, err := a.store.BackfillThreads(a.ctx, threadBackfillBatch)
		if err != nil {
			a.log.Error("thread cached mail", "err", err)
			break
		}
		if n == 0 {
			break
		}
		total += n
	}
	if total > 0 {
		a.log.Info("threaded cached mail", "count", total)
		a.emit(EventMailRepaired, MailRepairedEvent{Count: total})
	}
}
