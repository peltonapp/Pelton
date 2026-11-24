package desktop

import (
	"cmp"
	"net/mail"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/storage"
)

// Mailbox insights: statistics computed from the local store only. Nothing
// here reaches the network. Each binding is one section of the page, so the
// page can show a section as soon as its numbers are in.

const (
	// insightsListLimit caps every top list on the page.
	insightsListLimit = 10
	// minReplySample is the fewest replies a median is shown for. Below it the
	// page leaves the number out rather than present noise as a fact.
	minReplySample = 5
	// maxReplyGap is the longest gap still counted as a reply.
	maxReplyGap = 30 * 24 * time.Hour
)

// InsightsFolderDTO is one folder's share of the local store.
type InsightsFolderDTO struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Role  string `json:"role"`
	Count int    `json:"count"`
	Bytes int64  `json:"bytes"`
}

// InsightsAccountDTO is one account's share of the local store, with its
// folders largest first.
type InsightsAccountDTO struct {
	ID      int64               `json:"id"`
	Email   string              `json:"email"`
	Label   string              `json:"label"`
	Local   bool                `json:"local"`
	Count   int                 `json:"count"`
	Bytes   int64               `json:"bytes"`
	Folders []InsightsFolderDTO `json:"folders"`
}

// InsightsSenderDTO is one sender with their message count and size.
type InsightsSenderDTO struct {
	Email string `json:"email"`
	Name  string `json:"name"`
	Count int    `json:"count"`
	Bytes int64  `json:"bytes"`
}

// InsightsAttachmentDTO is one stored attachment and the message holding it.
type InsightsAttachmentDTO struct {
	ID          int64  `json:"id"`
	MessageID   int64  `json:"messageId"`
	Filename    string `json:"filename"`
	ContentType string `json:"contentType"`
	Bytes       int64  `json:"bytes"`
	Subject     string `json:"subject"`
	From        string `json:"from"`
	Date        string `json:"date"`
}

// InsightsStorageDTO is the storage section: where the space goes.
type InsightsStorageDTO struct {
	Accounts    []InsightsAccountDTO    `json:"accounts"`
	Senders     []InsightsSenderDTO     `json:"senders"`
	Attachments []InsightsAttachmentDTO `json:"attachments"`
}

// InsightsDayDTO is the mail of one local day, YYYY-MM-DD.
type InsightsDayDTO struct {
	Day      string `json:"day"`
	Received int    `json:"received"`
	Sent     int    `json:"sent"`
}

// InsightsHourDTO is the received mail of one local weekday (0 is Sunday) and
// hour.
type InsightsHourDTO struct {
	Weekday int `json:"weekday"`
	Hour    int `json:"hour"`
	Count   int `json:"count"`
}

// InsightsVolumeDTO is the volume section for a range. Since is the first day
// of the range, empty for all mail. HasSent is false when no account has a sent
// folder, so the page can say why nothing reads as sent.
type InsightsVolumeDTO struct {
	Since   string            `json:"since"`
	Days    []InsightsDayDTO  `json:"days"`
	Hours   []InsightsHourDTO `json:"hours"`
	HasSent bool              `json:"hasSent"`
}

// InsightsReplyDTO is one side's median reply time. Available is false when
// fewer than five replies could be measured, and the median is then 0 and not
// to be shown.
type InsightsReplyDTO struct {
	Available     bool    `json:"available"`
	Count         int     `json:"count"`
	MedianSeconds float64 `json:"medianSeconds"`
}

// InsightsPeopleDTO is the people section for a range: who the user writes to
// most, who writes to the user most, and how quickly both sides answer.
type InsightsPeopleDTO struct {
	WritesTo     []InsightsSenderDTO `json:"writesTo"`
	WritesYou    []InsightsSenderDTO `json:"writesYou"`
	MyReplies    InsightsReplyDTO    `json:"myReplies"`
	TheirReplies InsightsReplyDTO    `json:"theirReplies"`
}

// insightsFolders sorts every folder of the profile's accounts into what the
// statistics read: all of them for storage, the counted ones for volume and
// people, and the sent ones among those.
type insightsFolders struct {
	accounts []storage.Account
	byID     map[int64]storage.Folder
	all      []int64
	counted  []int64
	sent     []int64
	own      []string
}

func (a *App) loadInsightsFolders() (insightsFolders, error) {
	out := insightsFolders{byID: map[int64]storage.Folder{}}
	accounts, err := a.store.ListAccounts(a.ctx)
	if err != nil {
		return out, err
	}
	out.accounts = accounts
	for _, acc := range accounts {
		if !acc.Local && acc.Email != "" {
			out.own = append(out.own, strings.ToLower(acc.Email))
		}
		folders, err := a.store.ListFolders(a.ctx, acc.ID)
		if err != nil {
			return out, err
		}
		for _, f := range folders {
			out.byID[f.ID] = f
			out.all = append(out.all, f.ID)
			role := folderRole(f)
			if role == roleDrafts || role == roleTrash || role == roleJunk || collectionFolder(f) {
				continue
			}
			out.counted = append(out.counted, f.ID)
			if role == roleSent {
				out.sent = append(out.sent, f.ID)
			}
		}
	}
	return out, nil
}

// scope is the statistics scope for a range of days; 0 days is all mail.
func (f insightsFolders) scope(days int, now time.Time) storage.StatsScope {
	_, offset := now.Zone()
	s := storage.StatsScope{
		Counted: f.counted,
		Sent:    f.sent,
		Own:     f.own,
		Offset:  time.Duration(offset) * time.Second,
	}
	if days > 0 {
		local := now.AddDate(0, 0, -(days - 1))
		s.Since = time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, now.Location())
	}
	return s
}

// InsightsStorage returns the storage section: every account and folder by
// size, the senders whose mail takes the most space and the biggest
// attachments. Drafts, trash and junk count here, since they take space too.
func (a *App) InsightsStorage() (InsightsStorageDTO, error) {
	out := InsightsStorageDTO{Accounts: []InsightsAccountDTO{}, Senders: []InsightsSenderDTO{}, Attachments: []InsightsAttachmentDTO{}}
	if err := a.ready(); err != nil {
		return out, err
	}
	folders, err := a.loadInsightsFolders()
	if err != nil {
		return out, err
	}
	usage, err := a.store.FolderUsage(a.ctx, folders.all)
	if err != nil {
		return out, err
	}
	byAccount := map[int64]*InsightsAccountDTO{}
	for _, acc := range folders.accounts {
		dto := &InsightsAccountDTO{ID: acc.ID, Email: acc.Email, Label: acc.Label(), Local: acc.Local, Folders: []InsightsFolderDTO{}}
		byAccount[acc.ID] = dto
	}
	for _, u := range usage {
		f := folders.byID[u.FolderID]
		acc := byAccount[f.AccountID]
		if acc == nil {
			continue
		}
		acc.Count += u.Count
		acc.Bytes += u.Bytes
		acc.Folders = append(acc.Folders, InsightsFolderDTO{ID: f.ID, Name: f.Name, Role: folderRole(f), Count: u.Count, Bytes: u.Bytes})
	}
	for _, acc := range folders.accounts {
		dto := byAccount[acc.ID]
		slices.SortFunc(dto.Folders, func(x, y InsightsFolderDTO) int { return cmp.Compare(y.Bytes, x.Bytes) })
		out.Accounts = append(out.Accounts, *dto)
	}
	slices.SortStableFunc(out.Accounts, func(x, y InsightsAccountDTO) int { return cmp.Compare(y.Bytes, x.Bytes) })

	senders, err := a.store.BiggestSenders(a.ctx, folders.all, insightsListLimit)
	if err != nil {
		return out, err
	}
	out.Senders = senderDTOs(senders)

	atts, err := a.store.BiggestAttachments(a.ctx, folders.all, insightsListLimit)
	if err != nil {
		return out, err
	}
	for _, at := range atts {
		from := displayNameOf(at.FromAddress, at.FromName)
		if from == "" {
			from = addressOnly(at.FromAddress)
		}
		out.Attachments = append(out.Attachments, InsightsAttachmentDTO{
			ID: at.ID, MessageID: at.MessageID, Filename: at.Filename, ContentType: at.ContentType,
			Bytes: at.Bytes, Subject: at.Subject, From: from, Date: formatDate(at.Date),
		})
	}
	return out, nil
}

func senderDTOs(senders []storage.SenderUsage) []InsightsSenderDTO {
	out := make([]InsightsSenderDTO, 0, len(senders))
	for _, s := range senders {
		out = append(out, InsightsSenderDTO{Email: s.Email, Name: displayNameOf(s.FromAddress, s.FromName), Count: s.Count, Bytes: s.Bytes})
	}
	return out
}

// InsightsVolume returns the mail received and sent per local day over the
// last days days (0 for all mail), and the received mail per weekday and hour.
func (a *App) InsightsVolume(days int) (InsightsVolumeDTO, error) {
	out := InsightsVolumeDTO{Days: []InsightsDayDTO{}, Hours: []InsightsHourDTO{}}
	if err := a.ready(); err != nil {
		return out, err
	}
	folders, err := a.loadInsightsFolders()
	if err != nil {
		return out, err
	}
	scope := folders.scope(days, time.Now())
	if !scope.Since.IsZero() {
		out.Since = scope.Since.Format(time.DateOnly)
	}
	out.HasSent = len(folders.sent) > 0
	volume, err := a.store.DailyVolume(a.ctx, scope)
	if err != nil {
		return out, err
	}
	for _, d := range volume {
		out.Days = append(out.Days, InsightsDayDTO{Day: d.Day, Received: d.Received, Sent: d.Sent})
	}
	hours, err := a.store.ReceivedByHour(a.ctx, scope)
	if err != nil {
		return out, err
	}
	for _, h := range hours {
		out.Hours = append(out.Hours, InsightsHourDTO{Weekday: h.Weekday, Hour: h.Hour, Count: h.Count})
	}
	return out, nil
}

// InsightsPeople returns the people section over the last days days (0 for
// all mail).
func (a *App) InsightsPeople(days int) (InsightsPeopleDTO, error) {
	out := InsightsPeopleDTO{WritesTo: []InsightsSenderDTO{}, WritesYou: []InsightsSenderDTO{}}
	if err := a.ready(); err != nil {
		return out, err
	}
	folders, err := a.loadInsightsFolders()
	if err != nil {
		return out, err
	}
	scope := folders.scope(days, time.Now())

	writesYou, err := a.store.TopSenders(a.ctx, scope, insightsListLimit)
	if err != nil {
		return out, err
	}
	out.WritesYou = senderDTOs(writesYou)

	groups, err := a.store.SentRecipients(a.ctx, scope)
	if err != nil {
		return out, err
	}
	out.WritesTo = topRecipients(groups, folders.own, insightsListLimit)

	replies, err := a.store.ReplyTimes(a.ctx, scope, maxReplyGap)
	if err != nil {
		return out, err
	}
	out.MyReplies = replyDTO(replies.Mine)
	out.TheirReplies = replyDTO(replies.Theirs)
	return out, nil
}

func replyDTO(m storage.ReplyMedian) InsightsReplyDTO {
	if m.N < minReplySample {
		return InsightsReplyDTO{Count: m.N}
	}
	return InsightsReplyDTO{Available: true, Count: m.N, MedianSeconds: m.Median.Seconds()}
}

// emailPattern finds the addresses in a header that net/mail could not parse.
var emailPattern = regexp.MustCompile(`[^\s<>,;"]+@[^\s<>,;"]+`)

// recipients splits a stored To or Cc value into addresses and names. A value
// net/mail refuses still yields its addresses, without names.
func recipients(header string) []mail.Address {
	if strings.TrimSpace(header) == "" {
		return nil
	}
	if list, err := mail.ParseAddressList(header); err == nil {
		out := make([]mail.Address, 0, len(list))
		for _, addr := range list {
			out = append(out, *addr)
		}
		return out
	}
	var out []mail.Address
	for _, addr := range emailPattern.FindAllString(header, -1) {
		out = append(out, mail.Address{Address: addr})
	}
	return out
}

// topRecipients counts every address in the grouped To and Cc lists of sent
// mail, leaving out the user's own, and returns the most written to.
func topRecipients(groups []storage.RecipientGroup, own []string, limit int) []InsightsSenderDTO {
	byEmail := map[string]*InsightsSenderDTO{}
	for _, g := range groups {
		seen := map[string]bool{}
		for _, addr := range append(recipients(g.To), recipients(g.Cc)...) {
			email := strings.ToLower(strings.TrimSpace(addr.Address))
			if email == "" || seen[email] || slices.Contains(own, email) {
				continue
			}
			seen[email] = true
			entry := byEmail[email]
			if entry == nil {
				entry = &InsightsSenderDTO{Email: email}
				byEmail[email] = entry
			}
			entry.Count += g.Count
			if entry.Name == "" {
				entry.Name = strings.TrimSpace(addr.Name)
			}
		}
	}
	out := make([]InsightsSenderDTO, 0, len(byEmail))
	for _, entry := range byEmail {
		out = append(out, *entry)
	}
	slices.SortFunc(out, func(x, y InsightsSenderDTO) int {
		if c := cmp.Compare(y.Count, x.Count); c != 0 {
			return c
		}
		return strings.Compare(x.Email, y.Email)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}
