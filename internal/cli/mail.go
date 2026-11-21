package cli

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/cliproto"
)

func cmdStatus(e *env, args []string) error {
	if len(args) > 0 {
		return usageErr("status takes no arguments")
	}
	var st cliproto.Status
	err := e.call(cliproto.CmdStatus, struct{}{}, &st)
	var pe *cliproto.Error
	switch {
	case errors.Is(err, errNotRunning):
		if e.json {
			_ = e.printJSON(map[string]bool{"running": false})
			return &exitError{code: ExitNotRunning}
		}
		return err
	case errors.As(err, &pe) && pe.Code == cliproto.CodeAccessOff:
		if e.json {
			_ = e.printJSON(map[string]bool{"running": true, "access": false})
			return &exitError{code: ExitAccessOff}
		}
		e.printf("Pelton is running. Command line access is off; turn it on in Settings, Backup.\n")
		return &exitError{code: ExitAccessOff}
	case err != nil:
		return err
	}
	if e.json {
		return e.printJSON(struct {
			Running bool `json:"running"`
			cliproto.Status
		}{true, st})
	}
	e.printf("Pelton %s is running.\nLocked: %s\nCommand line access: on\n\n", line(st.Version, 40), yesNo(st.Locked))
	rows := [][]string{{"SWITCH", "STATE", "NAME"}}
	for _, c := range st.Capabilities {
		rows = append(rows, []string{c.Name, onOff(c.On), c.Label})
	}
	table(e.opts.Stdout, rows)
	return nil
}

func cmdAccounts(e *env, args []string) error {
	if len(args) > 0 {
		return usageErr("accounts takes no arguments")
	}
	var out []cliproto.Account
	if err := e.call(cliproto.CmdAccounts, struct{}{}, &out); err != nil {
		return err
	}
	if e.json {
		return e.printJSON(nonNil(out))
	}
	rows := [][]string{{"ID", "EMAIL", "NAME"}}
	for _, a := range out {
		rows = append(rows, []string{strconv.FormatInt(a.ID, 10), line(a.Email, 60), line(a.Name, 60)})
	}
	table(e.opts.Stdout, rows)
	return nil
}

func cmdFolders(e *env, args []string) error {
	if len(args) > 1 {
		return usageErr("usage: pelton folders [ACCOUNT]")
	}
	req := cliproto.FolderRequest{}
	if len(args) == 1 {
		req.Account = args[0]
	}
	var out []cliproto.Folder
	if err := e.call(cliproto.CmdFolders, req, &out); err != nil {
		return err
	}
	if e.json {
		return e.printJSON(nonNil(out))
	}
	rows := [][]string{{"ID", "ACCOUNT", "ROLE", "PATH"}}
	for _, f := range out {
		rows = append(rows, []string{strconv.FormatInt(f.ID, 10), strconv.FormatInt(f.AccountID, 10), f.Role, line(f.Path, 80)})
	}
	table(e.opts.Stdout, rows)
	return nil
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// printMessages prints a message list, or only its ids.
func (e *env) printMessages(msgs []cliproto.Message, idsOnly bool) error {
	if idsOnly {
		for _, m := range msgs {
			e.printf("%d\n", m.ID)
		}
		return nil
	}
	if e.json {
		return e.printJSON(nonNil(msgs))
	}
	rows := [][]string{{"ID", "DATE", "", "FROM", "SUBJECT"}}
	for _, m := range msgs {
		marks := ""
		if m.Unread {
			marks += "U"
		}
		if m.Flagged {
			marks += "F"
		}
		if m.Attachments {
			marks += "A"
		}
		date := m.Date
		if t, err := time.Parse(time.RFC3339, m.Date); err == nil {
			date = t.Local().Format("2006-01-02 15:04")
		}
		from := m.From.Name
		if from == "" {
			from = m.From.Email
		}
		rows = append(rows, []string{strconv.FormatInt(m.ID, 10), date, marks, line(from, 28), line(m.Subject, 80)})
	}
	table(e.opts.Stdout, rows)
	return nil
}

func cmdList(e *env, args []string) error {
	fs := newFlags("list")
	var req cliproto.ListRequest
	var since string
	var idsOnly bool
	fs.StringVar(&req.Account, "account", "", "")
	fs.BoolVar(&req.Unread, "unread", false, "")
	fs.IntVar(&req.Limit, "limit", 0, "")
	fs.StringVar(&since, "since", "", "")
	fs.BoolVar(&idsOnly, "ids", false, "")
	rest, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(rest) > 1 {
		return usageErr("usage: pelton list [FOLDER] [flags]")
	}
	if len(rest) == 1 {
		req.Folder = rest[0]
	}
	if req.Since, err = parseSince(since, time.Now()); err != nil {
		return err
	}
	var out []cliproto.Message
	if err := e.call(cliproto.CmdList, req, &out); err != nil {
		return err
	}
	return e.printMessages(out, idsOnly)
}

func cmdSearch(e *env, args []string) error {
	fs := newFlags("search")
	var req cliproto.SearchRequest
	var idsOnly bool
	fs.StringVar(&req.Account, "account", "", "")
	fs.IntVar(&req.Limit, "limit", 0, "")
	fs.BoolVar(&idsOnly, "ids", false, "")
	rest, err := parse(fs, args)
	if err != nil {
		return err
	}
	req.Query = strings.TrimSpace(strings.Join(rest, " "))
	if req.Query == "" {
		return usageErr("usage: pelton search QUERY...")
	}
	var out []cliproto.Message
	if err := e.call(cliproto.CmdSearch, req, &out); err != nil {
		return err
	}
	return e.printMessages(out, idsOnly)
}

func oneID(args []string, usage string) (int64, error) {
	if len(args) != 1 {
		return 0, usageErr("usage: pelton %s", usage)
	}
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || id <= 0 {
		return 0, usageErr("%q is not a message id", args[0])
	}
	return id, nil
}

func cmdShow(e *env, args []string) error {
	fs := newFlags("show")
	var headers, text, html, raw bool
	fs.BoolVar(&headers, "headers", false, "")
	fs.BoolVar(&text, "text", false, "")
	fs.BoolVar(&html, "html", false, "")
	fs.BoolVar(&raw, "raw", false, "")
	rest, err := parse(fs, args)
	if err != nil {
		return err
	}
	id, err := oneID(rest, "show ID [--headers] [--text | --html | --raw]")
	if err != nil {
		return err
	}
	if n := countTrue(text, html, raw); n > 1 {
		return usageErr("give only one of --text, --html and --raw")
	}
	var m cliproto.MessageDetail
	if err := e.call(cliproto.CmdShow, cliproto.ShowRequest{ID: id, Headers: headers, Raw: raw}, &m); err != nil {
		return err
	}
	if e.json {
		return e.printJSON(m)
	}
	if raw {
		if e.opts.StdoutTerminal {
			fmt.Fprint(e.opts.Stdout, clean(m.Raw, true))
		} else {
			fmt.Fprint(e.opts.Stdout, m.Raw)
		}
		return nil
	}
	if headers {
		fmt.Fprint(e.opts.Stdout, clean(m.Headers, true))
		e.printf("\n")
	} else {
		from := m.From.Email
		if m.From.Name != "" {
			from = m.From.Name + " <" + m.From.Email + ">"
		}
		e.printf("From:    %s\n", line(from, 0))
		e.printf("To:      %s\n", line(m.To, 0))
		if m.Cc != "" {
			e.printf("Cc:      %s\n", line(m.Cc, 0))
		}
		e.printf("Date:    %s\n", line(m.Date, 0))
		e.printf("Subject: %s\n", line(m.Subject, 0))
		e.printf("Folder:  %s\n", line(m.Folder, 0))
		for _, a := range m.AttachList {
			e.printf("Attach:  %s (%d bytes)\n", line(a.Name, 0), a.Size)
		}
		e.printf("\n")
	}
	body := m.Text
	if html || (body == "" && m.HTML != "") && !text {
		body = m.HTML
	}
	fmt.Fprint(e.opts.Stdout, clean(body, true))
	if !strings.HasSuffix(body, "\n") {
		e.printf("\n")
	}
	return nil
}

func countTrue(bs ...bool) int {
	n := 0
	for _, b := range bs {
		if b {
			n++
		}
	}
	return n
}

func cmdAttachments(e *env, args []string) error {
	id, err := oneID(args, "attachments ID")
	if err != nil {
		return err
	}
	var out []cliproto.Attachment
	if err := e.call(cliproto.CmdAttachments, cliproto.IDRequest{ID: id}, &out); err != nil {
		return err
	}
	if e.json {
		return e.printJSON(nonNil(out))
	}
	rows := [][]string{{"ID", "SIZE", "TYPE", "NAME"}}
	for _, a := range out {
		rows = append(rows, []string{strconv.FormatInt(a.ID, 10), strconv.FormatInt(a.Size, 10), line(a.ContentType, 40), line(a.Name, 80)})
	}
	table(e.opts.Stdout, rows)
	return nil
}

func cmdSaveAttachment(e *env, args []string) error {
	fs := newFlags("save-attachment")
	dir := "."
	var force bool
	fs.StringVar(&dir, "to", ".", "")
	fs.BoolVar(&force, "force", false, "")
	rest, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 2 {
		return usageErr("usage: pelton save-attachment ID NAME [--to DIR] [--force]")
	}
	id, err := oneID(rest[:1], "save-attachment ID NAME")
	if err != nil {
		return err
	}
	var list []cliproto.Attachment
	if err := e.call(cliproto.CmdAttachments, cliproto.IDRequest{ID: id}, &list); err != nil {
		return err
	}
	var target *cliproto.Attachment
	for i := range list {
		if list[i].Name == rest[1] || strconv.FormatInt(list[i].ID, 10) == rest[1] {
			target = &list[i]
			break
		}
	}
	if target == nil {
		return &exitError{code: ExitNotFound, msg: fmt.Sprintf("message %d has no attachment %q", id, line(rest[1], 80))}
	}
	resp, err := e.request(cliproto.CmdAttachment, cliproto.AttachmentRequest{ID: id, Name: strconv.FormatInt(target.ID, 10)})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	path, err := saveInto(dir, safeName(target.Name, "attachment"), resp.Body, force)
	if err != nil {
		return err
	}
	if e.json {
		return e.printJSON(map[string]string{"path": path})
	}
	e.printf("%s\n", line(path, 0))
	return nil
}

func actCommand(action string) func(*env, []string) error {
	return func(e *env, args []string) error {
		fs := newFlags(action)
		var to string
		if action == cliproto.ActMove {
			fs.StringVar(&to, "to", "", "")
		}
		rest, err := parse(fs, args)
		if err != nil {
			return err
		}
		if action == cliproto.ActMove && to == "" {
			return usageErr("usage: pelton move IDS... --to FOLDER")
		}
		ids, err := e.ids(rest)
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			return usageErr("no message ids; give them as arguments or \"-\" to read them from stdin")
		}
		var results []cliproto.Result
		if err := e.call(cliproto.CmdAct, cliproto.ActRequest{Action: action, IDs: ids, To: to}, &results); err != nil {
			return err
		}
		if e.json {
			if err := e.printJSON(nonNil(results)); err != nil {
				return err
			}
		}
		var first error
		for _, r := range results {
			if r.OK {
				if !e.json {
					e.printf("%d %s\n", r.ID, pastTense(action))
				}
				continue
			}
			if !e.json {
				fmt.Fprintf(e.opts.Stderr, "pelton: %d: %s\n", r.ID, clean(r.Error, false))
			}
			if first == nil {
				first = &exitError{code: codeExit(r.Code)}
			}
		}
		return first
	}
}

func pastTense(action string) string {
	switch action {
	case cliproto.ActFlag:
		return "flagged"
	case cliproto.ActUnflag:
		return "unflagged"
	case cliproto.ActRead:
		return "marked read"
	case cliproto.ActUnread:
		return "marked unread"
	case cliproto.ActArchive:
		return "archived"
	case cliproto.ActTrash:
		return "deleted"
	}
	return "moved"
}
