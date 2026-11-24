// Package notes is the notes space: a folder of plain Markdown files (a
// vault) that Pelton reads, edits and indexes. The files on disk are the
// truth. Pelton may point at a vault another app also edits, such as an
// Obsidian vault, so it never rewrites a file it was not asked to change,
// writes atomically, keeps away from dot folders like .obsidian, and refuses
// any path that would lead outside the vault, symlinks included.
package notes

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Errors a vault operation reports.
var (
	// ErrOutside is a path that would leave the vault.
	ErrOutside = errors.New("notes: path is outside the vault")
	// ErrProtected is a path inside a folder Pelton never touches, such as
	// .obsidian or the trash.
	ErrProtected = errors.New("notes: path is in a protected folder")
	// ErrExists is a create or rename onto a path that is already taken.
	ErrExists = errors.New("notes: a file with that name already exists")
	// ErrNotFound is a path with nothing behind it.
	ErrNotFound = errors.New("notes: no such note")
)

// TrashDir is the folder inside the vault deleted notes move to, the same
// one Obsidian uses.
const TrashDir = ".trash"

// Vault is one notes folder on disk. Every path it takes is relative to the
// folder and uses forward slashes.
type Vault struct {
	root string
}

// OpenVault opens the folder at dir, which must exist. A symlinked folder is
// resolved once here, so everything below is checked against its real
// location.
func OpenVault(dir string) (*Vault, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("notes: vault path %q: %w", dir, err)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("notes: open vault %q: %w", dir, err)
	}
	fi, err := os.Stat(real)
	if err != nil {
		return nil, fmt.Errorf("notes: open vault %q: %w", dir, err)
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("notes: vault %q is not a folder", dir)
	}
	return &Vault{root: real}, nil
}

// Root returns the vault folder's absolute, symlink-free path.
func (v *Vault) Root() string {
	return v.root
}

// CleanRel normalises a vault-relative path: forward slashes, no leading
// slash, no "." or ".." segments. It fails for an empty path, an absolute
// path, a path that climbs out of the vault, and one inside a dot folder.
func CleanRel(rel string) (string, error) {
	rel = strings.ReplaceAll(strings.TrimSpace(rel), "\\", "/")
	if rel == "" || strings.HasPrefix(rel, "/") || filepath.IsAbs(rel) || filepath.VolumeName(rel) != "" || strings.ContainsRune(rel, 0) {
		return "", ErrOutside
	}
	clean := path.Clean(rel)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", ErrOutside
	}
	for _, seg := range strings.Split(clean, "/") {
		if strings.HasPrefix(seg, ".") {
			return "", ErrProtected
		}
	}
	return clean, nil
}

// within reports whether p is root or below it.
func within(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	if err != nil || filepath.IsAbs(rel) {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// abs maps a vault-relative path onto disk and checks that whatever part of it
// already exists stays inside the vault once symlinks are followed. The path
// returned is the real location: writing through a symlink inside the vault
// writes its target rather than replacing the link.
func (v *Vault) abs(rel string) (string, error) {
	clean, err := CleanRel(rel)
	if err != nil {
		return "", err
	}
	p := filepath.Join(v.root, filepath.FromSlash(clean))
	// find the deepest part that exists and resolve it.
	existing, rest := p, ""
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return "", ErrOutside
		}
		rest = filepath.Join(filepath.Base(existing), rest)
		existing = parent
	}
	real, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", fmt.Errorf("notes: resolve %q: %w", rel, err)
	}
	if !within(v.root, real) {
		return "", ErrOutside
	}
	if rest != "" {
		real = filepath.Join(real, rest)
	}
	return real, nil
}

// Read returns the bytes of a file in the vault.
func (v *Vault) Read(rel string) ([]byte, error) {
	p, err := v.abs(rel)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("notes: read %q: %w", rel, err)
	}
	return data, nil
}

// Stat returns the size and modification time of a file in the vault.
func (v *Vault) Stat(rel string) (fs.FileInfo, error) {
	p, err := v.abs(rel)
	if err != nil {
		return nil, err
	}
	fi, err := os.Stat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}
	return fi, err
}

// Exists reports whether a path in the vault is taken. On a case-insensitive
// file system a name differing only in case counts as taken.
func (v *Vault) Exists(rel string) bool {
	p, err := v.abs(rel)
	if err != nil {
		return false
	}
	_, err = os.Lstat(p)
	return err == nil
}

// Write replaces a file atomically: the bytes go to a temporary file in the
// same folder, which is synced and then renamed over the old one, so a crash
// or a reader in another app never sees half a note. The file keeps its
// permissions. Missing folders are created.
func (v *Vault) Write(rel string, data []byte) error {
	p, err := v.abs(rel)
	if err != nil {
		return err
	}
	return writeAtomic(p, data)
}

// Create writes a new file and fails with ErrExists when the path is taken.
func (v *Vault) Create(rel string, data []byte) error {
	p, err := v.abs(rel)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(p); err == nil {
		return ErrExists
	}
	return writeAtomic(p, data)
}

func writeAtomic(p string, data []byte) error {
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("notes: create folder: %w", err)
	}
	mode := fs.FileMode(0o644)
	if fi, err := os.Stat(p); err == nil {
		mode = fi.Mode().Perm()
	}
	tmp, err := os.CreateTemp(dir, ".pelton-*.tmp")
	if err != nil {
		return fmt.Errorf("notes: write: %w", err)
	}
	name := tmp.Name()
	cleanup := func() { _ = os.Remove(name) }
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		cleanup()
		return fmt.Errorf("notes: write: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		cleanup()
		return fmt.Errorf("notes: sync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("notes: write: %w", err)
	}
	if err := os.Chmod(name, mode); err != nil {
		cleanup()
		return fmt.Errorf("notes: write: %w", err)
	}
	if err := os.Rename(name, p); err != nil {
		cleanup()
		return fmt.Errorf("notes: replace: %w", err)
	}
	syncDir(dir)
	return nil
}

// syncDir makes a rename durable where the platform allows it.
func syncDir(dir string) {
	if runtime.GOOS == "windows" {
		return
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		d.Close()
	}
}

// Mkdir creates a folder and any missing parents.
func (v *Vault) Mkdir(rel string) error {
	p, err := v.abs(rel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(p, 0o755); err != nil {
		return fmt.Errorf("notes: create folder %q: %w", rel, err)
	}
	return nil
}

// Rename moves a file or folder inside the vault. It fails with ErrExists
// when the destination is taken, unless only the case of the name changes.
func (v *Vault) Rename(from, to string) error {
	src, err := v.abs(from)
	if err != nil {
		return err
	}
	dst, err := v.abs(to)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(src); err != nil {
		return ErrNotFound
	}
	if _, err := os.Lstat(dst); err == nil && !strings.EqualFold(src, dst) {
		return ErrExists
	}
	if within(src, dst) && src != dst && !strings.EqualFold(src, dst) {
		return ErrOutside
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("notes: create folder: %w", err)
	}
	if err := os.Rename(src, dst); err != nil {
		return fmt.Errorf("notes: rename %q: %w", from, err)
	}
	return nil
}

// Trash moves a file or folder into the vault's .trash folder and returns
// where it went, relative to the vault. A name already in the trash gets a
// number added.
func (v *Vault) Trash(rel string) (string, error) {
	src, err := v.abs(rel)
	if err != nil {
		return "", err
	}
	if _, err := os.Lstat(src); err != nil {
		return "", ErrNotFound
	}
	clean, _ := CleanRel(rel)
	trashRoot := filepath.Join(v.root, TrashDir)
	if fi, err := os.Lstat(trashRoot); err == nil && fi.Mode()&fs.ModeSymlink != 0 {
		return "", ErrOutside
	}
	dest := path.Join(TrashDir, clean)
	for i := 1; ; i++ {
		if _, err := os.Lstat(filepath.Join(v.root, filepath.FromSlash(dest))); errors.Is(err, fs.ErrNotExist) {
			break
		}
		ext := path.Ext(clean)
		dest = path.Join(TrashDir, strings.TrimSuffix(clean, ext)+" "+strconv.Itoa(i)+ext)
	}
	abs := filepath.Join(v.root, filepath.FromSlash(dest))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", fmt.Errorf("notes: create trash: %w", err)
	}
	if err := os.Rename(src, abs); err != nil {
		return "", fmt.Errorf("notes: move %q to trash: %w", rel, err)
	}
	return dest, nil
}

// Restore moves something Trash put away back to rel. The trash path must be
// inside the trash folder.
func (v *Vault) Restore(trashed, rel string) error {
	trashed = strings.ReplaceAll(trashed, "\\", "/")
	clean := path.Clean(trashed)
	if !strings.HasPrefix(clean, TrashDir+"/") || strings.Contains(clean, "..") {
		return ErrOutside
	}
	src := filepath.Join(v.root, filepath.FromSlash(clean))
	real, err := filepath.EvalSymlinks(filepath.Dir(src))
	if err != nil || !within(filepath.Join(v.root, TrashDir), real) {
		return ErrOutside
	}
	dst, err := v.abs(rel)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(dst); err == nil {
		return ErrExists
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("notes: create folder: %w", err)
	}
	if err := os.Rename(src, dst); err != nil {
		return fmt.Errorf("notes: restore %q: %w", rel, err)
	}
	return nil
}

// Entry is one file or folder found in the vault.
type Entry struct {
	Path  string
	Dir   bool
	Size  int64
	MTime time.Time
}

// Walk lists every file and folder in the vault, skipping dot folders and
// dot files (.obsidian, .trash, .git and the like). Symlinked folders are not
// followed; a symlinked file is listed only when it points at a file inside
// the vault.
func (v *Vault) Walk() ([]Entry, error) {
	var out []Entry
	err := filepath.WalkDir(v.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == v.root {
				return err
			}
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if p == v.root {
			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(v.root, p)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if d.Type()&fs.ModeSymlink != 0 {
			real, err := filepath.EvalSymlinks(p)
			if err != nil || !within(v.root, real) {
				return nil
			}
			fi, err := os.Stat(real)
			if err != nil || !fi.Mode().IsRegular() {
				return nil
			}
			out = append(out, Entry{Path: rel, Size: fi.Size(), MTime: fi.ModTime()})
			return nil
		}
		if d.IsDir() {
			out = append(out, Entry{Path: rel, Dir: true})
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		out = append(out, Entry{Path: rel, Size: fi.Size(), MTime: fi.ModTime()})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("notes: scan vault: %w", err)
	}
	return out, nil
}

// IsNote reports whether a vault path is a Markdown note.
func IsNote(rel string) bool {
	return strings.EqualFold(path.Ext(rel), ".md")
}
