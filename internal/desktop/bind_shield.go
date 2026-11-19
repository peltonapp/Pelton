package desktop

import (
	"slices"
	"sync"
	"time"

	"github.com/peltonapp/Pelton/internal/mailview"
	"github.com/peltonapp/Pelton/internal/storage"
)

// The privacy shield: what a message tried to learn about its reader and what
// Pelton did about it. Computed when a message is opened and in a bounded pass
// over newly synced mail; nothing is fetched to find out.

// EventPrivacyChanged fires when the privacy counters or an unsubscribe's
// state changed, so an open report can reload.
const EventPrivacyChanged = "privacy:changed"

// Shield item statuses: what happened to each thing the message asked for.
const (
	// shieldBlocked: kept from loading.
	shieldBlocked = "blocked"
	// shieldLoaded: allowed to load, by the reader's choice.
	shieldLoaded = "loaded"
	// shieldRemoved: taken out of every message, whatever is allowed.
	shieldRemoved = "removed"
	// shieldClick: a link, which reports nothing unless it is clicked.
	shieldClick = "click"
	// shieldNotSent: a read receipt request, which Pelton never answers.
	shieldNotSent = "not_sent"
)

const (
	// privacyPassCap bounds the messages one after-sync pass reads.
	privacyPassCap = 100
	// defaultUnsubscribeGrace is how long after an unsubscribe mail from the
	// same list can still arrive before it counts as still sending: senders
	// are given up to ten days to act on it.
	defaultUnsubscribeGrace = 10
)

// ShieldItemDTO is one thing a message tried to load or learn.
type ShieldItemDTO struct {
	// Kind is "pixel", "image", "link", "receipt", "font", "stylesheet",
	// "form", "frame", "media" or "background".
	Kind    string   `json:"kind"`
	Host    string   `json:"host"`
	Company string   `json:"company"`
	URL     string   `json:"url"`
	Reasons []string `json:"reasons"`
	// Destination is where a tracked link ends up, when the url says.
	Destination string `json:"destination"`
	Status      string `json:"status"`
}

// ShieldUnsubDTO is the unsubscribe state of the message's sender.
type ShieldUnsubDTO struct {
	At string `json:"at"`
	// StillSending is set while mail keeps arriving past the grace period and
	// the user has not dismissed it.
	StillSending bool   `json:"stillSending"`
	StillCount   int    `json:"stillCount"`
	Blocked      string `json:"blocked"`
	Sender       string `json:"sender"`
}

// ShieldDTO is the shield of one message.
type ShieldDTO struct {
	MessageID int64 `json:"messageId"`
	// RemoteAllowed: remote content loads for this message without asking.
	RemoteAllowed bool `json:"remoteAllowed"`
	// HoldsTrackers: the block-tracking-pixels setting keeps pixels out even
	// when images load.
	HoldsTrackers bool `json:"holdsTrackers"`
	// Trackers is how many tracking requests were kept from the sender: the
	// pixels blocked and an unanswered read receipt request.
	Trackers int             `json:"trackers"`
	Links    int             `json:"links"`
	Items    []ShieldItemDTO `json:"items"`
	Unsub    *ShieldUnsubDTO `json:"unsub"`
}

// listedMessage reads a message together with its List-Id, which the
// unsubscribe watch matches on and a plain message read leaves out.
func (a *App) listedMessage(id int64) (*storage.Message, error) {
	m, err := a.store.GetMessage(a.ctx, id)
	if err != nil {
		return nil, err
	}
	if m.ListID, err = a.store.MessageListID(a.ctx, id); err != nil {
		return nil, err
	}
	return m, nil
}

// GetPrivacyShield returns what one message tried to learn and what was done
// about it, and counts it for the report when counting is on.
func (a *App) GetPrivacyShield(id int64) (ShieldDTO, error) {
	if err := a.ready(); err != nil {
		return ShieldDTO{}, err
	}
	m, err := a.listedMessage(id)
	if err != nil {
		return ShieldDTO{}, err
	}
	trust, err := a.store.GetMessageTrust(a.ctx, id)
	if err != nil {
		return ShieldDTO{}, err
	}
	remote := a.remoteAutoAllow(m.FromAddress) || a.remoteMessageAllowed(m)
	dto := buildShield(*m, trust, remote, a.blockTrackers())
	if a.privacyCounting() {
		a.countPrivacy(*m, dto)
	}
	if u, ok, err := a.store.UnsubscribeFor(a.ctx, bareAddress(m.FromAddress), m.ListID); err == nil && ok {
		dto.Unsub = unsubState(u)
	}
	return dto, nil
}

// buildShield works out the shield of a message from its body and record.
func buildShield(m storage.Message, trust storage.MessageTrust, remote, holdTrackers bool) ShieldDTO {
	dto := ShieldDTO{MessageID: m.ID, RemoteAllowed: remote, HoldsTrackers: holdTrackers, Items: []ShieldItemDTO{}}
	imageStatus := shieldBlocked
	if remote {
		imageStatus = shieldLoaded
	}
	pixelStatus := imageStatus
	if holdTrackers {
		pixelStatus = shieldBlocked
	}

	scan := mailview.ScanRemoteImages(m.BodyHTML)
	trackers := scan.TrackerURLs()
	for _, img := range scan.Images {
		item := ShieldItemDTO{Kind: "image", Host: img.Host, Company: mailview.Company(img.Host), URL: img.URL, Status: imageStatus, Reasons: []string{}}
		if trackers[img.URL] {
			item.Kind, item.Status = "pixel", pixelStatus
			for _, s := range img.Signals {
				item.Reasons = append(item.Reasons, string(s))
			}
			if pixelStatus == shieldBlocked {
				dto.Trackers++
			}
		}
		dto.Items = append(dto.Items, item)
	}

	links := mailview.ScanLinks(m.BodyHTML)
	if m.BodyHTML == "" {
		links = mailview.ScanPlainLinks(m.BodyPlain)
	}
	for _, l := range links {
		dto.Items = append(dto.Items, ShieldItemDTO{Kind: "link", Host: l.Host, Company: mailview.Company(l.Host), URL: l.URL,
			Reasons: l.Reasons, Destination: l.Destination, Status: shieldClick})
		dto.Links++
	}

	if trust.ReadReceiptTo != "" {
		dto.Items = append(dto.Items, ShieldItemDTO{Kind: "receipt", Host: domainPart(trust.ReadReceiptTo), URL: trust.ReadReceiptTo,
			Reasons: []string{}, Status: shieldNotSent})
		dto.Trackers++
	}

	for _, r := range mailview.ScanExternal(m.BodyHTML) {
		status := shieldRemoved
		if r.Kind == "background" || r.Kind == "media" {
			status = imageStatus
		}
		dto.Items = append(dto.Items, ShieldItemDTO{Kind: r.Kind, Host: r.Host, Company: mailview.Company(r.Host), Reasons: []string{}, Status: status})
	}
	return dto
}

// unsubState turns a stored unsubscribe into what the reader shows.
func unsubState(u storage.Unsubscribe) *ShieldUnsubDTO {
	return &ShieldUnsubDTO{
		At:           formatDate(u.At),
		StillSending: u.StillCount > 0 && u.StillAt.After(u.DismissedAt),
		StillCount:   u.StillCount,
		Blocked:      u.Blocked,
		Sender:       u.Sender,
	}
}

// privacyCounting reports the keep-counters setting.
func (a *App) privacyCounting() bool {
	return a.boolSetting(settingPrivacyCounters, true)
}

// unsubscribeGraceDays is how many days mail may keep coming after an
// unsubscribe.
func (a *App) unsubscribeGraceDays() int {
	days := a.intSetting(settingUnsubscribeGrace, defaultUnsubscribeGrace)
	if days < 1 {
		return defaultUnsubscribeGrace
	}
	return days
}

// countPrivacy stores a message's counts for the report, once.
func (a *App) countPrivacy(m storage.Message, dto ShieldDTO) {
	scan := storage.PrivacyScan{
		Key: storage.PrivacyKey(m), Sender: bareAddress(m.FromAddress), SenderName: displayNameOf(m.FromAddress, m.FromName),
		ListID: m.ListID, Date: m.Date,
	}
	for _, it := range dto.Items {
		switch it.Kind {
		case "pixel":
			scan.Pixels++
		case "image":
			scan.RemoteImages++
		case "link":
			scan.Links++
		case "receipt":
			scan.Receipt = true
		default:
			scan.Other++
		}
		if it.Company != "" && it.Kind != "image" && !slices.Contains(scan.Companies, it.Company) {
			scan.Companies = append(scan.Companies, it.Company)
		}
	}
	if err := a.store.RecordPrivacyScan(a.ctx, scan); err != nil {
		a.log.Error("privacy: count a message", "message", m.ID, "err", err)
	}
}

// privacyMu keeps the after-sync passes of several folders from interleaving.
var privacyMu sync.Mutex

// privacyAfterSync reads newly synced mail in the background: it counts what
// the mail tried to learn, notices mail from senders the user unsubscribed
// from, and moves mail from senders the user blocked.
func (a *App) privacyAfterSync(f storage.Folder, ids []int64) {
	switch folderRole(f) {
	case roleSent, roleDrafts, roleTrash:
		return
	}
	if len(ids) == 0 {
		return
	}
	goSafe("reading new mail for the privacy report", func() {
		privacyMu.Lock()
		defer privacyMu.Unlock()
		a.privacyPass(ids)
	})
}

// privacyPass is the body of privacyAfterSync, run in the foreground by tests.
func (a *App) privacyPass(ids []int64) {
	if len(ids) > privacyPassCap {
		ids = ids[len(ids)-privacyPassCap:]
	}
	counting := a.privacyCounting()
	grace := time.Duration(a.unsubscribeGraceDays()) * 24 * time.Hour
	changed := false
	for _, id := range ids {
		if a.ctx.Err() != nil {
			return
		}
		m, err := a.listedMessage(id)
		if err != nil {
			continue
		}
		sender := bareAddress(m.FromAddress)
		if u, ok, err := a.store.UnsubscribeFor(a.ctx, sender, m.ListID); err == nil && ok {
			if m.Date.After(u.At.Add(grace)) {
				if err := a.store.NoteStillSending(a.ctx, u.Sender, m.Date, m.ID); err == nil {
					changed = true
				}
			}
			if u.Blocked != "" && m.Date.After(u.At) {
				a.moveBlocked(m, u.Blocked)
			}
		}
		if !counting {
			continue
		}
		if done, err := a.store.PrivacyScanned(a.ctx, []string{storage.PrivacyKey(*m)}); err != nil || done[storage.PrivacyKey(*m)] {
			continue
		}
		trust, err := a.store.GetMessageTrust(a.ctx, id)
		if err != nil {
			continue
		}
		a.countPrivacy(*m, buildShield(*m, trust, a.remoteAutoAllow(m.FromAddress) || a.remoteMessageAllowed(m), a.blockTrackers()))
		changed = true
	}
	if changed {
		a.emit(EventPrivacyChanged, nil)
	}
}

// moveBlocked sends a message from a blocked sender to junk or trash through
// the same paths the reader's own buttons use.
func (a *App) moveBlocked(m *storage.Message, where string) {
	if folder, err := a.store.GetFolder(a.ctx, m.FolderID); err == nil {
		if role := folderRole(*folder); role == roleJunk || role == roleTrash {
			return
		}
	}
	var err error
	switch where {
	case "junk":
		_, err = a.MarkSpam(m.ID)
	case "trash":
		err = a.DeleteMessage(m.ID)
	}
	if err != nil {
		a.log.Warn("privacy: move mail from a blocked sender", "message", m.ID, "err", err)
	}
}

// PrivacySenderDTO is one sender's line of the privacy report.
type PrivacySenderDTO struct {
	Address   string   `json:"address"`
	Name      string   `json:"name"`
	Mails     int      `json:"mails"`
	Pixels    int      `json:"pixels"`
	Links     int      `json:"links"`
	Receipts  int      `json:"receipts"`
	Other     int      `json:"other"`
	First     string   `json:"first"`
	Last      string   `json:"last"`
	Companies []string `json:"companies"`
	// Unsub is the unsubscribe state, nil when Pelton never unsubscribed.
	Unsub *ShieldUnsubDTO `json:"unsub"`
}

// PrivacyReportDTO is the privacy report for a period.
type PrivacyReportDTO struct {
	// Counting reports the keep-counters setting; with it off the report
	// shows what was counted before.
	Counting bool `json:"counting"`
	// Since is the start of the period, '' for all time.
	Since string `json:"since"`
	// Totals over the period: tracking requests (pixels and read receipt
	// requests), tracked links, mails, and senders that tried to track.
	Trackers int                `json:"trackers"`
	Links    int                `json:"links"`
	Mails    int                `json:"mails"`
	Senders  int                `json:"senders"`
	Rows     []PrivacySenderDTO `json:"rows"`
}

// PrivacyReport lists senders by how much they tried to track over the last
// days days; 0 means all the history there is. Senders whose mail never
// tracked anything and who were never unsubscribed from are left out.
func (a *App) PrivacyReport(days int) (PrivacyReportDTO, error) {
	if err := a.ready(); err != nil {
		return PrivacyReportDTO{}, err
	}
	var since time.Time
	if days > 0 {
		since = time.Now().AddDate(0, 0, -days)
	}
	rows, err := a.store.PrivacySenders(a.ctx, since)
	if err != nil {
		return PrivacyReportDTO{}, err
	}
	unsubs, err := a.store.ListUnsubscribes(a.ctx)
	if err != nil {
		return PrivacyReportDTO{}, err
	}
	bySender := map[string]storage.Unsubscribe{}
	for _, u := range unsubs {
		bySender[u.Sender] = u
	}
	out := PrivacyReportDTO{Counting: a.privacyCounting(), Since: formatDate(since), Rows: []PrivacySenderDTO{}}
	for _, r := range rows {
		out.Mails += r.Mails
		u, unsubscribed := bySender[r.Sender]
		tracked := r.Pixels+r.Links+r.Receipts > 0
		if !tracked && !unsubscribed {
			continue
		}
		if tracked {
			out.Senders++
		}
		out.Trackers += r.Pixels + r.Receipts
		out.Links += r.Links
		row := PrivacySenderDTO{
			Address: r.Sender, Name: r.Name, Mails: r.Mails, Pixels: r.Pixels, Links: r.Links, Receipts: r.Receipts,
			Other: r.Other, First: formatDate(r.First), Last: formatDate(r.Last), Companies: nonNil(r.Companies),
		}
		if unsubscribed {
			row.Unsub = unsubState(u)
			delete(bySender, r.Sender)
		}
		out.Rows = append(out.Rows, row)
	}
	for _, u := range unsubs {
		if _, left := bySender[u.Sender]; left {
			out.Rows = append(out.Rows, PrivacySenderDTO{Address: u.Sender, Companies: []string{}, Unsub: unsubState(u)})
		}
	}
	return out, nil
}

// ClearPrivacyHistory forgets every counter. Unsubscribe records stay: they
// are what still-sending detection works from.
func (a *App) ClearPrivacyHistory() error {
	if err := a.ready(); err != nil {
		return err
	}
	if err := a.store.ClearPrivacyScans(a.ctx); err != nil {
		return err
	}
	a.emit(EventPrivacyChanged, nil)
	return nil
}

// DismissStillSending stops telling the user a sender is still sending,
// until more mail arrives from them.
func (a *App) DismissStillSending(sender string) error {
	if err := a.ready(); err != nil {
		return err
	}
	if err := a.store.DismissStillSending(a.ctx, bareAddress(sender)); err != nil {
		return err
	}
	a.emit(EventPrivacyChanged, nil)
	return nil
}

// BlockUnsubscribedSender moves new mail from a sender the user unsubscribed
// from to junk ("junk") or trash ("trash"), or stops ("") doing so. Mail
// already in the mailbox is left where it is.
func (a *App) BlockUnsubscribedSender(sender, where string) error {
	if err := a.ready(); err != nil {
		return err
	}
	if err := a.store.SetUnsubscribeBlock(a.ctx, bareAddress(sender), where); err != nil {
		return err
	}
	a.emit(EventPrivacyChanged, nil)
	return nil
}

// GetMessageHTMLBlockingTrackers renders a message with its remote images
// loaded and everything that looks like a tracking pixel left out, whatever
// the block-tracking-pixels setting says: the shield's "load images, keep
// trackers blocked".
func (a *App) GetMessageHTMLBlockingTrackers(id int64) (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	m, err := a.store.GetMessage(a.ctx, id)
	if err != nil {
		return "", err
	}
	atts, err := a.store.ListAttachments(a.ctx, id)
	if err != nil {
		return "", err
	}
	html := mailview.StripTrackers(m.BodyHTML, mailview.ScanRemoteImages(m.BodyHTML).TrackerURLs())
	if html == "" {
		return "", nil
	}
	return mailview.Sanitize(mailview.ResolveCIDs(html, a.inlineDataURLs(atts)), true, a.senderFonts()), nil
}
