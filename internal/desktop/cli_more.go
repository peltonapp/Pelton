package desktop

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/mail"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/autocrypt"
	"github.com/peltonapp/Pelton/internal/cliproto"
	"github.com/peltonapp/Pelton/internal/cliserver"
	"github.com/peltonapp/Pelton/internal/scripting"
	"github.com/peltonapp/Pelton/internal/storage"
)

// cliAddresses reads address lists, each entry one address or several
// separated by commas.
func cliAddresses(entries []string) ([]AddressDTO, error) {
	var out []AddressDTO
	for _, entry := range entries {
		if strings.TrimSpace(entry) == "" {
			continue
		}
		list, err := mail.ParseAddressList(entry)
		if err != nil {
			return nil, cliproto.Errorf(cliproto.CodeBadRequest, "%q is not an address: %v", entry, err)
		}
		for _, addr := range list {
			out = append(out, AddressDTO{Name: addr.Name, Email: addr.Address})
		}
	}
	return out, nil
}

// cliSendAccount is the account a message goes out from: the one named, or
// the first that is not imported mail.
func (a *App) cliSendAccount(ctx context.Context, spec string) (*storage.Account, error) {
	if spec != "" {
		return a.cliAccount(ctx, spec)
	}
	accounts, err := a.store.ListAccounts(ctx)
	if err != nil {
		return nil, err
	}
	for i := range accounts {
		if !accounts[i].Local {
			return &accounts[i], nil
		}
	}
	return nil, cliproto.Errorf(cliproto.CodeNotFound, "there is no account to send from")
}

// cliSend queues a message through the composer's own send path, or keeps
// it as a draft. Sending asks in the app window first unless sending without
// asking is on; the before-send scripts, their recipient confirmation and
// automatic encryption then apply as for any message.
func (a *App) cliSend(ctx context.Context, req cliproto.SendRequest, draft bool) (cliserver.Response, error) {
	account, err := a.cliSendAccount(ctx, req.From)
	if err != nil {
		return cliserver.Response{}, err
	}
	compose := ComposeRequest{AccountID: account.ID, Subject: req.Subject, Text: req.Body}
	for _, list := range []struct {
		in  []string
		out *[]AddressDTO
	}{{req.To, &compose.To}, {req.Cc, &compose.Cc}, {req.Bcc, &compose.Bcc}} {
		if *list.out, err = cliAddresses(list.in); err != nil {
			return cliserver.Response{}, err
		}
	}
	for _, at := range req.Attachments {
		compose.Attachments = append(compose.Attachments, ComposeAttachment{
			Filename: filepath.Base(at.Name), ContentType: at.ContentType,
			ContentBase64: base64.StdEncoding.EncodeToString(at.Content),
		})
	}
	recipients := len(compose.To) + len(compose.Cc) + len(compose.Bcc)
	noun := "a message"
	if draft {
		noun = "a draft"
	}
	touched := fmt.Sprintf("%s from account %d to %d recipient(s) with %d attachment(s)", noun, account.ID, recipients, len(compose.Attachments))
	if draft {
		id, err := a.SaveDraft(0, compose)
		if err != nil {
			return cliserver.Response{Touched: touched}, err
		}
		return cliserver.Response{Data: cliproto.Sent{DraftID: id, Protection: protectionNone}, Touched: touched}, nil
	}
	if recipients == 0 {
		return cliserver.Response{Touched: touched}, cliproto.Errorf(cliproto.CodeBadRequest, "a message needs at least one recipient")
	}
	a.cliProtection(*account, &compose)
	if err := a.cliConfirm(ctx, cliproto.CapSendUnasked, "cli.send", a.cliSendSummary(*account, compose)); err != nil {
		return cliserver.Response{Touched: touched}, err
	}
	id, err := a.sendMessage(ctx, compose)
	if errors.Is(err, errRecipientsHeld) {
		return cliserver.Response{Touched: touched}, cliproto.Errorf(cliproto.CodeRefused, "%s", err.Error())
	}
	if err != nil {
		return cliserver.Response{Touched: touched}, err
	}
	return cliserver.Response{Data: cliproto.Sent{OutboxID: id, Protection: compose.Protection}, Touched: touched}, nil
}

// cliProtection picks the protection the composer would start with: with
// automatic encryption on, encrypted when every recipient allows it; else
// the account's default as far as the keys allow.
func (a *App) cliProtection(account storage.Account, req *ComposeRequest) {
	if a.autoEncryptOn() {
		req.AutoEncrypt = true
		req.Protection = protectionNone
		status, err := a.AutoEncryptStatus(AutoEncryptQuery{
			AccountID: account.ID, To: emailsOf(req.To), Cc: emailsOf(req.Cc), Bcc: emailsOf(req.Bcc),
		})
		if err == nil && status.Recommendation == autocrypt.Encrypt.String() {
			req.Protection = protectionSignEncrypt
		}
		return
	}
	all := slices.Concat(emailsOf(req.To), emailsOf(req.Cc), emailsOf(req.Bcc))
	status, err := a.ComposeProtectionStatus(account.ID, all)
	if err != nil {
		req.Protection = protectionNone
		return
	}
	req.Protection = status.Suggested
}

func (a *App) cliSendSummary(account storage.Account, req ComposeRequest) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Send a message from %s.\n\n%s\nSubject: %s\n", account.Email, recipientsText(req), req.Subject)
	if n := len(req.Attachments); n > 0 {
		names := make([]string, 0, n)
		for _, at := range req.Attachments {
			names = append(names, at.Filename)
		}
		fmt.Fprintf(&b, "Attachments: %s\n", strings.Join(names, ", "))
	}
	if line := a.encryptionSummary(req); line != "" {
		b.WriteString("\n" + line + "\n")
	} else if req.Protection != protectionNone && req.Protection != "" {
		fmt.Fprintf(&b, "\nProtection: %s\n", req.Protection)
	}
	return b.String()
}

// cliCompose opens a composer with the fields or the mailto: URL given. It
// sends nothing.
func (a *App) cliCompose(req cliproto.ComposeRequest) (cliserver.Response, error) {
	var draft MailtoDraft
	if req.Mailto != "" {
		if !strings.HasPrefix(strings.ToLower(req.Mailto), "mailto:") {
			return cliserver.Response{}, cliproto.Errorf(cliproto.CodeBadRequest, "%q is not a mailto: link", req.Mailto)
		}
		draft = parseMailto(req.Mailto)
	} else {
		draft = MailtoDraft{
			To: strings.Join(req.To, ", "), Cc: strings.Join(req.Cc, ", "), Bcc: strings.Join(req.Bcc, ", "),
			Subject: req.Subject, Body: req.Body,
		}
	}
	a.deliverDraft(draft)
	a.showWindow()
	return cliserver.Response{Data: struct{}{}, Touched: "a new composer"}, nil
}

// cliScript finds an automation by id or by name, ignoring case.
func (a *App) cliScript(ctx context.Context, svc *scripting.Service, spec string) (*scripting.Script, error) {
	if id, err := strconv.ParseInt(spec, 10, 64); err == nil {
		if sc, err := svc.Script(ctx, id); err == nil {
			return sc, nil
		}
	}
	scripts, err := a.ListScripts()
	if err != nil {
		return nil, err
	}
	var found []int64
	for _, s := range scripts {
		if strings.EqualFold(strings.TrimSpace(s.Name), strings.TrimSpace(spec)) {
			found = append(found, s.ID)
		}
	}
	switch len(found) {
	case 0:
		return nil, cliproto.Errorf(cliproto.CodeNotFound, "no automation %q", spec)
	case 1:
		return svc.Script(ctx, found[0])
	}
	return nil, cliproto.Errorf(cliproto.CodeBadRequest, "%d automations are called %q; give the id", len(found), spec)
}

// cliSettings reads --set values by the type the script declares for them.
func cliSettings(schema []scripting.Param, raw map[string]string) (map[string]any, error) {
	out := map[string]any{}
	for name, value := range raw {
		i := slices.IndexFunc(schema, func(p scripting.Param) bool { return p.Name == name })
		if i < 0 {
			return nil, cliproto.Errorf(cliproto.CodeBadRequest, "the automation has no setting %q", name)
		}
		switch schema[i].Type {
		case scripting.TypeNumber, scripting.TypeColor, scripting.TypeAccount:
			n, err := strconv.ParseFloat(value, 64)
			if err != nil {
				return nil, cliproto.Errorf(cliproto.CodeBadRequest, "setting %q needs a number", name)
			}
			out[name] = n
		case scripting.TypeBool:
			b, err := strconv.ParseBool(value)
			if err != nil {
				return nil, cliproto.Errorf(cliproto.CodeBadRequest, "setting %q needs true or false", name)
			}
			out[name] = b
		case scripting.TypeList:
			return nil, cliproto.Errorf(cliproto.CodeBadRequest, "setting %q is a list, which cannot be set from the command line", name)
		default:
			out[name] = value
		}
	}
	return out, nil
}

// cliRun runs an automation started by hand, with its own permissions and
// the questions it asks in the window. A dry run takes the test path and
// changes nothing.
func (a *App) cliRun(ctx context.Context, req cliproto.RunRequest) (cliserver.Response, error) {
	svc, err := a.scriptService()
	if err != nil {
		return cliserver.Response{}, err
	}
	sc, err := a.cliScript(ctx, svc, req.Script)
	if err != nil {
		return cliserver.Response{}, err
	}
	touched := fmt.Sprintf("automation %d", sc.ID)
	if req.DryRun {
		touched += ", a dry run"
	}
	if sc.Trigger.Type != scripting.TriggerManual {
		return cliserver.Response{Touched: touched}, cliproto.Errorf(cliproto.CodeBadRequest, "%q is not started by hand, so it cannot be run from the command line", sc.Name)
	}
	settings, err := cliSettings(sc.SettingsSchema, req.Setting)
	if err != nil {
		return cliserver.Response{Touched: touched}, err
	}
	var data map[string]any
	if req.On != 0 {
		if _, err := a.cliReach(ctx, req.On); err != nil {
			return cliserver.Response{Touched: touched}, err
		}
		data = manualEvent(req.On)
		touched += fmt.Sprintf(" on message %d", req.On)
	}
	var run *scripting.Run
	if req.DryRun {
		test := *sc
		if len(settings) > 0 {
			merged := map[string]any{}
			for k, v := range sc.Settings {
				merged[k] = v
			}
			for k, v := range settings {
				merged[k] = v
			}
			if test.Settings, err = scripting.CheckSettings(sc.SettingsSchema, merged); err != nil {
				return cliserver.Response{Touched: touched}, cliproto.Errorf(cliproto.CodeBadRequest, "%v", err)
			}
		}
		run, err = svc.TestRun(ctx, &test, data)
	} else {
		if !sc.Enabled {
			return cliserver.Response{Touched: touched}, cliproto.Errorf(cliproto.CodeBadRequest, "%q is switched off; switch it on in Pelton, or use --dry-run", sc.Name)
		}
		run, err = svc.RunNowWithSettings(ctx, sc.ID, data, settings)
	}
	if err != nil {
		return cliserver.Response{Touched: touched}, err
	}
	dto := a.runDTO(sc.Name, run)
	out := cliproto.Run{
		ID: dto.ID, ScriptID: dto.ScriptID, ScriptName: dto.ScriptName, Dry: dto.Dry, Status: dto.Status,
		Error: dto.Error, Changes: dto.Changes, DurationMs: dto.DurationMs, Steps: []cliproto.Step{},
	}
	for _, st := range dto.Steps {
		out.Steps = append(out.Steps, cliproto.Step{Kind: string(st.Kind), Function: st.Function, Summary: st.Summary, Error: st.Error})
	}
	return cliserver.Response{Data: out, Touched: touched}, nil
}

func (a *App) cliCalendar(req cliproto.RangeRequest) (cliserver.Response, error) {
	occ, err := a.ListOccurrences(req.From, req.To, nil)
	if err != nil {
		return cliserver.Response{Touched: "calendar"}, cliproto.Errorf(cliproto.CodeBadRequest, "%v", err)
	}
	out := make([]cliproto.Event, 0, len(occ))
	for _, o := range occ {
		out = append(out, cliproto.Event{ID: o.EventID, Calendar: o.CalendarID, Summary: o.Summary, Location: o.Location, Start: o.Start, End: o.End, AllDay: o.AllDay})
	}
	return cliserver.Response{Data: out, Touched: fmt.Sprintf("calendar, %d events", len(out))}, nil
}

func (a *App) cliTasks(req cliproto.TasksRequest) (cliserver.Response, error) {
	tasks, err := a.ListTasks()
	if err != nil {
		return cliserver.Response{Touched: "tasks"}, err
	}
	out := []cliproto.Task{}
	for _, t := range tasks {
		if (t.Done && !req.Done) || (req.List != "" && !strings.EqualFold(t.ListName, req.List)) {
			continue
		}
		if req.Due != "" && (t.Due == "" || dayOf(t.Due) > req.Due) {
			continue
		}
		out = append(out, cliproto.Task{ID: t.ID, List: t.ListName, Title: t.Title, Due: t.Due, Done: t.Done})
	}
	return cliserver.Response{Data: out, Touched: fmt.Sprintf("tasks, %d listed", len(out))}, nil
}

// dayOf is the YYYY-MM-DD day of a date or an RFC 3339 time.
func dayOf(due string) string {
	if t, err := time.Parse(time.RFC3339, due); err == nil {
		return t.Local().Format("2006-01-02")
	}
	return due
}

func (a *App) cliTaskAdd(req cliproto.TaskAddRequest) (cliserver.Response, error) {
	if strings.TrimSpace(req.Title) == "" {
		return cliserver.Response{Touched: "a new task"}, cliproto.Errorf(cliproto.CodeBadRequest, "a task needs a title")
	}
	var listID int64
	if req.List != "" {
		lists, err := a.ListTaskLists()
		if err != nil {
			return cliserver.Response{Touched: "a new task"}, err
		}
		for _, l := range lists {
			if strings.EqualFold(l.Name, req.List) || strconv.FormatInt(l.ID, 10) == req.List {
				listID = l.ID
				break
			}
		}
		if listID == 0 {
			return cliserver.Response{Touched: "a new task"}, cliproto.Errorf(cliproto.CodeNotFound, "no task list %q", req.List)
		}
	}
	res, err := a.CreateTask(TaskRequest{ListID: listID, Title: strings.TrimSpace(req.Title), Due: req.Due})
	touched := "a new task"
	if err != nil {
		return cliserver.Response{Touched: touched}, err
	}
	touched = fmt.Sprintf("task %d", res.Task.ID)
	return cliserver.Response{Data: cliproto.Task{ID: res.Task.ID, List: res.Task.ListName, Title: res.Task.Title, Due: res.Task.Due, Done: res.Task.Done}, Touched: touched}, nil
}

func (a *App) cliContacts(req cliproto.QueryRequest) (cliserver.Response, error) {
	entries, err := a.SearchAddresses(req.Query, cliLimit(req.Limit))
	if err != nil {
		return cliserver.Response{Touched: "contacts"}, err
	}
	out := make([]cliproto.Contact, 0, len(entries))
	for _, e := range entries {
		out = append(out, cliproto.Contact{Name: e.Name, Email: e.Email, Contact: e.Contact})
	}
	return cliserver.Response{Data: out, Touched: fmt.Sprintf("contacts, %d found", len(out))}, nil
}

// cliOpen brings the window forward on a message, a mailto: link, an .ics
// file or a script file to import.
func (a *App) cliOpen(ctx context.Context, req cliproto.OpenRequest) (cliserver.Response, error) {
	switch {
	case req.ID != 0:
		touched := fmt.Sprintf("message %d", req.ID)
		m, err := a.cliReach(ctx, req.ID)
		if err != nil {
			return cliserver.Response{Touched: touched}, err
		}
		a.showWindow()
		a.emit(EventOpenMessage, OpenMessageEvent{MessageID: m.ID, AccountID: m.AccountID, FolderID: m.FolderID})
		return cliserver.Response{Data: struct{}{}, Touched: touched}, nil
	case req.Mailto != "":
		if !strings.HasPrefix(strings.ToLower(req.Mailto), "mailto:") {
			return cliserver.Response{}, cliproto.Errorf(cliproto.CodeBadRequest, "%q is not a mailto: link", req.Mailto)
		}
		a.deliverMailto(req.Mailto)
		a.showWindow()
		return cliserver.Response{Data: struct{}{}, Touched: "a new composer"}, nil
	case req.File != "":
		return a.cliOpenFile(req.File)
	}
	a.showWindow()
	return cliserver.Response{Data: struct{}{}, Touched: "the window"}, nil
}

func (a *App) cliOpenFile(file string) (cliserver.Response, error) {
	path := filepath.Clean(file)
	ext := strings.ToLower(filepath.Ext(path))
	touched := "a " + ext + " file"
	if !filepath.IsAbs(path) {
		return cliserver.Response{Touched: touched}, cliproto.Errorf(cliproto.CodeBadRequest, "the file path must be absolute")
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return cliserver.Response{Touched: touched}, cliproto.Errorf(cliproto.CodeNotFound, "%s is not a file", path)
	}
	switch ext {
	case ".ics":
		a.deliverICS([]string{path})
	case scripting.FileExtension:
		a.emit(EventFilesDropped, FilesDroppedEvent{Target: "app", Paths: []string{path}})
	default:
		return cliserver.Response{Touched: touched}, cliproto.Errorf(cliproto.CodeBadRequest, "Pelton opens .ics and %s files; it has no viewer for %s files", scripting.FileExtension, ext)
	}
	a.showWindow()
	return cliserver.Response{Data: struct{}{}, Touched: touched}, nil
}
