package desktop

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/chat"
	"github.com/peltonapp/Pelton/internal/scripting/spaces"
	"github.com/peltonapp/Pelton/internal/storage"
)

// scriptContacts is the address book a script reaches.
type scriptContacts struct {
	app *App
}

func scriptContactOf(c storage.Contact, books map[int64]string) spaces.Contact {
	out := spaces.Contact{ID: c.ID, BookID: c.BookID, Book: books[c.BookID], Name: c.FullName,
		Organization: c.Organization, Title: c.Title, Note: c.Note}
	for _, e := range c.Emails {
		out.Emails = append(out.Emails, e.Value)
	}
	for _, p := range c.Phones {
		out.Phones = append(out.Phones, p.Value)
	}
	return out
}

func (s *scriptContacts) books() map[int64]string {
	out := map[int64]string{}
	if list, err := s.app.ListAddressBooks(); err == nil {
		for _, b := range list {
			out[b.ID] = b.Name
		}
	}
	return out
}

func (s *scriptContacts) all() ([]storage.Contact, error) {
	if err := s.app.ready(); err != nil {
		return nil, err
	}
	return s.app.store.ListContacts(s.app.ctx, 0)
}

func (s *scriptContacts) Contacts(_ context.Context, query string, limit int) ([]spaces.Contact, error) {
	list, err := s.all()
	if err != nil {
		return nil, err
	}
	books := s.books()
	q := strings.ToLower(strings.TrimSpace(query))
	var out []spaces.Contact
	for _, c := range list {
		text := c.FullName + "\n" + c.Organization
		for _, e := range c.Emails {
			text += "\n" + e.Value
		}
		if q != "" && !strings.Contains(strings.ToLower(text), q) {
			continue
		}
		out = append(out, scriptContactOf(c, books))
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (s *scriptContacts) Contact(_ context.Context, id int64) (spaces.Contact, error) {
	if err := s.app.ready(); err != nil {
		return spaces.Contact{}, err
	}
	c, err := s.app.store.GetContact(s.app.ctx, id)
	if err != nil {
		return spaces.Contact{}, err
	}
	return scriptContactOf(*c, s.books()), nil
}

func (s *scriptContacts) FindByEmail(_ context.Context, email string) (spaces.Contact, bool, error) {
	list, err := s.all()
	if err != nil {
		return spaces.Contact{}, false, err
	}
	for _, c := range list {
		for _, e := range c.Emails {
			if strings.EqualFold(strings.TrimSpace(e.Value), email) {
				return scriptContactOf(c, s.books()), true, nil
			}
		}
	}
	return spaces.Contact{}, false, nil
}

func (s *scriptContacts) CreateContact(ctx context.Context, in spaces.ContactInput) (spaces.Contact, error) {
	a := s.app
	if in.BookID == 0 {
		books, err := a.ListAddressBooks()
		if err != nil {
			return spaces.Contact{}, err
		}
		for _, b := range books {
			if !b.ReadOnly {
				in.BookID = b.ID
				break
			}
		}
		if in.BookID == 0 {
			return spaces.Contact{}, fmt.Errorf("pelton: there is no address book to add contacts to")
		}
	}
	res, err := a.saveContact(ContactRequest{
		BookID: in.BookID, FullName: in.Name, Organization: in.Organization, Note: in.Note,
		Emails: []ContactValueDTO{{Value: in.Email}}, Phones: []ContactValueDTO{},
	})
	if err != nil {
		return spaces.Contact{}, err
	}
	if res.Conflict {
		return spaces.Contact{}, fmt.Errorf("pelton: the address book refused the new contact")
	}
	a.scriptContactAdded(ctx, res.Saved.ID)
	return s.Contact(ctx, res.Saved.ID)
}

// scriptContactAdded reports a contact added here.
func (a *App) scriptContactAdded(ctx context.Context, id int64) {
	a.scriptEvent(ctx, spaces.TriggerContactAdded, map[string]any{"contact_id": id})
}

// scriptProjects is the project boards a script reaches.
type scriptProjects struct {
	app *App
}

func (s *scriptProjects) columns(projectID int64) ([]storage.ProjectColumn, error) {
	if err := s.app.ready(); err != nil {
		return nil, err
	}
	return s.app.store.ListProjectColumns(s.app.ctx, projectID)
}

func (s *scriptProjects) cardOf(dto ProjectCardDTO) spaces.Card {
	a := s.app
	card := spaces.Card{
		TaskID: dto.Task.ID, UID: dto.Task.UID, Title: dto.Task.Title, Notes: dto.Task.Notes,
		ProjectID: dto.ProjectID, Due: taskDueTime(dto.Task.Due), Done: dto.Task.Done,
	}
	if col, err := a.store.GetProjectColumn(a.ctx, dto.ColumnID); err == nil {
		card.Column = col.Name
	}
	if p, err := a.store.GetProject(a.ctx, dto.ProjectID); err == nil {
		card.Project = p.Name
	}
	return card
}

func (s *scriptProjects) Projects(context.Context) ([]spaces.Project, error) {
	list, err := s.app.ListProjects()
	if err != nil {
		return nil, err
	}
	out := make([]spaces.Project, 0, len(list))
	for _, p := range list {
		cols, err := s.columns(p.ID)
		if err != nil {
			return nil, err
		}
		names := make([]string, 0, len(cols))
		for _, c := range cols {
			names = append(names, c.Name)
		}
		out = append(out, spaces.Project{ID: p.ID, Name: p.Name, Status: p.Status, Columns: names})
	}
	return out, nil
}

func (s *scriptProjects) Cards(_ context.Context, projectID int64) ([]spaces.Card, error) {
	board, err := s.app.GetProjectBoard(projectID)
	if err != nil {
		return nil, err
	}
	out := make([]spaces.Card, 0, len(board.Cards))
	for _, c := range board.Cards {
		out = append(out, s.cardOf(c))
	}
	return out, nil
}

func (s *scriptProjects) Card(_ context.Context, uid string) (spaces.Card, error) {
	if err := s.app.ready(); err != nil {
		return spaces.Card{}, err
	}
	dto, err := s.app.cardOf(uid)
	if err != nil {
		return spaces.Card{}, err
	}
	return s.cardOf(dto), nil
}

// column finds a board's column by name, or for "" its first open one.
func (s *scriptProjects) column(projectID int64, name string) (storage.ProjectColumn, error) {
	cols, err := s.columns(projectID)
	if err != nil {
		return storage.ProjectColumn{}, err
	}
	name = strings.TrimSpace(name)
	for _, c := range cols {
		if (name == "" && !c.Done) || (name != "" && strings.EqualFold(c.Name, name)) {
			return c, nil
		}
	}
	if name == "" && len(cols) > 0 {
		return cols[0], nil
	}
	return storage.ProjectColumn{}, fmt.Errorf("pelton: the board has no column %q", name)
}

func (s *scriptProjects) CreateCard(ctx context.Context, in spaces.CardInput) (spaces.Card, error) {
	col, err := s.column(in.ProjectID, in.Column)
	if err != nil {
		return spaces.Card{}, err
	}
	dto, err := s.app.createProjectCard(ProjectCardRequest{
		ProjectID: in.ProjectID, ColumnID: col.ID, Title: in.Title, Notes: in.Notes, Due: taskDueText(in.Due), Index: -1,
	})
	if err != nil {
		return spaces.Card{}, err
	}
	s.app.scriptCardCreated(ctx, dto)
	return s.cardOf(dto), nil
}

func (s *scriptProjects) MoveCard(ctx context.Context, uid, column string) (spaces.Card, error) {
	if err := s.app.ready(); err != nil {
		return spaces.Card{}, err
	}
	before, err := s.app.cardOf(uid)
	if err != nil {
		return spaces.Card{}, err
	}
	col, err := s.column(before.ProjectID, column)
	if err != nil {
		return spaces.Card{}, err
	}
	if col.ID == before.ColumnID {
		return s.cardOf(before), nil
	}
	res, err := s.app.moveProjectCard(uid, col.ID, 1<<30)
	if err != nil {
		return spaces.Card{}, err
	}
	s.app.scriptCardMoved(ctx, before, res)
	return s.cardOf(res.Card), nil
}

func (s *scriptProjects) LinkMail(_ context.Context, projectID, messageID int64, cardUID string) error {
	_, err := s.app.AddMailToProject(projectID, []int64{messageID}, cardUID)
	return err
}

// scriptCardCreated reports a new card, which is a new task too.
func (a *App) scriptCardCreated(ctx context.Context, card ProjectCardDTO) {
	a.scriptEvent(ctx, spaces.TriggerCardCreated, map[string]any{"card_uid": card.Task.UID})
	a.scriptEvent(ctx, spaces.TriggerTaskCreated, map[string]any{"task_id": card.Task.ID})
}

// scriptCardMoved reports a card that changed column, and the task it
// completed by moving into the done column.
func (a *App) scriptCardMoved(ctx context.Context, before ProjectCardDTO, res ProjectMoveResultDTO) {
	if res.Card.ColumnID != before.ColumnID {
		from := ""
		if col, err := a.store.GetProjectColumn(a.ctx, before.ColumnID); err == nil {
			from = col.Name
		}
		a.scriptEvent(ctx, spaces.TriggerCardMoved, map[string]any{"card_uid": res.Card.Task.UID, "from_column": from})
	}
	if (res.Card.Task.Done && !before.Task.Done) || res.Advanced {
		a.scriptEvent(ctx, spaces.TriggerTaskCompleted, map[string]any{"task_id": res.Card.Task.ID})
	}
}

// scriptNotes is the notes space a script reaches.
type scriptNotes struct {
	app *App
}

func noteTitle(rel string) string {
	return strings.TrimSuffix(path.Base(rel), path.Ext(rel))
}

func (s *scriptNotes) Note(_ context.Context, rel string) (spaces.Note, error) {
	svc, err := s.app.openedVault()
	if err != nil {
		return spaces.Note{}, err
	}
	f, err := svc.Read(rel)
	if err != nil {
		return spaces.Note{}, err
	}
	return spaces.Note{Path: f.Path, Title: noteTitle(f.Path), Content: f.Content, Modified: time.UnixMilli(f.MTime)}, nil
}

func (s *scriptNotes) SearchNotes(_ context.Context, query string, limit int) ([]spaces.Note, error) {
	svc, err := s.app.openedVault()
	if err != nil {
		return nil, err
	}
	hits, err := svc.Search(query, limit)
	if err != nil {
		return nil, err
	}
	out := make([]spaces.Note, 0, len(hits))
	for _, h := range hits {
		out = append(out, spaces.Note{Path: h.Path, Title: h.Title})
	}
	return out, nil
}

func (s *scriptNotes) CreateNote(ctx context.Context, rel, content string) (spaces.Note, error) {
	svc, err := s.app.openedVault()
	if err != nil {
		return spaces.Note{}, err
	}
	free, err := svc.UniquePath(rel)
	if err != nil {
		return spaces.Note{}, err
	}
	p, err := svc.Create(s.app.ctx, free, []byte(content))
	if err != nil {
		return spaces.Note{}, err
	}
	s.app.scriptNoteSaved(ctx, p)
	return spaces.Note{Path: p, Title: noteTitle(p)}, nil
}

// AppendNote adds text on a line of its own, trying again once when the note
// changed between reading and writing it.
func (s *scriptNotes) AppendNote(ctx context.Context, rel, text string) (spaces.Note, error) {
	svc, err := s.app.openedVault()
	if err != nil {
		return spaces.Note{}, err
	}
	for range 2 {
		f, err := svc.Read(rel)
		if errors.Is(err, fs.ErrNotExist) {
			p, err := svc.Create(s.app.ctx, rel, []byte(text))
			if err != nil {
				return spaces.Note{}, err
			}
			s.app.scriptNoteSaved(ctx, p)
			return spaces.Note{Path: p, Title: noteTitle(p)}, nil
		}
		if err != nil {
			return spaces.Note{}, err
		}
		content := f.Content
		if content != "" && !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		r, err := svc.Save(s.app.ctx, f.Path, content+text, f.Hash, false)
		if err != nil {
			return spaces.Note{}, err
		}
		if !r.Conflict {
			s.app.scriptNoteSaved(ctx, f.Path)
			return spaces.Note{Path: f.Path, Title: noteTitle(f.Path)}, nil
		}
	}
	return spaces.Note{}, fmt.Errorf("pelton: %s keeps changing; nothing was added", rel)
}

func (s *scriptNotes) DailyNotePath(_ context.Context, day time.Time) (string, error) {
	a := s.app
	folder := strings.Trim(strings.TrimSpace(a.stringSetting(settingNotesDailyFolder, "Daily")), "/")
	name := formatNoteDate(day, a.stringSetting(settingNotesDailyFormat, defaultDailyFormat))
	if folder == "" {
		return name + ".md", nil
	}
	return folder + "/" + name + ".md", nil
}

// scriptNoteSaved reports a note created or saved in Pelton.
func (a *App) scriptNoteSaved(ctx context.Context, rel string) {
	a.scriptEvent(ctx, spaces.TriggerNoteSaved, map[string]any{"note_path": rel})
}

// scriptFeeds is the feeds a script reaches.
type scriptFeeds struct {
	app *App
}

func (s *scriptFeeds) titles() map[int64]string {
	out := map[int64]string{}
	if list, err := s.app.store.ListFeeds(s.app.ctx); err == nil {
		for _, f := range list {
			out[f.ID] = f.Title
			if f.CustomTitle != "" {
				out[f.ID] = f.CustomTitle
			}
		}
	}
	return out
}

func scriptFeedItemOf(e storage.FeedEntry, titles map[int64]string) spaces.FeedItem {
	summary := e.Summary
	if summary == "" {
		summary = e.Snippet
	}
	return spaces.FeedItem{ID: e.ID, FeedID: e.FeedID, Feed: titles[e.FeedID], Title: e.Title, URL: e.URL,
		Author: e.Author, Published: e.Published, Summary: summary, Read: e.Read, Starred: e.Starred}
}

func (s *scriptFeeds) FeedItems(_ context.Context, f spaces.FeedFilter) ([]spaces.FeedItem, error) {
	if err := s.app.ready(); err != nil {
		return nil, err
	}
	titles := s.titles()
	q := storage.FeedEntryQuery{UnreadOnly: f.UnreadOnly, Search: f.Query, Limit: f.Limit}
	if f.Feed != "" {
		for id, t := range titles {
			if strings.EqualFold(t, strings.TrimSpace(f.Feed)) {
				q.FeedIDs = append(q.FeedIDs, id)
			}
		}
		if len(q.FeedIDs) == 0 {
			return nil, nil
		}
	}
	list, err := s.app.store.QueryFeedEntries(s.app.ctx, q)
	if err != nil {
		return nil, err
	}
	out := make([]spaces.FeedItem, 0, len(list))
	for _, e := range list {
		out = append(out, scriptFeedItemOf(e, titles))
	}
	return out, nil
}

func (s *scriptFeeds) FeedItem(_ context.Context, id int64) (spaces.FeedItem, error) {
	if err := s.app.ready(); err != nil {
		return spaces.FeedItem{}, err
	}
	e, err := s.app.store.GetFeedEntry(s.app.ctx, id)
	if err != nil {
		return spaces.FeedItem{}, err
	}
	return scriptFeedItemOf(*e, s.titles()), nil
}

func (s *scriptFeeds) SetFeedItemsRead(_ context.Context, ids []int64, read bool) error {
	_, err := s.app.SetFeedEntriesRead(ids, read)
	return err
}

func (s *scriptFeeds) SetFeedItemStarred(_ context.Context, id int64, starred bool) error {
	return s.app.SetFeedEntryStarred(id, starred)
}

// scriptChat is the chat a script reaches.
type scriptChat struct {
	app *App
}

func (s *scriptChat) Enabled() bool {
	return s.app.chatEnabled()
}

func (s *scriptChat) Rooms(context.Context) ([]spaces.ChatRoom, error) {
	rooms, err := s.app.ChatRooms()
	if err != nil {
		return nil, err
	}
	out := make([]spaces.ChatRoom, 0, len(rooms))
	for _, r := range rooms {
		out = append(out, spaces.ChatRoom{ID: r.ID, Name: r.Name, Direct: r.Kind == "direct"})
	}
	return out, nil
}

// Send finds the room by id, alias or name, and refuses a name two rooms
// share, so a message never goes to the wrong one.
func (s *scriptChat) Send(_ context.Context, room, text string) (string, error) {
	rooms, err := s.app.ChatRooms()
	if err != nil {
		return "", err
	}
	id := ""
	for _, r := range rooms {
		if r.ID == room || (r.Alias != "" && r.Alias == room) {
			id = r.ID
			break
		}
		if strings.EqualFold(r.Name, room) {
			if id != "" {
				return "", fmt.Errorf("pelton: more than one chat room is called %q; use its id", room)
			}
			id = r.ID
		}
	}
	if id == "" {
		return "", fmt.Errorf("pelton: no chat room %q", room)
	}
	return s.app.ChatSend(chat.SendRequest{RoomID: id, Text: text})
}
