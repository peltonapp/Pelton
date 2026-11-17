package storage

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
)

func newProject(t *testing.T, db *DB, name string, columns ...string) (Project, []ProjectColumn) {
	t.Helper()
	ctx := context.Background()
	p := Project{Name: name}
	if err := db.CreateProject(ctx, &p); err != nil {
		t.Fatal(err)
	}
	var cols []ProjectColumn
	for i, c := range columns {
		col := ProjectColumn{ProjectID: p.ID, Name: c, Done: i == len(columns)-1 && c == "Done"}
		if err := db.CreateProjectColumn(ctx, &col); err != nil {
			t.Fatal(err)
		}
		cols = append(cols, col)
	}
	return p, cols
}

func cardOrder(t *testing.T, db *DB, projectID, columnID int64) []string {
	t.Helper()
	cards, err := db.ListProjectCards(context.Background(), projectID)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, c := range cards {
		if c.ColumnID == columnID {
			out = append(out, c.TaskUID)
		}
	}
	return out
}

func TestProjectIconNodesRoundTrip(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	var version int
	if err := db.sql.QueryRowContext(ctx, `SELECT id FROM schema_migrations WHERE id = 97`).Scan(&version); err != nil {
		t.Fatalf("migration 97 not recorded: %v", err)
	}
	plain := Project{Name: "Plain", Icon: "rocket"}
	if err := db.CreateProject(ctx, &plain); err != nil {
		t.Fatal(err)
	}
	var isNull bool
	if err := db.sql.QueryRowContext(ctx, `SELECT icon_nodes IS NULL FROM projects WHERE id = ?`, plain.ID).Scan(&isNull); err != nil || !isNull {
		t.Errorf("icon_nodes of a project without geometry: null = %v, err = %v", isNull, err)
	}
	const nodes = `[["path","M5 4h4l3 3"]]`
	p := Project{Name: "Folder", Icon: "folder", IconNodes: nodes}
	if err := db.CreateProject(ctx, &p); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetProject(ctx, p.ID)
	if err != nil || got.IconNodes != nodes || got.Icon != "folder" {
		t.Fatalf("got %+v, %v", got, err)
	}
	got.IconNodes = ""
	if err := db.UpdateProject(ctx, *got); err != nil {
		t.Fatal(err)
	}
	list, err := db.ListProjects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range list {
		if q.IconNodes != "" {
			t.Errorf("%s kept geometry %q", q.Name, q.IconNodes)
		}
	}
}

func TestProjectLifecycleAndOrder(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	a, _ := newProject(t, db, "Alpha")
	b, _ := newProject(t, db, "Beta")
	c, _ := newProject(t, db, "Gamma")
	if a.Status != ProjectActive || a.CreatedAt == "" || a.Position != 0 || c.Position != 2 {
		t.Fatalf("created = %+v / %+v", a, c)
	}
	a.Description, a.Color, a.DueDate = "the first", "#3b6fd4", "2026-12-01"
	if err := db.UpdateProject(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := db.ReorderProjects(ctx, []int64{c.ID, a.ID}); err != nil {
		t.Fatal(err)
	}
	list, err := db.ListProjects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range list {
		names = append(names, p.Name)
	}
	if !slices.Equal(names, []string{"Gamma", "Alpha", "Beta"}) {
		t.Fatalf("order = %v", names)
	}
	if list[1].Description != "the first" || list[1].DueDate != "2026-12-01" {
		t.Errorf("update lost: %+v", list[1])
	}
	if err := db.SetProjectStatus(ctx, b.ID, ProjectArchived); err != nil {
		t.Fatal(err)
	}
	got, _ := db.GetProject(ctx, b.ID)
	if got.Status != ProjectArchived || got.ArchivedAt == "" {
		t.Errorf("archived = %+v", got)
	}
	if err := db.SetProjectStatus(ctx, b.ID, ProjectActive); err != nil {
		t.Fatal(err)
	}
	got, _ = db.GetProject(ctx, b.ID)
	if got.ArchivedAt != "" {
		t.Errorf("reactivated project kept its archive date: %+v", got)
	}
	if err := db.UpdateProject(ctx, Project{ID: 999, Name: "x"}); !errors.Is(err, ErrProjectNotFound) {
		t.Errorf("update of a missing project: %v", err)
	}
}

// Deleting a project takes its board, links and notes, never the tasks.
func TestDeleteProjectKeepsTasks(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	list := taskList(t, db)
	task := CalendarEvent{CalendarID: list.ID, UID: "t1", Data: "x", Kind: KindTask}
	if err := db.SaveCalendarEvent(ctx, &task); err != nil {
		t.Fatal(err)
	}
	p, cols := newProject(t, db, "Launch", "To do", "Done")
	if err := db.PlaceProjectCard(ctx, "t1", cols[0].ID, 0, 0); err != nil {
		t.Fatal(err)
	}
	link := ProjectLink{ProjectID: p.ID, Kind: LinkContact, Ref: "ada@example.com", Label: "Ada"}
	if err := db.AddProjectLink(ctx, &link); err != nil {
		t.Fatal(err)
	}
	note := ProjectNote{ProjectID: p.ID, Title: "Plan"}
	if err := db.CreateProjectNote(ctx, &note); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteProject(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetCalendarEvent(ctx, task.ID); err != nil {
		t.Errorf("the task went with the project: %v", err)
	}
	for _, table := range []string{"project_columns", "project_cards", "project_links", "project_notes"} {
		var n int
		if err := db.sql.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("%s kept %d rows", table, n)
		}
	}
}

func TestProjectColumns(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	p, cols := newProject(t, db, "Site", "To do", "Doing", "Done")
	if !cols[2].Done || cols[0].Done || cols[1].Position != 1 {
		t.Fatalf("columns = %+v", cols)
	}
	if err := db.SetProjectDoneColumn(ctx, p.ID, cols[1].ID); err != nil {
		t.Fatal(err)
	}
	got, _ := db.ListProjectColumns(ctx, p.ID)
	if got[1].Done != true || got[2].Done {
		t.Errorf("one done column at a time: %+v", got)
	}
	extra := ProjectColumn{ProjectID: p.ID, Name: "Review", Done: true}
	if err := db.CreateProjectColumn(ctx, &extra); err != nil {
		t.Fatal(err)
	}
	got, _ = db.ListProjectColumns(ctx, p.ID)
	if got[1].Done || !got[3].Done {
		t.Errorf("a new done column did not take over: %+v", got)
	}
	other, otherCols := newProject(t, db, "Other", "Inbox")
	if err := db.SetProjectDoneColumn(ctx, p.ID, otherCols[0].ID); !errors.Is(err, ErrProjectColumnNotFound) {
		t.Errorf("another project's column became done: %v", err)
	}
	_ = other
	if err := db.UpdateProjectColumn(ctx, ProjectColumn{ID: cols[0].ID, Name: "Backlog", WIPLimit: -3}); err != nil {
		t.Fatal(err)
	}
	if err := db.ReorderProjectColumns(ctx, p.ID, []int64{extra.ID, cols[0].ID}); err != nil {
		t.Fatal(err)
	}
	got, _ = db.ListProjectColumns(ctx, p.ID)
	var names []string
	for _, c := range got {
		names = append(names, c.Name)
	}
	if !slices.Equal(names, []string{"Review", "Backlog", "Doing", "Done"}) || got[1].WIPLimit != 0 {
		t.Errorf("columns = %+v", got)
	}
}

func TestDeleteColumnMovesOrDropsCards(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	p, cols := newProject(t, db, "Move", "A", "B", "C")
	for i, uid := range []string{"a1", "a2"} {
		if err := db.PlaceProjectCard(ctx, uid, cols[0].ID, i, 0); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.PlaceProjectCard(ctx, "b1", cols[1].ID, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := db.PlaceProjectCard(ctx, "c1", cols[2].ID, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteProjectColumn(ctx, cols[0].ID, cols[1].ID); err != nil {
		t.Fatal(err)
	}
	if got := cardOrder(t, db, p.ID, cols[1].ID); !slices.Equal(got, []string{"b1", "a1", "a2"}) {
		t.Errorf("moved cards = %v", got)
	}
	if err := db.DeleteProjectColumn(ctx, cols[2].ID, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetProjectCard(ctx, "c1"); !errors.Is(err, ErrProjectCardNotFound) {
		t.Errorf("a card of a dropped column stayed: %v", err)
	}
	_, otherCols := newProject(t, db, "Other", "X")
	if err := db.DeleteProjectColumn(ctx, cols[1].ID, otherCols[0].ID); !errors.Is(err, ErrProjectColumnNotFound) {
		t.Errorf("cards moved to another project's column: %v", err)
	}
}

func TestPlaceProjectCard(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	p, cols := newProject(t, db, "Order", "Todo", "Done")
	for _, uid := range []string{"a", "b", "c"} {
		if err := db.PlaceProjectCard(ctx, uid, cols[0].ID, 99, 0); err != nil {
			t.Fatal(err)
		}
	}
	if got := cardOrder(t, db, p.ID, cols[0].ID); !slices.Equal(got, []string{"a", "b", "c"}) {
		t.Fatalf("appended = %v", got)
	}
	// within the column.
	if err := db.PlaceProjectCard(ctx, "c", cols[0].ID, 0, 0); err != nil {
		t.Fatal(err)
	}
	if got := cardOrder(t, db, p.ID, cols[0].ID); !slices.Equal(got, []string{"c", "a", "b"}) {
		t.Errorf("moved up = %v", got)
	}
	// across columns, with where it came from.
	if err := db.PlaceProjectCard(ctx, "a", cols[1].ID, 0, cols[0].ID); err != nil {
		t.Fatal(err)
	}
	if got := cardOrder(t, db, p.ID, cols[0].ID); !slices.Equal(got, []string{"c", "b"}) {
		t.Errorf("source column = %v", got)
	}
	card, err := db.GetProjectCard(ctx, "a")
	if err != nil || card.ColumnID != cols[1].ID || card.PrevColumnID != cols[0].ID || card.Position != 0 {
		t.Errorf("card = %+v, %v", card, err)
	}
	cards, _ := db.ListProjectCards(ctx, p.ID)
	for _, c := range cards {
		if c.TaskUID == "b" && c.Position != 1 {
			t.Errorf("source column not renumbered: %+v", c)
		}
	}
	if err := db.PlaceProjectCard(ctx, "x", 999, 0, 0); !errors.Is(err, ErrProjectColumnNotFound) {
		t.Errorf("placed in a missing column: %v", err)
	}
}

// A task is on one board at most: placing it on another moves it, with the
// links its card holds.
func TestCardMovesBetweenProjects(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	p1, c1 := newProject(t, db, "One", "Todo")
	p2, c2 := newProject(t, db, "Two", "Todo")
	if err := db.PlaceProjectCard(ctx, "t", c1[0].ID, 0, 0); err != nil {
		t.Fatal(err)
	}
	link := ProjectLink{ProjectID: p1.ID, TaskUID: "t", Kind: LinkEvent, Ref: "ev-1", Label: "Kickoff"}
	if err := db.AddProjectLink(ctx, &link); err != nil {
		t.Fatal(err)
	}
	if err := db.PlaceProjectCard(ctx, "t", c2[0].ID, 0, 0); err != nil {
		t.Fatal(err)
	}
	if got := cardOrder(t, db, p1.ID, c1[0].ID); len(got) != 0 {
		t.Errorf("the card stayed on the first board: %v", got)
	}
	links, _ := db.ListCardLinks(ctx, "t")
	if len(links) != 1 || links[0].ProjectID != p2.ID {
		t.Errorf("card links = %+v", links)
	}
	if err := db.RemoveProjectCard(ctx, "t"); err != nil {
		t.Fatal(err)
	}
	if links, _ := db.ListCardLinks(ctx, "t"); len(links) != 0 {
		t.Errorf("card links outlived the card: %+v", links)
	}
	if err := db.RemoveProjectCard(ctx, "t"); !errors.Is(err, ErrProjectCardNotFound) {
		t.Errorf("second remove: %v", err)
	}
}

func TestCardSource(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	_, cols := newProject(t, db, "Src", "Todo")
	if err := db.PlaceProjectCard(ctx, "t", cols[0].ID, 0, 0); err != nil {
		t.Fatal(err)
	}
	card, _ := db.GetProjectCard(ctx, "t")
	if card.SourceKind != "" || card.SourceRef != "" {
		t.Fatalf("a new card has a source: %+v", card)
	}
	if err := db.SetProjectCardSource(ctx, "t", "tracker", "org/repo#12"); err != nil {
		t.Fatal(err)
	}
	card, _ = db.GetProjectCard(ctx, "t")
	if card.SourceKind != "tracker" || card.SourceRef != "org/repo#12" {
		t.Errorf("source = %+v", card)
	}
	if err := db.SetProjectCardSource(ctx, "t", "", ""); err != nil {
		t.Fatal(err)
	}
	var kind *string
	if err := db.sql.QueryRow(`SELECT source_kind FROM project_cards WHERE task_uid = 't'`).Scan(&kind); err != nil || kind != nil {
		t.Errorf("cleared source is %v, %v; want null", kind, err)
	}
}

func TestProjectTasksAndTasksByUID(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	list := taskList(t, db)
	other := Calendar{Name: "Other", Components: KindTask, Visible: true}
	if _, err := db.CreateCalendar(ctx, &other); err != nil {
		t.Fatal(err)
	}
	rows := []CalendarEvent{
		{CalendarID: list.ID, UID: "open", TaskDue: "2026-05-01"},
		{CalendarID: list.ID, UID: "done", TaskCompleted: "2026-04-01T10:00:00Z"},
		{CalendarID: other.ID, UID: "open"},
		{CalendarID: list.ID, UID: "gone", Pending: PendingDelete},
	}
	for i := range rows {
		rows[i].Kind, rows[i].Data = KindTask, "x"
		if err := db.SaveCalendarEvent(ctx, &rows[i]); err != nil {
			t.Fatal(err)
		}
	}
	p, cols := newProject(t, db, "Count", "Todo")
	for _, uid := range []string{"open", "done", "gone", "never"} {
		if err := db.PlaceProjectCard(ctx, uid, cols[0].ID, 9, 0); err != nil {
			t.Fatal(err)
		}
	}
	got, err := db.ProjectTasks(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].TaskUID != "done" || got[1].TaskUID != "open" || got[1].Due != "2026-05-01" {
		t.Errorf("project tasks = %+v", got)
	}
	tasks, err := db.TasksByUID(ctx, []string{"open", "gone", "done"})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 || tasks[0].UID != "open" || tasks[0].CalendarID != list.ID {
		t.Errorf("tasks by uid = %+v", tasks)
	}
}

func TestProjectLinksAndThreads(t *testing.T) {
	f := newThreadFixture(t)
	ctx := context.Background()
	db := f.db
	p, _ := newProject(t, db, "Mail")
	child := f.add(Message{MessageID: "c@x", Subject: "Re: trip", InReplyTo: []string{"p@x"}}, 5)
	other := f.add(Message{MessageID: "o@x", Subject: "Re: trip", InReplyTo: []string{"g@x"}}, 4)
	childThread, otherThread := f.thread(child), f.thread(other)

	account, ref, thread, subject, err := db.MessageThreadKey(ctx, other.ID)
	if err != nil || account != f.account || ref != "o@x" || thread != otherThread || subject != "Re: trip" {
		t.Fatalf("thread key = %d %q %d %q %v", account, ref, thread, subject, err)
	}
	link := ProjectLink{ProjectID: p.ID, Kind: LinkMail, AccountID: account, Ref: ref, ThreadID: thread, Label: "trip"}
	if err := db.AddProjectLink(ctx, &link); err != nil {
		t.Fatal(err)
	}
	again := ProjectLink{ProjectID: p.ID, Kind: LinkMail, AccountID: account, Ref: ref}
	if err := db.AddProjectLink(ctx, &again); err != nil || again.ID != link.ID || again.Label != "trip" {
		t.Fatalf("relinking = %+v, %v", again, err)
	}
	// a message with no Message-ID is linked by its conversation.
	bare := f.add(Message{Subject: "no id"}, 6)
	_, bareRef, bareThread, _, err := db.MessageThreadKey(ctx, bare.ID)
	if err != nil || bareRef == "" || bareRef[0] != '#' {
		t.Fatalf("bare ref = %q, %v", bareRef, err)
	}
	if err := db.AddProjectLink(ctx, &ProjectLink{ProjectID: p.ID, Kind: LinkMail, AccountID: f.account, Ref: bareRef, ThreadID: bareThread}); err != nil {
		t.Fatal(err)
	}
	threads, err := db.ProjectThreads(ctx, p.ID)
	if err != nil || len(threads) != 2 {
		t.Fatalf("threads = %+v, %v", threads, err)
	}

	// the parent links the two conversations; the project follows the merge.
	f.add(Message{MessageID: "p@x", Subject: "Re: trip", InReplyTo: []string{"g@x"}, References: []string{"g@x"}}, 3)
	merged := f.thread(other)
	if merged != f.thread(child) || (merged != childThread && merged != otherThread) {
		t.Fatalf("not merged: %d", merged)
	}
	threads, _ = db.ProjectThreads(ctx, 0)
	ids := []int64{}
	for _, th := range threads {
		ids = append(ids, th.ThreadID)
	}
	if !slices.Contains(ids, merged) || !slices.Contains(ids, bareThread) || len(ids) != 2 {
		t.Errorf("threads after the merge = %v, want %d and %d", ids, merged, bareThread)
	}

	latest, err := db.LatestThreadMessages(ctx, []int64{merged, bareThread}, []int64{f.inbox})
	if err != nil || len(latest) != 2 || latest[0].ID != bare.ID || latest[1].ID != child.ID {
		t.Errorf("latest = %+v, %v", latest, err)
	}

	if err := db.DeleteProjectThread(ctx, p.ID, merged); err != nil {
		t.Fatal(err)
	}
	threads, _ = db.ProjectThreads(ctx, p.ID)
	if len(threads) != 1 || threads[0].ThreadID != bareThread {
		t.Errorf("after removing the conversation = %+v", threads)
	}
	if id, err := db.ThreadOfHeader(ctx, f.account, "<c@x>"); err != nil || id != merged {
		t.Errorf("thread of header = %d, %v", id, err)
	}
}

func TestDismissalsFollowMerges(t *testing.T) {
	f := newThreadFixture(t)
	ctx := context.Background()
	child := f.add(Message{MessageID: "c@x", Subject: "Re: trip", InReplyTo: []string{"p@x"}}, 5)
	other := f.add(Message{MessageID: "o@x", Subject: "Re: trip", InReplyTo: []string{"g@x"}}, 4)
	if err := f.db.DismissProjectSuggestion(ctx, f.thread(child)); err != nil {
		t.Fatal(err)
	}
	if err := f.db.DismissProjectSuggestion(ctx, f.thread(other)); err != nil {
		t.Fatal(err)
	}
	f.add(Message{MessageID: "p@x", Subject: "Re: trip", InReplyTo: []string{"g@x"}, References: []string{"g@x"}}, 3)
	got, err := f.db.ListProjectDismissals(ctx)
	if err != nil || !slices.Equal(got, []int64{f.thread(child)}) {
		t.Errorf("dismissals = %v, %v", got, err)
	}
}

func TestProjectNotes(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	p, cols := newProject(t, db, "Notes", "Todo")
	n := ProjectNote{ProjectID: p.ID, Title: "Kickoff", Body: "# agenda"}
	if err := db.CreateProjectNote(ctx, &n); err != nil {
		t.Fatal(err)
	}
	second := ProjectNote{ProjectID: p.ID, Title: "Later"}
	if err := db.CreateProjectNote(ctx, &second); err != nil {
		t.Fatal(err)
	}
	n.Body = "# agenda\n- budget"
	if err := db.UpdateProjectNote(ctx, &n); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetProjectNote(ctx, n.ID)
	if err != nil || got.Body != n.Body {
		t.Fatalf("note = %+v, %v", got, err)
	}
	if err := db.PlaceProjectCard(ctx, "t", cols[0].ID, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := db.AddProjectLink(ctx, &ProjectLink{ProjectID: p.ID, TaskUID: "t", Kind: LinkNote, Ref: fmt.Sprint(n.ID)}); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteProjectNote(ctx, n.ID); err != nil {
		t.Fatal(err)
	}
	if links, _ := db.ListCardLinks(ctx, "t"); len(links) != 0 {
		t.Errorf("a link to a deleted note stayed: %+v", links)
	}
	notes, _ := db.ListProjectNotes(ctx, p.ID)
	if len(notes) != 1 || notes[0].ID != second.ID {
		t.Errorf("notes = %+v", notes)
	}
	if err := db.UpdateProjectNote(ctx, &ProjectNote{ID: 999}); !errors.Is(err, ErrProjectNoteNotFound) {
		t.Errorf("update of a missing note: %v", err)
	}
}
