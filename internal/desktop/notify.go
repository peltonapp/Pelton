package desktop

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

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
	// actions asks for the Archive, Mark read and Reply buttons the user left
	// on (notify_actions.go). Only a notification about one message has them.
	actions bool
	// feedEntryID is set instead for a new feed entry.
	feedEntryID int64
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
	return notifyDataID(data, notifyDataMessageID)
}

// notifyDataID reads a positive id stored under key in a notification's data.
func notifyDataID(data map[string]any, key string) (int64, bool) {
	var id int64
	switch v := data[key].(type) {
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

// deliverNotification posts one notification. Without the service (tests, or
// a dev build outside an .app on macOS) it does nothing. On macOS the first one asks for permission; after that the
// stored answer is used without asking again. The sound is off on purpose:
// what Pelton should make a noise about is its own question (#240).
func (a *App) deliverNotification(n notification) error {
	if a.notifier == nil || !a.notifyReady.Load() {
		return nil
	}
	if ok, err := a.notifier.RequestNotificationAuthorization(); err != nil || !ok {
		return fmt.Errorf("pelton: notifications not allowed: %v", err)
	}
	data := map[string]any{}
	if n.messageID > 0 {
		data[notifyDataMessageID] = n.messageID
	}
	if n.feedEntryID > 0 {
		data[notifyDataFeedEntry] = n.feedEntryID
	}
	options := notifications.NotificationOptions{
		ID:    "pelton-" + strconv.FormatInt(notifySeq.Add(1), 10),
		Title: n.title,
		Body:  n.body,
		Data:  data,
		Sound: &notifications.NotificationSound{Silent: true},
	}
	if n.actions && n.messageID > 0 {
		lang := a.stringSetting(settingLanguage, "en")
		actions := enabledNotifyActions(func(key string) bool { return a.boolSetting(key, true) })
		if len(actions) > 0 {
			if category, ok := a.ensureNotifyCategory(actions, lang, notifyStringsFor(lang)); ok {
				options.CategoryID = category
				return a.notifier.SendNotificationWithActions(options)
			}
		}
	}
	return a.notifier.SendNotification(options)
}

// onNotificationResponse runs when a notification is clicked or one of its
// buttons is pressed. See dispatchNotificationResponse. A button's work can
// reach the mail server, so it runs off the thread the system called on.
func (a *App) onNotificationResponse(result notifications.NotificationResult) {
	if result.Error != nil {
		a.log.Warn("notification response", "err", result.Error)
		return
	}
	go func() {
		if err := dispatchNotificationResponse(a.notifyHandlers(), result.Response); err != nil {
			a.log.Warn("notification button", "action", result.Response.ActionIdentifier, "err", err)
		}
	}()
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
	followUp    string // title of a follow-up reminder that came due
	followUpTo  string // first body line of a follow-up reminder, with %s for the recipients

	// the summary raised when focus mode ends (bind_focus.go). Many messages
	// with none from a VIP reuse summary.
	focusEnded      string // its title
	focusOne        string // one message, not from a VIP
	focusOneVIP     string // one message, from a VIP
	focusManyVIP    string // %d messages, %d of them from VIPs
	focusManyOneVIP string // %d messages, one of them from a VIP

	// the buttons of a new-mail notification (notify_actions.go).
	actionArchive  string
	actionMarkRead string
	actionReply    string
	// the body that stands in for any notification while the app is locked
	// with notifications hidden (applock.go).
	locked string
	// the Feeds space (feeds_refresh.go).
	feeds   string // title of a summary over several feeds
	feedNew string // %d new entries
}

var notifyLocales = map[string]notifyStrings{
	"en": {
		newMail: "New mail", vipNewMail: "New mail from a VIP", noSubject: "(no subject)", fromUnknown: "Unknown sender", summary: "%d new messages",
		followUp: "No reply yet", followUpTo: "To %s",
		focusEnded: "Focus ended", focusOne: "1 new message", focusOneVIP: "1 new message, from a VIP",
		focusManyVIP: "%d new messages, %d from VIPs", focusManyOneVIP: "%d new messages, 1 from a VIP",
		actionArchive: "Archive", actionMarkRead: "Mark read", actionReply: "Reply",
		locked: "Unlock Pelton to see what is new.",
		feeds:  "Feeds", feedNew: "%d new articles",
	},
	"de": {
		newMail: "Neue Nachricht", vipNewMail: "Neue Nachricht von einem VIP", noSubject: "(kein Betreff)", fromUnknown: "Unbekannter Absender", summary: "%d neue Nachrichten",
		followUp: "Noch keine Antwort", followUpTo: "An %s",
		focusEnded: "Fokus beendet", focusOne: "1 neue Nachricht", focusOneVIP: "1 neue Nachricht, von einem VIP",
		focusManyVIP: "%d neue Nachrichten, %d von VIPs", focusManyOneVIP: "%d neue Nachrichten, 1 von einem VIP",
		actionArchive: "Archivieren", actionMarkRead: "Als gelesen markieren", actionReply: "Antworten",
		locked: "Entsperre Pelton, um zu sehen, was neu ist.",
		feeds:  "Feeds", feedNew: "%d neue Artikel",
	},
	"fr": {
		newMail: "Nouveau message", vipNewMail: "Nouveau message d'un VIP", noSubject: "(sans objet)", fromUnknown: "Expéditeur inconnu", summary: "%d nouveaux messages",
		followUp: "Pas encore de réponse", followUpTo: "À %s",
		focusEnded: "Concentration terminée", focusOne: "1 nouveau message", focusOneVIP: "1 nouveau message, d'un VIP",
		focusManyVIP: "%d nouveaux messages, dont %d de VIP", focusManyOneVIP: "%d nouveaux messages, dont 1 d'un VIP",
		actionArchive: "Archiver", actionMarkRead: "Marquer comme lu", actionReply: "Répondre",
		locked: "Déverrouille Pelton pour voir les nouveautés.",
		feeds:  "Flux", feedNew: "%d nouveaux articles",
	},
	"nl": {
		newMail: "Nieuw bericht", vipNewMail: "Nieuw bericht van een VIP", noSubject: "(geen onderwerp)", fromUnknown: "Onbekende afzender", summary: "%d nieuwe berichten",
		followUp: "Nog geen antwoord", followUpTo: "Aan %s",
		focusEnded: "Focus beëindigd", focusOne: "1 nieuw bericht", focusOneVIP: "1 nieuw bericht, van een VIP",
		focusManyVIP: "%d nieuwe berichten, %d van VIP's", focusManyOneVIP: "%d nieuwe berichten, 1 van een VIP",
		actionArchive: "Archiveren", actionMarkRead: "Markeer als gelezen", actionReply: "Beantwoorden",
		locked: "Ontgrendel Pelton om te zien wat er nieuw is.",
		feeds:  "Feeds", feedNew: "%d nieuwe artikelen",
	},
	"es": {
		newMail: "Nuevo mensaje", vipNewMail: "Nuevo mensaje de un VIP", noSubject: "(sin asunto)", fromUnknown: "Remitente desconocido", summary: "%d mensajes nuevos",
		followUp: "Aún sin respuesta", followUpTo: "Para %s",
		focusEnded: "Concentración terminada", focusOne: "1 mensaje nuevo", focusOneVIP: "1 mensaje nuevo, de un VIP",
		focusManyVIP: "%d mensajes nuevos, %d de VIP", focusManyOneVIP: "%d mensajes nuevos, 1 de un VIP",
		actionArchive: "Archivar", actionMarkRead: "Marcar como leído", actionReply: "Responder",
		locked: "Desbloquea Pelton para ver las novedades.",
		feeds:  "Fuentes", feedNew: "%d artículos nuevos",
	},
	"it": {
		newMail: "Nuova email", vipNewMail: "Nuova email da un VIP", noSubject: "(nessun oggetto)", fromUnknown: "Mittente sconosciuto", summary: "%d nuove email",
		followUp: "Ancora nessuna risposta", followUpTo: "A %s",
		focusEnded: "Concentrazione terminata", focusOne: "1 nuova email", focusOneVIP: "1 nuova email, da un VIP",
		focusManyVIP: "%d nuove email, %d da VIP", focusManyOneVIP: "%d nuove email, 1 da un VIP",
		actionArchive: "Archivia", actionMarkRead: "Segna come letto", actionReply: "Rispondi",
		locked: "Sblocca Pelton per vedere le novità.",
		feeds:  "Feed", feedNew: "%d nuovi articoli",
	},
	"pl": {
		newMail: "Nowa wiadomość", vipNewMail: "Nowa wiadomość od VIP-a", noSubject: "(bez tematu)", fromUnknown: "Nieznany nadawca", summary: "Nowe wiadomości: %d",
		followUp: "Brak odpowiedzi", followUpTo: "Do: %s",
		focusEnded: "Skupienie zakończone", focusOne: "1 nowa wiadomość", focusOneVIP: "1 nowa wiadomość, od VIP-a",
		focusManyVIP: "Nowe wiadomości: %d, od VIP-ów: %d", focusManyOneVIP: "Nowe wiadomości: %d, od VIP-a: 1",
		actionArchive: "Archiwizuj", actionMarkRead: "Oznacz jako przeczytane", actionReply: "Odpowiedz",
		locked: "Odblokuj Pelton, aby zobaczyć, co nowego.",
		feeds:  "Kanały", feedNew: "Nowe artykuły: %d",
	},
	"ru": {
		newMail: "Новая почта", vipNewMail: "Новая почта от VIP-отправителя", noSubject: "(без темы)", fromUnknown: "Неизвестный отправитель", summary: "Новых писем: %d",
		followUp: "Ответа пока нет", followUpTo: "Кому: %s",
		focusEnded: "Режим фокуса завершён", focusOne: "1 новое письмо", focusOneVIP: "1 новое письмо, от VIP-отправителя",
		focusManyVIP: "Новых писем: %d, от VIP: %d", focusManyOneVIP: "Новых писем: %d, от VIP: 1",
		actionArchive: "В архив", actionMarkRead: "Прочитано", actionReply: "Ответить",
		locked: "Разблокируйте Pelton, чтобы увидеть новое.",
		feeds:  "Ленты", feedNew: "Новых статей: %d",
	},
	"tr": {
		newMail: "Yeni posta", vipNewMail: "Bir VIP'ten yeni posta", noSubject: "(konu yok)", fromUnknown: "Bilinmeyen gönderici", summary: "%d yeni posta",
		followUp: "Henüz yanıt yok", followUpTo: "Alıcı: %s",
		focusEnded: "Odak sona erdi", focusOne: "1 yeni posta", focusOneVIP: "1 yeni posta, bir VIP'ten",
		focusManyVIP: "%d yeni posta, %d tanesi VIP'lerden", focusManyOneVIP: "%d yeni posta, 1 tanesi bir VIP'ten",
		actionArchive: "Arşivle", actionMarkRead: "Okundu işaretle", actionReply: "Yanıtla",
		locked: "Yenilikleri görmek için Pelton'un kilidini açın.",
		feeds:  "Akışlar", feedNew: "%d yeni makale",
	},
	"pt": {
		newMail: "Correio novo", vipNewMail: "Correio novo de um VIP", noSubject: "(sem assunto)", fromUnknown: "Remetente desconhecido", summary: "%d mensagens novas",
		followUp: "Ainda sem resposta", followUpTo: "Para %s",
		focusEnded: "Foco terminado", focusOne: "1 mensagem nova", focusOneVIP: "1 mensagem nova, de um VIP",
		focusManyVIP: "%d mensagens novas, %d de VIPs", focusManyOneVIP: "%d mensagens novas, 1 de um VIP",
		actionArchive: "Arquivar", actionMarkRead: "Marcar como lida", actionReply: "Responder",
		locked: "Desbloqueie o Pelton para ver as novidades.",
		feeds:  "Feeds", feedNew: "%d artigos novos",
	},
	"ar": {
		newMail: "بريد جديد", vipNewMail: "بريد جديد من جهة مهمة", noSubject: "(بلا موضوع)", fromUnknown: "مرسل غير معروف", summary: "رسائل جديدة: %d",
		followUp: "لا رد بعد", followUpTo: "إلى %s",
		focusEnded: "انتهى وضع التركيز", focusOne: "رسالة جديدة واحدة", focusOneVIP: "رسالة جديدة واحدة من جهة مهمة",
		focusManyVIP: "رسائل جديدة: %d، من جهات مهمة: %d", focusManyOneVIP: "رسائل جديدة: %d، من جهة مهمة: 1",
		actionArchive: "أرشفة", actionMarkRead: "تعليم كمقروء", actionReply: "رد",
		locked: "افتح قفل Pelton لرؤية الجديد.",
		feeds:  "الخلاصات", feedNew: "مقالات جديدة: %d",
	},
	"zh-CN": {
		newMail: "新邮件", vipNewMail: "来自 VIP 的新邮件", noSubject: "（无主题）", fromUnknown: "未知发件人", summary: "%d 封新邮件",
		followUp: "尚无回复", followUpTo: "收件人：%s",
		focusEnded: "专注已结束", focusOne: "1 封新邮件", focusOneVIP: "1 封新邮件，来自 VIP",
		focusManyVIP: "%d 封新邮件，其中 %d 封来自 VIP", focusManyOneVIP: "%d 封新邮件，其中 1 封来自 VIP",
		actionArchive: "归档", actionMarkRead: "标为已读", actionReply: "回复",
		locked: "解锁 Pelton 以查看新内容。",
		feeds:  "订阅源", feedNew: "%d 篇新文章",
	},
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
// During focus mode only VIP senders notify, and every new message is counted
// for the summary raised when focus ends.
func (a *App) notifyNewMail(folder storage.Folder, ids []int64) {
	if len(ids) == 0 || !strings.EqualFold(folder.IMAPPath, "INBOX") {
		return
	}
	focused := a.advanceFocus(time.Now()).Active
	general := a.boolSetting(settingNotifyNewMail, false)
	vips := a.vipSet()
	if !focused && !general && len(vips) == 0 {
		return
	}

	s := notifyStringsFor(a.stringSetting(settingLanguage, "en"))
	var pending []notification
	var held, heldVIP int
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
		// a muted conversation stays quiet, VIP sender or not.
		if m.Muted {
			continue
		}
		vip := vips[bareAddress(m.FromAddress)]
		if focused {
			held++
			if vip {
				heldVIP++
			}
		}
		if !notifyAllowed(general, vip, focused) {
			continue
		}

		title := s.newMail
		if vip {
			title = s.vipNewMail
		}
		pending = append(pending, notification{title: title, body: notifyBody(m, s), messageID: m.ID, actions: true})
	}
	if held > 0 {
		a.recordHeld(held, heldVIP)
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
	n = a.maskForLock(n)
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
