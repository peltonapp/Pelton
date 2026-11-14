package desktop

import (
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/mailview"
	"github.com/peltonapp/Pelton/internal/storage"
)

// mailTextLimit caps how much of a mail's text the calendar reads for dates.
const mailTextLimit = 20000

// MailEventSourceDTO is what the calendar reads from a mail dropped on it:
// the text its dates and times are found in, the date that text is read
// against, and what a link back to it needs.
type MailEventSourceDTO struct {
	Subject     string `json:"subject"`
	Text        string `json:"text"`
	Date        string `json:"date"`
	FromName    string `json:"fromName"`
	FromAddress string `json:"fromAddress"`
	// Link is a mid: URL (RFC 2392) naming the message by its Message-ID, or
	// empty when the message has none.
	Link string `json:"link"`
}

// MailEventSource reads a mail for turning it into an event. Nothing is stored.
func (a *App) MailEventSource(messageID int64) (MailEventSourceDTO, error) {
	if err := a.ready(); err != nil {
		return MailEventSourceDTO{}, err
	}
	m, err := a.store.GetMessage(a.ctx, messageID)
	if err != nil {
		return MailEventSourceDTO{}, err
	}
	text := m.BodyPlain
	if strings.TrimSpace(text) == "" {
		text = mailview.PlainText(m.BodyHTML)
	}
	if len(text) > mailTextLimit {
		text = text[:mailTextLimit]
	}
	out := MailEventSourceDTO{
		Subject:     stripSubjectPrefixes(m.Subject),
		Text:        text,
		FromName:    m.FromName,
		FromAddress: m.FromAddress,
		Link:        midLink(m.MessageID),
	}
	if !m.Date.IsZero() {
		out.Date = m.Date.In(time.Local).Format(time.RFC3339)
	}
	return out, nil
}

// midLink builds the mid: URL of a Message-ID.
func midLink(headerID string) string {
	id := strings.Trim(strings.TrimSpace(headerID), "<>")
	if id == "" {
		return ""
	}
	return "mid:" + url.PathEscape(id)
}

// MessageRefDTO says where a message lives, for revealing it in Mail.
type MessageRefDTO struct {
	Found     bool  `json:"found"`
	ID        int64 `json:"id"`
	AccountID int64 `json:"accountId"`
	FolderID  int64 `json:"folderId"`
}

// FindMessageByLink finds the message a mid: link names. Found is false when
// no stored message has that Message-ID.
func (a *App) FindMessageByLink(link string) (MessageRefDTO, error) {
	if err := a.ready(); err != nil {
		return MessageRefDTO{}, err
	}
	link = strings.TrimSpace(link)
	if len(link) < 5 || !strings.EqualFold(link[:4], "mid:") {
		return MessageRefDTO{}, errors.New("not a mid: link")
	}
	// a mid: URL may name a part after a slash; the message is what is wanted.
	raw, _, _ := strings.Cut(link[4:], "/")
	id, err := url.PathUnescape(raw)
	if err != nil {
		return MessageRefDTO{}, err
	}
	ref, err := a.store.FindMessageByHeaderID(a.ctx, strings.Trim(id, "<>"))
	if errors.Is(err, storage.ErrMessageNotFound) {
		return MessageRefDTO{}, nil
	}
	if err != nil {
		return MessageRefDTO{}, err
	}
	return MessageRefDTO{Found: true, ID: ref.ID, AccountID: ref.AccountID, FolderID: ref.FolderID}, nil
}
