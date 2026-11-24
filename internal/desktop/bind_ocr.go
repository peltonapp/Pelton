package desktop

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Text recognition for scanned pdfs (the viewer's "Recognize text" button).
// Tesseract runs in the frontend with the languages bundled there; this file
// holds the language files a user adds on their own, in an "ocr" folder next
// to the database.

const (
	settingOCRLanguages = "ocr_languages"
	// a tesseract language file is a few MB; the largest official ones are
	// around 60 MB.
	ocrLanguageCap = 200 << 20
)

// ocrCodePattern is what a tesseract language code looks like: eng, chi_sim,
// Latin. It doubles as the file name, so nothing else is let through.
var ocrCodePattern = regexp.MustCompile(`^[A-Za-z0-9_]{2,40}$`)

func (a *App) ocrDir() (string, error) {
	if a.dataDir == "" {
		return "", errors.New("storage is not available")
	}
	dir := filepath.Join(a.dataDir, "ocr")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// ocrCode is the language code a file name stands for, or "" if the name is
// not a tesseract language file.
func ocrCode(name string) string {
	lower := strings.ToLower(name)
	var code string
	switch {
	case strings.HasSuffix(lower, ".traineddata.gz"):
		code = name[:len(name)-len(".traineddata.gz")]
	case strings.HasSuffix(lower, ".traineddata"):
		code = name[:len(name)-len(".traineddata")]
	default:
		return ""
	}
	if !ocrCodePattern.MatchString(code) {
		return ""
	}
	return code
}

// ocrFile finds the stored file for a code, compressed or not.
func (a *App) ocrFile(code string) (string, error) {
	if !ocrCodePattern.MatchString(code) {
		return "", fmt.Errorf("invalid language code %q", code)
	}
	dir, err := a.ocrDir()
	if err != nil {
		return "", err
	}
	for _, name := range []string{code + ".traineddata", code + ".traineddata.gz"} {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("language %q is not installed", code)
}

// ListOCRLanguages returns the codes of the language files the user added.
func (a *App) ListOCRLanguages() ([]string, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	dir, err := a.ocrDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	codes := []string{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if code := ocrCode(e.Name()); code != "" && !seen[code] {
			seen[code] = true
			codes = append(codes, code)
		}
	}
	sort.Strings(codes)
	return codes, nil
}

// AddOCRLanguage asks for a tesseract language file (.traineddata, optionally
// gzipped) and copies it into the ocr folder. Returns its code, or "" if the
// user canceled. A file with the same code replaces the old one.
func (a *App) AddOCRLanguage() (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	path, err := a.openFileDialog(openDialogOptions{
		Title:   "Add a text recognition language",
		Filters: []fileFilter{{DisplayName: "Tesseract language files", Pattern: "*.traineddata;*.gz"}},
	})
	if err != nil || path == "" {
		return "", err
	}
	code := ocrCode(filepath.Base(path))
	if code == "" {
		return "", errors.New("not a tesseract language file: the name should end in .traineddata, like deu.traineddata")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.Size() > ocrLanguageCap {
		return "", fmt.Errorf("language file larger than %d MB", ocrLanguageCap>>20)
	}
	dir, err := a.ocrDir()
	if err != nil {
		return "", err
	}
	if old, err := a.ocrFile(code); err == nil {
		_ = os.Remove(old)
	}
	if err := copyFile(path, filepath.Join(dir, filepath.Base(path))); err != nil {
		return "", err
	}
	return code, nil
}

// RemoveOCRLanguage deletes a language file the user added.
func (a *App) RemoveOCRLanguage(code string) error {
	if err := a.ready(); err != nil {
		return err
	}
	p, err := a.ocrFile(code)
	if err != nil {
		return err
	}
	return os.Remove(p)
}

// ReadOCRLanguage returns a user-added language file for tesseract to load.
func (a *App) ReadOCRLanguage(code string) ([]byte, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	p, err := a.ocrFile(code)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(p)
}

// OpenOCRFolder shows the ocr folder in the system file manager.
func (a *App) OpenOCRFolder() error {
	if err := a.ready(); err != nil {
		return err
	}
	dir, err := a.ocrDir()
	if err != nil {
		return err
	}
	return openPath(dir)
}

func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := to + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, to)
}
