package desktop

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"html"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/microcosm-cc/bluemonday"

	"github.com/peltonapp/Pelton/internal/scripting"
	"github.com/peltonapp/Pelton/internal/storage"
)

// Script events for the frontend.
const (
	// EventScriptRun fires when a script run ends, with a ScriptRunDTO.
	EventScriptRun = "scripts:run"
	// EventScriptsChanged fires when scripts were added, changed or removed.
	EventScriptsChanged = "scripts:changed"
	// EventScriptConfirm asks the user about a risky call, with a
	// ScriptConfirmDTO. AnswerScriptConfirm carries the answer back.
	EventScriptConfirm = "scripts:confirm"
	// EventScriptCatchUp asks whether a script runs over mail that arrived
	// while the app was closed, with a ScriptCatchUpDTO.
	EventScriptCatchUp = "scripts:catchup"
	// EventScriptToast shows a script's notify.toast, with a ScriptToastDTO.
	EventScriptToast = "scripts:toast"
)

// settingScriptLogDays is how many days of script runs the log keeps.
const settingScriptLogDays = "scripts_log_days"

// scriptConfirmWait is how long a risky call waits for the user. The run's
// own timeout still applies on top, so this only matters for long timeouts.
const scriptConfirmWait = 2 * time.Minute

// scriptJournalRuns is how many runs' undo records are kept.
const scriptJournalRuns = 100

// scriptState is what the scripting bindings keep in memory.
type scriptState struct {
	mu sync.Mutex
	// journal holds, per run, how to reverse each mail change the run made,
	// in the order made.
	journal map[int64]*runJournal
	order   []int64
	// confirms holds the risky calls waiting for an answer, by request id.
	confirms map[string]chan scripting.ConfirmAnswer
	// synced holds the accounts that finished a full sync in this session;
	// mail a first sync brings in may have arrived while the app was closed.
	synced sync.Map
}

type runJournal struct {
	undo     []func() error
	messages []int64
}

// startScripts builds the scripting service on the open store and starts it.
// A failure leaves automations off and the rest of the app running.
func (a *App) startScripts() {
	svc, err := scripting.NewService(scripting.Config{
		DB: a.store.Handle(),
		Hosts: &scripting.Hosts{
			Mail:    &scriptMail{app: a},
			Notify:  &scriptNotify{app: a},
			Confirm: &scriptConfirm{app: a},
		},
		Log:           a.log,
		OnRun:         a.onScriptRun,
		OnChange:      func() { a.emit(EventScriptsChanged, nil) },
		OnCatchUp:     a.onScriptCatchUp,
		RetentionDays: func() int { return a.intSetting(settingScriptLogDays, scripting.DefaultRetentionDays) },
	})
	if err != nil {
		a.log.Error("scripts: set up", "err", err)
		return
	}
	if err := svc.Start(a.ctx); err != nil {
		a.log.Error("scripts: start", "err", err)
		return
	}
	a.scripts = svc
}

func (a *App) stopScripts() {
	if a.scripts != nil {
		a.scripts.Stop()
	}
}

// emitScript hands an event to the scripts when one listens for it.
func (a *App) emitScript(ev scripting.Event) {
	if a.scripts == nil || !a.scripts.Wants(ev.Trigger) {
		return
	}
	a.scripts.Emit(ev)
}

// scriptMailArrived reports new mail to the scripts. Mail from an account's
// first full sync in this session is marked for catch-up.
func (a *App) scriptMailArrived(ids []int64, firstSync bool) {
	if a.scripts == nil || !a.scripts.Wants(scripting.TriggerMailArrived) {
		return
	}
	for _, id := range ids {
		ev := scripting.Event{Trigger: scripting.TriggerMailArrived, Data: map[string]any{"message_id": id}, CatchUp: firstSync}
		if firstSync {
			m, err := a.store.GetMessage(a.ctx, id)
			if err != nil {
				continue
			}
			ev.Date = m.Date
		}
		a.scripts.Emit(ev)
	}
}

// firstScriptSync reports whether this is the account's first full sync of
// the session, and records that it happened.
func (a *App) firstScriptSync(accountID int64) bool {
	_, seen := a.scriptState.synced.LoadOrStore(accountID, true)
	return !seen
}

// scriptMoved reports a message that moved; it is a snapshot from before the
// move, since the moved copy gets a new id at the next sync.
func (a *App) scriptMoved(ctx context.Context, m *storage.Message, dest storage.Folder) {
	if a.scripts == nil || !a.scripts.Wants(scripting.TriggerMailMoved) {
		return
	}
	msg, err := a.scriptMessage(*m)
	if err != nil {
		return
	}
	a.scripts.Emit(scripting.Event{Trigger: scripting.TriggerMailMoved, Depth: scripting.EventDepth(ctx), Data: map[string]any{
		"message": scripting.MessageObject(msg), "to_folder": dest.IMAPPath, "to_role": folderRole(dest),
	}})
}

// scriptFlagChanged reports a read or flag change.
func (a *App) scriptFlagChanged(ctx context.Context, id int64, flag storage.Flag, on bool) {
	switch flag {
	case storage.FlagSeen:
		a.emitScript(scripting.Event{Trigger: scripting.TriggerMailReadMark, Depth: scripting.EventDepth(ctx), Data: map[string]any{"message_id": id, "read": on}})
	case storage.FlagFlagged:
		a.emitScript(scripting.Event{Trigger: scripting.TriggerMailFlagged, Depth: scripting.EventDepth(ctx), Data: map[string]any{"message_id": id, "flagged": on}})
	}
}

// scriptAfterSend reports a message the outbox sent.
func (a *App) scriptAfterSend(outboxID int64) {
	if a.scripts == nil || !a.scripts.Wants(scripting.TriggerAfterSend) {
		return
	}
	v, ok := a.sentRequests.Load(outboxID)
	if !ok {
		return
	}
	req := v.(ComposeRequest)
	a.scripts.Emit(scripting.Event{Trigger: scripting.TriggerAfterSend, Data: map[string]any{"draft": draftEvent(req)}})
}

// draftEvent is a message being written as the scripts see it.
func draftEvent(req ComposeRequest) map[string]any {
	list := func(addrs []AddressDTO) []any {
		out := make([]any, 0, len(addrs))
		for _, ad := range addrs {
			out = append(out, ad.Email)
		}
		return out
	}
	body := req.Text
	if body == "" && req.HTML != "" {
		body = htmlText(req.HTML)
	}
	return map[string]any{
		"account_id": req.AccountID, "to": list(req.To), "cc": list(req.Cc), "bcc": list(req.Bcc),
		"subject": req.Subject, "body": body,
	}
}

// applyDraftEvent writes what a before-send script changed back into the
// request. A changed body replaces the html with the text, since a script
// edits plain text.
func applyDraftEvent(req ComposeRequest, ev map[string]any) ComposeRequest {
	d, ok := ev["draft"].(map[string]any)
	if !ok {
		return req
	}
	before := draftEvent(req)
	if s, ok := d["subject"].(string); ok {
		req.Subject = s
	}
	if s, ok := d["body"].(string); ok && s != before["body"] {
		req.Text, req.HTML = s, ""
	}
	addrs := func(cur []AddressDTO, v any) []AddressDTO {
		items, ok := v.([]any)
		if !ok {
			if m, isMap := v.(map[string]any); isMap && len(m) == 0 {
				return []AddressDTO{}
			}
			return cur
		}
		out := make([]AddressDTO, 0, len(items))
		for _, item := range items {
			email, ok := item.(string)
			if !ok || strings.TrimSpace(email) == "" {
				continue
			}
			name := ""
			if i := slices.IndexFunc(cur, func(ad AddressDTO) bool { return strings.EqualFold(ad.Email, email) }); i >= 0 {
				name = cur[i].Name
			}
			out = append(out, AddressDTO{Name: name, Email: strings.TrimSpace(email)})
		}
		return out
	}
	req.To, req.Cc, req.Bcc = addrs(req.To, d["to"]), addrs(req.Cc, d["cc"]), addrs(req.Bcc, d["bcc"])
	return req
}

// beforeSendScripts runs the before-send scripts over a message about to be
// queued, returning the request as they left it, or an error when one of
// them stopped the send.
func (a *App) beforeSendScripts(ctx context.Context, req ComposeRequest) (ComposeRequest, error) {
	if a.scripts == nil || !a.scripts.Wants(scripting.TriggerBeforeSend) {
		return req, nil
	}
	runs := a.scripts.RunSync(ctx, scripting.Event{
		Trigger: scripting.TriggerBeforeSend,
		Data:    map[string]any{"draft": draftEvent(req)},
		Depth:   scripting.EventDepth(ctx),
	})
	if len(runs) == 0 {
		return req, nil
	}
	last := runs[len(runs)-1]
	if last.Cancelled {
		name := "a script"
		if sc, err := a.scripts.Script(ctx, last.ScriptID); err == nil {
			name = fmt.Sprintf("the script %q", sc.Name)
		}
		if last.CancelReason != "" {
			return req, fmt.Errorf("pelton: %s stopped this message: %s", name, last.CancelReason)
		}
		return req, fmt.Errorf("pelton: %s stopped this message", name)
	}
	for i := len(runs) - 1; i >= 0; i-- {
		if runs[i].Event != nil {
			return applyDraftEvent(req, runs[i].Event), nil
		}
	}
	return req, nil
}

func (a *App) onScriptRun(sc *scripting.Script, r *scripting.Run) {
	a.emit(EventScriptRun, a.runDTO(sc.Name, r))
}

func (a *App) onScriptCatchUp(sc *scripting.Script, count int) {
	a.emit(EventScriptCatchUp, ScriptCatchUpDTO{ScriptID: sc.ID, Name: sc.Name, Count: count})
}

// journal records how to reverse one change of a run.
func (a *App) journal(ctx context.Context, messageID int64, undo func() error) {
	info, ok := scripting.RunInfoFrom(ctx)
	if !ok || info.RunID == 0 {
		return
	}
	st := &a.scriptState
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.journal == nil {
		st.journal = map[int64]*runJournal{}
	}
	j := st.journal[info.RunID]
	if j == nil {
		j = &runJournal{}
		st.journal[info.RunID] = j
		st.order = append(st.order, info.RunID)
		if len(st.order) > scriptJournalRuns {
			delete(st.journal, st.order[0])
			st.order = st.order[1:]
		}
	}
	j.undo = append(j.undo, undo)
	if messageID != 0 && !slices.Contains(j.messages, messageID) {
		j.messages = append(j.messages, messageID)
	}
}

// runUndo returns whether a run can be undone and the messages it touched.
func (a *App) runUndo(runID int64) (bool, []int64) {
	st := &a.scriptState
	st.mu.Lock()
	defer st.mu.Unlock()
	j := st.journal[runID]
	if j == nil || len(j.undo) == 0 {
		return false, []int64{}
	}
	return true, slices.Clone(j.messages)
}

// scriptMail is the mail a script reaches, through the same operations the
// bindings use.
type scriptMail struct {
	app *App
}

func (a *App) scriptMessage(m storage.Message) (scripting.Message, error) {
	folder, err := a.store.GetFolder(a.ctx, m.FolderID)
	if err != nil {
		return scripting.Message{}, err
	}
	return scripting.Message{
		ID: m.ID, AccountID: m.AccountID, Account: a.accountEmail(m.AccountID),
		Folder: folder.IMAPPath, FolderRole: folderRole(*folder),
		Subject: m.Subject, From: m.FromAddress, FromName: m.FromName,
		To: m.ToAddresses, Cc: m.CcAddresses, ReplyTo: m.ReplyTo, Date: m.Date,
		Size: m.SizeBytes, HasAttachments: m.HasAttachments,
		Read: m.Flags&storage.FlagSeen != 0, Flagged: m.Flags&storage.FlagFlagged != 0,
		Pinned: m.Pinned, Color: m.FlagColor,
		Headers: map[string]string{
			"message-id":  m.MessageID,
			"list-id":     m.ListID,
			"reply-to":    m.ReplyTo,
			"in-reply-to": strings.Join(m.InReplyTo, " "),
			"references":  strings.Join(m.References, " "),
		},
	}, nil
}

func (s *scriptMail) Message(_ context.Context, id int64) (scripting.Message, error) {
	if err := s.app.ready(); err != nil {
		return scripting.Message{}, err
	}
	m, err := s.app.store.GetMessage(s.app.ctx, id)
	if err != nil {
		return scripting.Message{}, err
	}
	return s.app.scriptMessage(*m)
}

var stripTags = bluemonday.StrictPolicy()

func htmlText(s string) string {
	return strings.TrimSpace(html.UnescapeString(stripTags.Sanitize(s)))
}

func (s *scriptMail) Body(_ context.Context, id int64) (string, error) {
	m, err := s.app.store.GetMessage(s.app.ctx, id)
	if err != nil {
		return "", err
	}
	if m.BodyPlain != "" {
		return m.BodyPlain, nil
	}
	return htmlText(m.BodyHTML), nil
}

// folder finds a folder of the message's account by role, path or name.
func (s *scriptMail) folder(accountID int64, name string) (storage.Folder, error) {
	folders, err := s.app.store.ListFolders(s.app.ctx, accountID)
	if err != nil {
		return storage.Folder{}, err
	}
	name = strings.TrimSpace(name)
	for _, match := range []func(storage.Folder) bool{
		func(f storage.Folder) bool {
			return strings.EqualFold(folderRole(f), name) && folderRole(f) != roleNormal
		},
		func(f storage.Folder) bool { return f.IMAPPath == name },
		func(f storage.Folder) bool {
			return strings.EqualFold(f.IMAPPath, name) || strings.EqualFold(f.Name, name)
		},
	} {
		if i := slices.IndexFunc(folders, match); i >= 0 {
			return folders[i], nil
		}
	}
	return storage.Folder{}, fmt.Errorf("pelton: no folder %q in this account", name)
}

func (s *scriptMail) moveTo(ctx context.Context, id int64, dest func(m *storage.Message) (storage.Folder, error)) error {
	a := s.app
	m, err := a.store.GetMessage(a.ctx, id)
	if err != nil {
		return err
	}
	f, err := dest(m)
	if err != nil {
		return err
	}
	if f.ID == m.FolderID {
		return nil
	}
	undo, err := a.moveMessageTo(m, f, nil)
	if err != nil {
		return err
	}
	if undo.MessageID != "" {
		a.journal(ctx, id, func() error { return a.RestoreMessage(undo.MessageID, undo.DestFolderID, undo.OriginalFolderID) })
	}
	a.scriptMoved(ctx, m, f)
	return nil
}

func (s *scriptMail) Move(ctx context.Context, id int64, folder string) error {
	return s.moveTo(ctx, id, func(m *storage.Message) (storage.Folder, error) { return s.folder(m.AccountID, folder) })
}

func (s *scriptMail) Archive(ctx context.Context, id int64) error {
	return s.moveTo(ctx, id, func(m *storage.Message) (storage.Folder, error) { return s.app.findArchiveFolder(m.AccountID) })
}

func (s *scriptMail) Delete(ctx context.Context, id int64) error {
	a := s.app
	if err := a.DeleteMessage(id); err != nil {
		return err
	}
	a.journal(ctx, id, func() error { return a.UndoDelete(id) })
	return nil
}

func (s *scriptMail) setFlag(ctx context.Context, id int64, flag storage.Flag, on bool) error {
	a := s.app
	m, err := a.store.GetMessage(a.ctx, id)
	if err != nil {
		return err
	}
	was := m.Flags&flag != 0
	if was == on {
		return nil
	}
	if err := a.updateFlag(id, flag, on); err != nil {
		return err
	}
	a.journal(ctx, id, func() error { return a.updateFlag(id, flag, was) })
	a.scriptFlagChanged(ctx, id, flag, on)
	return nil
}

func (s *scriptMail) SetRead(ctx context.Context, id int64, read bool) error {
	return s.setFlag(ctx, id, storage.FlagSeen, read)
}

func (s *scriptMail) SetFlagged(ctx context.Context, id int64, on bool) error {
	return s.setFlag(ctx, id, storage.FlagFlagged, on)
}

func (s *scriptMail) SetColor(ctx context.Context, id int64, color int) error {
	a := s.app
	m, err := a.store.GetMessage(a.ctx, id)
	if err != nil {
		return err
	}
	was := m.FlagColor
	if err := a.SetFlagColor(id, color); err != nil {
		return err
	}
	a.journal(ctx, id, func() error { return a.SetFlagColor(id, was) })
	return nil
}

func (s *scriptMail) SetPinned(ctx context.Context, id int64, on bool) error {
	a := s.app
	m, err := a.store.GetMessage(a.ctx, id)
	if err != nil {
		return err
	}
	was := m.Pinned
	if err := a.SetPinned(id, on, false); err != nil {
		return err
	}
	a.journal(ctx, id, func() error { return a.SetPinned(id, was, false) })
	return nil
}

func (s *scriptMail) Snooze(ctx context.Context, id int64, until time.Time) error {
	a := s.app
	m, err := a.store.GetMessage(a.ctx, id)
	if err != nil {
		return err
	}
	was, wasHidden := m.SnoozeUntil, m.SnoozeHidden
	if err := a.SnoozeMessage(id, until.UTC().Format(time.RFC3339), true); err != nil {
		return err
	}
	a.journal(ctx, id, func() error {
		if was == "" {
			return a.UnsnoozeMessage(id)
		}
		return a.SnoozeMessage(id, was, wasHidden)
	})
	return nil
}

// Compose turns a script's draft into a compose request: a reply answers the
// sender (and everyone, for reply all) with the original quoted, a forward
// carries the original's text. Sending goes through the normal send path,
// so the undo-send delay and the before-send scripts apply.
func (s *scriptMail) Compose(ctx context.Context, d scripting.Draft, send bool) (int64, error) {
	a := s.app
	if err := a.ready(); err != nil {
		return 0, err
	}
	req := ComposeRequest{AccountID: d.AccountID, Subject: d.Subject, Text: d.Body,
		To: addressList(d.To), Cc: addressList(d.Cc), Bcc: addressList(d.Bcc)}
	if orig := d.ReplyTo + d.Forward; orig != 0 {
		m, err := a.store.GetMessage(a.ctx, orig)
		if err != nil {
			return 0, err
		}
		body, _ := s.Body(ctx, orig)
		if req.AccountID == 0 {
			req.AccountID = m.AccountID
		}
		if d.ReplyTo != 0 {
			fillReply(&req, m, d.ReplyAll, a.accountEmail(m.AccountID), body)
		} else {
			fillForward(&req, m, body)
		}
	}
	if req.AccountID == 0 {
		accounts, err := a.store.ListAccounts(a.ctx)
		if err != nil || len(accounts) == 0 {
			return 0, fmt.Errorf("pelton: no account to write from")
		}
		req.AccountID = accounts[0].ID
	}
	if send {
		return a.sendMessage(ctx, req)
	}
	id, err := a.SaveDraft(0, req)
	if err != nil {
		return 0, err
	}
	a.journal(ctx, 0, func() error { return a.DeleteDraft(id) })
	return id, nil
}

func addressList(list []string) []AddressDTO {
	out := []AddressDTO{}
	for _, s := range list {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, AddressDTO{Email: s})
		}
	}
	return out
}

func quoted(m *storage.Message, body string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n\nOn %s, %s wrote:\n", m.Date.Format("2006-01-02 15:04"), m.FromAddress)
	for _, line := range strings.Split(strings.TrimRight(body, "\n"), "\n") {
		b.WriteString("> " + line + "\n")
	}
	return b.String()
}

func fillReply(req *ComposeRequest, m *storage.Message, all bool, own, body string) {
	if len(req.To) == 0 {
		to := m.ReplyTo
		if to == "" {
			to = m.FromAddress
		}
		req.To = addressList([]string{to})
		if all {
			for _, field := range []string{m.ToAddresses, m.CcAddresses} {
				for _, addr := range strings.Split(field, ",") {
					addr = strings.TrimSpace(addr)
					if addr != "" && !strings.EqualFold(addr, own) && !strings.EqualFold(addr, to) {
						req.Cc = append(req.Cc, AddressDTO{Email: addr})
					}
				}
			}
		}
	}
	if req.Subject == "" {
		req.Subject = m.Subject
		if !strings.HasPrefix(strings.ToLower(req.Subject), "re:") {
			req.Subject = "Re: " + req.Subject
		}
	}
	req.InReplyTo = m.MessageID
	req.References = append(slices.Clone(m.References), m.MessageID)
	req.Text += quoted(m, body)
}

func fillForward(req *ComposeRequest, m *storage.Message, body string) {
	if req.Subject == "" {
		req.Subject = "Fwd: " + m.Subject
	}
	req.ForwardOf = m.ID
	req.Text += fmt.Sprintf("\n\n---------- Forwarded message ----------\nFrom: %s\nDate: %s\nSubject: %s\n\n%s",
		m.FromAddress, m.Date.Format("2006-01-02 15:04"), m.Subject, body)
}

// scriptNotify shows a script's messages.
type scriptNotify struct {
	app *App
}

func (n *scriptNotify) Toast(_ context.Context, script, text, level string) {
	n.app.emit(EventScriptToast, ScriptToastDTO{Script: script, Text: text, Level: level})
}

func (n *scriptNotify) System(_ context.Context, title, body string) error {
	n.app.sendNotification(notification{title: title, body: body})
	return nil
}

// scriptConfirm asks the user about risky calls through a dialog in the
// frontend.
type scriptConfirm struct {
	app *App
}

func (c *scriptConfirm) Confirm(ctx context.Context, req scripting.ConfirmRequest) (scripting.ConfirmAnswer, error) {
	a := c.app
	if a.wapp == nil {
		return scripting.ConfirmAnswer{}, fmt.Errorf("pelton: no window to ask in")
	}
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return scripting.ConfirmAnswer{}, err
	}
	id := hex.EncodeToString(buf)
	answer := make(chan scripting.ConfirmAnswer, 1)
	st := &a.scriptState
	st.mu.Lock()
	if st.confirms == nil {
		st.confirms = map[string]chan scripting.ConfirmAnswer{}
	}
	st.confirms[id] = answer
	st.mu.Unlock()
	defer func() {
		st.mu.Lock()
		delete(st.confirms, id)
		st.mu.Unlock()
	}()
	a.emit(EventScriptConfirm, ScriptConfirmDTO{ID: id, ScriptID: req.ScriptID, ScriptName: req.ScriptName, Function: req.Function, Summary: req.Summary})
	timer := time.NewTimer(scriptConfirmWait)
	defer timer.Stop()
	select {
	case ans := <-answer:
		return ans, nil
	case <-ctx.Done():
		return scripting.ConfirmAnswer{}, ctx.Err()
	case <-timer.C:
		return scripting.ConfirmAnswer{}, fmt.Errorf("pelton: no answer")
	}
}
