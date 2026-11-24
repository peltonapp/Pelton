package cli

import (
	"bufio"
	"flag"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/cliproto"
)

// command is one pelton command. Its help is the documentation.
type command struct {
	name    string
	usage   string
	summary string
	detail  string
	run     func(e *env, args []string) error
}

var commands []command

func init() {
	commands = []command{
		{"status", "status", "Say whether Pelton is running, locked, and which switches are on.", "", cmdStatus},
		{"accounts", "accounts", "List the mail accounts.", "Needs: Read mail.", cmdAccounts},
		{"folders", "folders [ACCOUNT]", "List the folders of every account, or of one.", "ACCOUNT is an account id, address or name.\nNeeds: Read mail.", cmdFolders},
		{"list", "list [FOLDER] [--account A] [--unread] [--limit N] [--since WHEN] [--ids]", "List messages in a folder, newest first.",
			"FOLDER is a folder id, a role (inbox, sent, drafts, archive, trash, junk), a path or a name.\nWithout one, the inbox of every account. WHEN is YYYY-MM-DD or an age such as 12h, 7d or 2w.\n--ids prints only message ids, one per line.\nNeeds: Read mail.", cmdList},
		{"search", "search QUERY... [--account A] [--limit N] [--ids]", "Search mail with the app's search syntax.",
			"QUERY takes the operators of the search field: from:, to:, subject:, in:, has:attachment,\nhas:note, is:unread, is:flagged, before:DATE, after:DATE and note:.\nNeeds: Search mail.", cmdSearch},
		{"show", "show ID [--headers] [--text | --html | --raw]", "Print one message.",
			"--headers adds the full header block and --raw prints the whole source; both fetch it from the server.\n--raw writes the source unchanged when stdout is not a terminal.\nNeeds: Read mail.", cmdShow},
		{"attachments", "attachments ID", "List the attachments of a message.", "Needs: Read mail.", cmdAttachments},
		{"save-attachment", "save-attachment ID NAME [--to DIR] [--force]", "Save one attachment to a folder.",
			"NAME is the attachment's file name or id. The file is written into DIR (default: the current\nfolder) under its own name, never outside it, and never over an existing file without --force.\n--force still refuses to write through a symbolic link.\nNeeds: Read mail.", cmdSaveAttachment},
		{"flag", "flag IDS...", "Flag messages.", actDetail, actCommand(cliproto.ActFlag)},
		{"unflag", "unflag IDS...", "Remove the flag from messages.", actDetail, actCommand(cliproto.ActUnflag)},
		{"read", "read IDS...", "Mark messages read.", actDetail, actCommand(cliproto.ActRead)},
		{"unread", "unread IDS...", "Mark messages unread.", actDetail, actCommand(cliproto.ActUnread)},
		{"archive", "archive IDS...", "Archive messages.", actDetail, actCommand(cliproto.ActArchive)},
		{"trash", "trash IDS...", "Move messages to the trash.",
			actDetail + "\nA message already in the trash, or in an account without one, is deleted for good; Pelton\nasks in its window first unless \"Delete permanently without asking\" is on.", actCommand(cliproto.ActTrash)},
		{"move", "move IDS... --to FOLDER", "Move messages to a folder of their own account.", actDetail, actCommand(cliproto.ActMove)},
		{"send", "send --to ADDR... [--cc ADDR] [--bcc ADDR] [--subject S] [--body TEXT] [--attach FILE]... [--from ACCOUNT] [--draft]", "Send a message, or keep it as a draft.",
			"Without --body the body is read from stdin. Each address flag can be given more than once.\nPelton asks in its window before sending unless \"Send without asking\" is on. Before-send\nautomations, their recipient check and automatic encryption apply as for any message.\n--draft saves a draft and sends nothing.\nNeeds: Send mail.", cmdSend},
		{"compose", "compose [MAILTO] [--to ADDR] [--cc ADDR] [--bcc ADDR] [--subject S] [--body TEXT]", "Open a composer filled in. Nothing is sent.", "", cmdCompose},
		{"run", "run SCRIPT [--dry-run] [--on ID] [--set KEY=VALUE]...", "Run an automation that is started by hand.",
			"SCRIPT is the automation's name or id. It runs with its own permissions and asks its own\nquestions in the window. --dry-run uses the test path and changes nothing. --on gives the\nmessage to run on, --set replaces one of its settings for this run.\nNeeds: Run automations.", cmdRun},
		{"export", "export (IDS... | FOLDER) --format mbox|eml --to PATH [--account A] [--force]", "Export messages from the server copy.",
			"mbox writes one file at PATH, refusing an existing one without --force. eml writes one file per\nmessage into the folder PATH, adding -2, -3 to a name already taken.\nNeeds: Export mail.", cmdExport},
		{"calendar", "calendar [--from DATE] [--to DATE]", "List calendar events, from today for a week unless told.", "Needs: Read calendar, tasks and contacts.", cmdCalendar},
		{"tasks", "tasks [--list NAME] [--due DATE] [--done]", "List open tasks.", "--due keeps tasks due on DATE or earlier (today, tomorrow or YYYY-MM-DD); --done includes finished ones.\nNeeds: Read calendar, tasks and contacts.", cmdTasks},
		{"task", "task add TITLE [--due DATE] [--list NAME]", "Add a task.", "Needs: Add tasks.", cmdTask},
		{"contacts", "contacts QUERY", "Find contacts and addresses you write to.", "Needs: Read calendar, tasks and contacts.", cmdContacts},
		{"open", "open [ID | MAILTO | FILE]", "Bring Pelton forward on a message, a mailto: link, an .ics or a .peltonscript file.",
			"Starts Pelton first when it is not running and this system allows it.", cmdOpen},
		{"completion", "completion bash|zsh|fish", "Print a shell completion script.", "", cmdCompletion},
		{"help", "help [COMMAND]", "Show help.", "", cmdHelp},
		{"version", "version", "Print the version of this command.", "", cmdVersion},
	}
}

const actDetail = "IDS are message ids; \"-\" reads them from stdin, one per line (the first word of each line).\nNeeds: Change mail."

func lookup(name string) (command, bool) {
	for _, c := range commands {
		if c.name == name {
			return c, true
		}
	}
	return command{}, false
}

func (e *env) dispatch(args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		return cmdHelp(e, nil)
	}
	if args[0] == "--version" {
		return cmdVersion(e, nil)
	}
	c, ok := lookup(args[0])
	if !ok {
		return usageErr("unknown command %q; see pelton help", args[0])
	}
	if slices.Contains(args[1:], "-h") || slices.Contains(args[1:], "--help") {
		return cmdHelp(e, []string{c.name})
	}
	return c.run(e, args[1:])
}

// listFlag collects a flag given more than once.
type listFlag []string

func (l *listFlag) String() string     { return strings.Join(*l, ",") }
func (l *listFlag) Set(v string) error { *l = append(*l, v); return nil }

// parse reads flags anywhere among the arguments and returns the others.
func parse(fs *flag.FlagSet, args []string) ([]string, error) {
	fs.SetOutput(io.Discard)
	var rest []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, usageErr("%s: %v", fs.Name(), err)
		}
		args = fs.Args()
		if len(args) == 0 {
			return rest, nil
		}
		if args[0] == "--" {
			return append(rest, args[1:]...), nil
		}
		rest = append(rest, args[0])
		args = args[1:]
	}
}

func newFlags(name string) *flag.FlagSet {
	return flag.NewFlagSet(name, flag.ContinueOnError)
}

// ids reads message ids from the arguments, "-" reading them from stdin.
func (e *env) ids(args []string) ([]int64, error) {
	var out []int64
	for _, a := range args {
		if a != "-" {
			id, err := strconv.ParseInt(a, 10, 64)
			if err != nil || id <= 0 {
				return nil, usageErr("%q is not a message id", a)
			}
			out = append(out, id)
			continue
		}
		sc := bufio.NewScanner(e.opts.Stdin)
		for sc.Scan() {
			fields := strings.Fields(sc.Text())
			if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
				continue
			}
			id, err := strconv.ParseInt(fields[0], 10, 64)
			if err != nil || id <= 0 {
				return nil, usageErr("%q on stdin is not a message id", line(fields[0], 40))
			}
			out = append(out, id)
		}
		if err := sc.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// parseSince reads YYYY-MM-DD as local midnight, or an age like 12h, 7d, 2w.
func parseSince(v string, now time.Time) (string, error) {
	if v == "" {
		return "", nil
	}
	if t, err := time.ParseInLocation("2006-01-02", v, time.Local); err == nil {
		return t.Format(time.RFC3339), nil
	}
	if len(v) >= 2 {
		n, err := strconv.Atoi(v[:len(v)-1])
		unit := map[byte]time.Duration{'h': time.Hour, 'd': 24 * time.Hour, 'w': 7 * 24 * time.Hour}[v[len(v)-1]]
		if err == nil && n >= 0 && unit != 0 {
			return now.Add(-time.Duration(n) * unit).Format(time.RFC3339), nil
		}
	}
	return "", usageErr("%q is not a date (YYYY-MM-DD) or an age (12h, 7d, 2w)", v)
}

// parseDay reads today, tomorrow or YYYY-MM-DD.
func parseDay(v string, now time.Time) (time.Time, error) {
	y, m, d := now.Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, time.Local)
	switch v {
	case "today":
		return today, nil
	case "tomorrow":
		return today.AddDate(0, 0, 1), nil
	}
	t, err := time.ParseInLocation("2006-01-02", v, time.Local)
	if err != nil {
		return time.Time{}, usageErr("%q is not today, tomorrow or a date (YYYY-MM-DD)", v)
	}
	return t, nil
}
