package desktop

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/mailview"
	"github.com/peltonapp/Pelton/internal/storage"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// ListMessagesRequest selects the page to read. Kind is "folder", "view" or
// "savedView". FolderID applies to "folder"; View (an inbox/flagged/sent/drafts
// key) applies to "view"; ViewID (a saved-search id) applies to "savedView".
// Limit and Offset drive pagination.
type ListMessagesRequest struct {
	Kind     string `json:"kind"`
	FolderID int64  `json:"folderId"`
	View     string `json:"view"`
	ViewID   int64  `json:"viewId"`
	Limit    int    `json:"limit"`
	Offset   int    `json:"offset"`
}

// ListMessages returns a page of summaries for a folder or a unified view, plus
// the total count for pagination. This is the single read path the list uses.
func (a *App) ListMessages(req ListMessagesRequest) (MessageListDTO, error) {
	if err := a.ready(); err != nil {
		return MessageListDTO{}, err
	}

	if req.Kind == "savedView" {
		return a.listSavedView(a.ctx, req.ViewID, req.Limit, req.Offset)
	}

	q, err := a.requestQuery(a.ctx, req)
	if err != nil {
		return MessageListDTO{}, err
	}
	q.Limit = req.Limit
	q.Offset = req.Offset

	messages, err := a.store.QueryMessages(a.ctx, q)
	if err != nil {
		return MessageListDTO{}, err
	}
	total, err := a.store.CountMessages(a.ctx, q)
	if err != nil {
		return MessageListDTO{}, err
	}

	summaries, err := a.buildSummaries(a.ctx, messages)
	if err != nil {
		return MessageListDTO{}, err
	}
	hasOlder, err := a.store.AnyFolderHasOlder(a.ctx, q.FolderIDs)
	if err != nil {
		return MessageListDTO{}, err
	}
	return MessageListDTO{Messages: summaries, Total: total, HasOlder: hasOlder}, nil
}

// requestQuery turns a list request into the storage query (the folder id set
// and any flag filter), delegating unified views to viewQuery.
func (a *App) requestQuery(ctx context.Context, req ListMessagesRequest) (storage.MessageQuery, error) {
	if req.Kind == "view" {
		return a.viewQuery(ctx, req.View)
	}
	return storage.MessageQuery{FolderIDs: []int64{req.FolderID}}, nil
}

// GetMessage returns the full message for the reading pane: sanitized html with
// remote content blocked and inline cid images resolved, the plain alternative,
// and the downloadable attachment list.
func (a *App) GetMessage(id int64) (MessageDetailDTO, error) {
	if err := a.ready(); err != nil {
		return MessageDetailDTO{}, err
	}
	m, err := a.store.GetMessage(a.ctx, id)
	if err != nil {
		return MessageDetailDTO{}, err
	}
	if !m.BodyComplete {
		detail, derr := a.messageDetailFromStored(id, m)
		if derr != nil {
			return MessageDetailDTO{}, derr
		}
		account, accErr := a.store.GetAccount(a.ctx, m.AccountID)
		if accErr == nil && !account.Local {
			if _, running := a.bodyFetches.LoadOrStore(id, struct{}{}); !running {
				goSafe("on-demand body", func() {
					defer a.bodyFetches.Delete(id)
					a.fetchBodyForPane(id)
				})
			}
		}
		return detail, nil
	}
	return a.messageDetailFromStored(id, m)
}

// onDemandBodyRetryDelays are the pauses before each retry of a body fetch for
// the reading pane. What makes one fail is usually brief (a dropped connection,
// a session being re-authenticated), and every failure the reader sees is one
// they have to retry by hand.
var onDemandBodyRetryDelays = []time.Duration{time.Second, 3 * time.Second, 8 * time.Second}

// errBodyNotStored is a fetch that finished without storing the body, which is
// what a message the server no longer has looks like.
var errBodyNotStored = errors.New("pelton: fetch finished without storing the message body")

// fetchBodyForPane fetches the body of a message open in the reading pane,
// retrying after each of the retry delays, and always tells the pane how it
// ended: mail:updated once the body is stored, mail:bodyFailed once every
// attempt failed. The pane's spinner waits for one of the two.
func (a *App) fetchBodyForPane(id int64) {
	delays := a.bodyRetryDelays
	if delays == nil {
		delays = onDemandBodyRetryDelays
	}
	var err error
	for attempt := 0; ; attempt++ {
		var msg *storage.Message
		msg, err = a.store.GetMessage(a.ctx, id)
		if err != nil {
			// deleted or moved while the pane had it open: there is nothing
			// left to fetch, and the pane follows the list, not this message.
			return
		}
		if err = a.fetchMessageBodyOnDemand(msg); err == nil {
			err = a.bodyStored(id)
		}
		if err == nil {
			a.emit(EventMailUpdated, MailUpdatedEvent{MessageID: id})
			return
		}
		if attempt == len(delays) {
			break
		}
		select {
		case <-time.After(delays[attempt]):
		case <-a.ctx.Done():
			return
		}
	}
	a.log.Warn("fetch message body", "message", id, "err", err)
	a.emit(EventMailBodyFailed, MailBodyFailedEvent{MessageID: id})
}

// bodyStored reports whether a fetch left the message complete.
func (a *App) bodyStored(id int64) error {
	m, err := a.store.GetMessage(a.ctx, id)
	if err != nil {
		return err
	}
	if !m.BodyComplete {
		return errBodyNotStored
	}
	return nil
}

func (a *App) messageDetailFromStored(id int64, m *storage.Message) (MessageDetailDTO, error) {
	email, folderName := a.lookupContext(a.ctx, m.AccountID, m.FolderID)
	summary := toSummaryDTO(*m, email, folderName)
	summary.SenderVIP = a.isVIP(m.FromAddress)

	atts, err := a.store.ListAttachments(a.ctx, id)
	if err != nil {
		return MessageDetailDTO{}, err
	}

	// trusted senders/domains (or the global setting) render remote content with
	// no prompt; a per-message allow does the same for this one message; everyone
	// else is blocked until the user asks.
	autoAllow := a.remoteAutoAllow(m.FromAddress) || a.remoteMessageAllowed(m)

	detail := MessageDetailDTO{
		MessageSummaryDTO: summary,
		ToAddresses:       m.ToAddresses,
		CcAddresses:       m.CcAddresses,
		BodyPlain:         m.BodyPlain,
		BodyQuote:         quoteText(m.BodyPlain, m.BodyHTML),
		IsHTML:            m.BodyHTML != "",
		HasRemoteContent:  mailview.HasRemoteContent(m.BodyHTML),
		RemoteAllowed:     autoAllow,
		RemoteHosts:       mailview.RemoteHosts(m.BodyHTML),
		TrackingPixels:    a.trackerDTOs(m.BodyHTML),
		Attachments:       toAttachmentDTOs(atts, m.BodyHTML),
		Unsubscribe:       a.unsubscribeInfo(m),
		Phishing:          a.checkPhishing(*m),
		CharsetGuess:      m.CharsetGuess,
		BodyComplete:      m.BodyComplete,
	}
	detail.BodyHTMLSafe = a.renderHTML(m.BodyHTML, atts, autoAllow)

	// protected mail is decrypted here and only here: the plaintext replaces the
	// armor for this response and is never written back. The stored body stays
	// the ciphertext, so closing the message loses nothing that was not sent.
	if body, isHTML, state, sig := a.openProtected(*m); state != "" {
		detail.PGPState = state
		detail.SMIME = smimeDTO(sig)
		if state == pgpStateOpen {
			if isHTML {
				detail.IsHTML = true
				detail.BodyPlain = ""
				detail.BodyQuote = quoteText("", body)
				detail.BodyHTMLSafe = a.renderHTML(body, atts, autoAllow)
				detail.HasRemoteContent = mailview.HasRemoteContent(body)
				detail.RemoteHosts = mailview.RemoteHosts(body)
				detail.TrackingPixels = a.trackerDTOs(body)
			} else {
				detail.IsHTML = false
				detail.BodyPlain = body
				detail.BodyQuote = body
				detail.BodyHTMLSafe = ""
			}
		}
	}
	return detail, nil
}

// GetMessageHTML re-renders a message body with the chosen remote policy. The ui
// calls it with allowRemote=true when the user clicks "load remote images".
//
// includeTrackers additionally loads the images that look like tracking pixels,
// which the tracking-pixel setting otherwise keeps out even here. It is the
// "load them anyway" path behind each load button, for the case where the
// detection got it wrong and the reader wants the picture (#205).
func (a *App) GetMessageHTML(id int64, allowRemote, includeTrackers bool) (string, error) {
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
	if includeTrackers {
		return a.renderHTMLWithTrackers(m.BodyHTML, atts), nil
	}
	return a.renderHTML(m.BodyHTML, atts, allowRemote), nil
}

// renderHTML resolves inline cid images to data urls then sanitizes with the
// given remote policy. Inlining happens before sanitize so the cid scheme is
// already gone and only trusted data urls remain.
//
// Images that look like tracking pixels are dropped before either step while
// the block-trackers setting is on, so loading a newsletter's pictures does not
// also confirm to the sender that it was opened. That holds however remote
// content was allowed: a trusted sender gets its images shown, not a read
// receipt (#205).
func (a *App) renderHTML(html string, atts []storage.Attachment, allowRemote bool) string {
	if html == "" {
		return ""
	}
	if allowRemote && a.blockTrackers() {
		html = mailview.StripTrackers(html, mailview.ScanRemoteImages(html).TrackerURLs())
	}
	resolved := mailview.ResolveCIDs(html, a.inlineDataURLs(atts))
	return mailview.Sanitize(resolved, allowRemote, a.senderFonts())
}

// senderFonts reports whether a message may use the font families it asks for.
// On by default: that is what every other mail client does, and a font family
// is a name, not a download.
func (a *App) senderFonts() bool {
	return a.boolSetting(settingSenderFonts, true)
}

// renderHTMLWithTrackers renders with remote content on and nothing held back,
// for the reader who looked at what was detected and wants it loaded anyway.
func (a *App) renderHTMLWithTrackers(html string, atts []storage.Attachment) string {
	if html == "" {
		return ""
	}
	return mailview.Sanitize(mailview.ResolveCIDs(html, a.inlineDataURLs(atts)), true, a.senderFonts())
}

// blockTrackers reports the block-tracking-pixels preference. Off by default:
// the detection is a guess and will sometimes be wrong, so it is something the
// user turns on (or picks up from the private preset in onboarding) rather than
// something that starts quietly rewriting their mail.
func (a *App) blockTrackers() bool {
	return a.boolSetting(settingBlockTrackers, false)
}

// trackerDTOs describes the tracking pixels a body would have loaded, for the
// blocked-remote banner. Empty when detection is off, since the banner would
// otherwise name pixels the app is about to load anyway.
func (a *App) trackerDTOs(html string) []TrackingPixelDTO {
	if !a.blockTrackers() {
		return nil
	}
	scan := mailview.ScanRemoteImages(html)
	out := make([]TrackingPixelDTO, 0, len(scan.Trackers))
	for _, t := range scan.Trackers {
		reasons := make([]string, 0, len(t.Signals))
		for _, s := range t.Signals {
			reasons = append(reasons, string(s))
		}
		out = append(out, TrackingPixelDTO{Host: t.Host, URL: t.URL, Reasons: reasons})
	}
	return out
}

// inlineDataURLs builds a content-id to data-url map for inline attachments by
// reading their files off disk. Failures are skipped so one unreadable inline
// image never breaks the whole body.
func (a *App) inlineDataURLs(atts []storage.Attachment) map[string]string {
	out := make(map[string]string)
	for _, att := range atts {
		if att.ContentID == "" {
			continue
		}
		rc, err := a.store.OpenAttachment(att.DiskPath)
		if err != nil {
			continue
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			continue
		}
		id := trimAngles(att.ContentID)
		out[id] = fmt.Sprintf("data:%s;base64,%s", att.ContentType, base64.StdEncoding.EncodeToString(data))
	}
	return out
}

// SetSeen sets or clears the \Seen flag on a message and queues the change for
// the next sync to push to the server.
func (a *App) SetSeen(id int64, seen bool) error {
	return a.updateFlag(id, storage.FlagSeen, seen)
}

// SetFlagged sets or clears the \Flagged flag and queues the push.
func (a *App) SetFlagged(id int64, flagged bool) error {
	return a.updateFlag(id, storage.FlagFlagged, flagged)
}

// updateFlag toggles one flag bit on a message and marks it pending so sync
// pushes it. It reads the current mask first so other flags are preserved.
func (a *App) updateFlag(id int64, flag storage.Flag, on bool) error {
	if err := a.ready(); err != nil {
		return err
	}
	m, err := a.store.GetMessage(a.ctx, id)
	if err != nil {
		return err
	}
	flags := m.Flags
	if on {
		flags |= flag
	} else {
		flags &^= flag
	}
	if err := a.store.MarkFlagsPending(a.ctx, id, flags); err != nil {
		return err
	}
	// read/flag changes move messages in and out of unread-only and flagged-only
	// views, so refresh the view badges without waiting for the next sync.
	goSafe("counting unread mail", a.refreshViewCounts)
	return nil
}

// DeleteMessage marks a message for deletion. The row is kept and hidden from
// the list until the next sync expunges it on the server, then it is purged
// locally. This is the safe path: nothing is lost if the server rejects it.
//
// Imported mail is deleted outright instead. There is no server to confirm the
// deletion, so a pending marker would never be resolved and the message would
// sit hidden forever.
func (a *App) DeleteMessage(id int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	m, err := a.store.GetMessage(a.ctx, id)
	if err != nil {
		return err
	}
	account, err := a.store.GetAccount(a.ctx, m.AccountID)
	if err != nil {
		return err
	}
	if account.Local {
		if err := a.store.DeleteAttachmentFilesForMessage(m.AccountID, m.ID); err != nil {
			return err
		}
		if err := a.store.DeleteMessage(a.ctx, id); err != nil {
			return err
		}
		go a.refreshViewCounts()
		return nil
	}
	if err := a.store.MarkDeletePending(a.ctx, id); err != nil {
		return err
	}
	goSafe("counting unread mail", a.refreshViewCounts)
	return nil
}

// UndoDelete reverses a pending delete while the message is still cached (before
// the next sync expunges it), bringing the row back into the list.
func (a *App) UndoDelete(id int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	if err := a.store.ClearDeletePending(a.ctx, id); err != nil {
		return err
	}
	goSafe("counting unread mail", a.refreshViewCounts)
	return nil
}

// SaveAttachment prompts for a destination and writes the attachment file there,
// returning the chosen path (empty if the user cancelled). messageID scopes the
// lookup so the id cannot reach another message's files.
func (a *App) SaveAttachment(messageID, attachmentID int64) (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	atts, err := a.store.ListAttachments(a.ctx, messageID)
	if err != nil {
		return "", err
	}
	var target *storage.Attachment
	for i := range atts {
		if atts[i].ID == attachmentID {
			target = &atts[i]
			break
		}
	}
	if target == nil {
		return "", fmt.Errorf("pelton: attachment %d not found", attachmentID)
	}

	dest, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		DefaultFilename: target.Filename,
		Title:           "Save attachment",
	})
	if err != nil {
		return "", err
	}
	if dest == "" {
		return "", nil
	}
	if err := a.copyAttachment(target.DiskPath, dest); err != nil {
		return "", err
	}
	return dest, nil
}

// copyAttachment streams an attachment file from disk to dest.
func (a *App) copyAttachment(diskPath, dest string) error {
	src, err := a.store.OpenAttachment(diskPath)
	if err != nil {
		return err
	}
	defer src.Close()

	out, err := os.Create(filepath.Clean(dest))
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, src); err != nil {
		return err
	}
	return nil
}

// buildSummaries flattens stored messages into list rows, resolving each
// message's account email and folder name through small caches so a page of
// rows from many folders does not re-query the same context repeatedly.
func (a *App) buildSummaries(ctx context.Context, messages []storage.Message) ([]MessageSummaryDTO, error) {
	emailCache := make(map[int64]string)
	folderCache := make(map[int64]string)
	vips := a.vipSet()

	out := make([]MessageSummaryDTO, 0, len(messages))
	for _, m := range messages {
		email, ok := emailCache[m.AccountID]
		if !ok {
			if acc, err := a.store.GetAccount(ctx, m.AccountID); err == nil {
				email = acc.Email
			}
			emailCache[m.AccountID] = email
		}
		folderName, ok := folderCache[m.FolderID]
		if !ok {
			if f, err := a.store.GetFolder(ctx, m.FolderID); err == nil {
				folderName = f.Name
			}
			folderCache[m.FolderID] = folderName
		}
		dto := toSummaryDTO(m, email, folderName)
		dto.SenderVIP = vips[bareAddress(m.FromAddress)]
		out = append(out, dto)
	}
	return out, nil
}

// lookupContext resolves one message's account email and folder name, tolerating
// missing rows by returning empty strings.
func (a *App) lookupContext(ctx context.Context, accountID, folderID int64) (email, folderName string) {
	if acc, err := a.store.GetAccount(ctx, accountID); err == nil {
		email = acc.Email
	}
	if f, err := a.store.GetFolder(ctx, folderID); err == nil {
		folderName = f.Name
	}
	return email, folderName
}

// toAttachmentDTOs flattens stored attachments. A part is treated as inline (and
// hidden from the download list) only when it carries a content id that the body
// actually references via a cid: url. This keeps real attachments that merely
// carry a content id visible in the list.
func toAttachmentDTOs(atts []storage.Attachment, bodyHTML string) []AttachmentDTO {
	referenced := mailview.ReferencedCIDs(bodyHTML)
	out := make([]AttachmentDTO, 0, len(atts))
	for _, att := range atts {
		inline := att.ContentID != "" && referenced[trimAngles(strings.ToLower(att.ContentID))]
		out = append(out, AttachmentDTO{
			ID:          att.ID,
			Filename:    att.Filename,
			ContentType: att.ContentType,
			SizeBytes:   att.SizeBytes,
			Inline:      inline,
		})
	}
	return out
}

// trimAngles strips the surrounding <> some content ids carry.
func trimAngles(s string) string {
	s = trimPrefixByte(s, '<')
	s = trimSuffixByte(s, '>')
	return s
}

func trimPrefixByte(s string, b byte) string {
	if len(s) > 0 && s[0] == b {
		return s[1:]
	}
	return s
}

func trimSuffixByte(s string, b byte) string {
	if len(s) > 0 && s[len(s)-1] == b {
		return s[:len(s)-1]
	}
	return s
}

// quoteText is the message as text for a reply or forward to quote: the text
// part when there is one, and otherwise the html rendered down to text. An
// html-only message has no text part at all, which is why replying to one used
// to quote nothing (#239).
func quoteText(plain, html string) string {
	if strings.TrimSpace(plain) != "" {
		return plain
	}
	return mailview.TextForQuote(html)
}
