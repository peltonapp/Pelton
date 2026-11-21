package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/cliproto"
	"github.com/peltonapp/Pelton/internal/cliserver"
)

var update = flag.Bool("update", false, "rewrite the golden files")

// rlo is U+202E, the right-to-left override.
const rlo = "\xe2\x80\xae"

func TestCleanNeutralisesTerminalControl(t *testing.T) {
	cases := []struct{ in, line, multi string }{
		{"\x1b]0;pwned\x07title", `\x1b]0;pwned\x07title`, `\x1b]0;pwned\x07title`},
		{"a\x1b[2Jb", `a\x1b[2Jb`, `a\x1b[2Jb`},
		{"over\rwrite", `over\x0dwrite`, `over\x0dwrite`},
		{"two\r\nlines\tand tab", "two lines and tab", "two\nlines\tand tab"},
		{"c1 \u009b31m", `c1 \u009b31m`, `c1 \u009b31m`},
		{"hide " + rlo + "txt.exe", `hide \u202` + "etxt.exe", `hide \u202` + "etxt.exe"},
		{"bad \xff utf8", "bad � utf8", "bad � utf8"},
		{"back\bspace\x7f", `back\x08space\x7f`, `back\x08space\x7f`},
		{"plain Grüße", "plain Grüße", "plain Grüße"},
	}
	for _, c := range cases {
		if got := clean(c.in, false); got != c.line {
			t.Errorf("clean(%q, false) = %q, want %q", c.in, got, c.line)
		}
		if got := clean(c.in, true); got != c.multi {
			t.Errorf("clean(%q, true) = %q, want %q", c.in, got, c.multi)
		}
	}
}

func TestIDsFromStdin(t *testing.T) {
	e := &env{opts: Options{Stdin: strings.NewReader("12  2026-01-01  Subject\n\n# comment\n 7\n")}}
	got, err := e.ids([]string{"3", "-"})
	if err != nil || len(got) != 3 || got[0] != 3 || got[1] != 12 || got[2] != 7 {
		t.Fatalf("%v %v", got, err)
	}
	e.opts.Stdin = strings.NewReader("abc\n")
	if _, err := e.ids([]string{"-"}); exitCode(err) != ExitUsage {
		t.Fatalf("bad stdin id: %v", err)
	}
	if _, err := e.ids([]string{"0"}); exitCode(err) != ExitUsage {
		t.Fatal("accepted id 0")
	}
}

func TestSafeName(t *testing.T) {
	for in, want := range map[string]string{
		"../../etc/passwd":      "passwd",
		"/etc/passwd":           "passwd",
		`..\..\windows\win.ini`: "win.ini",
		"..":                    "attachment",
		".":                     "attachment",
		"":                      "attachment",
		"a\x1b[2Jb.pdf":         "a_[2Jb.pdf",
		"CON.txt":               "_CON.txt",
		"report?.pdf":           "report_.pdf",
		"trailing. ":            "trailing",
		"evil" + rlo + "fdp.exe": "evil_fdp.exe",
	} {
		if got := safeName(in, "attachment"); got != want {
			t.Errorf("safeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSaveIntoNeverLeavesTheFolderOrOverwrites(t *testing.T) {
	dir := t.TempDir()
	path, err := saveInto(dir, "a.txt", strings.NewReader("one"), false)
	if err != nil || filepath.Dir(path) != dir {
		t.Fatalf("%s %v", path, err)
	}
	if _, err := saveInto(dir, "a.txt", strings.NewReader("two"), false); err == nil {
		t.Fatal("overwrote without --force")
	}
	if data, _ := os.ReadFile(path); string(data) != "one" {
		t.Fatalf("file changed to %q", data)
	}
	if _, err := saveInto(dir, "a.txt", strings.NewReader("two"), true); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); string(data) != "two" {
		t.Fatalf("--force did not replace: %q", data)
	}
	for _, name := range []string{"../escape", "sub/file", ".."} {
		if _, err := saveInto(dir, name, strings.NewReader("x"), true); err == nil {
			t.Errorf("wrote %q", name)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "escape")); err == nil {
		t.Fatal("wrote outside the folder")
	}
	if _, err := saveInto(filepath.Join(dir, "a.txt"), "b", strings.NewReader("x"), false); err == nil {
		t.Fatal("wrote into a file as if it were a folder")
	}

	if runtime.GOOS == "windows" {
		return
	}
	outside := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(outside, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "link.txt")); err != nil {
		t.Fatal(err)
	}
	for _, force := range []bool{false, true} {
		if _, err := saveInto(dir, "link.txt", strings.NewReader("pwned"), force); err == nil {
			t.Fatalf("wrote through a link (force %v)", force)
		}
	}
	if data, _ := os.ReadFile(outside); string(data) != "keep" {
		t.Fatalf("the link target changed: %q", data)
	}
}

func TestCreateUniqueAddsASuffix(t *testing.T) {
	dir := t.TempDir()
	for _, want := range []string{"m.eml", "m-2.eml", "m-3.eml"} {
		f, err := createUnique(dir, "m.eml")
		if err != nil {
			t.Fatal(err)
		}
		f.Close()
		if filepath.Base(f.Name()) != want {
			t.Fatalf("%s, want %s", f.Name(), want)
		}
	}
}

func TestMboxQuotesFromLines(t *testing.T) {
	var b bytes.Buffer
	w := bufio.NewWriter(&b)
	if err := writeMbox(w, []byte("Subject: x\r\n\r\nFrom here\r\n>From there\r\nok"), "Mon Jan  2 15:04:05 2006"); err != nil {
		t.Fatal(err)
	}
	w.Flush()
	want := "From MAILER-DAEMON Mon Jan  2 15:04:05 2006\nSubject: x\n\n>From here\n>>From there\nok\n\n"
	if b.String() != want {
		t.Fatalf("%q", b.String())
	}
}

func TestParseKeepsFlagsAnywhere(t *testing.T) {
	fs := newFlags("t")
	n := fs.Int("limit", 0, "")
	rest, err := parse(fs, []string{"INBOX", "--limit", "5", "-", "x"})
	if err != nil || *n != 5 || strings.Join(rest, ",") != "INBOX,-,x" {
		t.Fatalf("%v %d %v", rest, *n, err)
	}
}

// cannedBackend answers every command with fixed data, for the golden files.
type cannedBackend struct{}

func (cannedBackend) State() cliserver.State {
	caps := map[string]bool{}
	for _, c := range cliproto.Capabilities {
		caps[c] = c != cliproto.CapSendUnasked && c != cliproto.CapDeleteUnasked
	}
	return cliserver.State{Access: true, Caps: caps, Version: "2026.5"}
}

func (cannedBackend) Record(cliserver.Entry) {}

var cannedMessage = cliproto.Message{
	ID: 42, AccountID: 1, FolderID: 3, Folder: "INBOX", Date: "2026-03-10T09:30:00Z",
	From: cliproto.Address{Name: "Ada " + rlo + "Lovelace", Email: "ada@example.org"}, Subject: "Hi \x1b[2J<there>",
	Unread: true, Attachments: true,
}

func (cannedBackend) Handle(_ context.Context, command string, _ []byte) (cliserver.Response, error) {
	var data any
	switch command {
	case cliproto.CmdAccounts:
		data = []cliproto.Account{{ID: 1, Email: "me@example.org", Name: "Work"}}
	case cliproto.CmdFolders:
		data = []cliproto.Folder{{ID: 3, AccountID: 1, Name: "INBOX", Path: "INBOX", Role: "inbox"}}
	case cliproto.CmdList, cliproto.CmdSearch:
		data = []cliproto.Message{cannedMessage}
	case cliproto.CmdShow:
		data = cliproto.MessageDetail{Message: cannedMessage, To: "me@example.org", MessageID: "<a@b>", Text: "body\n",
			AttachList: []cliproto.Attachment{{ID: 9, Name: "a.pdf", ContentType: "application/pdf", Size: 3}}}
	case cliproto.CmdAttachments:
		data = []cliproto.Attachment{{ID: 9, Name: "a.pdf", ContentType: "application/pdf", Size: 3}}
	case cliproto.CmdAct:
		data = []cliproto.Result{{ID: 42, OK: true}}
	case cliproto.CmdSend:
		data = cliproto.Sent{OutboxID: 5, Protection: "none"}
	case cliproto.CmdRun:
		data = cliproto.Run{ID: 7, ScriptID: 2, ScriptName: "tidy", Status: "ok", Changes: 1, DurationMs: 12,
			Steps: []cliproto.Step{{Kind: "call", Function: "mail.flag", Summary: "flag 42"}}}
	case cliproto.CmdCalendar:
		data = []cliproto.Event{{ID: 1, Calendar: 2, Summary: "Standup", Start: "2026-03-10T09:00:00Z", End: "2026-03-10T09:15:00Z"}}
	case cliproto.CmdTasks:
		data = []cliproto.Task{{ID: 4, List: "Home", Title: "Milk", Due: "2026-03-11"}}
	case cliproto.CmdTaskAdd:
		data = cliproto.Task{ID: 5, List: "Home", Title: "Bread"}
	case cliproto.CmdContacts:
		data = []cliproto.Contact{{Name: "Ada", Email: "ada@example.org", Contact: true}}
	default:
		data = struct{}{}
	}
	return cliserver.Response{Data: data}, nil
}

func cannedServer(t *testing.T) cliproto.Paths {
	t.Helper()
	dir, err := os.MkdirTemp("", "pc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	paths := cliproto.PathsIn(dir)
	s := cliserver.New(paths, cannedBackend{}, nil)
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Stop(context.Background()) })
	if err := s.SetToken("tok"); err != nil {
		t.Fatal(err)
	}
	return paths
}

// TestGolden pins what each command prints, with --json and without, so a
// field rename or a change in the text is a deliberate edit of the golden
// file. Run with -update to rewrite them.
func TestGolden(t *testing.T) {
	local := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = local })
	paths := cannedServer(t)
	cases := map[string][]string{
		"status":      {"status"},
		"accounts":    {"accounts"},
		"folders":     {"folders"},
		"list":        {"list"},
		"search":      {"search", "from:ada"},
		"show":        {"show", "42"},
		"attachments": {"attachments", "42"},
		"flag":        {"flag", "42"},
		"send":        {"send", "--to", "a@example.org", "--body", "x"},
		"run":         {"run", "tidy"},
		"calendar":    {"calendar"},
		"tasks":       {"tasks"},
		"task-add":    {"task", "add", "Bread"},
		"contacts":    {"contacts", "ada"},
	}
	for name, args := range cases {
		for _, asJSON := range []bool{false, true} {
			file := name + ".txt"
			a := args
			if asJSON {
				file = name + ".json"
				a = append(append([]string{}, args...), "--json")
			}
			var out, errb bytes.Buffer
			code := Main(a, Options{Stdin: strings.NewReader(""), StdinTerminal: true, Stdout: &out, Stderr: &errb, Paths: paths, Version: "test"})
			if code != ExitOK {
				t.Fatalf("%v: exit %d: %s", a, code, errb.String())
			}
			if strings.Contains(out.String(), "\x1b") || strings.Contains(out.String(), rlo) {
				t.Fatalf("%v printed a raw control character", a)
			}
			if asJSON && !json.Valid(out.Bytes()) {
				t.Fatalf("%v: not JSON: %s", a, out.String())
			}
			golden := filepath.Join("testdata", file)
			if *update {
				if err := os.MkdirAll("testdata", 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(golden, out.Bytes(), 0o644); err != nil {
					t.Fatal(err)
				}
				continue
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("%v (run with -update to create it)", err)
			}
			if out.String() != string(want) {
				t.Errorf("%s changed:\n%s\nwant:\n%s", file, out.String(), want)
			}
		}
	}
}

func TestNotRunningAndUsage(t *testing.T) {
	dir := t.TempDir()
	opts := Options{Stdin: strings.NewReader(""), Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Paths: cliproto.PathsIn(dir)}
	if code := Main([]string{"list"}, opts); code != ExitNotRunning {
		t.Fatalf("not running: exit %d", code)
	}
	if code := Main([]string{"open", "42"}, opts); code != ExitNotRunning {
		t.Fatalf("open without a launcher: exit %d", code)
	}
	for _, args := range [][]string{{"bogus"}, {"move", "1"}, {"show"}, {"export", "1", "--format", "pdf", "--to", "x"}, {"completion", "tcsh"}} {
		if code := Main(args, opts); code != ExitUsage {
			t.Fatalf("%v: exit %d", args, code)
		}
	}
	for _, args := range [][]string{{"help"}, {"help", "send"}, {"version"}, {"completion", "bash"}, {"completion", "zsh"}, {"completion", "fish"}} {
		if code := Main(args, opts); code != ExitOK {
			t.Fatalf("%v: exit %d", args, code)
		}
	}
}

func TestIsCommand(t *testing.T) {
	for args, want := range map[string]bool{
		"": false, "--debug": false, "mailto:a@b": false, "/tmp/x.ics": false, "--quick-compose": false,
		"status": true, "list INBOX": true, "--help": true, "help": true, "version": true,
	} {
		if got := IsCommand(strings.Fields(args)); got != want {
			t.Errorf("IsCommand(%q) = %v", args, got)
		}
	}
}

func TestHelpHasNoDashes(t *testing.T) {
	var out bytes.Buffer
	opts := Options{Stdout: &out, Stderr: &out, Paths: cliproto.PathsIn(t.TempDir())}
	Main([]string{"help"}, opts)
	for _, c := range commands {
		Main([]string{"help", c.name}, opts)
	}
	if strings.ContainsAny(out.String(), "–—") {
		t.Fatal("help text has an en or em dash")
	}
}
