package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// ErrProjectNotFound is returned when a project id has no row.
var ErrProjectNotFound = errors.New("storage: project not found")

// ErrProjectColumnNotFound is returned when a board column id has no row, or
// the column belongs to another project than the one asked about.
var ErrProjectColumnNotFound = errors.New("storage: project column not found")

// ErrProjectCardNotFound is returned when a task is on no board.
var ErrProjectCardNotFound = errors.New("storage: card not found")

// ErrProjectNoteNotFound is returned when a project note id has no row.
var ErrProjectNoteNotFound = errors.New("storage: project note not found")

// The statuses a project can have.
const (
	ProjectActive   = "active"
	ProjectArchived = "archived"
)

// The kinds of thing a project link points at.
const (
	LinkMail    = "mail"
	LinkEvent   = "event"
	LinkContact = "contact"
	LinkNote    = "note"
)

// Project is one project: its name and look, its dates and where it sorts.
type Project struct {
	ID int64
	// UID names the project outside the database: in a note's front matter
	// and in pelton://project links.
	UID         string
	Name        string
	Color       string
	Icon        string
	Description string
	Status      string
	StartDate   string
	DueDate     string
	Position    int
	// ListID is the task list new cards are made in, 0 for the default one.
	ListID     int64
	CreatedAt  string
	UpdatedAt  string
	ArchivedAt string
}

// ProjectColumn is one column of a project's board.
type ProjectColumn struct {
	ID        int64
	ProjectID int64
	Name      string
	Position  int
	// WIPLimit is how many cards the column should hold, 0 for no limit.
	WIPLimit int
	// Done marks the column completed tasks gather in.
	Done bool
}

// ProjectCard places a task on a board. SourceKind and SourceRef are empty for
// a card made in Pelton and name the outside thing it mirrors otherwise.
type ProjectCard struct {
	TaskUID      string
	ProjectID    int64
	ColumnID     int64
	Position     int
	PrevColumnID int64
	SourceKind   string
	SourceRef    string
	AddedAt      string
}

// ProjectLink is something a project, or one of its cards when TaskUID is
// set, points at. See the project_links migration for what Ref holds per
// Kind.
type ProjectLink struct {
	ID        int64
	ProjectID int64
	TaskUID   string
	Kind      string
	AccountID int64
	ThreadID  int64
	Ref       string
	Label     string
	CreatedAt string
}

// ProjectNote is a note kept with a project in the database. VaultPath is
// the file it is being or was written to in the notes folder, and MigratedAt
// is set once that is done.
type ProjectNote struct {
	ID         int64
	ProjectID  int64
	Title      string
	Body       string
	CreatedAt  string
	UpdatedAt  string
	VaultPath  string
	MigratedAt string
}

// ProjectTaskRow is a card's task as the overview counts need it: whether it
// is done and when it is due.
type ProjectTaskRow struct {
	ProjectID int64
	TaskUID   string
	Due       string
	Completed string
}

// ProjectThread is a conversation linked to a project.
type ProjectThread struct {
	ThreadID  int64
	ProjectID int64
}

const projectColumns = `id, uid, name, color, icon, description, status, start_date, due_date, position, list_id,
       created_at, updated_at, archived_at`

// CreateProject inserts a project at the end of the list and returns it with
// its id and a new random uid. An empty status is stored as active.
func (d *DB) CreateProject(ctx context.Context, p *Project) error {
	if p.Status == "" {
		p.Status = ProjectActive
	}
	now := nowText()
	res, err := d.sql.ExecContext(ctx, `
INSERT INTO projects (uid, name, color, icon, description, status, start_date, due_date, position, list_id, created_at, updated_at)
VALUES (lower(hex(randomblob(16))), ?, ?, ?, ?, ?, ?, ?, (SELECT coalesce(max(position), -1) + 1 FROM projects), ?, ?, ?)`,
		p.Name, p.Color, p.Icon, p.Description, p.Status, p.StartDate, p.DueDate, p.ListID, now, now)
	if err != nil {
		return fmt.Errorf("storage: insert project %q: %w", p.Name, err)
	}
	if p.ID, err = res.LastInsertId(); err != nil {
		return fmt.Errorf("storage: project insert id: %w", err)
	}
	stored, err := d.GetProject(ctx, p.ID)
	if err != nil {
		return err
	}
	*p = *stored
	return nil
}

// ListProjects returns every project in its manual order.
func (d *DB) ListProjects(ctx context.Context) ([]Project, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT `+projectColumns+` FROM projects ORDER BY position, id`)
	if err != nil {
		return nil, fmt.Errorf("storage: list projects: %w", err)
	}
	defer rows.Close()
	var out []Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan project: %w", err)
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// GetProject returns one project, or ErrProjectNotFound.
func (d *DB) GetProject(ctx context.Context, id int64) (*Project, error) {
	p, err := scanProject(d.sql.QueryRowContext(ctx, `SELECT `+projectColumns+` FROM projects WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrProjectNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: get project %d: %w", id, err)
	}
	return p, nil
}

// UpdateProject saves a project's name, look, description, dates and task
// list. Status and position have their own calls.
func (d *DB) UpdateProject(ctx context.Context, p Project) error {
	res, err := d.sql.ExecContext(ctx, `
UPDATE projects SET name = ?, color = ?, icon = ?, description = ?, start_date = ?, due_date = ?, list_id = ?, updated_at = ?
WHERE id = ?`, p.Name, p.Color, p.Icon, p.Description, p.StartDate, p.DueDate, p.ListID, nowText(), p.ID)
	if err != nil {
		return fmt.Errorf("storage: update project %d: %w", p.ID, err)
	}
	return requireOneRow(res, ErrProjectNotFound)
}

// SetProjectStatus archives a project or makes it active again. Archiving
// records when; making it active clears that.
func (d *DB) SetProjectStatus(ctx context.Context, id int64, status string) error {
	archivedAt := ""
	if status == ProjectArchived {
		archivedAt = nowText()
	}
	res, err := d.sql.ExecContext(ctx, `UPDATE projects SET status = ?, archived_at = ?, updated_at = ? WHERE id = ?`,
		status, archivedAt, nowText(), id)
	if err != nil {
		return fmt.Errorf("storage: set project %d status: %w", id, err)
	}
	return requireOneRow(res, ErrProjectNotFound)
}

// DeleteProject removes a project with its board, links and notes. The tasks,
// mail, events and contacts it pointed at are not touched.
func (d *DB) DeleteProject(ctx context.Context, id int64) error {
	res, err := d.sql.ExecContext(ctx, `DELETE FROM projects WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("storage: delete project %d: %w", id, err)
	}
	return requireOneRow(res, ErrProjectNotFound)
}

// ReorderProjects gives the projects the order of ids. Projects left out keep
// their place after the ones named.
func (d *DB) ReorderProjects(ctx context.Context, ids []int64) error {
	return d.renumber(ctx, `UPDATE projects SET position = ? WHERE id = ?`, ids, `SELECT id FROM projects ORDER BY position, id`)
}

// renumber writes positions 0..n for ids, then for the rows listQuery returns
// that ids did not name, in one transaction.
func (d *DB) renumber(ctx context.Context, update string, ids []int64, listQuery string, args ...any) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("storage: begin reorder: %w", err)
	}
	defer tx.Rollback()
	rest, err := queryInt64s(ctx, tx, listQuery, args...)
	if err != nil {
		return err
	}
	order := slices.Clone(ids)
	for _, id := range rest {
		if !slices.Contains(order, id) {
			order = append(order, id)
		}
	}
	for i, id := range order {
		if !slices.Contains(rest, id) {
			continue
		}
		if _, err := tx.ExecContext(ctx, update, i, id); err != nil {
			return fmt.Errorf("storage: reorder %d: %w", id, err)
		}
	}
	return tx.Commit()
}

// --- columns ---------------------------------------------------------------

// CreateProjectColumn adds a column at the end of a project's board. A column
// marked Done takes that role from any column that had it.
func (d *DB) CreateProjectColumn(ctx context.Context, c *ProjectColumn) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("storage: begin column insert: %w", err)
	}
	defer tx.Rollback()
	if c.Done {
		if _, err := tx.ExecContext(ctx, `UPDATE project_columns SET is_done = 0 WHERE project_id = ?`, c.ProjectID); err != nil {
			return fmt.Errorf("storage: clear done column: %w", err)
		}
	}
	res, err := tx.ExecContext(ctx, `
INSERT INTO project_columns (project_id, name, position, wip_limit, is_done)
VALUES (?, ?, (SELECT coalesce(max(position), -1) + 1 FROM project_columns WHERE project_id = ?), ?, ?)`,
		c.ProjectID, c.Name, c.ProjectID, max(c.WIPLimit, 0), boolToInt(c.Done))
	if err != nil {
		return fmt.Errorf("storage: insert column %q: %w", c.Name, err)
	}
	if c.ID, err = res.LastInsertId(); err != nil {
		return fmt.Errorf("storage: column insert id: %w", err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT position FROM project_columns WHERE id = ?`, c.ID).Scan(&c.Position); err != nil {
		return fmt.Errorf("storage: column position: %w", err)
	}
	return tx.Commit()
}

// ListProjectColumns returns a project's columns in board order.
func (d *DB) ListProjectColumns(ctx context.Context, projectID int64) ([]ProjectColumn, error) {
	rows, err := d.sql.QueryContext(ctx, `
SELECT id, project_id, name, position, wip_limit, is_done FROM project_columns
WHERE project_id = ? ORDER BY position, id`, projectID)
	if err != nil {
		return nil, fmt.Errorf("storage: list columns: %w", err)
	}
	defer rows.Close()
	var out []ProjectColumn
	for rows.Next() {
		var (
			c    ProjectColumn
			done int
		)
		if err := rows.Scan(&c.ID, &c.ProjectID, &c.Name, &c.Position, &c.WIPLimit, &done); err != nil {
			return nil, fmt.Errorf("storage: scan column: %w", err)
		}
		c.Done = done != 0
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetProjectColumn returns one column, or ErrProjectColumnNotFound.
func (d *DB) GetProjectColumn(ctx context.Context, id int64) (*ProjectColumn, error) {
	var (
		c    ProjectColumn
		done int
	)
	err := d.sql.QueryRowContext(ctx, `SELECT id, project_id, name, position, wip_limit, is_done FROM project_columns WHERE id = ?`, id).
		Scan(&c.ID, &c.ProjectID, &c.Name, &c.Position, &c.WIPLimit, &done)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrProjectColumnNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: get column %d: %w", id, err)
	}
	c.Done = done != 0
	return &c, nil
}

// UpdateProjectColumn saves a column's name and WIP limit.
func (d *DB) UpdateProjectColumn(ctx context.Context, c ProjectColumn) error {
	res, err := d.sql.ExecContext(ctx, `UPDATE project_columns SET name = ?, wip_limit = ? WHERE id = ?`, c.Name, max(c.WIPLimit, 0), c.ID)
	if err != nil {
		return fmt.Errorf("storage: update column %d: %w", c.ID, err)
	}
	return requireOneRow(res, ErrProjectColumnNotFound)
}

// SetProjectDoneColumn makes one column of a project its done column, or
// leaves the project without one for columnID 0.
func (d *DB) SetProjectDoneColumn(ctx context.Context, projectID, columnID int64) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("storage: begin done column: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE project_columns SET is_done = 0 WHERE project_id = ?`, projectID); err != nil {
		return fmt.Errorf("storage: clear done column: %w", err)
	}
	if columnID != 0 {
		res, err := tx.ExecContext(ctx, `UPDATE project_columns SET is_done = 1 WHERE id = ? AND project_id = ?`, columnID, projectID)
		if err != nil {
			return fmt.Errorf("storage: set done column: %w", err)
		}
		if err := requireOneRow(res, ErrProjectColumnNotFound); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ReorderProjectColumns gives a project's columns the order of ids.
func (d *DB) ReorderProjectColumns(ctx context.Context, projectID int64, ids []int64) error {
	return d.renumber(ctx, `UPDATE project_columns SET position = ? WHERE id = ?`, ids,
		`SELECT id FROM project_columns WHERE project_id = ? ORDER BY position, id`, projectID)
}

// DeleteProjectColumn removes a column. Its cards go to the end of moveTo, a
// column of the same project, or leave the board for moveTo 0; their tasks
// stay either way.
func (d *DB) DeleteProjectColumn(ctx context.Context, id, moveTo int64) error {
	col, err := d.GetProjectColumn(ctx, id)
	if err != nil {
		return err
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("storage: begin column delete: %w", err)
	}
	defer tx.Rollback()
	if moveTo != 0 && moveTo != id {
		var project int64
		if err := tx.QueryRowContext(ctx, `SELECT project_id FROM project_columns WHERE id = ?`, moveTo).Scan(&project); err != nil || project != col.ProjectID {
			return ErrProjectColumnNotFound
		}
		var end int
		if err := tx.QueryRowContext(ctx, `SELECT coalesce(max(position), -1) + 1 FROM project_cards WHERE column_id = ?`, moveTo).Scan(&end); err != nil {
			return fmt.Errorf("storage: column end: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
UPDATE project_cards SET column_id = ?, position = position + ?, prev_column_id = 0 WHERE column_id = ?`, moveTo, end, id); err != nil {
			return fmt.Errorf("storage: move cards out of column %d: %w", id, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE project_cards SET prev_column_id = 0 WHERE prev_column_id = ?`, id); err != nil {
		return fmt.Errorf("storage: forget column %d: %w", id, err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM project_columns WHERE id = ?`, id); err != nil {
		return fmt.Errorf("storage: delete column %d: %w", id, err)
	}
	return tx.Commit()
}

// --- cards -------------------------------------------------------------------

const cardColumns = `task_uid, project_id, column_id, position, prev_column_id, coalesce(source_kind, ''), coalesce(source_ref, ''), added_at`

// ListProjectCards returns the cards of a project, by column and position.
func (d *DB) ListProjectCards(ctx context.Context, projectID int64) ([]ProjectCard, error) {
	return d.queryCards(ctx, `SELECT `+cardColumns+` FROM project_cards WHERE project_id = ?
ORDER BY column_id, position, added_at`, projectID)
}

// GetProjectCard returns the card a task is, or ErrProjectCardNotFound.
func (d *DB) GetProjectCard(ctx context.Context, taskUID string) (*ProjectCard, error) {
	cards, err := d.queryCards(ctx, `SELECT `+cardColumns+` FROM project_cards WHERE task_uid = ?`, taskUID)
	if err != nil {
		return nil, err
	}
	if len(cards) == 0 {
		return nil, ErrProjectCardNotFound
	}
	return &cards[0], nil
}

func (d *DB) queryCards(ctx context.Context, query string, args ...any) ([]ProjectCard, error) {
	rows, err := d.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: list cards: %w", err)
	}
	defer rows.Close()
	var out []ProjectCard
	for rows.Next() {
		var c ProjectCard
		if err := rows.Scan(&c.TaskUID, &c.ProjectID, &c.ColumnID, &c.Position, &c.PrevColumnID, &c.SourceKind, &c.SourceRef, &c.AddedAt); err != nil {
			return nil, fmt.Errorf("storage: scan card: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// PlaceProjectCard puts a task on a board at index in a column, making it a
// card when it is none, taking it off another board when it was on one, and
// renumbering the column. An index past the end appends. prevColumnID is kept
// on the card as where it came from, 0 for nowhere. A card that leaves a
// project leaves its card links behind with it.
func (d *DB) PlaceProjectCard(ctx context.Context, taskUID string, columnID int64, index int, prevColumnID int64) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("storage: begin card place: %w", err)
	}
	defer tx.Rollback()
	var projectID int64
	if err := tx.QueryRowContext(ctx, `SELECT project_id FROM project_columns WHERE id = ?`, columnID).Scan(&projectID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrProjectColumnNotFound
		}
		return fmt.Errorf("storage: card column: %w", err)
	}
	var (
		oldProject int64
		oldColumn  int64
	)
	err = tx.QueryRowContext(ctx, `SELECT project_id, column_id FROM project_cards WHERE task_uid = ?`, taskUID).Scan(&oldProject, &oldColumn)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if _, err := tx.ExecContext(ctx, `
INSERT INTO project_cards (task_uid, project_id, column_id, position, prev_column_id, added_at) VALUES (?, ?, ?, 0, ?, ?)`,
			taskUID, projectID, columnID, prevColumnID, nowText()); err != nil {
			return fmt.Errorf("storage: insert card: %w", err)
		}
	case err != nil:
		return fmt.Errorf("storage: read card: %w", err)
	default:
		if oldProject != projectID {
			if _, err := tx.ExecContext(ctx, `UPDATE project_links SET project_id = ? WHERE project_id = ? AND task_uid = ?`,
				projectID, oldProject, taskUID); err != nil {
				return fmt.Errorf("storage: move card links: %w", err)
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE project_cards SET project_id = ?, column_id = ?, prev_column_id = ? WHERE task_uid = ?`,
			projectID, columnID, prevColumnID, taskUID); err != nil {
			return fmt.Errorf("storage: move card: %w", err)
		}
		if oldColumn != columnID {
			if err := renumberColumn(ctx, tx, oldColumn, "", 0); err != nil {
				return err
			}
		}
	}
	if err := renumberColumn(ctx, tx, columnID, taskUID, index); err != nil {
		return err
	}
	return tx.Commit()
}

// renumberColumn writes positions 0..n to a column's cards, with uid put at
// index when it is set.
func renumberColumn(ctx context.Context, tx *sql.Tx, columnID int64, uid string, index int) error {
	rows, err := tx.QueryContext(ctx, `SELECT task_uid FROM project_cards WHERE column_id = ? AND task_uid != ? ORDER BY position, added_at`, columnID, uid)
	if err != nil {
		return fmt.Errorf("storage: column cards: %w", err)
	}
	var order []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			rows.Close()
			return fmt.Errorf("storage: scan column card: %w", err)
		}
		order = append(order, u)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if uid != "" {
		index = min(max(index, 0), len(order))
		order = slices.Insert(order, index, uid)
	}
	for i, u := range order {
		if _, err := tx.ExecContext(ctx, `UPDATE project_cards SET position = ? WHERE task_uid = ?`, i, u); err != nil {
			return fmt.Errorf("storage: number card: %w", err)
		}
	}
	return nil
}

// RemoveProjectCard takes a task off its board, with the links its card held.
// The task itself stays.
func (d *DB) RemoveProjectCard(ctx context.Context, taskUID string) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("storage: begin card remove: %w", err)
	}
	defer tx.Rollback()
	var columnID int64
	if err := tx.QueryRowContext(ctx, `SELECT column_id FROM project_cards WHERE task_uid = ?`, taskUID).Scan(&columnID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrProjectCardNotFound
		}
		return fmt.Errorf("storage: read card: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM project_cards WHERE task_uid = ?`, taskUID); err != nil {
		return fmt.Errorf("storage: delete card: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM project_links WHERE task_uid = ?`, taskUID); err != nil {
		return fmt.Errorf("storage: delete card links: %w", err)
	}
	if err := renumberColumn(ctx, tx, columnID, "", 0); err != nil {
		return err
	}
	return tx.Commit()
}

// SetProjectCardSource records the outside thing a card mirrors, or clears it
// for an empty kind.
func (d *DB) SetProjectCardSource(ctx context.Context, taskUID, kind, ref string) error {
	var k, r any
	if kind != "" {
		k, r = kind, ref
	}
	res, err := d.sql.ExecContext(ctx, `UPDATE project_cards SET source_kind = ?, source_ref = ? WHERE task_uid = ?`, k, r, taskUID)
	if err != nil {
		return fmt.Errorf("storage: set card source: %w", err)
	}
	return requireOneRow(res, ErrProjectCardNotFound)
}

// ProjectTasks returns the tasks on boards, one row per card whose task still
// exists, for counting progress and deadlines. projectID 0 means every board.
func (d *DB) ProjectTasks(ctx context.Context, projectID int64) ([]ProjectTaskRow, error) {
	query := `
SELECT c.project_id, c.task_uid, e.task_due, e.task_completed
FROM project_cards c JOIN calendar_events e ON e.uid = c.task_uid AND e.kind = 'VTODO' AND e.pending != 'delete'`
	var args []any
	if projectID != 0 {
		query += ` WHERE c.project_id = ?`
		args = append(args, projectID)
	}
	rows, err := d.sql.QueryContext(ctx, query+` ORDER BY c.project_id, c.task_uid, e.id`, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: project tasks: %w", err)
	}
	defer rows.Close()
	var out []ProjectTaskRow
	for rows.Next() {
		var r ProjectTaskRow
		if err := rows.Scan(&r.ProjectID, &r.TaskUID, &r.Due, &r.Completed); err != nil {
			return nil, fmt.Errorf("storage: scan project task: %w", err)
		}
		if n := len(out); n > 0 && out[n-1].TaskUID == r.TaskUID {
			continue
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// TasksByUID returns the stored tasks with the given UIDs, pending deletions
// left out. A UID two lists share comes back once, from the lowest row.
func (d *DB) TasksByUID(ctx context.Context, uids []string) ([]CalendarEvent, error) {
	var out []CalendarEvent
	seen := map[string]bool{}
	const batch = 400
	for len(uids) > 0 {
		part := uids[:min(len(uids), batch)]
		uids = uids[len(part):]
		args := make([]any, len(part))
		for i, u := range part {
			args[i] = u
		}
		rows, err := d.queryEvents(ctx, `SELECT `+eventColumns+` FROM calendar_events
WHERE kind = 'VTODO' AND pending != 'delete' AND uid IN (`+strings.TrimSuffix(strings.Repeat("?,", len(part)), ",")+`) ORDER BY id`, args...)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			if !seen[r.UID] {
				seen[r.UID] = true
				out = append(out, r)
			}
		}
	}
	return out, nil
}

// --- links -------------------------------------------------------------------

const linkColumns = `id, project_id, task_uid, kind, account_id, thread_id, ref, label, created_at`

// AddProjectLink stores a link unless the same one is already there, and
// returns it with its id either way. A label given again replaces the stored
// one.
func (d *DB) AddProjectLink(ctx context.Context, l *ProjectLink) error {
	_, err := d.sql.ExecContext(ctx, `
INSERT INTO project_links (project_id, task_uid, kind, account_id, thread_id, ref, label, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(project_id, task_uid, kind, account_id, ref) DO UPDATE SET
    label = CASE WHEN excluded.label != '' THEN excluded.label ELSE project_links.label END,
    thread_id = CASE WHEN excluded.thread_id != 0 THEN excluded.thread_id ELSE project_links.thread_id END`,
		l.ProjectID, l.TaskUID, l.Kind, l.AccountID, l.ThreadID, l.Ref, l.Label, nowText())
	if err != nil {
		return fmt.Errorf("storage: add project link: %w", err)
	}
	return d.sql.QueryRowContext(ctx, `SELECT `+linkColumns+` FROM project_links
WHERE project_id = ? AND task_uid = ? AND kind = ? AND account_id = ? AND ref = ?`,
		l.ProjectID, l.TaskUID, l.Kind, l.AccountID, l.Ref).
		Scan(&l.ID, &l.ProjectID, &l.TaskUID, &l.Kind, &l.AccountID, &l.ThreadID, &l.Ref, &l.Label, &l.CreatedAt)
}

// ListProjectLinks returns a project's links of one kind, or of every kind
// for an empty kind, oldest first.
func (d *DB) ListProjectLinks(ctx context.Context, projectID int64, kind string) ([]ProjectLink, error) {
	query := `SELECT ` + linkColumns + ` FROM project_links WHERE project_id = ?`
	args := []any{projectID}
	if kind != "" {
		query += ` AND kind = ?`
		args = append(args, kind)
	}
	return d.queryLinks(ctx, query+` ORDER BY id`, args...)
}

// ListAllProjectLinks returns the links of one kind across every project.
func (d *DB) ListAllProjectLinks(ctx context.Context, kind string) ([]ProjectLink, error) {
	return d.queryLinks(ctx, `SELECT `+linkColumns+` FROM project_links WHERE kind = ? ORDER BY project_id, id`, kind)
}

// ListCardLinks returns the links a card holds, oldest first.
func (d *DB) ListCardLinks(ctx context.Context, taskUID string) ([]ProjectLink, error) {
	if taskUID == "" {
		return nil, nil
	}
	return d.queryLinks(ctx, `SELECT `+linkColumns+` FROM project_links WHERE task_uid = ? ORDER BY id`, taskUID)
}

// GetProjectLink returns one link, or ErrProjectNotFound when it is gone.
func (d *DB) GetProjectLink(ctx context.Context, id int64) (*ProjectLink, error) {
	links, err := d.queryLinks(ctx, `SELECT `+linkColumns+` FROM project_links WHERE id = ?`, id)
	if err != nil {
		return nil, err
	}
	if len(links) == 0 {
		return nil, ErrProjectNotFound
	}
	return &links[0], nil
}

func (d *DB) queryLinks(ctx context.Context, query string, args ...any) ([]ProjectLink, error) {
	rows, err := d.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: list project links: %w", err)
	}
	defer rows.Close()
	var out []ProjectLink
	for rows.Next() {
		var l ProjectLink
		if err := rows.Scan(&l.ID, &l.ProjectID, &l.TaskUID, &l.Kind, &l.AccountID, &l.ThreadID, &l.Ref, &l.Label, &l.CreatedAt); err != nil {
			return nil, fmt.Errorf("storage: scan project link: %w", err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// DeleteProjectLink removes one link.
func (d *DB) DeleteProjectLink(ctx context.Context, id int64) error {
	if _, err := d.sql.ExecContext(ctx, `DELETE FROM project_links WHERE id = ?`, id); err != nil {
		return fmt.Errorf("storage: delete project link %d: %w", id, err)
	}
	return nil
}

// DeleteProjectLinksByRef removes a project's links of one kind to ref, the
// project's own and its cards'.
func (d *DB) DeleteProjectLinksByRef(ctx context.Context, projectID int64, kind, ref string) error {
	if _, err := d.sql.ExecContext(ctx, `DELETE FROM project_links WHERE project_id = ? AND kind = ? AND ref = ?`, projectID, kind, ref); err != nil {
		return fmt.Errorf("storage: delete project links: %w", err)
	}
	return nil
}

// linkedThreadExpr is the conversation a mail link is in today: the one its
// Message-ID maps to, or the one it was made in.
const linkedThreadExpr = `coalesce((SELECT k.thread_id FROM thread_keys k WHERE k.account_id = l.account_id AND k.key = l.ref), l.thread_id)`

// ProjectThreads returns the conversations linked to projects, each pair
// once. projectID 0 means every project.
func (d *DB) ProjectThreads(ctx context.Context, projectID int64) ([]ProjectThread, error) {
	query := `SELECT DISTINCT ` + linkedThreadExpr + ` AS tid, l.project_id FROM project_links l WHERE l.kind = 'mail'`
	var args []any
	if projectID != 0 {
		query += ` AND l.project_id = ?`
		args = append(args, projectID)
	}
	rows, err := d.sql.QueryContext(ctx, query+` ORDER BY l.project_id, tid`, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: project threads: %w", err)
	}
	defer rows.Close()
	var out []ProjectThread
	for rows.Next() {
		var t ProjectThread
		if err := rows.Scan(&t.ThreadID, &t.ProjectID); err != nil {
			return nil, fmt.Errorf("storage: scan project thread: %w", err)
		}
		if t.ThreadID > 0 {
			out = append(out, t)
		}
	}
	return out, rows.Err()
}

// DeleteProjectThread removes every mail link of a project that leads to a
// conversation, the project's own and its cards'.
func (d *DB) DeleteProjectThread(ctx context.Context, projectID, threadID int64) error {
	if _, err := d.sql.ExecContext(ctx, `DELETE FROM project_links WHERE id IN (
SELECT l.id FROM project_links l WHERE l.project_id = ? AND l.kind = 'mail' AND `+linkedThreadExpr+` = ?)`, projectID, threadID); err != nil {
		return fmt.Errorf("storage: remove conversation from project: %w", err)
	}
	return nil
}

// LatestThreadMessages returns the newest message of each conversation, held
// in one of folderIDs, newest first. Hidden rows are left out.
func (d *DB) LatestThreadMessages(ctx context.Context, threadIDs, folderIDs []int64) ([]Message, error) {
	if len(threadIDs) == 0 || len(folderIDs) == 0 {
		return nil, nil
	}
	threadMarks, args := inClause(threadIDs)
	folderMarks, folderArgs := inClause(folderIDs)
	args = append(args, folderArgs...)
	ids, err := queryInt64s(ctx, d.sql, `
SELECT id FROM (
  SELECT id, date, uid, ROW_NUMBER() OVER (PARTITION BY thread_id ORDER BY date DESC, uid DESC, id DESC) AS rn
  FROM messages
  WHERE thread_id IN (`+threadMarks+`) AND folder_id IN (`+folderMarks+`) AND pending_delete = 0 AND snooze_hidden = 0
) WHERE rn = 1 ORDER BY date DESC, uid DESC, id DESC`, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: latest thread messages: %w", err)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	byID, err := d.messagesByID(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]Message, 0, len(ids))
	for _, id := range ids {
		if m, ok := byID[id]; ok {
			out = append(out, m)
		}
	}
	return out, nil
}

// MessageThreadKey returns what a mail link to a message's conversation
// stores: its account, its Message-ID without angle brackets (or '#' and the
// conversation id when it has none) and its conversation.
func (d *DB) MessageThreadKey(ctx context.Context, messageID int64) (accountID int64, ref string, threadID int64, subject string, err error) {
	var header string
	err = d.sql.QueryRowContext(ctx, `SELECT account_id, message_id, thread_id, subject FROM messages WHERE id = ?`, messageID).
		Scan(&accountID, &header, &threadID, &subject)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", 0, "", ErrMessageNotFound
	}
	if err != nil {
		return 0, "", 0, "", fmt.Errorf("storage: message key %d: %w", messageID, err)
	}
	ref = threadKey(header)
	if ref == "" {
		ref = fmt.Sprintf("#%d", threadID)
	}
	return accountID, ref, threadID, subject, nil
}

// ThreadOfHeader returns the conversation a Message-ID belongs to in an
// account, 0 when none is known.
func (d *DB) ThreadOfHeader(ctx context.Context, accountID int64, header string) (int64, error) {
	var id int64
	err := d.sql.QueryRowContext(ctx, `SELECT thread_id FROM thread_keys WHERE account_id = ? AND key = ?`, accountID, threadKey(header)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("storage: thread of header: %w", err)
	}
	return id, nil
}

// --- notes -------------------------------------------------------------------

// CreateProjectNote adds a note to a project and returns it with its id.
func (d *DB) CreateProjectNote(ctx context.Context, n *ProjectNote) error {
	now := nowText()
	res, err := d.sql.ExecContext(ctx, `INSERT INTO project_notes (project_id, title, body, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		n.ProjectID, n.Title, n.Body, now, now)
	if err != nil {
		return fmt.Errorf("storage: insert project note: %w", err)
	}
	if n.ID, err = res.LastInsertId(); err != nil {
		return fmt.Errorf("storage: project note id: %w", err)
	}
	n.CreatedAt, n.UpdatedAt = now, now
	return nil
}

const projectNoteColumns = `id, project_id, title, body, created_at, updated_at, vault_path, migrated_at`

// ListProjectNotes returns a project's notes that were not moved into the
// notes folder yet, the last edited first.
func (d *DB) ListProjectNotes(ctx context.Context, projectID int64) ([]ProjectNote, error) {
	return d.queryNotes(ctx, `SELECT `+projectNoteColumns+` FROM project_notes
WHERE project_id = ? AND migrated_at = '' ORDER BY updated_at DESC, id DESC`, projectID)
}

// PendingProjectNotes returns every note of every project that was not moved
// into the notes folder yet, oldest first.
func (d *DB) PendingProjectNotes(ctx context.Context) ([]ProjectNote, error) {
	return d.queryNotes(ctx, `SELECT `+projectNoteColumns+` FROM project_notes WHERE migrated_at = '' ORDER BY id`)
}

// SetProjectNoteVaultPath records the file a note is about to be written to.
func (d *DB) SetProjectNoteVaultPath(ctx context.Context, id int64, path string) error {
	res, err := d.sql.ExecContext(ctx, `UPDATE project_notes SET vault_path = ? WHERE id = ?`, path, id)
	if err != nil {
		return fmt.Errorf("storage: set project note path %d: %w", id, err)
	}
	return requireOneRow(res, ErrProjectNoteNotFound)
}

// FinishProjectNoteMigration marks a note as moved to path in the notes
// folder and points the card links that named it by row id at path instead,
// in one transaction. A card that already links to path keeps that one link.
func (d *DB) FinishProjectNoteMigration(ctx context.Context, id int64, path string) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("storage: begin note migration: %w", err)
	}
	defer tx.Rollback()
	var projectID int64
	if err := tx.QueryRowContext(ctx, `SELECT project_id FROM project_notes WHERE id = ?`, id).Scan(&projectID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrProjectNoteNotFound
		}
		return fmt.Errorf("storage: read project note: %w", err)
	}
	old := fmt.Sprint(id)
	if _, err := tx.ExecContext(ctx, `UPDATE OR IGNORE project_links SET ref = ? WHERE project_id = ? AND kind = 'note' AND ref = ?`, path, projectID, old); err != nil {
		return fmt.Errorf("storage: repoint note links: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM project_links WHERE project_id = ? AND kind = 'note' AND ref = ?`, projectID, old); err != nil {
		return fmt.Errorf("storage: drop duplicate note links: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE project_notes SET vault_path = ?, migrated_at = ? WHERE id = ?`, path, nowText(), id); err != nil {
		return fmt.Errorf("storage: mark project note migrated: %w", err)
	}
	return tx.Commit()
}

// RepointNoteLinks follows notes that moved in the notes folder: every note
// link to an old path in moves names the new path after it. A card that
// already links to the new path keeps that one link. It reports whether any
// link changed.
func (d *DB) RepointNoteLinks(ctx context.Context, moves map[string]string) (bool, error) {
	if len(moves) == 0 {
		return false, nil
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("storage: begin note link move: %w", err)
	}
	defer tx.Rollback()
	changed := false
	for from, to := range moves {
		res, err := tx.ExecContext(ctx, `UPDATE OR IGNORE project_links SET ref = ? WHERE kind = 'note' AND ref = ?`, to, from)
		if err != nil {
			return false, fmt.Errorf("storage: move note links: %w", err)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			changed = true
		}
		res, err = tx.ExecContext(ctx, `DELETE FROM project_links WHERE kind = 'note' AND ref = ?`, from)
		if err != nil {
			return false, fmt.Errorf("storage: drop duplicate note links: %w", err)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			changed = true
		}
	}
	return changed, tx.Commit()
}

// GetProjectNote returns one note, or ErrProjectNoteNotFound.
func (d *DB) GetProjectNote(ctx context.Context, id int64) (*ProjectNote, error) {
	notes, err := d.queryNotes(ctx, `SELECT `+projectNoteColumns+` FROM project_notes WHERE id = ?`, id)
	if err != nil {
		return nil, err
	}
	if len(notes) == 0 {
		return nil, ErrProjectNoteNotFound
	}
	return &notes[0], nil
}

func (d *DB) queryNotes(ctx context.Context, query string, args ...any) ([]ProjectNote, error) {
	rows, err := d.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: list project notes: %w", err)
	}
	defer rows.Close()
	var out []ProjectNote
	for rows.Next() {
		var n ProjectNote
		if err := rows.Scan(&n.ID, &n.ProjectID, &n.Title, &n.Body, &n.CreatedAt, &n.UpdatedAt, &n.VaultPath, &n.MigratedAt); err != nil {
			return nil, fmt.Errorf("storage: scan project note: %w", err)
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// UpdateProjectNote saves a note's title and body.
func (d *DB) UpdateProjectNote(ctx context.Context, n *ProjectNote) error {
	n.UpdatedAt = nowText()
	res, err := d.sql.ExecContext(ctx, `UPDATE project_notes SET title = ?, body = ?, updated_at = ? WHERE id = ?`, n.Title, n.Body, n.UpdatedAt, n.ID)
	if err != nil {
		return fmt.Errorf("storage: update project note %d: %w", n.ID, err)
	}
	return requireOneRow(res, ErrProjectNoteNotFound)
}

// DeleteProjectNote removes a note and the card links to it.
func (d *DB) DeleteProjectNote(ctx context.Context, id int64) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("storage: begin note delete: %w", err)
	}
	defer tx.Rollback()
	var projectID int64
	if err := tx.QueryRowContext(ctx, `SELECT project_id FROM project_notes WHERE id = ?`, id).Scan(&projectID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrProjectNoteNotFound
		}
		return fmt.Errorf("storage: read project note: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM project_links WHERE project_id = ? AND kind = 'note' AND ref = ?`, projectID, fmt.Sprint(id)); err != nil {
		return fmt.Errorf("storage: delete note links: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM project_notes WHERE id = ?`, id); err != nil {
		return fmt.Errorf("storage: delete project note %d: %w", id, err)
	}
	return tx.Commit()
}

// --- suggestions -------------------------------------------------------------

// DismissProjectSuggestion remembers that a conversation is not to be
// suggested for a project again.
func (d *DB) DismissProjectSuggestion(ctx context.Context, threadID int64) error {
	if _, err := d.sql.ExecContext(ctx, `INSERT OR IGNORE INTO project_dismissals (thread_id, created_at) VALUES (?, ?)`, threadID, nowText()); err != nil {
		return fmt.Errorf("storage: dismiss suggestion: %w", err)
	}
	return nil
}

// ListProjectDismissals returns the conversations whose suggestion was
// dismissed.
func (d *DB) ListProjectDismissals(ctx context.Context) ([]int64, error) {
	return queryInt64s(ctx, d.sql, `SELECT thread_id FROM project_dismissals ORDER BY thread_id`)
}

func scanProject(row rowScanner) (*Project, error) {
	var p Project
	if err := row.Scan(&p.ID, &p.UID, &p.Name, &p.Color, &p.Icon, &p.Description, &p.Status, &p.StartDate, &p.DueDate,
		&p.Position, &p.ListID, &p.CreatedAt, &p.UpdatedAt, &p.ArchivedAt); err != nil {
		return nil, err
	}
	return &p, nil
}
