package desktop

import (
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/storage"
)

// The newsletter manager: every sender whose mail offers List-Unsubscribe,
// with what their mail costs, and the bulk actions over it. Unsubscribing goes
// through Unsubscribe on the sender's newest message, so it keeps that flow's
// safety rules.

// newsletterRateWindow is how far back "how often they write" looks.
const newsletterRateWindow = 90 * 24 * time.Hour

// NewsletterSenderDTO is one row of the newsletter manager.
type NewsletterSenderDTO struct {
	Email string `json:"email"`
	// Name is the display name of the newest message, '' when it had none.
	Name   string `json:"name"`
	ListID string `json:"listId"`
	Count  int    `json:"count"`
	Unread int    `json:"unread"`
	Bytes  int64  `json:"bytes"`
	// FirstDate and LastDate are RFC 3339.
	FirstDate string `json:"firstDate"`
	LastDate  string `json:"lastDate"`
	// PerWeek is how many messages a week arrived over the last 90 days, or
	// since the first one when that is later.
	PerWeek float64 `json:"perWeek"`
	// LatestID is the newest message, which Unsubscribe acts on.
	LatestID int64 `json:"latestId"`
	// UnsubscribeKind is oneclick, mailto or link, as on a message.
	UnsubscribeKind string `json:"unsubscribeKind"`
	// UnsubscribedAt is when Pelton unsubscribed from this sender, '' if never.
	UnsubscribedAt string `json:"unsubscribedAt"`
}

// NewsletterSelectionDTO picks the newsletter mail of some senders for a bulk
// action. OlderThanDays above 0 keeps only mail older than that many days.
// KeepLatest leaves each sender's newest message out. ForArchive leaves out
// mail already in an archive folder.
type NewsletterSelectionDTO struct {
	Emails        []string `json:"emails"`
	OlderThanDays int      `json:"olderThanDays"`
	KeepLatest    bool     `json:"keepLatest"`
	ForArchive    bool     `json:"forArchive"`
}

// newsletterFolders are the folders the newsletter manager reads: every folder
// of the synced accounts except sent, drafts, trash, junk and server side
// collections. Imported mail is left out, since nothing can be unsubscribed
// from or moved on a server for it. withoutArchive also drops archive folders.
func (a *App) newsletterFolders(withoutArchive bool) ([]int64, error) {
	accounts, err := a.store.ListAccounts(a.ctx)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for _, acc := range accounts {
		if acc.Local {
			continue
		}
		folders, err := a.store.ListFolders(a.ctx, acc.ID)
		if err != nil {
			return nil, err
		}
		for _, f := range folders {
			role := folderRole(f)
			switch {
			case role == roleSent, role == roleDrafts, role == roleTrash, role == roleJunk:
				continue
			case withoutArchive && role == roleArchive:
				continue
			case collectionFolder(f), !folderSelectable(f):
				continue
			}
			ids = append(ids, f.ID)
		}
	}
	return ids, nil
}

// collectionFolder reports whether a folder is a server side view onto mail
// that lives elsewhere too, such as Gmail's All Mail, Starred and Important.
// Counting one would count the same message twice.
func collectionFolder(f storage.Folder) bool {
	for _, attr := range f.Attributes {
		switch strings.ToLower(strings.TrimPrefix(attr, "\\")) {
		case "all", "flagged", "important":
			return true
		}
	}
	return false
}

// ListNewsletters returns every newsletter sender of the synced accounts.
// Order is left to the caller, which sorts by what the user picked.
func (a *App) ListNewsletters() ([]NewsletterSenderDTO, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	folders, err := a.newsletterFolders(false)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	senders, err := a.store.NewsletterSenders(a.ctx, folders, now.Add(-newsletterRateWindow))
	if err != nil {
		return nil, err
	}
	done := a.unsubscribedSenders()
	out := make([]NewsletterSenderDTO, 0, len(senders))
	for _, s := range senders {
		kind, _ := parseListUnsubscribe(s.ListUnsubscribe, s.ListUnsubscribePost)
		if kind == unsubKindOneClick && !a.oneClickVerified(s.LatestID) {
			kind = unsubKindLink
		}
		out = append(out, NewsletterSenderDTO{
			Email:           s.Email,
			Name:            displayNameOf(s.LatestFromAddress, s.LatestFromName),
			ListID:          s.ListID,
			Count:           s.Count,
			Unread:          s.Unread,
			Bytes:           s.Bytes,
			FirstDate:       formatDate(s.First),
			LastDate:        formatDate(s.Last),
			PerWeek:         perWeek(s.Recent, s.First, now),
			LatestID:        s.LatestID,
			UnsubscribeKind: kind,
			UnsubscribedAt:  done[s.Email],
		})
	}
	return out, nil
}

// perWeek turns the messages of the rate window into a weekly rate. A sender
// first seen inside the window is measured from their first message, and
// anything shorter than a week counts as a week, so two mails in two days do
// not read as seven a week.
func perWeek(recent int, first, now time.Time) float64 {
	if recent == 0 {
		return 0
	}
	span := newsletterRateWindow
	if since := now.Sub(first); since < span {
		span = since
	}
	const week = 7 * 24 * time.Hour
	if span < week {
		span = week
	}
	return float64(recent) / (float64(span) / float64(week))
}

// unsubscribedSenders maps each bare address Pelton unsubscribed from to when.
// The stored keys are the raw sender as the message had it, which for synced
// mail includes the display name.
func (a *App) unsubscribedSenders() map[string]string {
	var stored map[string]string
	if err := a.store.GetJSON(a.ctx, settingUnsubscribed, &stored); err != nil {
		return nil
	}
	out := make(map[string]string, len(stored))
	for sender, at := range stored {
		email := addressOnly(sender)
		if at > out[email] {
			out[email] = at
		}
	}
	return out
}

// NewsletterMessageIDs returns the ids of the newsletter mail sel picks,
// which the frontend counts for its confirmation and then archives or
// deletes through the bulk bindings.
func (a *App) NewsletterMessageIDs(sel NewsletterSelectionDTO) ([]int64, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	folders, err := a.newsletterFolders(sel.ForArchive)
	if err != nil {
		return nil, err
	}
	q := storage.SenderMessagesQuery{Emails: sel.Emails, FolderIDs: folders, KeepLatest: sel.KeepLatest}
	if sel.OlderThanDays > 0 {
		q.Before = time.Now().Add(-time.Duration(sel.OlderThanDays) * 24 * time.Hour)
	}
	ids, err := a.store.SenderMessageIDs(a.ctx, q)
	if err != nil {
		return nil, err
	}
	if ids == nil {
		ids = []int64{}
	}
	return ids, nil
}
