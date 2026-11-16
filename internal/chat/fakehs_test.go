package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeHomeserver is an in-process stand-in for a Matrix homeserver: enough
// of the client-server API for sign-in, syncing, sending, pagination, media
// and this device's keys. Tests queue sync responses and read back what the
// client sent. Nothing leaves the process.
type fakeHomeserver struct {
	t      *testing.T
	srv    *httptest.Server
	userID string
	device string
	token  string

	mu         sync.Mutex
	syncQueue  []map[string]any
	syncWake   chan struct{}
	batch      int
	sent       []sentEvent
	eventSeq   int
	deviceKeys json.RawMessage
	media      map[string][]byte
	mediaSeq   int
	pages      map[string]map[string]any
	members    map[string][]map[string]any
	accountDat map[string]json.RawMessage
	readMarks  []map[string]string
	typing     []bool
	logouts    int
	unknown    []string
	passwords  map[string]string
}

// sentEvent is one event the client put into a room.
type sentEvent struct {
	Room    string
	Type    string
	EventID string
	Content map[string]any
}

func newFakeHomeserver(t *testing.T, tls bool) *fakeHomeserver {
	t.Helper()
	f := &fakeHomeserver{
		t:          t,
		token:      "secret-access-token-for-the-test",
		device:     "PELTONDEV",
		syncWake:   make(chan struct{}, 1),
		media:      map[string][]byte{},
		pages:      map[string]map[string]any{},
		members:    map[string][]map[string]any{},
		accountDat: map[string]json.RawMessage{},
		passwords:  map[string]string{"alice": "hunter2"},
	}
	if tls {
		f.srv = httptest.NewTLSServer(http.HandlerFunc(f.serve))
	} else {
		f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	}
	f.userID = "@alice:" + f.host()
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeHomeserver) host() string {
	return strings.TrimPrefix(strings.TrimPrefix(f.srv.URL, "https://"), "http://")
}

// queueSync adds a sync response; next_batch is filled in.
func (f *fakeHomeserver) queueSync(resp map[string]any) {
	f.mu.Lock()
	f.syncQueue = append(f.syncQueue, resp)
	f.mu.Unlock()
	select {
	case f.syncWake <- struct{}{}:
	default:
	}
}

func (f *fakeHomeserver) sentEvents() []sentEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]sentEvent(nil), f.sent...)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func notFound(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotFound, map[string]string{"errcode": "M_NOT_FOUND", "error": "not found"})
}

func (f *fakeHomeserver) serve(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	switch {
	case p == "/.well-known/matrix/client":
		writeJSON(w, 200, map[string]any{"m.homeserver": map[string]string{"base_url": f.srv.URL}})
		return
	case p == "/_matrix/client/versions":
		writeJSON(w, 200, map[string]any{"versions": []string{"v1.11", "v1.12"}})
		return
	case p == "/_matrix/client/v3/login" && r.Method == http.MethodGet:
		writeJSON(w, 200, map[string]any{"flows": []map[string]any{
			{"type": "m.login.password"},
			{"type": "m.login.sso", "identity_providers": []map[string]string{{"id": "oidc-example", "name": "Example ID"}}},
			{"type": "m.login.token"},
		}})
		return
	case p == "/_matrix/client/v3/login" && r.Method == http.MethodPost:
		f.login(w, r)
		return
	}

	if r.Header.Get("Authorization") != "Bearer "+f.token {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"errcode": "M_UNKNOWN_TOKEN", "error": "unknown token"})
		return
	}

	parts := strings.Split(strings.TrimPrefix(p, "/_matrix/"), "/")
	switch {
	case p == "/_matrix/client/v3/sync":
		f.sync(w, r)
	case strings.HasPrefix(p, "/_matrix/client/v3/user/") && strings.HasSuffix(p, "/filter"):
		writeJSON(w, 200, map[string]string{"filter_id": "f1"})
	case p == "/_matrix/client/v3/keys/upload":
		var body struct {
			DeviceKeys json.RawMessage `json:"device_keys"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		if len(body.DeviceKeys) > 0 && string(body.DeviceKeys) != "null" {
			f.deviceKeys = body.DeviceKeys
		}
		f.mu.Unlock()
		writeJSON(w, 200, map[string]any{"one_time_key_counts": map[string]int{"signed_curve25519": 50}})
	case p == "/_matrix/client/v3/keys/query":
		f.mu.Lock()
		keys := map[string]any{}
		if f.deviceKeys != nil {
			keys[f.userID] = map[string]json.RawMessage{f.device: f.deviceKeys}
		}
		f.mu.Unlock()
		writeJSON(w, 200, map[string]any{"device_keys": keys})
	case p == "/_matrix/client/v3/keys/claim":
		writeJSON(w, 200, map[string]any{"one_time_keys": map[string]any{}})
	case len(parts) >= 3 && parts[1] == "v3" && parts[2] == "sendToDevice":
		writeJSON(w, 200, map[string]any{})
	case len(parts) >= 6 && parts[1] == "v3" && parts[2] == "rooms" && parts[4] == "send":
		f.send(w, r, parts[3], parts[5])
	case len(parts) >= 5 && parts[1] == "v3" && parts[2] == "rooms" && parts[4] == "messages":
		f.mu.Lock()
		page, ok := f.pages[parts[3]+"|"+r.URL.Query().Get("from")]
		f.mu.Unlock()
		if !ok {
			writeJSON(w, 200, map[string]any{"chunk": []any{}, "start": r.URL.Query().Get("from")})
			return
		}
		writeJSON(w, 200, page)
	case len(parts) >= 5 && parts[1] == "v3" && parts[2] == "rooms" && parts[4] == "members":
		f.mu.Lock()
		chunk := f.members[parts[3]]
		f.mu.Unlock()
		writeJSON(w, 200, map[string]any{"chunk": chunk})
	case len(parts) >= 5 && parts[1] == "v3" && parts[2] == "rooms" && parts[4] == "read_markers":
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.readMarks = append(f.readMarks, body)
		f.mu.Unlock()
		writeJSON(w, 200, map[string]any{})
	case len(parts) >= 5 && parts[1] == "v3" && parts[2] == "rooms" && parts[4] == "typing":
		var body struct {
			Typing bool `json:"typing"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.typing = append(f.typing, body.Typing)
		f.mu.Unlock()
		writeJSON(w, 200, map[string]any{})
	case len(parts) >= 6 && parts[1] == "v3" && parts[2] == "rooms" && parts[4] == "redact":
		f.send(w, r, parts[3], "m.room.redaction")
	case p == "/_matrix/client/v3/createRoom":
		writeJSON(w, 200, map[string]string{"room_id": "!created:" + f.host()})
	case len(parts) >= 5 && parts[1] == "v3" && parts[2] == "user" && parts[4] == "account_data":
		f.accountData(w, r, parts[5])
	case p == "/_matrix/client/v1/media/config":
		writeJSON(w, 200, map[string]int64{"m.upload.size": 1 << 20})
	case p == "/_matrix/media/v3/upload":
		data, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.mediaSeq++
		mid := fmt.Sprintf("m%d", f.mediaSeq)
		f.media[mid] = data
		f.mu.Unlock()
		writeJSON(w, 200, map[string]string{"content_uri": "mxc://" + f.host() + "/" + mid})
	case len(parts) >= 6 && parts[1] == "v1" && parts[2] == "media" && (parts[3] == "download" || parts[3] == "thumbnail"):
		f.mu.Lock()
		data, ok := f.media[parts[5]]
		f.mu.Unlock()
		if !ok {
			notFound(w)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(data)
	case p == "/_matrix/client/v3/logout":
		f.mu.Lock()
		f.logouts++
		f.mu.Unlock()
		writeJSON(w, 200, map[string]any{})
	case strings.HasPrefix(p, "/_matrix/client/v3/profile/"):
		if strings.HasSuffix(p, "/displayname") {
			writeJSON(w, 200, map[string]string{"displayname": "Alice"})
			return
		}
		notFound(w)
	default:
		f.mu.Lock()
		f.unknown = append(f.unknown, r.Method+" "+p)
		f.mu.Unlock()
		notFound(w)
	}
}

func (f *fakeHomeserver) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Type       string `json:"type"`
		Password   string `json:"password"`
		Token      string `json:"token"`
		Identifier struct {
			User string `json:"user"`
		} `json:"identifier"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	ok := false
	switch req.Type {
	case "m.login.password":
		ok = f.passwords[req.Identifier.User] == req.Password && req.Password != ""
	case "m.login.token":
		ok = req.Token == "login-token-from-sso"
	}
	if !ok {
		writeJSON(w, http.StatusForbidden, map[string]string{"errcode": "M_FORBIDDEN", "error": "Invalid username or password"})
		return
	}
	writeJSON(w, 200, map[string]string{"access_token": f.token, "device_id": f.device, "user_id": f.userID})
}

func (f *fakeHomeserver) accountData(w http.ResponseWriter, r *http.Request, name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Method == http.MethodPut {
		data, _ := io.ReadAll(r.Body)
		f.accountDat[name] = data
		writeJSON(w, 200, map[string]any{})
		return
	}
	data, ok := f.accountDat[name]
	if !ok {
		notFound(w)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

func (f *fakeHomeserver) send(w http.ResponseWriter, r *http.Request, room, evtType string) {
	var content map[string]any
	_ = json.NewDecoder(r.Body).Decode(&content)
	f.mu.Lock()
	f.eventSeq++
	eid := fmt.Sprintf("$sent%d", f.eventSeq)
	f.sent = append(f.sent, sentEvent{Room: room, Type: evtType, EventID: eid, Content: content})
	f.mu.Unlock()
	writeJSON(w, 200, map[string]string{"event_id": eid})
}

// sync answers with the next queued response, or holds the request open the
// way a real long poll does until one is queued or the client gives up.
func (f *fakeHomeserver) sync(w http.ResponseWriter, r *http.Request) {
	timeout := r.URL.Query().Get("timeout")
	deadline := time.After(2 * time.Second)
	for {
		f.mu.Lock()
		if len(f.syncQueue) > 0 {
			resp := f.syncQueue[0]
			f.syncQueue = f.syncQueue[1:]
			f.batch++
			resp["next_batch"] = fmt.Sprintf("s%d", f.batch)
			f.mu.Unlock()
			writeJSON(w, 200, resp)
			return
		}
		f.mu.Unlock()
		if timeout == "" || timeout == "0" {
			f.mu.Lock()
			f.batch++
			nb := fmt.Sprintf("s%d", f.batch)
			f.mu.Unlock()
			writeJSON(w, 200, map[string]any{"next_batch": nb})
			return
		}
		select {
		case <-f.syncWake:
		case <-deadline:
			f.mu.Lock()
			f.batch++
			nb := fmt.Sprintf("s%d", f.batch)
			f.mu.Unlock()
			writeJSON(w, 200, map[string]any{"next_batch": nb})
			return
		case <-r.Context().Done():
			return
		}
	}
}

// memSecrets is a SecretStore in memory, standing in for the OS credential
// store.
type memSecrets struct {
	mu   sync.Mutex
	data map[string]Secret
}

func newMemSecrets() *memSecrets { return &memSecrets{data: map[string]Secret{}} }

func (m *memSecrets) Load(key string) (Secret, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.data[key]
	if !ok {
		return Secret{}, ErrNoSecret
	}
	return s, nil
}

func (m *memSecrets) Store(key string, s Secret) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = s
	return nil
}

func (m *memSecrets) Delete(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	return nil
}

// recorder collects the events a Manager emits.
type recorder struct {
	mu     sync.Mutex
	events []string
	notes  []Notification
}

func (r *recorder) emit(name string, _ any) {
	r.mu.Lock()
	r.events = append(r.events, name)
	r.mu.Unlock()
}

func (r *recorder) notify(list []Notification) {
	r.mu.Lock()
	r.notes = append(r.notes, list...)
	r.mu.Unlock()
}

func (r *recorder) notifications() []Notification {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Notification(nil), r.notes...)
}

// newTestManager returns a Manager wired to the fake server.
func newTestManager(t *testing.T, f *fakeHomeserver) (*Manager, *memSecrets, *recorder) {
	t.Helper()
	secrets := newMemSecrets()
	rec := &recorder{}
	m := New(Options{
		Dir:        t.TempDir(),
		SecretKey:  "test",
		Secrets:    secrets,
		HTTPClient: func() *http.Client { return f.srv.Client() },
		Emit:       rec.emit,
		Notify:     rec.notify,
		DeviceName: "Pelton test",
	})
	t.Cleanup(m.Stop)
	return m, secrets, rec
}

// signIn signs the manager in with the fake's password and waits for the
// first sync.
func signIn(t *testing.T, m *Manager, f *fakeHomeserver) {
	t.Helper()
	if err := m.LoginPassword(context.Background(), f.srv.URL, "alice", "hunter2"); err != nil {
		t.Fatalf("LoginPassword: %v", err)
	}
	waitFor(t, "first sync", func() bool { return m.Status().State == StateOnline })
}

// waitFor polls cond until it holds or the test times out.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// ev builds a timeline event.
func ev(id, sender, typ string, ts int64, content map[string]any) map[string]any {
	return map[string]any{"event_id": id, "sender": sender, "type": typ, "origin_server_ts": ts, "content": content}
}

// stateEv builds a state event.
func stateEv(id, sender, typ, key string, content map[string]any) map[string]any {
	e := ev(id, sender, typ, 1, content)
	e["state_key"] = key
	return e
}
