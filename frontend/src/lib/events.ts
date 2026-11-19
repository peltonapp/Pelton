// events.ts is the typed contract for the wails runtime events the backend emits
// (see events.go). it wraps Events.On so subscribers get a typed payload and a
// single unsubscribe function, and keeps the event-name strings in one place.

import { Events } from '@wailsio/runtime'
import type {
  AccountSyncState,
  OutboxLogEntry,
  FocusInfo,
  FocusSummary,
  ChatStatus,
  ChatMember,
  ChatVerification,
  ChatMediaProgress,
} from './types'
import type { LiveRun, ScriptAsk, ScriptCatchUp, ScriptConfirmRequest, ScriptResult, ScriptRun, ScriptToast } from './scripting'
import type { AssistantScriptEvent } from './assistantScripts'

// EventsOn subscribes to a backend event and hands the callback its payload.
function EventsOn<T>(name: string, cb: (data: T) => void): () => void {
  return Events.On(name, (ev) => cb(ev.data as T))
}

// event names, matching the go constants exactly.
export const EventNames = {
  mailNew: 'mail:new',
  mailRepaired: 'mail:repaired',
  syncProgress: 'sync:progress',
  syncState: 'sync:state',
  accountSyncState: 'sync:accounts',
  outboxChanged: 'outbox:changed',
  outboxLog: 'outbox:log',
  menu: 'menu:action',
  downloadProgress: 'download:progress',
  attachmentProgress: 'attachment:progress',
  updateAvailable: 'update:available',
  mailtoCompose: 'mailto:compose',
  viewsChanged: 'views:changed',
  foldersChanged: 'folders:changed',
  profileChanged: 'profile:changed',
  importProgress: 'import:progress',
  filesDropped: 'files:dropped',
  agentProposals: 'agent:proposals',
  openMessage: 'message:open',
  replyMessage: 'message:reply',
  composeCloseRequest: 'compose:close-request',
  appLock: 'applock:changed',
  mailVault: 'mailvault:changed',
  calendarSync: 'calendar:sync',
  calendarChanged: 'calendar:changed',
  calendarReminder: 'calendar:reminder',
  icsOpen: 'calendar:open-files',
  taskReminder: 'tasks:reminder',
  followUpsChanged: 'followups:changed',
  promisesChanged: 'promises:changed',
  focusChanged: 'focus:changed',
  focusEnded: 'focus:ended',
  projectsChanged: 'projects:changed',
  vaultChanged: 'notes:changed',
  feedsProgress: 'feeds:progress',
  feedsChanged: 'feeds:changed',
  feedsOpen: 'feeds:open',
  chatStatus: 'chat:status',
  chatRooms: 'chat:rooms',
  chatTimeline: 'chat:timeline',
  chatTyping: 'chat:typing',
  chatReceipts: 'chat:receipts',
  chatVerification: 'chat:verification',
  chatMediaProgress: 'chat:media-progress',
  chatOpen: 'chat:open',
  scriptRun: 'scripts:run',
  scriptsChanged: 'scripts:changed',
  scriptConfirm: 'scripts:confirm',
  scriptCatchUp: 'scripts:catchup',
  scriptToast: 'scripts:toast',
  scriptAsk: 'scripts:ask',
  scriptAskDone: 'scripts:askdone',
  scriptResult: 'scripts:result',
  scriptProgress: 'scripts:progress',
  assistantScript: 'scripts:assistant',
  autoEncryptChanged: 'autoencrypt:changed',
  // sent between windows by the frontend, not by go.
  archiveAfterSend: 'compose:archive-after-send',
} as const

/** TaskReminderEvent is a task reminder that came due, alongside its notification. */
export interface TaskReminderEvent {
  taskId: number
  listId: number
  summary: string
  /** A date, rfc3339, or empty for a task with no due date. */
  due: string
}

/** onTaskReminder fires when a task's reminder is due. */
export function onTaskReminder(cb: (e: TaskReminderEvent) => void): Unsubscribe {
  return EventsOn(EventNames.taskReminder, (e: TaskReminderEvent) => cb(e))
}

/**
 * VaultChangeEvent says which notes changed in the notes folder, through
 * Pelton or another app. reset means the folder was (re)loaded; tree that
 * files or folders came or went.
 */
export interface VaultChangeEvent {
  changed: string[] | null
  removed: string[] | null
  reset: boolean
  tree: boolean
}

/** onVaultChanged fires when notes in the notes folder changed. */
export function onVaultChanged(cb: (e: VaultChangeEvent) => void): Unsubscribe {
  return EventsOn(EventNames.vaultChanged, (e: VaultChangeEvent) => cb(e))
}

/** onAutoEncryptChanged fires when own keys were made or renewed or a lookup found keys. */
export function onAutoEncryptChanged(cb: () => void): () => void {
  return EventsOn(EventNames.autoEncryptChanged, () => cb())
}

/** onICSOpen fires when the system asks the running app to open .ics files; collect them with consumePendingICS. */
export function onICSOpen(cb: () => void): () => void {
  return EventsOn(EventNames.icsOpen, () => cb())
}

/**
 * CalendarSyncEvent reports a running calendar sync. running is false on the
 * final event, which carries any error. done and total count calendars.
 */
export interface CalendarSyncEvent {
  running: boolean
  calendarId: number
  calendar: string
  done: number
  total: number
  error: string
}

/** CalendarChangedEvent names the calendars whose events changed; empty means any. */
export interface CalendarChangedEvent {
  calendarIds: number[]
}

/** CalendarReminderEvent is a reminder that came due, alongside its notification. */
export interface CalendarReminderEvent {
  eventId: number
  calendarId: number
  recurrenceId: string
  summary: string
  location: string
  start: string
  end: string
  allDay: boolean
  /** Others are invited, so the reminder can offer to prepare for the meeting. */
  hasAttendees: boolean
}

// payloads, mirroring the go event structs.
export interface MailNewEvent {
  accountId: number
  folderId: number
  count: number
}

// MailRepairedEvent reports cached messages that changed underneath an open
// list: text replaced because it had been cached in an encoding nothing could
// read, conversations regrouped, or messages marked answered or forwarded.
// accountId and folderId are 0 when the change spans more than one.
export interface MailRepairedEvent {
  accountId: number
  folderId: number
  count: number
}

// SyncProgressEvent reports a running sync or backfill. An empty folder means
// the run is over.
export interface SyncProgressEvent {
  accountId: number
  // who is syncing and where from, for the verbose line.
  accountEmail: string
  server: string
  folder: string
  // message bodies fetched so far in this run, and how many the reconcile
  // plans have asked for up to now. total grows as folders open, and 0 means
  // nothing is known yet, which the bar shows as indeterminate rather than
  // guessing.
  done: number
  total: number
  // the same counts for the folder named above.
  folderDone: number
  folderTotal: number
  // mailboxes rather than messages.
  foldersDone: number
  foldersTotal: number
}

export interface SyncStateEvent {
  running: boolean
  error: string
}

// AccountSyncStateEvent carries how every account fared, fired when a sync run
// ends. A run that failed one account out of several used to report nothing at
// all, which is how a mailbox can go quiet for weeks.
export interface AccountSyncStateEvent {
  states: AccountSyncState[]
}

export interface DownloadProgressEvent {
  running: boolean
  done: number
  total: number
  percent: number
  etaSeconds: number
  label: string
  error: string
}

export interface AttachmentProgressEvent {
  running: boolean
  attachmentId: number
  filename: string
  bytesDone: number
  bytesTotal: number
  filesDone: number
  filesTotal: number
  error: string
}

// ImportProgressEvent reports a running mail import. running is false on the
// final event, which carries the totals and any error. Progress is measured in
// source bytes: an mbox does not say how many messages it holds until it has
// been read through.
export interface ImportProgressEvent {
  running: boolean
  folder: string
  imported: number
  skipped: number
  failed: number
  bytesDone: number
  bytesTotal: number
  // which mailbox is being read, counting from 1, out of fileTotal. Bytes say
  // how far along the import is; these say where it is.
  fileIndex: number
  fileTotal: number
  folders: string[]
  error: string
}

// UpdateAvailableEvent mirrors go's UpdateCheckResult, fired after an
// automatic (frequency-driven) update check completes.
export interface UpdateAvailableEvent {
  checked: boolean
  available: boolean
  currentVersion: string
  latestVersion: string
  releaseUrl: string
  error: string
}

// MailtoDraft mirrors the go MailtoDraft: a compose prefill parsed from a
// mailto: link. Address fields are comma-joined for the raw recipient inputs.
export interface MailtoDraft {
  to: string
  cc: string
  bcc: string
  subject: string
  body: string
}

// OpenMessageEvent asks the ui to show one specific message, which today only
// happens when a new-mail notification is clicked. The folder travels with it
// so the list can move to where the message lives first.
export interface OpenMessageEvent {
  messageId: number
  accountId: number
  folderId: number
}

// ArchiveAfterSendEvent asks the main window to archive the conversation a
// reply sent from a compose window answered, once delaySeconds have passed.
export interface ArchiveAfterSendEvent {
  messageId: number
  delaySeconds: number
}

// Unsubscribe removes an event listener.
export type Unsubscribe = () => void

// onMailNew fires when sync or idle pulled new messages.
export function onMailNew(cb: (e: MailNewEvent) => void): Unsubscribe {
  return EventsOn(EventNames.mailNew, (e: MailNewEvent) => cb(e))
}

// onMailRepaired fires when cached messages changed without new mail arriving
// (text rewritten, conversations regrouped, reply state changed), so the list
// and any open message reload.
export function onMailRepaired(cb: (e: MailRepairedEvent) => void): Unsubscribe {
  return EventsOn(EventNames.mailRepaired, (e: MailRepairedEvent) => cb(e))
}

// onSyncProgress fires per folder as a sync runs.
export function onSyncProgress(cb: (e: SyncProgressEvent) => void): Unsubscribe {
  return EventsOn(EventNames.syncProgress, (e: SyncProgressEvent) => cb(e))
}

// onSyncState fires when background sync starts or stops.
export function onSyncState(cb: (e: SyncStateEvent) => void): Unsubscribe {
  return EventsOn(EventNames.syncState, (e: SyncStateEvent) => cb(e))
}

// onAccountSyncState fires when a sync run ends, with each account's outcome.
export function onAccountSyncState(cb: (e: AccountSyncStateEvent) => void): Unsubscribe {
  return EventsOn(EventNames.accountSyncState, (e: AccountSyncStateEvent) => cb(e))
}

// onOutboxChanged fires when the outbox contents or a message state change. it
// carries no payload; subscribers refetch the outbox.
export function onOutboxChanged(cb: () => void): Unsubscribe {
  return EventsOn(EventNames.outboxChanged, () => cb())
}

// onOutboxLog fires with each new line of the send activity log.
export function onOutboxLog(cb: (e: OutboxLogEntry) => void): Unsubscribe {
  return EventsOn(EventNames.outboxLog, (e: OutboxLogEntry) => cb(e))
}

// onFollowUpsChanged fires when a follow-up reminder is set, comes due, is
// answered or is cancelled. It carries no payload; subscribers refetch.
export function onFollowUpsChanged(cb: () => void): Unsubscribe {
  return EventsOn(EventNames.followUpsChanged, () => cb())
}

/** Fires when promises were found, accepted, dismissed or closed. Subscribers refetch. */
export function onPromisesChanged(cb: () => void): Unsubscribe {
  return EventsOn(EventNames.promisesChanged, () => cb())
}

// onMenu fires when a native menubar item is chosen. the payload is a short
// action string (preferences, compose, sync, add-mailbox, about).
export function onMenu(cb: (action: string) => void): Unsubscribe {
  return EventsOn(EventNames.menu, (action: string) => cb(action))
}

// onDownloadProgress fires during a bulk offline range download.
export function onDownloadProgress(cb: (e: DownloadProgressEvent) => void): Unsubscribe {
  return EventsOn(EventNames.downloadProgress, (e: DownloadProgressEvent) => cb(e))
}

// onAttachmentProgress fires while saving one or more attachments.
export function onAttachmentProgress(cb: (e: AttachmentProgressEvent) => void): Unsubscribe {
  return EventsOn(EventNames.attachmentProgress, (e: AttachmentProgressEvent) => cb(e))
}

// onUpdateAvailable fires after an automatic update check completes (never
// for a manual "check now", which gets its result directly instead).
export function onUpdateAvailable(cb: (e: UpdateAvailableEvent) => void): Unsubscribe {
  return EventsOn(EventNames.updateAvailable, (e: UpdateAvailableEvent) => cb(e))
}

// onMailtoCompose fires when a mailto: link is opened while the app is already
// running. A mailto that launched the app is delivered via consumePendingMailto.
export function onMailtoCompose(cb: (e: MailtoDraft) => void): Unsubscribe {
  return EventsOn(EventNames.mailtoCompose, (e: MailtoDraft) => cb(e))
}

// onImportProgress fires while mail is being imported from files, and once more
// with running false when the job ends.
export function onImportProgress(cb: (e: ImportProgressEvent) => void): Unsubscribe {
  return EventsOn(EventNames.importProgress, (e: ImportProgressEvent) => cb(e))
}

/** FilesDroppedEvent names the drop target files from the system landed on. */
export interface FilesDroppedEvent {
  /** The target's data-file-drop-target value. */
  target: string
  paths: string[]
}

/** onFilesDropped fires when files are dropped onto an element marked data-file-drop-target. */
export function onFilesDropped(cb: (e: FilesDroppedEvent) => void): Unsubscribe {
  return EventsOn(EventNames.filesDropped, (e: FilesDroppedEvent) => cb(e))
}

// onViewsChanged fires when saved views or their eager-run counts change. it
// carries no payload; subscribers reload the views store.
export function onViewsChanged(cb: () => void): Unsubscribe {
  return EventsOn(EventNames.viewsChanged, () => cb())
}

// onFoldersChanged fires when a sync found mailboxes added or deleted on the
// server.
export function onFoldersChanged(cb: () => void): Unsubscribe {
  return EventsOn(EventNames.foldersChanged, () => cb())
}

// onProfileChanged fires when the app switches profile, or when the profile it
// is in is edited. Everything the ui holds is scoped to a profile, so the
// subscriber reloads rather than patching.
export function onProfileChanged(cb: () => void): Unsubscribe {
  return EventsOn(EventNames.profileChanged, () => cb())
}

// onAgentProposals fires when an agent proposes a message, or one is approved
// or discarded, so the approval queue matches what is stored.
export function onAgentProposals(cb: () => void): Unsubscribe {
  return EventsOn(EventNames.agentProposals, () => cb())
}

// onOpenMessage fires when something outside the ui asks for one message to be
// shown. Today that is a clicked new-mail notification on Windows.
export function onOpenMessage(cb: (e: OpenMessageEvent) => void): Unsubscribe {
  return EventsOn(EventNames.openMessage, (e: OpenMessageEvent) => cb(e))
}

// onReplyMessage fires when something outside the ui asks for a reply to one
// message in a composer: the Reply button of a new-mail notification.
export function onReplyMessage(cb: (e: OpenMessageEvent) => void): Unsubscribe {
  return EventsOn(EventNames.replyMessage, (e: OpenMessageEvent) => cb(e))
}

/** Fires when focus mode starts, ends or changes its end time. */
export function onFocusChanged(cb: (e: FocusInfo) => void): Unsubscribe {
  return EventsOn(EventNames.focusChanged, (e: FocusInfo) => cb(e))
}

/** Fires when the chat account's state changes. */
export function onChatStatus(cb: (e: ChatStatus) => void): Unsubscribe {
  return EventsOn(EventNames.chatStatus, (e: ChatStatus) => cb(e))
}

/** Fires when chat rooms changed; the list names them. */
export function onChatRooms(cb: (roomIds: string[]) => void): Unsubscribe {
  return EventsOn(EventNames.chatRooms, (e: { roomIds: string[] | null } | null) => cb(e?.roomIds ?? []))
}

/** Fires when a room's timeline changed; an empty id means any room. */
export function onChatTimeline(cb: (roomId: string) => void): Unsubscribe {
  return EventsOn(EventNames.chatTimeline, (e: { roomId: string } | null) => cb(e?.roomId ?? ''))
}

/** Fires when someone's read marker in a room moved. */
export function onChatReceipts(cb: (roomId: string) => void): Unsubscribe {
  return EventsOn(EventNames.chatReceipts, (e: { roomId: string } | null) => cb(e?.roomId ?? ''))
}

/** Fires with who is typing in a room. */
export function onChatTyping(cb: (roomId: string, users: ChatMember[]) => void): Unsubscribe {
  return EventsOn(EventNames.chatTyping, (e: { roomId: string; users: ChatMember[] | null }) => cb(e.roomId, e.users ?? []))
}

/** Fires when a device verification moves on. */
export function onChatVerification(cb: (e: ChatVerification) => void): Unsubscribe {
  return EventsOn(EventNames.chatVerification, (e: ChatVerification) => cb(e))
}

/** Fires while a chat file is saved or sent. */
export function onChatMediaProgress(cb: (e: ChatMediaProgress) => void): Unsubscribe {
  return EventsOn(EventNames.chatMediaProgress, (e: ChatMediaProgress) => cb(e))
}

/** Fires when a clicked notification asks for a chat room. */
export function onChatOpen(cb: (roomId: string) => void): Unsubscribe {
  return EventsOn(EventNames.chatOpen, (e: { roomId: string }) => cb(e.roomId))
}

/** Fires once when focus mode ends, with what arrived meanwhile. */
export function onFocusEnded(cb: (e: FocusSummary) => void): Unsubscribe {
  return EventsOn(EventNames.focusEnded, (e: FocusSummary) => cb(e))
}

// emitArchiveAfterSend hands a send and archive from a compose window to the
// main window, which outlives it.
export function emitArchiveAfterSend(e: ArchiveAfterSendEvent): void {
  void Events.Emit(EventNames.archiveAfterSend, e)
}

// onArchiveAfterSend fires in every window when a compose window sent a reply
// whose conversation is to be archived.
export function onArchiveAfterSend(cb: (e: ArchiveAfterSendEvent) => void): Unsubscribe {
  return EventsOn(EventNames.archiveAfterSend, cb)
}

// onComposeCloseRequest fires in a compose window whose close button was
// pressed, so the compose closes the way its own close button does.
export function onComposeCloseRequest(cb: () => void): Unsubscribe {
  return EventsOn(EventNames.composeCloseRequest, () => cb())
}

/** onAppLock fires in every window when the app locks or unlocks. */
export function onAppLock(cb: (e: { locked: boolean }) => void): Unsubscribe {
  return EventsOn(EventNames.appLock, (e: { locked: boolean }) => cb(e))
}

/** onMailVault fires when a mail vault locks, unlocks, changes or moves along sealing. */
export function onMailVault(cb: (e: { id: number; locked: boolean; removed: boolean }) => void): Unsubscribe {
  return EventsOn(EventNames.mailVault, (e: { id: number; locked: boolean; removed: boolean }) => cb(e))
}

/** onCalendarSync fires as calendars sync, and once more when the run ends. */
export function onCalendarSync(cb: (e: CalendarSyncEvent) => void): Unsubscribe {
  return EventsOn(EventNames.calendarSync, (e: CalendarSyncEvent) => cb(e))
}

/** onCalendarChanged fires when events or calendars changed, from a sync or an edit. */
export function onCalendarChanged(cb: (e: CalendarChangedEvent) => void): Unsubscribe {
  return EventsOn(EventNames.calendarChanged, (e: CalendarChangedEvent | null) => cb({ calendarIds: e?.calendarIds ?? [] }))
}

/** ProjectsChangedEvent names the projects that changed; empty means any. */
export interface ProjectsChangedEvent {
  projectIds: number[]
}

/** onProjectsChanged fires when a project, its board, its links or its notes changed. */
export function onProjectsChanged(cb: (e: ProjectsChangedEvent) => void): Unsubscribe {
  return EventsOn(EventNames.projectsChanged, (e: ProjectsChangedEvent | null) => cb({ projectIds: e?.projectIds ?? [] }))
}

/** onCalendarReminder fires when an event's reminder is due. */
export function onCalendarReminder(cb: (e: CalendarReminderEvent) => void): Unsubscribe {
  return EventsOn(EventNames.calendarReminder, (e: CalendarReminderEvent) => cb(e))
}

/** FeedsProgressEvent reports a feed refresh in progress, and its end. */
export interface FeedsProgressEvent {
  running: boolean
  done: number
  total: number
  /** The feed just fetched. */
  feed: string
  failed: number
}

/** FeedsChangedEvent says feeds or entries changed; no ids means any. */
export interface FeedsChangedEvent {
  feedIds: number[]
  added: number
}

/** onFeedsProgress fires as feeds are fetched, and once more when the run ends. */
export function onFeedsProgress(cb: (e: FeedsProgressEvent) => void): Unsubscribe {
  return EventsOn(EventNames.feedsProgress, (e: FeedsProgressEvent) => cb(e))
}

/** onFeedsChanged fires when feeds or their entries changed. */
export function onFeedsChanged(cb: (e: FeedsChangedEvent) => void): Unsubscribe {
  return EventsOn(EventNames.feedsChanged, (e: FeedsChangedEvent | null) => cb({ feedIds: e?.feedIds ?? [], added: e?.added ?? 0 }))
}

/** onFeedsOpen fires when a clicked notification asks for a feed entry. */
export function onFeedsOpen(cb: (entryId: number) => void): Unsubscribe {
  return EventsOn(EventNames.feedsOpen, (e: { entryId: number }) => cb(e.entryId))
}

/** onScriptRun fires when a script run ends, test runs included. */
export function onScriptRun(cb: (run: ScriptRun) => void): Unsubscribe {
  return EventsOn(EventNames.scriptRun, (r: ScriptRun) => cb(r))
}

/** onScriptsChanged fires when scripts were added, changed or removed. */
export function onScriptsChanged(cb: () => void): Unsubscribe {
  return EventsOn(EventNames.scriptsChanged, () => cb())
}

/** onScriptConfirm fires when a script asks before a risky call. */
export function onScriptConfirm(cb: (req: ScriptConfirmRequest) => void): Unsubscribe {
  return EventsOn(EventNames.scriptConfirm, (r: ScriptConfirmRequest) => cb(r))
}

/** onScriptCatchUp fires when a script asks whether to run over mail that arrived while Pelton was closed. */
export function onScriptCatchUp(cb: (c: ScriptCatchUp) => void): Unsubscribe {
  return EventsOn(EventNames.scriptCatchUp, (c: ScriptCatchUp) => cb(c))
}

/** onScriptToast fires when a script shows a message. */
export function onScriptToast(cb: (toast: ScriptToast) => void): Unsubscribe {
  return EventsOn(EventNames.scriptToast, (e: ScriptToast) => cb(e))
}

/** onScriptAsk fires when a running script asks the user something. */
export function onScriptAsk(cb: (ask: ScriptAsk) => void): Unsubscribe {
  return EventsOn(EventNames.scriptAsk, (e: ScriptAsk) => cb(e))
}

/** onScriptAskDone fires with the id of a question that gave up waiting. */
export function onScriptAskDone(cb: (id: string) => void): Unsubscribe {
  return EventsOn(EventNames.scriptAskDone, (id: string) => cb(id))
}

/** onScriptResult fires when a script shows a result in the result panel. */
export function onScriptResult(cb: (result: ScriptResult) => void): Unsubscribe {
  return EventsOn(EventNames.scriptResult, (e: ScriptResult) => cb(e))
}

/** onScriptProgress fires while a run that takes a moment goes, and when it is done. */
export function onScriptProgress(cb: (run: LiveRun) => void): Unsubscribe {
  return EventsOn(EventNames.scriptProgress, (e: LiveRun) => cb(e))
}

/** onAssistantScript fires when an assistant creates, changes or proposes a change to a script, and when a proposal is settled. */
export function onAssistantScript(cb: (e: AssistantScriptEvent) => void): Unsubscribe {
  return EventsOn(EventNames.assistantScript, (e: AssistantScriptEvent) => cb(e))
}
