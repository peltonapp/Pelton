package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	goflag "flag"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/cliproto"
)

// maxAttachment bounds the files one send carries, all together.
const maxAttachment = 48 << 20

func cmdSend(e *env, args []string) error {
	fs := newFlags("send")
	var to, cc, bcc, attach listFlag
	var req cliproto.SendRequest
	var body string
	var draft bool
	fs.Var(&to, "to", "")
	fs.Var(&cc, "cc", "")
	fs.Var(&bcc, "bcc", "")
	fs.Var(&attach, "attach", "")
	fs.StringVar(&req.Subject, "subject", "", "")
	fs.StringVar(&body, "body", "", "")
	fs.StringVar(&req.From, "from", "", "")
	fs.BoolVar(&draft, "draft", false, "")
	rest, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		return usageErr("send takes no arguments besides its flags; addresses go in --to")
	}
	req.To, req.Cc, req.Bcc = to, cc, bcc
	if !draft && len(to)+len(cc)+len(bcc) == 0 {
		return usageErr("a message needs at least one recipient (--to, --cc or --bcc)")
	}
	bodySet := false
	fs.Visit(func(f *goflag.Flag) { bodySet = bodySet || f.Name == "body" })
	req.Body = body
	if !bodySet && !e.opts.StdinTerminal {
		data, err := io.ReadAll(io.LimitReader(e.opts.Stdin, 16<<20))
		if err != nil {
			return err
		}
		req.Body = string(data)
	}
	total := 0
	for _, path := range attach {
		data, err := os.ReadFile(path)
		if err != nil {
			return &exitError{code: ExitError, msg: fmt.Sprintf("cannot attach %s: %v", path, err)}
		}
		total += len(data)
		if total > maxAttachment {
			return &exitError{code: ExitError, msg: "the attachments are larger than 48 MiB together"}
		}
		ct := mime.TypeByExtension(filepath.Ext(path))
		if ct == "" {
			ct = "application/octet-stream"
		}
		req.Attachments = append(req.Attachments, cliproto.OutAttachment{Name: filepath.Base(path), ContentType: ct, Content: data})
	}
	command := cliproto.CmdSend
	if draft {
		command = cliproto.CmdDraft
	}
	if !draft && !e.json {
		fmt.Fprintln(e.opts.Stderr, "pelton: waiting for the send to be confirmed in Pelton...")
	}
	var sent cliproto.Sent
	if err := e.call(command, req, &sent); err != nil {
		return err
	}
	if e.json {
		return e.printJSON(sent)
	}
	if draft {
		e.printf("saved as draft %d\n", sent.DraftID)
	} else {
		e.printf("queued for sending (outbox %d, protection %s)\n", sent.OutboxID, sent.Protection)
	}
	return nil
}

func cmdCompose(e *env, args []string) error {
	fs := newFlags("compose")
	var to, cc, bcc listFlag
	var req cliproto.ComposeRequest
	fs.Var(&to, "to", "")
	fs.Var(&cc, "cc", "")
	fs.Var(&bcc, "bcc", "")
	fs.StringVar(&req.Subject, "subject", "", "")
	fs.StringVar(&req.Body, "body", "", "")
	rest, err := parse(fs, args)
	if err != nil {
		return err
	}
	switch {
	case len(rest) == 1 && strings.HasPrefix(strings.ToLower(rest[0]), "mailto:"):
		req.Mailto = rest[0]
	case len(rest) == 0:
	default:
		return usageErr("usage: pelton compose [MAILTO] [--to ADDR] [--subject S] [--body TEXT]")
	}
	req.To, req.Cc, req.Bcc = to, cc, bcc
	if err := e.call(cliproto.CmdCompose, req, nil); err != nil {
		return err
	}
	if e.json {
		return e.printJSON(map[string]bool{"opened": true})
	}
	return nil
}

func cmdRun(e *env, args []string) error {
	fs := newFlags("run")
	var req cliproto.RunRequest
	var set listFlag
	fs.BoolVar(&req.DryRun, "dry-run", false, "")
	fs.Int64Var(&req.On, "on", 0, "")
	fs.Var(&set, "set", "")
	rest, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return usageErr("usage: pelton run SCRIPT [--dry-run] [--on ID] [--set KEY=VALUE]...")
	}
	req.Script = rest[0]
	for _, kv := range set {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || k == "" {
			return usageErr("--set takes KEY=VALUE, not %q", kv)
		}
		if req.Setting == nil {
			req.Setting = map[string]string{}
		}
		req.Setting[k] = v
	}
	var run cliproto.Run
	if err := e.call(cliproto.CmdRun, req, &run); err != nil {
		return err
	}
	if e.json {
		if err := e.printJSON(run); err != nil {
			return err
		}
	} else {
		kind := "run"
		if run.Dry {
			kind = "dry run"
		}
		e.printf("%s %q: %s, %d change(s), %d ms\n", kind, line(run.ScriptName, 0), line(run.Status, 0), run.Changes, run.DurationMs)
		for _, st := range run.Steps {
			e.printf("  %-8s %s\n", line(st.Kind, 8), line(st.Summary, 0))
			if st.Error != "" {
				e.printf("           error: %s\n", line(st.Error, 0))
			}
		}
		if run.Error != "" {
			e.printf("error: %s\n", line(run.Error, 0))
		}
	}
	if run.Status != "ok" {
		return &exitError{code: ExitError}
	}
	return nil
}

func cmdExport(e *env, args []string) error {
	fs := newFlags("export")
	var format, to string
	var req cliproto.ExportRequest
	var force bool
	fs.StringVar(&format, "format", "", "")
	fs.StringVar(&to, "to", "", "")
	fs.StringVar(&req.Account, "account", "", "")
	fs.BoolVar(&force, "force", false, "")
	rest, err := parse(fs, args)
	if err != nil {
		return err
	}
	if (format != "mbox" && format != "eml") || to == "" || len(rest) == 0 {
		return usageErr("usage: pelton export (IDS... | FOLDER) --format mbox|eml --to PATH")
	}
	if allIDs(rest) {
		if req.IDs, err = e.ids(rest); err != nil {
			return err
		}
	} else if len(rest) == 1 {
		req.Folder = rest[0]
	} else {
		return usageErr("give message ids or one folder")
	}

	var mbox *bufio.Writer
	var finish func() error
	switch format {
	case "mbox":
		path, err := filepath.Abs(to)
		if err != nil {
			return err
		}
		if info, err := os.Lstat(path); err == nil {
			if !force {
				return &exitError{code: ExitError, msg: fmt.Sprintf("%s already exists; use --force to replace it", path)}
			}
			if !info.Mode().IsRegular() {
				return &exitError{code: ExitError, msg: fmt.Sprintf("%s is a link or not a regular file; it is not replaced", path)}
			}
		}
		tmp, err := os.CreateTemp(filepath.Dir(path), ".pelton-*.mbox")
		if err != nil {
			return err
		}
		defer os.Remove(tmp.Name())
		mbox = bufio.NewWriter(tmp)
		finish = func() error {
			if err := mbox.Flush(); err != nil {
				tmp.Close()
				return err
			}
			if err := tmp.Close(); err != nil {
				return err
			}
			if !force {
				if _, err := os.Lstat(path); err == nil {
					return &exitError{code: ExitError, msg: fmt.Sprintf("%s appeared while exporting; it is not replaced", path)}
				}
			}
			return os.Rename(tmp.Name(), path)
		}
	case "eml":
		if err := os.MkdirAll(to, 0o700); err != nil {
			return err
		}
	}

	resp, err := e.request(cliproto.CmdExport, req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	dec := json.NewDecoder(resp.Body)
	written, failed := 0, 0
	ended := false
	for {
		var item cliproto.ExportItem
		if err := dec.Decode(&item); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return &exitError{code: ExitError, msg: "the export was cut short"}
		}
		if item.End {
			ended = true
			break
		}
		if item.Error != "" {
			failed++
			fmt.Fprintf(e.opts.Stderr, "pelton: message %d: %s\n", item.ID, clean(item.Error, false))
			continue
		}
		if mbox != nil {
			date := time.Now()
			if t, err := time.Parse(time.RFC3339, item.Date); err == nil {
				date = t
			}
			if err := writeMbox(mbox, item.Raw, date.UTC().Format(time.ANSIC)); err != nil {
				return err
			}
		} else {
			f, err := createUnique(to, safeName(item.Name, strconv.FormatInt(item.ID, 10))+".eml")
			if err != nil {
				return err
			}
			if _, err := f.Write(item.Raw); err != nil {
				f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
		}
		written++
	}
	if !ended {
		return &exitError{code: ExitError, msg: "the export was cut short"}
	}
	if finish != nil {
		if err := finish(); err != nil {
			return err
		}
	}
	if e.json {
		if err := e.printJSON(map[string]int{"exported": written, "failed": failed}); err != nil {
			return err
		}
	} else {
		e.printf("exported %d message(s) to %s\n", written, line(to, 0))
	}
	if failed > 0 {
		return &exitError{code: ExitError, msg: fmt.Sprintf("%d message(s) could not be exported", failed)}
	}
	return nil
}

func allIDs(args []string) bool {
	for _, a := range args {
		if a == "-" {
			continue
		}
		if _, err := strconv.ParseInt(a, 10, 64); err != nil {
			return false
		}
	}
	return true
}

func cmdCalendar(e *env, args []string) error {
	fs := newFlags("calendar")
	var from, to string
	fs.StringVar(&from, "from", "today", "")
	fs.StringVar(&to, "to", "", "")
	rest, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		return usageErr("usage: pelton calendar [--from DATE] [--to DATE]")
	}
	start, err := parseDay(from, time.Now())
	if err != nil {
		return err
	}
	end := start.AddDate(0, 0, 7)
	if to != "" {
		if end, err = parseDay(to, time.Now()); err != nil {
			return err
		}
		end = end.AddDate(0, 0, 1)
	}
	var out []cliproto.Event
	if err := e.call(cliproto.CmdCalendar, cliproto.RangeRequest{From: start.Format(time.RFC3339), To: end.Format(time.RFC3339)}, &out); err != nil {
		return err
	}
	if e.json {
		return e.printJSON(nonNil(out))
	}
	rows := [][]string{{"START", "END", "SUMMARY", "LOCATION"}}
	for _, ev := range out {
		rows = append(rows, []string{when(ev.Start, ev.AllDay), when(ev.End, ev.AllDay), line(ev.Summary, 60), line(ev.Location, 40)})
	}
	table(e.opts.Stdout, rows)
	return nil
}

func when(v string, allDay bool) string {
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return line(v, 25)
	}
	if allDay {
		return t.Format("2006-01-02")
	}
	return t.Local().Format("2006-01-02 15:04")
}

func cmdTasks(e *env, args []string) error {
	fs := newFlags("tasks")
	var req cliproto.TasksRequest
	var due string
	fs.StringVar(&req.List, "list", "", "")
	fs.StringVar(&due, "due", "", "")
	fs.BoolVar(&req.Done, "done", false, "")
	rest, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		return usageErr("usage: pelton tasks [--list NAME] [--due DATE] [--done]")
	}
	if due != "" {
		day, err := parseDay(due, time.Now())
		if err != nil {
			return err
		}
		req.Due = day.Format("2006-01-02")
	}
	var out []cliproto.Task
	if err := e.call(cliproto.CmdTasks, req, &out); err != nil {
		return err
	}
	if e.json {
		return e.printJSON(nonNil(out))
	}
	rows := [][]string{{"ID", "DUE", "DONE", "LIST", "TITLE"}}
	for _, t := range out {
		rows = append(rows, []string{strconv.FormatInt(t.ID, 10), line(t.Due, 25), yesNo(t.Done), line(t.List, 24), line(t.Title, 80)})
	}
	table(e.opts.Stdout, rows)
	return nil
}

func cmdTask(e *env, args []string) error {
	fs := newFlags("task")
	var req cliproto.TaskAddRequest
	var due string
	fs.StringVar(&due, "due", "", "")
	fs.StringVar(&req.List, "list", "", "")
	rest, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(rest) < 2 || rest[0] != "add" {
		return usageErr("usage: pelton task add TITLE [--due DATE] [--list NAME]")
	}
	req.Title = strings.Join(rest[1:], " ")
	if due != "" {
		day, err := parseDay(due, time.Now())
		if err != nil {
			return err
		}
		req.Due = day.Format("2006-01-02")
	}
	var t cliproto.Task
	if err := e.call(cliproto.CmdTaskAdd, req, &t); err != nil {
		return err
	}
	if e.json {
		return e.printJSON(t)
	}
	e.printf("added task %d\n", t.ID)
	return nil
}

func cmdContacts(e *env, args []string) error {
	fs := newFlags("contacts")
	var req cliproto.QueryRequest
	fs.IntVar(&req.Limit, "limit", 0, "")
	rest, err := parse(fs, args)
	if err != nil {
		return err
	}
	req.Query = strings.Join(rest, " ")
	if req.Query == "" {
		return usageErr("usage: pelton contacts QUERY")
	}
	var out []cliproto.Contact
	if err := e.call(cliproto.CmdContacts, req, &out); err != nil {
		return err
	}
	if e.json {
		return e.printJSON(nonNil(out))
	}
	rows := [][]string{{"EMAIL", "NAME", "CONTACT"}}
	for _, c := range out {
		rows = append(rows, []string{line(c.Email, 60), line(c.Name, 60), yesNo(c.Contact)})
	}
	table(e.opts.Stdout, rows)
	return nil
}

// cmdOpen brings the app forward, starting it first when it is not running
// and this build knows how.
func cmdOpen(e *env, args []string) error {
	if len(args) > 1 {
		return usageErr("usage: pelton open [ID | MAILTO | FILE]")
	}
	var req cliproto.OpenRequest
	if len(args) == 1 {
		a := args[0]
		switch {
		case strings.HasPrefix(strings.ToLower(a), "mailto:"):
			req.Mailto = a
		case allIDs([]string{a}) && a != "-":
			req.ID, _ = strconv.ParseInt(a, 10, 64)
		default:
			abs, err := filepath.Abs(a)
			if err != nil {
				return err
			}
			req.File = abs
		}
	}
	err := e.call(cliproto.CmdOpen, req, nil)
	if !errors.Is(err, errNotRunning) {
		return err
	}
	if e.opts.Launch == nil {
		return &exitError{code: ExitNotRunning, msg: "Pelton is not running, and this pelton command cannot start it. Start Pelton first."}
	}
	var launchArgs []string
	if req.Mailto != "" {
		launchArgs = []string{req.Mailto}
	}
	if err := e.opts.Launch(launchArgs); err != nil {
		return &exitError{code: ExitNotRunning, msg: "could not start Pelton: " + err.Error()}
	}
	if req.Mailto != "" || (req.ID == 0 && req.File == "") {
		return nil
	}
	deadline := time.Now().Add(e.opts.WaitForApp)
	for time.Now().Before(deadline) {
		select {
		case <-e.ctx.Done():
			return &exitError{code: ExitError, msg: "interrupted"}
		case <-time.After(500 * time.Millisecond):
		}
		err = e.call(cliproto.CmdOpen, req, nil)
		if !errors.Is(err, errNotRunning) {
			return err
		}
	}
	return &exitError{code: ExitNotRunning, msg: "Pelton was started but did not answer in time"}
}
