package cliproto

// The data that crosses. The field names are what `pelton --json` prints, so
// they only ever gain fields; none is renamed or dropped.

// Status is the reply to status.
type Status struct {
	Version      string       `json:"version"`
	Protocol     int          `json:"protocol"`
	Access       bool         `json:"access"`
	Locked       bool         `json:"locked"`
	Capabilities []Capability `json:"capabilities"`
}

// Capability is one switch and whether it is on.
type Capability struct {
	Name  string `json:"name"`
	Label string `json:"label"`
	On    bool   `json:"on"`
}

// Account is one mail account.
type Account struct {
	ID    int64  `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

// Folder is one folder. Role is inbox, sent, drafts, archive, trash, junk or
// empty.
type Folder struct {
	ID        int64  `json:"id"`
	AccountID int64  `json:"accountId"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Role      string `json:"role"`
}

// Address is a name and an email address.
type Address struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// Message is one message as a list shows it. Date is RFC 3339.
type Message struct {
	ID          int64   `json:"id"`
	AccountID   int64   `json:"accountId"`
	FolderID    int64   `json:"folderId"`
	Folder      string  `json:"folder"`
	Date        string  `json:"date"`
	From        Address `json:"from"`
	Subject     string  `json:"subject"`
	Unread      bool    `json:"unread"`
	Flagged     bool    `json:"flagged"`
	Attachments bool    `json:"attachments"`
}

// MessageDetail is one message in full. Headers is the raw header block, set
// only when asked for; Raw is the whole source, set only for --raw.
type MessageDetail struct {
	Message
	To          string       `json:"to"`
	Cc          string       `json:"cc"`
	ReplyTo     string       `json:"replyTo"`
	MessageID   string       `json:"messageId"`
	Text        string       `json:"text"`
	HTML        string       `json:"html"`
	Headers     string       `json:"headers,omitempty"`
	Raw         string       `json:"raw,omitempty"`
	AttachList  []Attachment `json:"attachmentList"`
	VaultSealed bool         `json:"vault"`
}

// Attachment is one attachment of a message.
type Attachment struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	ContentType string `json:"contentType"`
	Size        int64  `json:"size"`
	Inline      bool   `json:"inline"`
}

// Result is the outcome of an action on one message. Code is empty when OK.
type Result struct {
	ID    int64  `json:"id"`
	OK    bool   `json:"ok"`
	Code  Code   `json:"code,omitempty"`
	Error string `json:"error,omitempty"`
}

// Sent is the reply to send and draft: the outbox id of a queued message,
// or the draft id of a saved one.
type Sent struct {
	OutboxID   int64  `json:"outboxId,omitempty"`
	DraftID    int64  `json:"draftId,omitempty"`
	Protection string `json:"protection"`
}

// Run is the reply to run.
type Run struct {
	ID         int64  `json:"id"`
	ScriptID   int64  `json:"scriptId"`
	ScriptName string `json:"scriptName"`
	Dry        bool   `json:"dry"`
	Status     string `json:"status"`
	Error      string `json:"error"`
	Changes    int    `json:"changes"`
	DurationMs int64  `json:"durationMs"`
	Steps      []Step `json:"steps"`
}

// Step is one step of a run.
type Step struct {
	Kind     string `json:"kind"`
	Function string `json:"function"`
	Summary  string `json:"summary"`
	Error    string `json:"error,omitempty"`
}

// Event is one calendar occurrence. Start and End are RFC 3339.
type Event struct {
	ID       int64  `json:"id"`
	Calendar int64  `json:"calendarId"`
	Summary  string `json:"summary"`
	Location string `json:"location"`
	Start    string `json:"start"`
	End      string `json:"end"`
	AllDay   bool   `json:"allDay"`
}

// Task is one task. Due is a date, an RFC 3339 time, or empty.
type Task struct {
	ID    int64  `json:"id"`
	List  string `json:"list"`
	Title string `json:"title"`
	Due   string `json:"due"`
	Done  bool   `json:"done"`
}

// Contact is one address book entry.
type Contact struct {
	Name    string `json:"name"`
	Email   string `json:"email"`
	Contact bool   `json:"contact"`
}

// ExportItem is one message of an export stream, one JSON object per line.
// Raw is the message source, base64 in JSON. Error is set instead for a
// message that could not be read. The stream ends with an item that has End
// set, so a cut stream is never taken for a whole one.
type ExportItem struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Date  string `json:"date"`
	Raw   []byte `json:"raw,omitempty"`
	Error string `json:"error,omitempty"`
	End   bool   `json:"end,omitempty"`
}

// The requests.

// FolderRequest names an account, by id or address, or none for all.
type FolderRequest struct {
	Account string `json:"account"`
}

// ListRequest selects messages of a folder: a folder id, path, name or role.
// An empty folder is the inbox of every account. Since is RFC 3339.
type ListRequest struct {
	Folder  string `json:"folder"`
	Account string `json:"account"`
	Unread  bool   `json:"unread"`
	Limit   int    `json:"limit"`
	Since   string `json:"since"`
}

// SearchRequest is a query in the app's search syntax.
type SearchRequest struct {
	Query   string `json:"query"`
	Account string `json:"account"`
	Limit   int    `json:"limit"`
}

// ShowRequest asks for one message; Headers and Raw fetch its source.
type ShowRequest struct {
	ID      int64 `json:"id"`
	Headers bool  `json:"headers"`
	Raw     bool  `json:"raw"`
}

// IDRequest names one message.
type IDRequest struct {
	ID int64 `json:"id"`
}

// AttachmentRequest names an attachment of a message by id or file name.
type AttachmentRequest struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// ActRequest applies one action to messages. To is the destination of move.
type ActRequest struct {
	Action string  `json:"action"`
	IDs    []int64 `json:"ids"`
	To     string  `json:"to"`
}

// OutAttachment is a file to attach, its bytes base64 in JSON.
type OutAttachment struct {
	Name        string `json:"name"`
	ContentType string `json:"contentType"`
	Content     []byte `json:"content"`
}

// SendRequest is a message to send or keep as a draft. From names the
// account by id or address; empty is the first account.
type SendRequest struct {
	From        string          `json:"from"`
	To          []string        `json:"to"`
	Cc          []string        `json:"cc"`
	Bcc         []string        `json:"bcc"`
	Subject     string          `json:"subject"`
	Body        string          `json:"body"`
	Attachments []OutAttachment `json:"attachments"`
}

// ComposeRequest opens a composer with these fields, or with a mailto: URL.
type ComposeRequest struct {
	Mailto  string   `json:"mailto"`
	To      []string `json:"to"`
	Cc      []string `json:"cc"`
	Bcc     []string `json:"bcc"`
	Subject string   `json:"subject"`
	Body    string   `json:"body"`
}

// RunRequest runs an automation by name or id.
type RunRequest struct {
	Script  string            `json:"script"`
	DryRun  bool              `json:"dryRun"`
	On      int64             `json:"on"`
	Setting map[string]string `json:"settings"`
}

// ExportRequest exports messages by id, or a folder.
type ExportRequest struct {
	IDs     []int64 `json:"ids"`
	Folder  string  `json:"folder"`
	Account string  `json:"account"`
}

// RangeRequest is a time range, both RFC 3339.
type RangeRequest struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// TasksRequest filters tasks: List by name, Due as the last due date
// (YYYY-MM-DD), Done to include finished ones.
type TasksRequest struct {
	List string `json:"list"`
	Due  string `json:"due"`
	Done bool   `json:"done"`
}

// TaskAddRequest adds a task. Due is YYYY-MM-DD or RFC 3339.
type TaskAddRequest struct {
	Title string `json:"title"`
	Due   string `json:"due"`
	List  string `json:"list"`
}

// QueryRequest is a free text query.
type QueryRequest struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

// OpenRequest brings the app forward on an item: a message id, a mailto:
// URL or a file path. All empty only brings it forward.
type OpenRequest struct {
	ID     int64  `json:"id"`
	Mailto string `json:"mailto"`
	File   string `json:"file"`
}
