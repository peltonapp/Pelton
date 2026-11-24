package desktop

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

// fakeCenter records what Pelton asks the notification service for.
type fakeCenter struct {
	categories  []notifications.NotificationCategory
	plain       []notifications.NotificationOptions
	withAction  []notifications.NotificationOptions
	registerErr error
}

func (f *fakeCenter) ServiceStartup(context.Context, application.ServiceOptions) error { return nil }
func (f *fakeCenter) ServiceShutdown() error                                           { return nil }
func (f *fakeCenter) RequestNotificationAuthorization() (bool, error)                  { return true, nil }
func (f *fakeCenter) RegisterNotificationCategory(c notifications.NotificationCategory) error {
	if f.registerErr != nil {
		return f.registerErr
	}
	f.categories = append(f.categories, c)
	return nil
}
func (f *fakeCenter) SendNotification(o notifications.NotificationOptions) error {
	f.plain = append(f.plain, o)
	return nil
}
func (f *fakeCenter) SendNotificationWithActions(o notifications.NotificationOptions) error {
	f.withAction = append(f.withAction, o)
	return nil
}

func fakeApp(center *fakeCenter) *App {
	a := &App{notifier: center, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	a.notifyReady.Store(true)
	return a
}

func TestEnabledNotifyActionsFollowsTheSettings(t *testing.T) {
	cases := []struct {
		name string
		off  map[string]bool
		want []string
	}{
		{"all on", nil, []string{"archive", "markRead", "reply"}},
		{"archive off", map[string]bool{settingNotifyActionArchive: true}, []string{"markRead", "reply"}},
		{"only reply", map[string]bool{settingNotifyActionArchive: true, settingNotifyActionMarkRead: true}, []string{"reply"}},
		{"none", map[string]bool{settingNotifyActionArchive: true, settingNotifyActionMarkRead: true, settingNotifyActionReply: true}, nil},
	}
	for _, c := range cases {
		got := enabledNotifyActions(func(key string) bool { return !c.off[key] })
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestNewMailNotificationCarriesTheButtons(t *testing.T) {
	center := &fakeCenter{}
	a := fakeApp(center)

	if err := a.deliverNotification(notification{title: "New mail", body: "x", messageID: 42, actions: true}); err != nil {
		t.Fatal(err)
	}
	if len(center.categories) != 1 || len(center.withAction) != 1 || len(center.plain) != 0 {
		t.Fatalf("want one category and one notification with actions, got %d, %d, %d", len(center.categories), len(center.withAction), len(center.plain))
	}
	category := center.categories[0]
	var ids, titles []string
	for _, action := range category.Actions {
		ids = append(ids, action.ID)
		titles = append(titles, action.Title)
	}
	if !reflect.DeepEqual(ids, []string{"archive", "markRead", "reply"}) || !reflect.DeepEqual(titles, []string{"Archive", "Mark read", "Reply"}) {
		t.Errorf("actions = %v %v", ids, titles)
	}
	if category.HasReplyField {
		t.Error("reply opens the composer; the category must not ask for an inline reply field")
	}
	sent := center.withAction[0]
	if sent.CategoryID != category.ID {
		t.Errorf("notification category %q, registered %q", sent.CategoryID, category.ID)
	}
	if id, ok := notifyMessageID(sent.Data); !ok || id != 42 {
		t.Errorf("the message id has to travel with the notification, got %v", sent.Data)
	}
}

func TestCategoryIsRegisteredOncePerRun(t *testing.T) {
	center := &fakeCenter{}
	a := fakeApp(center)
	for i := 0; i < 3; i++ {
		if err := a.deliverNotification(notification{title: "t", messageID: int64(i + 1), actions: true}); err != nil {
			t.Fatal(err)
		}
	}
	if len(center.categories) != 1 {
		t.Errorf("registered %d times, want once", len(center.categories))
	}
	if len(center.withAction) != 3 {
		t.Errorf("sent %d with actions, want 3", len(center.withAction))
	}
}

func TestNotificationsWithoutAMessageStayPlain(t *testing.T) {
	center := &fakeCenter{}
	a := fakeApp(center)
	// a summary, a focus recap and a reminder name no message to act on.
	for _, n := range []notification{
		{title: "t", body: "5 new messages"},
		{title: "t", messageID: 7},
	} {
		if err := a.deliverNotification(n); err != nil {
			t.Fatal(err)
		}
	}
	if len(center.withAction) != 0 || len(center.categories) != 0 || len(center.plain) != 2 {
		t.Errorf("got %d with actions, %d categories, %d plain", len(center.withAction), len(center.categories), len(center.plain))
	}
}

func TestNotificationFallsBackToPlainWhenTheCategoryCannotBeRegistered(t *testing.T) {
	center := &fakeCenter{registerErr: errors.New("not supported")}
	a := fakeApp(center)
	if err := a.deliverNotification(notification{title: "t", messageID: 1, actions: true}); err != nil {
		t.Fatal(err)
	}
	if len(center.plain) != 1 || center.plain[0].CategoryID != "" || len(center.withAction) != 0 {
		t.Errorf("want one plain notification, got plain=%d withAction=%d", len(center.plain), len(center.withAction))
	}
}

func TestNotifyCategoryIDsDifferByButtonsAndLanguage(t *testing.T) {
	all := []string{"archive", "markRead", "reply"}
	ids := map[string]bool{
		notifyCategoryID(all, "en"):                 true,
		notifyCategoryID(all, "de"):                 true,
		notifyCategoryID([]string{"reply"}, "en"):   true,
		notifyCategoryID([]string{"archive"}, "en"): true,
	}
	if len(ids) != 4 {
		t.Errorf("category ids collide: %v", ids)
	}
}

func TestEveryLanguageNamesTheButtons(t *testing.T) {
	for lang, s := range notifyLocales {
		for _, action := range []string{notifyActionArchive, notifyActionMarkRead, notifyActionReply} {
			if notifyActionTitle(s, action) == "" {
				t.Errorf("%s has no title for %s", lang, action)
			}
		}
	}
}

// recorder logs which handler a response reached.
type recorder struct {
	calls  []string
	failOn string
}

func (r *recorder) handlers() notifyHandlers {
	do := func(name string) func(int64) error {
		return func(id int64) error {
			r.calls = append(r.calls, name)
			if r.failOn == name {
				return errors.New(name + " failed")
			}
			return nil
		}
	}
	return notifyHandlers{
		archive:  do("archive"),
		markRead: do("markRead"),
		reply:    func(int64) { r.calls = append(r.calls, "reply") },
		open:     func(int64) { r.calls = append(r.calls, "open") },
		show:     func() { r.calls = append(r.calls, "show") },
	}
}

func TestDispatchRoutesEachButtonToItsAction(t *testing.T) {
	data := map[string]any{notifyDataMessageID: float64(9)}
	cases := []struct {
		name     string
		response notifications.NotificationResponse
		want     string
	}{
		{"archive", notifications.NotificationResponse{ActionIdentifier: "archive", UserInfo: data}, "archive"},
		{"mark read", notifications.NotificationResponse{ActionIdentifier: "markRead", UserInfo: data}, "markRead"},
		{"reply", notifications.NotificationResponse{ActionIdentifier: "reply", UserInfo: data}, "reply"},
		{"plain click", notifications.NotificationResponse{ActionIdentifier: notifications.DefaultActionIdentifier, UserInfo: data}, "open"},
		{"unknown button counts as a click", notifications.NotificationResponse{ActionIdentifier: "something-else", UserInfo: data}, "open"},
		{"click on a summary", notifications.NotificationResponse{ActionIdentifier: notifications.DefaultActionIdentifier}, "show"},
		{"button without a message", notifications.NotificationResponse{ActionIdentifier: "archive"}, "show"},
	}
	for _, c := range cases {
		r := &recorder{}
		if err := dispatchNotificationResponse(r.handlers(), c.response); err != nil {
			t.Errorf("%s: %v", c.name, err)
		}
		if !reflect.DeepEqual(r.calls, []string{c.want}) {
			t.Errorf("%s: handlers called %v, want [%s]", c.name, r.calls, c.want)
		}
	}
}

func TestDispatchHandsTheMessageIDToTheAction(t *testing.T) {
	var got int64
	h := notifyHandlers{
		archive:  func(id int64) error { got = id; return nil },
		markRead: func(int64) error { return nil },
		reply:    func(int64) {},
		open:     func(int64) {},
		show:     func() {},
	}
	err := dispatchNotificationResponse(h, notifications.NotificationResponse{
		ActionIdentifier: "archive",
		UserInfo:         map[string]any{notifyDataMessageID: "314"},
	})
	if err != nil || got != 314 {
		t.Errorf("archive got id %d, err %v", got, err)
	}
}

func TestDispatchReportsAFailedButton(t *testing.T) {
	r := &recorder{failOn: "archive"}
	err := dispatchNotificationResponse(r.handlers(), notifications.NotificationResponse{
		ActionIdentifier: "archive",
		UserInfo:         map[string]any{notifyDataMessageID: 1},
	})
	if err == nil {
		t.Error("a failed archive has to come back to the caller to be logged")
	}
}
