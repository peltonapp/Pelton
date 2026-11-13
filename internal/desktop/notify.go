package desktop

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/peltonapp/Pelton/internal/storage"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

// Native OS notifications for new mail (#126), through the Wails notification
// service on every platform: UserNotifications on macOS, so the alert carries
// the app's own icon (#143); a toast on Windows, under Pelton's name and the
// exe's icon (#170); and a dbus notification on Linux. A click on one opens the
// message it was about, wherever the platform reports clicks back.
// Notifications only fire for INBOX so sent/archived mail moved by a sync never
// notifies.

// notification is one OS notification waiting to be delivered. The message id
// travels with it so a click can open that message.
type notification struct {
	title     string
	body      string
	messageID int64
}

// notifyDataMessageID is the key of the message id in a notification's data.
// The OS hands the data back on a click.
const notifyDataMessageID = "messageId"

// notifySeq makes notification ids unique within a run.
var notifySeq atomic.Int64

// notifyMessageID reads a message id back out of a clicked notification's
// data. ok is false for anything else, including a summary notification, which
// names no message. The data has been through JSON on some platforms, so the
// id can come back as any of the shapes a number takes there.
func notifyMessageID(data map[string]any) (int64, bool) {
	var id int64
	switch v := data[notifyDataMessageID].(type) {
	case int64:
		id = v
	case int:
		id = int64(v)
	case float64:
		id = int64(v)
	case json.Number:
		id, _ = v.Int64()
	case string:
		id, _ = strconv.ParseInt(v, 10, 64)
	}
	return id, id > 0
}

// deliverNotification posts one notification. Without the service (tests) it
// does nothing. On macOS the first one asks for permission; after that the
// stored answer is used without asking again. The sound is off on purpose:
// what Pelton should make a noise about is its own question (#240).
func (a *App) deliverNotification(n notification) error {
	if a.notifier == nil {
		return nil
	}
	if ok, err := a.notifier.RequestNotificationAuthorization(); err != nil || !ok {
		return fmt.Errorf("pelton: notifications not allowed: %v", err)
	}
	data := map[string]any{}
	if n.messageID > 0 {
		data[notifyDataMessageID] = n.messageID
	}
	return a.notifier.SendNotification(notifications.NotificationOptions{
		ID:    "pelton-" + strconv.FormatInt(notifySeq.Add(1), 10),
		Title: n.title,
		Body:  n.body,
		Data:  data,
		Sound: &notifications.NotificationSound{Silent: true},
	})
}

// onNotificationResponse runs when a notification is clicked. A click on one
// about a message opens that message; any other click still asked for Pelton,
// so it brings the window up.
func (a *App) onNotificationResponse(result notifications.NotificationResult) {
	if result.Error != nil {
		a.log.Warn("notification response", "err", result.Error)
		return
	}
	if id, ok := notifyMessageID(result.Response.UserInfo); ok {
		a.openFromNotification(id)
		return
	}
	a.showWindow()
}

// notifyStrings holds the localizable text of a new-mail notification. Like the
// native menu (see menu_i18n.go), notifications are built in the Go process, so
// their text lives in a Go-side table keyed by the language setting rather than
// the frontend's locale files.
type notifyStrings struct {
	newMail     string // title for a plain new-mail notification
	vipNewMail  string // title when the sender is a VIP
	noSubject   string // shown in place of an empty subject
	fromUnknown string // shown in place of an empty sender
	summary     string // body of the one notification standing in for a burst, with %d for the count
}

var notifyLocales = map[string]notifyStrings{
	"en":    {newMail: "New mail", vipNewMail: "New mail from a VIP", noSubject: "(no subject)", fromUnknown: "Unknown sender", summary: "%d new messages"},
	"de":    {newMail: "Neue Nachricht", vipNewMail: "Neue Nachricht von einem VIP", noSubject: "(kein Betreff)", fromUnknown: "Unbekannter Absender", summary: "%d neue Nachrichten"},
	"fr":    {newMail: "Nouveau message", vipNewMail: "Nouveau message d'un VIP", noSubject: "(sans objet)", fromUnknown: "Expéditeur inconnu", summary: "%d nouveaux messages"},
	"nl":    {newMail: "Nieuw bericht", vipNewMail: "Nieuw bericht van een VIP", noSubject: "(geen onderwerp)", fromUnknown: "Onbekende afzender", summary: "%d nieuwe berichten"},
	"es":    {newMail: "Nuevo mensaje", vipNewMail: "Nuevo mensaje de un VIP", noSubject: "(sin asunto)", fromUnknown: "Remitente desconocido", summary: "%d mensajes nuevos"},
	"it":    {newMail: "Nuova email", vipNewMail: "Nuova email da un VIP", noSubject: "(nessun oggetto)", fromUnknown: "Mittente sconosciuto", summary: "%d nuove email"},
	"pl":    {newMail: "Nowa wiadomość", vipNewMail: "Nowa wiadomość od VIP-a", noSubject: "(bez tematu)", fromUnknown: "Nieznany nadawca", summary: "Nowe wiadomości: %d"},
	"ru":    {newMail: "Новая почта", vipNewMail: "Новая почта от VIP-отправителя", noSubject: "(без темы)", fromUnknown: "Неизвестный отправитель", summary: "Новых писем: %d"},
	"tr":    {newMail: "Yeni posta", vipNewMail: "Bir VIP'ten yeni posta", noSubject: "(konu yok)", fromUnknown: "Bilinmeyen gönderici", summary: "%d yeni posta"},
	"pt":    {newMail: "Correio novo", vipNewMail: "Correio novo de um VIP", noSubject: "(sem assunto)", fromUnknown: "Remetente desconhecido", summary: "%d mensagens novas"},
	"ar":    {newMail: "بريد جديد", vipNewMail: "بريد جديد من جهة مهمة", noSubject: "(بلا موضوع)", fromUnknown: "مرسل غير معروف", summary: "رسائل جديدة: %d"},
	"zh-CN": {newMail: "新邮件", vipNewMail: "来自 VIP 的新邮件", noSubject: "（无主题）", fromUnknown: "未知发件人", summary: "%d 封新邮件"},
}

// notifyStringsFor returns the notification text table for lang, falling back to
// English for an unknown or empty code.
func notifyStringsFor(lang string) notifyStrings {
	if s, ok := notifyLocales[lang]; ok {
		return s
	}
	return notifyLocales["en"]
}

// notifyNewMail raises OS notifications for messages freshly pulled into a
// folder. It runs on its own goroutine off the sync path. General new-mail
// notifications only fire when the setting is on; VIP-sender notifications fire
// regardless, so important senders cut through a quiet inbox. Only INBOX is
// considered so mail landing in other folders during a full sync stays quiet.
func (a *App) notifyNewMail(folder storage.Folder, ids []int64) {
	if len(ids) == 0 || !strings.EqualFold(folder.IMAPPath, "INBOX") {
		return
	}
	general := a.boolSetting(settingNotifyNewMail, false)
	vips := a.vipSet()
	if !general && len(vips) == 0 {
		return
	}

	s := notifyStringsFor(a.stringSetting(settingLanguage, "en"))
	var pending []notification
	for _, id := range ids {
		if a.ctx.Err() != nil {
			return
		}
		m, err := a.store.GetMessage(a.ctx, id)
		if err != nil {
			continue
		}
		// never notify for mail the user has already seen (e.g. read elsewhere and
		// pulled in on a first sync).
		if m.Flags.Has(storage.FlagSeen) {
			continue
		}
		vip := vips[bareAddress(m.FromAddress)]
		if !vip && !general {
			continue
		}

		title := s.newMail
		if vip {
			title = s.vipNewMail
		}
		pending = append(pending, notification{title: title, body: notifyBody(m, s), messageID: m.ID})
	}
	for _, n := range collapseNotifications(pending, s) {
		a.sendNotification(n)
	}
}

// notifyBurstLimit is the most notifications one sync of a folder raises one
// by one. Past it, a single summary stands in for all of them, so opening
// Pelton after a while away does not bring a flood of alerts.
const notifyBurstLimit = 3

// collapseNotifications returns list as is, or one summary notification when
// it holds more than notifyBurstLimit. The summary names no message, so a
// click on it only shows the window.
func collapseNotifications(list []notification, s notifyStrings) []notification {
	if len(list) <= notifyBurstLimit {
		return list
	}
	return []notification{{title: s.newMail, body: fmt.Sprintf(s.summary, len(list))}}
}

// notifyBody builds the notification body: sender then subject, each falling
// back to a placeholder so the notification is never blank.
func notifyBody(m *storage.Message, s notifyStrings) string {
	from := strings.TrimSpace(m.FromName)
	if from == "" {
		from = bareAddress(m.FromAddress)
	}
	if from == "" {
		from = s.fromUnknown
	}
	subject := strings.TrimSpace(m.Subject)
	if subject == "" {
		subject = s.noSubject
	}
	return from + "\n" + subject
}

// sendNotification raises one native OS notification, logging (not failing) on
// error so a notification backend that is missing or busy never disrupts sync.
// The delivery itself is per platform: see notify_darwin.go, notify_windows.go
// and notify_other.go.
func (a *App) sendNotification(n notification) {
	if err := a.deliverNotification(n); err != nil {
		a.log.Warn("notify", "err", err)
	}
}

// openFromNotification brings the window up and opens the message a clicked
// notification was about. Only Windows can report a click back today; the other
// two backends have no per-notification callback at all.
//
// The message is looked up rather than trusted: by the time a toast is clicked
// it may have been deleted, moved, or left behind by a profile switch. Any of
// those just show the window, which is what the click asked for first.
func (a *App) openFromNotification(messageID int64) {
	a.showWindow()
	if a.store == nil {
		return
	}
	m, err := a.store.GetMessage(a.ctx, messageID)
	if err != nil {
		a.log.Warn("open the message a notification was about", "id", messageID, "err", err)
		return
	}
	a.emit(EventOpenMessage, OpenMessageEvent{
		MessageID: m.ID,
		AccountID: m.AccountID,
		FolderID:  m.FolderID,
	})
}
