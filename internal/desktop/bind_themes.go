package desktop

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/peltonapp/Pelton/internal/themepack"
)

// Themes live in the themes folder next to the database as .peltontheme
// container files: importing copies the chosen file in, the palette editor
// saves there, and the bundled default themes are seeded there on first use
// as regular files the user can delete, export or edit like any other.
// Extracted theme folders (the pre-file layout) keep working side by side.

// settingThemeID is the selected custom theme's id; empty means the built-in
// default (light/dark/system per the theme setting).
const settingThemeID = "theme_id"

// settingThemesSeeded records that the default themes were written to the
// themes folder once, so deleting one does not resurrect it on the next
// launch.
const settingThemesSeeded = "themes_seeded"

// ThemeInfoDTO describes one installed theme for the settings gallery.
type ThemeInfoDTO struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Author      string `json:"author"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Base        string `json:"base"`
	HasCSS      bool   `json:"hasCss"`
	// RemoteRefs are network references still present in the installed css
	// (only non-empty when the user chose Allow at import).
	RemoteRefs []string `json:"remoteRefs"`
	// Preview is the theme's screenshot as a data uri, or "".
	Preview string `json:"preview"`
	// CompatWarning is set when the running app version is outside the range
	// the theme declares itself made for. Informational only.
	CompatWarning string `json:"compatWarning"`
	// Swatches are a few of the theme's token colors for the gallery card,
	// for themes without a preview screenshot.
	Swatches []string `json:"swatches"`
}

// ThemeApplyDTO is everything the frontend needs to apply a theme: the base
// to fall back on, validated token overrides, the concatenated css with
// bundled assets inlined, and sanitized icon override svgs.
type ThemeApplyDTO struct {
	ID     string            `json:"id"`
	Base   string            `json:"base"`
	Tokens map[string]string `json:"tokens"`
	CSS    string            `json:"css"`
	Icons  map[string]string `json:"icons"`
}

// themesDir returns the themes folder next to the database, creating it on
// first use.
func (a *App) themesDir() (string, error) {
	if a.dataDir == "" {
		return "", errors.New("storage is not available")
	}
	dir := filepath.Join(a.dataDir, "themes")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// containerName is the canonical file name a theme id is stored under.
func containerName(id string) string {
	return id + ".peltontheme"
}

// seedDefaultThemes writes the bundled default themes into the themes folder
// as regular .peltontheme files, once ever. A failed write only costs that
// preset, so failures are logged, not fatal.
func (a *App) seedDefaultThemes(root string) {
	if a.stringSetting(settingThemesSeeded, "") == "true" {
		return
	}
	for _, p := range themepack.Presets() {
		id := p.Manifest.ID
		dest := filepath.Join(root, containerName(id))
		if _, err := os.Stat(dest); err == nil {
			continue
		}
		if info, err := os.Stat(filepath.Join(root, id)); err == nil && info.IsDir() {
			continue
		}
		if err := themepack.WriteContainer(p, dest, false); err != nil {
			a.log.Warn("seed default theme", "id", id, "err", err)
		}
	}
	if err := a.store.Set(a.ctx, settingThemesSeeded, "true"); err != nil {
		a.log.Warn("record theme seeding", "err", err)
	}
}

// installedTheme is one theme found in the themes folder: its parsed package
// and where it lives (a .peltontheme file, or an extracted folder from the
// previous layout).
type installedTheme struct {
	pkg   *themepack.Package
	path  string
	isDir bool
}

// scanThemes reads every theme in the folder. Entries that fail to parse are
// skipped (and logged) rather than failing the whole scan, so one broken
// theme cannot hide the rest.
func (a *App) scanThemes(root string) ([]installedTheme, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	themes := make([]installedTheme, 0, len(entries))
	for _, e := range entries {
		path := filepath.Join(root, e.Name())
		switch {
		case e.IsDir():
			p, err := themepack.LoadInstalled(path)
			if err != nil {
				a.log.Warn("skip unreadable theme folder", "dir", e.Name(), "err", err)
				continue
			}
			themes = append(themes, installedTheme{pkg: p, path: path, isDir: true})
		case strings.HasSuffix(strings.ToLower(e.Name()), ".peltontheme"):
			p, err := a.readContainerFile(path)
			if err != nil {
				a.log.Warn("skip unreadable theme file", "file", e.Name(), "err", err)
				continue
			}
			themes = append(themes, installedTheme{pkg: p, path: path})
		}
	}
	return themes, nil
}

// findTheme resolves a theme id to its parsed package and location. The
// canonical file and folder names are tried directly; renamed files dropped
// into the folder by hand are found by scanning.
func (a *App) findTheme(id string) (installedTheme, error) {
	none := installedTheme{}
	if !themepack.ValidID(id) {
		return none, fmt.Errorf("invalid theme id %q", id)
	}
	root, err := a.themesDir()
	if err != nil {
		return none, err
	}
	file := filepath.Join(root, containerName(id))
	if _, err := os.Stat(file); err == nil {
		p, err := a.readContainerFile(file)
		if err != nil {
			return none, err
		}
		if p.Manifest.ID == id {
			return installedTheme{pkg: p, path: file}, nil
		}
	}
	dir := filepath.Join(root, id)
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		p, err := themepack.LoadInstalled(dir)
		if err != nil {
			return none, err
		}
		return installedTheme{pkg: p, path: dir, isDir: true}, nil
	}
	themes, err := a.scanThemes(root)
	if err != nil {
		return none, err
	}
	for _, t := range themes {
		if t.pkg.Manifest.ID == id {
			return t, nil
		}
	}
	return none, fmt.Errorf("theme %q is not installed", id)
}

// themeInfo builds the gallery DTO for a parsed theme. Slice fields must not
// be nil: they cross the json bridge, and the frontend expects arrays.
func (a *App) themeInfo(p *themepack.Package) ThemeInfoDTO {
	m := p.Manifest
	refs := p.RemoteRefs()
	if refs == nil {
		refs = []string{}
	}
	return ThemeInfoDTO{
		ID:            m.ID,
		Name:          m.Name,
		Author:        m.Author,
		Version:       m.Version,
		Description:   m.Description,
		Base:          m.Base,
		HasCSS:        len(m.CSS) > 0,
		RemoteRefs:    refs,
		Preview:       p.PreviewDataURI(),
		CompatWarning: themepack.CompatWarning(m.Pelton, a.version),
		Swatches:      swatchTokens(p.Tokens),
	}
}

// swatchTokens picks a handful of directly renderable token colors for the
// gallery card, in a fixed order. Derived values (rgba, color-mix) are
// skipped; the card only needs a recognizable strip, not the full palette.
func swatchTokens(tokens map[string]string) []string {
	swatches := []string{}
	for _, name := range []string{
		"surface-base", "surface-raised", "text-primary",
		"success", "warning", "danger",
	} {
		if v, ok := tokens[name]; ok && strings.HasPrefix(v, "#") {
			swatches = append(swatches, v)
		}
	}
	return swatches
}

// ListThemes returns every theme in the themes folder, seeding the bundled
// defaults on first use.
func (a *App) ListThemes() ([]ThemeInfoDTO, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	root, err := a.themesDir()
	if err != nil {
		return nil, err
	}
	a.seedDefaultThemes(root)
	themes, err := a.scanThemes(root)
	if err != nil {
		return nil, err
	}
	infos := make([]ThemeInfoDTO, 0, len(themes))
	for _, t := range themes {
		infos = append(infos, a.themeInfo(t.pkg))
	}
	return infos, nil
}

// GetThemeApply loads an installed theme in apply form. The frontend calls it
// at startup for the persisted selection and again when the user activates a
// theme.
func (a *App) GetThemeApply(id string) (ThemeApplyDTO, error) {
	if err := a.ready(); err != nil {
		return ThemeApplyDTO{}, err
	}
	t, err := a.findTheme(id)
	if err != nil {
		return ThemeApplyDTO{}, err
	}
	p := t.pkg
	return ThemeApplyDTO{
		ID:     p.Manifest.ID,
		Base:   p.Manifest.Base,
		Tokens: p.Tokens,
		CSS:    p.AppliedCSS(),
		Icons:  p.Icons,
	}, nil
}

// DeleteTheme removes a theme's file (or legacy folder). If it was the
// selected theme, the selection resets to the built-in default so the next
// launch does not chase a missing theme.
func (a *App) DeleteTheme(id string) error {
	if err := a.ready(); err != nil {
		return err
	}
	t, err := a.findTheme(id)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(t.path); err != nil {
		return err
	}
	if a.stringSetting(settingThemeID, "") == id {
		return a.store.Set(a.ctx, settingThemeID, "")
	}
	return nil
}

// ExportTheme copies an installed theme into a shareable .peltontheme file
// via a save dialog. Returns the chosen path, or "" if the user canceled.
func (a *App) ExportTheme(id string) (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	t, err := a.findTheme(id)
	if err != nil {
		return "", err
	}
	dest, err := a.saveFileDialog(saveDialogOptions{
		DefaultFilename: themepack.ContainerFileName(t.pkg.Manifest.Name),
		Title:           "Export theme",
	})
	if err != nil || dest == "" {
		return "", err
	}
	if t.isDir {
		if err := themepack.Export(t.path, dest); err != nil {
			return "", err
		}
		return dest, nil
	}
	data, err := os.ReadFile(t.path)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(dest, data, 0o600); err != nil {
		return "", err
	}
	return dest, nil
}

// OpenThemesFolder shows the themes folder in the system file manager, so
// .peltontheme files can be dropped in or copied out directly.
func (a *App) OpenThemesFolder() error {
	if err := a.ready(); err != nil {
		return err
	}
	dir, err := a.themesDir()
	if err != nil {
		return err
	}
	return openPath(dir)
}
