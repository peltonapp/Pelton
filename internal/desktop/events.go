package desktop

import (
	"errors"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// errStoreUnavailable is returned by bound methods when the store failed to open
// at startup, so the ui can show a clear error instead of hanging.
var errStoreUnavailable = errors.New("pelton: local store is unavailable")

// errSearchUnavailable is returned when the search index failed to open, so the
// ui can fall back gracefully instead of treating it as a hard error.
var errSearchUnavailable = errors.New("pelton: search index is unavailable")

// Event names emitted on the wails runtime event bus. The frontend subscribes to
// these by the same string keys (see src/lib/events.ts). Keep this list and the
// typescript EventName union in sync.
const (
	// EventMailNew fires when a sync or idle cycle pulled new messages.
	EventMailNew = "mail:new"
	// EventMailRepaired fires when a sync replaced the text of messages that
	// were cached with an encoding nothing could read, so the list reloads.
	EventMailRepaired = "mail:repaired"
	// EventMailUpdated fires when a message row changes in place, such as a stub
	// gaining its full body after a background fetch.
	EventMailUpdated = "mail:updated"
	// EventMailBodyFailed fires when the body of a message opened in the reading
	// pane could not be fetched, after every retry, so the pane stops waiting.
	EventMailBodyFailed = "mail:bodyFailed"
	// EventSyncProgress fires as folders are synced so the ui can show progress.
	EventSyncProgress = "sync:progress"
	// EventSyncState fires when background sync starts or stops, with any error.
	EventSyncState = "sync:state"
	// EventAccountSyncState fires when a sync run finishes, carrying how each
	// account fared, so a mailbox that failed while the others succeeded gets
	// marked instead of being reported as a clean sync.
	EventAccountSyncState = "sync:accounts"
	// EventOutboxChanged fires when the outbox contents or a message state change.
	EventOutboxChanged = "outbox:changed"
	// EventAgentProposals fires when an agent proposes a message, or one is
	// approved or discarded, so the queue in the ui matches what is stored.
	EventAgentProposals = "agent:proposals"
	// EventMenu fires when a native menu item is chosen. The payload is a short
	// action string (preferences, compose, sync, add-mailbox, about) the frontend
	// maps to an action.
	EventMenu = "menu:action"
	// EventDownloadProgress fires during a bulk offline range download so the
	// status bar can show a progress bar with percent and eta.
	EventDownloadProgress = "download:progress"
	// EventAttachmentProgress fires while saving one or more attachments.
	EventAttachmentProgress = "attachment:progress"
	// EventUpdateAvailable fires after an automatic (frequency-driven) update
	// check completes, so the about section can show a notice without polling.
	// It is not fired for a manual "check now" call, which returns its result
	// directly to the caller instead.
	EventUpdateAvailable = "update:available"
	// EventMailtoCompose fires when a mailto: link is opened while the app is
	// already running. The payload is a MailtoDraft the frontend opens as a
	// prefilled compose. A mailto that launched the app is delivered through
	// ConsumePendingMailto instead.
	EventMailtoCompose = "mailto:compose"
	// EventViewsChanged fires when saved views or their eager-run counts change,
	// so the sidebar can refresh view rows and badges without polling.
	EventViewsChanged = "views:changed"
	// EventImportProgress fires while mail is being imported from files, and
	// once more with Running false when the job ends.
	EventImportProgress = "import:progress"
	// EventProfileChanged fires when the app switches profile, or when the
	// profile it is in is edited. Everything the ui holds is scoped to a profile,
	// so the frontend reloads its preferences, sidebar, list and views rather
	// than trying to patch them.
	EventProfileChanged = "profile:changed"
	// EventOpenMessage fires when something outside the ui asks for a specific
	// message to be shown, which today means a clicked new-mail notification.
	// It carries the folder as well as the message so the list can move to
	// where the message actually lives rather than opening it out of context.
	EventOpenMessage = "message:open"
)

// OpenMessageEvent is the payload for EventOpenMessage.
type OpenMessageEvent struct {
	MessageID int64 `json:"messageId"`
	AccountID int64 `json:"accountId"`
	FolderID  int64 `json:"folderId"`
}

// DownloadProgressEvent is the payload for EventDownloadProgress. Running is
// false on the final event (done, cancelled, or failed); Error is set on failure.
type DownloadProgressEvent struct {
	Running    bool   `json:"running"`
	Done       int    `json:"done"`
	Total      int    `json:"total"`
	Percent    int    `json:"percent"`
	ETASeconds int    `json:"etaSeconds"`
	Label      string `json:"label"`
	Error      string `json:"error"`
}

// AttachmentProgressEvent is the payload for EventAttachmentProgress. It reports
// byte progress for the current file plus how many files are done in a save-all.
type AttachmentProgressEvent struct {
	Running    bool   `json:"running"`
	Filename   string `json:"filename"`
	BytesDone  int64  `json:"bytesDone"`
	BytesTotal int64  `json:"bytesTotal"`
	FilesDone  int    `json:"filesDone"`
	FilesTotal int    `json:"filesTotal"`
	Error      string `json:"error"`
}

// ImportProgressEvent is the payload for EventImportProgress. Running is false
// on the final event, which carries the totals; Error is set on failure.
// BytesDone/BytesTotal measure the source files, because an mbox does not say
// how many messages it holds until it has been read.
type ImportProgressEvent struct {
	Running    bool   `json:"running"`
	Folder     string `json:"folder"`
	Imported   int    `json:"imported"`
	Skipped    int    `json:"skipped"`
	Failed     int    `json:"failed"`
	BytesDone  int64  `json:"bytesDone"`
	BytesTotal int64  `json:"bytesTotal"`
	// FileIndex is which mailbox is being read, counting from 1, out of
	// FileTotal. Bytes say how far along the import is; these say where it is.
	FileIndex int      `json:"fileIndex"`
	FileTotal int      `json:"fileTotal"`
	Folders   []string `json:"folders"`
	Error     string   `json:"error"`
}

// MailNewEvent is the payload for EventMailNew.
type MailNewEvent struct {
	AccountID int64 `json:"accountId"`
	FolderID  int64 `json:"folderId"`
	Count     int   `json:"count"`
}

// MailRepairedEvent is the payload for EventMailRepaired. Count is how many
// messages in the folder were rewritten.
type MailRepairedEvent struct {
	AccountID int64 `json:"accountId"`
	FolderID  int64 `json:"folderId"`
	Count     int   `json:"count"`
}

// SyncProgressEvent is the payload for EventSyncProgress. An empty Folder means
// the run finished and the ui clears its line.
type SyncProgressEvent struct {
	AccountID int64 `json:"accountId"`
	// AccountEmail and Server name who is syncing and where from, for the
	// verbose line: two accounts syncing at once are otherwise two identical
	// "Syncing INBOX" lines (#310).
	AccountEmail string `json:"accountEmail"`
	Server       string `json:"server"`
	Folder       string `json:"folder"`
	// Done and Total count message bodies for the run so far, which is what a
	// progress bar is about. Total is what the reconcile plans have asked for up
	// to now, not the size of the mailboxes: a resync of a cached folder is a
	// handful of messages and the bar says so (#313). It grows as folders open,
	// and a Total of 0 means nothing is known yet and the bar is indeterminate.
	Done  int `json:"done"`
	Total int `json:"total"`
	// FolderDone and FolderTotal are the same counts for the folder named above,
	// since one mailbox is often most of the work.
	FolderDone  int `json:"folderDone"`
	FolderTotal int `json:"folderTotal"`
	// FoldersDone and FoldersTotal count mailboxes rather than messages, for
	// the "mailbox 3 of 12" part of the line.
	FoldersDone  int `json:"foldersDone"`
	FoldersTotal int `json:"foldersTotal"`
	// Phase is "stubs" or "bodies" during a phased initial campaign, "verify"
	// during the background folder check, else empty.
	Phase string `json:"phase"`
}

// MailUpdatedEvent is the payload for EventMailUpdated. The reading pane reloads
// when the open message gains a body that was fetched in the background.
type MailUpdatedEvent struct {
	MessageID int64 `json:"messageId"`
}

// MailBodyFailedEvent is the payload for EventMailBodyFailed.
type MailBodyFailedEvent struct {
	MessageID int64 `json:"messageId"`
}

// SyncStateEvent is the payload for EventSyncState. Error is empty on success.
type SyncStateEvent struct {
	Running bool   `json:"running"`
	Error   string `json:"error"`
}

// AccountSyncStateEvent is the payload for EventAccountSyncState: every account
// that has a recorded outcome, failing or not.
type AccountSyncStateEvent struct {
	States []AccountSyncStateDTO `json:"states"`
}

// emit sends a runtime event if the context is set. It is a thin wrapper so call
// sites stay terse and a nil context (before startup) is a safe no-op.
func (a *App) emit(name string, payload any) {
	if a.emitForTest != nil {
		a.emitForTest(name, payload)
	}
	if a.syncProgressEmitForTest != nil && name == EventSyncProgress {
		if ev, ok := payload.(SyncProgressEvent); ok {
			a.syncProgressEmitForTest(ev)
		}
	}
	if a.ctx == nil || !a.runtimeReady.Load() {
		return
	}
	runtime.EventsEmit(a.ctx, name, payload)
}
