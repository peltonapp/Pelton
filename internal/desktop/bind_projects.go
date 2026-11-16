// bind_projects.go is the Projects space: projects with a board of cards, and
// the mail, events, people and notes that belong to them.
//
// A card is a task from the task lists. Which board and column it is on is
// kept here, keyed by the task's UID, and never written into the VTODO, so a
// task syncs with its CalDAV server exactly as before and other apps never
// see the board. A board's done column and the task's completion follow each
// other: a card dropped there completes its task, and a task completed
// anywhere else moves to it the next time the board is read.
package desktop

import (
	"errors"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/ical"
	"github.com/peltonapp/Pelton/internal/storage"
)

var (
	errProjectName       = errors.New("pelton: a project needs a name")
	errProjectStatus     = errors.New("pelton: a project is active or archived")
	errProjectDate       = errors.New("pelton: a project date is written 2006-01-02")
	errProjectColor      = errors.New("pelton: a project color is written #rrggbb")
	errProjectColumnName = errors.New("pelton: a column needs a name")
	errProjectOtherBoard = errors.New("pelton: that column is on another board")
	errProjectTaskGone   = errors.New("pelton: the task of this card is gone")
	errProjectPerson     = errors.New("pelton: a person needs an address")
)

var projectColorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// ProjectPersonDTO is someone a project is with: a contact or an address.
type ProjectPersonDTO struct {
	Email string `json:"email"`
	Name  string `json:"name"`
}

// ProjectDTO is one project with the counts its tile shows. Cards counts the
// cards whose task still exists and Done those completed; NextDue is the
// earliest due date of an open card, a date or rfc3339, empty for none, and
// Overdue how many open cards are past theirs. Mail counts conversations. UID
// is what notes and pelton://project links name the project by.
type ProjectDTO struct {
	ID          int64              `json:"id"`
	UID         string             `json:"uid"`
	Name        string             `json:"name"`
	Color       string             `json:"color"`
	Icon        string             `json:"icon"`
	Description string             `json:"description"`
	Status      string             `json:"status"`
	StartDate   string             `json:"startDate"`
	DueDate     string             `json:"dueDate"`
	Position    int                `json:"position"`
	ListID      int64              `json:"listId"`
	CreatedAt   string             `json:"createdAt"`
	UpdatedAt   string             `json:"updatedAt"`
	ArchivedAt  string             `json:"archivedAt"`
	Cards       int                `json:"cards"`
	Done        int                `json:"done"`
	Overdue     int                `json:"overdue"`
	NextDue     string             `json:"nextDue"`
	Mail        int                `json:"mail"`
	Events      int                `json:"events"`
	Notes       int                `json:"notes"`
	People      []ProjectPersonDTO `json:"people"`
}

// ProjectColumnRequest is a column to make with a new project.
type ProjectColumnRequest struct {
	Name     string `json:"name"`
	WIPLimit int    `json:"wipLimit"`
	Done     bool   `json:"done"`
}

// ProjectRequest is the project editor's project. An ID of 0 creates one,
// with Columns as its board; an update leaves the board alone. ListID is the
// task list new cards go to, 0 for the default.
type ProjectRequest struct {
	ID          int64                  `json:"id"`
	Name        string                 `json:"name"`
	Color       string                 `json:"color"`
	Icon        string                 `json:"icon"`
	Description string                 `json:"description"`
	StartDate   string                 `json:"startDate"`
	DueDate     string                 `json:"dueDate"`
	ListID      int64                  `json:"listId"`
	Columns     []ProjectColumnRequest `json:"columns"`
}

// ProjectColumnDTO is one column of a board.
type ProjectColumnDTO struct {
	ID        int64  `json:"id"`
	ProjectID int64  `json:"projectId"`
	Name      string `json:"name"`
	Position  int    `json:"position"`
	WIPLimit  int    `json:"wipLimit"`
	Done      bool   `json:"done"`
}

// ProjectCardDTO is a card: its task, where it sits, and how much is attached
// to it. Mail, Events and Notes count the card's own links plus the mail and
// events the task links to itself. SourceKind and SourceRef are empty for a
// card made in Pelton; they name the outside item a mirrored card stands for.
type ProjectCardDTO struct {
	Task       TaskDTO `json:"task"`
	ProjectID  int64   `json:"projectId"`
	ColumnID   int64   `json:"columnId"`
	Position   int     `json:"position"`
	Mail       int     `json:"mail"`
	Events     int     `json:"events"`
	Notes      int     `json:"notes"`
	SourceKind string  `json:"sourceKind"`
	SourceRef  string  `json:"sourceRef"`
}

// ProjectBoardDTO is a project's board: its columns in order and its cards,
// by column and position.
type ProjectBoardDTO struct {
	Project ProjectDTO         `json:"project"`
	Columns []ProjectColumnDTO `json:"columns"`
	Cards   []ProjectCardDTO   `json:"cards"`
}

// ProjectCardRequest makes a new task and puts it on a board. Index is where
// in the column, -1 for the end. A card made in the done column is made
// completed.
type ProjectCardRequest struct {
	ProjectID int64  `json:"projectId"`
	ColumnID  int64  `json:"columnId"`
	Title     string `json:"title"`
	Notes     string `json:"notes"`
	Due       string `json:"due"`
	TimeZone  string `json:"timeZone"`
	Priority  int    `json:"priority"`
	Index     int    `json:"index"`
}

// ProjectMoveResultDTO is a card after a move. Advanced is true when the move
// to the done column completed a repeating task, which moved on to its next
// occurrence and so stayed where it was.
type ProjectMoveResultDTO struct {
	Card     ProjectCardDTO `json:"card"`
	Advanced bool           `json:"advanced"`
}

// ProjectLinkDTO is something a project or a card points at. TaskUID is empty
// for the project's own links.
type ProjectLinkDTO struct {
	ID        int64  `json:"id"`
	ProjectID int64  `json:"projectId"`
	TaskUID   string `json:"taskUid"`
	Kind      string `json:"kind"`
	AccountID int64  `json:"accountId"`
	ThreadID  int64  `json:"threadId"`
	Ref       string `json:"ref"`
	Label     string `json:"label"`
	CreatedAt string `json:"createdAt"`
}

// ProjectEventDTO is an event linked to a project, at its next occurrence
// from now, or its last one when it has none to come. Found is false when no
// calendar has it any more; Label then names what it was.
type ProjectEventDTO struct {
	UID           string `json:"uid"`
	TaskUID       string `json:"taskUid"`
	Label         string `json:"label"`
	Found         bool   `json:"found"`
	EventID       int64  `json:"eventId"`
	CalendarColor string `json:"calendarColor"`
	Summary       string `json:"summary"`
	Start         string `json:"start"`
	End           string `json:"end"`
	AllDay        bool   `json:"allDay"`
	Recurring     bool   `json:"recurring"`
	RecurrenceID  string `json:"recurrenceId"`
	Past          bool   `json:"past"`
}

// ProjectNoteDTO is a note kept with a project, its body Markdown.
type ProjectNoteDTO struct {
	ID        int64  `json:"id"`
	ProjectID int64  `json:"projectId"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

// ProjectMailMarkDTO says a conversation belongs to a project.
type ProjectMailMarkDTO struct {
	ThreadID  int64 `json:"threadId"`
	ProjectID int64 `json:"projectId"`
}

// --- projects ------------------------------------------------------------------

// ListProjects returns every project in its manual order, archived ones
// included, with the counts their tiles show.
func (a *App) ListProjects() ([]ProjectDTO, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	projects, err := a.store.ListProjects(a.ctx)
	if err != nil {
		return nil, err
	}
	stats, err := a.projectStats(0)
	if err != nil {
		return nil, err
	}
	out := make([]ProjectDTO, 0, len(projects))
	for _, p := range projects {
		out = append(out, projectDTO(p, stats[p.ID]))
	}
	return out, nil
}

// GetProject returns one project with its counts.
func (a *App) GetProject(id int64) (ProjectDTO, error) {
	if err := a.ready(); err != nil {
		return ProjectDTO{}, err
	}
	return a.projectWithStats(id)
}

// CreateProject makes a project with the board its request names. Columns
// without a name are left out.
func (a *App) CreateProject(req ProjectRequest) (ProjectDTO, error) {
	if err := a.ready(); err != nil {
		return ProjectDTO{}, err
	}
	p, err := projectFromRequest(req)
	if err != nil {
		return ProjectDTO{}, err
	}
	if err := a.store.CreateProject(a.ctx, &p); err != nil {
		return ProjectDTO{}, err
	}
	for _, c := range req.Columns {
		name := strings.TrimSpace(c.Name)
		if name == "" {
			continue
		}
		col := storage.ProjectColumn{ProjectID: p.ID, Name: name, WIPLimit: c.WIPLimit, Done: c.Done}
		if err := a.store.CreateProjectColumn(a.ctx, &col); err != nil {
			return ProjectDTO{}, err
		}
	}
	a.emitProjects(p.ID)
	return a.projectWithStats(p.ID)
}

// UpdateProject saves a project's name, look, description, dates and task
// list. A new name is written into the front matter of the project's notes
// in the background.
func (a *App) UpdateProject(req ProjectRequest) (ProjectDTO, error) {
	if err := a.ready(); err != nil {
		return ProjectDTO{}, err
	}
	p, err := projectFromRequest(req)
	if err != nil {
		return ProjectDTO{}, err
	}
	before, err := a.store.GetProject(a.ctx, p.ID)
	if err != nil {
		return ProjectDTO{}, err
	}
	if err := a.store.UpdateProject(a.ctx, p); err != nil {
		return ProjectDTO{}, err
	}
	if before.Name != p.Name {
		after := *before
		after.Name = p.Name
		goSafe("renaming a project in its notes", func() { a.renameProjectNotes(before, &after) })
	}
	a.emitProjects(p.ID)
	return a.projectWithStats(p.ID)
}

// SetProjectStatus archives a project or makes it active again.
func (a *App) SetProjectStatus(id int64, status string) error {
	if err := a.ready(); err != nil {
		return err
	}
	if status != storage.ProjectActive && status != storage.ProjectArchived {
		return errProjectStatus
	}
	if err := a.store.SetProjectStatus(a.ctx, id, status); err != nil {
		return err
	}
	a.emitProjects(id)
	return nil
}

// DeleteProject removes a project with its board, its links and its notes.
// The tasks on the board, and the mail, events and contacts it pointed at,
// stay where they are.
func (a *App) DeleteProject(id int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	if err := a.store.DeleteProject(a.ctx, id); err != nil {
		return err
	}
	a.emitProjects(id)
	return nil
}

// ReorderProjects gives the projects the order of ids.
func (a *App) ReorderProjects(ids []int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	if err := a.store.ReorderProjects(a.ctx, ids); err != nil {
		return err
	}
	a.emitProjects()
	return nil
}

// --- board ---------------------------------------------------------------------

// GetProjectBoard returns a project's board. Cards whose task is gone are left
// out but kept, so a task brought back by undo returns to its place. Before
// reading, cards follow their tasks into and out of the done column.
func (a *App) GetProjectBoard(id int64) (ProjectBoardDTO, error) {
	if err := a.ready(); err != nil {
		return ProjectBoardDTO{}, err
	}
	project, err := a.projectWithStats(id)
	if err != nil {
		return ProjectBoardDTO{}, err
	}
	columns, err := a.store.ListProjectColumns(a.ctx, id)
	if err != nil {
		return ProjectBoardDTO{}, err
	}
	cards, tasks, err := a.boardCards(id)
	if err != nil {
		return ProjectBoardDTO{}, err
	}
	if a.followDone(columns, cards, tasks) {
		if cards, tasks, err = a.boardCards(id); err != nil {
			return ProjectBoardDTO{}, err
		}
	}
	counts, err := a.cardLinkCounts(id)
	if err != nil {
		return ProjectBoardDTO{}, err
	}
	out := ProjectBoardDTO{Project: project, Columns: make([]ProjectColumnDTO, 0, len(columns)), Cards: []ProjectCardDTO{}}
	for _, c := range columns {
		out.Columns = append(out.Columns, columnDTO(c))
	}
	for _, c := range cards {
		task, ok := tasks[c.TaskUID]
		if !ok {
			continue
		}
		out.Cards = append(out.Cards, cardDTO(c, task, counts[c.TaskUID]))
	}
	return out, nil
}

// boardCards reads a board's cards and the tasks they stand for, by UID.
func (a *App) boardCards(projectID int64) ([]storage.ProjectCard, map[string]TaskDTO, error) {
	cards, err := a.store.ListProjectCards(a.ctx, projectID)
	if err != nil {
		return nil, nil, err
	}
	uids := make([]string, 0, len(cards))
	for _, c := range cards {
		uids = append(uids, c.TaskUID)
	}
	tasks, err := a.tasksByUID(uids)
	return cards, tasks, err
}

// tasksByUID reads and parses the tasks with these UIDs.
func (a *App) tasksByUID(uids []string) (map[string]TaskDTO, error) {
	out := map[string]TaskDTO{}
	if len(uids) == 0 {
		return out, nil
	}
	rows, err := a.store.TasksByUID(a.ctx, uids)
	if err != nil {
		return nil, err
	}
	cals, err := a.calendarIndex(a.ctx)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		obj, err := ical.ParseTodoObject([]byte(row.Data), time.Local)
		if err != nil {
			a.log.Warn("unreadable task on a board", "task", row.ID, "err", err)
			continue
		}
		out[row.UID] = taskDTO(row, cals[row.CalendarID], obj)
	}
	return out, nil
}

// followDone moves cards whose task was completed elsewhere to the done
// column, and cards in the done column whose task is open again back to where
// they came from. It reports whether anything moved.
func (a *App) followDone(columns []storage.ProjectColumn, cards []storage.ProjectCard, tasks map[string]TaskDTO) bool {
	var done *storage.ProjectColumn
	for i := range columns {
		if columns[i].Done {
			done = &columns[i]
		}
	}
	if done == nil {
		return false
	}
	back := func(c storage.ProjectCard) int64 {
		for _, col := range columns {
			if col.ID == c.PrevColumnID && !col.Done {
				return col.ID
			}
		}
		for _, col := range columns {
			if !col.Done {
				return col.ID
			}
		}
		return 0
	}
	moved := false
	for _, c := range cards {
		task, ok := tasks[c.TaskUID]
		if !ok {
			continue
		}
		switch {
		case task.Done && c.ColumnID != done.ID:
			if err := a.store.PlaceProjectCard(a.ctx, c.TaskUID, done.ID, 0, c.ColumnID); err != nil {
				a.log.Warn("move a completed card", "task", c.TaskUID, "err", err)
				continue
			}
			moved = true
		case !task.Done && c.ColumnID == done.ID:
			to := back(c)
			if to == 0 {
				continue
			}
			if err := a.store.PlaceProjectCard(a.ctx, c.TaskUID, to, 1<<30, 0); err != nil {
				a.log.Warn("move a reopened card", "task", c.TaskUID, "err", err)
				continue
			}
			moved = true
		}
	}
	return moved
}

// cardLinkCounts counts the links each card of a project holds, by kind.
func (a *App) cardLinkCounts(projectID int64) (map[string]map[string]int, error) {
	links, err := a.store.ListProjectLinks(a.ctx, projectID, "")
	if err != nil {
		return nil, err
	}
	out := map[string]map[string]int{}
	for _, l := range links {
		if l.TaskUID == "" {
			continue
		}
		if out[l.TaskUID] == nil {
			out[l.TaskUID] = map[string]int{}
		}
		out[l.TaskUID][l.Kind]++
	}
	return out, nil
}

// AddProjectColumn adds a column at the end of a board.
func (a *App) AddProjectColumn(projectID int64, name string) (ProjectColumnDTO, error) {
	if err := a.ready(); err != nil {
		return ProjectColumnDTO{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return ProjectColumnDTO{}, errProjectColumnName
	}
	if _, err := a.store.GetProject(a.ctx, projectID); err != nil {
		return ProjectColumnDTO{}, err
	}
	col := storage.ProjectColumn{ProjectID: projectID, Name: name}
	if err := a.store.CreateProjectColumn(a.ctx, &col); err != nil {
		return ProjectColumnDTO{}, err
	}
	a.emitProjects(projectID)
	return columnDTO(col), nil
}

// UpdateProjectColumn renames a column and sets its WIP limit, 0 for none.
func (a *App) UpdateProjectColumn(id int64, name string, wipLimit int) error {
	if err := a.ready(); err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return errProjectColumnName
	}
	col, err := a.store.GetProjectColumn(a.ctx, id)
	if err != nil {
		return err
	}
	col.Name, col.WIPLimit = name, max(wipLimit, 0)
	if err := a.store.UpdateProjectColumn(a.ctx, *col); err != nil {
		return err
	}
	a.emitProjects(col.ProjectID)
	return nil
}

// SetProjectDoneColumn makes a column the board's done column, or leaves the
// board without one for columnID 0. Cards then follow their tasks the next
// time the board is read.
func (a *App) SetProjectDoneColumn(projectID, columnID int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	if err := a.store.SetProjectDoneColumn(a.ctx, projectID, columnID); err != nil {
		return err
	}
	a.emitProjects(projectID)
	return nil
}

// ReorderProjectColumns gives a board's columns the order of ids.
func (a *App) ReorderProjectColumns(projectID int64, ids []int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	if err := a.store.ReorderProjectColumns(a.ctx, projectID, ids); err != nil {
		return err
	}
	a.emitProjects(projectID)
	return nil
}

// DeleteProjectColumn removes a column. Its cards go to the end of moveTo, or
// leave the board for moveTo 0; their tasks stay either way.
func (a *App) DeleteProjectColumn(id, moveTo int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	col, err := a.store.GetProjectColumn(a.ctx, id)
	if err != nil {
		return err
	}
	if moveTo == 0 {
		cards, err := a.store.ListProjectCards(a.ctx, col.ProjectID)
		if err != nil {
			return err
		}
		for _, c := range cards {
			if c.ColumnID == id {
				if err := a.store.RemoveProjectCard(a.ctx, c.TaskUID); err != nil {
					return err
				}
			}
		}
	}
	if err := a.store.DeleteProjectColumn(a.ctx, id, moveTo); err != nil {
		return err
	}
	a.emitProjects(col.ProjectID)
	return nil
}

// CreateProjectCard makes a task in the project's list and puts it on the
// board. In the done column it is made completed.
func (a *App) CreateProjectCard(req ProjectCardRequest) (ProjectCardDTO, error) {
	if err := a.ready(); err != nil {
		return ProjectCardDTO{}, err
	}
	project, err := a.store.GetProject(a.ctx, req.ProjectID)
	if err != nil {
		return ProjectCardDTO{}, err
	}
	col, err := a.store.GetProjectColumn(a.ctx, req.ColumnID)
	if err != nil {
		return ProjectCardDTO{}, err
	}
	if col.ProjectID != project.ID {
		return ProjectCardDTO{}, errProjectOtherBoard
	}
	listID := project.ListID
	if listID != 0 {
		if _, err := a.writableTaskList(listID); err != nil {
			listID = 0
		}
	}
	task := TaskRequest{
		ListID: listID, Title: req.Title, Notes: req.Notes, Due: req.Due, TimeZone: req.TimeZone, Priority: req.Priority,
	}
	if col.Done {
		task.Status = ical.TodoCompleted
	}
	saved, err := a.CreateTask(task)
	if err != nil {
		return ProjectCardDTO{}, err
	}
	index := req.Index
	if index < 0 {
		index = 1 << 30
	}
	if err := a.store.PlaceProjectCard(a.ctx, saved.Task.UID, col.ID, index, 0); err != nil {
		return ProjectCardDTO{}, err
	}
	a.emitProjects(project.ID)
	return a.cardOf(saved.Task.UID)
}

// AddTaskToProject puts an existing task on a board, at the end of a column:
// the one given, or for columnID 0 the done column for a completed task and
// the first other column for an open one. A task on another board leaves it.
func (a *App) AddTaskToProject(projectID int64, taskUID string, columnID int64) (ProjectCardDTO, error) {
	if err := a.ready(); err != nil {
		return ProjectCardDTO{}, err
	}
	tasks, err := a.tasksByUID([]string{taskUID})
	if err != nil {
		return ProjectCardDTO{}, err
	}
	task, ok := tasks[taskUID]
	if !ok {
		return ProjectCardDTO{}, errProjectTaskGone
	}
	columns, err := a.store.ListProjectColumns(a.ctx, projectID)
	if err != nil {
		return ProjectCardDTO{}, err
	}
	if columnID == 0 {
		for _, c := range columns {
			if c.Done == task.Done {
				columnID = c.ID
				break
			}
		}
		if columnID == 0 && len(columns) > 0 {
			columnID = columns[0].ID
		}
	} else if !slices.ContainsFunc(columns, func(c storage.ProjectColumn) bool { return c.ID == columnID }) {
		return ProjectCardDTO{}, errProjectOtherBoard
	}
	if columnID == 0 {
		return ProjectCardDTO{}, storage.ErrProjectColumnNotFound
	}
	before, _ := a.store.GetProjectCard(a.ctx, taskUID)
	if err := a.store.PlaceProjectCard(a.ctx, taskUID, columnID, 1<<30, 0); err != nil {
		return ProjectCardDTO{}, err
	}
	if before != nil && before.ProjectID != projectID {
		a.emitProjects(before.ProjectID, projectID)
	} else {
		a.emitProjects(projectID)
	}
	return a.cardOf(taskUID)
}

// MoveProjectCard moves a card to index in a column of its board. Into the
// done column it completes the task; out of it, it opens the task again. A
// repeating task completed this way moves on to its next occurrence and the
// card stays where it was, which the result says with Advanced.
func (a *App) MoveProjectCard(taskUID string, columnID int64, index int) (ProjectMoveResultDTO, error) {
	if err := a.ready(); err != nil {
		return ProjectMoveResultDTO{}, err
	}
	card, err := a.store.GetProjectCard(a.ctx, taskUID)
	if err != nil {
		return ProjectMoveResultDTO{}, err
	}
	to, err := a.store.GetProjectColumn(a.ctx, columnID)
	if err != nil {
		return ProjectMoveResultDTO{}, err
	}
	if to.ProjectID != card.ProjectID {
		return ProjectMoveResultDTO{}, errProjectOtherBoard
	}
	from, err := a.store.GetProjectColumn(a.ctx, card.ColumnID)
	if err != nil {
		return ProjectMoveResultDTO{}, err
	}
	tasks, err := a.tasksByUID([]string{taskUID})
	if err != nil {
		return ProjectMoveResultDTO{}, err
	}
	task, ok := tasks[taskUID]
	if !ok {
		return ProjectMoveResultDTO{}, errProjectTaskGone
	}
	prev := int64(0)
	switch {
	case to.Done && !task.Done:
		result, err := a.SetTaskDone(task.ID, true)
		if err != nil {
			return ProjectMoveResultDTO{}, err
		}
		if result.Advanced {
			out, err := a.cardOf(taskUID)
			a.emitProjects(card.ProjectID)
			return ProjectMoveResultDTO{Card: out, Advanced: true}, err
		}
		prev = from.ID
	case to.Done:
		prev = card.PrevColumnID
		if from.ID != to.ID {
			prev = from.ID
		}
	case from.Done && task.Done:
		if _, err := a.SetTaskDone(task.ID, false); err != nil {
			return ProjectMoveResultDTO{}, err
		}
	}
	if err := a.store.PlaceProjectCard(a.ctx, taskUID, to.ID, index, prev); err != nil {
		return ProjectMoveResultDTO{}, err
	}
	a.emitProjects(card.ProjectID)
	out, err := a.cardOf(taskUID)
	return ProjectMoveResultDTO{Card: out}, err
}

// RemoveProjectCard takes a card off its board with the links it held. The
// task stays in its list.
func (a *App) RemoveProjectCard(taskUID string) error {
	if err := a.ready(); err != nil {
		return err
	}
	card, err := a.store.GetProjectCard(a.ctx, taskUID)
	if err != nil {
		return err
	}
	if err := a.store.RemoveProjectCard(a.ctx, taskUID); err != nil {
		return err
	}
	a.emitProjects(card.ProjectID)
	return nil
}

// ProjectOfTask returns the project a task is a card of, 0 for none.
func (a *App) ProjectOfTask(taskUID string) (int64, error) {
	if err := a.ready(); err != nil {
		return 0, err
	}
	card, err := a.store.GetProjectCard(a.ctx, taskUID)
	if errors.Is(err, storage.ErrProjectCardNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return card.ProjectID, nil
}

// cardOf reads one card as the board shows it.
func (a *App) cardOf(taskUID string) (ProjectCardDTO, error) {
	card, err := a.store.GetProjectCard(a.ctx, taskUID)
	if err != nil {
		return ProjectCardDTO{}, err
	}
	tasks, err := a.tasksByUID([]string{taskUID})
	if err != nil {
		return ProjectCardDTO{}, err
	}
	task, ok := tasks[taskUID]
	if !ok {
		return ProjectCardDTO{}, errProjectTaskGone
	}
	counts, err := a.cardLinkCounts(card.ProjectID)
	if err != nil {
		return ProjectCardDTO{}, err
	}
	return cardDTO(*card, task, counts[taskUID]), nil
}

// --- mail --------------------------------------------------------------------

// AddMailToProject files the conversations of these messages under a
// project, or under one of its cards when taskUID is set. The whole
// conversation follows, replies to come included. It returns how many
// conversations were added.
func (a *App) AddMailToProject(projectID int64, messageIDs []int64, taskUID string) (int, error) {
	if err := a.ready(); err != nil {
		return 0, err
	}
	if _, err := a.store.GetProject(a.ctx, projectID); err != nil {
		return 0, err
	}
	if taskUID != "" {
		card, err := a.store.GetProjectCard(a.ctx, taskUID)
		if err != nil {
			return 0, err
		}
		if card.ProjectID != projectID {
			return 0, errProjectOtherBoard
		}
	}
	seen := map[int64]bool{}
	added := 0
	for _, id := range messageIDs {
		account, ref, thread, subject, err := a.store.MessageThreadKey(a.ctx, id)
		if err != nil {
			return added, err
		}
		if thread != 0 && seen[thread] {
			continue
		}
		seen[thread] = true
		link := storage.ProjectLink{ProjectID: projectID, TaskUID: taskUID, Kind: storage.LinkMail, AccountID: account, ThreadID: thread, Ref: ref, Label: subject}
		if err := a.store.AddProjectLink(a.ctx, &link); err != nil {
			return added, err
		}
		added++
	}
	a.emitProjects(projectID)
	return added, nil
}

// RemoveProjectThread takes a conversation out of a project and its cards.
// The mail stays where it is.
func (a *App) RemoveProjectThread(projectID, threadID int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	if err := a.store.DeleteProjectThread(a.ctx, projectID, threadID); err != nil {
		return err
	}
	a.emitProjects(projectID)
	return nil
}

// ListProjectThreads returns a project's conversations as list rows, newest
// first. A conversation whose mail is all gone or in Trash is left out.
func (a *App) ListProjectThreads(projectID int64) ([]ThreadSummaryDTO, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	threads, err := a.store.ProjectThreads(a.ctx, projectID)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(threads))
	for _, t := range threads {
		ids = append(ids, t.ThreadID)
	}
	folders, err := a.threadMemberFolders(a.ctx)
	if err != nil {
		return nil, err
	}
	heads, err := a.store.LatestThreadMessages(a.ctx, ids, folders)
	if err != nil {
		return nil, err
	}
	pages, err := a.store.GroupThreads(a.ctx, heads, nil, folders)
	if err != nil {
		return nil, err
	}
	out, err := a.threadSummaries(a.ctx, pages)
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = []ThreadSummaryDTO{}
	}
	return out, nil
}

// ListProjectMailMarks returns which conversations belong to which projects,
// for the chips in the message list and the reader.
func (a *App) ListProjectMailMarks() ([]ProjectMailMarkDTO, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	threads, err := a.store.ProjectThreads(a.ctx, 0)
	if err != nil {
		return nil, err
	}
	out := make([]ProjectMailMarkDTO, 0, len(threads))
	for _, t := range threads {
		out = append(out, ProjectMailMarkDTO{ThreadID: t.ThreadID, ProjectID: t.ProjectID})
	}
	return out, nil
}

// DismissProjectSuggestion stops a conversation being suggested for a
// project again.
func (a *App) DismissProjectSuggestion(threadID int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	if threadID <= 0 {
		return nil
	}
	return a.store.DismissProjectSuggestion(a.ctx, threadID)
}

// ListProjectDismissals returns the conversations whose project suggestion
// was dismissed.
func (a *App) ListProjectDismissals() ([]int64, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	ids, err := a.store.ListProjectDismissals(a.ctx)
	if ids == nil {
		ids = []int64{}
	}
	return ids, err
}

// recordProjectMail files a message being sent under the project the
// composer picked: by its own Message-ID, so the sent copy and every reply
// join the project once they arrive, and by the conversation it answers when
// that is already here.
func (a *App) recordProjectMail(req ComposeRequest, messageID string) {
	if req.ProjectID == 0 {
		return
	}
	if _, err := a.store.GetProject(a.ctx, req.ProjectID); err != nil {
		a.log.Warn("file sent mail under a project", "project", req.ProjectID, "err", err)
		return
	}
	thread := int64(0)
	if req.InReplyTo != "" {
		if id, err := a.store.ThreadOfHeader(a.ctx, req.AccountID, req.InReplyTo); err == nil {
			thread = id
		}
	}
	link := storage.ProjectLink{
		ProjectID: req.ProjectID, Kind: storage.LinkMail, AccountID: req.AccountID,
		ThreadID: thread, Ref: strings.Trim(strings.TrimSpace(messageID), "<>"), Label: req.Subject,
	}
	if err := a.store.AddProjectLink(a.ctx, &link); err != nil {
		a.log.Warn("file sent mail under a project", "project", req.ProjectID, "err", err)
		return
	}
	a.emitProjects(req.ProjectID)
}

// --- events ------------------------------------------------------------------

// SetEventProject files an event under a project by its UID, taking it out of
// any other project; projectID 0 takes it out of all of them. Links a card
// holds to the event are left alone.
func (a *App) SetEventProject(uid, summary string, projectID int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	uid = strings.TrimSpace(uid)
	if uid == "" {
		return errProjectTaskGone
	}
	links, err := a.store.ListAllProjectLinks(a.ctx, storage.LinkEvent)
	if err != nil {
		return err
	}
	touched := []int64{}
	for _, l := range links {
		if l.Ref == uid && l.TaskUID == "" && l.ProjectID != projectID {
			if err := a.store.DeleteProjectLink(a.ctx, l.ID); err != nil {
				return err
			}
			touched = append(touched, l.ProjectID)
		}
	}
	if projectID != 0 {
		if _, err := a.store.GetProject(a.ctx, projectID); err != nil {
			return err
		}
		link := storage.ProjectLink{ProjectID: projectID, Kind: storage.LinkEvent, Ref: uid, Label: strings.TrimSpace(summary)}
		if err := a.store.AddProjectLink(a.ctx, &link); err != nil {
			return err
		}
		touched = append(touched, projectID)
	}
	a.emitProjects(touched...)
	return nil
}

// LinkEventToCard attaches an event to a card by its UID.
func (a *App) LinkEventToCard(taskUID, uid, summary string) error {
	if err := a.ready(); err != nil {
		return err
	}
	card, err := a.store.GetProjectCard(a.ctx, taskUID)
	if err != nil {
		return err
	}
	link := storage.ProjectLink{ProjectID: card.ProjectID, TaskUID: taskUID, Kind: storage.LinkEvent, Ref: strings.TrimSpace(uid), Label: strings.TrimSpace(summary)}
	if err := a.store.AddProjectLink(a.ctx, &link); err != nil {
		return err
	}
	a.emitProjects(card.ProjectID)
	return nil
}

// ProjectsOfEvent returns the projects an event is filed under itself.
func (a *App) ProjectsOfEvent(uid string) ([]int64, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	links, err := a.store.ListAllProjectLinks(a.ctx, storage.LinkEvent)
	if err != nil {
		return nil, err
	}
	out := []int64{}
	for _, l := range links {
		if l.Ref == uid && l.TaskUID == "" && !slices.Contains(out, l.ProjectID) {
			out = append(out, l.ProjectID)
		}
	}
	return out, nil
}

// ListProjectEvents returns the events linked to a project or its cards,
// each once, the coming ones first by start and the past ones after them,
// latest first.
func (a *App) ListProjectEvents(projectID int64) ([]ProjectEventDTO, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	links, err := a.store.ListProjectLinks(a.ctx, projectID, storage.LinkEvent)
	if err != nil {
		return nil, err
	}
	cals, err := a.calendarIndex(a.ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	out := []ProjectEventDTO{}
	seen := map[string]bool{}
	for _, l := range links {
		if seen[l.Ref] {
			continue
		}
		seen[l.Ref] = true
		dto := ProjectEventDTO{UID: l.Ref, TaskUID: l.TaskUID, Label: l.Label}
		rows, err := a.store.FindCalendarEventsByUID(a.ctx, l.Ref)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			if row.Pending == storage.PendingDelete {
				continue
			}
			obj, err := parseStoredEvent(row)
			if err != nil || obj.First() == nil {
				continue
			}
			fillProjectEvent(&dto, row, cals[row.CalendarID], obj, now)
			break
		}
		out = append(out, dto)
	}
	slices.SortStableFunc(out, func(x, y ProjectEventDTO) int {
		switch {
		case x.Found != y.Found:
			if x.Found {
				return -1
			}
			return 1
		case x.Past != y.Past:
			if y.Past {
				return -1
			}
			return 1
		case x.Past:
			return strings.Compare(y.Start, x.Start)
		}
		return strings.Compare(x.Start, y.Start)
	})
	return out, nil
}

// fillProjectEvent sets an event's next occurrence from now, or its last one
// when none is to come.
func fillProjectEvent(dto *ProjectEventDTO, row storage.CalendarEvent, cal storage.Calendar, obj *ical.Object, now time.Time) {
	first := obj.First()
	dto.Found = true
	dto.EventID = row.ID
	dto.CalendarColor = cal.Color
	dto.Summary = first.Summary
	dto.Recurring = obj.Master != nil && obj.Master.IsRecurring() || len(obj.Overrides) > 0
	set := func(o ical.Occurrence) {
		dto.Summary = o.Event.Summary
		dto.Start = formatEventTime(o.Start, o.AllDay)
		dto.End = formatEventTime(o.End, o.AllDay)
		dto.AllDay = o.AllDay
		dto.RecurrenceID = formatRecurrenceID(o.RecurrenceID)
	}
	if dto.Recurring {
		if next := obj.Expand(now.Add(-24*time.Hour), now.AddDate(1, 0, 0)); len(next) > 0 {
			for _, o := range next {
				if !o.End.Before(now) {
					set(o)
					return
				}
			}
			set(next[len(next)-1])
			dto.Past = true
			return
		}
	}
	start := obj.Zones.Instant(first.Start)
	end := obj.Zones.Instant(first.End)
	if end.IsZero() {
		end = start
	}
	dto.Start = formatEventTime(start, first.Start.AllDay)
	dto.End = formatEventTime(end, first.Start.AllDay)
	dto.AllDay = first.Start.AllDay
	dto.Past = end.Before(now)
}

// --- people ------------------------------------------------------------------

// AddProjectPerson adds someone to a project by address. Their mail is what
// the reader suggests filing under the project.
func (a *App) AddProjectPerson(projectID int64, email, name string) error {
	if err := a.ready(); err != nil {
		return err
	}
	email = strings.ToLower(bareAddress(strings.TrimSpace(email)))
	if email == "" || !strings.Contains(email, "@") {
		return errProjectPerson
	}
	if _, err := a.store.GetProject(a.ctx, projectID); err != nil {
		return err
	}
	link := storage.ProjectLink{ProjectID: projectID, Kind: storage.LinkContact, Ref: email, Label: strings.TrimSpace(name)}
	if err := a.store.AddProjectLink(a.ctx, &link); err != nil {
		return err
	}
	a.emitProjects(projectID)
	return nil
}

// RemoveProjectPerson takes someone off a project.
func (a *App) RemoveProjectPerson(projectID int64, email string) error {
	if err := a.ready(); err != nil {
		return err
	}
	if err := a.store.DeleteProjectLinksByRef(a.ctx, projectID, storage.LinkContact, strings.ToLower(bareAddress(strings.TrimSpace(email)))); err != nil {
		return err
	}
	a.emitProjects(projectID)
	return nil
}

// --- card links ----------------------------------------------------------------

// ListCardLinks returns what a card holds: its mail, events and notes.
func (a *App) ListCardLinks(taskUID string) ([]ProjectLinkDTO, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	links, err := a.store.ListCardLinks(a.ctx, taskUID)
	if err != nil {
		return nil, err
	}
	out := make([]ProjectLinkDTO, 0, len(links))
	for _, l := range links {
		out = append(out, linkDTO(l))
	}
	return out, nil
}

// RemoveProjectLink removes one link of a project or a card.
func (a *App) RemoveProjectLink(id int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	link, err := a.store.GetProjectLink(a.ctx, id)
	if err != nil {
		return err
	}
	if err := a.store.DeleteProjectLink(a.ctx, id); err != nil {
		return err
	}
	a.emitProjects(link.ProjectID)
	return nil
}

// AttachProjectNote attaches one of a project's notes to a card of it.
func (a *App) AttachProjectNote(noteID int64, taskUID string) error {
	if err := a.ready(); err != nil {
		return err
	}
	note, err := a.store.GetProjectNote(a.ctx, noteID)
	if err != nil {
		return err
	}
	card, err := a.store.GetProjectCard(a.ctx, taskUID)
	if err != nil {
		return err
	}
	if card.ProjectID != note.ProjectID {
		return errProjectOtherBoard
	}
	link := storage.ProjectLink{ProjectID: note.ProjectID, TaskUID: taskUID, Kind: storage.LinkNote, Ref: strconv.FormatInt(note.ID, 10), Label: note.Title}
	if err := a.store.AddProjectLink(a.ctx, &link); err != nil {
		return err
	}
	a.emitProjects(note.ProjectID)
	return nil
}

// --- notes -------------------------------------------------------------------

// ListProjectNotes returns a project's notes, the last edited first.
func (a *App) ListProjectNotes(projectID int64) ([]ProjectNoteDTO, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	notes, err := a.store.ListProjectNotes(a.ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]ProjectNoteDTO, 0, len(notes))
	for _, n := range notes {
		out = append(out, noteDTO(n))
	}
	return out, nil
}

// CreateProjectNote adds a note to a project.
func (a *App) CreateProjectNote(projectID int64, title, body string) (ProjectNoteDTO, error) {
	if err := a.ready(); err != nil {
		return ProjectNoteDTO{}, err
	}
	if _, err := a.store.GetProject(a.ctx, projectID); err != nil {
		return ProjectNoteDTO{}, err
	}
	n := storage.ProjectNote{ProjectID: projectID, Title: strings.TrimSpace(title), Body: body}
	if err := a.store.CreateProjectNote(a.ctx, &n); err != nil {
		return ProjectNoteDTO{}, err
	}
	a.emitProjects(projectID)
	return noteDTO(n), nil
}

// UpdateProjectNote saves a note's title and body.
func (a *App) UpdateProjectNote(id int64, title, body string) (ProjectNoteDTO, error) {
	if err := a.ready(); err != nil {
		return ProjectNoteDTO{}, err
	}
	n, err := a.store.GetProjectNote(a.ctx, id)
	if err != nil {
		return ProjectNoteDTO{}, err
	}
	n.Title, n.Body = strings.TrimSpace(title), body
	if err := a.store.UpdateProjectNote(a.ctx, n); err != nil {
		return ProjectNoteDTO{}, err
	}
	a.emitProjects(n.ProjectID)
	return noteDTO(*n), nil
}

// DeleteProjectNote removes a note and the card links to it.
func (a *App) DeleteProjectNote(id int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	n, err := a.store.GetProjectNote(a.ctx, id)
	if err != nil {
		return err
	}
	if err := a.store.DeleteProjectNote(a.ctx, id); err != nil {
		return err
	}
	a.emitProjects(n.ProjectID)
	return nil
}

// --- helpers -------------------------------------------------------------------

// projectStat is what a project's tile counts.
type projectStat struct {
	cards, done, overdue int
	nextDue              string
	mail, events, notes  int
	people               []ProjectPersonDTO
}

// projectStats counts cards, deadlines, mail, events, notes and people per
// project; projectID 0 counts every project.
func (a *App) projectStats(projectID int64) (map[int64]*projectStat, error) {
	out := map[int64]*projectStat{}
	stat := func(id int64) *projectStat {
		if out[id] == nil {
			out[id] = &projectStat{people: []ProjectPersonDTO{}}
		}
		return out[id]
	}
	tasks, err := a.store.ProjectTasks(a.ctx, projectID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	var nextAt = map[int64]time.Time{}
	for _, t := range tasks {
		s := stat(t.ProjectID)
		s.cards++
		if t.Completed != "" {
			s.done++
			continue
		}
		due, ok := dueTime(t.Due)
		if !ok {
			continue
		}
		if due.Before(now) {
			s.overdue++
		}
		if at, seen := nextAt[t.ProjectID]; !seen || due.Before(at) {
			nextAt[t.ProjectID] = due
			s.nextDue = t.Due
		}
	}
	threads, err := a.store.ProjectThreads(a.ctx, projectID)
	if err != nil {
		return nil, err
	}
	for _, t := range threads {
		stat(t.ProjectID).mail++
	}
	for _, kind := range []string{storage.LinkEvent, storage.LinkContact} {
		var links []storage.ProjectLink
		if projectID == 0 {
			links, err = a.store.ListAllProjectLinks(a.ctx, kind)
		} else {
			links, err = a.store.ListProjectLinks(a.ctx, projectID, kind)
		}
		if err != nil {
			return nil, err
		}
		events := map[int64]map[string]bool{}
		for _, l := range links {
			s := stat(l.ProjectID)
			if kind == storage.LinkContact {
				s.people = append(s.people, ProjectPersonDTO{Email: l.Ref, Name: l.Label})
				continue
			}
			if events[l.ProjectID] == nil {
				events[l.ProjectID] = map[string]bool{}
			}
			if !events[l.ProjectID][l.Ref] {
				events[l.ProjectID][l.Ref] = true
				s.events++
			}
		}
	}
	var projects []storage.Project
	if projectID == 0 {
		if projects, err = a.store.ListProjects(a.ctx); err != nil {
			return nil, err
		}
	} else {
		p, err := a.store.GetProject(a.ctx, projectID)
		if err != nil {
			return nil, err
		}
		projects = []storage.Project{*p}
	}
	notes, err := a.projectNoteCounts(projects)
	if err != nil {
		return nil, err
	}
	for id, n := range notes {
		stat(id).notes = n
	}
	return out, nil
}

// dueTime reads a task's stored due: a date counts as due at the end of that
// day in the local zone, an instant as itself.
func dueTime(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	if d, err := time.ParseInLocation(dateLayout, s, time.Local); err == nil {
		return d.AddDate(0, 0, 1).Add(-time.Nanosecond), true
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, true
	}
	return time.Time{}, false
}

func (a *App) projectWithStats(id int64) (ProjectDTO, error) {
	p, err := a.store.GetProject(a.ctx, id)
	if err != nil {
		return ProjectDTO{}, err
	}
	stats, err := a.projectStats(id)
	if err != nil {
		return ProjectDTO{}, err
	}
	return projectDTO(*p, stats[id]), nil
}

// projectFromRequest checks a request and turns it into a project.
func projectFromRequest(req ProjectRequest) (storage.Project, error) {
	p := storage.Project{
		ID:          req.ID,
		Name:        strings.TrimSpace(req.Name),
		Color:       strings.TrimSpace(req.Color),
		Icon:        strings.TrimSpace(req.Icon),
		Description: strings.TrimSpace(req.Description),
		StartDate:   strings.TrimSpace(req.StartDate),
		DueDate:     strings.TrimSpace(req.DueDate),
		ListID:      req.ListID,
	}
	if p.Name == "" {
		return p, errProjectName
	}
	if p.Color != "" && !projectColorPattern.MatchString(p.Color) {
		return p, errProjectColor
	}
	if len(p.Icon) > 40 {
		p.Icon = ""
	}
	for _, d := range []string{p.StartDate, p.DueDate} {
		if d == "" {
			continue
		}
		if _, err := time.Parse(dateLayout, d); err != nil {
			return p, errProjectDate
		}
	}
	return p, nil
}

func projectDTO(p storage.Project, s *projectStat) ProjectDTO {
	dto := ProjectDTO{
		ID: p.ID, UID: p.UID, Name: p.Name, Color: p.Color, Icon: p.Icon, Description: p.Description, Status: p.Status,
		StartDate: p.StartDate, DueDate: p.DueDate, Position: p.Position, ListID: p.ListID,
		CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt, ArchivedAt: p.ArchivedAt,
		People: []ProjectPersonDTO{},
	}
	if s != nil {
		dto.Cards, dto.Done, dto.Overdue, dto.NextDue = s.cards, s.done, s.overdue, s.nextDue
		dto.Mail, dto.Events, dto.Notes = s.mail, s.events, s.notes
		dto.People = s.people
	}
	return dto
}

func columnDTO(c storage.ProjectColumn) ProjectColumnDTO {
	return ProjectColumnDTO{ID: c.ID, ProjectID: c.ProjectID, Name: c.Name, Position: c.Position, WIPLimit: c.WIPLimit, Done: c.Done}
}

func cardDTO(c storage.ProjectCard, task TaskDTO, counts map[string]int) ProjectCardDTO {
	return ProjectCardDTO{
		Task: task, ProjectID: c.ProjectID, ColumnID: c.ColumnID, Position: c.Position,
		Mail:       counts[storage.LinkMail] + len(task.MailLinks),
		Events:     counts[storage.LinkEvent] + len(task.EventUIDs),
		Notes:      counts[storage.LinkNote],
		SourceKind: c.SourceKind, SourceRef: c.SourceRef,
	}
}

func linkDTO(l storage.ProjectLink) ProjectLinkDTO {
	return ProjectLinkDTO{
		ID: l.ID, ProjectID: l.ProjectID, TaskUID: l.TaskUID, Kind: l.Kind, AccountID: l.AccountID,
		ThreadID: l.ThreadID, Ref: l.Ref, Label: l.Label, CreatedAt: l.CreatedAt,
	}
}

func noteDTO(n storage.ProjectNote) ProjectNoteDTO {
	return ProjectNoteDTO{ID: n.ID, ProjectID: n.ProjectID, Title: n.Title, Body: n.Body, CreatedAt: n.CreatedAt, UpdatedAt: n.UpdatedAt}
}

// emitProjects tells the ui these projects changed; none means any may have.
func (a *App) emitProjects(ids ...int64) {
	clean := []int64{}
	for _, id := range ids {
		if id != 0 && !slices.Contains(clean, id) {
			clean = append(clean, id)
		}
	}
	a.emit(EventProjectsChanged, ProjectsChangedEvent{ProjectIDs: clean})
}
