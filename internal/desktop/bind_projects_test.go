package desktop

import (
	"bytes"
	"errors"
	"net/mail"
	"slices"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/outbox"
	"github.com/peltonapp/Pelton/internal/storage"
)

// newBoard makes a project with To do, Doing and Done, Done the done column.
func newBoard(t *testing.T, a *App, name string) (ProjectDTO, []ProjectColumnDTO) {
	t.Helper()
	p, err := a.CreateProject(ProjectRequest{Name: name, Columns: []ProjectColumnRequest{
		{Name: "To do"}, {Name: "Doing", WIPLimit: 2}, {Name: " "}, {Name: "Done", Done: true},
	}})
	if err != nil {
		t.Fatal(err)
	}
	board, err := a.GetProjectBoard(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	return p, board.Columns
}

func boardColumnOf(t *testing.T, a *App, projectID int64, uid string) int64 {
	t.Helper()
	board, err := a.GetProjectBoard(projectID)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range board.Cards {
		if c.Task.UID == uid {
			return c.ColumnID
		}
	}
	return 0
}

func TestProjectRequestChecks(t *testing.T) {
	a := newCalendarTestApp(t)
	for _, req := range []ProjectRequest{
		{Name: "  "},
		{Name: "x", Color: "blue"},
		{Name: "x", DueDate: "next week"},
	} {
		if _, err := a.CreateProject(req); err == nil {
			t.Errorf("%+v was taken", req)
		}
	}
	p, err := a.CreateProject(ProjectRequest{Name: " Launch ", Color: "#3B6FD4", StartDate: "2026-11-01", DueDate: "2026-12-24"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "Launch" || p.Status != storage.ProjectActive || p.Cards != 0 || p.People == nil {
		t.Errorf("created = %+v", p)
	}
	if err := a.SetProjectStatus(p.ID, "paused"); !errors.Is(err, errProjectStatus) {
		t.Errorf("an unknown status was taken: %v", err)
	}
}

func TestProjectBoardColumnsAndCards(t *testing.T) {
	a := newCalendarTestApp(t)
	p, cols := newBoard(t, a, "Site")
	if len(cols) != 3 || cols[1].WIPLimit != 2 || !cols[2].Done {
		t.Fatalf("columns = %+v", cols)
	}
	first, err := a.CreateProjectCard(ProjectCardRequest{ProjectID: p.ID, ColumnID: cols[0].ID, Title: "Draft copy", Priority: 1, Index: -1})
	if err != nil {
		t.Fatal(err)
	}
	second, err := a.CreateProjectCard(ProjectCardRequest{ProjectID: p.ID, ColumnID: cols[0].ID, Title: "Pick fonts", Index: 0})
	if err != nil {
		t.Fatal(err)
	}
	shipped, err := a.CreateProjectCard(ProjectCardRequest{ProjectID: p.ID, ColumnID: cols[2].ID, Title: "Buy domain", Index: -1})
	if err != nil {
		t.Fatal(err)
	}
	if !shipped.Task.Done {
		t.Errorf("a card made in the done column is open: %+v", shipped.Task)
	}
	board, err := a.GetProjectBoard(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, c := range board.Cards {
		titles = append(titles, c.Task.Title)
	}
	if !slices.Equal(titles, []string{"Pick fonts", "Draft copy", "Buy domain"}) {
		t.Errorf("board = %v", titles)
	}
	if board.Project.Cards != 3 || board.Project.Done != 1 {
		t.Errorf("counts = %+v", board.Project)
	}
	// the tasks are ordinary tasks in the default list.
	tasks, err := a.ListTasks()
	if err != nil || len(tasks) != 3 {
		t.Fatalf("tasks = %+v, %v", tasks, err)
	}
	if first.Task.Priority != 1 || second.Position != 0 {
		t.Errorf("cards = %+v / %+v", first, second)
	}
	if _, err := a.CreateProjectCard(ProjectCardRequest{ProjectID: p.ID + 1, ColumnID: cols[0].ID, Title: "x"}); err == nil {
		t.Error("a card went on a missing project")
	}
}

// The done column and the task's completion follow each other.
func TestDoneColumnFollowsTasks(t *testing.T) {
	a := newCalendarTestApp(t)
	p, cols := newBoard(t, a, "Done")
	card, err := a.CreateProjectCard(ProjectCardRequest{ProjectID: p.ID, ColumnID: cols[1].ID, Title: "Write", Index: -1})
	if err != nil {
		t.Fatal(err)
	}
	uid := card.Task.UID

	moved, err := a.MoveProjectCard(uid, cols[2].ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !moved.Card.Task.Done || moved.Card.ColumnID != cols[2].ID {
		t.Fatalf("dropped in done = %+v", moved)
	}
	if _, err := a.MoveProjectCard(uid, cols[0].ID, 0); err != nil {
		t.Fatal(err)
	}
	task, _ := a.GetTask(card.Task.ID)
	if task.Done {
		t.Error("moving out of the done column left the task completed")
	}

	// completed in the Tasks screen: the board moves it to done.
	if _, err := a.SetTaskDone(card.Task.ID, true); err != nil {
		t.Fatal(err)
	}
	if got := boardColumnOf(t, a, p.ID, uid); got != cols[2].ID {
		t.Errorf("a completed task stayed in column %d", got)
	}
	// opened again: back where it came from.
	if _, err := a.SetTaskDone(card.Task.ID, false); err != nil {
		t.Fatal(err)
	}
	if got := boardColumnOf(t, a, p.ID, uid); got != cols[0].ID {
		t.Errorf("a reopened task went to column %d, want %d", got, cols[0].ID)
	}
}

func TestRepeatingCardMovesOnInsteadOfDone(t *testing.T) {
	a := newCalendarTestApp(t)
	p, cols := newBoard(t, a, "Chores")
	list := localTaskList(t, a)
	saved, err := a.CreateTask(TaskRequest{ListID: list, Title: "Water plants", Due: "2026-11-16", RRule: "FREQ=WEEKLY"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddTaskToProject(p.ID, saved.Task.UID, 0); err != nil {
		t.Fatal(err)
	}
	if got := boardColumnOf(t, a, p.ID, saved.Task.UID); got != cols[0].ID {
		t.Fatalf("an open task was added to column %d", got)
	}
	moved, err := a.MoveProjectCard(saved.Task.UID, cols[2].ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !moved.Advanced || moved.Card.Task.Done || moved.Card.ColumnID != cols[0].ID || moved.Card.Task.Due != "2026-11-23" {
		t.Errorf("repeating card = %+v", moved)
	}
}

func TestAddTaskToAnotherProjectMovesIt(t *testing.T) {
	a := newCalendarTestApp(t)
	p1, cols1 := newBoard(t, a, "One")
	p2, _ := newBoard(t, a, "Two")
	card, err := a.CreateProjectCard(ProjectCardRequest{ProjectID: p1.ID, ColumnID: cols1[0].ID, Title: "Shared", Index: -1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddTaskToProject(p2.ID, card.Task.UID, cols1[0].ID); !errors.Is(err, errProjectOtherBoard) {
		t.Errorf("a column of another board was taken: %v", err)
	}
	if _, err := a.AddTaskToProject(p2.ID, card.Task.UID, 0); err != nil {
		t.Fatal(err)
	}
	if got, _ := a.ProjectOfTask(card.Task.UID); got != p2.ID {
		t.Errorf("task is on project %d, want %d", got, p2.ID)
	}
	b1, _ := a.GetProjectBoard(p1.ID)
	if len(b1.Cards) != 0 {
		t.Errorf("the first board kept the card: %+v", b1.Cards)
	}
	if _, err := a.MoveProjectCard(card.Task.UID, cols1[1].ID, 0); !errors.Is(err, errProjectOtherBoard) {
		t.Errorf("moved to another board's column: %v", err)
	}
}

// Deleting a column or a project, or removing a card, never deletes a task.
func TestRemovingBoardsKeepsTasks(t *testing.T) {
	a := newCalendarTestApp(t)
	p, cols := newBoard(t, a, "Keep")
	for _, title := range []string{"a", "b"} {
		if _, err := a.CreateProjectCard(ProjectCardRequest{ProjectID: p.ID, ColumnID: cols[0].ID, Title: title, Index: -1}); err != nil {
			t.Fatal(err)
		}
	}
	c, err := a.CreateProjectCard(ProjectCardRequest{ProjectID: p.ID, ColumnID: cols[1].ID, Title: "c", Index: -1})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.DeleteProjectColumn(cols[0].ID, cols[1].ID); err != nil {
		t.Fatal(err)
	}
	board, _ := a.GetProjectBoard(p.ID)
	if len(board.Columns) != 2 || len(board.Cards) != 3 || board.Cards[0].Task.Title != "c" {
		t.Fatalf("after moving the column's cards = %+v", board)
	}
	if err := a.DeleteProjectColumn(cols[1].ID, 0); err != nil {
		t.Fatal(err)
	}
	board, _ = a.GetProjectBoard(p.ID)
	if len(board.Cards) != 0 {
		t.Errorf("dropped column kept cards: %+v", board.Cards)
	}
	if err := a.RemoveProjectCard(c.Task.UID); !errors.Is(err, storage.ErrProjectCardNotFound) {
		t.Errorf("remove of a card no longer on the board: %v", err)
	}
	if err := a.DeleteProject(p.ID); err != nil {
		t.Fatal(err)
	}
	tasks, _ := a.ListTasks()
	if len(tasks) != 3 {
		t.Errorf("tasks after deleting the project = %d, want 3", len(tasks))
	}
	if list, _ := a.ListProjects(); len(list) != 0 {
		t.Errorf("projects = %+v", list)
	}
}

func TestProjectStatsAndPeople(t *testing.T) {
	a := newCalendarTestApp(t)
	p, cols := newBoard(t, a, "Stats")
	yesterday := time.Now().AddDate(0, 0, -1).Format(dateLayout)
	nextWeek := time.Now().AddDate(0, 0, 7).Format(dateLayout)
	for _, due := range []string{nextWeek, yesterday, ""} {
		if _, err := a.CreateProjectCard(ProjectCardRequest{ProjectID: p.ID, ColumnID: cols[0].ID, Title: "t", Due: due, Index: -1}); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.AddProjectPerson(p.ID, "Ada <ADA@example.com>", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if err := a.AddProjectPerson(p.ID, "not an address", ""); !errors.Is(err, errProjectPerson) {
		t.Errorf("a bad address was taken: %v", err)
	}
	if err := a.SetEventProject("ev-1", "Kickoff", p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateProjectNote(p.ID, "Plan", "# plan"); err != nil {
		t.Fatal(err)
	}
	list, err := a.ListProjects()
	if err != nil || len(list) != 1 {
		t.Fatalf("projects = %+v, %v", list, err)
	}
	got := list[0]
	if got.Cards != 3 || got.Overdue != 1 || got.NextDue != yesterday || got.Events != 1 || got.Notes != 1 {
		t.Errorf("stats = %+v", got)
	}
	if len(got.People) != 1 || got.People[0].Email != "ada@example.com" || got.People[0].Name != "Ada Lovelace" {
		t.Errorf("people = %+v", got.People)
	}
	if err := a.RemoveProjectPerson(p.ID, "ada@example.com"); err != nil {
		t.Fatal(err)
	}
	if got, _ := a.GetProject(p.ID); len(got.People) != 0 {
		t.Errorf("person stayed: %+v", got.People)
	}
}

func TestEventProject(t *testing.T) {
	a := newCalendarTestApp(t)
	cal := localCalendar(t, a)
	start := time.Now().Add(48 * time.Hour).Truncate(time.Hour)
	ev, err := a.CreateEvent(EventRequest{CalendarID: cal, Summary: "Review", Start: start.Format(time.RFC3339), End: start.Add(time.Hour).Format(time.RFC3339)})
	if err != nil {
		t.Fatal(err)
	}
	p1, _ := newBoard(t, a, "One")
	p2, _ := newBoard(t, a, "Two")
	if err := a.SetEventProject(ev.Event.UID, "Review", p1.ID); err != nil {
		t.Fatal(err)
	}
	if err := a.SetEventProject(ev.Event.UID, "Review", p2.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := a.ProjectsOfEvent(ev.Event.UID); !slices.Equal(got, []int64{p2.ID}) {
		t.Errorf("event projects = %v", got)
	}
	if err := a.SetEventProject("gone-uid", "Old meeting", p2.ID); err != nil {
		t.Fatal(err)
	}
	events, err := a.ListProjectEvents(p2.ID)
	if err != nil || len(events) != 2 {
		t.Fatalf("events = %+v, %v", events, err)
	}
	if !events[0].Found || events[0].Summary != "Review" || events[0].Past || events[0].EventID != ev.Event.ID {
		t.Errorf("found event = %+v", events[0])
	}
	if events[1].Found || events[1].Label != "Old meeting" {
		t.Errorf("missing event = %+v", events[1])
	}
	if err := a.SetEventProject(ev.Event.UID, "", 0); err != nil {
		t.Fatal(err)
	}
	if got, _ := a.ProjectsOfEvent(ev.Event.UID); len(got) != 0 {
		t.Errorf("event still filed: %v", got)
	}
}

func TestProjectNotesAndCardLinks(t *testing.T) {
	a := newCalendarTestApp(t)
	p, cols := newBoard(t, a, "Notes")
	other, _ := newBoard(t, a, "Other")
	card, err := a.CreateProjectCard(ProjectCardRequest{ProjectID: p.ID, ColumnID: cols[0].ID, Title: "t", Index: -1})
	if err != nil {
		t.Fatal(err)
	}
	note, err := a.CreateProjectNote(p.ID, " Spec ", "body")
	if err != nil || note.Title != "Spec" {
		t.Fatalf("note = %+v, %v", note, err)
	}
	foreign, _ := a.CreateProjectNote(other.ID, "x", "")
	if err := a.AttachProjectNote(foreign.ID, card.Task.UID); !errors.Is(err, errProjectOtherBoard) {
		t.Errorf("another project's note was attached: %v", err)
	}
	if err := a.AttachProjectNote(note.ID, card.Task.UID); err != nil {
		t.Fatal(err)
	}
	if err := a.LinkEventToCard(card.Task.UID, "ev-9", "Demo"); err != nil {
		t.Fatal(err)
	}
	links, err := a.ListCardLinks(card.Task.UID)
	if err != nil || len(links) != 2 || links[0].Kind != storage.LinkNote || links[0].Label != "Spec" {
		t.Fatalf("card links = %+v, %v", links, err)
	}
	board, _ := a.GetProjectBoard(p.ID)
	if board.Cards[0].Notes != 1 || board.Cards[0].Events != 1 {
		t.Errorf("card counts = %+v", board.Cards[0])
	}
	if err := a.RemoveProjectLink(links[1].ID); err != nil {
		t.Fatal(err)
	}
	updated, err := a.UpdateProjectNote(note.ID, "Spec v2", "new body")
	if err != nil || updated.Body != "new body" {
		t.Fatalf("updated = %+v, %v", updated, err)
	}
	if err := a.DeleteProjectNote(note.ID); err != nil {
		t.Fatal(err)
	}
	if links, _ := a.ListCardLinks(card.Task.UID); len(links) != 0 {
		t.Errorf("links after deleting the note = %+v", links)
	}
}

func TestProjectMail(t *testing.T) {
	b := newThreadTestMailbox(t)
	b.app.queue = outbox.NewQueue(b.db)
	a := b.app
	p, cols := newBoard(t, a, "Mail")
	first := b.add(t, b.inbox, storage.Message{MessageID: "a@x", Subject: "plans", FromAddress: "Alice <alice@x>"}, 0)
	reply := b.add(t, b.inbox, storage.Message{MessageID: "b@x", Subject: "Re: plans", FromAddress: "bob@x", InReplyTo: []string{"a@x"}}, 1)
	lone := b.add(t, b.inbox, storage.Message{MessageID: "c@x", Subject: "lunch", FromAddress: "carol@x"}, 2)

	added, err := a.AddMailToProject(p.ID, []int64{first, reply}, "")
	if err != nil || added != 1 {
		t.Fatalf("added = %d, %v; the conversation should count once", added, err)
	}
	card, err := a.CreateProjectCard(ProjectCardRequest{ProjectID: p.ID, ColumnID: cols[0].ID, Title: "answer carol", Index: -1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddMailToProject(p.ID, []int64{lone}, card.Task.UID); err != nil {
		t.Fatal(err)
	}
	threads, err := a.ListProjectThreads(p.ID)
	if err != nil || len(threads) != 2 {
		t.Fatalf("threads = %+v, %v", threads, err)
	}
	if threads[0].Head.ID != lone || threads[1].Head.ID != reply || threads[1].Count != 2 {
		t.Errorf("threads = %+v", threads)
	}
	marks, _ := a.ListProjectMailMarks()
	if len(marks) != 2 {
		t.Errorf("marks = %+v", marks)
	}
	board, _ := a.GetProjectBoard(p.ID)
	if board.Cards[0].Mail != 1 || board.Project.Mail != 2 {
		t.Errorf("mail counts: card %d, project %d", board.Cards[0].Mail, board.Project.Mail)
	}

	// a reply sent with the project picked is filed under it, and so is the
	// answer that comes back to it.
	other, _ := newBoard(t, a, "Other")
	if _, err := a.SendMessage(ComposeRequest{AccountID: b.account, To: []AddressDTO{{Email: "dan@x"}}, Subject: "budget", Text: "numbers", ProjectID: other.ID}); err != nil {
		t.Fatal(err)
	}
	queued, err := a.queue.List(a.ctx)
	if err != nil || len(queued) != 1 {
		t.Fatalf("queue = %+v, %v", queued, err)
	}
	parsed, err := mail.ReadMessage(bytes.NewReader(queued[0].Raw))
	if err != nil {
		t.Fatal(err)
	}
	sentID := parsed.Header.Get("Message-ID")
	if threads, _ := a.ListProjectThreads(other.ID); len(threads) != 0 {
		t.Errorf("listed before the sent copy arrived: %+v", threads)
	}
	b.add(t, b.sent, storage.Message{MessageID: sentID, Subject: "budget", FromAddress: "me@example.com"}, 3)
	answer := b.add(t, b.inbox, storage.Message{MessageID: "d@x", Subject: "Re: budget", FromAddress: "dan@x", InReplyTo: []string{sentID}}, 4)
	threads, _ = a.ListProjectThreads(other.ID)
	if len(threads) != 1 || threads[0].Head.ID != answer || threads[0].Count != 2 {
		t.Errorf("sent conversation = %+v", threads)
	}

	if err := a.RemoveProjectThread(p.ID, threads[0].ThreadID); err != nil {
		t.Fatal(err)
	}
	if err := a.RemoveProjectThread(p.ID, mustThread(t, b.db, lone)); err != nil {
		t.Fatal(err)
	}
	if left, _ := a.ListProjectThreads(p.ID); len(left) != 1 || left[0].Head.ID != reply {
		t.Errorf("after removing = %+v", left)
	}
	if _, err := b.db.GetMessage(a.ctx, lone); err != nil {
		t.Errorf("removing from the project touched the mail: %v", err)
	}

	thread := mustThread(t, b.db, first)
	if err := a.DismissProjectSuggestion(thread); err != nil {
		t.Fatal(err)
	}
	if got, _ := a.ListProjectDismissals(); !slices.Equal(got, []int64{thread}) {
		t.Errorf("dismissals = %v", got)
	}
}

func TestReorderProjectsAndColumns(t *testing.T) {
	a := newCalendarTestApp(t)
	p1, cols := newBoard(t, a, "One")
	p2, _ := newBoard(t, a, "Two")
	if err := a.ReorderProjects([]int64{p2.ID, p1.ID}); err != nil {
		t.Fatal(err)
	}
	list, _ := a.ListProjects()
	if list[0].ID != p2.ID {
		t.Errorf("order = %+v", list)
	}
	if err := a.ReorderProjectColumns(p1.ID, []int64{cols[2].ID, cols[0].ID, cols[1].ID}); err != nil {
		t.Fatal(err)
	}
	if err := a.UpdateProjectColumn(cols[0].ID, "Backlog", 5); err != nil {
		t.Fatal(err)
	}
	if err := a.UpdateProjectColumn(cols[0].ID, " ", 5); !errors.Is(err, errProjectColumnName) {
		t.Errorf("an empty column name was taken: %v", err)
	}
	if err := a.SetProjectDoneColumn(p1.ID, 0); err != nil {
		t.Fatal(err)
	}
	added, err := a.AddProjectColumn(p1.ID, "Later")
	if err != nil {
		t.Fatal(err)
	}
	board, _ := a.GetProjectBoard(p1.ID)
	var names []string
	for _, c := range board.Columns {
		names = append(names, c.Name)
		if c.Done {
			t.Errorf("a done column is left: %+v", c)
		}
	}
	if !slices.Equal(names, []string{"Done", "Backlog", "Doing", "Later"}) || added.Position != 3 || board.Columns[1].WIPLimit != 5 {
		t.Errorf("columns = %+v", board.Columns)
	}
}
