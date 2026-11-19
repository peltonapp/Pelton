package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/mailvault"
	"github.com/peltonapp/Pelton/internal/mcpserver"
	"github.com/peltonapp/Pelton/internal/scripting"
	"github.com/peltonapp/Pelton/internal/storage"
)

// messageFields are field names that carry what a vault seals. A type that
// has one of them, at any depth, returns message data.
var messageFields = map[string]bool{
	"Subject": true, "Snippet": true, "FromAddress": true, "FromName": true, "From": true,
	"BodyText": true, "BodyHTML": true, "BodyPlain": true, "Body": true, "Preview": true,
	"Quote": true, "Filename": true, "To": true, "Cc": true, "Headers": true, "HTML": true,
}

func carriesMessageData(t reflect.Type, seen map[reflect.Type]bool) bool {
	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice || t.Kind() == reflect.Array || t.Kind() == reflect.Map {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct || seen[t] {
		return false
	}
	seen[t] = true
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if messageFields[f.Name] || carriesMessageData(f.Type, seen) {
			return true
		}
	}
	return false
}

var mailish = regexp.MustCompile(`(?i)message|thread|mail|attachment|search|sender|newsletter|insight|draft|source|promise|meeting|unread|split|note|count|invite|quote|passport|shield|privacy|trust|unsubscribe|tracker`)

var (
	errorType = reflect.TypeOf((*error)(nil)).Elem()
	boolType  = reflect.TypeOf(true)
)

// messageMethods lists the exported methods of v's type that return data
// (anything but an error or a bool) shaped like a message, or that return
// data and are named for mail, which catches a body handed back as a string
// or a list of message ids.
func messageMethods(v any) []string {
	typ := reflect.TypeOf(v)
	var out []string
	for i := 0; i < typ.NumMethod(); i++ {
		m := typ.Method(i)
		data, shaped := false, false
		for j := 0; j < m.Type.NumOut(); j++ {
			o := m.Type.Out(j)
			if o == errorType || o == boolType {
				continue
			}
			data = true
			shaped = shaped || carriesMessageData(o, map[reflect.Type]bool{})
		}
		if shaped || (data && mailish.MatchString(m.Name)) {
			out = append(out, m.Name)
		}
	}
	sort.Strings(out)
	return out
}

const (
	vaultMarkSubject = "VAULTMARKSUBJECTq7"
	vaultMarkBody    = "VAULTMARKBODYw3"
	vaultMarkFrom    = "vaultmarkfrom9@example.org"
	vaultMarkFile    = "VAULTMARKFILE4.pdf"
)

var vaultMarks = []string{vaultMarkSubject, vaultMarkBody, "vaultmarkfrom9", "VAULTMARKFILE4"}

type vaultFixture struct {
	app       *App
	account   int64
	inbox     int64
	secret    int64
	vault     int64
	sealedID  int64
	sealedAtt int64
	thread    int64
	clearID   int64
}

var testVaultKDF = mailvault.KDFParams{Time: 1, MemoryKiB: 64, Threads: 1}

// newVaultApp builds an app with a vaulted folder holding one message that
// arrived while the vault was locked, and an ordinary inbox message.
func newVaultApp(t *testing.T) *vaultFixture {
	t.Helper()
	ctx, stopBackground := testContext(t)
	dir := t.TempDir()
	store, err := storage.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	t.Cleanup(stopBackground)
	if err := store.RunMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	a := &App{ctx: ctx, store: store, log: slog.New(slog.DiscardHandler), dataDir: dir}
	a.mailVault.kdf = &testVaultKDF
	a.initMailVault()
	f := &vaultFixture{app: a}
	if f.account, err = store.CreateAccount(ctx, &storage.Account{Email: "me@example.com", IMAPHost: "imap.example.com", IMAPPort: 993}); err != nil {
		t.Fatal(err)
	}
	if f.inbox, err = store.CreateFolder(ctx, &storage.Folder{AccountID: f.account, Name: "INBOX", IMAPPath: "INBOX"}); err != nil {
		t.Fatal(err)
	}
	if f.secret, err = store.CreateFolder(ctx, &storage.Folder{AccountID: f.account, Name: "Secret", IMAPPath: "Secret"}); err != nil {
		t.Fatal(err)
	}
	created, err := a.CreateMailVault(MailVaultCreateRequest{Name: "Private", Passphrase: "correct horse battery",
		Members: []MailVaultMemberDTO{{AccountID: f.account, FolderID: f.secret}}})
	if err != nil {
		t.Fatal(err)
	}
	f.vault = created.ID
	waitForVaultState(t, a, f.vault, storage.MailVaultReady)
	a.LockAllMailVaults()

	sealed := &storage.Message{AccountID: f.account, FolderID: f.secret, UID: 1, MessageID: "<sealed@example.org>",
		Subject: vaultMarkSubject, FromAddress: vaultMarkFrom, FromName: "Vault Mark", ToAddresses: "me@example.com",
		BodyPlain: vaultMarkBody, BodyHTML: "<p>" + vaultMarkBody + "</p>", Date: time.Now().Add(-time.Hour),
		ListUnsubscribe: "<mailto:unsubscribe@example.org>"}
	if f.sealedID, err = store.InsertMessageWithAttachments(ctx, sealed, []storage.IncomingAttachment{
		{Filename: vaultMarkFile, ContentType: "application/pdf", Content: strings.NewReader("pdf bytes " + vaultMarkBody)},
	}); err != nil {
		t.Fatal(err)
	}
	f.thread = sealed.ThreadID
	if f.clearID, err = store.InsertMessage(ctx, &storage.Message{AccountID: f.account, FolderID: f.inbox, UID: 1,
		MessageID: "<clear@example.org>", Subject: "Lunch", FromAddress: "friend@example.org", BodyPlain: "see you",
		Date: time.Now()}); err != nil {
		t.Fatal(err)
	}
	var atts []storage.Attachment
	f.app.mailVault.kdf = &testVaultKDF
	if res := a.UnlockMailVault(f.vault, "correct horse battery"); !res.OK {
		t.Fatalf("unlock: %+v", res)
	}
	if atts, err = store.ListAttachments(a.mailCtx(), f.sealedID); err != nil || len(atts) != 1 {
		t.Fatalf("vault attachment while unlocked: %+v %v", atts, err)
	}
	f.sealedAtt = atts[0].ID
	a.LockAllMailVaults()
	return f
}

func waitForVaultState(t *testing.T, a *App, id int64, state string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		v, err := a.store.GetMailVault(a.ctx, id)
		a.mailVault.mu.Lock()
		running := a.mailVault.jobs[id]
		a.mailVault.mu.Unlock()
		if err == nil && v.State == state && !running {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("vault %d never reached %s", id, state)
}

// leak fails when the JSON of v holds a vault marker.
func leak(t *testing.T, name string, v any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("%s: marshal: %v", name, err)
	}
	for _, m := range vaultMarks {
		if strings.Contains(strings.ToLower(string(b)), strings.ToLower(m)) {
			t.Errorf("%s returned sealed content %q while the vault is locked", name, m)
		}
	}
}

// vaultProbes call each bound method that returns mail, aimed at the vault
// where it takes an id, while the vault is locked.
func vaultProbes(f *vaultFixture) map[string]func() (any, error) {
	a := f.app
	folderReq := ListMessagesRequest{Kind: "folder", FolderID: f.secret, Limit: 50}
	inboxReq := ListMessagesRequest{Kind: "folder", FolderID: f.inbox, Limit: 50}
	unified := ListMessagesRequest{Kind: "view", View: "inbox", Limit: 50}
	search := SearchRequestDTO{Query: vaultMarkSubject, Limit: 50}
	wrap := func(v any, err error) (any, error) { return v, err }
	return map[string]func() (any, error){
		"GetMessage":       func() (any, error) { return wrap(a.GetMessage(f.sealedID)) },
		"GetMessageHTML":   func() (any, error) { return wrap(a.GetMessageHTML(f.sealedID, true, true)) },
		"GetMessageSource": func() (any, error) { return wrap(a.GetMessageSource(f.sealedID)) },
		"GetThread":        func() (any, error) { return wrap(a.GetThread(f.thread)) },
		"ListMessages": func() (any, error) {
			x, err := a.ListMessages(folderReq)
			y, _ := a.ListMessages(inboxReq)
			z, _ := a.ListMessages(unified)
			return []any{x, y, z}, err
		},
		"ListThreads": func() (any, error) {
			x, err := a.ListThreads(folderReq)
			z, _ := a.ListThreads(unified)
			return []any{x, z}, err
		},
		"MessageIDs":         func() (any, error) { return wrap(a.MessageIDs(folderReq)) },
		"SearchMessageIDs":   func() (any, error) { return wrap(a.SearchMessageIDs(search)) },
		"Search":             func() (any, error) { return wrap(a.Search(search)) },
		"SplitCounts":        func() (any, error) { return wrap(a.SplitCounts(folderReq)) },
		"ReadAttachment":     func() (any, error) { return wrap(a.ReadAttachment(f.sealedID, f.sealedAtt)) },
		"FetchAttachment":    func() (any, error) { return wrap(a.FetchAttachment(f.sealedAtt)) },
		"QueryAttachments":   func() (any, error) { return wrap(a.QueryAttachments(AttachmentQueryRequest{})) },
		"SaveAttachment":     func() (any, error) { return wrap(a.SaveAttachment(f.sealedID, f.sealedAtt)) },
		"SaveAllAttachments": func() (any, error) { return wrap(a.SaveAllAttachments(f.sealedID)) },
		"SenderOverview": func() (any, error) {
			o, err := a.SenderOverview(vaultMarkFrom)
			o.Address = ""
			return o, err
		},
		"ListNewsletters":    func() (any, error) { return wrap(a.ListNewsletters()) },
		"ListNewsletterMail": func() (any, error) { return wrap(a.ListNewsletterMail(50, 0, false)) },
		"CountNewsletterMail": func() (any, error) {
			n, err := a.CountNewsletterMail()
			if n != 0 {
				return nil, fmt.Errorf("counted %d newsletter messages, all of them sealed", n)
			}
			return n, err
		},
		"NewsletterMessageIDs": func() (any, error) {
			return wrap(a.NewsletterMessageIDs(NewsletterSelectionDTO{Emails: []string{"vaultmarkfrom9@example.org"}}))
		},
		"InsightsPeople":                 func() (any, error) { return wrap(a.InsightsPeople(30)) },
		"InsightsStorage":                func() (any, error) { return wrap(a.InsightsStorage()) },
		"InsightsVolume":                 func() (any, error) { return wrap(a.InsightsVolume(30)) },
		"PromisesForMessage":             func() (any, error) { return wrap(a.PromisesForMessage(f.sealedID)) },
		"FindPromises":                   func() (any, error) { return wrap(a.FindPromises(f.sealedID)) },
		"ListPromises":                   func() (any, error) { return wrap(a.ListPromises()) },
		"ScanMessage":                    func() (any, error) { return wrap(a.ScanMessage(f.sealedID, true, true)) },
		"ScanAttachment":                 func() (any, error) { return wrap(a.ScanAttachment(f.sealedID, f.sealedAtt)) },
		"ExportMessageEML":               func() (any, error) { return wrap(a.ExportMessageEML(f.sealedID)) },
		"FindMessageByLink":              func() (any, error) { return wrap(a.FindMessageByLink("message:<sealed@example.org>")) },
		"MailEventSource":                func() (any, error) { return wrap(a.MailEventSource(f.sealedID)) },
		"CreateEventDraftFromMessage":    func() (any, error) { return wrap(a.CreateEventDraftFromMessage(f.sealedID)) },
		"ParseInvite":                    func() (any, error) { return wrap(a.ParseInvite(f.sealedID)) },
		"ApplyInvite":                    func() (any, error) { return wrap(a.ApplyInvite(f.sealedID)) },
		"AnswerInvite":                   func() (any, error) { return wrap(a.AnswerInvite(InviteAnswerRequest{MessageID: f.sealedID})) },
		"MoveMessage":                    func() (any, error) { return wrap(a.MoveMessage(f.sealedID, f.inbox)) },
		"ArchiveMessage":                 func() (any, error) { return wrap(a.ArchiveMessage(f.sealedID)) },
		"ArchiveMessages":                func() (any, error) { return wrap(a.ArchiveMessages([]int64{f.sealedID})) },
		"DeleteMessages":                 func() (any, error) { return wrap(a.DeleteMessages([]int64{f.sealedID})) },
		"AddMailToProject":               func() (any, error) { return wrap(a.AddMailToProject(0, []int64{f.sealedID}, "")) },
		"GetMessageHTMLBlockingTrackers": func() (any, error) { return wrap(a.GetMessageHTMLBlockingTrackers(f.sealedID)) },
		"GetSenderPassport":              func() (any, error) { return wrap(a.GetSenderPassport(f.sealedID)) },
		"GetPrivacyShield":               func() (any, error) { return wrap(a.GetPrivacyShield(f.sealedID)) },
		"PrivacyReport": func() (any, error) {
			if err := a.store.Set(a.ctx, settingPrivacyCounters, "true"); err != nil {
				return nil, err
			}
			a.privacyPass([]int64{f.sealedID})
			return wrap(a.PrivacyReport(30))
		},
		"TestScriptMatch": func() (any, error) {
			return wrap(a.TestScriptMatch(scripting.MatchRequest{Folder: "Secret", Limit: 50, Pattern: ".", Field: "subject"}))
		},
	}
}

// vaultExempt are the methods that return data shaped like mail but cannot
// reach a vault's, each with the reason. A method that starts returning
// message data fails TestLockedVaultIsAbsentEverywhere until it is probed or
// listed here.
var vaultExempt = map[string]string{
	// accounts, servers and settings: no message content.
	"AccountOAuthProvider": "account settings", "AccountSyncStates": "sync state per account",
	"AccountsNeedingPassword": "account list", "AddCalendarAccount": "calendar account", "AddOAuthAccount": "account",
	"AddPasswordAccount": "account", "AllAccounts": "account list", "CheckAccountPassword": "account",
	"ListAccounts": "account list", "ListCalendarAccounts": "calendar accounts", "UpdateAccount": "account",
	"UpdateCalendarAccount": "calendar account", "ProbeAccountCertificates": "server certificates",
	"TestConnection": "server check", "ImportThunderbirdAccounts": "account import", "DefaultMailClientStatus": "os setting",
	"GetAccountPGPKey": "the account's own key", "GetAccountSignatures": "the user's signatures",
	"ChooseArchiveExportFolder": "a folder picker", "ChooseMailFiles": "a file picker",
	// other spaces with content of their own.
	"ChatRooms": "chat", "ChatTimeline": "chat", "GetFeedEntry": "feeds", "ListFeedEntries": "feeds",
	"CheckSieveScript": "server filters", "RecheckSieve": "server filters", "RestoreSievePrevious": "server filters",
	"SaveSieveRules": "server filters", "SaveSieveScript": "server filters", "SaveSieveVacation": "server filters",
	"SetSieveConnection": "server filters", "SieveState": "server filters",
	"ConfirmThemeImport": "themes", "GetThemeDraft": "themes", "ListThemes": "themes", "PreviewThemeImport": "themes",
	"SaveCustomTheme": "themes", "CompileScriptBlocks": "script source", "ScriptLibraryNote": "script source",
	"ScriptRegistry": "script list", "ListSnippets": "the user's snippets", "SaveSnippet": "the user's snippets",
	"CreateProjectNote": "the user's notes", "CreateProjectVaultNote": "the user's notes", "ListProjectNotes": "the user's notes",
	"ListProjectVaultNotes": "the user's notes", "TrashProjectNote": "the user's notes", "UpdateProjectNote": "the user's notes",
	"VaultSaveAttachment": "the notes folder", "VaultSearch": "the notes folder", "VaultSnapshot": "the notes folder",
	"GetNote": "the user's own note text, never mail content", "SaveNote": "the user's own note text",
	// what the user writes: drafts and the outbox are not in mail vaults.
	"CancelOutboxSend": "outbox", "EditOutboxMessage": "outbox", "ListDrafts": "drafts", "OpenDraft": "drafts",
	"SaveDraft": "drafts", "UploadDraft": "drafts", "UnsealDraft": "drafts", "SendMessage": "sending",
	"AgentProposals": "messages an agent proposed, not received mail", "ConsumePendingMailto": "a mailto link",
	"ListFollowUps": "the user's own sent mail",
	// the vault's own controls.
	"ChangeMailVaultPassphrase": "vault control", "CreateMailVault": "vault control", "ListMailVaults": "vault control",
	"RecoverMailVault": "vault control", "UnlockMailVault": "vault control",
	// reads that go through storage without an audience, which never sees vault mail.
	"PrepareMeeting":       "meeting prep reads with notVaulted",
	"ListProjectThreads":   "threads through visible folders of a.ctx",
	"ListProjectMailMarks": "ids of mail linked to projects; linking vault mail is refused",
	"ListTaskMailMarks":    "ids of mail linked to tasks; linking vault mail is refused",
	"AcceptPromise":        "promise rows; vault mail is never scanned and sealing purges its promises",
	"QuietPromiseSender":   "promise settings", "GetPromiseSettings": "promise settings",
	"SearchAddresses": "address book; harvesting skips vault mail",
	"ListVIPSenders":  "the user's VIP list", "ListSenderReading": "per-sender reading settings",
	"ResetSenderReading": "per-sender reading settings", "SenderPhotos": "avatars by address",
	"FetchOlderMessages": "fetches from the server into storage, which seals vaulted folders on insert",
	"ExportMessagesEML":  "asks for a folder first, then reads each message without an audience",
	"SaveAttachmentsTo":  "asks for a folder first, then resolves each attachment through findAttachment",
}

func TestLockedVaultIsAbsentEverywhere(t *testing.T) {
	f := newVaultApp(t)
	probes := vaultProbes(f)
	var unclassified []string
	for _, name := range messageMethods(f.app) {
		_, probed := probes[name]
		_, exempt := vaultExempt[name]
		if !probed && !exempt {
			unclassified = append(unclassified, name)
		}
	}
	if len(unclassified) > 0 {
		t.Fatalf("these bound methods return mail but are neither probed nor exempt; add a probe to vaultProbes:\n%s",
			strings.Join(unclassified, "\n"))
	}
	for name := range vaultExempt {
		if _, ok := reflect.TypeOf(f.app).MethodByName(name); !ok {
			t.Errorf("vaultExempt names %s, which is gone", name)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(probes)) {
		v, _ := probes[name]()
		leak(t, name, v)
	}

	ids, err := f.app.MessageIDs(ListMessagesRequest{Kind: "folder", FolderID: f.secret})
	if err != nil || len(ids.IDs) != 0 {
		t.Fatalf("select-all in a locked vault folder found %v %v", ids, err)
	}
	if _, err := f.app.GetMessage(f.sealedID); err == nil {
		t.Fatal("a locked vault message opened by id")
	}
	if _, err := f.store().GetMessage(f.app.mailCtx(), f.sealedID); err == nil {
		t.Fatal("storage handed out a locked vault message")
	}
}

func (f *vaultFixture) store() *storage.DB { return f.app.store }

// TestUnlockedVaultIsOpenOnlyToItsAudience checks that unlocking brings the
// vault into the mail space and nowhere else: scripts and MCP stay out until
// their switch is on.
func TestUnlockedVaultIsOpenOnlyToItsAudience(t *testing.T) {
	f := newVaultApp(t)
	a := f.app
	if res := a.UnlockMailVault(f.vault, "correct horse battery"); !res.OK {
		t.Fatalf("unlock: %+v", res)
	}
	detail, err := a.GetMessage(f.sealedID)
	if err != nil || !strings.Contains(detail.Subject, vaultMarkSubject) {
		t.Fatalf("unlocked vault message = %+v %v", detail, err)
	}
	list, err := a.ListMessages(ListMessagesRequest{Kind: "folder", FolderID: f.secret, Limit: 50})
	if err != nil || len(list.Messages) != 1 {
		t.Fatalf("unlocked vault folder lists %+v %v", list, err)
	}
	found, err := a.Search(SearchRequestDTO{Query: strings.ToLower(vaultMarkBody), Limit: 20})
	if err != nil && err != errSearchUnavailable {
		t.Fatal(err)
	}
	if err == nil && len(found.Messages) != 1 {
		t.Fatalf("search over an unlocked vault found %d", len(found.Messages))
	}
	if hits := a.searchMailVaults(SearchRequestDTO{Query: strings.ToLower(vaultMarkBody)}, 10); len(hits) != 1 {
		t.Fatalf("vault search found %d", len(hits))
	}
	for _, name := range []string{"SenderOverview", "ListNewsletters", "InsightsPeople", "QueryAttachments", "ListPromises"} {
		v, _ := vaultProbes(f)[name]()
		leak(t, name+" (unlocked)", v)
	}

	sm := &scriptMail{app: a}
	if _, err := sm.Message(context.Background(), f.sealedID); err == nil {
		t.Fatal("a script read vault mail without the switch")
	}
	if _, err := sm.Body(context.Background(), f.sealedID); err == nil {
		t.Fatal("a script read a vault body without the switch")
	}
	recent, err := sm.Recent(context.Background(), "Secret", f.account, 10)
	if err != nil || len(recent) != 0 {
		t.Fatalf("scripts listed a vault folder: %+v %v", recent, err)
	}
	mb := &mcpMailbox{app: a}
	if _, err := mb.GetMessage(context.Background(), f.sealedID); err == nil {
		t.Fatal("MCP read vault mail without the switch")
	}
	if got, _ := mb.ListMessages(context.Background(), f.secret, 10); len(got) != 0 {
		t.Fatal("MCP listed a vault folder without the switch")
	}
	w := &mcpWriter{app: a}
	if err := w.Flag(context.Background(), f.sealedID, true); err == nil {
		t.Fatal("MCP flagged vault mail without the switch")
	}

	st := scriptTrust{app: a}
	if _, err := st.Passport(context.Background(), f.sealedID); err == nil {
		t.Fatal("a script read the passport of vault mail")
	}
	if _, err := st.Shield(context.Background(), f.sealedID); err == nil {
		t.Fatal("a script read the shield of vault mail")
	}
	if err := st.Unsubscribe(context.Background(), f.sealedID); err == nil {
		t.Fatal("a script unsubscribed through vault mail")
	}

	if err := a.UpdateMailVaultSettings(MailVaultSettingsDTO{ID: f.vault, Name: "Private", AllowScripts: true}); err != nil {
		t.Fatal(err)
	}
	if msg, err := sm.Message(context.Background(), f.sealedID); err != nil || msg.Subject != vaultMarkSubject {
		t.Fatalf("scripts with the switch on: %+v %v", msg, err)
	}
	if _, err := st.Passport(context.Background(), f.sealedID); err == nil {
		t.Fatal("vault mail has no passport, even for scripts the vault admits")
	}
	if html, err := a.GetMessageHTMLBlockingTrackers(f.sealedID); err != nil || !strings.Contains(html, vaultMarkBody) {
		t.Fatalf("the tracker-blocking reader cannot show unlocked vault mail: %q %v", html, err)
	}
	if _, err := mb.GetMessage(context.Background(), f.sealedID); err == nil {
		t.Fatal("the scripts switch opened the vault to MCP")
	}
	if err := a.UpdateMailVaultSettings(MailVaultSettingsDTO{ID: f.vault, Name: "Private", AllowMCP: true}); err != nil {
		t.Fatal(err)
	}
	if got, err := mb.GetMessage(context.Background(), f.sealedID); err != nil || got.Subject != vaultMarkSubject {
		t.Fatalf("MCP with the switch on: %+v %v", got, err)
	}

	a.LockMailVault(f.vault)
	if _, err := mb.GetMessage(context.Background(), f.sealedID); err == nil {
		t.Fatal("MCP still reads after the vault locked")
	}
	if _, err := sm.Message(context.Background(), f.sealedID); err == nil {
		t.Fatal("scripts still read after the vault locked")
	}
}

func TestScriptAndMCPAdaptersAreCovered(t *testing.T) {
	covered := map[string]bool{"Message": true, "Body": true, "Recent": true}
	for _, name := range messageMethods(&scriptMail{}) {
		if !covered[name] {
			t.Errorf("scriptMail.%s returns mail and is not covered by TestUnlockedVaultIsOpenOnlyToItsAudience", name)
		}
	}
	trust := map[string]bool{"Passport": true, "Shield": true}
	for _, name := range messageMethods(scriptTrust{}) {
		if !trust[name] {
			t.Errorf("scriptTrust.%s returns mail data and is not covered", name)
		}
	}
	mcp := map[string]bool{"GetMessage": true, "ListMessages": true, "Search": true}
	for _, name := range messageMethods(&mcpMailbox{}) {
		if !mcp[name] && name != "ListAccounts" && name != "ListFolders" {
			t.Errorf("mcpMailbox.%s returns mail and is not covered", name)
		}
	}
	var _ mcpserver.Mailbox = (*mcpMailbox)(nil)
}

func TestVaultNotificationNamesNothing(t *testing.T) {
	f := newVaultApp(t)
	a := f.app
	if err := a.store.Set(a.ctx, settingNotifyNewMail, "true"); err != nil {
		t.Fatal(err)
	}
	folder, err := a.store.GetFolder(a.ctx, f.secret)
	if err != nil {
		t.Fatal(err)
	}
	n, ok := a.mailVaultNotification(*folder)
	if !ok || n.body != "New mail in a locked folder" || n.messageID != 0 || n.actions {
		t.Fatalf("locked vault notification = %+v %v", n, ok)
	}
	if err := a.UpdateMailVaultSettings(MailVaultSettingsDTO{ID: f.vault, Notify: storage.MailVaultNotifyNone}); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.mailVaultNotification(*folder); ok {
		t.Fatal("a vault set to no notifications still notified")
	}
}
