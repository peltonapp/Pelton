package desktop

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/peltonapp/Pelton/internal/themepack"
)

// The palette editor (#57) saves its themes as regular .peltontheme files in
// the themes folder, so an edited palette shows up in the gallery, exports
// and travels with backups exactly like an imported theme.

// SaveThemeRequest is a palette-editor save: metadata, light/dark base, the
// token overrides and the editor's own stylesheet. ID is set when editing an
// existing installed theme and empty when creating a new one.
type SaveThemeRequest struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Author  string            `json:"author"`
	Version string            `json:"version"`
	Base    string            `json:"base"`
	Tokens  map[string]string `json:"tokens"`
	// CSS is the stylesheet typed in the editor's advanced section. It is
	// stored as one file the editor owns; stylesheets the theme brought with
	// it from an import are left alone.
	CSS string `json:"css"`
}

// editorCSSPath is the one stylesheet the editor writes. Keeping it to a
// fixed path means editing a theme that already ships css adds to it instead
// of overwriting whatever the theme author wrote.
const editorCSSPath = "css/custom.css"

// maxEditorCSS caps what the editor itself will write, well under the
// container's own total css limit so a save fails here with a clear message
// rather than at the read-back.
const maxEditorCSS = 256 << 10

// ThemeDraftDTO is everything the editor needs to reopen a theme: the same
// fields it saves. CSS is the raw source of the editor's own stylesheet, not
// the applied one (GetThemeApply returns that, with assets inlined).
type ThemeDraftDTO struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Author  string            `json:"author"`
	Version string            `json:"version"`
	Base    string            `json:"base"`
	Tokens  map[string]string `json:"tokens"`
	CSS     string            `json:"css"`
}

// GetThemeDraft loads an installed theme back into editor form.
func (a *App) GetThemeDraft(id string) (ThemeDraftDTO, error) {
	if err := a.ready(); err != nil {
		return ThemeDraftDTO{}, err
	}
	t, err := a.findTheme(id)
	if err != nil {
		return ThemeDraftDTO{}, err
	}
	p := t.pkg
	return ThemeDraftDTO{
		ID:      p.Manifest.ID,
		Name:    p.Manifest.Name,
		Author:  p.Manifest.Author,
		Version: p.Manifest.Version,
		Base:    p.Manifest.Base,
		Tokens:  p.Tokens,
		CSS:     string(p.Files[editorCSSPath]),
	}, nil
}

// SaveCustomTheme validates and writes a palette-editor theme, returning its
// gallery info. New themes get an id derived from the name, kept clear of
// existing themes. Editing keeps everything else the theme carries (author,
// icons, preview, bundled files) and only replaces name, base and tokens.
func (a *App) SaveCustomTheme(req SaveThemeRequest) (ThemeInfoDTO, error) {
	if err := a.ready(); err != nil {
		return ThemeInfoDTO{}, err
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return ThemeInfoDTO{}, fmt.Errorf("the theme needs a name")
	}
	if req.Base != "light" && req.Base != "dark" {
		return ThemeInfoDTO{}, fmt.Errorf("base must be light or dark")
	}
	tokens, err := themepack.ValidateTokens(req.Tokens)
	if err != nil {
		return ThemeInfoDTO{}, err
	}
	root, err := a.themesDir()
	if err != nil {
		return ThemeInfoDTO{}, err
	}

	manifest := themepack.Manifest{ManifestVersion: themepack.ManifestVersion}
	files := map[string][]byte{}
	var previousPath string
	if req.ID == "" {
		id, err := a.newThemeID(root, name)
		if err != nil {
			return ThemeInfoDTO{}, err
		}
		manifest.ID = id
	} else {
		existing, err := a.findTheme(req.ID)
		if err != nil {
			return ThemeInfoDTO{}, err
		}
		manifest = existing.pkg.Manifest
		maps.Copy(files, existing.pkg.Files)
		previousPath = existing.path
	}
	manifest.Name = name
	manifest.Author = strings.TrimSpace(req.Author)
	manifest.Version = strings.TrimSpace(req.Version)
	manifest.Base = req.Base
	if manifest.Tokens, err = json.Marshal(tokens); err != nil {
		return ThemeInfoDTO{}, err
	}
	if err := setEditorCSS(&manifest, files, req.CSS); err != nil {
		return ThemeInfoDTO{}, err
	}
	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return ThemeInfoDTO{}, err
	}
	files["manifest.json"] = manifestJSON

	dest := filepath.Join(root, containerName(manifest.ID))
	err = themepack.WriteContainer(&themepack.Package{Manifest: manifest, Files: files}, dest, false)
	if err != nil {
		return ThemeInfoDTO{}, err
	}
	// an edited legacy folder migrates to a file; drop the folder.
	if previousPath != "" && previousPath != dest {
		if err := os.RemoveAll(previousPath); err != nil {
			a.log.Warn("remove replaced theme", "path", previousPath, "err", err)
		}
	}
	p, err := a.readContainerFile(dest)
	if err != nil {
		return ThemeInfoDTO{}, err
	}
	return a.themeInfo(p), nil
}

// setEditorCSS writes (or clears) the editor's own stylesheet in the package
// being saved and keeps the manifest's css list in step. Remote references
// are stripped: css typed or pasted into the editor is held to the same no
// network rule as an import, and unlike an import there is no author to ask.
func setEditorCSS(manifest *themepack.Manifest, files map[string][]byte, raw string) error {
	css := strings.TrimSpace(raw)
	if css == "" {
		delete(files, editorCSSPath)
		manifest.CSS = slices.DeleteFunc(manifest.CSS, func(p string) bool { return p == editorCSSPath })
		return nil
	}
	if len(css) > maxEditorCSS {
		return fmt.Errorf("the theme's css is larger than %d KB", maxEditorCSS>>10)
	}
	files[editorCSSPath] = []byte(themepack.StripRemote(css))
	if !slices.Contains(manifest.CSS, editorCSSPath) {
		manifest.CSS = append(manifest.CSS, editorCSSPath)
	}
	return nil
}

// newThemeID derives a fresh theme id from the name, stepping around
// existing themes with a numeric suffix.
func (a *App) newThemeID(root, name string) (string, error) {
	slug := themepack.Slug(name)
	for i := 1; i <= 100; i++ {
		id := slug
		if i > 1 {
			id = fmt.Sprintf("%s-%d", slug, i)
		}
		if _, err := a.findTheme(id); err != nil {
			return id, nil
		}
	}
	return "", fmt.Errorf("could not find a free id for %q", name)
}
