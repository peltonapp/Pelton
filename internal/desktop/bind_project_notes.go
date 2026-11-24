package desktop

// A project's notes are Markdown files in the notes folder. A note belongs to
// a project through its front matter:
//
//	project: Website relaunch
//	project-id: 3fa9c0...
//
// project-id is the project's uid and decides; project is its name, for
// people and for other apps such as Obsidian, and is kept current when the
// project is renamed. A note with only a project name, typed by hand, belongs
// to the project of that name. So a note stays in its project when the
// project is renamed, when the file is moved or renamed, here or in another
// app, and when it is edited anywhere else. New notes go to a folder per
// project, Projects/<name> by default.
//
// A card holds a note by its path in the notes folder. Pelton keeps that path
// current when it moves a note, and when a scan finds a note that vanished
// and one that appeared with the same bytes, which is what a rename in
// another app looks like.
//
// Notes kept in the database before are written into the notes folder the
// first time project notes are asked for with a folder open; see
// migrateProjectNotes. Without a folder they stay in the database and work
// as before.

import (
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/notes"
	"github.com/peltonapp/Pelton/internal/storage"
)

// defaultProjectsDir is the folder of the notes folder project notes go to.
const defaultProjectsDir = "Projects"

// ProjectVaultNoteDTO is a note of the notes folder as the Projects space
// lists it. MTime is unix milliseconds.
type ProjectVaultNoteDTO struct {
	Path    string `json:"path"`
	Title   string `json:"title"`
	Snippet string `json:"snippet"`
	MTime   int64  `json:"mtime"`
}

// ProjectVaultNotesDTO is what the notes folder holds for a project: the
// notes that belong to it and the other notes that link to it, its cards,
// its mail or its events, the last edited first. Available is false when no
// notes folder is open; the project's notes then live in the database.
type ProjectVaultNotesDTO struct {
	Available bool                  `json:"available"`
	Notes     []ProjectVaultNoteDTO `json:"notes"`
	Linked    []ProjectVaultNoteDTO `json:"linked"`
}

func projectProps(p *storage.Project) []notes.Property {
	return []notes.Property{{Key: notes.PropProject, Value: p.Name}, {Key: notes.PropProjectID, Value: p.UID}}
}

// projectNoteDir is the folder new notes of a project go to.
func (a *App) projectNoteDir(p *storage.Project) string {
	base := strings.Trim(strings.TrimSpace(a.stringSetting(settingNotesProjects, defaultProjectsDir)), "/")
	if base == "" {
		return notes.SafeName(p.Name)
	}
	return base + "/" + notes.SafeName(p.Name)
}

// ListProjectVaultNotes returns a project's notes in the notes folder and the
// notes that link to it. The first call with a folder open moves the notes
// kept in the database into it.
func (a *App) ListProjectVaultNotes(projectID int64) (ProjectVaultNotesDTO, error) {
	out := ProjectVaultNotesDTO{Notes: []ProjectVaultNoteDTO{}, Linked: []ProjectVaultNoteDTO{}}
	if err := a.ready(); err != nil {
		return out, err
	}
	p, err := a.store.GetProject(a.ctx, projectID)
	if err != nil {
		return out, err
	}
	svc := a.vaultService()
	if svc.Root() == "" {
		return out, nil
	}
	if err := a.migrateProjectNotes(svc); err != nil {
		a.log.Warn("move project notes into the notes folder", "err", err)
	}
	briefs, err := svc.Briefs()
	if errors.Is(err, notes.ErrNoVault) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	out.Available = true
	targets, err := a.projectTargets(p)
	if err != nil {
		return out, err
	}
	for _, b := range briefs {
		dto := ProjectVaultNoteDTO{Path: b.Path, Title: b.Title, Snippet: b.Snippet, MTime: b.MTime}
		switch {
		case notes.InProject(b.Props, p.UID, p.Name):
			out.Notes = append(out.Notes, dto)
		case linksToAny(b.Pelton, targets):
			out.Linked = append(out.Linked, dto)
		}
	}
	for _, list := range [][]ProjectVaultNoteDTO{out.Notes, out.Linked} {
		sort.SliceStable(list, func(i, j int) bool { return list[i].MTime > list[j].MTime })
	}
	return out, nil
}

// projectTargets is every pelton:// object that belongs to a project, as
// "kind:id": the project, its cards' tasks, its conversations and events.
func (a *App) projectTargets(p *storage.Project) (map[string]bool, error) {
	out := map[string]bool{"project:" + p.UID: true}
	cards, err := a.store.ListProjectCards(a.ctx, p.ID)
	if err != nil {
		return nil, err
	}
	for _, c := range cards {
		out["task:"+c.TaskUID] = true
	}
	links, err := a.store.ListProjectLinks(a.ctx, p.ID, "")
	if err != nil {
		return nil, err
	}
	for _, l := range links {
		switch l.Kind {
		case storage.LinkMail:
			ref := strings.ToLower(strings.Trim(l.Ref, "<>"))
			out["mail:"+ref] = true
			out["conversation:"+ref] = true
		case storage.LinkEvent:
			out["event:"+l.Ref] = true
		}
	}
	return out, nil
}

func linksToAny(links []string, targets map[string]bool) bool {
	for _, raw := range links {
		kind, id, _, ok := notes.ParsePeltonURL(raw)
		if !ok {
			continue
		}
		if kind == "mail" || kind == "conversation" {
			id = strings.ToLower(strings.Trim(id, "<>"))
		}
		if targets[kind+":"+id] {
			return true
		}
	}
	return false
}

// CreateProjectVaultNote writes a new note of a project into the project's
// folder, under a free name made from title. body is the note's text, usually
// a filled template; the project goes into its front matter.
func (a *App) CreateProjectVaultNote(projectID int64, title, body string) (ProjectVaultNoteDTO, error) {
	svc, err := a.openedVault()
	if err != nil {
		return ProjectVaultNoteDTO{}, err
	}
	p, err := a.store.GetProject(a.ctx, projectID)
	if err != nil {
		return ProjectVaultNoteDTO{}, err
	}
	free, err := svc.UniquePath(a.projectNoteDir(p) + "/" + notes.SafeName(title) + ".md")
	if err != nil {
		return ProjectVaultNoteDTO{}, err
	}
	rel, err := svc.Create(a.ctx, free, []byte(notes.SetProperties(body, projectProps(p), nil)))
	if err != nil {
		return ProjectVaultNoteDTO{}, err
	}
	a.emitProjects(p.ID)
	return ProjectVaultNoteDTO{Path: rel, Title: notes.Title(rel), MTime: time.Now().UnixMilli()}, nil
}

// AddNoteToProject puts a note of the notes folder in a project by writing
// the project into its front matter. A note in another project moves to this
// one. Nothing else in the file changes.
func (a *App) AddNoteToProject(rel string, projectID int64) error {
	svc, err := a.openedVault()
	if err != nil {
		return err
	}
	p, err := a.store.GetProject(a.ctx, projectID)
	if err != nil {
		return err
	}
	if _, err := svc.SetProperties(a.ctx, rel, projectProps(p), nil); err != nil {
		return err
	}
	a.emitProjects()
	return nil
}

// RemoveNoteFromProject takes a note out of a project: the project leaves
// its front matter when it is this one, and the project's cards let go of
// it. The file stays.
func (a *App) RemoveNoteFromProject(rel string, projectID int64) error {
	svc, err := a.openedVault()
	if err != nil {
		return err
	}
	p, err := a.store.GetProject(a.ctx, projectID)
	if err != nil {
		return err
	}
	f, err := svc.Read(rel)
	if err != nil {
		return err
	}
	if notes.InProject(notes.Parse(f.Content).Props, p.UID, p.Name) {
		if _, err := svc.SetProperties(a.ctx, f.Path, nil, []string{notes.PropProject, notes.PropProjectID}); err != nil {
			return err
		}
	}
	if err := a.store.DeleteProjectLinksByRef(a.ctx, p.ID, storage.LinkNote, f.Path); err != nil {
		return err
	}
	a.emitProjects(p.ID)
	return nil
}

// AttachVaultNote attaches a note of the notes folder to a card. A note in no
// project yet joins the card's project.
func (a *App) AttachVaultNote(rel, taskUID string) error {
	svc, err := a.openedVault()
	if err != nil {
		return err
	}
	card, err := a.store.GetProjectCard(a.ctx, taskUID)
	if err != nil {
		return err
	}
	f, err := svc.Read(rel)
	if err != nil {
		return err
	}
	if uid, name := notes.ProjectOf(notes.Parse(f.Content).Props); uid == "" && name == "" {
		p, err := a.store.GetProject(a.ctx, card.ProjectID)
		if err != nil {
			return err
		}
		if _, err := svc.SetProperties(a.ctx, f.Path, projectProps(p), nil); err != nil {
			return err
		}
	}
	link := storage.ProjectLink{ProjectID: card.ProjectID, TaskUID: taskUID, Kind: storage.LinkNote, Ref: f.Path, Label: notes.Title(f.Path)}
	if err := a.store.AddProjectLink(a.ctx, &link); err != nil {
		return err
	}
	a.emitProjects(card.ProjectID)
	return nil
}

// TrashProjectNote moves a project's note to the trash folder of the notes
// folder and lets the project's cards go of it. VaultUndo with the returned
// id brings the file back.
func (a *App) TrashProjectNote(rel string, projectID int64) (VaultOpDTO, error) {
	svc, err := a.openedVault()
	if err != nil {
		return VaultOpDTO{}, err
	}
	clean, err := notes.CleanRel(rel)
	if err != nil {
		return VaultOpDTO{}, err
	}
	r, err := svc.Delete(a.ctx, clean)
	if err != nil {
		return VaultOpDTO{}, err
	}
	if err := a.store.DeleteProjectLinksByRef(a.ctx, projectID, storage.LinkNote, clean); err != nil {
		return VaultOpDTO{}, err
	}
	a.emitProjects(projectID)
	return VaultOpDTO{UndoID: r.UndoID, Moves: r.Moves, Rewritten: []string{}}, nil
}

// followNoteMoves points card links at the new paths of notes that moved.
func (a *App) followNoteMoves(moves map[string]string) {
	if len(moves) == 0 || a.store == nil {
		return
	}
	changed, err := a.store.RepointNoteLinks(a.ctx, moves)
	if err != nil {
		a.log.Warn("follow moved notes", "err", err)
		return
	}
	if changed {
		a.emitProjects()
	}
}

// renameProjectNotes writes a project's new name into the front matter of
// its notes, and its uid into notes that named it only by its old name.
func (a *App) renameProjectNotes(before, after *storage.Project) {
	svc := a.vaultService()
	briefs, err := svc.Briefs()
	if err != nil {
		return
	}
	for _, b := range briefs {
		if !notes.InProject(b.Props, before.UID, before.Name) {
			continue
		}
		if _, err := svc.SetProperties(a.ctx, b.Path, projectProps(after), nil); err != nil {
			a.log.Warn("rename the project in a note", "err", err)
		}
	}
}

// projectNoteCounts counts each project's notes: the ones in the notes folder
// and the ones still kept in the database.
func (a *App) projectNoteCounts(projects []storage.Project) (map[int64]int, error) {
	out := map[int64]int{}
	for _, p := range projects {
		rows, err := a.store.ListProjectNotes(a.ctx, p.ID)
		if err != nil {
			return nil, err
		}
		out[p.ID] = len(rows)
	}
	briefs, err := a.vaultService().Briefs()
	if err != nil {
		return out, nil
	}
	for _, b := range briefs {
		uid, name := notes.ProjectOf(b.Props)
		if uid == "" && name == "" {
			continue
		}
		for _, p := range projects {
			if notes.InProject(b.Props, p.UID, p.Name) {
				out[p.ID]++
				break
			}
		}
	}
	return out, nil
}

// --- moving the database notes into the notes folder ------------------------

// migrateProjectNotes writes every project note still kept in the database
// into the notes folder, once per folder. Each note goes to a free name in
// its project's folder; a file that is there already is never overwritten.
// The row keeps its text and is marked as moved, and the card links that
// named it by row id name the file instead. A note that fails stays in the
// database and is tried again next time.
func (a *App) migrateProjectNotes(svc *notes.Service) error {
	a.projectNotesMu.Lock()
	defer a.projectNotesMu.Unlock()
	root := svc.Root()
	if root == "" || a.projectNotesMoved == root {
		return nil
	}
	pending, err := a.store.PendingProjectNotes(a.ctx)
	if err != nil {
		return err
	}
	var first error
	touched := []int64{}
	projects := map[int64]*storage.Project{}
	for _, n := range pending {
		p := projects[n.ProjectID]
		if p == nil {
			if p, err = a.store.GetProject(a.ctx, n.ProjectID); err != nil {
				first = firstErr(first, err)
				continue
			}
			projects[n.ProjectID] = p
		}
		if err := a.migrateProjectNote(svc, p, n); err != nil {
			first = firstErr(first, fmt.Errorf("project note %d: %w", n.ID, err))
			continue
		}
		touched = append(touched, n.ProjectID)
	}
	if first == nil {
		a.projectNotesMoved = root
	}
	if len(touched) > 0 {
		a.emitProjects(touched...)
	}
	return first
}

func firstErr(first, next error) error {
	if first != nil {
		return first
	}
	return next
}

// migrateProjectNote moves one note. The path is recorded before the file is
// written, so a move cut short finds its own file: the same text there means
// it was written and only the bookkeeping is left; other text means someone
// else's file took the name, and the note goes to another free one.
func (a *App) migrateProjectNote(svc *notes.Service, p *storage.Project, n storage.ProjectNote) error {
	content := legacyNoteText(p, n)
	rel := n.VaultPath
	if rel != "" {
		f, err := svc.Read(rel)
		switch {
		case err == nil && f.Content == content:
			return a.store.FinishProjectNoteMigration(a.ctx, n.ID, f.Path)
		case err == nil:
			rel = ""
		case !errors.Is(err, notes.ErrNotFound):
			return err
		}
	}
	for tries := 0; ; tries++ {
		if rel == "" {
			free, err := svc.UniquePath(path.Join(a.projectNoteDir(p), notes.SafeName(legacyNoteTitle(n))+".md"))
			if err != nil {
				return err
			}
			rel = free
			if err := a.store.SetProjectNoteVaultPath(a.ctx, n.ID, rel); err != nil {
				return err
			}
		}
		_, err := svc.Create(a.ctx, rel, []byte(content))
		if errors.Is(err, notes.ErrExists) && tries < 3 {
			rel = ""
			continue
		}
		if err != nil {
			return err
		}
		break
	}
	f, err := svc.Read(rel)
	if err != nil {
		return err
	}
	if f.Content != content {
		return fmt.Errorf("the file written to %q does not hold the note", rel)
	}
	return a.store.FinishProjectNoteMigration(a.ctx, n.ID, f.Path)
}

// legacyNoteTitle is what a database note is called: its title, else its
// first line of text.
func legacyNoteTitle(n storage.ProjectNote) string {
	if t := strings.TrimSpace(n.Title); t != "" {
		return t
	}
	for _, line := range strings.Split(n.Body, "\n") {
		if t := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "#")); t != "" {
			return t
		}
	}
	return "Untitled"
}

// legacyNoteText is the file a database note becomes: its text with the
// project and the day it was made in the front matter. It depends on the row
// alone, so an interrupted move can recognise what it wrote.
func legacyNoteText(p *storage.Project, n storage.ProjectNote) string {
	props := projectProps(p)
	if t, err := time.Parse(time.RFC3339, n.CreatedAt); err == nil {
		props = append(props, notes.Property{Key: "created", Value: t.UTC().Format(dateLayout)})
	}
	return notes.SetProperties(n.Body, props, nil)
}
