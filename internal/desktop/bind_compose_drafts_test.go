package desktop

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"maps"
	"mime"
	"net/mail"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"

	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/outbox"
	"github.com/peltonapp/Pelton/internal/storage"
)

// draftTestApp is pgpTestApp with the account's IMAP connection faked and a
// Drafts folder synced.
func draftTestApp(t *testing.T, own, others []string) (*App, int64, *fakeIMAP) {
	t.Helper()
	app, _, accountID := pgpTestApp(t, own, others)
	client := &fakeIMAP{}
	useFake(t, app, accountID, client)
	addFolder(t, app, accountID, "Drafts", `\Drafts`)
	return app, accountID, client
}

func addFolder(t *testing.T, app *App, accountID int64, path string, attrs ...string) *storage.Folder {
	t.Helper()
	folder := &storage.Folder{AccountID: accountID, Name: path, IMAPPath: path, Attributes: attrs}
	if _, err := app.store.CreateFolder(app.ctx, folder); err != nil {
		t.Fatalf("create folder %q: %v", path, err)
	}
	return folder
}

func saveDraft(t *testing.T, app *App, id int64, req ComposeRequest) int64 {
	t.Helper()
	id, err := app.SaveDraft(id, req)
	if err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	return id
}

func listDrafts(t *testing.T, app *App) []DraftDTO {
	t.Helper()
	drafts, err := app.ListDrafts()
	if err != nil {
		t.Fatalf("ListDrafts: %v", err)
	}
	return drafts
}

// settle waits for the goroutines a binding left behind, so a test reads the
// fake only once nothing else is using it.
func settle(t *testing.T) {
	t.Helper()
	if !waitForBackgroundWork(backgroundWorkGrace) {
		t.Fatal("background work did not finish")
	}
}

// The frontend holds draft ids as JavaScript numbers. An id it cannot hold
// exactly comes back rounded, and saving or deleting with it used to report
// success while touching nothing.
func TestDraftIDsSurviveAJavaScriptNumber(t *testing.T) {
	app, _, accountID := pgpTestApp(t, nil, nil)

	id := saveDraft(t, app, 0, ComposeRequest{AccountID: accountID, Subject: "one"})
	jsID := int64(float64(id))
	if jsID != id {
		t.Fatalf("draft id %d does not survive a float64 round trip (%d)", id, jsID)
	}
	saveDraft(t, app, jsID, ComposeRequest{AccountID: accountID, Subject: "two"})
	if drafts := listDrafts(t, app); len(drafts) != 1 || drafts[0].Request.Subject != "two" {
		t.Fatalf("drafts = %+v, want one draft with the re-saved subject", drafts)
	}
	if err := app.DeleteDraft(jsID); err != nil {
		t.Fatalf("DeleteDraft: %v", err)
	}
	if drafts := listDrafts(t, app); len(drafts) != 0 {
		t.Fatalf("drafts after delete = %+v, want none", drafts)
	}
	if _, err := app.SaveDraft(jsID, ComposeRequest{AccountID: accountID}); !errors.Is(err, ErrDraftNotFound) {
		t.Fatalf("SaveDraft(unknown) = %v, want ErrDraftNotFound", err)
	}
}

func TestLegacyDraftIDsAreRenumbered(t *testing.T) {
	app, db, accountID := pgpTestApp(t, nil, nil)
	legacy := []storedDraft{
		{ID: 3, SavedAt: "2026-10-01T10:00:00Z", Request: ComposeRequest{AccountID: accountID, Subject: "small"}, AccountID: accountID},
		{ID: 1791136856123456789, SavedAt: "2026-10-01T11:00:00Z", Request: ComposeRequest{AccountID: accountID, Subject: "big"}, AccountID: accountID},
	}
	if err := db.SetJSON(app.ctx, draftsKey, legacy); err != nil {
		t.Fatalf("seed drafts: %v", err)
	}

	ids := map[string]int64{}
	for _, d := range listDrafts(t, app) {
		ids[d.Request.Subject] = d.ID
	}
	if len(ids) != 2 || ids["small"] != 3 || ids["big"] == 0 || ids["big"] > maxSafeDraftID {
		t.Fatalf("ids = %v, want small kept at 3 and big renumbered to at most %d", ids, maxSafeDraftID)
	}
	saveDraft(t, app, ids["big"], ComposeRequest{AccountID: accountID, Subject: "big edited"})
}

// draftsOnServer stores messages in the account's Drafts folder as sync would,
// Message-ID without its angle brackets as the envelope parse leaves it, and
// returns their storage ids.
func draftsOnServer(t *testing.T, app *App, accountID int64, messageIDs ...string) []int64 {
	t.Helper()
	folders, err := app.store.ListFolders(app.ctx, accountID)
	if err != nil {
		t.Fatalf("list folders: %v", err)
	}
	i := slices.IndexFunc(folders, func(f storage.Folder) bool { return f.IMAPPath == "Drafts" })
	if i < 0 {
		t.Fatal("no Drafts folder")
	}
	var ids []int64
	for n, messageID := range messageIDs {
		id, err := app.store.InsertMessage(app.ctx, &storage.Message{
			AccountID: accountID,
			FolderID:  folders[i].ID,
			UID:       uint32(n + 1),
			MessageID: strings.Trim(messageID, "<>"),
			Subject:   "draft",
			Date:      time.Now(),
		})
		if err != nil {
			t.Fatalf("insert message: %v", err)
		}
		ids = append(ids, id)
	}
	return ids
}

// A draft saved, uploaded, edited and uploaded again goes up under a fresh
// Message-ID each time, the copy before it removed, so the server holds one
// copy, and opening that copy from the Drafts folder gets the local draft back
// whole. Bcc is checked because a draft rebuilt from the
// server copy alone is where it would go missing.
func TestDraftRoundTripsThroughTheServer(t *testing.T) {
	app, accountID, client := draftTestApp(t, nil, nil)
	req := ComposeRequest{
		AccountID: accountID,
		To:        []AddressDTO{{Email: "you@example.com"}},
		Bcc:       []AddressDTO{{Email: "hidden@example.com"}},
		Text:      "body",
	}
	var id int64
	var messageID, first string
	// the composer never passes the Message-ID back, as after a reload: the
	// second save must still replace the first copy, not add another.
	for _, subject := range []string{"first", "second"} {
		req.Subject = subject
		id = saveDraft(t, app, id, req)
		got, err := app.UploadDraft(id)
		settle(t)
		if err != nil {
			t.Fatalf("upload %q: %v", subject, err)
		}
		if got == "" || got == messageID {
			t.Errorf("upload %q used %q, want a Message-ID of its own", subject, got)
		}
		first, messageID = cmp.Or(first, got), got
		if drafts := listDrafts(t, app); messageID == "" || drafts[0].Request.DraftMessageID != messageID {
			t.Fatalf("local draft links to %q, want the uploaded %q", drafts[0].Request.DraftMessageID, messageID)
		}
	}
	if len(client.drafts) != 2 {
		t.Fatalf("got %d uploads, want 2", len(client.drafts))
	}
	if !slices.Equal(client.removedDrafts, []string{first}) {
		t.Fatalf("removed %v, want the first copy %q", client.removedDrafts, first)
	}
	for i, call := range client.drafts {
		raw := string(call.raw)
		if !strings.Contains(raw, "Message-ID: "+call.messageID) {
			t.Errorf("upload %d under %q carries another Message-ID: %q", i, call.messageID, raw)
		}
		if !strings.Contains(raw, "hidden@example.com") {
			t.Errorf("upload %d lost the Bcc recipient: %q", i, raw)
		}
	}

	ids := draftsOnServer(t, app, accountID, messageID, "<webmail@example.com>")
	opened, err := app.OpenDraft(ids[0])
	if err != nil {
		t.Fatalf("OpenDraft: %v", err)
	}
	if opened.ID != id || opened.Request.Subject != "second" || !slices.Equal(opened.Request.Bcc, req.Bcc) {
		t.Fatalf("opened %+v, want local draft %d with its subject and Bcc", opened, id)
	}
	if other, err := app.OpenDraft(ids[1]); err != nil || other.ID != 0 {
		t.Fatalf("OpenDraft(written elsewhere) = %+v, %v, want an empty draft and no error", other, err)
	}

	// once sent, the composer deletes the draft and its server copy has to go
	// too, found by the Message-ID stored here: the composer may not know it.
	app.queue = outbox.NewQueue(app.store)
	if _, err := app.SendMessage(req); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if err := app.DeleteDraft(id); err != nil {
		t.Fatalf("DeleteDraft: %v", err)
	}
	settle(t)
	if !slices.Equal(client.removedDrafts, []string{first, messageID}) {
		t.Fatalf("removed %v, want %q then %q", client.removedDrafts, first, messageID)
	}
}

// The draft goes to the folder the folder list shows as Drafts: one the user
// picked by hand wins over the server's, and with neither nothing is uploaded.
func TestDraftsGoToTheFolderUsedAsDrafts(t *testing.T) {
	tests := map[string]struct{ server, picked, want string }{
		"server Drafts":  {server: "INBOX.Entwurf", want: "INBOX.Entwurf"},
		"picked by hand": {server: "Drafts", picked: "Wersje robocze", want: "Wersje robocze"},
		"none":           {},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			app, _, accountID := pgpTestApp(t, nil, nil)
			client := &fakeIMAP{}
			useFake(t, app, accountID, client)
			if tt.server != "" {
				addFolder(t, app, accountID, tt.server, `\Drafts`)
			}
			if tt.picked != "" {
				mine := addFolder(t, app, accountID, tt.picked)
				if err := app.store.SetFolderRoleOverride(app.ctx, mine.ID, roleDrafts); err != nil {
					t.Fatalf("set role: %v", err)
				}
			}
			id := saveDraft(t, app, 0, ComposeRequest{AccountID: accountID, Subject: "hi"})

			_, err := app.UploadDraft(id)
			settle(t)
			if tt.want == "" {
				if !errors.Is(err, ErrNoDraftsFolder) || len(client.drafts) != 0 {
					t.Fatalf("UploadDraft = %v with %d uploads, want ErrNoDraftsFolder and none", err, len(client.drafts))
				}
				return
			}
			if err != nil {
				t.Fatalf("UploadDraft: %v", err)
			}
			if len(client.drafts) != 1 || client.drafts[0].mailbox != tt.want {
				t.Fatalf("uploads = %+v, want one to %q", client.drafts, tt.want)
			}
		})
	}
}

// A draft of a message to be encrypted goes to the server as PGP/MIME the
// account's own key alone can open: not the recipients, whose keys are in the
// store, and not the server. Opening the server copy finds the local draft.
func TestEncryptedDraftGoesUpAsCiphertext(t *testing.T) {
	tests := map[string]struct {
		protection string
		restart    bool
	}{
		"encrypt":     {protection: protectionEncrypt},
		"signencrypt": {protection: protectionSignEncrypt},
		// the request a save kept in memory is gone, so the draft is unsealed.
		"after a restart": {protection: protectionEncrypt, restart: true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			app, accountID, client := draftTestApp(t, []string{"me@example.com"}, []string{"a@example.com"})
			id := saveDraft(t, app, 0, ComposeRequest{
				AccountID:  accountID,
				Protection: tt.protection,
				To:         addr("a@example.com"),
				Cc:         addr("carol@example.com"),
				Bcc:        addr("hidden@example.com"),
				Subject:    "quarterly numbers",
				Text:       "not for the server",
			})
			if tt.restart {
				app.sealedDrafts = nil
			}
			messageID, err := app.UploadDraft(id)
			settle(t)
			if err != nil {
				t.Fatalf("UploadDraft: %v", err)
			}
			if len(client.drafts) != 1 || messageID == "" || client.drafts[0].messageID != messageID {
				t.Fatalf("uploads %+v, want one replacing %q", client.drafts, messageID)
			}
			raw := string(client.drafts[0].raw)
			if !strings.Contains(raw, "Message-ID: "+messageID) || !strings.Contains(raw, "multipart/encrypted") {
				t.Errorf("server copy is not PGP/MIME with Message-ID %s:\n%s", messageID, raw)
			}
			// a Message-ID is hex, so a recipient address is only looked for
			// where it would be named: in a header of its own.
			for _, leak := range []string{"not for the server", "quarterly numbers", "\r\nTo:", "\r\nCc:", "carol@example.com", "hidden@example.com"} {
				if strings.Contains(raw, leak) {
					t.Errorf("server copy carries %q in the clear:\n%s", leak, raw)
				}
			}
			if body := openAsOwnKeyOnly(t, app, raw); !strings.Contains(body, "not for the server") {
				t.Errorf("decrypted server copy lacks the body: %q", body)
			}

			opened, err := app.OpenDraft(draftsOnServer(t, app, accountID, messageID)[0])
			if err != nil || opened.ID != id || opened.Locked || opened.Request.Subject != "quarterly numbers" {
				t.Fatalf("OpenDraft = %+v, %v, want sealed draft %d opened", opened, err, id)
			}
		})
	}
}

// storedDrafts reads the drafts value as stored.
func storedDrafts(t *testing.T, app *App) []storedDraft {
	t.Helper()
	var stored []storedDraft
	if err := app.store.GetJSON(app.ctx, draftsKey, &stored); err != nil {
		t.Fatalf("read drafts: %v", err)
	}
	return stored
}

// removalQueue reads the server copies waiting to be removed.
func removalQueue(t *testing.T, app *App) []draftCopy {
	t.Helper()
	var queue []draftCopy
	if err := app.store.GetJSON(app.ctx, draftRemovalsKey, &queue); err != nil && !isSettingMissing(err) {
		t.Fatalf("read removal queue: %v", err)
	}
	return queue
}

// copyIDs lists the Message-IDs of the server copies a draft may have.
func copyIDs(d storedDraft) []string {
	var ids []string
	for _, c := range d.ServerCopies {
		ids = append(ids, c.MessageID)
	}
	return ids
}

// uploadPlain uploads a plaintext draft and returns its id, the request and
// the copy's Message-ID.
func uploadPlain(t *testing.T, app *App, accountID int64) (int64, ComposeRequest, string) {
	t.Helper()
	req := ComposeRequest{AccountID: accountID, To: addr("a@example.com"), Subject: "numbers", Text: "plain"}
	id := saveDraft(t, app, 0, req)
	plain, err := app.UploadDraft(id)
	settle(t)
	if err != nil || plain == "" {
		t.Fatalf("UploadDraft = %q, %v", plain, err)
	}
	return id, req, plain
}

// seal saves draft id set to encrypt and waits for the removals that queues.
func seal(t *testing.T, app *App, id int64, req ComposeRequest) ComposeRequest {
	t.Helper()
	req.Protection = protectionEncrypt
	saveDraft(t, app, id, req)
	settle(t)
	return req
}

// uploadPlainThenSeal is uploadPlain, then seal.
func uploadPlainThenSeal(t *testing.T, app *App, accountID int64) (int64, ComposeRequest, string) {
	t.Helper()
	id, req, plain := uploadPlain(t, app, accountID)
	return id, seal(t, app, id, req), plain
}

// A draft uploaded in the clear and then switched to encrypt has its plaintext
// copy removed as soon as it is saved that way, and its encrypted copy goes up
// under a fresh Message-ID, as every upload does, so no server keeping one
// message per Message-ID can keep the plaintext copy in its place.
func TestEncryptingAnUploadedDraftReplacesThePlaintextCopy(t *testing.T) {
	app, accountID, client := draftTestApp(t, []string{"me@example.com"}, []string{"a@example.com"})
	id, _, plain := uploadPlainThenSeal(t, app, accountID)

	stored := storedDrafts(t, app)
	if len(stored) != 1 || stored[0].Sealed == "" || len(stored[0].ServerCopies) != 0 || stored[0].Request.DraftMessageID != "" {
		t.Fatalf("stored %+v, want one sealed draft with no server copy left", stored)
	}
	if !slices.Equal(client.removedDrafts, []string{plain}) || len(removalQueue(t, app)) != 0 {
		t.Fatalf("removed %v with %v queued, want the plaintext copy %q removed", client.removedDrafts, removalQueue(t, app), plain)
	}

	sealed, err := app.UploadDraft(id)
	settle(t)
	if err != nil || sealed == "" || sealed == plain {
		t.Fatalf("encrypted UploadDraft = %q, %v; want a new Message-ID", sealed, err)
	}
	if len(client.drafts) != 2 || client.drafts[1].messageID != sealed || !strings.Contains(string(client.drafts[1].raw), "multipart/encrypted") {
		t.Fatalf("uploads %+v, want the second an encrypted copy under %q", client.drafts, sealed)
	}
	if !slices.Equal(client.removedDrafts, []string{plain}) || len(removalQueue(t, app)) != 0 {
		t.Fatalf("removed %v with %v queued, want just the plaintext copy %q", client.removedDrafts, removalQueue(t, app), plain)
	}
	if stored := storedDrafts(t, app); !slices.Equal(stored[0].ServerCopies, []serverCopy{{draftCopy{accountID, sealed, "Drafts"}, true}}) || stored[0].Request.DraftMessageID != sealed {
		t.Fatalf("stored %+v, want the encrypted copy %q alone", stored[0], sealed)
	}

	if err := app.DeleteDraft(id); err != nil {
		t.Fatalf("DeleteDraft: %v", err)
	}
	settle(t)
	if !slices.Equal(client.removedDrafts, []string{plain, sealed}) {
		t.Fatalf("after delete removed %v, want %q then %q", client.removedDrafts, plain, sealed)
	}
}

// draftServer is one account's Drafts folder, shared by every IMAP session a
// test opens on it, each a fresh fakeIMAP so concurrent sessions share nothing
// else. Like Gmail it keeps one message per Message-ID: storing a draft under
// one it holds keeps the copy already there.
type draftServer struct {
	mu      sync.Mutex
	copies  map[string]string
	removed []string
	uploads int
	// down makes every connection fail, refuse every upload, and lose the next
	// upload store it but report a failure, as when the answer is lost.
	down   bool
	refuse error
	lose   bool
	// with block, the first upload waits inside the session until release
	// closes; entered says it got there.
	block   bool
	entered chan struct{}
	release chan struct{}
}

func newDraftServer() *draftServer {
	return &draftServer{copies: map[string]string{}, entered: make(chan struct{}), release: make(chan struct{})}
}

type draftServerSession struct {
	*fakeIMAP
	srv *draftServer
}

func (s draftServerSession) ReplaceDraft(_ string, raw []byte, messageID string) error {
	srv := s.srv
	srv.mu.Lock()
	srv.uploads++
	first := srv.uploads == 1
	srv.mu.Unlock()
	if first && srv.block {
		close(srv.entered)
		<-srv.release
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if srv.refuse != nil {
		return srv.refuse
	}
	if _, held := srv.copies[messageID]; !held {
		srv.copies[messageID] = string(raw)
	}
	if srv.lose {
		srv.lose = false
		return errors.New("connection lost")
	}
	return nil
}

func (s draftServerSession) RemoveDraft(_, messageID string) error {
	s.srv.mu.Lock()
	defer s.srv.mu.Unlock()
	delete(s.srv.copies, messageID)
	s.srv.removed = append(s.srv.removed, messageID)
	return nil
}

// set changes the server under its lock.
func (srv *draftServer) set(change func(*draftServer)) {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	change(srv)
}

// serve routes the IMAP sessions of app to a server per login name.
func serve(app *App, servers map[string]*draftServer) {
	app.newIMAPClient = func(cfg pimap.Config) (mailClient, error) {
		srv := servers[cfg.Username]
		srv.mu.Lock()
		down := srv.down
		srv.mu.Unlock()
		if down {
			return nil, errors.New("server down")
		}
		return draftServerSession{&fakeIMAP{}, srv}, nil
	}
}

// holds fails the test unless the server holds exactly these Message-IDs, the
// copy under each containing its text.
func (srv *draftServer) holds(t *testing.T, want map[string]string) {
	t.Helper()
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if len(srv.copies) != len(want) {
		t.Fatalf("server holds %v, want %v", slices.Collect(maps.Keys(srv.copies)), slices.Collect(maps.Keys(want)))
	}
	for id, text := range want {
		if raw, ok := srv.copies[id]; !ok || !strings.Contains(raw, text) {
			t.Fatalf("server copy %q = %q, want one containing %q", id, raw, text)
		}
	}
}

// encrypted fails the test unless raw is ciphertext whose body has text.
func encryptedWith(t *testing.T, app *App, raw, text string) {
	t.Helper()
	if !strings.Contains(raw, "multipart/encrypted") || strings.Contains(raw, text) {
		t.Fatalf("server copy is not ciphertext:\n%s", raw)
	}
	if body := openAsOwnKeyOnly(t, app, raw); !strings.Contains(body, text) {
		t.Fatalf("decrypted server copy lacks %q: %q", text, body)
	}
}

// oneServer is draftTestApp with its account served by a draftServer.
func oneServer(t *testing.T) (*App, int64, *draftServer) {
	t.Helper()
	app, accountID, _ := draftTestApp(t, []string{"me@example.com"}, []string{"a@example.com"})
	srv := newDraftServer()
	serve(app, map[string]*draftServer{"me@example.com": srv})
	return app, accountID, srv
}

// On a server keeping the first copy stored under a Message-ID, every edit
// still reaches it, encrypted or not, since every upload has a Message-ID of
// its own, and switching encryption off replaces the ciphertext.
func TestEveryEditReachesADeduplicatingServer(t *testing.T) {
	app, accountID, srv := oneServer(t)
	req := ComposeRequest{AccountID: accountID, To: addr("a@example.com"), Subject: "numbers"}
	var id int64
	upload := func(protection, text string) string {
		t.Helper()
		req.Protection, req.Text = protection, text
		id = saveDraft(t, app, id, req)
		messageID, err := app.UploadDraft(id)
		settle(t)
		if err != nil {
			t.Fatalf("upload %q: %v", text, err)
		}
		return messageID
	}

	srv.holds(t, map[string]string{upload(protectionNone, "first"): "first"})
	srv.holds(t, map[string]string{upload(protectionNone, "second"): "second"})
	for _, text := range []string{"sealed", "sealed and edited"} {
		messageID := upload(protectionEncrypt, text)
		srv.holds(t, map[string]string{messageID: "multipart/encrypted"})
		encryptedWith(t, app, srv.copies[messageID], text)
	}
	srv.holds(t, map[string]string{upload(protectionNone, "plain again"): "plain again"})
}

// Server copies belong to the account they went up on. Switching a draft to
// another account, in the clear or encrypted, removes the copy on the first
// account, not a search for it on the second.
func TestSwitchingAccountsRemovesTheCopyOnTheFirst(t *testing.T) {
	for name, protection := range map[string]string{"plaintext": protectionNone, "encrypted": protectionEncrypt} {
		t.Run(name, func(t *testing.T) {
			app, first, _ := draftTestApp(t, []string{"me@example.com", "b@example.com"}, []string{"a@example.com"})
			second, err := app.store.CreateAccount(app.ctx, &storage.Account{Email: "b@example.com"})
			if err != nil {
				t.Fatalf("create account: %v", err)
			}
			useFake(t, app, second, &fakeIMAP{})
			addFolder(t, app, second, "Drafts", `\Drafts`)
			onFirst, onSecond := newDraftServer(), newDraftServer()
			serve(app, map[string]*draftServer{"me@example.com": onFirst, "b@example.com": onSecond})

			req := ComposeRequest{AccountID: first, To: addr("a@example.com"), Subject: "numbers", Text: "body"}
			id := saveDraft(t, app, 0, req)
			if _, err := app.UploadDraft(id); err != nil {
				t.Fatalf("upload on the first account: %v", err)
			}
			req.AccountID, req.Protection = second, protection
			saveDraft(t, app, id, req)
			messageID, err := app.UploadDraft(id)
			settle(t)
			if err != nil {
				t.Fatalf("upload on the second account: %v", err)
			}
			onFirst.holds(t, map[string]string{})
			onSecond.holds(t, map[string]string{messageID: ""})
			if len(onSecond.removed) != 0 {
				t.Fatalf("removed %v on the second account, want nothing", onSecond.removed)
			}
		})
	}
}

// An encrypted copy survives two refused uploads around it, one with
// encryption off and one with it on again: what is on the server is told by
// the copies recorded, not by how the draft was last saved.
func TestRefusedUploadsKeepTheEncryptedCopy(t *testing.T) {
	app, accountID, srv := oneServer(t)
	req := ComposeRequest{AccountID: accountID, Protection: protectionEncrypt, To: addr("a@example.com"), Subject: "numbers", Text: "sealed"}
	id := saveDraft(t, app, 0, req)
	sealed, err := app.UploadDraft(id)
	settle(t)
	if err != nil {
		t.Fatalf("UploadDraft: %v", err)
	}

	srv.set(func(s *draftServer) { s.refuse = errors.New("over quota") })
	for _, protection := range []string{protectionNone, protectionEncrypt} {
		req.Protection = protection
		saveDraft(t, app, id, req)
		if _, err := app.UploadDraft(id); err == nil {
			t.Fatalf("upload with %q succeeded against a refusing server", protection)
		}
		settle(t)
	}
	srv.holds(t, map[string]string{sealed: "multipart/encrypted"})
	if stored := storedDrafts(t, app); !slices.Equal(copyIDs(stored[0]), []string{sealed}) {
		t.Fatalf("server copies %v, want just %q", copyIDs(stored[0]), sealed)
	}
}

// An encrypted upload the server refuses still removes the plaintext copy:
// the local draft remains, and a plaintext copy of a draft the user set to
// encrypt must not.
func TestRefusedEncryptedUploadStillRemovesThePlaintextCopy(t *testing.T) {
	app, accountID, srv := oneServer(t)
	id, _, _ := uploadPlainThenSeal(t, app, accountID)

	srv.set(func(s *draftServer) { s.refuse = errors.New("over quota") })
	if _, err := app.UploadDraft(id); err == nil {
		t.Fatal("UploadDraft succeeded against a refusing server")
	}
	settle(t)
	srv.holds(t, map[string]string{})
	if stored := storedDrafts(t, app); len(stored[0].ServerCopies) != 0 || len(removalQueue(t, app)) != 0 {
		t.Fatalf("copies %v, queued %v; want neither", stored[0].ServerCopies, removalQueue(t, app))
	}
}

// An upload whose answer is lost may still have stored its copy, so the copy
// is recorded before it goes up and removed when the upload fails, leaving
// nothing behind.
func TestLostUploadAnswerLeavesNoOrphan(t *testing.T) {
	app, accountID, srv := oneServer(t)
	req := ComposeRequest{AccountID: accountID, To: addr("a@example.com"), Subject: "numbers", Text: "first"}
	id := saveDraft(t, app, 0, req)
	first, err := app.UploadDraft(id)
	settle(t)
	if err != nil {
		t.Fatalf("UploadDraft: %v", err)
	}

	srv.set(func(s *draftServer) { s.lose = true })
	req.Text = "second"
	saveDraft(t, app, id, req)
	if _, err := app.UploadDraft(id); err == nil {
		t.Fatal("UploadDraft with a lost answer succeeded")
	}
	settle(t)
	srv.holds(t, map[string]string{first: "first"})
	if len(removalQueue(t, app)) != 0 {
		t.Fatalf("still queued: %v", removalQueue(t, app))
	}
}

// Deleting a draft, as sending it does, while its server is down keeps its
// copies queued for removal; the next upload of any draft on the account, or
// the next start, removes them.
func TestCopiesOfADeletedDraftAreRemovedOnceTheServerIsBack(t *testing.T) {
	for name, drain := range map[string]func(t *testing.T, app *App, accountID int64){
		"next upload": func(t *testing.T, app *App, accountID int64) {
			id := saveDraft(t, app, 0, ComposeRequest{AccountID: accountID, Subject: "another", Text: "other"})
			if _, err := app.UploadDraft(id); err != nil {
				t.Fatalf("UploadDraft: %v", err)
			}
		},
		"next start": func(t *testing.T, app *App, _ int64) { app.drainDraftRemovals() },
	} {
		t.Run(name, func(t *testing.T) {
			app, accountID, srv := oneServer(t)
			id, req, plain := uploadPlain(t, app, accountID)
			srv.set(func(s *draftServer) { s.down = true })
			seal(t, app, id, req)
			if _, err := app.UploadDraft(id); err == nil {
				t.Fatal("UploadDraft succeeded with the server down")
			}
			if err := app.DeleteDraft(id); err != nil {
				t.Fatalf("DeleteDraft: %v", err)
			}
			settle(t)
			if queued := removalQueue(t, app); len(queued) == 0 || !slices.ContainsFunc(queued, func(c draftCopy) bool { return c.MessageID == plain }) {
				t.Fatalf("queued %v, want %q among them", queued, plain)
			}

			srv.set(func(s *draftServer) { s.down = false })
			drain(t, app, accountID)
			settle(t)
			if queued := removalQueue(t, app); len(queued) != 0 {
				t.Fatalf("still queued: %v", queued)
			}
			srv.mu.Lock()
			_, left := srv.copies[plain]
			srv.mu.Unlock()
			if left {
				t.Fatalf("the plaintext copy %q of the deleted draft is still there", plain)
			}
		})
	}
}

// A removal that fails stays queued and is tried again by the next upload,
// and only one that worked leaves the queue.
func TestFailedRemovalIsRetried(t *testing.T) {
	app, accountID, client := draftTestApp(t, []string{"me@example.com"}, []string{"a@example.com"})
	id, req, plain := uploadPlain(t, app, accountID)

	client.failRemoveDrafts = 1
	req = seal(t, app, id, req)
	if queued := removalQueue(t, app); !slices.Equal(queued, []draftCopy{{accountID, plain, "Drafts"}}) {
		t.Fatalf("queued %v after a failed removal, want %q", queued, plain)
	}

	req.Text = "edited"
	saveDraft(t, app, id, req)
	if _, err := app.UploadDraft(id); err != nil {
		t.Fatalf("second UploadDraft: %v", err)
	}
	settle(t)
	if !slices.Contains(client.removedDrafts, plain) || len(removalQueue(t, app)) != 0 {
		t.Fatalf("removed %v with %v queued, want %q removed on the retry", client.removedDrafts, removalQueue(t, app), plain)
	}
}

// When a sealed draft's encrypted copy cannot be built, here because it can no
// longer be unsealed, the upload fails and the plaintext copy from before
// encryption was turned on is removed.
func TestEncryptedDraftThatCannotGoUpLeavesNoPlaintextCopy(t *testing.T) {
	app, accountID, client := draftTestApp(t, []string{"me@example.com"}, []string{"a@example.com"})
	id, _, plain := uploadPlainThenSeal(t, app, accountID)

	stored := storedDrafts(t, app)
	stored[0].Sealed = "not armor"
	if err := app.store.SetJSON(app.ctx, draftsKey, stored); err != nil {
		t.Fatalf("write drafts: %v", err)
	}
	forgetHeld(t, app, id)

	if _, err := app.UploadDraft(id); err == nil {
		t.Fatal("UploadDraft of an unreadable sealed draft succeeded")
	}
	settle(t)
	if len(client.drafts) != 1 || !slices.Equal(client.removedDrafts, []string{plain}) {
		t.Fatalf("uploads %d, removed %v; want no new upload and %q removed", len(client.drafts), client.removedDrafts, plain)
	}
}

// Any failure to build the encrypted copy, here an attachment that does not
// decode, removes the plaintext copy just the same.
func TestEncryptedDraftThatCannotBeBuiltLeavesNoPlaintextCopy(t *testing.T) {
	app, accountID, client := draftTestApp(t, []string{"me@example.com"}, []string{"a@example.com"})
	id, req, plain := uploadPlainThenSeal(t, app, accountID)

	req.Attachments = []ComposeAttachment{{Filename: "broken.bin", ContentBase64: "not base64!"}}
	saveDraft(t, app, id, req)
	if _, err := app.UploadDraft(id); err == nil {
		t.Fatal("UploadDraft of a draft with a broken attachment succeeded")
	}
	settle(t)
	if len(client.drafts) != 1 || !slices.Equal(client.removedDrafts, []string{plain}) {
		t.Fatalf("uploads %d, removed %v; want no new upload and %q removed", len(client.drafts), client.removedDrafts, plain)
	}
}

// A draft stored before server copies were recorded, its one Message-ID all
// there is, has that copy treated as plaintext: the next upload replaces it,
// sealed or not.
func TestDraftsFromBeforeServerCopiesAreMigrated(t *testing.T) {
	for name, tt := range map[string]struct{ protection, copy string }{
		"plaintext": {protectionNone, "body"},
		"sealed":    {protectionEncrypt, "multipart/encrypted"},
	} {
		t.Run(name, func(t *testing.T) {
			protection := tt.protection
			app, accountID, srv := oneServer(t)
			const old = "<old@example.com>"
			id := saveDraft(t, app, 0, ComposeRequest{AccountID: accountID, Protection: protection, To: addr("a@example.com"), Text: "body"})
			stored := storedDrafts(t, app)
			stored[0].ServerCopies, stored[0].Request.DraftMessageID = nil, old
			if err := app.store.SetJSON(app.ctx, draftsKey, stored); err != nil {
				t.Fatalf("write drafts: %v", err)
			}
			srv.set(func(s *draftServer) { s.copies[old] = "old plaintext" })

			messageID, err := app.UploadDraft(id)
			settle(t)
			if err != nil {
				t.Fatalf("UploadDraft: %v", err)
			}
			srv.holds(t, map[string]string{messageID: tt.copy})
		})
	}
}

// Uploads of one draft take turns, each building from the draft as stored
// when its turn comes: a plaintext upload that stalls on the server cannot
// outlast the encrypted upload after it.
func TestStalledPlaintextUploadCannotOutliveTheEncryptedCopy(t *testing.T) {
	app, accountID, srv := oneServer(t)
	srv.block = true

	req := ComposeRequest{AccountID: accountID, To: addr("a@example.com"), Subject: "numbers", Text: "plain"}
	id := saveDraft(t, app, 0, req)
	plain := make(chan error, 1)
	go func() { _, err := app.UploadDraft(id); plain <- err }()
	<-srv.entered

	req.Protection = protectionEncrypt
	saveDraft(t, app, id, req)
	encrypted := make(chan error, 1)
	go func() { _, err := app.UploadDraft(id); encrypted <- err }()
	select {
	case err := <-encrypted:
		encrypted <- err
	case <-time.After(200 * time.Millisecond):
	}
	close(srv.release)
	for _, done := range []chan error{plain, encrypted} {
		if err := <-done; err != nil {
			t.Fatalf("UploadDraft: %v", err)
		}
	}
	settle(t)
	srv.holds(t, map[string]string{storedDrafts(t, app)[0].Request.DraftMessageID: "multipart/encrypted"})
}

// A draft deleted while its upload is still on the way to the server loses
// that copy too, once the upload is done with it.
func TestDraftDeletedDuringItsUploadLeavesNoCopy(t *testing.T) {
	app, accountID, srv := oneServer(t)
	srv.block = true
	id := saveDraft(t, app, 0, ComposeRequest{AccountID: accountID, Subject: "numbers", Text: "plain"})
	done := make(chan error, 1)
	go func() { _, err := app.UploadDraft(id); done <- err }()
	<-srv.entered

	if err := app.DeleteDraft(id); err != nil {
		t.Fatalf("DeleteDraft: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	close(srv.release)
	<-done
	settle(t)
	srv.holds(t, map[string]string{})
	if len(removalQueue(t, app)) != 0 {
		t.Fatalf("still queued: %v", removalQueue(t, app))
	}
}

// openAsOwnKeyOnly decrypts a PGP/MIME message with the account's own key,
// failing the test unless that key is its one recipient.
func openAsOwnKeyOnly(t *testing.T, app *App, raw string) string {
	t.Helper()
	block, err := armor.Decode(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("no armored message in:\n%s\n%v", raw, err)
	}
	keys, err := app.keyStore()
	if err != nil {
		t.Fatalf("key store: %v", err)
	}
	own, err := keys.SenderKey("me@example.com")
	if err != nil {
		t.Fatalf("own key: %v", err)
	}
	md, err := openpgp.ReadMessage(block.Body, openpgp.EntityList{own}, nil, nil)
	if err != nil {
		t.Fatalf("decrypt with the own key: %v", err)
	}
	if len(md.EncryptedToKeyIds) != 1 || md.IsSigned {
		t.Errorf("encrypted to %d keys, signed %v; want the own key alone, unsigned", len(md.EncryptedToKeyIds), md.IsSigned)
	}
	body, err := io.ReadAll(md.UnverifiedBody)
	if err != nil {
		t.Fatalf("read decrypted body: %v", err)
	}
	return string(body)
}

// The server copy of an encrypted draft is encrypted to the account's own key,
// the one its local copy is sealed to, so a draft from an address with no key
// of its own still goes up, readable with the account's key.
func TestEncryptedDraftFromAnAliasUsesTheAccountKey(t *testing.T) {
	app, accountID, _ := draftTestApp(t, []string{"me@example.com"}, nil)
	msg, err := app.buildMessage(ComposeRequest{AccountID: accountID, Subject: "numbers", Text: "alias body"})
	if err != nil {
		t.Fatalf("buildMessage: %v", err)
	}
	msg.From.Email = "alias@example.com"
	msg.MessageID = "<alias-draft@example.com>"

	raw, messageID, err := app.encryptDraft(msg, "me@example.com")
	if err != nil || messageID != msg.MessageID {
		t.Fatalf("encryptDraft = %q, %v; want %q", messageID, err, msg.MessageID)
	}
	if body := openAsOwnKeyOnly(t, app, string(raw)); !strings.Contains(body, "alias body") {
		t.Errorf("decrypted draft lacks its body:\n%s", body)
	}
}

// Turning encryption on and off again gives every upload a Message-ID of its
// own and removes the copy before it, ciphertext as well as plaintext.
func TestDraftMessageIDAcrossProtection(t *testing.T) {
	app, accountID, client := draftTestApp(t, []string{"me@example.com"}, []string{"a@example.com"})
	var id int64
	for _, protection := range []string{protectionNone, protectionEncrypt, protectionNone} {
		id = saveDraft(t, app, id, ComposeRequest{AccountID: accountID, Protection: protection, To: addr("a@example.com"), Text: "body"})
		if _, err := app.UploadDraft(id); err != nil {
			t.Fatalf("upload with %q: %v", protection, err)
		}
		settle(t)
	}
	if len(client.drafts) != 3 {
		t.Fatalf("got %d uploads, want 3", len(client.drafts))
	}
	ids := []string{client.drafts[0].messageID, client.drafts[1].messageID, client.drafts[2].messageID}
	if ids[0] == ids[1] || ids[1] == ids[2] || ids[0] == ids[2] {
		t.Errorf("uploaded under %v, want a Message-ID each", ids)
	}
	for i, call := range client.drafts {
		if encrypted := strings.Contains(string(call.raw), "multipart/encrypted"); encrypted != (i == 1) {
			t.Errorf("upload %d encrypted = %v", i, encrypted)
		}
	}
	if !slices.Equal(client.removedDrafts, ids[:2]) {
		t.Errorf("removed %v, want %v", client.removedDrafts, ids[:2])
	}
	if stored := storedDrafts(t, app); stored[0].Request.DraftMessageID != ids[2] || !slices.Equal(copyIDs(stored[0]), ids[2:]) {
		t.Errorf("local draft links to %q with copies %v, want %q alone", stored[0].Request.DraftMessageID, copyIDs(stored[0]), ids[2])
	}
}

// Two compose panes autosaving at once must not lose a draft to each other's
// load, modify, write of the one drafts value.
func TestConcurrentDraftSavesKeepEveryDraft(t *testing.T) {
	app, _, accountID := pgpTestApp(t, nil, nil)

	const panes = 20
	var wg sync.WaitGroup
	for i := range panes {
		wg.Go(func() {
			if _, err := app.SaveDraft(0, ComposeRequest{AccountID: accountID, Subject: fmt.Sprintf("draft %d", i)}); err != nil {
				t.Errorf("SaveDraft: %v", err)
			}
		})
	}
	wg.Wait()
	if drafts := listDrafts(t, app); len(drafts) != panes {
		t.Fatalf("got %d drafts, want %d", len(drafts), panes)
	}
}

// Draft changes made while a sync runs queue one sync of the account, not one
// each. A draft that never reached the server has nothing to remove there.
func TestDraftRemovalsDuringASyncQueueOneSync(t *testing.T) {
	app, accountID, client := draftTestApp(t, nil, nil)
	var connects atomic.Int32
	app.newIMAPClient = func(pimap.Config) (mailClient, error) {
		connects.Add(1)
		return client, nil
	}

	syncMu.Lock()
	for _, messageID := range []string{"", "<d1@example.com>", "<d2@example.com>", "<d3@example.com>"} {
		if err := app.RemoveServerDraft(accountID, messageID); err != nil {
			syncMu.Unlock()
			t.Fatalf("RemoveServerDraft(%q): %v", messageID, err)
		}
	}
	syncMu.Unlock()
	settle(t)
	want := []string{"<d1@example.com>", "<d2@example.com>", "<d3@example.com>"}
	if got := connects.Load(); got != 4 || !slices.Equal(client.removedDrafts, want) {
		t.Fatalf("got %d connections removing %v, want 4 (three removals, then one sync) removing %v", got, client.removedDrafts, want)
	}
}

// Reading drafts can renumber and write them back, so it waits for a change
// already under way rather than writing over it.
func TestReadingDraftsWaitsForADraftChange(t *testing.T) {
	app, _, accountID := pgpTestApp(t, nil, nil)
	id := saveDraft(t, app, 0, ComposeRequest{AccountID: accountID, Subject: "one"})

	for name, read := range map[string]func() error{
		"ListDrafts": func() error { _, err := app.ListDrafts(); return err },
		"UnsealDraft": func() error {
			_, err := app.UnsealDraft(id, "")
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			app.draftsMu.Lock()
			done := make(chan error, 1)
			go func() { done <- read() }()
			select {
			case <-done:
				app.draftsMu.Unlock()
				t.Fatal("read the drafts while a change held them")
			case <-time.After(100 * time.Millisecond):
			}
			app.draftsMu.Unlock()
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}

// A Message-ID from the frontend ends up in a header of the server copy, so
// anything but a single <local@domain> is refused before it gets there, and
// one that trims to nothing removes nothing.
func TestDraftMessageIDsFromTheFrontendAreChecked(t *testing.T) {
	app, accountID, client := draftTestApp(t, nil, nil)
	for _, bad := range []string{
		"<a@example.com>\r\nBcc: x@example.com",
		"<a@example.com>\n",
		"a@example.com",
		"<a.example.com>",
		"<@example.com>",
		"<a@>",
		"<a@b@example.com>",
		"<a b@example.com>",
	} {
		if _, err := app.SaveDraft(0, ComposeRequest{AccountID: accountID, DraftMessageID: bad}); !errors.Is(err, errBadMessageID) {
			t.Errorf("SaveDraft(%q) = %v, want errBadMessageID", bad, err)
		}
		if err := app.RemoveServerDraft(accountID, bad); !errors.Is(err, errBadMessageID) {
			t.Errorf("RemoveServerDraft(%q) = %v, want errBadMessageID", bad, err)
		}
	}
	for _, empty := range []string{"", "<>", " "} {
		if err := app.RemoveServerDraft(accountID, empty); err != nil {
			t.Errorf("RemoveServerDraft(%q) = %v, want nil", empty, err)
		}
	}
	settle(t)
	if len(client.removedDrafts) != 0 {
		t.Fatalf("drafts removed on the server: %q", client.removedDrafts)
	}

	const good = "<a1.b-c@mail.example.com>"
	id := saveDraft(t, app, 0, ComposeRequest{AccountID: accountID, DraftMessageID: good})
	if drafts := listDrafts(t, app); len(drafts) != 1 || drafts[0].ID != id || drafts[0].Request.DraftMessageID != good {
		t.Fatalf("drafts = %+v, want one keeping %s", drafts, good)
	}
	if err := app.RemoveServerDraft(accountID, good); err != nil {
		t.Fatalf("RemoveServerDraft(%s): %v", good, err)
	}
	settle(t)
}

// The server copy of a sealed draft is titled "...", so the drafts list asks
// for the real subjects it can read without a passphrase prompt: one this
// session saved, or one unsealed with a key needing no passphrase. A draft that
// cannot be opened is left out, and so is a plain draft, whose server copy
// already has its subject.
func TestDraftSubjects(t *testing.T) {
	app, accountID, _ := draftTestApp(t, []string{"me@example.com"}, nil)
	sealed := func(subject string) int64 {
		id := saveDraft(t, app, 0, ComposeRequest{AccountID: accountID, Protection: protectionEncrypt, Subject: subject})
		if _, err := app.UploadDraft(id); err != nil {
			t.Fatalf("UploadDraft: %v", err)
		}
		return id
	}
	sealed("held this session")
	unsealed := sealed("unsealed again")
	saveDraft(t, app, 0, ComposeRequest{AccountID: accountID, Subject: "plain"})
	forgetHeld(t, app, unsealed)
	settle(t)

	drafts, err := app.loadDrafts()
	if err != nil {
		t.Fatalf("load drafts: %v", err)
	}
	unreadable := storedDraft{ID: 99, AccountID: accountID, Protection: protectionEncrypt, Sealed: "not armor",
		Request: ComposeRequest{AccountID: accountID, Protection: protectionEncrypt, DraftMessageID: "<locked@example.com>"}}
	other := storedDraft{ID: 100, AccountID: accountID + 1, Protection: protectionEncrypt, Sealed: drafts[0].Sealed,
		Request: ComposeRequest{AccountID: accountID + 1, Protection: protectionEncrypt, DraftMessageID: "<other@example.com>"}}
	if err := app.store.SetJSON(app.ctx, draftsKey, append(drafts, unreadable, other)); err != nil {
		t.Fatalf("seed drafts: %v", err)
	}

	got, err := app.DraftSubjects(accountID)
	if err != nil {
		t.Fatalf("DraftSubjects: %v", err)
	}
	want := map[string]string{
		strings.Trim(drafts[0].Request.DraftMessageID, "<>"): "held this session",
		strings.Trim(drafts[1].Request.DraftMessageID, "<>"): "unsealed again",
	}
	if !maps.Equal(got, want) {
		t.Fatalf("DraftSubjects = %v, want %v", got, want)
	}
}

// The draft copies queued for removal name accounts of this install, so a
// backup leaves them out, as it does the drafts themselves.
func TestBackupLeavesOutTheDraftRemovalQueue(t *testing.T) {
	app, accountID, _ := draftTestApp(t, nil, nil)
	if err := app.store.SetJSON(app.ctx, draftRemovalsKey, []draftCopy{{accountID, "<a@example.com>", "Drafts"}}); err != nil {
		t.Fatalf("seed queue: %v", err)
	}
	settings, err := app.exportSettings()
	if err != nil {
		t.Fatalf("exportSettings: %v", err)
	}
	if _, ok := settings[draftRemovalsKey]; ok {
		t.Fatal("the backup carries the draft removal queue")
	}
}

// forgetHeld drops the request a save of sealed draft id kept in memory, as a
// restart would.
func forgetHeld(t *testing.T, app *App, id int64) {
	t.Helper()
	i := slices.IndexFunc(storedDrafts(t, app), func(d storedDraft) bool { return d.ID == id })
	if i < 0 {
		t.Fatalf("no draft %d", id)
	}
	delete(app.sealedDrafts, draftKey{app.store.SettingsScope(), storedDrafts(t, app)[i].Token})
}

// workProfile adds a profile with settings of its own, and so drafts of its
// own, and returns it with the main profile's id.
func workProfile(t *testing.T, app *App) (storage.Profile, int64) {
	t.Helper()
	dto, err := app.CreateProfile(ProfileRequest{Name: "Work", StartSettings: startFresh})
	if err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}
	work, err := app.store.GetProfile(app.ctx, dto.ID)
	if err != nil {
		t.Fatalf("GetProfile: %v", err)
	}
	main, err := app.store.MainProfile(app.ctx)
	if err != nil {
		t.Fatalf("MainProfile: %v", err)
	}
	return *work, main.ID
}

// enter scopes the store to a profile, as a switch does.
func enter(t *testing.T, app *App, id, mainID int64) {
	t.Helper()
	p, err := app.store.GetProfile(app.ctx, id)
	if err != nil {
		t.Fatalf("GetProfile: %v", err)
	}
	app.store.UseProfile(*p, mainID)
}

// Each profile numbers its drafts from 1, so two profiles each have a sealed
// draft 1. What a save keeps in memory for one is never used for the other.
func TestSealedDraftsOfTwoProfilesStayApart(t *testing.T) {
	app, accountID, srv := oneServer(t)
	work, mainID := workProfile(t, app)
	sealed := func(text string) int64 {
		return saveDraft(t, app, 0, ComposeRequest{AccountID: accountID, Protection: protectionEncrypt, To: addr("a@example.com"), Subject: text, Text: text + " body"})
	}
	if id := sealed("home"); id != 1 {
		t.Fatalf("home draft id %d, want 1", id)
	}
	enter(t, app, work.ID, mainID)
	if id := sealed("work"); id != 1 {
		t.Fatalf("work draft id %d, want 1", id)
	}
	enter(t, app, mainID, mainID)

	messageID, err := app.UploadDraft(1)
	settle(t)
	if err != nil {
		t.Fatalf("UploadDraft: %v", err)
	}
	encryptedWith(t, app, srv.copies[messageID], "home body")
	subjects, err := app.DraftSubjects(accountID)
	if err != nil || !maps.Equal(subjects, map[string]string{strings.Trim(messageID, "<>"): "home"}) {
		t.Fatalf("DraftSubjects = %v, %v; want the home draft's alone", subjects, err)
	}
}

// An upload stays with the profile it started in: switching profiles while it
// is on the way to the server neither links it to the other profile's draft 1
// nor leaves its own draft unlinked.
func TestUploadStaysWithItsProfileAcrossASwitch(t *testing.T) {
	app, accountID, srv := oneServer(t)
	work, mainID := workProfile(t, app)
	enter(t, app, work.ID, mainID)
	saveDraft(t, app, 0, ComposeRequest{AccountID: accountID, Subject: "work", Text: "work body"})
	enter(t, app, mainID, mainID)
	id := saveDraft(t, app, 0, ComposeRequest{AccountID: accountID, Subject: "home", Text: "home body"})

	srv.block = true
	done := make(chan error, 1)
	var messageID string
	go func() {
		var err error
		messageID, err = app.UploadDraft(id)
		done <- err
	}()
	<-srv.entered
	enter(t, app, work.ID, mainID)
	close(srv.release)
	if err := <-done; err != nil {
		t.Fatalf("UploadDraft: %v", err)
	}
	settle(t)

	if stored := storedDrafts(t, app); len(stored[0].ServerCopies) != 0 {
		t.Fatalf("the work draft got server copies %v", stored[0].ServerCopies)
	}
	var home []storedDraft
	if err := app.store.GetJSONIn(app.ctx, mainID, draftsKey, &home); err != nil {
		t.Fatalf("read home drafts: %v", err)
	}
	if home[0].Request.DraftMessageID != messageID || !slices.Equal(copyIDs(home[0]), []string{messageID}) {
		t.Fatalf("home draft links %q with copies %v, want %q", home[0].Request.DraftMessageID, copyIDs(home[0]), messageID)
	}
}

// A draft deleted while its upload was under way, its id then given to a new
// draft, has the upload's copy neither recorded on nor settled onto the new
// one: drafts are told apart by a token of their own, not their id.
func TestUploadOfADeletedDraftNeverLandsOnItsSuccessor(t *testing.T) {
	app, accountID, _ := draftTestApp(t, nil, nil)
	id := saveDraft(t, app, 0, ComposeRequest{AccountID: accountID, Subject: "first"})
	scope, first := app.store.SettingsScope(), storedDrafts(t, app)[0].Token
	if err := app.DeleteDraft(id); err != nil {
		t.Fatalf("DeleteDraft: %v", err)
	}
	settle(t)
	if again := saveDraft(t, app, 0, ComposeRequest{AccountID: accountID, Subject: "second"}); again != id {
		t.Fatalf("new draft got id %d, want the freed %d", again, id)
	}

	sent := serverCopy{draftCopy{accountID, "<late@example.com>", "Drafts"}, false}
	if err := app.recordServerCopy(scope, first, sent); !errors.Is(err, ErrDraftNotFound) {
		t.Fatalf("recordServerCopy = %v, want ErrDraftNotFound", err)
	}
	if err := app.settleUpload(scope, first, &sent, true, false); err != nil {
		t.Fatalf("settleUpload: %v", err)
	}
	if stored := storedDrafts(t, app); len(stored[0].ServerCopies) != 0 || stored[0].Request.DraftMessageID != "" {
		t.Fatalf("the new draft took %+v", stored[0])
	}
	if !slices.Contains(removalQueue(t, app), sent.draftCopy) {
		t.Fatalf("queued %v, want the late copy among them", removalQueue(t, app))
	}
}

// Saving a draft set to encrypt queues its plaintext copies for removal in the
// same write, so quitting before the next upload leaves none behind.
func TestSealingADraftQueuesItsPlaintextCopies(t *testing.T) {
	app, accountID, client := draftTestApp(t, []string{"me@example.com"}, []string{"a@example.com"})
	client.failRemoveDrafts = 1
	_, _, plain := uploadPlainThenSeal(t, app, accountID)
	settle(t)

	if stored := storedDrafts(t, app); len(stored[0].ServerCopies) != 0 {
		t.Fatalf("the sealed draft still records %v", stored[0].ServerCopies)
	}
	if queued := removalQueue(t, app); len(queued) != 1 || queued[0].MessageID != plain {
		t.Fatalf("queued %v, want %q", queued, plain)
	}
	app.drainDraftRemovals()
	if !slices.Equal(client.removedDrafts, []string{plain}) || len(removalQueue(t, app)) != 0 {
		t.Fatalf("removed %v with %v queued, want %q removed", client.removedDrafts, removalQueue(t, app), plain)
	}
}

// A copy is removed from the folder it went up to, even after another folder
// was picked as Drafts.
func TestCopyIsRemovedFromTheFolderItWentTo(t *testing.T) {
	app, accountID, client := draftTestApp(t, []string{"me@example.com"}, []string{"a@example.com"})
	req := ComposeRequest{AccountID: accountID, To: addr("a@example.com"), Subject: "numbers", Text: "plain"}
	id := saveDraft(t, app, 0, req)
	plain, err := app.UploadDraft(id)
	settle(t)
	if err != nil {
		t.Fatalf("UploadDraft: %v", err)
	}
	picked := addFolder(t, app, accountID, "Entwürfe")
	if err := app.store.SetFolderRoleOverride(app.ctx, picked.ID, roleDrafts); err != nil {
		t.Fatalf("set role: %v", err)
	}

	if err := app.DeleteDraft(id); err != nil {
		t.Fatalf("DeleteDraft: %v", err)
	}
	settle(t)
	for i, messageID := range client.removedDrafts {
		if messageID == plain && client.removedFrom[i] == "Drafts" {
			return
		}
	}
	t.Fatalf("removed %v from %v, want %q from Drafts", client.removedDrafts, client.removedFrom, plain)
}

// Deleting a profile with settings of its own deletes its drafts, so their
// server copies are queued for removal first, in a queue every profile drains.
func TestDeletingAProfileRemovesItsDraftCopies(t *testing.T) {
	app, accountID, srv := oneServer(t)
	work, mainID := workProfile(t, app)
	enter(t, app, work.ID, mainID)
	id := saveDraft(t, app, 0, ComposeRequest{AccountID: accountID, Subject: "work", Text: "work body"})
	if _, err := app.UploadDraft(id); err != nil {
		t.Fatalf("UploadDraft: %v", err)
	}
	settle(t)
	srv.set(func(s *draftServer) { s.down = true })
	enter(t, app, mainID, mainID)

	if err := app.DeleteProfile(work.ID); err != nil {
		t.Fatalf("DeleteProfile: %v", err)
	}
	settle(t)
	srv.set(func(s *draftServer) { s.down = false })
	app.drainDraftRemovals()
	srv.holds(t, map[string]string{})
}

// A profile that kept drafts of its own and only later turned on sharing main's
// settings still has those drafts in its own rows, and deleting it deletes
// them, so their server copies are queued for removal too.
func TestDeletingAProfileThatSharesSettingsNowRemovesItsOwnDraftCopies(t *testing.T) {
	app, accountID, srv := oneServer(t)
	work, mainID := workProfile(t, app)
	enter(t, app, work.ID, mainID)
	id := saveDraft(t, app, 0, ComposeRequest{AccountID: accountID, Subject: "work", Text: "work body"})
	if _, err := app.UploadDraft(id); err != nil {
		t.Fatalf("UploadDraft: %v", err)
	}
	settle(t)
	enter(t, app, mainID, mainID)
	if _, err := app.UpdateProfile(ProfileRequest{ID: work.ID, Name: "Work", StartSettings: startShare}); err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}

	if err := app.DeleteProfile(work.ID); err != nil {
		t.Fatalf("DeleteProfile: %v", err)
	}
	settle(t)
	srv.holds(t, map[string]string{})
}

// The encrypted server copy decrypts to an entity carrying the draft's real
// headers as protected headers, so another device or client gets the subject
// and recipients back, and Pelton reading that entity shows the body alone,
// the headers being the part's own rather than text.
func TestEncryptedServerCopyCarriesProtectedHeaders(t *testing.T) {
	app, accountID, client := draftTestApp(t, []string{"me@example.com"}, []string{"a@example.com"})
	id := saveDraft(t, app, 0, ComposeRequest{
		AccountID: accountID, Protection: protectionEncrypt,
		To: addr("a@example.com"), Cc: addr("carol@example.com"), Bcc: addr("hidden@example.com"),
		Subject: "Zahlen für Q3", Text: "not for the server",
		InReplyTo: "<thread-2@example.com>", References: []string{"<thread-1@example.com>"},
	})
	if _, err := app.UploadDraft(id); err != nil {
		t.Fatalf("UploadDraft: %v", err)
	}
	settle(t)

	entity := openAsOwnKeyOnly(t, app, string(client.drafts[0].raw))
	inner, err := mail.ReadMessage(strings.NewReader(entity))
	if err != nil {
		t.Fatalf("parse the decrypted entity: %v", err)
	}
	if _, params, err := mime.ParseMediaType(inner.Header.Get("Content-Type")); err != nil || params["protected-headers"] != "v1" {
		t.Fatalf("root Content-Type %q, want protected-headers=v1", inner.Header.Get("Content-Type"))
	}
	subject, _ := new(mime.WordDecoder).DecodeHeader(inner.Header.Get("Subject"))
	got := []string{subject, inner.Header.Get("To"), inner.Header.Get("Cc"), inner.Header.Get("Bcc"), inner.Header.Get("In-Reply-To"), inner.Header.Get("References")}
	want := []string{"Zahlen für Q3", "a@example.com", "carol@example.com", "hidden@example.com", "<thread-2@example.com>", "<thread-1@example.com> <thread-2@example.com>"}
	if !slices.Equal(got, want) {
		t.Fatalf("protected headers %q, want %q", got, want)
	}

	plain, html := splitOpenedBody([]byte(entity))
	if !strings.Contains(plain, "not for the server") || strings.Contains(plain, "Subject") || strings.Contains(plain, "hidden@example.com") || html != "" {
		t.Fatalf("Pelton reads the entity as %q / %q, want the body alone", plain, html)
	}
}

// An encrypted draft with nothing to read in it, an empty body or attachments
// alone, opens as an empty body, not as its MIME source with the protected
// headers on show; decrypted text that is no MIME entity still shows as is.
func TestEncryptedDraftWithoutABodyOpensEmpty(t *testing.T) {
	for name, req := range map[string]ComposeRequest{
		"empty body":       {},
		"attachments only": {Attachments: []ComposeAttachment{{Filename: "a.bin", ContentType: "application/octet-stream", ContentBase64: "AAEC"}}},
	} {
		t.Run(name, func(t *testing.T) {
			app, accountID, client := draftTestApp(t, []string{"me@example.com"}, []string{"a@example.com"})
			req.AccountID, req.Protection, req.To, req.Bcc = accountID, protectionEncrypt, addr("a@example.com"), addr("hidden@example.com")
			id := saveDraft(t, app, 0, req)
			if _, err := app.UploadDraft(id); err != nil {
				t.Fatalf("UploadDraft: %v", err)
			}
			settle(t)
			entity := openAsOwnKeyOnly(t, app, string(client.drafts[0].raw))
			if plain, html := splitOpenedBody([]byte(entity)); plain != "" || html != "" {
				t.Fatalf("opens as %q / %q, want an empty body", plain, html)
			}
		})
	}
	const text = "Hello Bob,\nsee you tomorrow.\n"
	if plain, html := splitOpenedBody([]byte(text)); plain != text || html != "" {
		t.Fatalf("plain text opens as %q / %q, want it as it stands", plain, html)
	}
}
