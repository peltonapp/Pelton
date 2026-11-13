package desktop

import (
	"bytes"
	"errors"
	"fmt"
	"net/mail"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/outbox"
	"github.com/peltonapp/Pelton/internal/storage"
)

// draftTestApp is pgpTestApp with the account's IMAP connection faked and a
// Drafts folder synced.
func draftTestApp(t *testing.T) (*App, int64, *fakeIMAP) {
	t.Helper()
	app, _, accountID := pgpTestApp(t, nil, nil)
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

// A draft saved, uploaded, edited and uploaded again keeps one Message-ID, so
// the server holds one copy, and opening that copy from the Drafts folder gets
// the local draft back whole. Bcc is checked because a draft rebuilt from the
// server copy alone is where it would go missing.
func TestDraftRoundTripsThroughTheServer(t *testing.T) {
	app, accountID, client := draftTestApp(t)
	req := ComposeRequest{
		AccountID:  accountID,
		To:         []AddressDTO{{Email: "you@example.com"}},
		Bcc:        []AddressDTO{{Email: "hidden@example.com"}},
		Text:       "body",
		InReplyTo:  "parent@example.com",
		References: []string{"root@example.com", "parent@example.com"},
	}
	var id int64
	var messageID string
	var ids []int64
	var opened DraftDTO
	// The composer may not pass the draft Message-ID back after a reload.
	for _, subject := range []string{"first", "second"} {
		req.Subject = subject
		req.DraftMessageID = ""
		id = saveDraft(t, app, id, req)
		drafts := listDrafts(t, app)
		if len(drafts) != 1 || drafts[0].ID != id || drafts[0].Request.Subject != subject {
			t.Fatalf("saved drafts = %+v, want draft %d with subject %q", drafts, id, subject)
		}
		assertDraftReplyRequest(t, drafts[0].Request)
		got, err := app.UploadDraft(id)
		settle(t)
		if err != nil {
			t.Fatalf("upload %q: %v", subject, err)
		}
		if messageID != "" && got != messageID {
			t.Errorf("upload %q used %q, want %q", subject, got, messageID)
		}
		messageID = got
		drafts = listDrafts(t, app)
		if len(drafts) != 1 || messageID == "" || drafts[0].Request.DraftMessageID != messageID {
			t.Fatalf("local drafts = %+v, want one linked to uploaded %q", drafts, messageID)
		}
		assertDraftReplyRequest(t, drafts[0].Request)
		if strings.Trim(messageID, "<>") == "parent@example.com" {
			t.Fatal("draft Message-ID reused the parent Message-ID")
		}
		// The fake reports an empty mailbox during the queued sync, so seed
		// the cached server copies after each upload has settled.
		ids = draftsOnServer(t, app, accountID, messageID, "<webmail@example.com>")
		opened, err = app.OpenDraft(ids[0])
		if err != nil {
			t.Fatalf("OpenDraft: %v", err)
		}
		if opened.ID != id || opened.Request.Subject != subject || !slices.Equal(opened.Request.Bcc, req.Bcc) || opened.Request.DraftMessageID != messageID {
			t.Fatalf("opened %+v, want local draft %d with its subject, Bcc and Message-ID", opened, id)
		}
		assertDraftReplyRequest(t, opened.Request)
		req = opened.Request
	}
	if len(client.drafts) != 2 {
		t.Fatalf("got %d uploads, want 2", len(client.drafts))
	}
	for i, call := range client.drafts {
		header := draftReplyHeaders(t, call.raw)
		if call.messageID != messageID || header.Get("Message-ID") != messageID {
			t.Errorf("upload %d replaces %q with %q, want %q for both", i, call.messageID, header.Get("Message-ID"), messageID)
		}
		if !strings.Contains(header.Get("Bcc"), "hidden@example.com") {
			t.Errorf("upload %d lost the Bcc recipient: %q", i, header.Get("Bcc"))
		}
	}
	if other, err := app.OpenDraft(ids[1]); err != nil || other.ID != 0 {
		t.Fatalf("OpenDraft(written elsewhere) = %+v, %v, want an empty draft and no error", other, err)
	}

	app.queue = outbox.NewQueue(app.store)
	queuedID, err := app.SendMessage(opened.Request)
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	queued, err := app.queue.List(app.ctx)
	if err != nil {
		t.Fatalf("list outbox: %v", err)
	}
	if len(queued) != 1 || queued[0].ID != queuedID {
		t.Fatalf("outbox = %+v, want queued message %d", queued, queuedID)
	}
	header := draftReplyHeaders(t, queued[0].Raw)
	if sentID := header.Get("Message-ID"); sentID == "" || sentID == messageID || sentID == "<parent@example.com>" {
		t.Errorf("sent Message-ID = %q, want a fresh ID distinct from draft %q and parent", sentID, messageID)
	}
	if !slices.Contains(queued[0].Recipients, "hidden@example.com") {
		t.Errorf("outbox recipients = %v, want the Bcc recipient", queued[0].Recipients)
	}
	// The composer deletes the local draft after sending; the stored draft
	// Message-ID also identifies the server copy to remove.
	if err := app.DeleteDraft(id); err != nil {
		t.Fatalf("DeleteDraft: %v", err)
	}
	settle(t)
	if !slices.Equal(client.removedDrafts, []string{messageID}) {
		t.Fatalf("removed %v, want just %q", client.removedDrafts, messageID)
	}
	if drafts := listDrafts(t, app); len(drafts) != 0 {
		t.Fatalf("drafts after send/delete = %+v, want none", drafts)
	}
}

func assertDraftReplyRequest(t *testing.T, req ComposeRequest) {
	t.Helper()
	if req.InReplyTo != "parent@example.com" || !slices.Equal(req.References, []string{"root@example.com", "parent@example.com"}) {
		t.Errorf("draft threading = %q, %v, want bare parent and root/parent chain", req.InReplyTo, req.References)
	}
}

func draftReplyHeaders(t *testing.T, raw []byte) mail.Header {
	t.Helper()
	parsed, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("parse draft/outbox MIME: %v", err)
	}
	for field, want := range map[string]string{
		"In-Reply-To": "<parent@example.com>",
		"References":  "<root@example.com> <parent@example.com>",
	} {
		if got := parsed.Header[field]; len(got) != 1 || got[0] != want {
			t.Errorf("%s = %q, want exactly one %q", field, got, want)
		}
	}
	return parsed.Header
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

func TestUploadDraftKeepsEncryptedDraftsOffTheServer(t *testing.T) {
	tests := []struct {
		protection string
		sealed     string
		want       error
	}{
		{protectionEncrypt, "armored", ErrProtectedDraftStaysLocal},
		{protectionSignEncrypt, "", ErrProtectedDraftStaysLocal},
		{protectionSign, "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.protection, func(t *testing.T) {
			app, accountID, client := draftTestApp(t)
			draft := storedDraft{
				ID: 1,
				Request: ComposeRequest{
					AccountID: accountID, Subject: "secret", Protection: tt.protection,
					InReplyTo: "parent@example.com", References: []string{"root@example.com", "parent@example.com"},
				},
				Sealed:     tt.sealed,
				AccountID:  accountID,
				Protection: tt.protection,
			}
			if err := app.store.SetJSON(app.ctx, draftsKey, []storedDraft{draft}); err != nil {
				t.Fatalf("seed draft: %v", err)
			}

			messageID, err := app.UploadDraft(1)
			settle(t)
			if !errors.Is(err, tt.want) {
				t.Fatalf("UploadDraft = %v, want %v", err, tt.want)
			}
			wantUploads := 0
			if tt.want == nil {
				wantUploads = 1
			}
			if len(client.drafts) != wantUploads {
				t.Errorf("got %d uploads with protection %q, want %d", len(client.drafts), tt.protection, wantUploads)
			}
			if tt.want != nil && messageID != "" {
				t.Errorf("refused upload returned Message-ID %q", messageID)
			}
			for _, call := range client.drafts {
				draftReplyHeaders(t, call.raw)
			}
		})
	}
}

// A draft uploaded in the clear and then switched to encrypt must not leave its
// plaintext copy on the server, even when the composer's own removal never
// happened (offline, or the session failed): the sealed draft keeps the
// Message-ID, its save removes the copy, and so does deleting it after send.
func TestEncryptingAnUploadedDraftRemovesThePlaintextCopy(t *testing.T) {
	app, _, accountID := pgpTestApp(t, []string{"me@example.com"}, []string{"a@example.com"})
	client := &fakeIMAP{}
	useFake(t, app, accountID, client)
	addFolder(t, app, accountID, "Drafts", `\Drafts`)

	req := ComposeRequest{AccountID: accountID, To: addr("a@example.com"), Subject: "numbers", Text: "plain"}
	id := saveDraft(t, app, 0, req)
	messageID, err := app.UploadDraft(id)
	settle(t)
	if err != nil || messageID == "" {
		t.Fatalf("UploadDraft = %q, %v", messageID, err)
	}

	// the composer has lost the Message-ID, as when its removal failed.
	req.Protection = protectionEncrypt
	saveDraft(t, app, id, req)
	settle(t)
	if !slices.Equal(client.removedDrafts, []string{messageID}) {
		t.Fatalf("after the encrypted save removed %v, want %q", client.removedDrafts, messageID)
	}
	var stored []storedDraft
	if err := app.store.GetJSON(app.ctx, draftsKey, &stored); err != nil {
		t.Fatalf("read drafts: %v", err)
	}
	if len(stored) != 1 || stored[0].Sealed == "" || stored[0].Request.DraftMessageID != messageID {
		t.Fatalf("stored %+v, want one sealed draft still linked to %q", stored, messageID)
	}

	if err := app.DeleteDraft(id); err != nil {
		t.Fatalf("DeleteDraft: %v", err)
	}
	settle(t)
	if !slices.Equal(client.removedDrafts, []string{messageID, messageID}) {
		t.Fatalf("after delete removed %v, want %q tried again", client.removedDrafts, messageID)
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
	app, accountID, client := draftTestApp(t)
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
	app, accountID, client := draftTestApp(t)
	for _, bad := range []string{
		"<a@example.com>\r\nBcc: x@example.com",
		"<a@example.com>\n",
		"a@example.com",
		"<a.example.com>",
		"<@example.com>",
		"<a@>",
		"<a@b@example.com>",
		"<a b@example.com>",
		"<<a@example.com>>",
		"<a@example.com",
		"a@example.com>",
		"<a\t@example.com>",
		"<a\x00@example.com>",
		"<a\x7f@example.com>",
	} {
		want := fmt.Sprintf("pelton: a draft Message-ID must have the form <local@domain>: %q", bad)
		if _, err := app.SaveDraft(0, ComposeRequest{AccountID: accountID, DraftMessageID: bad}); !errors.Is(err, errBadMessageID) || err.Error() != want {
			t.Errorf("SaveDraft(%q) = %v, want errBadMessageID", bad, err)
		}
		if err := app.RemoveServerDraft(accountID, bad); !errors.Is(err, errBadMessageID) || err.Error() != want {
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

	for _, good := range []string{
		"<a1.b-c@mail.example.com>",
		"<" + strings.Repeat("a", 1000) + "@example.com>",
		`<"quoted"@example.com>`,
		"<é@example.com>",
		"<a@exämple.com>",
		"<.a@example.com>",
		"<a.@example.com>",
		"<a..b@example.com>",
		"<a@.example.com>",
		"<a@example..com>",
		"<a@example.com.>",
	} {
		id := saveDraft(t, app, 0, ComposeRequest{AccountID: accountID, DraftMessageID: good})
		drafts := listDrafts(t, app)
		i := slices.IndexFunc(drafts, func(d DraftDTO) bool { return d.ID == id })
		if i < 0 || drafts[i].Request.DraftMessageID != good {
			t.Fatalf("drafts = %+v, want draft %d keeping %s", drafts, id, good)
		}
		if err := app.RemoveServerDraft(accountID, good); err != nil {
			t.Fatalf("RemoveServerDraft(%s): %v", good, err)
		}
		settle(t)
		if got := client.removedDrafts; len(got) != 1 || got[0] != good {
			t.Fatalf("removed %v, want exactly %q", got, good)
		}
		client.removedDrafts = nil
	}
}

func TestOpenDraftRejectsMalformedMessageIDs(t *testing.T) {
	for _, tt := range []struct {
		name, cached, local string
		wantMatch           bool
		prependInvalid      bool
	}{
		{"bare cache bracketed local", "parent@example.com", "<parent@example.com>", true, false},
		{"bracketed cache bracketed local", "<parent@example.com>", "<parent@example.com>", true, false},
		{"bare cache bare local", "parent@example.com", "parent@example.com", true, false},
		{"bracketed cache bare local", "<parent@example.com>", "parent@example.com", true, false},
		{"nested cache", "<<parent@example.com>>", "<parent@example.com>", false, false},
		{"nested local", "parent@example.com", "<<parent@example.com>>", false, false},
		{"invalid cache and local", "<<parent@example.com>>", "<<parent@example.com>>", false, false},
		{"empty cache and invalid local", "", "<<parent@example.com>>", false, false},
		{"unicode", "draft@bücher.de", "<draft@bücher.de>", true, false},
		{"trailing dot", "<draft@example.com.>", "draft@example.com.", true, false},
		{"no punycode conversion", "draft@xn--bcher-kva.de", "<draft@bücher.de>", false, false},
		{"case sensitive", "Parent@example.com", "<parent@example.com>", false, false},
		{"empty both", "", "", false, false},
		{"empty wrappers", "<>", "<>", false, false},
		{"unclosed cache", "<parent@example.com", "<parent@example.com>", false, false},
		{"unopened local", "parent@example.com", "parent@example.com>", false, false},
		{"skip invalid candidate", "parent@example.com", "<parent@example.com>", true, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			app, db, accountID := pgpTestApp(t, nil, nil)
			folder := addFolder(t, app, accountID, "Drafts", `\Drafts`)
			localID := saveDraft(t, app, 0, ComposeRequest{
				AccountID: accountID, Subject: "editable local draft", DraftMessageID: "<parent@example.com>",
			})
			var drafts []storedDraft
			if err := db.GetJSON(app.ctx, draftsKey, &drafts); err != nil {
				t.Fatalf("read local draft: %v", err)
			}
			drafts[0].Request.DraftMessageID = tt.local
			if tt.prependInvalid {
				invalid := drafts[0]
				invalid.ID = localID + 1
				invalid.Request.DraftMessageID = "<<parent@example.com>>"
				drafts = append([]storedDraft{invalid}, drafts...)
			}
			if err := db.SetJSON(app.ctx, draftsKey, drafts); err != nil {
				t.Fatalf("seed legacy local_drafts: %v", err)
			}
			id, err := db.InsertMessage(app.ctx, &storage.Message{
				AccountID: accountID, FolderID: folder.ID, UID: 1, MessageID: tt.cached,
				Subject: "server copy", Date: time.Now(),
			})
			if err != nil {
				t.Fatalf("insert cached draft: %v", err)
			}
			got, err := app.OpenDraft(id)
			if err != nil {
				t.Fatalf("OpenDraft: %v", err)
			}
			if tt.wantMatch {
				if got.ID != localID || got.Request.Subject != "editable local draft" {
					t.Errorf("OpenDraft = %+v, want editable local draft %d", got, localID)
				}
			} else if !reflect.DeepEqual(got, DraftDTO{}) {
				t.Errorf("OpenDraft = %+v, want DraftDTO{}", got)
			}
		})
	}
}
