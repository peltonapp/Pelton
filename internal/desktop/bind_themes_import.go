package desktop

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/peltonapp/Pelton/internal/themepack"
)

// ThemeImportPreviewDTO is the read-before-import view of a chosen theme
// file: its metadata, every stylesheet's raw content and remote references,
// and whether installing would update an already-installed theme. Nothing is
// written anywhere at preview time.
type ThemeImportPreviewDTO struct {
	// Canceled is true when the user dismissed the file dialog.
	Canceled bool         `json:"canceled"`
	Path     string       `json:"path"`
	Info     ThemeInfoDTO `json:"info"`
	// CSSFiles carry the raw stylesheet contents for the read-only viewer.
	CSSFiles []themepack.CSSFile `json:"cssFiles"`
	// TokenCount is how many color/token values the theme defines, so the
	// import modal can offer the parts choice only for parts that exist.
	TokenCount int `json:"tokenCount"`
	// UpdatesExisting is true when a theme with the same id is installed;
	// InstalledVersion is that install's version string.
	UpdatesExisting  bool   `json:"updatesExisting"`
	InstalledVersion string `json:"installedVersion"`
}

// PreviewThemeImport opens a file dialog for a .peltontheme container and
// returns everything the import modal shows. The file is parsed and fully
// validated but not installed; ConfirmThemeImport does that after the user
// has seen the css and made the remote-reference choice.
func (a *App) PreviewThemeImport() (ThemeImportPreviewDTO, error) {
	if err := a.ready(); err != nil {
		return ThemeImportPreviewDTO{}, err
	}
	path, err := a.openFileDialog(openDialogOptions{
		Title: "Import theme",
		Filters: []fileFilter{
			{DisplayName: "Pelton themes (*.peltontheme)", Pattern: "*.peltontheme"},
		},
	})
	if err != nil {
		return ThemeImportPreviewDTO{}, err
	}
	if path == "" {
		return ThemeImportPreviewDTO{Canceled: true}, nil
	}
	p, err := a.readContainerFile(path)
	if err != nil {
		return ThemeImportPreviewDTO{}, err
	}
	cssFiles := p.CSSFiles
	if cssFiles == nil {
		cssFiles = []themepack.CSSFile{}
	}
	preview := ThemeImportPreviewDTO{
		Path:       path,
		Info:       a.themeInfo(p),
		CSSFiles:   cssFiles,
		TokenCount: len(p.Tokens),
	}
	if installed, err := a.findTheme(p.Manifest.ID); err == nil {
		preview.UpdatesExisting = true
		preview.InstalledVersion = installed.pkg.Manifest.Version
	}
	return preview, nil
}

// ConfirmThemeImport installs a previewed container by copying it into the
// themes folder under its id (the chosen file itself stays where it is).
// allowRemote is the user's choice from the import warning: false strips
// every remote reference from the css before it touches disk, so the copy in
// the folder is exactly what will run. importTokens and importCSS are the
// user's parts choice; a deselected part is dropped from the container before
// it is written. The file is re-read and re-validated here so the preview and
// the install can never diverge. An existing theme with the same id is
// replaced, which is how updates work.
func (a *App) ConfirmThemeImport(path string, allowRemote, importTokens, importCSS bool) (ThemeInfoDTO, error) {
	if err := a.ready(); err != nil {
		return ThemeInfoDTO{}, err
	}
	p, err := a.readContainerFile(path)
	if err != nil {
		return ThemeInfoDTO{}, err
	}
	if err := p.SelectParts(importTokens, importCSS); err != nil {
		return ThemeInfoDTO{}, err
	}
	root, err := a.themesDir()
	if err != nil {
		return ThemeInfoDTO{}, err
	}
	if existing, err := a.findTheme(p.Manifest.ID); err == nil {
		if err := os.RemoveAll(existing.path); err != nil {
			return ThemeInfoDTO{}, err
		}
	}
	dest := filepath.Join(root, containerName(p.Manifest.ID))
	if err := themepack.WriteContainer(p, dest, !allowRemote); err != nil {
		return ThemeInfoDTO{}, err
	}
	installed, err := a.readContainerFile(dest)
	if err != nil {
		return ThemeInfoDTO{}, err
	}
	return a.themeInfo(installed), nil
}

// readContainerFile reads and parses a .peltontheme file with the container
// size cap enforced before the bytes are even read.
func (a *App) readContainerFile(path string) (*themepack.Package, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > 20<<20 {
		return nil, fmt.Errorf("theme file is larger than 20 MB")
	}
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	return themepack.ReadContainer(data)
}
