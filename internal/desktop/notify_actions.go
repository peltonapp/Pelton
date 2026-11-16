package desktop

import (
	"context"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

// Buttons on a new-mail notification: Archive, Mark read and Reply, each
// switchable in the settings. They go through the Wails notification service's
// categories: UNNotificationCategory actions on macOS, toast buttons on
// Windows and D-Bus actions on Linux. A platform or notification daemon that
// does not show them leaves the plain notification, and a click on the body of
// any of them still opens the message.
//
// Reply opens the composer rather than taking text in the notification: the
// service offers a reply field on macOS and Windows but not on Linux, and a
// message should not leave without the user seeing it in a composer first.

// The identifiers of the buttons, as they come back in a response.
const (
	notifyActionArchive  = "archive"
	notifyActionMarkRead = "markRead"
	notifyActionReply    = "reply"
)

// notificationCenter is the part of the Wails notification service Pelton
// uses, so tests can stand in for it.
type notificationCenter interface {
	ServiceStartup(ctx context.Context, opts application.ServiceOptions) error
	ServiceShutdown() error
	RequestNotificationAuthorization() (bool, error)
	RegisterNotificationCategory(category notifications.NotificationCategory) error
	SendNotification(options notifications.NotificationOptions) error
	SendNotificationWithActions(options notifications.NotificationOptions) error
}

// enabledNotifyActions returns the buttons the user left on, in the order
// they are shown. on reports a setting by key.
func enabledNotifyActions(on func(key string) bool) []string {
	var out []string
	if on(settingNotifyActionArchive) {
		out = append(out, notifyActionArchive)
	}
	if on(settingNotifyActionMarkRead) {
		out = append(out, notifyActionMarkRead)
	}
	if on(settingNotifyActionReply) {
		out = append(out, notifyActionReply)
	}
	return out
}

// notifyActionTitle is the label of one button in the notification language.
func notifyActionTitle(s notifyStrings, action string) string {
	switch action {
	case notifyActionArchive:
		return s.actionArchive
	case notifyActionMarkRead:
		return s.actionMarkRead
	default:
		return s.actionReply
	}
}

// notifyCategoryID names the category for one set of buttons in one language.
// A category's titles are fixed when it is registered, so a language change
// gets a category of its own.
func notifyCategoryID(actions []string, lang string) string {
	return "pelton-mail-" + strings.Join(actions, "-") + "-" + lang
}

// ensureNotifyCategory registers the category for a set of buttons once per
// run, and reports whether it is available.
func (a *App) ensureNotifyCategory(actions []string, lang string, s notifyStrings) (string, bool) {
	id := notifyCategoryID(actions, lang)
	if _, done := a.notifyCategories.Load(id); done {
		return id, true
	}
	category := notifications.NotificationCategory{ID: id}
	for _, action := range actions {
		category.Actions = append(category.Actions, notifications.NotificationAction{ID: action, Title: notifyActionTitle(s, action)})
	}
	if err := a.notifier.RegisterNotificationCategory(category); err != nil {
		a.log.Warn("register notification buttons", "err", err)
		return "", false
	}
	a.notifyCategories.Store(id, struct{}{})
	return id, true
}

// notifyHandlers is what a button press does, split from the response parsing
// so each half can be tested on its own.
type notifyHandlers struct {
	archive  func(id int64) error
	markRead func(id int64) error
	reply    func(id int64)
	open     func(id int64)
	openFeed func(id int64)
	show     func()
}

// dispatchNotificationResponse runs what a notification response asks for: a
// button on a message acts on that message, a click on a message or a feed
// entry opens it, and anything else brings the window up. It returns the failure of a button's
// action, which the caller logs; the click paths cannot fail.
func dispatchNotificationResponse(h notifyHandlers, response notifications.NotificationResponse) error {
	id, hasMessage := notifyMessageID(response.UserInfo)
	if hasMessage {
		switch response.ActionIdentifier {
		case notifyActionArchive:
			return h.archive(id)
		case notifyActionMarkRead:
			return h.markRead(id)
		case notifyActionReply:
			h.reply(id)
			return nil
		}
		h.open(id)
		return nil
	}
	if id, ok := notifyDataID(response.UserInfo, notifyDataFeedEntry); ok && h.openFeed != nil {
		h.openFeed(id)
		return nil
	}
	h.show()
	return nil
}

// notifyHandlers wires the buttons to the app: archive and mark read run
// through the same bound methods the window uses, then ask an open window to
// reload its list, and reply raises the window and asks it for a composer.
func (a *App) notifyHandlers() notifyHandlers {
	return notifyHandlers{
		archive: func(id int64) error {
			if err := a.ready(); err != nil {
				return err
			}
			m, err := a.store.GetMessage(a.ctx, id)
			if err != nil {
				return err
			}
			if _, err := a.ArchiveMessage(id); err != nil {
				return err
			}
			a.emit(EventMailRepaired, MailRepairedEvent{AccountID: m.AccountID, FolderID: m.FolderID, Count: 1})
			return nil
		},
		markRead: func(id int64) error {
			if err := a.ready(); err != nil {
				return err
			}
			m, err := a.store.GetMessage(a.ctx, id)
			if err != nil {
				return err
			}
			if err := a.SetSeen(id, true); err != nil {
				return err
			}
			a.emit(EventMailRepaired, MailRepairedEvent{AccountID: m.AccountID, FolderID: m.FolderID, Count: 1})
			return nil
		},
		reply:    a.replyFromNotification,
		open:     a.openFromNotification,
		openFeed: a.openFeedFromNotification,
		show:     a.showWindow,
	}
}

// replyFromNotification brings the window up and asks it to open a reply to a
// message. The message is looked up first, since by the time a button is
// pressed it may have been deleted or moved; then the window is just shown.
func (a *App) replyFromNotification(messageID int64) {
	a.showWindow()
	if a.store == nil {
		return
	}
	m, err := a.store.GetMessage(a.ctx, messageID)
	if err != nil {
		a.log.Warn("reply to the message a notification was about", "id", messageID, "err", err)
		return
	}
	a.emit(EventReplyMessage, OpenMessageEvent{MessageID: m.ID, AccountID: m.AccountID, FolderID: m.FolderID})
}
