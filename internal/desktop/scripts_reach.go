package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/peltonapp/Pelton/internal/scripting"
	"github.com/peltonapp/Pelton/internal/scripting/files"
	"github.com/peltonapp/Pelton/internal/scripting/spaces"
	"github.com/peltonapp/Pelton/internal/scripting/system"
	"github.com/peltonapp/Pelton/internal/scripting/templates"
	"github.com/peltonapp/Pelton/internal/scripting/web"
)

// settingScriptFolders holds the folder each script was granted, as a JSON
// object of script id to path. It belongs to this device: a backup leaves it
// out and an exported script never carries it.
const settingScriptFolders = "script_folders"

// scriptFoldersMu serializes the read, change and write of the grants.
var scriptFoldersMu sync.Mutex

// registerScriptReach adds the other spaces and the outside world to the
// scripts' registry: the spaces, web requests, files, the clipboard and
// links.
func (a *App) registerScriptReach(r *scripting.Registry) error {
	cal := &scriptCalendar{app: a}
	if err := spaces.Register(r, spaces.Hosts{
		Calendar: cal, Tasks: cal, Contacts: &scriptContacts{app: a}, Projects: &scriptProjects{app: a},
		Notes: &scriptNotes{app: a}, Feeds: &scriptFeeds{app: a}, Chat: &scriptChat{app: a},
	}); err != nil {
		return err
	}
	if err := web.Register(r, web.Config{Dial: a.scriptDial}); err != nil {
		return err
	}
	if err := files.Register(r, scriptFolders{app: a}); err != nil {
		return err
	}
	if err := system.Register(r, scriptDesktop{app: a}); err != nil {
		return err
	}
	if err := a.registerScriptAI(r); err != nil {
		return err
	}
	if err := a.registerScriptPromises(r); err != nil {
		return err
	}
	if err := templates.Register(r); err != nil {
		return err
	}
	spaces.SetChatAvailable(r, a.chatEnabled())
	return nil
}

// scriptDial connects a script's web request to an address the web module
// checked, through the proxy the app uses for its own web calls.
func (a *App) scriptDial(ctx context.Context, network, addr string) (net.Conn, error) {
	if dial := a.currentProxy().DialContext(); dial != nil {
		return dial(ctx, network, addr)
	}
	return (&net.Dialer{Timeout: 20 * time.Second}).DialContext(ctx, network, addr)
}

// scriptChatAvailability follows chat being switched on or off.
func (a *App) scriptChatAvailability() {
	if a.scripts != nil {
		spaces.SetChatAvailable(a.scripts.Registry(), a.chatEnabled())
	}
}

// scriptDesktop is the clipboard and the browser a script reaches.
type scriptDesktop struct {
	app *App
}

func (d scriptDesktop) SetClipboard(_ context.Context, text string) error {
	if d.app.wapp == nil || !d.app.wapp.Clipboard.SetText(text) {
		return fmt.Errorf("pelton: the clipboard is not available")
	}
	return nil
}

func (d scriptDesktop) Clipboard(context.Context) (string, error) {
	if d.app.wapp == nil {
		return "", fmt.Errorf("pelton: the clipboard is not available")
	}
	text, _ := d.app.wapp.Clipboard.Text()
	return text, nil
}

func (d scriptDesktop) OpenLink(_ context.Context, link string) error {
	if err := system.CheckLink(link); err != nil {
		return err
	}
	if d.app.wapp == nil {
		return fmt.Errorf("pelton: no browser to open the link in")
	}
	return d.app.wapp.Browser.OpenURL(link)
}

// scriptFolders reads the folder grants for the files module.
type scriptFolders struct {
	app *App
}

func (f scriptFolders) Folder(_ context.Context, scriptID int64) (string, error) {
	return f.app.scriptFolders()[strconv.FormatInt(scriptID, 10)], nil
}

func (a *App) scriptFolders() map[string]string {
	out := map[string]string{}
	if raw := a.stringSetting(settingScriptFolders, ""); raw != "" {
		if err := json.Unmarshal([]byte(raw), &out); err != nil {
			a.log.Warn("scripts: read folder grants", "err", err)
		}
	}
	return out
}

func (a *App) setScriptFolder(id int64, dir string) error {
	scriptFoldersMu.Lock()
	defer scriptFoldersMu.Unlock()
	grants := a.scriptFolders()
	key := strconv.FormatInt(id, 10)
	if dir == "" {
		delete(grants, key)
	} else {
		grants[key] = dir
	}
	b, err := json.Marshal(grants)
	if err != nil {
		return err
	}
	return a.store.Set(a.ctx, settingScriptFolders, string(b))
}

// ScriptFolder returns the folder a script may read and write files in, ""
// when it has none.
func (a *App) ScriptFolder(id int64) (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	return a.scriptFolders()[strconv.FormatInt(id, 10)], nil
}

// PickScriptFolder asks for the folder a script may read and write files
// in and grants it. It returns the folder, "" when the user cancelled, which
// keeps the grant as it was.
func (a *App) PickScriptFolder(id int64) (string, error) {
	svc, err := a.scriptService()
	if err != nil {
		return "", err
	}
	if _, err := svc.Script(a.ctx, id); err != nil {
		return "", err
	}
	dir, err := a.openDirectoryDialog(openDialogOptions{Title: "Folder for this script"})
	if err != nil || dir == "" {
		return "", err
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return "", fmt.Errorf("pelton: %s is not a folder", dir)
	}
	return dir, a.setScriptFolder(id, dir)
}

// ClearScriptFolder takes back a script's folder.
func (a *App) ClearScriptFolder(id int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.setScriptFolder(id, "")
}
