package scripting

import (
	"context"
	"errors"
	"time"
)

// ErrUnavailable is returned by a host function whose host the app did not
// provide.
var ErrUnavailable = errors.New("scripting: not available in this app")

// Hosts is how scripts reach the app. A nil member makes the functions that
// need it fail with ErrUnavailable rather than crash.
type Hosts struct {
	Mail    MailHost
	Notify  NotifyHost
	Confirm Confirmer
	// Now is the clock, time.Now when nil.
	Now func() time.Time
}

func (h *Hosts) now() time.Time {
	if h == nil || h.Now == nil {
		return time.Now()
	}
	return h.Now()
}

// Message is a message as scripts see it.
type Message struct {
	ID        int64
	AccountID int64
	// Account is the account's address.
	Account string
	// Folder is the folder's path and FolderRole its role (inbox, archive,
	// trash, junk, sent, drafts), empty when it has none.
	Folder         string
	FolderRole     string
	Subject        string
	From           string
	FromName       string
	To             string
	Cc             string
	ReplyTo        string
	Date           time.Time
	Size           int64
	HasAttachments bool
	Read           bool
	Flagged        bool
	Pinned         bool
	Color          int
	// Headers holds the headers the app keeps, by lower case name. Raw
	// headers are not stored, so this is a fixed set.
	Headers map[string]string
}

// Draft is a message a script writes. ReplyTo or Forward name the local id of
// the message being answered or forwarded, and the host fills the threading,
// subject and recipients it implies.
type Draft struct {
	AccountID int64
	To        []string
	Cc        []string
	Bcc       []string
	Subject   string
	Body      string
	ReplyTo   int64
	ReplyAll  bool
	Forward   int64
}

// MailHost is the mail a script can read and change. Every method acts as
// the user would through the app, so the same server round trips, undo
// records and checks apply. Folder arguments are a path or a role, resolved
// in the message's own account.
type MailHost interface {
	Message(ctx context.Context, id int64) (Message, error)
	Body(ctx context.Context, id int64) (string, error)
	Move(ctx context.Context, id int64, folder string) error
	// Copy puts a copy of the message in another folder of its account.
	Copy(ctx context.Context, id int64, folder string) error
	Archive(ctx context.Context, id int64) error
	// Delete moves to the trash; it never expunges.
	Delete(ctx context.Context, id int64) error
	SetRead(ctx context.Context, id int64, read bool) error
	SetFlagged(ctx context.Context, id int64, flagged bool) error
	SetColor(ctx context.Context, id int64, color int) error
	SetPinned(ctx context.Context, id int64, pinned bool) error
	Snooze(ctx context.Context, id int64, until time.Time) error
	// Compose saves the draft, or queues it for sending when send is set,
	// and returns its id.
	Compose(ctx context.Context, d Draft, send bool) (int64, error)
}

// NotifyHost shows a script's messages to the user.
type NotifyHost interface {
	// Toast shows a short in-app message; level is info, success, warning or
	// error.
	Toast(ctx context.Context, script, text, level string)
	// System posts an operating system notification.
	System(ctx context.Context, title, body string) error
}

// ConfirmRequest is a risky call waiting for the user's yes.
type ConfirmRequest struct {
	ScriptID   int64  `json:"scriptId"`
	ScriptName string `json:"scriptName"`
	// Function is the full Lua name, e.g. "mail.send".
	Function string `json:"function"`
	// Summary says what the call would do, with its arguments.
	Summary string `json:"summary"`
}

// ConfirmAnswer is the user's answer. Always means "allow without asking"
// for this function in this script from now on.
type ConfirmAnswer struct {
	Allow  bool
	Always bool
}

// Confirmer asks the user about a risky call. It blocks until they answer or
// ctx ends, which counts as no.
type Confirmer interface {
	Confirm(ctx context.Context, req ConfirmRequest) (ConfirmAnswer, error)
}

// Object is a structured value of a registered value type, e.g. a message.
// The engine hands it to Lua as a table, with the type's lazy fields fetched
// on first use.
type Object struct {
	Type   string
	Fields map[string]any
}

// MessageObject turns a message into the value scripts get.
func MessageObject(m Message) Object {
	headers := map[string]any{}
	for k, v := range m.Headers {
		headers[k] = v
	}
	return Object{Type: "message", Fields: map[string]any{
		"id":              m.ID,
		"account_id":      m.AccountID,
		"account":         m.Account,
		"folder":          m.Folder,
		"folder_role":     m.FolderRole,
		"subject":         m.Subject,
		"from":            m.From,
		"from_name":       m.FromName,
		"to":              m.To,
		"cc":              m.Cc,
		"reply_to":        m.ReplyTo,
		"date":            m.Date.Unix(),
		"size":            m.Size,
		"has_attachments": m.HasAttachments,
		"read":            m.Read,
		"flagged":         m.Flagged,
		"pinned":          m.Pinned,
		"color":           m.Color,
		"headers":         headers,
	}}
}

// SampleMessage is the message a test run uses when the user picks none. Its
// id is 0, which no stored message has.
func SampleMessage(now time.Time) Message {
	return Message{
		Account:        "you@example.org",
		Folder:         "INBOX",
		FolderRole:     "inbox",
		Subject:        "Invoice 2026-114 for November",
		From:           "billing@example.com",
		FromName:       "Example Billing",
		To:             "you@example.org",
		Date:           now.Add(-2 * time.Hour),
		Size:           48213,
		HasAttachments: true,
		Headers: map[string]string{
			"list-id":  "",
			"reply-to": "",
		},
	}
}

// sampleBody is the body of SampleMessage.
const sampleBody = "Hello,\n\nplease find attached invoice 2026-114 for November.\nThe amount of 120.00 EUR is due on 1 December.\n\nKind regards\nExample Billing\n"

// PrepareContext is what a trigger's Prepare gets to build the event.
type PrepareContext struct {
	Ctx   context.Context
	Hosts *Hosts
}
