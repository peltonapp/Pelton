package cli

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// safeName turns a name that came from mail into one plain file name: the
// last part of any path, without control characters, never "." or "..", and
// not one of the names Windows reserves for devices.
func safeName(name, fallback string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) || isBidiControl(r) || strings.ContainsRune(`<>:"|?*`, r) {
			return '_'
		}
		return r
	}, name)
	name = strings.TrimSpace(strings.TrimRight(name, ". "))
	if name == "" || name == "." || name == ".." {
		return fallback
	}
	stem := strings.ToUpper(strings.SplitN(name, ".", 2)[0])
	switch stem {
	case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		name = "_" + name
	}
	if len(name) > 200 {
		ext := filepath.Ext(name)
		if len(ext) > 20 {
			ext = ""
		}
		name = name[:200-len(ext)] + ext
	}
	return name
}

// targetIn joins a plain file name to dir, refusing anything that would land
// outside it.
func targetIn(dir, name string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return "", &exitError{code: ExitError, msg: fmt.Sprintf("%s is not a folder", dir)}
	}
	path := filepath.Join(abs, name)
	if filepath.Dir(path) != abs {
		return "", &exitError{code: ExitError, msg: fmt.Sprintf("%q is not a plain file name", name)}
	}
	return path, nil
}

// saveInto writes r to dir/name. Without force it never touches an
// existing file; with force it replaces a regular file, never a link or
// anything else, and the old file stays whole until the new one is written.
func saveInto(dir, name string, r io.Reader, force bool) (string, error) {
	path, err := targetIn(dir, name)
	if err != nil {
		return "", err
	}
	if !force {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, fs.ErrExist) {
			return "", &exitError{code: ExitError, msg: fmt.Sprintf("%s already exists; use --force to replace it", path)}
		}
		if err != nil {
			return "", err
		}
		if _, err := io.Copy(f, r); err != nil {
			f.Close()
			os.Remove(path)
			return "", err
		}
		return path, f.Close()
	}
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return "", &exitError{code: ExitError, msg: fmt.Sprintf("%s is a link or not a regular file; it is not replaced", path)}
	}
	return path, replaceFile(path, r)
}

// replaceFile writes r next to path and renames it over path.
func replaceFile(path string, r io.Reader) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".pelton-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, r); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// createUnique creates dir/name, adding -2, -3 before the extension while
// that name is taken. It never opens an existing file.
func createUnique(dir, name string) (*os.File, error) {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for n := 1; n < 10000; n++ {
		candidate := name
		if n > 1 {
			candidate = fmt.Sprintf("%s-%d%s", stem, n, ext)
		}
		path, err := targetIn(dir, candidate)
		if err != nil {
			return nil, err
		}
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		return f, err
	}
	return nil, fmt.Errorf("no free file name for %s", name)
}

// writeMbox appends one message to an mboxrd file: a From line, the source
// with line endings made LF, and every line that starts with any number of
// ">" and then "From " quoted with one more ">".
func writeMbox(w *bufio.Writer, raw []byte, date string) error {
	if _, err := fmt.Fprintf(w, "From MAILER-DAEMON %s\n", date); err != nil {
		return err
	}
	raw = bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n"))
	for _, l := range bytes.SplitAfter(raw, []byte("\n")) {
		if len(l) == 0 {
			continue
		}
		if bytes.HasPrefix(bytes.TrimLeft(l, ">"), []byte("From ")) {
			w.WriteByte('>')
		}
		w.Write(l)
	}
	if !bytes.HasSuffix(raw, []byte("\n")) {
		w.WriteByte('\n')
	}
	return w.WriteByte('\n')
}
