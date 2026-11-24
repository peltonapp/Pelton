package desktop

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// The app's own binary answers the pelton commands on macOS and Linux (see
// main.go), so installing the command line tool is a symbolic link named
// pelton to it, in a folder the user picks. Pelton never raises its
// privileges for that: a folder it cannot write to gets the command to run
// instead. On Windows the app is a windowed program that cannot write to a
// terminal, so there the command is a separate pelton.exe the installer
// puts on the PATH.

// cliLinkName is the name of the link.
const cliLinkName = "pelton"

// CLIInstallDirDTO is a folder the link can go in, and whether it is there.
type CLIInstallDirDTO struct {
	Dir       string `json:"dir"`
	Installed bool   `json:"installed"`
}

// CLIInstallDTO says whether the app can install the link here, what it
// would point to, the usual folders for it, and the pelton already on the
// PATH when that is this app.
type CLIInstallDTO struct {
	Supported  bool               `json:"supported"`
	Executable string             `json:"executable"`
	OnPath     string             `json:"onPath"`
	Dirs       []CLIInstallDirDTO `json:"dirs"`
}

// CLIInstallResultDTO is the outcome of InstallCLI: the link made, or the
// command to run when the folder needs administrator rights.
type CLIInstallResultDTO struct {
	Path    string `json:"path"`
	Command string `json:"command"`
}

func cliExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

// cliInstallDirs are the usual folders for a command, those that exist.
func cliInstallDirs() []string {
	var dirs []string
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".local", "bin"), filepath.Join(home, "bin"))
	}
	dirs = append(dirs, "/usr/local/bin")
	if runtime.GOOS == "darwin" {
		dirs = append(dirs, "/opt/homebrew/bin")
	}
	out := dirs[:0]
	for _, d := range dirs {
		if info, err := os.Stat(d); err == nil && info.IsDir() {
			out = append(out, d)
		}
	}
	return out
}

// linksTo reports whether path is a symbolic link that ends at exe.
func linksTo(path, exe string) bool {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return false
	}
	target, err := filepath.EvalSymlinks(path)
	return err == nil && target == exe
}

// CLIInstallInfo describes installing the command line tool on this system.
func (a *App) CLIInstallInfo() CLIInstallDTO {
	out := CLIInstallDTO{Supported: runtime.GOOS != "windows", Dirs: []CLIInstallDirDTO{}}
	exe, err := cliExecutable()
	if err != nil {
		out.Supported = false
		return out
	}
	out.Executable = exe
	if found, err := exec.LookPath(cliLinkName); err == nil {
		if target, err := filepath.EvalSymlinks(found); err == nil && target == exe {
			out.OnPath = found
		}
	}
	if !out.Supported {
		return out
	}
	for _, d := range cliInstallDirs() {
		out.Dirs = append(out.Dirs, CLIInstallDirDTO{Dir: d, Installed: linksTo(filepath.Join(d, cliLinkName), exe)})
	}
	return out
}

// ChooseCLIInstallDir asks for a folder to put the link in. Empty means
// cancelled.
func (a *App) ChooseCLIInstallDir() (string, error) {
	return a.openDirectoryDialog(openDialogOptions{Title: "Choose where to install the pelton command"})
}

// InstallCLI puts a link named pelton to the app into dir. It never replaces
// a file that is already there unless that is a link to this app, and when
// dir needs administrator rights it returns the command to run instead.
func (a *App) InstallCLI(dir string) (CLIInstallResultDTO, error) {
	if runtime.GOOS == "windows" {
		return CLIInstallResultDTO{}, errors.New("pelton: on Windows the installer puts pelton.exe on the PATH")
	}
	exe, err := cliExecutable()
	if err != nil {
		return CLIInstallResultDTO{}, err
	}
	return installCLILink(exe, dir)
}

func installCLILink(exe, dir string) (CLIInstallResultDTO, error) {
	if !filepath.IsAbs(dir) {
		return CLIInstallResultDTO{}, errors.New("pelton: choose a folder by its full path")
	}
	dir = filepath.Clean(dir)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return CLIInstallResultDTO{}, fmt.Errorf("pelton: %s is not a folder", dir)
	}
	link := filepath.Join(dir, cliLinkName)
	if linksTo(link, exe) {
		return CLIInstallResultDTO{Path: link}, nil
	}
	if _, err := os.Lstat(link); err == nil {
		return CLIInstallResultDTO{}, fmt.Errorf("pelton: %s already exists; remove it first if it is an older copy", link)
	}
	if err := os.Symlink(exe, link); err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return CLIInstallResultDTO{Command: "sudo ln -s " + shellQuote(exe) + " " + shellQuote(link)}, nil
		}
		return CLIInstallResultDTO{}, err
	}
	return CLIInstallResultDTO{Path: link}, nil
}

// shellQuote quotes s for a POSIX shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
