package desktop

import (
	"errors"
	"strings"
	"unicode"

	"github.com/peltonapp/Pelton/internal/storage"
)

// snippets: reusable text blocks with placeholders, managed in settings and
// inserted from the composer. The store keeps them in the user's order; these
// bindings check names and trigger words on the way in.

// ErrSnippetNameEmpty is returned when a snippet is saved without a name.
var ErrSnippetNameEmpty = errors.New("pelton: a snippet needs a name")

// ErrSnippetTriggerInvalid is returned when a trigger word holds anything but
// letters, digits, dashes and underscores.
var ErrSnippetTriggerInvalid = errors.New("pelton: a trigger is one word of letters, digits, - or _")

// ErrSnippetTriggerTaken is returned when another snippet already uses the
// trigger word.
var ErrSnippetTriggerTaken = errors.New("pelton: another snippet already uses this trigger")

// SnippetDTO mirrors storage.Snippet for the frontend. To and Cc are address
// lists as typed; Trigger is stored without its prefix.
type SnippetDTO struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Body     string `json:"body"`
	Subject  string `json:"subject"`
	To       string `json:"to"`
	Cc       string `json:"cc"`
	Trigger  string `json:"trigger"`
	Position int    `json:"position"`
}

func toSnippetDTO(s storage.Snippet) SnippetDTO {
	return SnippetDTO{ID: s.ID, Name: s.Name, Body: s.Body, Subject: s.Subject, To: s.To, Cc: s.Cc, Trigger: s.Trigger, Position: s.Position}
}

// normalizeTrigger trims a trigger word, drops a leading ";" the user may have
// typed with it, and lowercases it, so ";Thanks" and "thanks" are one trigger.
func normalizeTrigger(raw string) (string, error) {
	word := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(raw), ";"))
	for _, r := range word {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '_' {
			return "", ErrSnippetTriggerInvalid
		}
	}
	return word, nil
}

// ListSnippets returns every snippet in the user's order.
func (a *App) ListSnippets() ([]SnippetDTO, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	list, err := a.store.ListSnippets(a.ctx)
	if err != nil {
		return nil, err
	}
	out := make([]SnippetDTO, 0, len(list))
	for _, s := range list {
		out = append(out, toSnippetDTO(s))
	}
	return out, nil
}

// SaveSnippet creates the snippet when ID is 0 and updates it otherwise,
// returning it as stored. The name must not be empty and the trigger, when
// set, must be one word no other snippet uses.
func (a *App) SaveSnippet(dto SnippetDTO) (SnippetDTO, error) {
	if err := a.ready(); err != nil {
		return SnippetDTO{}, err
	}
	name := strings.TrimSpace(dto.Name)
	if name == "" {
		return SnippetDTO{}, ErrSnippetNameEmpty
	}
	trigger, err := normalizeTrigger(dto.Trigger)
	if err != nil {
		return SnippetDTO{}, err
	}
	if trigger != "" {
		existing, err := a.store.ListSnippets(a.ctx)
		if err != nil {
			return SnippetDTO{}, err
		}
		for _, s := range existing {
			if s.ID != dto.ID && s.Trigger == trigger {
				return SnippetDTO{}, ErrSnippetTriggerTaken
			}
		}
	}
	s := storage.Snippet{
		ID:      dto.ID,
		Name:    name,
		Body:    dto.Body,
		Subject: strings.TrimSpace(dto.Subject),
		To:      strings.TrimSpace(dto.To),
		Cc:      strings.TrimSpace(dto.Cc),
		Trigger: trigger,
	}
	if s.ID == 0 {
		if _, err := a.store.CreateSnippet(a.ctx, &s); err != nil {
			return SnippetDTO{}, err
		}
	} else {
		if err := a.store.UpdateSnippet(a.ctx, &s); err != nil {
			return SnippetDTO{}, err
		}
		s.Position = dto.Position
	}
	return toSnippetDTO(s), nil
}

// DeleteSnippet removes a snippet.
func (a *App) DeleteSnippet(id int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.store.DeleteSnippet(a.ctx, id)
}

// ReorderSnippets stores the order the snippets are listed in.
func (a *App) ReorderSnippets(ids []int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.store.ReorderSnippets(a.ctx, ids)
}

// snippetBackup is one snippet as exported, travelling with the signatures.
type snippetBackup struct {
	Name    string `json:"name"`
	Body    string `json:"body"`
	Subject string `json:"subject,omitempty"`
	To      string `json:"to,omitempty"`
	Cc      string `json:"cc,omitempty"`
	Trigger string `json:"trigger,omitempty"`
}

// exportSnippets returns every snippet in order for the backup.
func (a *App) exportSnippets() ([]snippetBackup, error) {
	list, err := a.store.ListSnippets(a.ctx)
	if err != nil {
		return nil, err
	}
	out := make([]snippetBackup, 0, len(list))
	for _, s := range list {
		out = append(out, snippetBackup{Name: s.Name, Body: s.Body, Subject: s.Subject, To: s.To, Cc: s.Cc, Trigger: s.Trigger})
	}
	return out, nil
}

// importSnippets recreates snippets from a backup after the local ones,
// skipping a name already present so a re-import never duplicates one, and
// dropping a trigger another snippet already uses.
func (a *App) importSnippets(snippets []snippetBackup) error {
	if len(snippets) == 0 {
		return nil
	}
	existing, err := a.store.ListSnippets(a.ctx)
	if err != nil {
		return err
	}
	names := make(map[string]bool, len(existing))
	triggers := make(map[string]bool, len(existing))
	for _, s := range existing {
		names[s.Name] = true
		if s.Trigger != "" {
			triggers[s.Trigger] = true
		}
	}
	for _, b := range snippets {
		name := strings.TrimSpace(b.Name)
		if name == "" || names[name] {
			continue
		}
		trigger, err := normalizeTrigger(b.Trigger)
		if err != nil || triggers[trigger] {
			trigger = ""
		}
		s := storage.Snippet{Name: name, Body: b.Body, Subject: b.Subject, To: b.To, Cc: b.Cc, Trigger: trigger}
		if _, err := a.store.CreateSnippet(a.ctx, &s); err != nil {
			return err
		}
		names[name] = true
		if trigger != "" {
			triggers[trigger] = true
		}
	}
	return nil
}
