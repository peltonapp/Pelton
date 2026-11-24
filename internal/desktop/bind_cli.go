package desktop

import (
	"fmt"
	"slices"
	"strconv"

	"github.com/peltonapp/Pelton/internal/cliproto"
)

// cliLogLimit caps the log view.
const cliLogLimit = 200

// CLIPermissionDTO is one capability switch of command line access.
type CLIPermissionDTO struct {
	Name    string `json:"name"`
	Allowed bool   `json:"allowed"`
}

// CLIConfigDTO is the settings view of command line access: whether it is
// on, whether the socket is up, where the socket and token file are, and the
// switches.
type CLIConfigDTO struct {
	Enabled     bool               `json:"enabled"`
	Running     bool               `json:"running"`
	Socket      string             `json:"socket"`
	TokenFile   string             `json:"tokenFile"`
	Permissions []CLIPermissionDTO `json:"permissions"`
}

// GetCLIConfig returns the command line access settings.
func (a *App) GetCLIConfig() (CLIConfigDTO, error) {
	if err := a.ready(); err != nil {
		return CLIConfigDTO{}, err
	}
	out := CLIConfigDTO{Enabled: a.boolSetting(settingCLIEnabled, false)}
	if paths, err := a.cliPaths(); err == nil {
		out.Socket, out.TokenFile = paths.Socket, paths.Token
	}
	a.cliMu.Lock()
	out.Running = a.cli != nil
	a.cliMu.Unlock()
	caps := a.cliCaps()
	for _, c := range cliproto.Capabilities {
		out.Permissions = append(out.Permissions, CLIPermissionDTO{Name: c, Allowed: caps[c]})
	}
	return out, nil
}

// SetCLIEnabled turns command line access on or off. Turning it on writes a
// fresh token; turning it off removes the token file, so nothing the
// command held still works.
func (a *App) SetCLIEnabled(enabled bool) error {
	if err := a.ready(); err != nil {
		return err
	}
	if err := a.store.Set(a.ctx, settingCLIEnabled, strconv.FormatBool(enabled)); err != nil {
		return err
	}
	return a.applyCLIToken()
}

// SetCLIPermission turns one capability on or off. It applies to the next
// request.
func (a *App) SetCLIPermission(name string, allowed bool) error {
	if err := a.ready(); err != nil {
		return err
	}
	if !slices.Contains(cliproto.Capabilities, name) {
		return fmt.Errorf("desktop: %q is not a command line switch", name)
	}
	return a.store.Set(a.ctx, cliPermissionKey(name), strconv.FormatBool(allowed))
}

// CLILogEntryDTO is one request of the pelton command.
type CLILogEntryDTO struct {
	ID      int64  `json:"id"`
	Command string `json:"command"`
	Touched string `json:"touched"`
	Result  string `json:"result"`
	Switch  string `json:"switch"`
	When    string `json:"when"`
}

// CLILog returns the newest requests of the pelton command.
func (a *App) CLILog() ([]CLILogEntryDTO, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	rows, err := a.store.ListCLIRequests(a.ctx, cliLogLimit)
	if err != nil {
		return nil, err
	}
	out := make([]CLILogEntryDTO, 0, len(rows))
	for _, r := range rows {
		out = append(out, CLILogEntryDTO{ID: r.ID, Command: r.Command, Touched: r.Touched, Result: r.Result, Switch: r.Switch, When: formatDate(r.CreatedAt)})
	}
	return out, nil
}

// ClearCLILog empties the log.
func (a *App) ClearCLILog() error {
	if err := a.ready(); err != nil {
		return err
	}
	if err := a.store.ClearCLIRequests(a.ctx); err != nil {
		return err
	}
	a.emit(EventCLILog, nil)
	return nil
}
