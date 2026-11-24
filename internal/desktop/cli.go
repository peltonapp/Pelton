package desktop

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/cliproto"
	"github.com/peltonapp/Pelton/internal/cliserver"
	"github.com/peltonapp/Pelton/internal/scripting"
	"github.com/peltonapp/Pelton/internal/storage"
)

// Command line access: the pelton command talks to the running app over a
// socket in the state folder (internal/cliserver). It is off until the user
// turns it on, and then every capability is a switch of its own, all off.
// The token rotates at every start and every time access is turned on, and
// lives only in the token file and in memory, never in the database.

// Settings keys.
const (
	settingCLIEnabled = "cli_enabled"
	settingCLIPrefix  = "cli_allow_"
)

// EventCLILog fires when the command line log gains a line or is cleared.
const EventCLILog = "cli:log"

// cliConfirmName is who the app window says is asking.
const cliConfirmName = "The pelton command"

// cliPermissionKey is the settings key of one capability.
func cliPermissionKey(capability string) string {
	return settingCLIPrefix + capability
}

// cliPaths is where this install's socket and token live: in the state
// folder, which a data folder moved elsewhere never changes.
func (a *App) cliPaths() (cliproto.Paths, error) {
	if a.cliPathsOverride.Dir != "" {
		return a.cliPathsOverride, nil
	}
	db, err := storage.DefaultPathForChannel(a.channel)
	if err != nil {
		return cliproto.Paths{}, err
	}
	return cliproto.PathsIn(filepath.Dir(db)), nil
}

// startCLI opens the socket once the store is up. A failure is logged: the
// command then reports the app as not running, which is the truth for it.
func (a *App) startCLI() {
	paths, err := a.cliPaths()
	if err != nil {
		a.log.Error("command line access", "err", err)
		return
	}
	srv := cliserver.New(paths, cliBackend{a}, a.log)
	if err := srv.Start(); err != nil {
		a.log.Error("command line access", "err", err)
		return
	}
	a.cliMu.Lock()
	a.cli = srv
	a.cliMu.Unlock()
	if err := a.applyCLIToken(); err != nil {
		a.log.Error("command line token", "err", err)
	}
}

// stopCLI closes the socket and removes the token file on quit.
func (a *App) stopCLI() {
	a.cliMu.Lock()
	srv := a.cli
	a.cli = nil
	a.cliMu.Unlock()
	if srv == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Stop(ctx)
}

// applyCLIToken writes a fresh token while access is on and removes it while
// it is off.
func (a *App) applyCLIToken() error {
	a.cliMu.Lock()
	srv := a.cli
	a.cliMu.Unlock()
	if srv == nil {
		return nil
	}
	if a.store == nil || !a.boolSetting(settingCLIEnabled, false) {
		return srv.SetToken("")
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Errorf("desktop: make the command line token: %w", err)
	}
	return srv.SetToken(hex.EncodeToString(buf))
}

// cliCaps reads every capability switch. Absent means off.
func (a *App) cliCaps() map[string]bool {
	caps := make(map[string]bool, len(cliproto.Capabilities))
	for _, c := range cliproto.Capabilities {
		caps[c] = a.boolSetting(cliPermissionKey(c), false)
	}
	return caps
}

// cliBackend is the app as the socket server sees it.
type cliBackend struct{ a *App }

func (b cliBackend) State() cliserver.State {
	a := b.a
	st := cliserver.State{Version: a.version, Locked: a.locked()}
	if a.store == nil {
		return st
	}
	st.Access = a.boolSetting(settingCLIEnabled, false)
	st.Caps = a.cliCaps()
	return st
}

func (b cliBackend) Record(e cliserver.Entry) {
	a := b.a
	if a.store == nil {
		return
	}
	if err := a.store.RecordCLIRequest(a.ctx, storage.CLIRequest{
		Command: e.Command, Touched: e.Touched, Result: e.Result, Switch: e.Switch,
	}); err != nil {
		a.log.Error("record command line request", "err", err)
		return
	}
	a.emit(EventCLILog, nil)
}

func (b cliBackend) Handle(ctx context.Context, command string, body []byte) (cliserver.Response, error) {
	a := b.a
	if err := a.ready(); err != nil {
		return cliserver.Response{}, err
	}
	ctx = cliCtx(ctx)
	switch command {
	case cliproto.CmdAccounts:
		return a.cliAccounts(ctx)
	case cliproto.CmdFolders:
		return withBody(body, func(r cliproto.FolderRequest) (cliserver.Response, error) { return a.cliFolderList(ctx, r) })
	case cliproto.CmdList:
		return withBody(body, func(r cliproto.ListRequest) (cliserver.Response, error) { return a.cliList(ctx, r) })
	case cliproto.CmdSearch:
		return withBody(body, func(r cliproto.SearchRequest) (cliserver.Response, error) { return a.cliSearch(ctx, r) })
	case cliproto.CmdShow:
		return withBody(body, func(r cliproto.ShowRequest) (cliserver.Response, error) { return a.cliShow(ctx, r) })
	case cliproto.CmdAttachments:
		return withBody(body, func(r cliproto.IDRequest) (cliserver.Response, error) { return a.cliAttachments(ctx, r) })
	case cliproto.CmdAttachment:
		return withBody(body, func(r cliproto.AttachmentRequest) (cliserver.Response, error) { return a.cliAttachment(ctx, r) })
	case cliproto.CmdAct:
		return withBody(body, func(r cliproto.ActRequest) (cliserver.Response, error) { return a.cliAct(ctx, r) })
	case cliproto.CmdSend:
		return withBody(body, func(r cliproto.SendRequest) (cliserver.Response, error) { return a.cliSend(ctx, r, false) })
	case cliproto.CmdDraft:
		return withBody(body, func(r cliproto.SendRequest) (cliserver.Response, error) { return a.cliSend(ctx, r, true) })
	case cliproto.CmdCompose:
		return withBody(body, func(r cliproto.ComposeRequest) (cliserver.Response, error) { return a.cliCompose(r) })
	case cliproto.CmdRun:
		return withBody(body, func(r cliproto.RunRequest) (cliserver.Response, error) { return a.cliRun(ctx, r) })
	case cliproto.CmdExport:
		return withBody(body, func(r cliproto.ExportRequest) (cliserver.Response, error) { return a.cliExport(ctx, r) })
	case cliproto.CmdCalendar:
		return withBody(body, func(r cliproto.RangeRequest) (cliserver.Response, error) { return a.cliCalendar(r) })
	case cliproto.CmdTasks:
		return withBody(body, func(r cliproto.TasksRequest) (cliserver.Response, error) { return a.cliTasks(r) })
	case cliproto.CmdTaskAdd:
		return withBody(body, func(r cliproto.TaskAddRequest) (cliserver.Response, error) { return a.cliTaskAdd(r) })
	case cliproto.CmdContacts:
		return withBody(body, func(r cliproto.QueryRequest) (cliserver.Response, error) { return a.cliContacts(r) })
	case cliproto.CmdOpen:
		return withBody(body, func(r cliproto.OpenRequest) (cliserver.Response, error) { return a.cliOpen(ctx, r) })
	}
	return cliserver.Response{}, cliproto.Errorf(cliproto.CodeBadRequest, "unknown command %q", command)
}

// withBody decodes a request body and runs fn with it.
func withBody[T any](body []byte, fn func(T) (cliserver.Response, error)) (cliserver.Response, error) {
	var req T
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			return cliserver.Response{}, cliproto.Errorf(cliproto.CodeBadRequest, "the request could not be read: %v", err)
		}
	}
	return fn(req)
}

// cliNotFound turns the store's not found errors into the command's.
func cliNotFound(err error, what string) error {
	if errors.Is(err, storage.ErrMessageNotFound) || errors.Is(err, storage.ErrFolderNotFound) ||
		errors.Is(err, storage.ErrAccountNotFound) {
		return cliproto.Errorf(cliproto.CodeNotFound, "%s was not found", what)
	}
	return err
}

// cliConfirm asks in the app window before a send or a permanent delete,
// unless the capability that skips the question is on. No answer, a no, or
// the command giving up counts as refused.
func (a *App) cliConfirm(ctx context.Context, unasked, function, summary string) error {
	if a.boolSetting(cliPermissionKey(unasked), false) {
		return nil
	}
	confirmer := a.cliConfirmer
	if confirmer == nil {
		a.showWindow()
		confirmer = &scriptConfirm{app: a}
	}
	ans, err := confirmer.Confirm(ctx, scripting.ConfirmRequest{
		ScriptName: cliConfirmName, Function: function, Summary: summary, EveryTime: true,
	})
	if err != nil || !ans.Allow {
		return cliproto.Errorf(cliproto.CodeRefused, "this was not confirmed in Pelton")
	}
	return nil
}

// touchedIDs words a list of message ids for the log, naming at most a few.
func touchedIDs(noun string, ids []int64) string {
	const shown = 10
	parts := make([]string, 0, min(len(ids), shown))
	for i, id := range ids {
		if i == shown {
			break
		}
		parts = append(parts, strconv.FormatInt(id, 10))
	}
	out := noun + " " + strings.Join(parts, ", ")
	if len(ids) > shown {
		out += fmt.Sprintf(" and %d more", len(ids)-shown)
	}
	return out
}
