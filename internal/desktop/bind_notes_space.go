package desktop

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/notes"
)

// The notes space keeps plain Markdown files in a folder the user picks, or
// in a Notes folder in the app's data directory. These bindings are its whole
// surface; the files, the index and the rules that keep a shared vault (an
// Obsidian one, say) undamaged live in internal/notes. Not to be confused
// with the private notes on a message in bind_notes.go, which live in the
// database.

const (
	// vaultScanInterval is how often the open folder is checked for changes
	// made by other apps. The window regaining focus checks at once too.
	vaultScanInterval = 5 * time.Second
	// vaultFileLimit caps the size of an image or other file a note embeds
	// that the frontend may read.
	vaultFileLimit = 25 << 20
	// defaultVaultDir is the folder in the data directory used when the user
	// has not chosen one.
	defaultVaultDir = "Notes"
	// vaultSearchDir is where the full-text index over the notes lives.
	vaultSearchDir = "notes-search"
	// defaultAttachmentsDir is where pasted and dropped images go.
	defaultAttachmentsDir = "Attachments"
)

// VaultInfoDTO describes the open notes folder. Folder is empty when none is
// open; Default is set when it is the one in the data directory.
type VaultInfoDTO struct {
	Folder  string `json:"folder"`
	Ready   bool   `json:"ready"`
	Default bool   `json:"default"`
	Error   string `json:"error"`
}

// VaultChangeEvent is the payload of EventVaultChanged.
type VaultChangeEvent struct {
	Changed []string `json:"changed"`
	Removed []string `json:"removed"`
	Reset   bool     `json:"reset"`
	Tree    bool     `json:"tree"`
}

// VaultHeadingDTO is one heading of a note; line counts from 0.
type VaultHeadingDTO struct {
	Level int    `json:"level"`
	Text  string `json:"text"`
	Line  int    `json:"line"`
}

// VaultLinkDTO is one link a note makes. Resolved is the vault path it
// points at, empty when it points at nothing or outside the vault.
type VaultLinkDTO struct {
	Kind     string `json:"kind"`
	Embed    bool   `json:"embed"`
	Target   string `json:"target"`
	Subpath  string `json:"subpath"`
	Display  string `json:"display"`
	Line     int    `json:"line"`
	Resolved string `json:"resolved"`
}

// VaultNoteDTO is what the notes space knows about a note without opening
// it. MTime is unix milliseconds.
type VaultNoteDTO struct {
	Path     string            `json:"path"`
	Title    string            `json:"title"`
	MTime    int64             `json:"mtime"`
	Size     int64             `json:"size"`
	Tags     []string          `json:"tags"`
	Aliases  []string          `json:"aliases"`
	Headings []VaultHeadingDTO `json:"headings"`
	Snippet  string            `json:"snippet"`
	Links    []VaultLinkDTO    `json:"links"`
}

// VaultSnapshotDTO is the whole index of the open folder.
type VaultSnapshotDTO struct {
	Folder  string         `json:"folder"`
	Ready   bool           `json:"ready"`
	Notes   []VaultNoteDTO `json:"notes"`
	Folders []string       `json:"folders"`
	Files   []string       `json:"files"`
}

// VaultFileDTO is a note's text as it is on disk.
type VaultFileDTO struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	Hash     string `json:"hash"`
	MTime    int64  `json:"mtime"`
	ReadOnly bool   `json:"readOnly"`
}

// VaultSaveDTO is what a save did. On a conflict nothing was written and
// Disk holds the text on disk now.
type VaultSaveDTO struct {
	Hash     string `json:"hash"`
	MTime    int64  `json:"mtime"`
	Conflict bool   `json:"conflict"`
	Disk     string `json:"disk"`
	DiskHash string `json:"diskHash"`
}

// VaultOpDTO is what a rename, move or delete did, with the id Undo takes.
type VaultOpDTO struct {
	UndoID    string            `json:"undoId"`
	Moves     map[string]string `json:"moves"`
	Rewritten []string          `json:"rewritten"`
}

// VaultMentionDTO is a line of a note that names another one. Match is the
// name as written, for a mention that is not a link yet.
type VaultMentionDTO struct {
	Source string `json:"source"`
	Line   int    `json:"line"`
	Text   string `json:"text"`
	Match  string `json:"match"`
}

// VaultHitDTO is one search result with the line that matched.
type VaultHitDTO struct {
	Path    string `json:"path"`
	Title   string `json:"title"`
	Context string `json:"context"`
}

// startVault opens the notes folder at startup when there is one to open:
// the folder the user chose, or the default one once it exists. It never
// creates a folder; opening the notes space does that.
func (a *App) startVault() {
	if a.store == nil {
		return
	}
	dir, chosen := a.vaultDir()
	if !chosen {
		if _, err := os.Stat(dir); err != nil {
			return
		}
	}
	if _, err := a.openVault(dir); err != nil {
		a.log.Warn("open notes folder", "err", err)
	}
}

// vaultDir is the folder the notes space uses and whether the user chose it.
func (a *App) vaultDir() (string, bool) {
	if dir := strings.TrimSpace(a.stringSetting(settingNotesVault, "")); dir != "" {
		return dir, true
	}
	return filepath.Join(a.dataDir, defaultVaultDir), false
}

// vaultService returns the notes service, creating it on first use.
func (a *App) vaultService() *notes.Service {
	a.vaultMu.Lock()
	defer a.vaultMu.Unlock()
	if a.vault == nil {
		a.vault = notes.NewService(a.store, filepath.Join(a.dataDir, vaultSearchDir), a.log, func(c notes.Change) {
			a.emit(EventVaultChanged, VaultChangeEvent{Changed: c.Changed, Removed: c.Removed, Reset: c.Reset, Tree: c.Tree})
		})
	}
	return a.vault
}

// openVault opens dir unless it is already the open folder, scans it in the
// background and starts watching it.
func (a *App) openVault(dir string) (*notes.Service, error) {
	svc := a.vaultService()
	if real, err := filepath.EvalSymlinks(dir); err == nil && real == svc.Root() {
		return svc, nil
	}
	a.vaultMu.Lock()
	if a.vaultStop != nil {
		a.vaultStop()
		a.vaultStop = nil
	}
	a.vaultMu.Unlock()
	if err := svc.Open(a.ctx, dir); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.vaultMu.Lock()
	a.vaultStop = cancel
	a.vaultMu.Unlock()
	goSafe("indexing the notes folder", func() {
		if err := svc.Scan(ctx); err != nil {
			a.log.Warn("scan notes folder", "err", err)
		}
		svc.Run(ctx, vaultScanInterval)
	})
	return svc, nil
}

func (a *App) closeVault() {
	a.vaultMu.Lock()
	svc, stop := a.vault, a.vaultStop
	a.vaultStop = nil
	a.vaultMu.Unlock()
	if stop != nil {
		stop()
	}
	if svc != nil {
		svc.Close()
	}
}

// openedVault returns the service with a folder open, or an error the ui
// can show.
func (a *App) openedVault() (*notes.Service, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	svc := a.vaultService()
	if svc.Root() == "" {
		return nil, notes.ErrNoVault
	}
	return svc, nil
}

func (a *App) vaultInfo(svc *notes.Service, err error) VaultInfoDTO {
	_, chosen := a.vaultDir()
	info := VaultInfoDTO{Default: !chosen}
	if svc != nil {
		info.Folder = svc.Root()
		info.Ready = svc.Ready()
	}
	if err != nil {
		info.Error = err.Error()
	}
	return info
}

// VaultOpen opens the notes folder in the settings, creating the default
// one in the data directory the first time the notes space is used. A
// folder the user chose is never created: when it is gone, the error says so.
func (a *App) VaultOpen() (VaultInfoDTO, error) {
	if err := a.ready(); err != nil {
		return VaultInfoDTO{}, err
	}
	dir, chosen := a.vaultDir()
	if !chosen {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return a.vaultInfo(nil, err), nil
		}
	}
	svc, err := a.openVault(dir)
	return a.vaultInfo(svc, err), nil
}

// VaultChooseFolder asks for a notes folder, stores it as the setting and
// opens it. An empty folder means the user cancelled and nothing changed.
func (a *App) VaultChooseFolder() (VaultInfoDTO, error) {
	if err := a.ready(); err != nil {
		return VaultInfoDTO{}, err
	}
	dir, err := a.openDirectoryDialog(openDialogOptions{Title: "Choose a notes folder"})
	if err != nil || dir == "" {
		return VaultInfoDTO{}, err
	}
	if err := a.store.Set(a.ctx, settingNotesVault, dir); err != nil {
		return VaultInfoDTO{}, err
	}
	svc, openErr := a.openVault(dir)
	return a.vaultInfo(svc, openErr), nil
}

// VaultUseDefaultFolder goes back to the Notes folder in the data directory.
func (a *App) VaultUseDefaultFolder() (VaultInfoDTO, error) {
	if err := a.ready(); err != nil {
		return VaultInfoDTO{}, err
	}
	if err := a.store.Set(a.ctx, settingNotesVault, ""); err != nil {
		return VaultInfoDTO{}, err
	}
	return a.VaultOpen()
}

// VaultRescan checks the open folder for changes made by other apps now,
// rather than at the next scan. The frontend calls it when the window gets
// focus back.
func (a *App) VaultRescan() error {
	svc, err := a.openedVault()
	if err != nil {
		return nil
	}
	goSafe("checking the notes folder", func() {
		if err := svc.Scan(a.ctx); err != nil && !errors.Is(err, notes.ErrNoVault) {
			a.log.Warn("scan notes folder", "err", err)
		}
	})
	return nil
}

// VaultSnapshot returns every note, folder and other file of the open folder.
func (a *App) VaultSnapshot() (VaultSnapshotDTO, error) {
	svc, err := a.openedVault()
	if err != nil {
		return VaultSnapshotDTO{}, err
	}
	snap, err := svc.Snapshot()
	if err != nil {
		return VaultSnapshotDTO{}, err
	}
	out := VaultSnapshotDTO{Folder: snap.Root, Ready: snap.Ready, Folders: nonNil(snap.Folders), Files: nonNil(snap.Files), Notes: make([]VaultNoteDTO, 0, len(snap.Notes))}
	for _, n := range snap.Notes {
		d := VaultNoteDTO{Path: n.Path, Title: n.Title, MTime: n.MTime, Size: n.Size, Tags: nonNil(n.Tags), Aliases: nonNil(n.Aliases), Snippet: n.Snippet, Headings: []VaultHeadingDTO{}, Links: []VaultLinkDTO{}}
		for _, h := range n.Headings {
			d.Headings = append(d.Headings, VaultHeadingDTO{Level: h.Level, Text: h.Text, Line: h.Line})
		}
		for _, l := range n.Links {
			d.Links = append(d.Links, VaultLinkDTO{Kind: l.Kind, Embed: l.Embed, Target: l.Target, Subpath: l.Subpath, Display: l.Display, Line: l.Line, Resolved: l.Resolved})
		}
		out.Notes = append(out.Notes, d)
	}
	return out, nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// VaultRead returns a note's text and the hash a save must pass back.
func (a *App) VaultRead(rel string) (VaultFileDTO, error) {
	svc, err := a.openedVault()
	if err != nil {
		return VaultFileDTO{}, err
	}
	f, err := svc.Read(rel)
	if err != nil {
		return VaultFileDTO{}, err
	}
	return VaultFileDTO{Path: f.Path, Content: f.Content, Hash: f.Hash, MTime: f.MTime, ReadOnly: f.ReadOnly}, nil
}

// VaultSave writes a note. baseHash is the hash of the version the text was
// edited from; when the file changed on disk since, nothing is written and
// the result carries the text on disk, unless force is set.
func (a *App) VaultSave(rel, content, baseHash string, force bool) (VaultSaveDTO, error) {
	svc, err := a.openedVault()
	if err != nil {
		return VaultSaveDTO{}, err
	}
	r, err := svc.Save(a.ctx, rel, content, baseHash, force)
	if err != nil {
		return VaultSaveDTO{}, err
	}
	return VaultSaveDTO{Hash: r.Hash, MTime: r.MTime, Conflict: r.Conflict, Disk: r.Disk, DiskHash: r.DiskHash}, nil
}

// VaultCreate writes a new note and returns its path. It fails when the
// name is taken; VaultUniquePath finds a free one.
func (a *App) VaultCreate(rel, content string) (string, error) {
	svc, err := a.openedVault()
	if err != nil {
		return "", err
	}
	return svc.Create(a.ctx, rel, []byte(content))
}

// VaultUniquePath returns rel, or rel with a number added when it is taken.
func (a *App) VaultUniquePath(rel string) (string, error) {
	svc, err := a.openedVault()
	if err != nil {
		return "", err
	}
	return svc.UniquePath(rel)
}

// VaultCreateFolder makes a folder in the notes folder.
func (a *App) VaultCreateFolder(rel string) (string, error) {
	svc, err := a.openedVault()
	if err != nil {
		return "", err
	}
	return svc.CreateFolder(a.ctx, rel)
}

// VaultMove renames or moves a note, file or folder and updates the links
// to it in every other note. VaultUndo with the returned id takes it back.
func (a *App) VaultMove(from, to string) (VaultOpDTO, error) {
	svc, err := a.openedVault()
	if err != nil {
		return VaultOpDTO{}, err
	}
	r, err := svc.Move(a.ctx, from, to)
	if err != nil {
		return VaultOpDTO{}, err
	}
	return VaultOpDTO{UndoID: r.UndoID, Moves: r.Moves, Rewritten: nonNil(r.Rewritten)}, nil
}

// VaultDelete moves a note, file or folder to the trash folder of the vault.
func (a *App) VaultDelete(rel string) (VaultOpDTO, error) {
	svc, err := a.openedVault()
	if err != nil {
		return VaultOpDTO{}, err
	}
	r, err := svc.Delete(a.ctx, rel)
	if err != nil {
		return VaultOpDTO{}, err
	}
	return VaultOpDTO{UndoID: r.UndoID, Moves: r.Moves, Rewritten: []string{}}, nil
}

// VaultUndo takes back a rename, move or delete.
func (a *App) VaultUndo(id string) error {
	svc, err := a.openedVault()
	if err != nil {
		return err
	}
	return svc.Undo(a.ctx, id)
}

func mentionDTOs(in []notes.Mention) []VaultMentionDTO {
	out := make([]VaultMentionDTO, 0, len(in))
	for _, m := range in {
		out = append(out, VaultMentionDTO{Source: m.Source, Line: m.Line, Text: m.Text, Match: m.Match})
	}
	return out
}

// VaultBacklinks lists the lines in other notes that link to a note.
func (a *App) VaultBacklinks(rel string) ([]VaultMentionDTO, error) {
	svc, err := a.openedVault()
	if err != nil {
		return nil, err
	}
	m, err := svc.Backlinks(rel)
	if err != nil {
		return nil, err
	}
	return mentionDTOs(m), nil
}

// VaultUnlinkedMentions lists the lines in other notes that write a note's
// name or alias without linking to it.
func (a *App) VaultUnlinkedMentions(rel string) ([]VaultMentionDTO, error) {
	svc, err := a.openedVault()
	if err != nil {
		return nil, err
	}
	m, err := svc.UnlinkedMentions(rel)
	if err != nil {
		return nil, err
	}
	return mentionDTOs(m), nil
}

// VaultLinkMention turns an unlinked mention into a link to target.
func (a *App) VaultLinkMention(source string, line int, match, target string) error {
	svc, err := a.openedVault()
	if err != nil {
		return err
	}
	return svc.LinkMention(a.ctx, source, line, match, target)
}

// VaultSearch finds notes by their text, name, aliases, headings and tags.
func (a *App) VaultSearch(query string) ([]VaultHitDTO, error) {
	svc, err := a.openedVault()
	if err != nil {
		return nil, err
	}
	hits, err := svc.Search(query, 50)
	if err != nil {
		return nil, err
	}
	out := make([]VaultHitDTO, 0, len(hits))
	for _, h := range hits {
		out = append(out, VaultHitDTO{Path: h.Path, Title: h.Title, Context: h.Context})
	}
	return out, nil
}

// VaultSaveAttachment stores a pasted or dropped file in the attachments
// folder of the vault under a free name and returns its path. data is
// base64.
func (a *App) VaultSaveAttachment(name, data string) (string, error) {
	svc, err := a.openedVault()
	if err != nil {
		return "", err
	}
	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return "", fmt.Errorf("notes: attachment data: %w", err)
	}
	return a.storeAttachment(svc, name, raw)
}

// vaultImportLimit caps a file dropped from the system into a note.
const vaultImportLimit = 200 << 20

// VaultImportFile copies a file the user dropped from the system onto a note
// into the attachments folder of the vault, under a free name, and returns
// its vault path. The original is left where it was.
func (a *App) VaultImportFile(src string) (string, error) {
	svc, err := a.openedVault()
	if err != nil {
		return "", err
	}
	fi, err := os.Stat(src)
	if err != nil {
		return "", err
	}
	if !fi.Mode().IsRegular() || fi.Size() > vaultImportLimit {
		return "", fmt.Errorf("notes: %q cannot be added to a note", filepath.Base(src))
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return "", err
	}
	return a.storeAttachment(svc, filepath.Base(src), data)
}

func (a *App) storeAttachment(svc *notes.Service, name string, data []byte) (string, error) {
	name = path.Base(strings.ReplaceAll(strings.TrimSpace(name), "\\", "/"))
	if name == "" || name == "." || name == "/" || strings.HasPrefix(name, ".") {
		name = "Pasted " + time.Now().Format("2006-01-02 150405") + ".png"
	}
	dir := strings.Trim(strings.TrimSpace(a.stringSetting(settingNotesAttachments, defaultAttachmentsDir)), "/")
	rel := name
	if dir != "" {
		rel = dir + "/" + name
	}
	free, err := svc.UniquePath(rel)
	if err != nil {
		return "", err
	}
	return svc.Create(a.ctx, free, data)
}

// VaultFileData returns a file of the vault, such as an image a note
// embeds, as a data: URL. Large files are refused.
func (a *App) VaultFileData(rel string) (string, error) {
	svc, err := a.openedVault()
	if err != nil {
		return "", err
	}
	data, err := svc.ReadFile(rel, vaultFileLimit)
	if err != nil {
		return "", err
	}
	kind := mime.TypeByExtension(strings.ToLower(path.Ext(rel)))
	if kind == "" {
		kind = "application/octet-stream"
	}
	return "data:" + kind + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}
