package desktop

// NoteDTO is a private note. Body is empty when there is none; UpdatedAt is
// rfc3339, empty along with it.
type NoteDTO struct {
	Body      string `json:"body"`
	UpdatedAt string `json:"updatedAt"`
}

// GetNote returns the private note on a message, or on its conversation when
// conversation is set.
func (a *App) GetNote(messageID int64, conversation bool) (NoteDTO, error) {
	if err := a.ready(); err != nil {
		return NoteDTO{}, err
	}
	n, err := a.store.GetNote(a.ctx, messageID, conversation)
	if err != nil || n == nil {
		return NoteDTO{}, err
	}
	return NoteDTO{Body: n.Body, UpdatedAt: formatDate(n.UpdatedAt)}, nil
}

// SaveNote writes the private note on a message, or on its conversation when
// conversation is set. A blank body deletes it. Notes stay on this device:
// they are never synced, sent or added to the message.
func (a *App) SaveNote(messageID int64, conversation bool, body string) (NoteDTO, error) {
	if err := a.ready(); err != nil {
		return NoteDTO{}, err
	}
	n, err := a.store.SaveNote(a.ctx, messageID, conversation, body)
	if err != nil || n == nil {
		return NoteDTO{}, err
	}
	return NoteDTO{Body: n.Body, UpdatedAt: formatDate(n.UpdatedAt)}, nil
}

// DeleteNote removes the private note on a message, or on its conversation
// when conversation is set.
func (a *App) DeleteNote(messageID int64, conversation bool) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.store.DeleteNote(a.ctx, messageID, conversation)
}
