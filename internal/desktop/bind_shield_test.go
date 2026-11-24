package desktop

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/phishing"
	"github.com/peltonapp/Pelton/internal/storage"
)

const trackedHTML = `<p>Hello</p>
<img src="https://img.shop.example/hero.png" width="600" height="300">
<img src="https://x.list-manage.com/track/open.php?u=1&id=2&e=anna%40example.com" width="1" height="1">
<a href="https://shop.example/sale">sale</a>
<a href="https://click.tracker.test/c?url=https%3A%2F%2Fshop.example%2Fsale">tracked</a>
<link rel="stylesheet" href="https://cdn.shop.example/s.css">`

// failingTransport fails the test if anything is sent through it.
type failingTransport struct{ t *testing.T }

func (f failingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	f.t.Errorf("a request went out to %s", r.URL)
	return nil, errors.New("no network in this test")
}

type shieldFixture struct {
	app     *App
	account int64
	inbox   storage.Folder
}

func newShieldFixture(t *testing.T) *shieldFixture {
	t.Helper()
	a := newTrashTestApp(t)
	forgetKnownPeople()
	t.Cleanup(forgetKnownPeople)
	accountID, err := a.store.CreateAccount(a.ctx, &storage.Account{Email: "me@gmail.com", IMAPHost: "imap.gmail.com"})
	if err != nil {
		t.Fatal(err)
	}
	return &shieldFixture{app: a, account: accountID, inbox: seedFolder(t, a, accountID, "INBOX", "INBOX", nil, 0)}
}

var shieldUID uint32 = 100

func (f *shieldFixture) message(t *testing.T, m storage.Message, trust storage.MessageTrust) int64 {
	t.Helper()
	shieldUID++
	m.AccountID, m.FolderID, m.UID = f.account, f.inbox.ID, shieldUID
	id, err := f.app.store.InsertMessage(f.app.ctx, &m)
	if err != nil {
		t.Fatal(err)
	}
	trust.MessageID = id
	if err := f.app.store.SetMessageTrust(f.app.ctx, trust); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestBuildShield(t *testing.T) {
	m := storage.Message{ID: 1, BodyHTML: trackedHTML}
	trust := storage.MessageTrust{ReadReceiptTo: "receipts@shop.example"}

	blocked := buildShield(m, trust, false, false)
	if blocked.Trackers != 2 || blocked.Links != 1 {
		t.Fatalf("shield = %+v, want a blocked pixel plus the receipt request, and one tracked link", blocked)
	}
	status := map[string]string{}
	for _, it := range blocked.Items {
		status[it.Kind] = it.Status
	}
	want := map[string]string{"image": shieldBlocked, "pixel": shieldBlocked, "link": shieldClick, "receipt": shieldNotSent, "stylesheet": shieldRemoved}
	for kind, s := range want {
		if status[kind] != s {
			t.Errorf("%s = %q, want %q", kind, status[kind], s)
		}
	}

	loaded := buildShield(m, trust, true, false)
	if loaded.Trackers != 1 {
		t.Errorf("images allowed and pixels not held: trackers = %d, want only the receipt request", loaded.Trackers)
	}
	held := buildShield(m, trust, true, true)
	if held.Trackers != 2 {
		t.Errorf("images allowed, pixels held: trackers = %d, want 2", held.Trackers)
	}
	for _, it := range held.Items {
		if it.Kind == "pixel" && (it.Status != shieldBlocked || it.Company != "Mailchimp") {
			t.Errorf("held pixel = %+v", it)
		}
		if it.Kind == "image" && it.Status != shieldLoaded {
			t.Errorf("an ordinary image stayed blocked: %+v", it)
		}
	}
}

func TestShieldCountsOnceAndObeysTheSwitch(t *testing.T) {
	f := newShieldFixture(t)
	a := f.app
	unsubscribeTransport = failingTransport{t}
	t.Cleanup(func() { unsubscribeTransport = nil })

	first := f.message(t, storage.Message{MessageID: "<a@shop.example>", FromAddress: "Shop <news@shop.example>", BodyHTML: trackedHTML, Date: time.Now()}, storage.MessageTrust{AuthState: phishing.AuthNone})
	for range 2 {
		if _, err := a.GetPrivacyShield(first); err != nil {
			t.Fatal(err)
		}
	}
	report, err := a.PrivacyReport(30)
	if err != nil {
		t.Fatal(err)
	}
	if report.Mails != 1 || report.Trackers != 1 || report.Links != 1 || report.Senders != 1 || len(report.Rows) != 1 {
		t.Fatalf("report = %+v, want one mail counted once", report)
	}
	if row := report.Rows[0]; row.Address != "news@shop.example" || row.Name != "Shop" || len(row.Companies) != 1 {
		t.Errorf("row = %+v", row)
	}

	if err := a.store.Set(a.ctx, settingPrivacyCounters, "false"); err != nil {
		t.Fatal(err)
	}
	second := f.message(t, storage.Message{MessageID: "<b@shop.example>", FromAddress: "news@shop.example", BodyHTML: trackedHTML, Date: time.Now()}, storage.MessageTrust{})
	if _, err := a.GetPrivacyShield(second); err != nil {
		t.Fatal(err)
	}
	a.privacyPass([]int64{second})
	if report, _ = a.PrivacyReport(0); report.Mails != 1 || report.Counting {
		t.Errorf("with counting off = %+v, want nothing new counted", report)
	}

	if err := a.ClearPrivacyHistory(); err != nil {
		t.Fatal(err)
	}
	if report, _ = a.PrivacyReport(0); report.Mails != 0 || len(report.Rows) != 0 {
		t.Errorf("after clearing = %+v", report)
	}

	// the passport never reaches the network either.
	if _, err := a.GetSenderPassport(first); err != nil {
		t.Fatal(err)
	}
}

func TestStillSendingAfterTheGracePeriod(t *testing.T) {
	f := newShieldFixture(t)
	a := f.app
	unsubscribeTransport = failingTransport{t}
	t.Cleanup(func() { unsubscribeTransport = nil })

	at := time.Now().Add(-30 * 24 * time.Hour)
	if err := a.store.RecordUnsubscribe(a.ctx, storage.Unsubscribe{Sender: "news@shop.example", ListID: "<news.shop.example>", Method: "oneclick", At: at}); err != nil {
		t.Fatal(err)
	}
	within := f.message(t, storage.Message{FromAddress: "news@shop.example", ListID: "<news.shop.example>", Date: at.Add(5 * 24 * time.Hour)}, storage.MessageTrust{})
	a.privacyPass([]int64{within})
	shield, err := a.GetPrivacyShield(within)
	if err != nil {
		t.Fatal(err)
	}
	if shield.Unsub == nil || shield.Unsub.StillSending {
		t.Fatalf("mail inside the grace period = %+v, want unsubscribed and not yet still sending", shield.Unsub)
	}

	after := f.message(t, storage.Message{FromAddress: "promo@shop.example", ListID: "<news.shop.example>", Date: at.Add(12 * 24 * time.Hour)}, storage.MessageTrust{})
	a.privacyPass([]int64{after, after})
	if shield, err = a.GetPrivacyShield(after); err != nil {
		t.Fatal(err)
	}
	if shield.Unsub == nil || !shield.Unsub.StillSending || shield.Unsub.StillCount != 1 {
		t.Fatalf("mail from the same list after 12 days = %+v, want still sending, counted once", shield.Unsub)
	}

	if err := a.DismissStillSending("news@shop.example"); err != nil {
		t.Fatal(err)
	}
	if shield, _ = a.GetPrivacyShield(after); shield.Unsub.StillSending {
		t.Error("a dismissed notice came back without new mail")
	}

	if err := a.store.Set(a.ctx, settingUnsubscribeGrace, "20"); err != nil {
		t.Fatal(err)
	}
	if err := a.store.RecordUnsubscribe(a.ctx, storage.Unsubscribe{Sender: "news@shop.example", ListID: "<news.shop.example>", At: at}); err != nil {
		t.Fatal(err)
	}
	a.privacyPass([]int64{after})
	if shield, _ = a.GetPrivacyShield(after); shield.Unsub.StillSending {
		t.Error("a longer grace period did not hold the notice back")
	}
}

// oneClickMessage stores a message offering a one-click unsubscribe at
// target, with a record that does or does not vouch for it.
func oneClickMessage(t *testing.T, f *shieldFixture, target string, verified bool) int64 {
	trust := storage.MessageTrust{AuthState: phishing.AuthTrusted, DKIM: "pass", DKIMDomains: []string{"shop.example"}}
	if verified {
		trust.UnsubSigners = []string{"shop.example"}
	}
	return f.message(t, storage.Message{
		FromAddress: "News <news@shop.example>", ListID: "<news.shop.example>", Date: time.Now(),
		ListUnsubscribe: "<mailto:u@shop.example>, <" + target + ">", ListUnsubscribePost: true,
	}, trust)
}

func TestOneClickRequestShape(t *testing.T) {
	var got *http.Request
	var body string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	unsubscribeTransport = server.Client().Transport
	t.Cleanup(func() { unsubscribeTransport = nil })

	f := newShieldFixture(t)
	id := oneClickMessage(t, f, server.URL+"/unsub?list=news&token=abc", true)
	if err := f.app.Unsubscribe(id); err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Method != http.MethodPost || got.URL.Path != "/unsub" || got.URL.Query().Get("token") != "abc" {
		t.Fatalf("request = %+v", got)
	}
	if ct := got.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" || body != "List-Unsubscribe=One-Click" {
		t.Errorf("content type %q, body %q", ct, body)
	}
	if got.Header.Get("Cookie") != "" || got.Header.Get("Authorization") != "" {
		t.Errorf("the request carried credentials: %v", got.Header)
	}
	u, ok, err := f.app.store.UnsubscribeFor(f.app.ctx, "news@shop.example", "")
	if err != nil || !ok || u.Method != unsubKindOneClick || u.ListID != "<news.shop.example>" {
		t.Errorf("record = %+v, %t, %v", u, ok, err)
	}
}

func TestOneClickRefusesRedirectsElsewhere(t *testing.T) {
	var reached bool
	other := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true }))
	defer other.Close()
	otherURL, _ := url.Parse(other.URL)
	elsewhere := "https://localhost:" + otherURL.Port() + "/collect"

	var sameHost bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/same":
			http.Redirect(w, r, "/done", http.StatusTemporaryRedirect)
		case "/done":
			sameHost = true
		default:
			http.Redirect(w, r, elsewhere, http.StatusTemporaryRedirect)
		}
	}))
	defer server.Close()
	unsubscribeTransport = server.Client().Transport
	t.Cleanup(func() { unsubscribeTransport = nil })

	f := newShieldFixture(t)
	away := oneClickMessage(t, f, server.URL+"/away", true)
	if err := f.app.Unsubscribe(away); err == nil || !strings.Contains(err.Error(), "refused") {
		t.Errorf("Unsubscribe = %v, want the redirect refused", err)
	}
	if reached {
		t.Error("the request followed a redirect to another host")
	}
	if _, ok, _ := f.app.store.UnsubscribeFor(f.app.ctx, "news@shop.example", ""); ok {
		t.Error("a refused unsubscribe was recorded as done")
	}

	same := oneClickMessage(t, f, server.URL+"/same", true)
	if err := f.app.Unsubscribe(same); err != nil || !sameHost {
		t.Errorf("a redirect on the same host = %v, reached %t", err, sameHost)
	}
}

func TestUnverifiedOneClickIsAWebPage(t *testing.T) {
	unsubscribeTransport = failingTransport{t}
	t.Cleanup(func() { unsubscribeTransport = nil })
	f := newShieldFixture(t)
	id := oneClickMessage(t, f, "https://shop.example/unsub", false)
	if err := f.app.Unsubscribe(id); !errors.Is(err, errUnverifiedUnsubscribe) {
		t.Errorf("Unsubscribe = %v, want it refused", err)
	}
	m, err := f.app.store.GetMessage(f.app.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	info := f.app.unsubscribeInfo(m)
	if info.Kind != unsubKindLink || !info.Unverified || info.Target != "https://shop.example/unsub" {
		t.Errorf("info = %+v, want the page offered as unverified", info)
	}
	if err := f.app.NoteUnsubscribeLink(id); err != nil {
		t.Fatal(err)
	}
	if u, ok, _ := f.app.store.UnsubscribeFor(f.app.ctx, "news@shop.example", ""); !ok || u.Method != unsubKindLink {
		t.Errorf("an opened page was not recorded for the still-sending watch: %+v", u)
	}

	// the classic design keeps the behaviour it had.
	if err := f.app.store.Set(f.app.ctx, settingDesign, "classic"); err != nil {
		t.Fatal(err)
	}
	if info = f.app.unsubscribeInfo(m); info.Kind != unsubKindOneClick || info.Unverified {
		t.Errorf("classic info = %+v, want it unchanged", info)
	}
}
