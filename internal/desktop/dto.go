package desktop

import (
	"slices"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/crypto"
	"github.com/peltonapp/Pelton/internal/mailview"
	"github.com/peltonapp/Pelton/internal/storage"
)

// The dtos below are the flat shapes the frontend receives. They exist so the ui
// contract is stable and json friendly and does not leak internal struct details
// like bitmask flags or go time values. Wails generates typescript interfaces
// from these, which src/lib/api.ts re-exports.

// AccountDTO is non sensitive account metadata for the sidebar.
type AccountDTO struct {
	ID    int64  `json:"id"`
	Email string `json:"email"`
	// DisplayName goes out in the From header of mail sent from this account.
	// LocalLabel is what the ui calls the mailbox instead, when UseLocalLabel is
	// set, and is never sent anywhere.
	DisplayName   string `json:"displayName"`
	LocalLabel    string `json:"localLabel"`
	UseLocalLabel bool   `json:"useLocalLabel"`
	Username      string `json:"username"`
	IMAPHost      string `json:"imapHost"`
	IMAPPort      int    `json:"imapPort"`
	SMTPHost      string `json:"smtpHost"`
	SMTPPort      int    `json:"smtpPort"`
	// Local marks the Local Folders account, which holds imported mail and has
	// no server. The ui labels it and hides the server-side actions.
	Local bool `json:"local"`
	// IMAPTLS and SMTPTLS are the pinned connection security ("ssl" or
	// "starttls"), or empty when it is derived from the port.
	IMAPTLS string `json:"imapTls"`
	SMTPTLS string `json:"smtpTls"`
	// ExportOnArchive and the three fields under it are the account's
	// export-on-archive settings: write a local .eml copy of every archived
	// message into ExportDir, grouped by ExportSubfolders ("none", "year" or
	// "month") and named after ExportNameTemplate.
	ExportOnArchive    bool   `json:"exportOnArchive"`
	ExportDir          string `json:"exportDir"`
	ExportSubfolders   string `json:"exportSubfolders"`
	ExportNameTemplate string `json:"exportNameTemplate"`
	// PGPDefault is how this account starts a new message: '' unprotected,
	// 'sign', or 'auto' to sign and encrypt whenever every recipient has a key.
	PGPDefault string `json:"pgpDefault"`
	// PasswordPromptDismissed is true when the user told the missing-password
	// prompt to stop asking for this account. The ui marks the mailbox instead
	// of interrupting.
	PasswordPromptDismissed bool `json:"passwordPromptDismissed"`
	// SyncMaxParallel is the account's own parallel-sync limit, or null when
	// it follows the global setting.
	SyncMaxParallel *int `json:"syncMaxParallel"`
	// TrustedCerts are the fingerprints of certificates the user accepted for
	// this mailbox, formatted for reading. CASubjects name the certificates in
	// the CA it trusts, empty when it has none.
	TrustedCerts []string `json:"trustedCerts"`
	CASubjects   []string `json:"caSubjects"`
	// Proxy is the route the mailbox's connections take, without the password.
	Proxy AccountProxyDTO `json:"proxy"`
}

// FolderDTO is one mailbox in an account's tree. ParentID is null at the root.
// Role classifies known special folders so the ui can icon them and so unified
// views can gather them. UnreadCount/TotalCount drive the badges.
type FolderDTO struct {
	ID          int64    `json:"id"`
	AccountID   int64    `json:"accountId"`
	Name        string   `json:"name"`
	IMAPPath    string   `json:"imapPath"`
	Delimiter   string   `json:"delimiter"`
	ParentID    *int64   `json:"parentId"`
	Role        string   `json:"role"`
	UnreadCount int      `json:"unreadCount"`
	TotalCount  int      `json:"totalCount"`
	Attributes  []string `json:"attributes"`
	// Pinned means the folder is mirrored into the sidebar's Pinned group. It
	// still appears in its own account's tree either way.
	Pinned bool `json:"pinned"`
	// RoleOverride is the role the user assigned by hand, empty when the role
	// above was detected. The ui needs the two apart to show which entry is
	// ticked and whether "detect automatically" is the current state.
	RoleOverride string `json:"roleOverride"`
	// SyncExcluded means the user unchecked this folder, so sync skips it.
	// Whatever was already cached stays readable (#173).
	SyncExcluded bool `json:"syncExcluded"`
}

// UnifiedViewDTO is a cross account view (inbox/flagged/sent/drafts). The ui
// passes Key back to ListMessages to read the merged list.
type UnifiedViewDTO struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	UnreadCount int    `json:"unreadCount"`
	TotalCount  int    `json:"totalCount"`
}

// MessageSummaryDTO is one row in the message list. It carries the badge data
// (account email, folder name) so the list is self contained for unified views.
// Auth is what the receiving server reported about spf/dkim/dmarc, or
// "unavailable" when it reported nothing.
type MessageSummaryDTO struct {
	ID             int64  `json:"id"`
	AccountID      int64  `json:"accountId"`
	FolderID       int64  `json:"folderId"`
	AccountEmail   string `json:"accountEmail"`
	FolderName     string `json:"folderName"`
	Subject        string `json:"subject"`
	FromName       string `json:"fromName"`
	FromAddress    string `json:"fromAddress"`
	Snippet        string `json:"snippet"`
	Date           string `json:"date"`
	Seen           bool   `json:"seen"`
	Flagged        bool   `json:"flagged"`
	HasAttachments bool   `json:"hasAttachments"`
	PGP            string `json:"pgp"`
	Auth           string `json:"auth"`
	// FlagColor is 0 (none) or 1..8. Offline marks a user-pinned message.
	// SnoozeUntil is a stored timestamp (empty when not snoozed).
	FlagColor   int    `json:"flagColor"`
	Offline     bool   `json:"offline"`
	SnoozeUntil string `json:"snoozeUntil"`
	// SenderVIP is true when this message's from-address is on the VIP list, so
	// the ui can mark it with a star (#126).
	SenderVIP bool `json:"senderVip"`
	// SMIME is the s/mime signature verdict recorded when the message synced.
	// Empty status means unsigned, which is most mail.
	SMIME SMIMEDTO `json:"smime"`
}

// smimeDTO flattens a verified signature for the ui. It carries both PGP and
// S/MIME verdicts: they answer the same question and the reading pane shows
// them the same way, so one shape keeps that from being two code paths.
func smimeDTO(sig crypto.Signature) SMIMEDTO {
	return SMIMEDTO{
		Status: string(sig.Status),
		Signer: sig.SignerName,
		Email:  sig.SignerEmail,
		Issuer: sig.Issuer,
		Detail: sig.Detail,
	}
}

// SMIMEDTO is a message's s/mime signature verdict for display. Status is one
// of "", "valid", "untrusted" or "invalid"; Detail explains anything that is
// not valid, in a sentence written for the reader.
type SMIMEDTO struct {
	Status string `json:"status"`
	Signer string `json:"signer"`
	Email  string `json:"email"`
	Issuer string `json:"issuer"`
	Detail string `json:"detail"`
}

// MessageListDTO is a page of summaries plus the unfiltered total for paging.
type MessageListDTO struct {
	Messages []MessageSummaryDTO `json:"messages"`
	Total    int                 `json:"total"`
	// HasOlder means the server still holds messages older than anything cached
	// for this selection, so reaching Total is not the end of the mailbox and
	// the list can offer to fetch more (see bind_backfill.go).
	HasOlder bool `json:"hasOlder"`
}

// AttachmentDTO describes a stored attachment. Inline parts (referenced by the
// html body via cid) are flagged so the ui can hide them from the download list.
type AttachmentDTO struct {
	ID          int64  `json:"id"`
	Filename    string `json:"filename"`
	ContentType string `json:"contentType"`
	SizeBytes   int64  `json:"sizeBytes"`
	Inline      bool   `json:"inline"`
}

// MessageDetailDTO is the full message for the reading pane. BodyHTMLSafe is
// already sanitized with remote content blocked and cid images inlined, ready to
// drop into the sandboxed renderer. HasRemoteContent tells the ui whether to
// offer "load remote images".
type MessageDetailDTO struct {
	MessageSummaryDTO
	ToAddresses  string `json:"toAddresses"`
	CcAddresses  string `json:"ccAddresses"`
	BodyPlain    string `json:"bodyPlain"`
	BodyHTMLSafe string `json:"bodyHtmlSafe"`
	// BodyQuote is the message as plain text for a reply or forward to quote.
	// It is BodyPlain when the message has a text part, and the html rendered
	// down to text when it does not, which is the case BodyPlain is empty for
	// and replies to html-only mail used to come out blank (#239).
	BodyQuote        string `json:"bodyQuote"`
	IsHTML           bool   `json:"isHtml"`
	HasRemoteContent bool   `json:"hasRemoteContent"`
	// RemoteAllowed is true when this message's remote content was rendered
	// because the sender or domain is trusted (or the global setting is on), so
	// the ui shows no blocked-images banner.
	RemoteAllowed bool `json:"remoteAllowed"`
	// RemoteHosts lists the hosts the blocked remote content would load from, so
	// the banner can show the user where.
	RemoteHosts []string `json:"remoteHosts"`
	// TrackingPixels are the remote images that look like they exist to report
	// the open rather than to be seen (#205). They stay blocked even when the
	// rest of the remote content is loaded, unless the user turns detection off.
	TrackingPixels []TrackingPixelDTO `json:"trackingPixels"`
	Attachments    []AttachmentDTO    `json:"attachments"`
	// Phishing is what the local checks made of the message: sender spoofing,
	// authentication failures and links that do not go where they say (#206).
	// Level "none" means nothing was found and the ui shows nothing.
	Phishing PhishingDTO `json:"phishing"`
	// PGPState is empty for ordinary mail, and otherwise says what happened when
	// the message was opened: "open" (decrypted, body below is the plaintext),
	// "locked" (a passphrase is needed), "nokey" (no imported key can open it)
	// or "failed" (the pgp data could not be read). The reading pane offers a
	// different next step for each, rather than one generic error.
	PGPState string `json:"pgpState"`
	// Unsubscribe describes the unsubscribe mechanism the message advertises
	// via its List-Unsubscribe headers, nil when it has none on record.
	Unsubscribe *UnsubscribeDTO `json:"unsubscribe"`
	// CharsetGuess names what the text was read as when the message declared no
	// charset or one nothing knows, and is "detected" when the encoding was
	// picked but not reported back. Empty for mail that was right about itself,
	// which is nearly all of it, and the reader is shown nothing then.
	CharsetGuess string `json:"charsetGuess"`
	// BodyComplete is false while the row is still a list stub whose preview
	// may be shown before the full body arrives.
	BodyComplete bool `json:"bodyComplete"`
}

// TrackingPixelDTO is one remote image the scan thinks exists to report the
// open. Reasons carries the signals that led to that (tiny, hidden,
// known-host, recipient, opaque-id, lone-image) so the ui can say why rather
// than asking the reader to take its word for it. Detection is a guess and
// will sometimes be wrong.
type TrackingPixelDTO struct {
	Host    string   `json:"host"`
	URL     string   `json:"url"`
	Reasons []string `json:"reasons"`
}

// folder roles. These match storage's known folder name constants and the
// special-use imap attributes.
const (
	roleInbox   = "inbox"
	roleSent    = "sent"
	roleDrafts  = "drafts"
	roleTrash   = "trash"
	roleJunk    = "junk"
	roleArchive = "archive"
	roleNormal  = "normal"
)

// assignableRoles are the roles a user may pick for a folder by hand. Inbox is
// deliberately absent: it is the one mailbox every imap server is required to
// have under a fixed name, so there is nothing to disambiguate, and letting a
// second folder claim it would give an account two inboxes.
var assignableRoles = []string{roleNormal, roleSent, roleDrafts, roleTrash, roleJunk, roleArchive}

// validFolderRole reports whether role is one a user may assign.
func validFolderRole(role string) bool {
	return slices.Contains(assignableRoles, role)
}

// toAccountDTO flattens a storage account.
func toAccountDTO(a storage.Account) AccountDTO {
	return AccountDTO{
		ID:            a.ID,
		Email:         a.Email,
		DisplayName:   a.DisplayName,
		LocalLabel:    a.LocalLabel,
		UseLocalLabel: a.UseLocalLabel,
		Username:      a.Username,
		IMAPHost:      a.IMAPHost,
		IMAPPort:      a.IMAPPort,
		SMTPHost:      a.SMTPHost,
		SMTPPort:      a.SMTPPort,
		Local:         a.Local,
		IMAPTLS:       effectiveIMAPTLS(a),
		SMTPTLS:       effectiveSMTPTLS(a),

		ExportOnArchive:    a.ExportOnArchive,
		ExportDir:          a.ExportDir,
		ExportSubfolders:   a.ExportSubfolders,
		ExportNameTemplate: a.ExportNameTemplate,
		PGPDefault:         a.PGPDefault,

		PasswordPromptDismissed: a.PasswordPromptDismissed,
		SyncMaxParallel:         a.SyncMaxParallel,

		TrustedCerts: displayPins(a.TrustedCerts),
		CASubjects:   caSubjects(a.CAPEM),
		Proxy:        toAccountProxyDTO(a.Proxy),
	}
}

// toFolderDTO flattens a storage folder and tags its role. Counts are filled by
// the caller since they need a second query.
func toFolderDTO(f storage.Folder) FolderDTO {
	return FolderDTO{
		ID:           f.ID,
		AccountID:    f.AccountID,
		Name:         f.Name,
		IMAPPath:     f.IMAPPath,
		Delimiter:    f.Delimiter,
		ParentID:     f.ParentID,
		Role:         folderRole(f),
		Attributes:   f.Attributes,
		Pinned:       f.PinnedPosition > 0,
		RoleOverride: f.RoleOverride,
		SyncExcluded: f.SyncExcluded,
	}
}

// folderRole classifies a folder: the role the user assigned by hand wins, then
// the special-use attributes (the reliable automatic signal), then the name as
// a fallback.
//
// Detection cannot be made to work everywhere. A server that reports no
// special-use attribute and names its archive anything but "Archive" leaves the
// mail cached but invisible to the unified views, and no list of localized or
// provider-specific names would ever be complete. The manual override is the
// escape hatch that does not depend on guessing (#186).
// folderSelectable reports whether a folder can be opened on the server. A
// \Noselect (or \NonExistent) folder is a container in the hierarchy, not a
// mailbox: SELECT on one is refused, so sync has to leave it alone.
func folderSelectable(f storage.Folder) bool {
	for _, attr := range f.Attributes {
		switch strings.ToLower(strings.TrimPrefix(attr, "\\")) {
		case "noselect", "nonexistent":
			return false
		}
	}
	return true
}

func folderRole(f storage.Folder) string {
	if role := f.RoleOverride; role != "" && validFolderRole(role) {
		return role
	}
	for _, attr := range f.Attributes {
		switch strings.ToLower(strings.TrimPrefix(attr, "\\")) {
		case "inbox":
			return roleInbox
		case "sent":
			return roleSent
		case "drafts":
			return roleDrafts
		case "trash":
			return roleTrash
		case "junk":
			return roleJunk
		case "archive":
			return roleArchive
		}
	}
	switch strings.ToUpper(f.IMAPPath) {
	case storage.FolderInbox:
		return roleInbox
	case strings.ToUpper(storage.FolderSent):
		return roleSent
	case strings.ToUpper(storage.FolderDrafts):
		return roleDrafts
	case strings.ToUpper(storage.FolderTrash):
		return roleTrash
	case strings.ToUpper(storage.FolderJunk):
		return roleJunk
	case strings.ToUpper(storage.FolderArchive):
		return roleArchive
	}
	return roleNormal
}

// toSummaryDTO flattens a stored message into a list row. accountEmail and
// folderName are looked up by the caller and passed in for the badge.
func toSummaryDTO(m storage.Message, accountEmail, folderName string) MessageSummaryDTO {
	return MessageSummaryDTO{
		ID:             m.ID,
		AccountID:      m.AccountID,
		FolderID:       m.FolderID,
		AccountEmail:   accountEmail,
		FolderName:     folderName,
		Subject:        m.Subject,
		FromName:       m.FromName,
		FromAddress:    m.FromAddress,
		Snippet:        mailview.Snippet(m.BodyPlain, m.BodyHTML),
		Date:           formatDate(m.Date),
		Seen:           m.Flags.Has(storage.FlagSeen),
		Flagged:        m.Flags.Has(storage.FlagFlagged),
		HasAttachments: m.HasAttachments,
		PGP:            string(mailview.DetectPGP(m.BodyPlain, m.BodyHTML)),
		Auth:           authStatus(m.Auth),
		FlagColor:      m.FlagColor,
		Offline:        m.Offline,
		SnoozeUntil:    m.SnoozeUntil,
		SMIME: SMIMEDTO{
			Status: m.SMIME.Status,
			Signer: m.SMIME.Signer,
			Email:  m.SMIME.Email,
			Issuer: m.SMIME.Issuer,
			Detail: m.SMIME.Detail,
		},
	}
}

// formatDate renders a message date as rfc3339 for the ui, or empty for the zero
// time so the frontend can show a neutral state.
func formatDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
