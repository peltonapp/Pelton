package notes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/peltonapp/Pelton/internal/storage"
)

// Store keeps the index rows of a vault between runs.
type Store interface {
	LoadVaultNotes(ctx context.Context, vault string) ([]storage.VaultNote, error)
	SaveVaultNotes(ctx context.Context, vault string, notes []storage.VaultNote) error
	DeleteVaultNotes(ctx context.Context, vault string, paths []string) error
}

// Change says which notes changed on disk or through Pelton. Reset means
// the whole vault was (re)loaded and anything may have changed; Tree means
// files or folders came or went.
type Change struct {
	Changed []string
	Removed []string
	Reset   bool
	Tree    bool
}

// ErrNoVault is any call made before a vault is open.
var ErrNoVault = errors.New("notes: no notes folder is open")

// Service owns one open vault: its files, the index of its notes, the
// search over them and the operations that change them. Reads are cheap and
// concurrent; every change runs one at a time.
type Service struct {
	store     Store
	searchDir string
	log       *slog.Logger
	onChange  func(Change)

	// op serialises scans and writes, so the index never sees a file half
	// way through one of Pelton's own changes.
	op sync.Mutex

	mu       sync.RWMutex
	vault    *Vault
	search   *searchIndex
	fresh    bool
	ready    bool
	notes    map[string]*storage.VaultNote
	files    map[string]Entry
	folders  []string
	resolver *Resolver
	undo     map[string]undoOp
	undoSeq  int
}

// NewService creates a service with no vault open. searchDir is where the
// full-text index lives; onChange, which may be nil, hears every change.
func NewService(store Store, searchDir string, log *slog.Logger, onChange func(Change)) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{store: store, searchDir: searchDir, log: log, onChange: onChange, undo: map[string]undoOp{}}
}

// Open closes any vault that is open and opens the folder at dir, loading
// what the index knew about it. Call Scan next to catch up with the disk.
func (s *Service) Open(ctx context.Context, dir string) error {
	s.op.Lock()
	defer s.op.Unlock()
	s.closeLocked()
	v, err := OpenVault(dir)
	if err != nil {
		return err
	}
	idx, fresh, err := openSearch(s.searchDir, v.Root())
	if err != nil {
		return err
	}
	rows, err := s.store.LoadVaultNotes(ctx, v.Root())
	if err != nil {
		idx.close()
		return err
	}
	notes := make(map[string]*storage.VaultNote, len(rows))
	files := make(map[string]Entry, len(rows))
	paths := make([]string, 0, len(rows))
	for i := range rows {
		n := rows[i]
		notes[n.Path] = &n
		files[n.Path] = Entry{Path: n.Path, Size: n.Size, MTime: time.Unix(0, n.MTime)}
		paths = append(paths, n.Path)
	}
	s.mu.Lock()
	s.vault = v
	s.search = idx
	s.fresh = fresh
	s.ready = false
	s.notes = notes
	s.files = files
	s.folders = nil
	s.resolver = NewResolver(paths)
	s.undo = map[string]undoOp{}
	s.mu.Unlock()
	return nil
}

// Close closes the open vault, if any.
func (s *Service) Close() {
	s.op.Lock()
	defer s.op.Unlock()
	s.closeLocked()
}

func (s *Service) closeLocked() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.search != nil {
		if err := s.search.close(); err != nil {
			s.log.Error("close notes search", "err", err)
		}
	}
	s.vault, s.search, s.notes, s.files, s.folders, s.resolver = nil, nil, nil, nil, nil, nil
	s.ready = false
}

// Root returns the open vault's folder, or "" when none is open.
func (s *Service) Root() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.vault == nil {
		return ""
	}
	return s.vault.Root()
}

// Ready reports whether the first scan of the open vault has finished.
func (s *Service) Ready() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ready
}

func (s *Service) current() (*Vault, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.vault == nil {
		return nil, ErrNoVault
	}
	return s.vault, nil
}

// Run scans the vault every interval until ctx ends, so edits made by other
// apps show up while Pelton runs.
func (s *Service) Run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := s.Scan(ctx); err != nil && !errors.Is(err, ErrNoVault) {
				s.log.Warn("scan notes folder", "err", err)
			}
		}
	}
}

// Scan compares the vault on disk with the index and brings the index up to
// date: new and changed notes are read and indexed, vanished ones dropped.
// A file whose size and time did not move is not read at all.
func (s *Service) Scan(ctx context.Context) error {
	s.op.Lock()
	defer s.op.Unlock()
	change, err := s.scanLocked(ctx)
	if err != nil {
		return err
	}
	s.emit(change)
	return nil
}

func (s *Service) scanLocked(ctx context.Context) (Change, error) {
	v, err := s.current()
	if err != nil {
		return Change{}, err
	}
	entries, err := v.Walk()
	if err != nil {
		return Change{}, err
	}
	s.mu.RLock()
	fresh, wasReady := s.fresh, s.ready
	known := s.notes
	oldFiles := s.files
	oldFolders := s.folders
	s.mu.RUnlock()

	var (
		saves   []storage.VaultNote
		docs    = map[string]searchDoc{}
		changed []string
		folders []string
	)
	files := make(map[string]Entry, len(entries))
	seen := map[string]bool{}
	for _, e := range entries {
		if e.Dir {
			folders = append(folders, e.Path)
			continue
		}
		files[e.Path] = e
		if !IsNote(e.Path) {
			continue
		}
		seen[e.Path] = true
		mem := known[e.Path]
		if mem != nil && !fresh && mem.MTime == e.MTime.UnixNano() && mem.Size == e.Size {
			continue
		}
		data, err := v.Read(e.Path)
		if err != nil {
			continue
		}
		hash := hashOf(data)
		if mem != nil && !fresh && mem.Hash == hash {
			n := *mem
			n.MTime, n.Size = e.MTime.UnixNano(), e.Size
			saves = append(saves, n)
			continue
		}
		n, doc := buildNote(e.Path, data, e.MTime.UnixNano(), e.Size, hash)
		saves = append(saves, n)
		docs[e.Path] = doc
		changed = append(changed, e.Path)
	}
	var removed []string
	for p := range known {
		if !seen[p] {
			removed = append(removed, p)
		}
	}
	sort.Strings(changed)
	sort.Strings(removed)
	sort.Strings(folders)

	if err := s.store.SaveVaultNotes(ctx, v.Root(), saves); err != nil {
		return Change{}, err
	}
	if err := s.store.DeleteVaultNotes(ctx, v.Root(), removed); err != nil {
		return Change{}, err
	}
	s.mu.Lock()
	if s.vault != v {
		s.mu.Unlock()
		return Change{}, nil
	}
	next := make(map[string]*storage.VaultNote, len(known)+len(saves))
	for p, n := range known {
		if seen[p] {
			next[p] = n
		}
	}
	for i := range saves {
		n := saves[i]
		next[n.Path] = &n
	}
	filesChanged := !sameFiles(oldFiles, files)
	s.notes = next
	s.files = files
	s.folders = folders
	if filesChanged || s.resolver == nil {
		s.resolver = NewResolver(keys(files))
	}
	s.fresh = false
	s.ready = true
	search := s.search
	s.mu.Unlock()
	if search != nil {
		if err := search.put(docs, removed); err != nil {
			s.log.Warn("index notes for search", "err", err)
		}
	}
	change := Change{
		Changed: changed,
		Removed: removed,
		Reset:   !wasReady,
		Tree:    filesChanged || strings.Join(oldFolders, "\n") != strings.Join(folders, "\n"),
	}
	return change, nil
}

func (s *Service) emit(c Change) {
	if s.onChange == nil || !c.Reset && !c.Tree && len(c.Changed) == 0 && len(c.Removed) == 0 {
		return
	}
	s.onChange(c)
}

func sameFiles(a, b map[string]Entry) bool {
	if len(a) != len(b) {
		return false
	}
	for p := range a {
		if _, ok := b[p]; !ok {
			return false
		}
	}
	return true
}

func keys(m map[string]Entry) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func hashOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// buildNote parses a note's bytes into its index row and search document.
func buildNote(rel string, data []byte, mtime, size int64, hash string) (storage.VaultNote, searchDoc) {
	text := string(data)
	if !utf8.ValidString(text) {
		text = strings.ToValidUTF8(text, "")
	}
	p := Parse(text)
	n := storage.VaultNote{Path: rel, Title: Title(rel), MTime: mtime, Size: size, Hash: hash, Aliases: p.Aliases, Tags: p.Tags}
	headings := make([]string, 0, len(p.Headings))
	for _, h := range p.Headings {
		n.Headings = append(n.Headings, storage.VaultHeading{Level: h.Level, Text: h.Text, Line: h.Line})
		headings = append(headings, h.Text)
	}
	for _, l := range p.Links {
		n.Links = append(n.Links, storage.VaultLink{Kind: string(l.Kind), Embed: l.Embed, Target: l.Target, Subpath: l.Subpath, Display: l.Display, Line: l.Line})
	}
	body := PlainBody(text, p)
	n.Snippet = Snippet(body)
	doc := searchDoc{
		Title:    n.Title,
		Aliases:  strings.Join(p.Aliases, "\n"),
		Headings: strings.Join(headings, "\n"),
		Tags:     strings.Join(p.Tags, " "),
		Body:     body,
	}
	return n, doc
}

// refreshLocked re-reads the given notes after Pelton wrote them, without a
// walk of the whole vault. The caller holds op.
func (s *Service) refreshLocked(ctx context.Context, paths ...string) Change {
	v, err := s.current()
	if err != nil {
		return Change{}
	}
	var (
		saves   []storage.VaultNote
		docs    = map[string]searchDoc{}
		changed []string
	)
	for _, p := range paths {
		fi, err := v.Stat(p)
		if err != nil {
			continue
		}
		data, err := v.Read(p)
		if err != nil {
			continue
		}
		n, doc := buildNote(p, data, fi.ModTime().UnixNano(), fi.Size(), hashOf(data))
		saves = append(saves, n)
		docs[p] = doc
		changed = append(changed, p)
	}
	if err := s.store.SaveVaultNotes(ctx, v.Root(), saves); err != nil {
		s.log.Warn("index saved note", "err", err)
	}
	s.mu.Lock()
	for i := range saves {
		n := saves[i]
		s.notes[n.Path] = &n
		s.files[n.Path] = Entry{Path: n.Path, Size: n.Size, MTime: time.Unix(0, n.MTime)}
	}
	search := s.search
	s.mu.Unlock()
	if search != nil {
		if err := search.put(docs, nil); err != nil {
			s.log.Warn("index saved note for search", "err", err)
		}
	}
	return Change{Changed: changed}
}

// --- reading -----------------------------------------------------------------

// NoteFile is a note's text as it is on disk. Hash is what a save passes back
// to prove it edited this version. ReadOnly is set for a file that is not
// valid UTF-8, which Pelton shows but will not write, since it could not do
// so without changing bytes it does not understand.
type NoteFile struct {
	Path     string
	Content  string
	Hash     string
	MTime    int64
	ReadOnly bool
}

// Read returns a note's text.
func (s *Service) Read(rel string) (NoteFile, error) {
	v, err := s.current()
	if err != nil {
		return NoteFile{}, err
	}
	clean, err := CleanRel(rel)
	if err != nil {
		return NoteFile{}, err
	}
	data, err := v.Read(clean)
	if err != nil {
		return NoteFile{}, err
	}
	fi, err := v.Stat(clean)
	if err != nil {
		return NoteFile{}, err
	}
	return NoteFile{Path: clean, Content: string(data), Hash: hashOf(data), MTime: fi.ModTime().UnixMilli(), ReadOnly: !utf8.Valid(data)}, nil
}

// ReadFile returns the bytes of any file in the vault, such as an image a
// note embeds. Files over maxFile are refused.
func (s *Service) ReadFile(rel string, maxFile int64) ([]byte, error) {
	v, err := s.current()
	if err != nil {
		return nil, err
	}
	fi, err := v.Stat(rel)
	if err != nil {
		return nil, err
	}
	if fi.IsDir() || fi.Size() > maxFile {
		return nil, fmt.Errorf("notes: %q is too large to show", rel)
	}
	return v.Read(rel)
}

// --- writing -----------------------------------------------------------------

// SaveResult is what a save did. On a conflict nothing was written and Disk
// holds the text that is on disk now.
type SaveResult struct {
	Hash     string
	MTime    int64
	Conflict bool
	Disk     string
	DiskHash string
}

// Save writes a note's new text. baseHash is the hash of the version the
// text was edited from; when the file changed on disk since, nothing is
// written and the result says so, unless force is set.
func (s *Service) Save(ctx context.Context, rel, content, baseHash string, force bool) (SaveResult, error) {
	s.op.Lock()
	defer s.op.Unlock()
	v, err := s.current()
	if err != nil {
		return SaveResult{}, err
	}
	clean, err := CleanRel(rel)
	if err != nil {
		return SaveResult{}, err
	}
	disk, readErr := v.Read(clean)
	created := errors.Is(readErr, ErrNotFound)
	switch {
	case created:
		if !force && baseHash != "" {
			return SaveResult{Conflict: true}, nil
		}
	case readErr != nil:
		return SaveResult{}, readErr
	default:
		if !utf8.Valid(disk) {
			return SaveResult{}, fmt.Errorf("notes: %q is not UTF-8 text and is kept read-only", clean)
		}
		h := hashOf(disk)
		if h == hashOf([]byte(content)) {
			fi, _ := v.Stat(clean)
			var mtime int64
			if fi != nil {
				mtime = fi.ModTime().UnixMilli()
			}
			return SaveResult{Hash: h, MTime: mtime}, nil
		}
		if !force && h != baseHash {
			return SaveResult{Conflict: true, Disk: string(disk), DiskHash: h}, nil
		}
	}
	if err := v.Write(clean, []byte(content)); err != nil {
		return SaveResult{}, err
	}
	if created {
		s.rescanLocked(ctx)
	} else {
		s.emit(s.refreshLocked(ctx, clean))
	}
	fi, err := v.Stat(clean)
	if err != nil {
		return SaveResult{}, err
	}
	return SaveResult{Hash: hashOf([]byte(content)), MTime: fi.ModTime().UnixMilli()}, nil
}

// Create writes a new note, or a new file of any kind, and fails with
// ErrExists when the path is taken.
func (s *Service) Create(ctx context.Context, rel string, content []byte) (string, error) {
	s.op.Lock()
	defer s.op.Unlock()
	v, err := s.current()
	if err != nil {
		return "", err
	}
	clean, err := CleanRel(rel)
	if err != nil {
		return "", err
	}
	if err := v.Create(clean, content); err != nil {
		return "", err
	}
	s.rescanLocked(ctx)
	return clean, nil
}

// CreateFolder makes a folder in the vault.
func (s *Service) CreateFolder(ctx context.Context, rel string) (string, error) {
	s.op.Lock()
	defer s.op.Unlock()
	v, err := s.current()
	if err != nil {
		return "", err
	}
	clean, err := CleanRel(rel)
	if err != nil {
		return "", err
	}
	if v.Exists(clean) {
		return "", ErrExists
	}
	if err := v.Mkdir(clean); err != nil {
		return "", err
	}
	s.rescanLocked(ctx)
	return clean, nil
}

func (s *Service) rescanLocked(ctx context.Context) {
	change, err := s.scanLocked(ctx)
	if err != nil {
		s.log.Warn("scan notes folder", "err", err)
		return
	}
	s.emit(change)
}

// UniquePath returns rel, or rel with a number before its extension when the
// name is taken: "Note.md", "Note 1.md", "Note 2.md".
func (s *Service) UniquePath(rel string) (string, error) {
	v, err := s.current()
	if err != nil {
		return "", err
	}
	clean, err := CleanRel(rel)
	if err != nil {
		return "", err
	}
	ext := path.Ext(clean)
	stem := strings.TrimSuffix(clean, ext)
	candidate := clean
	for i := 1; v.Exists(candidate); i++ {
		candidate = stem + " " + strconv.Itoa(i) + ext
	}
	return candidate, nil
}

// --- moving, renaming and deleting -------------------------------------------

// undoOp is enough to take back one rename or delete.
type undoOp struct {
	moves    map[string]string
	trashed  string
	original string
	rewrites []rewrite
}

type rewrite struct {
	path   string
	before []byte
	after  string
}

// OpResult is what a rename, move or delete did. Moves maps every old path
// to its new one; Rewritten lists the notes whose links were updated.
type OpResult struct {
	UndoID    string
	Moves     map[string]string
	Rewritten []string
}

// Move renames or moves a note, a file or a folder, and updates every link
// in the vault that pointed at what moved. Only the link targets change in
// those notes. The whole operation can be taken back with Undo.
func (s *Service) Move(ctx context.Context, from, to string) (OpResult, error) {
	s.op.Lock()
	defer s.op.Unlock()
	v, err := s.current()
	if err != nil {
		return OpResult{}, err
	}
	src, err := CleanRel(from)
	if err != nil {
		return OpResult{}, err
	}
	dst, err := CleanRel(to)
	if err != nil {
		return OpResult{}, err
	}
	if src == dst {
		return OpResult{Moves: map[string]string{}}, nil
	}
	// catch up first, so the links are rewritten against what is on disk.
	if _, err := s.scanLocked(ctx); err != nil {
		return OpResult{}, err
	}
	s.mu.RLock()
	before := s.resolver
	moves := map[string]string{}
	var after []string
	for p := range s.files {
		switch {
		case p == src:
			moves[p] = dst
			after = append(after, dst)
		case strings.HasPrefix(p, src+"/"):
			moved := dst + strings.TrimPrefix(p, src)
			moves[p] = moved
			after = append(after, moved)
		default:
			after = append(after, p)
		}
	}
	type pending struct {
		before, after string
	}
	var candidates []pending
	for p, n := range s.notes {
		touches := moves[p] != ""
		if !touches {
			for _, l := range n.Links {
				var r string
				switch LinkKind(l.Kind) {
				case LinkWiki:
					r = before.Resolve(l.Target, p)
				case LinkMarkdown:
					r = before.ResolveMarkdown(l.Target, p)
				}
				if _, ok := moves[r]; ok && r != "" {
					touches = true
					break
				}
			}
		}
		if touches {
			np := p
			if m, ok := moves[p]; ok {
				np = m
			}
			candidates = append(candidates, pending{p, np})
		}
	}
	s.mu.RUnlock()
	if len(moves) == 0 {
		return OpResult{}, ErrNotFound
	}
	afterResolver := NewResolver(after)

	// work out every rewrite before anything moves, so a file that cannot be
	// read leaves the vault as it was.
	var rewrites []rewrite
	for _, c := range candidates {
		data, err := v.Read(c.before)
		if err != nil || !utf8.Valid(data) {
			continue
		}
		next, n := RewriteLinks(string(data), c.before, c.after, before, afterResolver, moves)
		if n > 0 {
			rewrites = append(rewrites, rewrite{path: c.after, before: data, after: next})
		}
	}
	if err := v.Rename(src, dst); err != nil {
		return OpResult{}, err
	}
	var rewritten []string
	for i, r := range rewrites {
		if err := v.Write(r.path, []byte(r.after)); err != nil {
			s.log.Warn("update links after rename", "note", r.path, "err", err)
			rewrites[i].after = ""
			continue
		}
		rewritten = append(rewritten, r.path)
	}
	id := s.remember(undoOp{moves: moves, rewrites: rewrites})
	s.rescanLocked(ctx)
	return OpResult{UndoID: id, Moves: moves, Rewritten: rewritten}, nil
}

// Delete moves a note, file or folder to the vault's trash folder. Links to
// it are left as they are. Undo puts it back.
func (s *Service) Delete(ctx context.Context, rel string) (OpResult, error) {
	s.op.Lock()
	defer s.op.Unlock()
	v, err := s.current()
	if err != nil {
		return OpResult{}, err
	}
	clean, err := CleanRel(rel)
	if err != nil {
		return OpResult{}, err
	}
	trashed, err := v.Trash(clean)
	if err != nil {
		return OpResult{}, err
	}
	id := s.remember(undoOp{trashed: trashed, original: clean})
	s.rescanLocked(ctx)
	return OpResult{UndoID: id, Moves: map[string]string{}}, nil
}

const undoKeep = 20

func (s *Service) remember(op undoOp) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.undoSeq++
	id := strconv.Itoa(s.undoSeq)
	s.undo[id] = op
	if old := strconv.Itoa(s.undoSeq - undoKeep); s.undoSeq > undoKeep {
		delete(s.undo, old)
	}
	return id
}

// Undo takes back a rename, move or delete. A note whose links were updated
// and that changed again since is left alone rather than overwritten.
func (s *Service) Undo(ctx context.Context, id string) error {
	s.op.Lock()
	defer s.op.Unlock()
	v, err := s.current()
	if err != nil {
		return err
	}
	s.mu.Lock()
	op, ok := s.undo[id]
	delete(s.undo, id)
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("notes: nothing to undo")
	}
	if op.trashed != "" {
		if err := v.Restore(op.trashed, op.original); err != nil {
			return err
		}
		s.rescanLocked(ctx)
		return nil
	}
	for _, r := range op.rewrites {
		if r.after == "" {
			continue
		}
		now, err := v.Read(r.path)
		if err != nil || string(now) != r.after {
			continue
		}
		if err := v.Write(r.path, r.before); err != nil {
			s.log.Warn("undo link update", "note", r.path, "err", err)
		}
	}
	// the top of what moved is the shortest new path.
	var top, back string
	for old, moved := range op.moves {
		if top == "" || len(moved) < len(top) {
			top, back = moved, old
		}
	}
	if top != "" {
		if err := v.Rename(top, back); err != nil {
			return err
		}
	}
	s.rescanLocked(ctx)
	return nil
}

// --- queries -----------------------------------------------------------------

// OutLink is one link a note makes. Resolved is the vault path it points at,
// or "" for a link to nothing or to something outside the vault.
type OutLink struct {
	Kind     string
	Embed    bool
	Target   string
	Subpath  string
	Display  string
	Line     int
	Resolved string
}

// NoteInfo is everything the notes space shows about a note without opening
// it.
type NoteInfo struct {
	Path     string
	Title    string
	MTime    int64
	Size     int64
	Tags     []string
	Aliases  []string
	Headings []Heading
	Snippet  string
	Links    []OutLink
}

// Snapshot is the whole index: every note, folder and other file.
type Snapshot struct {
	Root    string
	Ready   bool
	Notes   []NoteInfo
	Folders []string
	Files   []string
}

// Snapshot returns the index of the open vault.
func (s *Service) Snapshot() (Snapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.vault == nil {
		return Snapshot{}, ErrNoVault
	}
	out := Snapshot{Root: s.vault.Root(), Ready: s.ready, Folders: append([]string(nil), s.folders...)}
	for p := range s.files {
		if !IsNote(p) {
			out.Files = append(out.Files, p)
		}
	}
	sort.Strings(out.Files)
	for p, n := range s.notes {
		info := NoteInfo{Path: p, Title: n.Title, MTime: n.MTime / int64(time.Millisecond), Size: n.Size, Tags: n.Tags, Aliases: n.Aliases, Snippet: n.Snippet}
		for _, h := range n.Headings {
			info.Headings = append(info.Headings, Heading{Level: h.Level, Text: h.Text, Line: h.Line})
		}
		for _, l := range n.Links {
			ol := OutLink{Kind: l.Kind, Embed: l.Embed, Target: l.Target, Subpath: l.Subpath, Display: l.Display, Line: l.Line}
			switch LinkKind(l.Kind) {
			case LinkWiki:
				ol.Resolved = s.resolver.Resolve(l.Target, p)
			case LinkMarkdown:
				ol.Resolved = s.resolver.ResolveMarkdown(l.Target, p)
			}
			info.Links = append(info.Links, ol)
		}
		out.Notes = append(out.Notes, info)
	}
	sort.Slice(out.Notes, func(i, j int) bool { return out.Notes[i].Path < out.Notes[j].Path })
	return out, nil
}

// Mention is a line of one note that names another: a link to it, or its
// name written out without a link.
type Mention struct {
	Source string
	Line   int
	Text   string
	// Match is the name as written, for an unlinked mention.
	Match string
}

// Backlinks lists the lines in other notes that link to rel.
func (s *Service) Backlinks(rel string) ([]Mention, error) {
	v, err := s.current()
	if err != nil {
		return nil, err
	}
	s.mu.RLock()
	bySource := map[string][]int{}
	for p, n := range s.notes {
		for _, l := range n.Links {
			var r string
			switch LinkKind(l.Kind) {
			case LinkWiki:
				r = s.resolver.Resolve(l.Target, p)
			case LinkMarkdown:
				r = s.resolver.ResolveMarkdown(l.Target, p)
			}
			if r == rel && p != rel {
				bySource[p] = append(bySource[p], l.Line)
			}
		}
	}
	s.mu.RUnlock()
	return mentionLines(v, bySource), nil
}

func mentionLines(v *Vault, bySource map[string][]int) []Mention {
	sources := make([]string, 0, len(bySource))
	for p := range bySource {
		sources = append(sources, p)
	}
	sort.Strings(sources)
	var out []Mention
	for _, p := range sources {
		data, err := v.Read(p)
		if err != nil {
			continue
		}
		lines := strings.Split(string(data), "\n")
		seen := map[int]bool{}
		for _, ln := range bySource[p] {
			if seen[ln] || ln >= len(lines) {
				continue
			}
			seen[ln] = true
			out = append(out, Mention{Source: p, Line: ln, Text: strings.TrimSpace(strings.TrimSuffix(lines[ln], "\r"))})
		}
	}
	return out
}

// UnlinkedMentions lists the lines in other notes that write rel's name or
// one of its aliases without linking to it.
func (s *Service) UnlinkedMentions(rel string) ([]Mention, error) {
	v, err := s.current()
	if err != nil {
		return nil, err
	}
	s.mu.RLock()
	n := s.notes[rel]
	if n == nil {
		s.mu.RUnlock()
		return nil, ErrNotFound
	}
	names := append([]string{n.Title}, n.Aliases...)
	search := s.search
	sources := map[string]bool{}
	for p := range s.notes {
		if p != rel {
			sources[p] = true
		}
	}
	s.mu.RUnlock()
	candidates := map[string]bool{}
	if search != nil {
		for _, name := range names {
			hits, err := search.phrase(name, 500)
			if err != nil {
				continue
			}
			for _, h := range hits {
				if sources[h] {
					candidates[h] = true
				}
			}
		}
	}
	paths := make([]string, 0, len(candidates))
	for p := range candidates {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var out []Mention
	for _, p := range paths {
		data, err := v.Read(p)
		if err != nil {
			continue
		}
		out = append(out, findUnlinked(p, string(data), names)...)
	}
	return out, nil
}

// findUnlinked finds the lines of text that name one of names as a whole
// word, outside links, code and front matter.
func findUnlinked(source, text string, names []string) []Mention {
	p := Parse(text)
	type span struct{ start, end int }
	var covered []span
	for _, l := range p.Links {
		covered = append(covered, span{l.Start, l.End})
	}
	var out []Mention
	lines := strings.SplitAfter(text, "\n")
	offset := 0
	fence := false
	for i, line := range lines {
		start := offset
		offset += len(line)
		if start < p.BodyStart {
			continue
		}
		content := strings.TrimRight(line, "\r\n")
		if fenceRe.MatchString(content) {
			fence = !fence
			continue
		}
		if fence {
			continue
		}
		lower := strings.ToLower(content)
		for _, name := range names {
			if strings.TrimSpace(name) == "" {
				continue
			}
			ln := strings.ToLower(name)
			found := ""
			for from := 0; from < len(lower); {
				at := strings.Index(lower[from:], ln)
				if at < 0 {
					break
				}
				at += from
				end := at + len(ln)
				from = end
				if !wordEdge(lower, at-1) || !wordEdge(lower, end) {
					continue
				}
				inLink := false
				for _, c := range covered {
					if start+at >= c.start && start+at < c.end {
						inLink = true
						break
					}
				}
				if !inLink {
					found = content[at:end]
					break
				}
			}
			if found != "" {
				out = append(out, Mention{Source: source, Line: i, Text: strings.TrimSpace(content), Match: found})
				break
			}
		}
	}
	return out
}

func wordEdge(s string, i int) bool {
	if i < 0 || i >= len(s) {
		return true
	}
	r, _ := utf8.DecodeRuneInString(s[i:])
	if r == utf8.RuneError && i > 0 {
		r, _ = utf8.DecodeLastRuneInString(s[:i+1])
	}
	return !isWordRune(r)
}

func isWordRune(r rune) bool {
	return r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r > 127 && (isTagRune(r) && r != '-' && r != '/')
}

// LinkMention turns the first unlinked occurrence of match on one line of
// source into a wiki link to target, keeping the text as written as the
// link's alias when it differs from the name the link uses.
func (s *Service) LinkMention(ctx context.Context, source string, line int, match, target string) error {
	s.op.Lock()
	defer s.op.Unlock()
	v, err := s.current()
	if err != nil {
		return err
	}
	data, err := v.Read(source)
	if err != nil {
		return err
	}
	if !utf8.Valid(data) {
		return fmt.Errorf("notes: %q is kept read-only", source)
	}
	text := string(data)
	s.mu.RLock()
	name := s.resolver.LinkText(target)
	s.mu.RUnlock()
	found := findUnlinked(source, text, []string{match})
	ok := false
	for _, m := range found {
		if m.Line == line {
			ok = true
		}
	}
	if !ok {
		return ErrNotFound
	}
	lines := strings.SplitAfter(text, "\n")
	offset := 0
	for i := 0; i < line; i++ {
		offset += len(lines[i])
	}
	content := lines[line]
	lower := strings.ToLower(content)
	at := -1
	for from := 0; from < len(lower); {
		i := strings.Index(lower[from:], strings.ToLower(match))
		if i < 0 {
			break
		}
		i += from
		if wordEdge(lower, i-1) && wordEdge(lower, i+len(match)) && !inLinkAt(text, offset+i) {
			at = i
			break
		}
		from = i + len(match)
	}
	if at < 0 {
		return ErrNotFound
	}
	written := content[at : at+len(match)]
	link := "[[" + name + "|" + written + "]]"
	if path.Base(name) == written {
		link = "[[" + name + "]]"
	}
	next := text[:offset+at] + link + text[offset+at+len(match):]
	if err := v.Write(source, []byte(next)); err != nil {
		return err
	}
	s.emit(s.refreshLocked(ctx, source))
	return nil
}

func inLinkAt(text string, at int) bool {
	for _, l := range Parse(text).Links {
		if at >= l.Start && at < l.End {
			return true
		}
	}
	return false
}

// SearchHit is one note matching a search, with the line that matched.
type SearchHit struct {
	Path    string
	Title   string
	Context string
}

// Search finds notes by their text, name, aliases, headings and tags.
func (s *Service) Search(query string, limit int) ([]SearchHit, error) {
	v, err := s.current()
	if err != nil {
		return nil, err
	}
	s.mu.RLock()
	search := s.search
	s.mu.RUnlock()
	if search == nil {
		return nil, nil
	}
	paths, err := search.search(query, limit)
	if err != nil {
		return nil, err
	}
	words := strings.Fields(strings.ToLower(query))
	out := make([]SearchHit, 0, len(paths))
	for _, p := range paths {
		hit := SearchHit{Path: p, Title: Title(p)}
		if data, err := v.Read(p); err == nil {
			hit.Context = contextLine(string(data), words)
		}
		out = append(out, hit)
	}
	return out, nil
}

// contextLine is the first body line holding one of words, shortened.
func contextLine(text string, words []string) string {
	p := Parse(text)
	for _, line := range strings.Split(text[p.BodyStart:], "\n") {
		lower := strings.ToLower(line)
		for _, w := range words {
			if i := strings.Index(lower, w); i >= 0 {
				return trimAround(strings.TrimSpace(strings.TrimSuffix(line, "\r")), w)
			}
		}
	}
	return ""
}

func trimAround(line, word string) string {
	const room = 80
	runes := []rune(line)
	if len(runes) <= room*2 {
		return line
	}
	at := strings.Index(strings.ToLower(line), word)
	pos := utf8.RuneCountInString(line[:max(at, 0)])
	start := max(0, pos-room)
	end := min(len(runes), pos+room)
	out := string(runes[start:end])
	if start > 0 {
		out = "…" + out
	}
	if end < len(runes) {
		out += "…"
	}
	return out
}
