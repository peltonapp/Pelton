package scripting

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	lua "github.com/yuin/gopher-lua"
)

// Library is a Lua module the user writes once and loads from several
// scripts with require("name"). It runs in the sandbox of the script that
// loads it, with that script's permissions, so it can do nothing the script
// could not.
type Library struct {
	ID          int64
	Name        string
	Description string
	Source      string
	Origin      Origin
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// KindLibrary is the kind a library has in a .peltonscript file.
const KindLibrary Kind = "library"

var libraryName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// Validate checks a library's name, which require takes, and its size.
func (l *Library) Validate() error {
	if !libraryName.MatchString(l.Name) {
		return fmt.Errorf("scripting: a library name is 1 to 64 lower case letters, digits and _, starting with a letter")
	}
	if len(l.Source) > maxSourceBytes {
		return fmt.Errorf("scripting: the library is longer than %d bytes", maxSourceBytes)
	}
	return nil
}

// LibrarySource finds the library require loads. Store implements it.
type LibrarySource interface {
	LibraryByName(ctx context.Context, name string) (*Library, error)
}

// libraryChunk is the chunk name a library's code runs under, so its errors
// read "name.lua:3: ...".
func libraryChunk(name string) string {
	return name + ".lua"
}

var libraryErrorRE = regexp.MustCompile(`^([a-z][a-z0-9_]*)\.lua:(\d+):`)

var requireRE = regexp.MustCompile(`\brequire\s*\(?\s*["']([a-z][a-z0-9_]*)["']`)

// RequiredLibraries lists the libraries Lua source loads by name with
// require, each once, in the order they first appear.
func RequiredLibraries(src string) []string {
	var out []string
	for _, m := range requireRE.FindAllStringSubmatch(src, -1) {
		if !slices.Contains(out, m[1]) {
			out = append(out, m[1])
		}
	}
	return out
}

// require loads a library by name: from the run's cache, or from the
// LibrarySource, in this run's state, once. A library that requires one
// already loading is a cycle and fails with the chain.
func (st *runState) require(L *lua.LState) int {
	name := L.CheckString(1)
	if v, ok := st.libs[name]; ok {
		L.Push(v)
		return 1
	}
	if !st.requirable[name] {
		L.RaiseError("require: %q is not named in the code; a library is loaded by its name written out, as in require(%q)", name, "name")
	}
	if i := slices.Index(st.loading, name); i >= 0 {
		chain := append(slices.Clone(st.loading[i:]), name)
		L.RaiseError("require: libraries load each other in a circle: %s", strings.Join(chain, " -> "))
	}
	if st.e.Libraries == nil {
		L.RaiseError("require: there is no library %q", name)
	}
	lib, err := st.e.Libraries.LibraryByName(st.ctx, name)
	if errors.Is(err, ErrNotFound) {
		L.RaiseError("require: there is no library %q; libraries are written in Automations, Libraries", name)
	}
	if err != nil {
		L.RaiseError("require %q: %s", name, err.Error())
	}
	fn, err := L.Load(strings.NewReader(lib.Source), libraryChunk(name))
	if err != nil {
		L.RaiseError("require %q: %s", name, cleanLuaError(err))
	}
	for _, n := range RequiredLibraries(lib.Source) {
		st.requirable[n] = true
	}
	st.loading = append(st.loading, name)
	L.Push(fn)
	err = L.PCall(0, 1, nil)
	st.loading = st.loading[:len(st.loading)-1]
	if st.unwinding() {
		L.RaiseError("%s", st.failed().msg)
	}
	if err != nil {
		L.Error(errorValue(err), 0)
	}
	v := L.Get(-1)
	L.Pop(1)
	if v == lua.LNil {
		v = lua.LTrue
	}
	st.libs[name] = v
	L.Push(v)
	return 1
}

const libraryColumns = `id, name, description, source, origin, created_at, updated_at`

func scanLibrary(row rowScanner) (*Library, error) {
	var (
		l                         Library
		origin, created, modified string
	)
	err := row.Scan(&l.ID, &l.Name, &l.Description, &l.Source, &origin, &created, &modified)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scripting: read library: %w", err)
	}
	l.Origin, l.CreatedAt, l.UpdatedAt = Origin(origin), parseTime(created), parseTime(modified)
	return &l, nil
}

// errLibraryTaken is returned when a library of that name exists.
func errLibraryTaken(name string) error {
	return fmt.Errorf("scripting: there is already a library named %q", name)
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// CreateLibrary stores a new library and returns its id.
func (st *Store) CreateLibrary(ctx context.Context, l *Library) (int64, error) {
	now := fmtTime(time.Now())
	res, err := st.db.ExecContext(ctx, `
INSERT INTO script_libraries (name, description, source, origin, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		l.Name, l.Description, l.Source, string(l.Origin), now, now)
	if isUniqueViolation(err) {
		return 0, errLibraryTaken(l.Name)
	}
	if err != nil {
		return 0, fmt.Errorf("scripting: create library: %w", err)
	}
	return res.LastInsertId()
}

// UpdateLibrary writes a library's name, description and source.
func (st *Store) UpdateLibrary(ctx context.Context, l *Library) error {
	res, err := st.db.ExecContext(ctx, `
UPDATE script_libraries SET name = ?, description = ?, source = ?, updated_at = ? WHERE id = ?`,
		l.Name, l.Description, l.Source, fmtTime(time.Now()), l.ID)
	if isUniqueViolation(err) {
		return errLibraryTaken(l.Name)
	}
	if err != nil {
		return fmt.Errorf("scripting: update library %d: %w", l.ID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// GetLibrary returns one library by id.
func (st *Store) GetLibrary(ctx context.Context, id int64) (*Library, error) {
	return scanLibrary(st.db.QueryRowContext(ctx, `SELECT `+libraryColumns+` FROM script_libraries WHERE id = ?`, id))
}

// LibraryByName returns the library require("name") loads, or ErrNotFound.
func (st *Store) LibraryByName(ctx context.Context, name string) (*Library, error) {
	return scanLibrary(st.db.QueryRowContext(ctx, `SELECT `+libraryColumns+` FROM script_libraries WHERE name = ?`, name))
}

// ListLibraries returns every library by name.
func (st *Store) ListLibraries(ctx context.Context) ([]*Library, error) {
	rows, err := st.db.QueryContext(ctx, `SELECT `+libraryColumns+` FROM script_libraries ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("scripting: list libraries: %w", err)
	}
	defer rows.Close()
	var out []*Library
	for rows.Next() {
		l, err := scanLibrary(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// DeleteLibrary removes a library. Scripts that require it fail until one of
// that name exists again.
func (st *Store) DeleteLibrary(ctx context.Context, id int64) error {
	if _, err := st.db.ExecContext(ctx, `DELETE FROM script_libraries WHERE id = ?`, id); err != nil {
		return fmt.Errorf("scripting: delete library %d: %w", id, err)
	}
	return nil
}

// Libraries lists every library.
func (s *Service) Libraries(ctx context.Context) ([]*Library, error) {
	return s.store.ListLibraries(ctx)
}

// Library returns one library.
func (s *Service) Library(ctx context.Context, id int64) (*Library, error) {
	return s.store.GetLibrary(ctx, id)
}

// CreateLibrary validates and stores a new library. A script that already
// requires a library of that name loses its approval, since it would now
// run code the user did not see when approving it.
func (s *Service) CreateLibrary(ctx context.Context, l *Library) (*Library, error) {
	if l.Origin == "" {
		l.Origin = OriginUser
	}
	if err := l.Validate(); err != nil {
		return nil, err
	}
	id, err := s.store.CreateLibrary(ctx, l)
	if err != nil {
		return nil, err
	}
	if err := s.revokeRequiring(ctx, l.Name); err != nil {
		return nil, err
	}
	s.changed()
	return s.store.GetLibrary(ctx, id)
}

// UpdateLibrary saves a library's name, description and source. A change to
// the source or the name takes back the approval of every script that loads
// the library, directly or through another library.
func (s *Service) UpdateLibrary(ctx context.Context, l *Library) (*Library, error) {
	if err := l.Validate(); err != nil {
		return nil, err
	}
	cur, err := s.store.GetLibrary(ctx, l.ID)
	if err != nil {
		return nil, err
	}
	if cur.Source != l.Source || cur.Name != l.Name {
		if err := s.revokeRequiring(ctx, cur.Name, l.Name); err != nil {
			return nil, err
		}
	}
	if err := s.store.UpdateLibrary(ctx, l); err != nil {
		return nil, err
	}
	s.changed()
	return s.store.GetLibrary(ctx, l.ID)
}

// DeleteLibrary removes a library, taking back the approval of the scripts
// that load it, so a library of that name written later is not run unseen.
func (s *Service) DeleteLibrary(ctx context.Context, id int64) error {
	cur, err := s.store.GetLibrary(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := s.revokeRequiring(ctx, cur.Name); err != nil {
		return err
	}
	if err := s.store.DeleteLibrary(ctx, id); err != nil {
		return err
	}
	s.changed()
	return nil
}

// librariesLoaded lists the libraries a script's code loads, directly or
// through other libraries, as they stand now.
func librariesLoaded(sc *Script, sources map[string]string) []string {
	var out []string
	queue := scriptRequires(sc)
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if slices.Contains(out, name) {
			continue
		}
		out = append(out, name)
		queue = append(queue, RequiredLibraries(sources[name])...)
	}
	return out
}

// revokeRequiring switches off and takes back the approvals of every script
// that loads one of names.
func (s *Service) revokeRequiring(ctx context.Context, names ...string) error {
	libs, err := s.store.ListLibraries(ctx)
	if err != nil {
		return err
	}
	sources := map[string]string{}
	for _, l := range libs {
		sources[l.Name] = l.Source
	}
	list, err := s.store.List(ctx)
	if err != nil {
		return err
	}
	touched := false
	for _, sc := range list {
		if !sc.Enabled && len(sc.Approved) == 0 && len(sc.AllowUnasked) == 0 {
			continue
		}
		loaded := librariesLoaded(sc, sources)
		if !slices.ContainsFunc(names, func(n string) bool { return slices.Contains(loaded, n) }) {
			continue
		}
		revoke(sc)
		if err := s.store.Update(ctx, sc); err != nil {
			return err
		}
		touched = true
	}
	if touched {
		return s.load(ctx)
	}
	return nil
}

// MissingLibraries returns the names among names that no library has.
func (s *Service) MissingLibraries(ctx context.Context, names []string) ([]string, error) {
	var out []string
	for _, n := range names {
		_, err := s.store.LibraryByName(ctx, n)
		if errors.Is(err, ErrNotFound) {
			out = append(out, n)
			continue
		}
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Service) changed() {
	if s.cfg.OnChange != nil {
		s.cfg.OnChange()
	}
}

// ExportLibrary renders a library as the text of a .peltonscript file of
// kind "library", noting the libraries it requires in turn.
func ExportLibrary(l *Library) ([]byte, error) {
	f := File{
		Format: FileFormat, Name: l.Name, Description: l.Description, Kind: KindLibrary,
		Permissions: []string{}, Source: l.Source, Requires: RequiredLibraries(l.Source),
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("scripting: export: %w", err)
	}
	return append(b, '\n'), nil
}

// ParseLibraryFile reads a .peltonscript file of kind "library" into a
// library ready to store, marked as imported, with the libraries it
// requires.
func ParseLibraryFile(data []byte) (*Library, []string, error) {
	f, err := readFile(data)
	if err != nil {
		return nil, nil, err
	}
	if f.Kind != KindLibrary {
		return nil, nil, fmt.Errorf("scripting: the file holds a script, not a library")
	}
	l := &Library{Name: f.Name, Description: f.Description, Source: f.Source, Origin: OriginImport}
	if err := l.Validate(); err != nil {
		return nil, nil, err
	}
	return l, fileRequires(f), nil
}

// ImportLibrary stores the library in a file. A library of the same name is
// not replaced: the import fails and says so.
func (s *Service) ImportLibrary(ctx context.Context, data []byte) (*Library, error) {
	l, _, err := ParseLibraryFile(data)
	if err != nil {
		return nil, err
	}
	return s.CreateLibrary(ctx, l)
}
