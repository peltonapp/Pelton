package chat

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDiscoverReadsWellKnownAndSignInMethods(t *testing.T) {
	f := newFakeHomeserver(t, true)
	m, _, _ := newTestManager(t, f)

	srv, err := m.Discover(context.Background(), "@alice:"+f.host())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if srv.Homeserver != f.srv.URL || srv.User != "alice" || srv.ServerName != f.host() {
		t.Fatalf("server = %+v", srv)
	}
	if !srv.Password || !srv.SSO || !srv.Token {
		t.Fatalf("sign-in methods = %+v", srv)
	}
	if len(srv.Providers) != 1 || srv.Providers[0].ID != "oidc-example" || srv.Providers[0].Name != "Example ID" {
		t.Fatalf("providers = %+v", srv.Providers)
	}
}

func TestSplitInput(t *testing.T) {
	cases := []struct {
		in, server, user, base string
		bad                    bool
	}{
		{in: "@bob:example.org", server: "example.org", user: "bob"},
		{in: "  matrix.org ", server: "matrix.org"},
		{in: "https://chat.example.org/", server: "chat.example.org", base: "https://chat.example.org"},
		{in: "@nobody", bad: true},
		{in: "ftp://example.org", bad: true},
		{in: "", bad: true},
	}
	for _, c := range cases {
		server, user, base, err := splitInput(c.in)
		if c.bad {
			if err == nil {
				t.Errorf("splitInput(%q) accepted it", c.in)
			}
			continue
		}
		if err != nil || server != c.server || user != c.user || base != c.base {
			t.Errorf("splitInput(%q) = %q %q %q %v", c.in, server, user, base, err)
		}
	}
}

func TestPasswordLoginKeepsSecretsOutOfTheDatabase(t *testing.T) {
	f := newFakeHomeserver(t, true)
	m, secrets, _ := newTestManager(t, f)

	if err := m.LoginPassword(context.Background(), f.srv.URL, "alice", "wrong"); err == nil {
		t.Fatal("a wrong password signed in")
	}
	signIn(t, m, f)

	st := m.Status()
	if st.UserID != f.userID || st.DeviceID != f.device {
		t.Fatalf("status = %+v", st)
	}
	secret, err := secrets.Load("test")
	if err != nil || secret.AccessToken != f.token || len(secret.PickleKey) < 32 {
		t.Fatalf("stored secret = %+v, %v", secret, err)
	}
	m.Stop()
	for _, name := range []string{"chat.db", "chat.db-wal"} {
		raw, err := os.ReadFile(filepath.Join(m.opts.Dir, name))
		if err != nil {
			continue
		}
		if bytes.Contains(raw, []byte(f.token)) || bytes.Contains(raw, []byte(secret.PickleKey)) {
			t.Fatalf("%s holds a secret", name)
		}
	}
}

func TestStartResumesAStoredSignIn(t *testing.T) {
	f := newFakeHomeserver(t, true)
	m, secrets, _ := newTestManager(t, f)
	signIn(t, m, f)
	m.Stop()

	again := New(Options{Dir: m.opts.Dir, SecretKey: "test", Secrets: secrets, HTTPClient: func() *http.Client { return f.srv.Client() }})
	t.Cleanup(again.Stop)
	if err := again.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitFor(t, "resumed sync", func() bool { return again.Status().State == StateOnline })

	// without the stored secret there is nothing to resume.
	again.Stop()
	_ = secrets.Delete("test")
	third := New(Options{Dir: m.opts.Dir, SecretKey: "test", Secrets: secrets, HTTPClient: func() *http.Client { return f.srv.Client() }})
	if err := third.Start(context.Background()); err != nil {
		t.Fatalf("Start without secret: %v", err)
	}
	if third.Status().State != StateSignedOut {
		t.Fatalf("state without a secret = %s", third.Status().State)
	}
}

func TestStartWithoutAnAccountDoesNothing(t *testing.T) {
	m := New(Options{Dir: filepath.Join(t.TempDir(), "never"), Secrets: newMemSecrets(), HTTPClient: func() *http.Client {
		t.Fatal("a client was made with no account")
		return nil
	}})
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(m.opts.Dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("Start created the data directory with no account")
	}
	if _, err := m.Rooms(context.Background()); !errors.Is(err, ErrNotSignedIn) {
		t.Fatalf("Rooms while signed out: %v", err)
	}
}

// roomSync is a sync with one joined room holding a few messages, an invite
// and a direct chat.
func roomSync(f *fakeHomeserver) map[string]any {
	bob := "@bob:" + f.host()
	carol := "@carol:" + f.host()
	return map[string]any{
		"account_data": map[string]any{"events": []any{
			map[string]any{"type": "m.direct", "content": map[string]any{bob: []string{"!dm:x"}}},
		}},
		"rooms": map[string]any{
			"join": map[string]any{
				"!garden:x": map[string]any{
					"summary": map[string]any{"m.joined_member_count": 2},
					"state": map[string]any{"events": []any{
						stateEv("$name", bob, "m.room.name", "", map[string]any{"name": "Garden"}),
						stateEv("$topic", bob, "m.room.topic", "", map[string]any{"topic": "Plants"}),
						stateEv("$mb", bob, "m.room.member", bob, map[string]any{"membership": "join", "displayname": "Bob"}),
						stateEv("$ma", f.userID, "m.room.member", f.userID, map[string]any{"membership": "join", "displayname": "Alice"}),
					}},
					"timeline": map[string]any{"limited": true, "prev_batch": "p1", "events": []any{
						ev("$1", bob, "m.room.message", 1000, map[string]any{"msgtype": "m.text", "body": "hello alice"}),
						ev("$2", bob, "m.room.message", 2000, map[string]any{"msgtype": "m.text", "body": "**bold**", "format": "org.matrix.custom.html", "formatted_body": "<strong>bold</strong>"}),
						ev("$3", f.userID, "m.room.message", 3000, map[string]any{"msgtype": "m.text", "body": "hi"}),
						ev("$r1", bob, "m.reaction", 3100, map[string]any{"m.relates_to": map[string]any{"rel_type": "m.annotation", "event_id": "$3", "key": "👍"}}),
						ev("$e1", bob, "m.room.message", 3200, map[string]any{"msgtype": "m.text", "body": "* bold edited",
							"m.new_content":  map[string]any{"msgtype": "m.text", "body": "bold edited"},
							"m.relates_to": map[string]any{"rel_type": "m.replace", "event_id": "$2"}}),
						ev("$4", bob, "m.room.message", 3300, map[string]any{"msgtype": "m.text", "body": "> <@alice> hi\n\nanswer",
							"m.relates_to": map[string]any{"m.in_reply_to": map[string]any{"event_id": "$3"}}}),
					}},
					"ephemeral": map[string]any{"events": []any{
						map[string]any{"type": "m.receipt", "content": map[string]any{"$3": map[string]any{"m.read": map[string]any{bob: map[string]any{"ts": 3500}}}}},
						map[string]any{"type": "m.typing", "content": map[string]any{"user_ids": []string{bob}}},
					}},
					"account_data": map[string]any{"events": []any{
						map[string]any{"type": "m.fully_read", "content": map[string]any{"event_id": "$2"}},
						map[string]any{"type": "m.tag", "content": map[string]any{"tags": map[string]any{"m.favourite": map[string]any{"order": 0.5}}}},
					}},
					"unread_notifications": map[string]any{"notification_count": 2, "highlight_count": 1},
				},
				"!dm:x": map[string]any{
					"summary": map[string]any{"m.heroes": []string{bob}},
					"state": map[string]any{"events": []any{
						stateEv("$dmb", bob, "m.room.member", bob, map[string]any{"membership": "join", "displayname": "Bob"}),
					}},
					"timeline": map[string]any{"events": []any{
						ev("$d1", bob, "m.room.message", 500, map[string]any{"msgtype": "m.text", "body": "psst"}),
					}},
				},
			},
			"invite": map[string]any{
				"!party:x": map[string]any{"invite_state": map[string]any{"events": []any{
					stateEv("", carol, "m.room.name", "", map[string]any{"name": "Party"}),
					stateEv("", carol, "m.room.member", carol, map[string]any{"membership": "join", "displayname": "Carol"}),
					stateEv("", carol, "m.room.member", f.userID, map[string]any{"membership": "invite"}),
				}}},
			},
		},
	}
}

func TestSyncFillsRoomsAndTimeline(t *testing.T) {
	ctx := context.Background()
	f := newFakeHomeserver(t, true)
	m, _, _ := newTestManager(t, f)
	signIn(t, m, f)
	f.queueSync(roomSync(f))

	var rooms []Room
	waitFor(t, "rooms", func() bool {
		rooms, _ = m.Rooms(ctx)
		return len(rooms) == 3
	})
	byID := map[string]Room{}
	for _, r := range rooms {
		byID[r.ID] = r
	}
	garden := byID["!garden:x"]
	if garden.Name != "Garden" || garden.Topic != "Plants" || garden.Kind != "room" || !garden.Favourite {
		t.Errorf("garden = %+v", garden)
	}
	if garden.Unread != 2 || garden.Highlight != 1 || garden.FullyRead != "$2" {
		t.Errorf("garden counts = %+v", garden)
	}
	if garden.Preview != "answer" || garden.PreviewSender != "Bob" {
		t.Errorf("garden preview = %q from %q", garden.Preview, garden.PreviewSender)
	}
	dm := byID["!dm:x"]
	if dm.Kind != "direct" || dm.Name != "Bob" || dm.DirectUser != "@bob:"+f.host() {
		t.Errorf("dm = %+v", dm)
	}
	party := byID["!party:x"]
	if party.Membership != "invite" || party.Name != "Party" || party.InviterName != "Carol" {
		t.Errorf("invite = %+v", party)
	}
	if rooms[0].ID != "!garden:x" {
		t.Errorf("rooms are not newest first: %s", rooms[0].ID)
	}

	page, err := m.Timeline(ctx, "!garden:x", "", 50)
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	var ids []string
	for _, msg := range page.Messages {
		ids = append(ids, msg.ID)
	}
	if strings.Join(ids, ",") != "$1,$2,$3,$4" {
		t.Fatalf("timeline ids = %v", ids)
	}
	first, edited, mine, reply := page.Messages[0], page.Messages[1], page.Messages[2], page.Messages[3]
	if !first.Mentioned || first.SenderName != "Bob" {
		t.Errorf("first = %+v", first)
	}
	if !edited.Edited || edited.Body != "bold edited" || edited.HTML != "" {
		t.Errorf("edit not applied: %+v", edited)
	}
	if !mine.Own || len(mine.Reactions) != 1 || mine.Reactions[0].Key != "👍" || mine.Reactions[0].Count != 1 {
		t.Errorf("reactions = %+v", mine.Reactions)
	}
	if len(mine.ReadBy) != 1 || mine.ReadBy[0].Name != "Bob" {
		t.Errorf("read by = %+v", mine.ReadBy)
	}
	if reply.ReplyTo == nil || reply.ReplyTo.ID != "$3" || reply.ReplyTo.Body != "hi" || reply.Body != "answer" {
		t.Errorf("reply = %+v / %+v", reply, reply.ReplyTo)
	}
	if typing := m.Typists(ctx, "!garden:x"); len(typing) != 1 || typing[0].Name != "Bob" {
		t.Errorf("typing = %+v", typing)
	}
}

func TestPaginationFetchesOlderMessages(t *testing.T) {
	ctx := context.Background()
	f := newFakeHomeserver(t, true)
	m, _, _ := newTestManager(t, f)
	signIn(t, m, f)
	bob := "@bob:" + f.host()
	f.mu.Lock()
	f.pages["!garden:x|p1"] = map[string]any{"start": "p1", "end": "p2", "chunk": []any{
		ev("$0b", bob, "m.room.message", 900, map[string]any{"msgtype": "m.text", "body": "older"}),
		ev("$0a", bob, "m.room.message", 800, map[string]any{"msgtype": "m.text", "body": "oldest"}),
	}}
	f.pages["!garden:x|p2"] = map[string]any{"start": "p2", "chunk": []any{
		stateEv("$create", bob, "m.room.create", "", map[string]any{}),
	}}
	f.mu.Unlock()
	f.queueSync(roomSync(f))
	waitFor(t, "rooms", func() bool { r, _ := m.Rooms(ctx); return len(r) == 3 })

	page, err := m.Timeline(ctx, "!garden:x", "", 3)
	if err != nil || len(page.Messages) != 3 || !page.HasMore {
		t.Fatalf("first page = %+v, %v", page, err)
	}
	older, err := m.Timeline(ctx, "!garden:x", page.Messages[0].ID, 10)
	if err != nil {
		t.Fatalf("older page: %v", err)
	}
	var bodies []string
	for _, msg := range older.Messages {
		if msg.Kind == "state" {
			bodies = append(bodies, "state:"+msg.State.Kind)
			continue
		}
		bodies = append(bodies, msg.Body)
	}
	if strings.Join(bodies, "|") != "state:created|oldest|older|hello alice" {
		t.Fatalf("older bodies = %v", bodies)
	}
	if older.HasMore {
		t.Fatal("the start of the room still reports more")
	}
}

func TestSendMarkdownReplyEditReactAndRead(t *testing.T) {
	ctx := context.Background()
	f := newFakeHomeserver(t, true)
	m, _, _ := newTestManager(t, f)
	signIn(t, m, f)
	f.queueSync(roomSync(f))
	waitFor(t, "rooms", func() bool { r, _ := m.Rooms(ctx); return len(r) == 3 })

	bob := "@bob:" + f.host()
	if _, err := m.Send(ctx, SendRequest{RoomID: "!garden:x", Text: "hi @Bob, *look*", Markdown: true, ReplyTo: "$1",
		Mentions: []Mention{{UserID: bob, Name: "Bob"}}}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if _, err := m.Send(ctx, SendRequest{RoomID: "!garden:x", Text: "fixed", Edit: "$3"}); err != nil {
		t.Fatalf("edit: %v", err)
	}
	if _, err := m.Send(ctx, SendRequest{RoomID: "!garden:x", Text: "not mine", Edit: "$1"}); err == nil {
		t.Fatal("edited someone else's message")
	}
	if _, err := m.React(ctx, "!garden:x", "$1", "🎉"); err != nil {
		t.Fatalf("React: %v", err)
	}
	if err := m.MarkRead(ctx, "!garden:x", "$4"); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}

	sent := f.sentEvents()
	if len(sent) != 3 {
		t.Fatalf("sent %d events: %+v", len(sent), sent)
	}
	msg := sent[0].Content
	html, _ := msg["formatted_body"].(string)
	if !strings.Contains(html, "<em>look</em>") || !strings.Contains(html, "https://matrix.to/#/"+bob) {
		t.Errorf("formatted body = %q", html)
	}
	if msg["body"] != "hi @Bob, *look*" {
		t.Errorf("plain body = %q", msg["body"])
	}
	rel, _ := msg["m.relates_to"].(map[string]any)
	if reply, _ := rel["m.in_reply_to"].(map[string]any); reply["event_id"] != "$1" {
		t.Errorf("reply relation = %+v", rel)
	}
	mentions, _ := msg["m.mentions"].(map[string]any)
	if ids, _ := mentions["user_ids"].([]any); len(ids) != 1 || ids[0] != bob {
		t.Errorf("mentions = %+v", mentions)
	}
	edit := sent[1].Content
	if nc, _ := edit["m.new_content"].(map[string]any); nc["body"] != "fixed" {
		t.Errorf("edit = %+v", edit)
	}
	if sent[2].Type != "m.reaction" {
		t.Errorf("reaction type = %s", sent[2].Type)
	}
	f.mu.Lock()
	marks := f.readMarks
	f.mu.Unlock()
	if len(marks) != 1 || marks[0]["m.fully_read"] != "$4" || marks[0]["m.read"] != "$4" {
		t.Errorf("read markers = %+v", marks)
	}
	if r, _ := m.Room(ctx, "!garden:x"); r.Unread != 0 || r.FullyRead != "$4" {
		t.Errorf("room after read = %+v", r)
	}
}

func TestPrivateReceiptsAndNoTypingWhenTurnedOff(t *testing.T) {
	ctx := context.Background()
	f := newFakeHomeserver(t, true)
	m, _, _ := newTestManager(t, f)
	m.opts.Prefs = func() Prefs { return Prefs{} }
	signIn(t, m, f)
	f.queueSync(roomSync(f))
	waitFor(t, "rooms", func() bool { r, _ := m.Rooms(ctx); return len(r) == 3 })

	if err := m.MarkRead(ctx, "!garden:x", "$4"); err != nil {
		t.Fatal(err)
	}
	if err := m.Typing(ctx, "!garden:x", true); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.readMarks) != 1 || f.readMarks[0]["m.read"] != "" || f.readMarks[0]["m.read.private"] != "$4" {
		t.Errorf("read markers = %+v", f.readMarks)
	}
	if len(f.typing) != 0 {
		t.Errorf("typing was sent: %v", f.typing)
	}
}

func TestEncryptedRoomRoundTrip(t *testing.T) {
	ctx := context.Background()
	f := newFakeHomeserver(t, true)
	m, _, _ := newTestManager(t, f)
	signIn(t, m, f)
	f.mu.Lock()
	f.members["!secret:x"] = []map[string]any{stateEv("$m", f.userID, "m.room.member", f.userID, map[string]any{"membership": "join"})}
	f.mu.Unlock()
	f.queueSync(map[string]any{"rooms": map[string]any{"join": map[string]any{"!secret:x": map[string]any{
		"state": map[string]any{"events": []any{
			stateEv("$enc", f.userID, "m.room.encryption", "", map[string]any{"algorithm": "m.megolm.v1.aes-sha2"}),
			stateEv("$m", f.userID, "m.room.member", f.userID, map[string]any{"membership": "join"}),
		}},
	}}}})
	waitFor(t, "encrypted room", func() bool { r, _ := m.Room(ctx, "!secret:x"); return r.Encrypted })

	if _, err := m.Send(ctx, SendRequest{RoomID: "!secret:x", Text: "top secret"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	plain := []byte("a picture that is not really a picture")
	if _, err := m.SendFile(ctx, FileRequest{RoomID: "!secret:x", Name: "note.txt", Mime: "text/plain"}, plain, nil); err != nil {
		t.Fatalf("SendFile: %v", err)
	}
	sent := f.sentEvents()
	if len(sent) != 2 || sent[0].Type != "m.room.encrypted" || sent[1].Type != "m.room.encrypted" {
		t.Fatalf("sent = %+v", sent)
	}
	if strings.Contains(sent[0].Content["ciphertext"].(string), "top secret") {
		t.Fatal("the message went out in the clear")
	}
	f.mu.Lock()
	for _, data := range f.media {
		if bytes.Contains(data, plain) {
			f.mu.Unlock()
			t.Fatal("the file was uploaded in the clear")
		}
	}
	f.mu.Unlock()

	// the server echoes both back in the next sync.
	var timeline []any
	for i, s := range sent {
		timeline = append(timeline, ev(s.EventID, f.userID, s.Type, int64(5000+i), s.Content))
	}
	f.queueSync(map[string]any{"rooms": map[string]any{"join": map[string]any{"!secret:x": map[string]any{
		"timeline": map[string]any{"events": timeline},
	}}}})
	var page TimelinePage
	waitFor(t, "encrypted echo", func() bool {
		page, _ = m.Timeline(ctx, "!secret:x", "", 50)
		n := 0
		for _, msg := range page.Messages {
			if msg.Kind != "state" {
				n++
			}
		}
		return n == 2
	})
	var text, file *Message
	for i := range page.Messages {
		switch page.Messages[i].Kind {
		case "text":
			text = &page.Messages[i]
		case "file":
			file = &page.Messages[i]
		}
	}
	if text == nil || text.Body != "top secret" || !text.Encrypted {
		t.Fatalf("decrypted text = %+v", text)
	}
	if file == nil || file.Media == nil || !file.Media.Encrypted || file.Media.Name != "note.txt" {
		t.Fatalf("file message = %+v", file)
	}
	got, err := m.Media(ctx, "!secret:x", file.ID, false)
	if err != nil {
		t.Fatalf("Media: %v", err)
	}
	data, _ := base64.StdEncoding.DecodeString(got.Data)
	if !bytes.Equal(data, plain) {
		t.Fatalf("downloaded %q", data)
	}
	out := filepath.Join(t.TempDir(), "saved.txt")
	var progressed bool
	if err := m.SaveMedia(ctx, "!secret:x", file.ID, out, func(done, total int64) { progressed = true }); err != nil {
		t.Fatalf("SaveMedia: %v", err)
	}
	if saved, _ := os.ReadFile(out); !bytes.Equal(saved, plain) || !progressed {
		t.Fatalf("saved %q, progress %v", saved, progressed)
	}
}

func TestUploadLimitAndPlainMedia(t *testing.T) {
	ctx := context.Background()
	f := newFakeHomeserver(t, true)
	m, _, _ := newTestManager(t, f)
	signIn(t, m, f)
	f.queueSync(roomSync(f))
	waitFor(t, "rooms", func() bool { r, _ := m.Rooms(ctx); return len(r) == 3 })

	if limit, err := m.UploadLimit(ctx); err != nil || limit != 1<<20 {
		t.Fatalf("UploadLimit = %d, %v", limit, err)
	}
	if _, err := m.SendFile(ctx, FileRequest{RoomID: "!garden:x", Name: "big.bin"}, make([]byte, 2<<20), nil); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("an oversized file went out: %v", err)
	}
	if _, err := m.SendFile(ctx, FileRequest{RoomID: "!garden:x", Name: "../../etc/notes.txt", Mime: "text/plain"}, []byte("hello"), nil); err != nil {
		t.Fatalf("SendFile: %v", err)
	}
	sent := f.sentEvents()
	last := sent[len(sent)-1].Content
	if last["msgtype"] != "m.file" || last["body"] != "notes.txt" || last["url"] == nil {
		t.Fatalf("file content = %+v", last)
	}
}

func TestSignOutRemovesEverything(t *testing.T) {
	f := newFakeHomeserver(t, true)
	m, secrets, _ := newTestManager(t, f)
	signIn(t, m, f)
	if err := m.SignOut(context.Background()); err != nil {
		t.Fatalf("SignOut: %v", err)
	}
	if _, err := secrets.Load("test"); !errors.Is(err, ErrNoSecret) {
		t.Fatal("the secret survived sign-out")
	}
	if _, err := os.Stat(m.opts.Dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the chat data survived sign-out")
	}
	f.mu.Lock()
	logouts := f.logouts
	f.mu.Unlock()
	if logouts != 1 || m.Status().State != StateSignedOut {
		t.Fatalf("logouts = %d, state = %s", logouts, m.Status().State)
	}
}

func TestNewMessagesNotifyAfterTheFirstSync(t *testing.T) {
	ctx := context.Background()
	f := newFakeHomeserver(t, true)
	m, _, rec := newTestManager(t, f)
	signIn(t, m, f)
	f.queueSync(roomSync(f))
	waitFor(t, "rooms", func() bool { r, _ := m.Rooms(ctx); return len(r) == 3 })
	bob := "@bob:" + f.host()
	f.queueSync(map[string]any{"rooms": map[string]any{"join": map[string]any{
		"!dm:x":     map[string]any{"timeline": map[string]any{"events": []any{ev("$d2", bob, "m.room.message", 9000, map[string]any{"msgtype": "m.text", "body": "are you there"})}}},
		"!garden:x": map[string]any{"timeline": map[string]any{"events": []any{ev("$g9", bob, "m.room.message", 9001, map[string]any{"msgtype": "m.text", "body": "weather is nice"})}}},
	}}})
	latest := func() map[string]Notification {
		out := map[string]Notification{}
		for _, n := range rec.notifications() {
			if n.EventID == "$d2" || n.EventID == "$g9" {
				out[n.EventID] = n
			}
		}
		return out
	}
	waitFor(t, "notifications", func() bool { return len(latest()) == 2 })
	for _, n := range latest() {
		switch n.RoomID {
		case "!dm:x":
			if !n.Direct || n.Body != "are you there" || n.Sender != "Bob" {
				t.Errorf("dm notification = %+v", n)
			}
		case "!garden:x":
			if n.Direct || n.Mention || n.RoomName != "Garden" {
				t.Errorf("room notification = %+v", n)
			}
		}
	}
}

func TestExpiredTokenEndsTheSession(t *testing.T) {
	f := newFakeHomeserver(t, true)
	m, _, _ := newTestManager(t, f)
	signIn(t, m, f)
	f.mu.Lock()
	f.token = "rotated"
	f.mu.Unlock()
	f.queueSync(map[string]any{})
	waitFor(t, "error state", func() bool { return m.Status().State == StateError })
}

func TestSSOHandlerOnlyTakesItsOwnPath(t *testing.T) {
	tokens := make(chan string, 1)
	h := ssoHandler("/sso/abc", tokens)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sso/other?loginToken=evil", nil))
	select {
	case <-tokens:
		t.Fatal("a token arrived on the wrong path")
	default:
	}
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/sso/abc?loginToken=good", nil))
	select {
	case tok := <-tokens:
		if tok != "good" {
			t.Fatalf("token = %q", tok)
		}
	case <-time.After(time.Second):
		t.Fatal("no token")
	}
}

func TestSSOLoginThroughTheBrowserRedirect(t *testing.T) {
	f := newFakeHomeserver(t, true)
	m, _, _ := newTestManager(t, f)
	m.opts.OpenURL = func(target string) {
		// play the browser: the server redirects back with a login token.
		go func() {
			u, _ := url.Parse(target)
			redirect := u.Query().Get("redirectUrl")
			resp, err := http.Get(redirect + "?loginToken=login-token-from-sso")
			if err == nil {
				resp.Body.Close()
			}
		}()
	}
	if err := m.LoginSSO(context.Background(), f.srv.URL, "oidc-example"); err != nil {
		t.Fatalf("LoginSSO: %v", err)
	}
	waitFor(t, "sync", func() bool { return m.Status().State == StateOnline })
}

func TestJoinTarget(t *testing.T) {
	cases := []struct {
		in, room string
		via      []string
		bad      bool
	}{
		{in: "#garden:example.org", room: "#garden:example.org"},
		{in: "!abc:example.org", room: "!abc:example.org"},
		{in: "https://matrix.to/#/%23garden%3Aexample.org?via=example.org", room: "#garden:example.org", via: []string{"example.org"}},
		{in: "#garden", bad: true},
		{in: "garden", bad: true},
	}
	for _, c := range cases {
		room, via, err := joinTarget(c.in)
		if c.bad != (err != nil) || room != c.room || strings.Join(via, ",") != strings.Join(c.via, ",") {
			t.Errorf("joinTarget(%q) = %q %v %v", c.in, room, via, err)
		}
	}
}

func TestContainsWord(t *testing.T) {
	cases := []struct {
		hay, needle string
		want        bool
	}{
		{"hi alice!", "alice", true},
		{"alice", "alice", true},
		{"malice aforethought", "alice", false},
		{"alice2 is here", "alice", false},
		{"ping @alice:", "alice", true},
	}
	for _, c := range cases {
		if got := containsWord(c.hay, c.needle); got != c.want {
			t.Errorf("containsWord(%q, %q) = %v", c.hay, c.needle, got)
		}
	}
}

func TestBuildContentWithoutMarkdownKeepsTextPlain(t *testing.T) {
	c, err := buildContent(SendRequest{Text: "*not emphasis*  \n"})
	if err != nil {
		t.Fatal(err)
	}
	if c.Body != "*not emphasis*" || c.FormattedBody != "" {
		t.Fatalf("content = %+v", c)
	}
	if _, err := buildContent(SendRequest{Text: "   "}); err == nil {
		t.Fatal("an empty message was accepted")
	}
	emote, _ := buildContent(SendRequest{Text: "waves", Emote: true})
	if emote.MsgType != "m.emote" {
		t.Fatalf("emote type = %s", emote.MsgType)
	}
}

func TestBackoffGrowsAndCaps(t *testing.T) {
	if d := backoff(1); d < time.Second || d > 1300*time.Millisecond {
		t.Errorf("first backoff = %v", d)
	}
	if d := backoff(3); d < 4*time.Second {
		t.Errorf("third backoff = %v", d)
	}
	if d := backoff(40); d < time.Minute || d > 73*time.Second {
		t.Errorf("late backoff = %v", d)
	}
}

func pngOf(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for x := range w {
		img.Set(x, x%h, color.NRGBA{R: 200, A: 255})
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestMakeThumbnailShrinksToFit(t *testing.T) {
	img := pngOf(t, 1600, 900)
	thumb, w, h, err := makeThumbnail(img)
	if err != nil || len(thumb) == 0 {
		t.Fatalf("makeThumbnail: %v", err)
	}
	if w != 800 || h != 450 {
		t.Fatalf("thumbnail is %dx%d", w, h)
	}
}
