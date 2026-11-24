package desktop

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/notes"
	"github.com/peltonapp/Pelton/internal/storage"
)

// writeVault puts files into the default notes folder before it is opened.
func writeVault(t *testing.T, a *App, files map[string]string) string {
	t.Helper()
	root := filepath.Join(a.dataDir, defaultVaultDir)
	for p, content := range files {
		full := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func openTestVault(t *testing.T, a *App) {
	t.Helper()
	if info, err := a.VaultOpen(); err != nil || info.Error != "" {
		t.Fatalf("open: %v %q", err, info.Error)
	}
	t.Cleanup(a.closeVault)
	waitVaultReady(t, a)
}

func readVault(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func projectNote(t *testing.T, a *App, projectID int64, title, body string) storage.ProjectNote {
	t.Helper()
	n := storage.ProjectNote{ProjectID: projectID, Title: title, Body: body}
	if err := a.store.CreateProjectNote(a.ctx, &n); err != nil {
		t.Fatal(err)
	}
	return n
}

func paths(list []ProjectVaultNoteDTO) []string {
	out := []string{}
	for _, n := range list {
		out = append(out, n.Path)
	}
	slices.Sort(out)
	return out
}

func noteLinkRefs(t *testing.T, a *App, projectID int64) []string {
	t.Helper()
	links, err := a.store.ListProjectLinks(a.ctx, projectID, storage.LinkNote)
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, l := range links {
		out = append(out, l.Ref)
	}
	slices.Sort(out)
	return out
}

func TestProjectNotesStayInTheDatabaseWithoutAFolder(t *testing.T) {
	a := newVaultTestApp(t)
	p, err := a.CreateProject(ProjectRequest{Name: "Launch"})
	if err != nil {
		t.Fatal(err)
	}
	if p.UID == "" {
		t.Fatal("a new project has no uid")
	}
	projectNote(t, a, p.ID, "Plan", "body")
	got, err := a.ListProjectVaultNotes(p.ID)
	if err != nil || got.Available {
		t.Fatalf("without a folder: %+v %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(a.dataDir, defaultVaultDir)); err == nil {
		t.Error("listing project notes created the notes folder")
	}
	local, err := a.ListProjectNotes(p.ID)
	if err != nil || len(local) != 1 {
		t.Fatalf("local notes %+v %v", local, err)
	}
	if dto, _ := a.GetProject(p.ID); dto.Notes != 1 {
		t.Errorf("count %d", dto.Notes)
	}

	// the folder turns up later: the note moves then.
	root := writeVault(t, a, nil)
	openTestVault(t, a)
	got, err = a.ListProjectVaultNotes(p.ID)
	if err != nil || !got.Available || !slices.Equal(paths(got.Notes), []string{"Projects/Launch/Plan.md"}) {
		t.Fatalf("after the folder came: %+v %v", got, err)
	}
	if text := readVault(t, root, "Projects/Launch/Plan.md"); !strings.Contains(text, "project-id: "+yamlID(p.UID)+"\n") {
		t.Errorf("front matter %q", text)
	}
	if dto, _ := a.GetProject(p.ID); dto.Notes != 1 {
		t.Errorf("count after the move %d", dto.Notes)
	}
}

func TestProjectNotesMigrateWithoutOverwriting(t *testing.T) {
	a := newVaultTestApp(t)
	p, err := a.CreateProject(ProjectRequest{Name: "Launch: Q3"})
	if err != nil {
		t.Fatal(err)
	}
	plan := projectNote(t, a, p.ID, "Plan", "# Plan\nsteps")
	agenda := projectNote(t, a, p.ID, "", "\n## Agenda\nitems")
	for _, task := range []string{"card-1", "card-2"} {
		l := storage.ProjectLink{ProjectID: p.ID, TaskUID: task, Kind: storage.LinkNote, Ref: strconv.FormatInt(plan.ID, 10), Label: "Plan"}
		if err := a.store.AddProjectLink(a.ctx, &l); err != nil {
			t.Fatal(err)
		}
	}
	mine := "my own plan, not Pelton's"
	root := writeVault(t, a, map[string]string{"Projects/Launch Q3/Plan.md": mine})
	openTestVault(t, a)

	got, err := a.ListProjectVaultNotes(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Projects/Launch Q3/Agenda.md", "Projects/Launch Q3/Plan 1.md"}
	if !slices.Equal(paths(got.Notes), want) {
		t.Fatalf("notes %v, want %v", paths(got.Notes), want)
	}
	if readVault(t, root, "Projects/Launch Q3/Plan.md") != mine {
		t.Error("an existing file was overwritten")
	}
	text := readVault(t, root, "Projects/Launch Q3/Plan 1.md")
	if !strings.HasPrefix(text, "---\nproject: \"Launch: Q3\"\nproject-id: ") || !strings.HasSuffix(text, "---\n# Plan\nsteps") {
		t.Errorf("migrated text %q", text)
	}
	if refs := noteLinkRefs(t, a, p.ID); !slices.Equal(refs, []string{"Projects/Launch Q3/Plan 1.md", "Projects/Launch Q3/Plan 1.md"}) {
		t.Errorf("card links %v", refs)
	}
	if pending, _ := a.store.PendingProjectNotes(a.ctx); len(pending) != 0 {
		t.Errorf("still pending %+v", pending)
	}
	row, err := a.store.GetProjectNote(a.ctx, agenda.ID)
	if err != nil || row.MigratedAt == "" || row.Body != agenda.Body || row.VaultPath != "Projects/Launch Q3/Agenda.md" {
		t.Errorf("row after the move %+v %v", row, err)
	}

	// asking again writes nothing new.
	a.projectNotesMoved = ""
	again, err := a.ListProjectVaultNotes(p.ID)
	if err != nil || len(again.Notes) != 2 {
		t.Fatalf("again %+v %v", again, err)
	}
	entries, _ := os.ReadDir(filepath.Join(root, "Projects", "Launch Q3"))
	if len(entries) != 3 {
		t.Errorf("%d files in the project folder, want 3", len(entries))
	}
}

func TestProjectNoteMigrationResumesAfterAnInterruption(t *testing.T) {
	a := newVaultTestApp(t)
	pDTO, err := a.CreateProject(ProjectRequest{Name: "Launch"})
	if err != nil {
		t.Fatal(err)
	}
	p, err := a.store.GetProject(a.ctx, pDTO.ID)
	if err != nil {
		t.Fatal(err)
	}
	// cut short after the path was recorded, before the file was written.
	unwritten := projectNote(t, a, p.ID, "Unwritten", "a")
	if err := a.store.SetProjectNoteVaultPath(a.ctx, unwritten.ID, "Projects/Launch/Unwritten.md"); err != nil {
		t.Fatal(err)
	}
	// cut short after the file was written, before the row was marked.
	written := projectNote(t, a, p.ID, "Written", "b")
	if err := a.store.SetProjectNoteVaultPath(a.ctx, written.ID, "Projects/Launch/Written.md"); err != nil {
		t.Fatal(err)
	}
	// cut short, and since then another file took the recorded name.
	taken := projectNote(t, a, p.ID, "Taken", "c")
	if err := a.store.SetProjectNoteVaultPath(a.ctx, taken.ID, "Projects/Launch/Taken.md"); err != nil {
		t.Fatal(err)
	}
	other := "someone else's note"
	root := writeVault(t, a, map[string]string{
		"Projects/Launch/Written.md": legacyNoteText(p, written),
		"Projects/Launch/Taken.md":   other,
	})
	openTestVault(t, a)

	got, err := a.ListProjectVaultNotes(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Projects/Launch/Taken 1.md", "Projects/Launch/Unwritten.md", "Projects/Launch/Written.md"}
	if !slices.Equal(paths(got.Notes), want) {
		t.Fatalf("notes %v, want %v", paths(got.Notes), want)
	}
	if readVault(t, root, "Projects/Launch/Taken.md") != other {
		t.Error("the file that took the name was overwritten")
	}
	if _, err := os.Stat(filepath.Join(root, "Projects", "Launch", "Written 1.md")); err == nil {
		t.Error("a note written before the interruption was written twice")
	}
	for _, id := range []int64{unwritten.ID, written.ID, taken.ID} {
		if row, err := a.store.GetProjectNote(a.ctx, id); err != nil || row.MigratedAt == "" {
			t.Errorf("row %d not marked: %+v %v", id, row, err)
		}
	}
}

func TestProjectVaultNoteMembership(t *testing.T) {
	a := newVaultTestApp(t)
	p, err := a.CreateProject(ProjectRequest{Name: "Launch"})
	if err != nil {
		t.Fatal(err)
	}
	other := "---\ntags: [x]\nweird: {a: [1, 2]}\n---\nBody with %% comment %% and ==mark==\r\n"
	root := writeVault(t, a, map[string]string{
		"Other.md":     other,
		"Typed.md":     "---\nproject: launch\n---\nby hand",
		"Mentions.md":  "see [the launch](pelton://project/" + p.UID + ")",
		"Card.md":      "about [a task](pelton://task/card-1)",
		"Elsewhere.md": "nothing",
	})
	openTestVault(t, a)
	if err := a.store.PlaceProjectCard(a.ctx, "card-1", mustColumn(t, a, p.ID), 0, 0); err != nil {
		t.Fatal(err)
	}

	made, err := a.CreateProjectVaultNote(p.ID, "Kick off", "---\ncreated: 2026-11-16\n---\n# Kick off\n")
	if err != nil || made.Path != "Projects/Launch/Kick off.md" {
		t.Fatalf("create %+v %v", made, err)
	}
	if text := readVault(t, root, made.Path); !strings.HasPrefix(text, "---\ncreated: 2026-11-16\nproject: Launch\nproject-id: ") {
		t.Errorf("new note %q", text)
	}
	if err := a.AddNoteToProject("Other.md", p.ID); err != nil {
		t.Fatal(err)
	}
	if text := readVault(t, root, "Other.md"); !strings.HasPrefix(text, "---\ntags: [x]\nweird: {a: [1, 2]}\nproject: Launch\nproject-id: ") ||
		!strings.HasSuffix(text, "---\nBody with %% comment %% and ==mark==\r\n") {
		t.Errorf("attached note %q", text)
	}

	got, err := a.ListProjectVaultNotes(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Other.md", "Projects/Launch/Kick off.md", "Typed.md"}; !slices.Equal(paths(got.Notes), want) {
		t.Errorf("members %v, want %v", paths(got.Notes), want)
	}
	if want := []string{"Card.md", "Mentions.md"}; !slices.Equal(paths(got.Linked), want) {
		t.Errorf("linked %v, want %v", paths(got.Linked), want)
	}

	// a card holds a note by path, and follows it when it moves.
	if err := a.AttachVaultNote("Elsewhere.md", "card-1"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readVault(t, root, "Elsewhere.md"), "project: Launch") {
		t.Error("a note in no project did not join the card's project")
	}
	if _, err := a.VaultMove("Elsewhere.md", "Moved/Elsewhere.md"); err != nil {
		t.Fatal(err)
	}
	if refs := noteLinkRefs(t, a, p.ID); !slices.Equal(refs, []string{"Moved/Elsewhere.md"}) {
		t.Errorf("after a move in Pelton %v", refs)
	}
	if err := os.Rename(filepath.Join(root, "Moved", "Elsewhere.md"), filepath.Join(root, "Renamed.md")); err != nil {
		t.Fatal(err)
	}
	if err := a.vaultService().Scan(a.ctx); err != nil {
		t.Fatal(err)
	}
	if refs := noteLinkRefs(t, a, p.ID); !slices.Equal(refs, []string{"Renamed.md"}) {
		t.Errorf("after a rename in another app %v", refs)
	}

	// renaming the project follows into the notes, the hand-typed one too.
	if _, err := a.UpdateProject(ProjectRequest{ID: p.ID, Name: "Relaunch"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(readVault(t, root, "Typed.md"), "project: Relaunch") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if text := readVault(t, root, "Typed.md"); text != "---\nproject: Relaunch\nproject-id: "+yamlID(p.UID)+"\n---\nby hand" {
		t.Errorf("hand-typed note after the rename %q", text)
	}

	// leaving the project keeps the file and the rest of its front matter.
	if err := a.RemoveNoteFromProject("Renamed.md", p.ID); err != nil {
		t.Fatal(err)
	}
	if text := readVault(t, root, "Renamed.md"); text != "nothing" {
		t.Errorf("after leaving %q", text)
	}
	if refs := noteLinkRefs(t, a, p.ID); len(refs) != 0 {
		t.Errorf("card links after leaving %v", refs)
	}

	// the trash takes the file, into the vault's .trash.
	if _, err := a.TrashProjectNote(made.Path, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".trash", "Projects", "Launch", "Kick off.md")); err != nil {
		t.Errorf("trashed note: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".obsidian")); err == nil {
		t.Error("something wrote into .obsidian")
	}
}

func yamlID(uid string) string {
	return strings.TrimSuffix(strings.TrimPrefix(notes.SetProperties("", []notes.Property{{Key: "k", Value: uid}}, nil), "---\nk: "), "\n---\n")
}

func mustColumn(t *testing.T, a *App, projectID int64) int64 {
	t.Helper()
	col := storage.ProjectColumn{ProjectID: projectID, Name: "To do"}
	if err := a.store.CreateProjectColumn(a.ctx, &col); err != nil {
		t.Fatal(err)
	}
	return col.ID
}
