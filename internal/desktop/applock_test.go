package desktop

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/applock"
	"github.com/peltonapp/Pelton/internal/storage"
)

// fakeAuth stands in for the system prompt, so no test raises a real one.
type fakeAuth struct {
	present bool
	pass    bool
	calls   int
	reasons []string
}

func (f *fakeAuth) available() bool { return f.present }

func (f *fakeAuth) authenticate(reason string) (bool, error) {
	f.calls++
	f.reasons = append(f.reasons, reason)
	return f.pass, nil
}

// newLockTestApp returns an App with a store, a lock and the given system
// prompt, which may be nil for a platform without one.
func newLockTestApp(t *testing.T, auth *fakeAuth) *App {
	t.Helper()
	ctx, stopBackground := testContext(t)
	store, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	t.Cleanup(stopBackground)
	if err := store.RunMigrations(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	a := &App{ctx: ctx, store: store, log: slog.New(slog.DiscardHandler), lock: applock.New(nil)}
	a.lock.OnChange(a.onLockChanged)
	if auth != nil {
		a.sysAuth = auth
	}
	return a
}

// gateRequest builds a runtime call the way the Wails runtime posts it.
func gateRequest(t *testing.T, object, method int, args any) *http.Request {
	t.Helper()
	body := map[string]any{"object": object, "method": method}
	if args != nil {
		body["args"] = args
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return httptest.NewRequest(http.MethodPost, runtimePath, strings.NewReader(string(raw)))
}

// callArgs are the args of a bound method call by id.
func callArgs(name string) map[string]any {
	return map[string]any{"call-id": "c1", "methodID": methodID(appMethodPrefix + name), "args": []any{}}
}

// serveGate runs req through the gate and reports whether it reached the
// next handler, and with which body.
func serveGate(t *testing.T, locked bool, req *http.Request) (*httptest.ResponseRecorder, bool, string) {
	t.Helper()
	gate := newLockGate(func() bool { return locked })
	reached := false
	seen := ""
	next := http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		reached = true
		if r.Body != nil {
			b, _ := io.ReadAll(r.Body)
			seen = string(b)
		}
		rw.WriteHeader(http.StatusOK)
	})
	rec := httptest.NewRecorder()
	gate.middleware(next).ServeHTTP(rec, req)
	return rec, reached, seen
}

func TestLockGate(t *testing.T) {
	tests := []struct {
		name   string
		locked bool
		req    func(t *testing.T) *http.Request
		want   bool
	}{
		{"unlocked lets mail through", false, func(t *testing.T) *http.Request { return gateRequest(t, 0, 0, callArgs("ListMessages")) }, true},
		{"locked refuses mail", true, func(t *testing.T) *http.Request { return gateRequest(t, 0, 0, callArgs("ListMessages")) }, false},
		{"locked refuses settings", true, func(t *testing.T) *http.Request { return gateRequest(t, 0, 0, callArgs("GetUIPrefs")) }, false},
		{"locked refuses a setting write", true, func(t *testing.T) *http.Request { return gateRequest(t, 0, 0, callArgs("SetSetting")) }, false},
		{"locked allows the lock state", true, func(t *testing.T) *http.Request { return gateRequest(t, 0, 0, callArgs("GetLockState")) }, true},
		{"locked allows unlocking", true, func(t *testing.T) *http.Request { return gateRequest(t, 0, 0, callArgs("UnlockWithPassphrase")) }, true},
		{"locked allows the window chrome", true, func(t *testing.T) *http.Request { return gateRequest(t, 0, 0, callArgs("MinimiseWindow")) }, true},
		{"locked refuses an unknown id", true, func(t *testing.T) *http.Request {
			return gateRequest(t, 0, 0, map[string]any{"call-id": "c1", "methodID": 12345})
		}, false},
		{"locked allows by name", true, func(t *testing.T) *http.Request {
			return gateRequest(t, 0, 0, map[string]any{"call-id": "c1", "methodName": appMethodPrefix + "GetLockState"})
		}, true},
		{"locked refuses mail by name", true, func(t *testing.T) *http.Request {
			return gateRequest(t, 0, 0, map[string]any{"call-id": "c1", "methodName": appMethodPrefix + "GetMessage"})
		}, false},
		{"locked refuses another service by name", true, func(t *testing.T) *http.Request {
			return gateRequest(t, 0, 0, map[string]any{"call-id": "c1", "methodName": "github.com/other/pkg.Service.GetLockState"})
		}, false},
		{"locked allows window requests", true, func(t *testing.T) *http.Request { return gateRequest(t, runtimeObjectWindow, 3, nil) }, true},
		{"locked allows events", true, func(t *testing.T) *http.Request { return gateRequest(t, runtimeObjectEvents, 0, nil) }, true},
		{"locked refuses the clipboard", true, func(t *testing.T) *http.Request { return gateRequest(t, 1, 0, nil) }, false},
		{"locked refuses dialogs", true, func(t *testing.T) *http.Request { return gateRequest(t, 5, 0, nil) }, false},
		{"locked refuses an unreadable body", true, func(t *testing.T) *http.Request {
			return httptest.NewRequest(http.MethodPost, runtimePath, strings.NewReader("{"))
		}, false},
		{"locked refuses a chunked call", true, func(t *testing.T) *http.Request {
			req := gateRequest(t, 0, 0, callArgs("GetLockState"))
			req.Header.Set(runtimeChunkHeader, "x")
			return req
		}, false},
		{"locked refuses an oversized call", true, func(t *testing.T) *http.Request {
			args := callArgs("GetLockState")
			args["args"] = []any{strings.Repeat("x", maxGatedBody)}
			return gateRequest(t, 0, 0, args)
		}, false},
		{"locked reads the query when the body is empty", true, func(t *testing.T) *http.Request {
			raw, _ := json.Marshal(callArgs("GetLockState"))
			q := url.Values{"object": {"0"}, "method": {"0"}, "args": {string(raw)}}
			return httptest.NewRequest(http.MethodPost, runtimePath+"?"+q.Encode(), nil)
		}, true},
		{"locked refuses mail through the query", true, func(t *testing.T) *http.Request {
			raw, _ := json.Marshal(callArgs("ListMessages"))
			q := url.Values{"object": {"0"}, "method": {"0"}, "args": {string(raw)}}
			return httptest.NewRequest(http.MethodPost, runtimePath+"?"+q.Encode(), nil)
		}, false},
		{"locked leaves the assets alone", true, func(t *testing.T) *http.Request {
			return httptest.NewRequest(http.MethodGet, "/assets/index.js", nil)
		}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, reached, _ := serveGate(t, tt.locked, tt.req(t))
			if reached != tt.want {
				t.Fatalf("reached = %v, want %v", reached, tt.want)
			}
			if tt.want {
				return
			}
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422", rec.Code)
			}
			var got struct {
				Message string            `json:"message"`
				Kind    string            `json:"kind"`
				Cause   map[string]string `json:"cause"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("refusal is not json: %v", err)
			}
			if got.Kind != "RuntimeError" || got.Cause["code"] != "locked" {
				t.Fatalf("refusal = %+v, want a RuntimeError with cause code locked", got)
			}
		})
	}
}

func TestLockGateKeepsTheBodyForWails(t *testing.T) {
	req := gateRequest(t, 0, 0, callArgs("GetLockState"))
	want, _ := json.Marshal(map[string]any{"object": 0, "method": 0, "args": callArgs("GetLockState")})
	_, reached, seen := serveGate(t, true, req)
	if !reached || seen != string(want) {
		t.Fatalf("reached=%v body=%q, want the original body %q", reached, seen, want)
	}
}

// TestLockGateIDsMatchTheBindings checks the gate's method ids against the
// ones the generated bindings call by, so a change in how Wails names methods
// cannot leave the gate refusing the lock screen or letting mail through.
func TestLockGateIDsMatchTheBindings(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "frontend", "bindings", "github.com", "peltonapp", "Pelton", "internal", "desktop", "app.ts"))
	if err != nil {
		t.Skipf("generated bindings not found: %v", err)
	}
	gate := newLockGate(func() bool { return true })
	re := regexp.MustCompile(`export function (\w+)\([^)]*\)[^{]*\{\s*return \$Call\.ByID\((\d+)`)
	matches := re.FindAllStringSubmatch(string(raw), -1)
	if len(matches) < 100 {
		t.Fatalf("found %d bound methods in the bindings, expected the whole App", len(matches))
	}
	for _, m := range matches {
		id, err := strconv.ParseUint(m[2], 10, 32)
		if err != nil {
			t.Fatal(err)
		}
		if got := gate.methods[uint32(id)]; got != m[1] {
			t.Errorf("binding %s has id %d, which the gate knows as %q", m[1], id, got)
		}
	}
}

func TestLockAllowedMethodsExist(t *testing.T) {
	gate := newLockGate(func() bool { return true })
	known := map[string]bool{}
	for _, name := range gate.methods {
		known[name] = true
	}
	for name := range lockAllowed {
		if !known[name] {
			t.Errorf("lockAllowed names %q, which App does not have", name)
		}
	}
}

func TestEnableWithPassphraseAndUnlock(t *testing.T) {
	a := newLockTestApp(t, nil)
	res, err := a.EnableAppLock("abc", "")
	if err != nil || res.OK || res.Reason != "short" {
		t.Fatalf("short passphrase: %+v %v, want refused as short", res, err)
	}
	res, err = a.EnableAppLock("long enough", "")
	if err != nil || !res.OK {
		t.Fatalf("enable: %+v %v", res, err)
	}
	stored := a.stringSetting(settingAppLockPassphrase, "")
	if stored == "" || strings.Contains(stored, "long enough") {
		t.Fatalf("stored passphrase %q, want a hash without the passphrase", stored)
	}
	state := a.GetLockState()
	if !state.Enabled || state.Locked || state.Method != lockMethodPassphrase {
		t.Fatalf("state after enabling = %+v", state)
	}
	if res, _ := a.EnableAppLock("another one", ""); res.OK || res.Reason != "on" {
		t.Fatalf("enabling again replaced the passphrase: %+v", res)
	}

	a.LockNow()
	if !a.locked() {
		t.Fatal("LockNow did not lock")
	}
	if res := a.UnlockWithSystem("unlock"); res.OK {
		t.Fatal("the system prompt unlocked a passphrase lock")
	}
	if res := a.UnlockWithPassphrase("wrong"); res.OK || res.Reason != "wrong" {
		t.Fatalf("wrong passphrase: %+v", res)
	}
	if !a.locked() {
		t.Fatal("a wrong passphrase unlocked")
	}
	if res := a.UnlockWithPassphrase("long enough"); !res.OK {
		t.Fatalf("right passphrase: %+v", res)
	}
	if a.locked() {
		t.Fatal("still locked after the right passphrase")
	}
}

func TestPassphraseBackoffSurvivesRestart(t *testing.T) {
	a := newLockTestApp(t, nil)
	if res, err := a.EnableAppLock("long enough", ""); err != nil || !res.OK {
		t.Fatalf("enable: %+v %v", res, err)
	}
	a.LockNow()
	for i := range 3 {
		if res := a.UnlockWithPassphrase("nope"); res.Reason != "wrong" || res.RetryAfterMs != 0 {
			t.Fatalf("attempt %d: %+v, want wrong with no wait", i+1, res)
		}
	}
	res := a.UnlockWithPassphrase("nope")
	if res.Reason != "wrong" || res.RetryAfterMs != (5*time.Second).Milliseconds() {
		t.Fatalf("fourth attempt: %+v, want a 5s wait", res)
	}
	if res := a.UnlockWithPassphrase("long enough"); res.OK || res.Reason != "wait" {
		t.Fatalf("right passphrase during the wait: %+v, want refused with wait", res)
	}
	if got := a.intSetting(settingAppLockFailures, 0); got != 4 {
		t.Fatalf("stored failures = %d, want 4", got)
	}

	// a relaunch reads the count back and keeps the wait.
	b := &App{ctx: a.ctx, store: a.store, log: a.log, lock: applock.New(nil)}
	b.lock.RestoreFailures(b.intSetting(settingAppLockFailures, 0))
	b.applyLockSettings()
	b.LockNow()
	if res := b.UnlockWithPassphrase("long enough"); res.OK || res.Reason != "wait" {
		t.Fatalf("after relaunch: %+v, want refused with wait", res)
	}
}

func TestSystemLock(t *testing.T) {
	auth := &fakeAuth{present: true, pass: false}
	a := newLockTestApp(t, auth)
	if res, _ := a.EnableAppLock("", "turn on"); res.OK || res.Reason != "cancelled" {
		t.Fatalf("enable with a failed prompt: %+v, want cancelled", res)
	}
	if a.lock.Enabled() {
		t.Fatal("the lock went on although the prompt failed")
	}
	auth.pass = true
	if res, _ := a.EnableAppLock("", "turn on"); !res.OK {
		t.Fatalf("enable: %+v", res)
	}
	if got := a.stringSetting(settingAppLockPassphrase, ""); got != "" {
		t.Fatalf("system lock stored a passphrase hash %q", got)
	}
	a.LockNow()
	auth.pass = false
	if res := a.UnlockWithSystem("unlock"); res.OK || !a.locked() {
		t.Fatalf("failed prompt: %+v, locked=%v", res, a.locked())
	}
	if res := a.UnlockWithPassphrase(""); res.OK {
		t.Fatal("an empty passphrase opened a system lock")
	}
	auth.pass = true
	if res := a.UnlockWithSystem("unlock"); !res.OK || a.locked() {
		t.Fatalf("passed prompt: %+v, locked=%v", res, a.locked())
	}
	if auth.reasons[len(auth.reasons)-1] != "unlock" {
		t.Fatalf("prompt reason = %q, want the one given", auth.reasons[len(auth.reasons)-1])
	}
}

func TestSystemLockWithoutAPromptNeedsAPassphrase(t *testing.T) {
	a := newLockTestApp(t, &fakeAuth{present: false})
	if res, _ := a.EnableAppLock("", "turn on"); res.OK || res.Reason != "short" {
		t.Fatalf("enable without a prompt or passphrase: %+v, want refused", res)
	}
	if a.GetLockState().SystemAvailable {
		t.Fatal("reports a system prompt that is not there")
	}
}

func TestDisableAndChangeNeedAuthentication(t *testing.T) {
	a := newLockTestApp(t, nil)
	if res, err := a.EnableAppLock("first secret", ""); err != nil || !res.OK {
		t.Fatalf("enable: %+v %v", res, err)
	}
	if res, _ := a.ChangeLockPassphrase("wrong", "second secret"); res.OK || res.Reason != "wrong" {
		t.Fatalf("change with a wrong passphrase: %+v", res)
	}
	if res, _ := a.ChangeLockPassphrase("first secret", "short"); res.OK || res.Reason != "short" {
		t.Fatalf("change to a short passphrase: %+v", res)
	}
	if res, _ := a.ChangeLockPassphrase("first secret", "second secret"); !res.OK {
		t.Fatalf("change: %+v", res)
	}
	if res, _ := a.DisableAppLock("first secret", ""); res.OK || !a.lock.Enabled() {
		t.Fatalf("disable with the old passphrase: %+v", res)
	}
	if res, _ := a.DisableAppLock("second secret", ""); !res.OK {
		t.Fatalf("disable: %+v", res)
	}
	if a.lock.Enabled() || a.stringSetting(settingAppLockPassphrase, "") != "" {
		t.Fatal("the lock or its hash survived turning it off")
	}
	if res, _ := a.DisableAppLock("", ""); res.Reason != "off" {
		t.Fatalf("disable twice: %+v, want off", res)
	}
}

func TestSetSettingRefusesProtectedLockSettings(t *testing.T) {
	a := newLockTestApp(t, nil)
	if res, err := a.EnableAppLock("long enough", ""); err != nil || !res.OK {
		t.Fatalf("enable: %+v %v", res, err)
	}
	for key := range protectedLockSettings {
		if err := a.SetSetting(key, ""); err == nil {
			t.Errorf("SetSetting(%q) was allowed", key)
		}
	}
	if !a.lock.Enabled() {
		t.Fatal("SetSetting turned the lock off")
	}
	if err := a.SetSetting(settingAppLockIdle, "1"); err != nil {
		t.Fatalf("idle minutes: %v", err)
	}
	prefs, err := a.GetUIPrefs()
	if err != nil || !prefs.AppLock || prefs.AppLockIdleMinutes != 1 {
		t.Fatalf("prefs = %+v %v, want the lock on with 1 minute", prefs.AppLock, err)
	}
}

func TestNotificationsHiddenWhileLocked(t *testing.T) {
	a := newLockTestApp(t, nil)
	n := notification{title: "New mail", body: "Ada\nQuarterly numbers", messageID: 7}
	if got := a.maskForLock(n); got != n {
		t.Fatalf("unlocked notification changed: %+v", got)
	}
	if res, err := a.EnableAppLock("long enough", ""); err != nil || !res.OK {
		t.Fatalf("enable: %+v %v", res, err)
	}
	a.LockNow()
	got := a.maskForLock(n)
	if strings.Contains(got.title+got.body, "Ada") || strings.Contains(got.title+got.body, "Quarterly") || got.messageID != 0 {
		t.Fatalf("locked notification leaks: %+v", got)
	}
	if err := a.SetSetting(settingAppLockHideNotifications, "false"); err != nil {
		t.Fatal(err)
	}
	if got := a.maskForLock(n); got != n {
		t.Fatalf("with hiding off the notification changed: %+v", got)
	}
}

func TestMailtoWaitsWhileLocked(t *testing.T) {
	a := newLockTestApp(t, nil)
	if res, err := a.EnableAppLock("long enough", ""); err != nil || !res.OK {
		t.Fatalf("enable: %+v %v", res, err)
	}
	a.LockNow()
	a.deliverMailto("mailto:ada@example.com")
	if got := a.ConsumePendingMailto(); !got.Present {
		t.Fatal("a mailto delivered while locked was dropped")
	}
}
