package desktop

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// User-provided localization (#59). Custom languages live in the locales
// folder next to the database as plain json files: {"name": ..., "base":
// "en", "strings": {key: text}}. The frontend lists them in the language
// picker alongside the built-ins and resolves missing keys through the base
// language, then English. A file carrying only a handful of strings works as
// a per-string override on top of its base.

// localeFileCap bounds one locale file; far beyond any real catalog.
const localeFileCap = 2 << 20

// localeStringsCap bounds the number of strings in one file.
const localeStringsCap = 20000

// builtinLocales are the bundled languages a user locale may use as its
// fallback base.
var builtinLocales = map[string]bool{
	"en": true, "de": true, "fr": true, "nl": true, "es": true, "it": true, "pl": true, "ru": true, "tr": true,
	"pt":    true,
	"ar":    true,
	"zh-CN": true,
}

// localeIDPattern is the shape of a user locale id (its file name without
// .json): a lowercase slug that can never be a path.
var localeIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// UserLocaleDTO describes one custom language for the picker.
type UserLocaleDTO struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Author string `json:"author"`
	Base   string `json:"base"`
	// Count is the number of strings the file provides, shown so a partial
	// override is recognizable as one.
	Count int `json:"count"`
}

// UserLocaleApplyDTO is a custom language in apply form.
type UserLocaleApplyDTO struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Base    string            `json:"base"`
	Strings map[string]string `json:"strings"`
}

// userLocaleFile is the on-disk shape of a locale file.
type userLocaleFile struct {
	Name    string            `json:"name"`
	Author  string            `json:"author"`
	Base    string            `json:"base"`
	Strings map[string]string `json:"strings"`
}

// parseUserLocale validates one locale file's content. The base defaults to
// English; an unknown base is an error rather than a silent fallback so a
// typo fails loudly.
func parseUserLocale(data []byte) (userLocaleFile, error) {
	var f userLocaleFile
	if err := json.Unmarshal(data, &f); err != nil {
		return userLocaleFile{}, err
	}
	if strings.TrimSpace(f.Name) == "" {
		return userLocaleFile{}, errors.New("missing name")
	}
	if f.Base == "" {
		f.Base = "en"
	}
	if !builtinLocales[f.Base] {
		return userLocaleFile{}, fmt.Errorf("base %q is not a built-in language", f.Base)
	}
	if len(f.Strings) == 0 {
		return userLocaleFile{}, errors.New("no strings")
	}
	if len(f.Strings) > localeStringsCap {
		return userLocaleFile{}, fmt.Errorf("more than %d strings", localeStringsCap)
	}
	return f, nil
}

// localesDir returns the locales folder next to the database, creating it on
// first use.
func (a *App) localesDir() (string, error) {
	if a.dataDir == "" {
		return "", errors.New("storage is not available")
	}
	dir := filepath.Join(a.dataDir, "locales")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// readUserLocale loads and validates one locale file by id.
func (a *App) readUserLocale(id string) (userLocaleFile, error) {
	if !localeIDPattern.MatchString(id) {
		return userLocaleFile{}, fmt.Errorf("invalid language id %q", id)
	}
	dir, err := a.localesDir()
	if err != nil {
		return userLocaleFile{}, err
	}
	path := filepath.Join(dir, id+".json")
	info, err := os.Stat(path)
	if err != nil {
		return userLocaleFile{}, err
	}
	if info.Size() > localeFileCap {
		return userLocaleFile{}, fmt.Errorf("language file larger than %d MB", localeFileCap>>20)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return userLocaleFile{}, err
	}
	return parseUserLocale(data)
}

// ListUserLocales returns every valid custom language in the locales folder.
// Files that fail to parse are skipped (and logged) so one broken file
// cannot hide the rest.
func (a *App) ListUserLocales() ([]UserLocaleDTO, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	dir, err := a.localesDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	locales := []UserLocaleDTO{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(name), ".json") {
			continue
		}
		id := strings.TrimSuffix(name, filepath.Ext(name))
		f, err := a.readUserLocale(id)
		if err != nil {
			a.log.Warn("skip unreadable language file", "file", name, "err", err)
			continue
		}
		locales = append(locales, UserLocaleDTO{
			ID:     id,
			Name:   f.Name,
			Author: f.Author,
			Base:   f.Base,
			Count:  len(f.Strings),
		})
	}
	return locales, nil
}

// GetUserLocale loads a custom language in apply form. The frontend calls it
// at startup for a persisted selection and when the user picks one.
func (a *App) GetUserLocale(id string) (UserLocaleApplyDTO, error) {
	if err := a.ready(); err != nil {
		return UserLocaleApplyDTO{}, err
	}
	f, err := a.readUserLocale(id)
	if err != nil {
		return UserLocaleApplyDTO{}, err
	}
	return UserLocaleApplyDTO{ID: id, Name: f.Name, Base: f.Base, Strings: f.Strings}, nil
}

// OpenLocalesFolder shows the locales folder in the system file manager, so
// language files can be dropped in or copied out directly.
func (a *App) OpenLocalesFolder() error {
	if err := a.ready(); err != nil {
		return err
	}
	dir, err := a.localesDir()
	if err != nil {
		return err
	}
	return openPath(dir)
}

// SaveLocaleTemplate writes a translation template (built by the frontend
// from the English catalog) via a save dialog. Returns the chosen path, or
// "" if the user canceled. The content is validated as a locale file first,
// so the template a translator starts from is guaranteed loadable.
func (a *App) SaveLocaleTemplate(content string) (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	if _, err := parseUserLocale([]byte(content)); err != nil {
		return "", fmt.Errorf("template is not a valid language file: %w", err)
	}
	dest, err := wailsruntime.SaveFileDialog(a.ctx, wailsruntime.SaveDialogOptions{
		DefaultFilename: "my-language.json",
		Title:           "Save language template",
	})
	if err != nil || dest == "" {
		return "", err
	}
	if err := os.WriteFile(dest, []byte(content), 0o600); err != nil {
		return "", err
	}
	return dest, nil
}
