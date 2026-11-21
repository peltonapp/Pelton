package desktop

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/peltonapp/Pelton/internal/applock"
	"github.com/peltonapp/Pelton/internal/cli"
	"github.com/peltonapp/Pelton/internal/cliproto"
	"github.com/peltonapp/Pelton/internal/outbox"
	"github.com/peltonapp/Pelton/internal/scripting"
	"github.com/peltonapp/Pelton/internal/storage"
)

// hostileSubject tries to clear the screen, retitle the terminal and hide
// what follows behind a right-to-left override.
const hostileSubject = "\x1b]0;pwned\x07Hi\x1b[2J\u009b31m" + rlo + "evil"

// rlo is U+202E, the right-to-left override.
const rlo = "\xe2\x80\xae"

// startCLIRig opens the app's command line socket in a short temporary
// folder, the way startup does.
func startCLIRig(t *testing.T, a *App) cliproto.Paths {
	t.Helper()
	keyring.MockInit()
	dir, err := os.MkdirTemp("", "pc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	a.cliPathsOverride = cliproto.PathsIn(dir)
	a.startCLI()
	t.Cleanup(a.stopCLI)
	if a.cli == nil {
		t.Fatal("the socket did not start")
	}
	return a.cliPathsOverride
}

func allowCLI(t *testing.T, a *App, caps ...string) {
	t.Helper()
	if err := a.SetCLIEnabled(true); err != nil {
		t.Fatal(err)
	}
	for _, c := range caps {
		if err := a.SetCLIPermission(c, true); err != nil {
			t.Fatal(err)
		}
	}
}

type cliOut struct {
	stdout, stderr string
	code           int
}

func runCLI(t *testing.T, paths cliproto.Paths, stdin string, args ...string) cliOut {
	t.Helper()
	var out, errb bytes.Buffer
	code := cli.Main(args, cli.Options{
		Stdin: strings.NewReader(stdin), Stdout: &out, Stderr: &errb, StdinTerminal: stdin == "",
		Paths: paths, Version: "test",
	})
	return cliOut{out.String(), errb.String(), code}
}

func (o cliOut) want(t *testing.T, code int) cliOut {
	t.Helper()
	if o.code != code {
		t.Fatalf("exit %d, want %d\nstdout: %s\nstderr: %s", o.code, code, o.stdout, o.stderr)
	}
	return o
}

func cliTestApp(t *testing.T) (*App, int64, int64, cliproto.Paths) {
	t.Helper()
	a, first := newScriptTestApp(t)
	m, err := a.store.GetMessage(a.ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	second, err := a.store.InsertMessage(a.ctx, &storage.Message{
		AccountID: m.AccountID, FolderID: m.FolderID, UID: 2, MessageID: "<two@example.test>",
		Subject: hostileSubject, FromName: rlo + "Mallory", FromAddress: "mallory@example.test",
		Date: time.Now().UTC().Add(-time.Minute), BodyPlain: "line\x1b[1A\rcovered",
	})
	if err != nil {
		t.Fatal(err)
	}
	return a, first, second, startCLIRig(t, a)
}

func TestCLIAccessSwitchesTokenAndLock(t *testing.T) {
	a, _, _, paths := cliTestApp(t)

	if o := runCLI(t, paths, "", "status").want(t, cli.ExitAccessOff); !strings.Contains(o.stdout, "Command line access is off") {
		t.Fatalf("status while off: %+v", o)
	}
	if _, err := os.Stat(paths.Token); err == nil {
		t.Fatal("a token file exists while access is off")
	}
	runCLI(t, paths, "", "list").want(t, cli.ExitAccessOff)

	allowCLI(t, a)
	if runtime.GOOS != "windows" {
		info, err := os.Stat(paths.Token)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("token file %v %v", info, err)
		}
	}
	o := runCLI(t, paths, "", "list").want(t, cli.ExitPermission)
	if !strings.Contains(o.stderr, "Read mail") {
		t.Fatalf("the refusal does not name the switch: %q", o.stderr)
	}
	for args, code := range map[string]int{
		"search invoice": cli.ExitPermission, "flag 1": cli.ExitPermission, "send --to x@example.test --body b": cli.ExitPermission,
		"run t": cli.ExitPermission, "export 1 --format eml --to x": cli.ExitPermission, "tasks": cli.ExitPermission,
		"task add milk": cli.ExitPermission, "contacts bo": cli.ExitPermission, "calendar": cli.ExitPermission,
	} {
		runCLI(t, paths, "", strings.Fields(args)...).want(t, code)
	}

	if err := a.SetCLIPermission(cliproto.CapRead, true); err != nil {
		t.Fatal(err)
	}
	runCLI(t, paths, "", "list").want(t, cli.ExitOK)

	if err := os.WriteFile(paths.Token, []byte("not the token"), 0o600); err != nil {
		t.Fatal(err)
	}
	runCLI(t, paths, "", "list").want(t, cli.ExitAccessOff)
	if err := a.applyCLIToken(); err != nil {
		t.Fatal(err)
	}
	runCLI(t, paths, "", "list").want(t, cli.ExitOK)

	a.lock = applock.New(nil)
	a.lock.Configure(true, 0)
	a.lock.Lock()
	runCLI(t, paths, "", "list").want(t, cli.ExitLocked)
	runCLI(t, paths, "", "open").want(t, cli.ExitLocked)
	if o := runCLI(t, paths, "", "status").want(t, cli.ExitOK); !strings.Contains(o.stdout, "Locked: yes") {
		t.Fatalf("status while locked: %s", o.stdout)
	}
	a.lock.Unlock()
	runCLI(t, paths, "", "list").want(t, cli.ExitOK)

	if err := a.SetCLIEnabled(false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(paths.Token); err == nil {
		t.Fatal("turning access off left the token file")
	}
	runCLI(t, paths, "", "list").want(t, cli.ExitAccessOff)
}

func TestCLIReadsMailSafely(t *testing.T) {
	a, first, second, paths := cliTestApp(t)
	allowCLI(t, a, cliproto.CapRead, cliproto.CapChange)

	o := runCLI(t, paths, "", "list").want(t, cli.ExitOK)
	if !strings.Contains(o.stdout, "Invoice 7") || !strings.Contains(o.stdout, "Hi") {
		t.Fatalf("list: %s", o.stdout)
	}
	for _, bad := range []string{"\x1b", "\x07", "\u009b", rlo} {
		if strings.Contains(o.stdout, bad) {
			t.Fatalf("list printed %q raw:\n%s", bad, o.stdout)
		}
	}
	if !strings.Contains(o.stdout, `\x1b]0;pwned\x07Hi`) || !strings.Contains(o.stdout, `\u202`+"e") {
		t.Fatalf("the escapes are not made visible:\n%s", o.stdout)
	}
	o = runCLI(t, paths, "", "show", strconv.FormatInt(second, 10)).want(t, cli.ExitOK)
	if strings.ContainsAny(o.stdout, "\x1b\r") || !strings.Contains(o.stdout, `line\x1b[1A\x0dcovered`) {
		t.Fatalf("show: %q", o.stdout)
	}

	o = runCLI(t, paths, "", "list", "--json").want(t, cli.ExitOK)
	var msgs []cliproto.Message
	if err := json.Unmarshal([]byte(o.stdout), &msgs); err != nil || len(msgs) != 2 {
		t.Fatalf("json list: %v %s", err, o.stdout)
	}
	if msgs[1].Subject != hostileSubject || strings.Contains(o.stdout, "\x1b") {
		t.Fatalf("json does not carry the subject as it is, encoded: %q", msgs[1].Subject)
	}

	runCLI(t, paths, "", "accounts").want(t, cli.ExitOK)
	if o := runCLI(t, paths, "", "folders").want(t, cli.ExitOK); !strings.Contains(o.stdout, "INBOX") {
		t.Fatalf("folders: %s", o.stdout)
	}
	runCLI(t, paths, "", "list", "Nowhere").want(t, cli.ExitNotFound)
	runCLI(t, paths, "", "show", "999999").want(t, cli.ExitNotFound)

	ids := runCLI(t, paths, "", "list", "--ids").want(t, cli.ExitOK).stdout
	if strings.TrimSpace(ids) != strconv.FormatInt(first, 10)+"\n"+strconv.FormatInt(second, 10) {
		t.Fatalf("ids: %q", ids)
	}
	o = runCLI(t, paths, ids, "flag", "-").want(t, cli.ExitOK)
	for _, id := range []int64{first, second} {
		m, _ := a.store.GetMessage(a.ctx, id)
		if m.Flags&storage.FlagFlagged == 0 {
			t.Fatalf("message %d not flagged from stdin ids: %s", id, o.stdout)
		}
	}
	runCLI(t, paths, "12x\n", "flag", "-").want(t, cli.ExitUsage)
	runCLI(t, paths, "", "read", strconv.FormatInt(first, 10), "999999").want(t, cli.ExitNotFound)
	if m, _ := a.store.GetMessage(a.ctx, first); m.Flags&storage.FlagSeen == 0 {
		t.Fatal("one missing id stopped the others")
	}

	log, err := a.CLILog()
	if err != nil || len(log) == 0 {
		t.Fatalf("log %v %v", log, err)
	}
	for _, e := range log {
		if strings.Contains(e.Touched, "Invoice") || strings.Contains(e.Touched, "Hi") || strings.Contains(e.Touched, "example.test") {
			t.Fatalf("the log holds mail content: %+v", e)
		}
	}
	if err := a.ClearCLILog(); err != nil {
		t.Fatal(err)
	}
	if log, _ := a.CLILog(); len(log) != 0 {
		t.Fatalf("log after clear: %+v", log)
	}
}

func TestCLIPermanentDeleteAsks(t *testing.T) {
	a, first, second, paths := cliTestApp(t)
	allowCLI(t, a, cliproto.CapChange)
	ask := &answerConfirm{allow: false}
	a.cliConfirmer = ask

	runCLI(t, paths, "", "trash", strconv.FormatInt(first, 10)).want(t, cli.ExitRefused)
	if len(ask.asked) != 1 || ask.asked[0].Function != "cli.delete" || !ask.asked[0].EveryTime {
		t.Fatalf("asked %+v", ask.asked)
	}
	if m, err := a.store.GetMessage(a.ctx, first); err != nil || m == nil {
		t.Fatalf("a refused delete removed the message: %v", err)
	}
	ask.allow = true
	runCLI(t, paths, "", "trash", strconv.FormatInt(first, 10)).want(t, cli.ExitOK)

	ask.asked = nil
	if err := a.SetCLIPermission(cliproto.CapDeleteUnasked, true); err != nil {
		t.Fatal(err)
	}
	runCLI(t, paths, "", "trash", strconv.FormatInt(second, 10)).want(t, cli.ExitOK)
	if len(ask.asked) != 0 {
		t.Fatal("asked with Delete permanently without asking on")
	}
}

func TestCLISendGoesThroughTheComposerPath(t *testing.T) {
	a, _, _, paths := cliTestApp(t)
	a.queue = outbox.NewQueue(a.store)
	allowCLI(t, a, cliproto.CapSend)
	enabledBeforeSend(t, a)
	sendAsk := &answerConfirm{allow: false}
	recipientAsk := &answerConfirm{allow: false}
	a.cliConfirmer, a.recipientConfirmer = sendAsk, recipientAsk

	queued := func() []outbox.Message {
		list, err := a.queue.List(a.ctx)
		if err != nil {
			t.Fatal(err)
		}
		return list
	}
	args := []string{"send", "--to", "Bo <bo@example.test>", "--subject", "Plans"}

	runCLI(t, paths, "the body from stdin", args...).want(t, cli.ExitRefused)
	if len(sendAsk.asked) != 1 || !strings.Contains(sendAsk.asked[0].Summary, "bo@example.test") || len(recipientAsk.asked) != 0 || len(queued()) != 0 {
		t.Fatalf("a refused send went on: send %+v recipients %+v", sendAsk.asked, recipientAsk.asked)
	}

	sendAsk.allow = true
	runCLI(t, paths, "the body from stdin", args...).want(t, cli.ExitRefused)
	if len(recipientAsk.asked) != 1 || len(queued()) != 0 {
		t.Fatalf("the before-send recipient check did not hold the message: %+v", recipientAsk.asked)
	}

	recipientAsk.allow = true
	o := runCLI(t, paths, "the body from stdin", append(args, "--json")...).want(t, cli.ExitOK)
	var sent cliproto.Sent
	if err := json.Unmarshal([]byte(o.stdout), &sent); err != nil || sent.OutboxID == 0 {
		t.Fatalf("sent %s %v", o.stdout, err)
	}
	list := queued()
	if len(list) != 1 {
		t.Fatalf("outbox %+v", list)
	}
	raw := string(list[0].Raw)
	if !strings.Contains(raw, "Plans [x]") || !strings.Contains(raw, "the body from stdin") || !strings.Contains(strings.Join(list[0].Recipients, ","), "spy@example.test") {
		t.Fatalf("the before-send script did not run on the message: %v\n%s", list[0].Recipients, raw)
	}

	sendAsk.asked = nil
	if err := a.SetCLIPermission(cliproto.CapSendUnasked, true); err != nil {
		t.Fatal(err)
	}
	runCLI(t, paths, "", append(args, "--body", "again")...).want(t, cli.ExitOK)
	if len(sendAsk.asked) != 0 {
		t.Fatal("asked with Send without asking on")
	}
	runCLI(t, paths, "", "send", "--subject", "nobody", "--body", "x").want(t, cli.ExitUsage)
	if o := runCLI(t, paths, "", "send", "--draft", "--subject", "later", "--body", "x").want(t, cli.ExitOK); !strings.Contains(o.stdout, "draft") {
		t.Fatalf("draft: %s", o.stdout)
	}
}

func TestCLIVaultMailNeedsItsSwitch(t *testing.T) {
	f := newVaultApp(t)
	a := f.app
	paths := startCLIRig(t, a)
	allowCLI(t, a, cliproto.CapRead, cliproto.CapChange, cliproto.CapExport, cliproto.CapSearch)
	sealed := strconv.FormatInt(f.sealedID, 10)

	absent := func(when string) {
		t.Helper()
		runCLI(t, paths, "", "show", sealed).want(t, cli.ExitNotFound)
		runCLI(t, paths, "", "list", "Secret").want(t, cli.ExitNotFound)
		runCLI(t, paths, "", "attachments", sealed).want(t, cli.ExitNotFound)
		runCLI(t, paths, "", "flag", sealed).want(t, cli.ExitNotFound)
		runCLI(t, paths, "", "export", sealed, "--format", "eml", "--to", t.TempDir()).want(t, cli.ExitNotFound)
		for _, args := range [][]string{{"folders", "--json"}, {"list", "--json"}, {"search", strings.ToLower(vaultMarkBody), "--json"}} {
			o := runCLI(t, paths, "", args...)
			if strings.Contains(o.stdout, vaultMarkSubject) || strings.Contains(o.stdout, "Secret") {
				t.Fatalf("%s: %v shows vault mail: %s", when, args, o.stdout)
			}
		}
	}

	absent("locked")
	if res := a.UnlockMailVault(f.vault, "correct horse battery"); !res.OK {
		t.Fatalf("unlock: %+v", res)
	}
	absent("unlocked without the switch")
	if err := a.UpdateMailVaultSettings(MailVaultSettingsDTO{ID: f.vault, Name: "Private", AllowScripts: true, AllowMCP: true}); err != nil {
		t.Fatal(err)
	}
	absent("unlocked with the scripts and MCP switches")

	if err := a.UpdateMailVaultSettings(MailVaultSettingsDTO{ID: f.vault, Name: "Private", AllowCLI: true}); err != nil {
		t.Fatal(err)
	}
	if o := runCLI(t, paths, "", "show", sealed).want(t, cli.ExitOK); !strings.Contains(o.stdout, vaultMarkSubject) {
		t.Fatalf("show with the switch on: %s", o.stdout)
	}
	if o := runCLI(t, paths, "", "list", "Secret", "--ids").want(t, cli.ExitOK); strings.TrimSpace(o.stdout) != sealed {
		t.Fatalf("list with the switch on: %q", o.stdout)
	}

	a.LockMailVault(f.vault)
	absent("locked again")
}

func TestCLIRunsManualAutomations(t *testing.T) {
	a, id, _, paths := cliTestApp(t)
	allowCLI(t, a, cliproto.CapScripts)
	sc, err := a.CreateScript(luaInput(`mail.flag(mail.get(event.message.id))`, scripting.PermMailRead, scripting.PermMailModify))
	if err != nil {
		t.Fatal(err)
	}
	ids := strconv.FormatInt(id, 10)
	runCLI(t, paths, "", "run", "t", "--on", ids).want(t, cli.ExitUsage)

	o := runCLI(t, paths, "", "run", "t", "--dry-run", "--on", ids, "--json").want(t, cli.ExitOK)
	var run cliproto.Run
	if err := json.Unmarshal([]byte(o.stdout), &run); err != nil || !run.Dry || len(run.Steps) == 0 {
		t.Fatalf("dry run %s %v", o.stdout, err)
	}
	if m, _ := a.store.GetMessage(a.ctx, id); m.Flags&storage.FlagFlagged != 0 {
		t.Fatal("a dry run changed mail")
	}

	if _, err := a.SetScriptEnabled(sc.ID, true, sc.Permissions); err != nil {
		t.Fatal(err)
	}
	runCLI(t, paths, "", "run", "t", "--on", ids, "--set", "nope=1").want(t, cli.ExitUsage)
	runCLI(t, paths, "", "run", strconv.FormatInt(sc.ID, 10), "--on", ids).want(t, cli.ExitOK)
	if m, _ := a.store.GetMessage(a.ctx, id); m.Flags&storage.FlagFlagged == 0 {
		t.Fatal("the run did not flag the message")
	}
	runCLI(t, paths, "", "run", "no such script").want(t, cli.ExitNotFound)

	in := luaInput(`local x = 1`, scripting.PermMailSend)
	in.Name = "on send"
	in.Trigger.Type = scripting.TriggerBeforeSend
	if _, err := a.CreateScript(in); err != nil {
		t.Fatal(err)
	}
	runCLI(t, paths, "", "run", "on send", "--dry-run").want(t, cli.ExitUsage)
}

func TestCLIStateDirMatchesStorage(t *testing.T) {
	for _, channel := range []string{"", storage.ChannelNightly} {
		db, err := storage.DefaultPathForChannel(channel)
		if err != nil {
			t.Fatal(err)
		}
		dir, err := cliproto.StateDir(channel)
		if err != nil || dir != filepath.Dir(db) {
			t.Fatalf("channel %q: %q, want %q (%v)", channel, dir, filepath.Dir(db), err)
		}
	}
}

func TestParseCLISearch(t *testing.T) {
	now := time.Date(2026, 3, 10, 15, 0, 0, 0, time.UTC)
	folder := func(name string) (int64, bool) { return 7, name == "Archive" }
	got := parseCLISearch(`invoice from:"Ada Lovelace" is:unread in:Archive in:Nowhere after:2026-01-02 before:today has:attachment`, now, folder)
	want := SearchRequestDTO{
		Query: "invoice in:Nowhere", From: "Ada Lovelace", UnreadOnly: true, FolderID: 7, HasAttachment: true,
		AfterUnix:  time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC).Unix(),
		BeforeUnix: time.Date(2026, 3, 11, 0, 0, 0, 0, time.UTC).Unix() - 1,
	}
	if got != want {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

func TestInstallCLILink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the link is for macOS and Linux")
	}
	exe := filepath.Join(t.TempDir(), "Pelton")
	if err := os.WriteFile(exe, []byte("app"), 0o755); err != nil {
		t.Fatal(err)
	}
	exe, _ = filepath.EvalSymlinks(exe)
	dir := t.TempDir()
	res, err := installCLILink(exe, dir)
	if err != nil || res.Path != filepath.Join(dir, "pelton") || !linksTo(res.Path, exe) {
		t.Fatalf("install %+v %v", res, err)
	}
	if again, err := installCLILink(exe, dir); err != nil || again.Path != res.Path {
		t.Fatalf("installing twice %+v %v", again, err)
	}

	taken := t.TempDir()
	if err := os.WriteFile(filepath.Join(taken, "pelton"), []byte("other"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := installCLILink(exe, taken); err == nil {
		t.Fatal("replaced a file that was already there")
	}
	if _, err := installCLILink(exe, "relative/dir"); err == nil {
		t.Fatal("accepted a relative folder")
	}

	if os.Getuid() == 0 {
		return
	}
	locked := t.TempDir()
	if err := os.Chmod(locked, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o755) })
	res, err = installCLILink(exe, locked)
	if err != nil || res.Path != "" || !strings.HasPrefix(res.Command, "sudo ln -s '") {
		t.Fatalf("a folder that needs admin rights: %+v %v", res, err)
	}
}
