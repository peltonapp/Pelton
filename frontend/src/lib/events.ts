// events.ts is the typed contract for the wails runtime events the backend emits
// (see events.go). it wraps EventsOn so subscribers get a typed payload and a
// single unsubscribe function, and keeps the event-name strings in one place.

import { EventsOn } from '../../wailsjs/runtime/runtime'
import type { AccountSyncState } from './types'
import type { SyncPhase } from './syncstatus'

// event names, matching the go constants exactly.
export const EventNames = {
  mailNew: 'mail:new',
  mailRepaired: 'mail:repaired',
  mailUpdated: 'mail:updated',
  mailBodyFailed: 'mail:bodyFailed',
  syncProgress: 'sync:progress',
  syncState: 'sync:state',
  accountSyncState: 'sync:accounts',
  outboxChanged: 'outbox:changed',
  menu: 'menu:action',
  downloadProgress: 'download:progress',
  attachmentProgress: 'attachment:progress',
  updateAvailable: 'update:available',
  mailtoCompose: 'mailto:compose',
  viewsChanged: 'views:changed',
  profileChanged: 'profile:changed',
  importProgress: 'import:progress',
  agentProposals: 'agent:proposals',
  openMessage: 'message:open',
} as const

// payloads, mirroring the go event structs.
export interface MailNewEvent {
  accountId: number
  folderId: number
  count: number
}

// MailRepairedEvent reports messages whose stored text was replaced because it
// had been cached in an encoding nothing could read.
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
  phase?: SyncPhase
}

/** Payload of mail:updated: the id of a message row that changed in place, such as a stub that gained its body. */
export interface MailUpdatedEvent {
  messageId: number
}

/** Payload of mail:bodyFailed: the id of a message whose body could not be fetched after every retry. */
export interface MailBodyFailedEvent {
  messageId: number
}

/** Payload of the sync state event: whether background sync is running, and the error that ended it, if any. */
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

/** Payload of the bulk download progress event: counts, percent and ETA of a running download. */
export interface DownloadProgressEvent {
  running: boolean
  done: number
  total: number
  percent: number
  etaSeconds: number
  label: string
  error: string
}

/** Payload of the attachment save progress event: bytes and files done out of the total. */
export interface AttachmentProgressEvent {
  running: boolean
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

// Unsubscribe removes an event listener.
export type Unsubscribe = () => void

// onMailNew fires when sync or idle pulled new messages.
export function onMailNew(cb: (e: MailNewEvent) => void): Unsubscribe {
  return EventsOn(EventNames.mailNew, (e: MailNewEvent) => cb(e))
}

// onMailRepaired fires when a sync rewrote the text of cached messages, so the
// list and any open message show the fixed text without a reload.
export function onMailRepaired(cb: (e: MailRepairedEvent) => void): Unsubscribe {
  return EventsOn(EventNames.mailRepaired, (e: MailRepairedEvent) => cb(e))
}

/** Subscribes to mail:updated; returns the unsubscribe function. */
export function onMailUpdated(cb: (e: MailUpdatedEvent) => void): Unsubscribe {
  return EventsOn(EventNames.mailUpdated, (e: MailUpdatedEvent) => cb(e))
}

/** Subscribes to mail:bodyFailed; returns the unsubscribe function. */
export function onMailBodyFailed(cb: (e: MailBodyFailedEvent) => void): Unsubscribe {
  return EventsOn(EventNames.mailBodyFailed, (e: MailBodyFailedEvent) => cb(e))
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

// onViewsChanged fires when saved views or their eager-run counts change. it
// carries no payload; subscribers reload the views store.
export function onViewsChanged(cb: () => void): Unsubscribe {
  return EventsOn(EventNames.viewsChanged, () => cb())
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
