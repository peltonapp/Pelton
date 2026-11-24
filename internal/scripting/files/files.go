// Package files gives scripts the files module: reading, writing, appending
// and listing files inside the one folder the user granted a script. The
// grant is the user's, picked in a folder dialog and kept per script on this
// device; a script cannot name or widen it, and an exported script does not
// carry it.
//
// Every access goes through an os.Root opened on the granted folder, so a
// path with "..", an absolute path or a symbolic link that leads outside the
// folder is refused when the file is opened, not only when the path is
// checked. Nothing is ever run, and files are written without execute bits.
package files

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/peltonapp/Pelton/internal/scripting"
)

// The permissions this package registers.
const (
	PermRead  = "files.read"
	PermWrite = "files.write"
)

// Limits of one call.
const (
	maxFileBytes = 4 << 20
	maxEntries   = 1000
)

// Grants tells which folder the user granted a script.
type Grants interface {
	// Folder returns the granted folder of a script, "" when there is none.
	Folder(ctx context.Context, scriptID int64) (string, error)
}

// ErrNoFolder is returned when the script has no folder granted.
var ErrNoFolder = errors.New("files: this script has no folder; pick one on its page")

// open returns the root of the script's granted folder.
func open(c *scripting.Call, g Grants) (*os.Root, error) {
	if g == nil {
		return nil, scripting.ErrUnavailable
	}
	if c.ScriptID() == 0 {
		return nil, ErrNoFolder
	}
	dir, err := g.Folder(c.Ctx, c.ScriptID())
	if err != nil {
		return nil, err
	}
	if dir == "" {
		return nil, ErrNoFolder
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("files: open the granted folder: %w", err)
	}
	return root, nil
}

// relPath reads argument i as a path inside the folder: relative, with "/"
// between parts, and without "..". def is used when the argument is missing.
func relPath(c *scripting.Call, i int, def string) (string, error) {
	p, err := c.OptString(i, def)
	if err != nil {
		return "", err
	}
	p = strings.ReplaceAll(p, "\\", "/")
	if p == "" || strings.ContainsRune(p, 0) || path.IsAbs(p) || filepath.IsAbs(p) || filepath.VolumeName(p) != "" {
		return "", fmt.Errorf("%s: give a path inside the folder, such as \"notes/today.txt\"", c.Function)
	}
	for _, part := range strings.Split(p, "/") {
		if part == ".." {
			return "", fmt.Errorf("%s: a path cannot leave the folder", c.Function)
		}
	}
	return filepath.FromSlash(path.Clean(p)), nil
}

func withRoot(g Grants, do func(c *scripting.Call, root *os.Root) ([]any, error)) scripting.Handler {
	return func(c *scripting.Call) ([]any, error) {
		root, err := open(c, g)
		if err != nil {
			return nil, err
		}
		defer root.Close()
		return do(c, root)
	}
}

func read(c *scripting.Call, root *os.Root) ([]any, error) {
	p, err := relPath(c, 0, "")
	if err != nil {
		return nil, err
	}
	if info, err := root.Stat(p); err == nil && !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: %s is not a plain file", c.Function, filepath.ToSlash(p))
	}
	f, err := root.Open(p)
	if err != nil {
		return []any{nil, cleanErr(err)}, nil
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: %s is not a plain file", c.Function, filepath.ToSlash(p))
	}
	data, err := io.ReadAll(io.LimitReader(f, maxFileBytes+1))
	if err != nil {
		return []any{nil, cleanErr(err)}, nil
	}
	if len(data) > maxFileBytes {
		return nil, fmt.Errorf("%s: the file is larger than %d bytes", c.Function, maxFileBytes)
	}
	return []any{string(data)}, nil
}

func exists(c *scripting.Call, root *os.Root) ([]any, error) {
	p, err := relPath(c, 0, "")
	if err != nil {
		return nil, err
	}
	_, err = root.Stat(p)
	return []any{err == nil}, nil
}

func list(c *scripting.Call, root *os.Root) ([]any, error) {
	p, err := relPath(c, 0, ".")
	if err != nil {
		return nil, err
	}
	dir, err := root.Open(p)
	if err != nil {
		return []any{nil, cleanErr(err)}, nil
	}
	defer dir.Close()
	entries, err := dir.ReadDir(maxEntries)
	if err != nil && !errors.Is(err, io.EOF) {
		return []any{nil, cleanErr(err)}, nil
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	out := make([]any, 0, len(entries))
	for _, e := range entries {
		item := map[string]any{"name": e.Name(), "dir": e.IsDir()}
		if info, err := e.Info(); err == nil {
			item["size"] = info.Size()
			item["modified"] = info.ModTime().Unix()
		}
		out = append(out, item)
	}
	return []any{out}, nil
}

func write(appendTo bool) func(c *scripting.Call, root *os.Root) ([]any, error) {
	return func(c *scripting.Call, root *os.Root) ([]any, error) {
		p, err := relPath(c, 0, "")
		if err != nil {
			return nil, err
		}
		text, err := c.String(1)
		if err != nil {
			return nil, err
		}
		if len(text) > maxFileBytes {
			return nil, fmt.Errorf("%s: at most %d bytes at a time", c.Function, maxFileBytes)
		}
		if info, err := root.Stat(p); err == nil && (!info.Mode().IsRegular() || info.Mode().Perm()&0o111 != 0) {
			return nil, fmt.Errorf("%s: %s is not a plain file a script may change", c.Function, filepath.ToSlash(p))
		}
		flag := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
		if appendTo {
			flag = os.O_WRONLY | os.O_CREATE | os.O_APPEND
			if info, err := root.Stat(p); err == nil && info.Size()+int64(len(text)) > maxFileBytes {
				return nil, fmt.Errorf("%s: the file would grow past %d bytes", c.Function, maxFileBytes)
			}
		}
		if dir := filepath.Dir(p); dir != "." {
			if err := root.MkdirAll(dir, 0o700); err != nil {
				return nil, fmt.Errorf("%s: %s", c.Function, cleanErr(err))
			}
		}
		f, err := root.OpenFile(p, flag, 0o600)
		if err != nil {
			return nil, fmt.Errorf("%s: %s", c.Function, cleanErr(err))
		}
		if _, err := f.WriteString(text); err != nil {
			f.Close()
			return nil, fmt.Errorf("%s: %s", c.Function, cleanErr(err))
		}
		return nil, f.Close()
	}
}

// cleanErr drops the absolute folder from an error, so a script learns
// nothing about where the folder is.
func cleanErr(err error) string {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Op + " " + filepath.ToSlash(pe.Path) + ": " + pe.Err.Error()
	}
	return err.Error()
}

// Register adds the files permissions, the files module and its block
// actions.
func Register(r *scripting.Registry, g Grants) error {
	pathParam := scripting.Required(scripting.NewParam("path", scripting.TypeString))
	dirParam := scripting.NewParam("path", scripting.TypeString)
	text := scripting.Required(scripting.NewParam("text", scripting.TypeText))

	reader := func(name string, h func(*scripting.Call, *os.Root) ([]any, error), returns []scripting.Field, params ...scripting.Param) scripting.Function {
		f := scripting.NewFunction("files", name, withRoot(g, h), params...)
		f.Permission = PermRead
		f.Returns = returns
		return f
	}
	writer := func(name string, appendTo bool) scripting.Function {
		f := scripting.NewFunction("files", name, withRoot(g, write(appendTo)), pathParam, text)
		f.Permission = PermWrite
		f.Effect = true
		return f
	}
	entry := []scripting.Field{
		scripting.NewField("name", "string"), scripting.NewField("dir", "bool"),
		scripting.NewField("size", "number"), scripting.NewField("modified", "number"),
	}
	fns := []scripting.Function{
		reader("read", read, []scripting.Field{scripting.NewField("text", "text")}, pathParam),
		reader("exists", exists, []scripting.Field{scripting.NewField("exists", "bool")}, pathParam),
		reader("list", list, entry, dirParam),
		writer("write", false),
		writer("append", true),
	}
	action := func(id, lua string) scripting.Action {
		return scripting.Action{Meta: scripting.EntryMeta("action", id, "files"), Params: []scripting.Param{pathParam, text}, Permission: PermWrite, Lua: lua}
	}
	return r.Register(scripting.Entries{
		Permissions: []scripting.Permission{
			{Meta: scripting.EntryMeta("perm", PermRead, "files"), Risk: "medium"},
			{Meta: scripting.EntryMeta("perm", PermWrite, "files"), Risk: "high"},
		},
		Modules: []scripting.Module{{Meta: scripting.EntryMeta("module", "files", "files"), Functions: fns}},
		Actions: []scripting.Action{
			action("files.write", "files.write({{path}}, {{text}})"),
			action("files.append", "files.append({{path}}, {{text}})"),
		},
	})
}
