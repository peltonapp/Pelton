// api.ts is the single typed boundary between the svelte ui and the go backend.
// components call these functions, never window.go.* or the generated bindings
// directly, so the call sites stay typed and the generated layer can change
// shape without touching components.
//
// Being the one boundary is also what makes demo mode work. A function that
// reads or writes real mail checks isDemoActive() first and answers from the
// samples instead: the real install is still sitting behind this file, with
// real messages under the same ids the samples use, so anything that reaches
// the backend in demo mode acts on somebody's actual mailbox.

import * as App from '../../bindings/github.com/peltonapp/Pelton/internal/desktop/app'
import * as desktop from '../../bindings/github.com/peltonapp/Pelton/internal/desktop/models'
import type { NewsletterSender, InsightsStorage, InsightsVolume, InsightsPeople } from './mailstats'
import {
  isDemoActive,
  demoAccounts,
  demoFolders,
  demoViews,
  demoList,
  demoMessage,
  demoOutbox,
  demoSearch,
  demoHtml,
  demoSource,
  demoThreads,
  demoThread,
} from './demo'
import { noteAction } from './coach'
import type {
  Account,
  AccountProxy,
  SMIMERevocation,
  MCPPermission,
  AgentAction,
  AgentProposal,
  Folder,
  UnifiedView,
  MessageList,
  MessageDetail,
  MessageSummary,
  ThreadList,
  Thread,
  ComposeRequest,
  Draft,
  FollowUp,
  Snippet,
  OutboxRow,
  OutboxEdit,
  OutboxCancel,
  OutboxLogEntry,
  FocusInfo,
  FocusSchedule,
  UIPrefs,
  SearchSort,
  ViewKey,
  Discovered,
  AddAccountRequest,
  TestConnectionRequest,
  ConnectionTest,
  UntrustedCert,
  CAFile,
  TLSMode,
  Signature,
  AccountSignatures,
  AddressBookEntry,
  AddressBook,
  AddressBookDraft,
  DiscoveredBook,
  Contact,
  ContactConflict,
  ContactDraft,
  Duplicates,
  MeetingPrep,
  MergeRequest,
  MergeResult,
  CalendarAccount,
  CalendarAccountDraft,
  CalendarDiscovery,
  Calendar,
  CalendarSettings,
  Occurrence,
  CalendarEvent,
  EventDraft,
  EventSaveResult,
  EventScope,
  FreeBusy,
  FreeBusyQuery,
  Invite,
  InviteAnswer,
  InviteAnswerResult,
  ICSImportResult,
  ICSFile,
  EventBundle,
  MailEventSource,
  MessageRef,
  TaskList,
  Task,
  TaskDraft,
  TaskSaveResult,
  TaskDeleteResult,
  RemovedTask,
  TaskMailMark,
  TaskSettings,
  Feed,
  FeedCandidate,
  FeedEntry,
  FeedEntryDetail,
  FeedFolder,
  FeedImportResult,
  FeedScope,
  FeedSettings,
  FeedSidebar,
  AttachmentContent,
  AttachmentEntry,
  AttachmentPage,
  AttachmentQuery,
  SenderOverview,
  ThemeInfo,
  ThemeApply,
  ThemeImportPreview,
  SaveThemeRequest,
  ThemeDraft,
  UserLocale,
  UserLocaleApply,
  ProxyConfig,
  MCPConfig,
  VirusTotalConfig,
  Verdict,
  MessageScan,
  View,
  Selection,
  FetchOlderResult,
  PGPKey,
  LogStatus,
  ThunderbirdProfile,
  DevActivity,
  DevProcessStats,
  Profile,
  ProfileDraft,
  PasswordCheck,
  AccountSyncState,
  Note,
  SplitTab,
  SieveState,
  SieveSave,
  SieveVacation,
  SieveRule,
  Project,
  ProjectDraft,
  ProjectBoard,
  ProjectColumn,
  ProjectCard,
  ProjectCardDraft,
  ProjectMoveResult,
  ProjectLink,
  ProjectEvent,
  ProjectNoteRecord,
  ProjectVaultNote,
  ProjectVaultNotes,
  ProjectMailMark,
  ThreadSummary,
  LockState,
  LockAuthResult,
  QuickComposeStatus,
  VaultInfo,
  VaultSnapshot,
  VaultFile,
  VaultSave,
  VaultOp,
  VaultMention,
  VaultHit,
  ChatStatus,
  ChatServer,
  ChatRoom,
  ChatTimelinePage,
  ChatSendRequest,
  ChatMember,
  ChatCreateRoomRequest,
  ChatProfile,
  ChatVerification,
  ChatDevice,
} from './types'
import { normalizeState } from './sieve'
import type {
  CompileResult,
  Script,
  ScriptCatalog,
  ScriptCatchUp,
  ScriptFile,
  ScriptInput,
  ScriptRun,
} from './scripting'

// isDemoMode reports whether the app launched in the cosmetic --potatoes-are-nice
// screenshot mode; the frontend reads it once to switch the data layer to samples.
export function isDemoMode(): Promise<boolean> {
  return App.IsDemoMode()
}

// isDevMode reports whether the app is running against the separate dev data
// directory (PELTON_DEV), so the ui can show a persistent reminder that this
// isn't pointed at a real install.
export function isDevMode(): Promise<boolean> {
  return App.IsDevMode()
}

// windowMaterialActive reports whether the window was created over a native
// material (Liquid Glass, Mica), so the ui can let it show through.
export function windowMaterialActive(): Promise<boolean> {
  return App.WindowMaterialActive()
}

// isNightly reports whether this is an automated nightly build cut from the dev
// branch. Nightlies are untested and keep their own separate data directory, so
// the ui warns about it at every launch and marks the status bar.
export function isNightly(): Promise<boolean> {
  return App.IsNightly()
}

// consumePendingMailto returns the mailto: draft the app was launched with (if
// any) and clears it, so a reload does not reopen the same compose.
export function consumePendingMailto(): Promise<desktop.PendingMailtoDTO> {
  return App.ConsumePendingMailto()
}

// defaultMailClientStatus reports whether Pelton is the default mailto handler.
// known is false where the platform cannot answer reliably; the ui then shows
// nothing rather than guessing.
export function defaultMailClientStatus(): Promise<desktop.DefaultMailStatusDTO> {
  return App.DefaultMailClientStatus()
}

// setDefaultMailClient asks the OS to make Pelton the default mailto handler
// (a system sheet on macOS, the xdg association on Linux, the Settings page on
// Windows).
export function setDefaultMailClient(): Promise<void> {
  return App.SetDefaultMailClient()
}

// listAccounts returns every configured account.
export function listAccounts(): Promise<Account[]> {
  if (isDemoActive()) {
    return Promise.resolve(demoAccounts())
  }
  return App.ListAccounts()
}

// updateAccount persists edits to an account's display name and server
// settings. An empty password leaves the stored one alone.
export function updateAccount(req: {
  id: number
  displayName: string
  localLabel: string
  useLocalLabel: boolean
  username: string
  imapHost: string
  imapPort: number
  smtpHost: string
  smtpPort: number
  password: string
  imapTls: TLSMode
  smtpTls: TLSMode
  exportOnArchive: boolean
  exportDir: string
  exportSubfolders: string
  exportNameTemplate: string
  pgpDefault: string
  proxy: AccountProxy
}): Promise<Account> {
  return App.UpdateAccount(new desktop.UpdateAccountRequest(req))
}

// accountProxyPasswordStored reports whether a mailbox's own proxy has a
// password in the keyring, so the editor can show a placeholder for it.
export function accountProxyPasswordStored(accountId: number): Promise<boolean> {
  return App.AccountProxyPasswordStored(accountId)
}

// testAccountRoute connects to a mailbox's servers along a route, saved or
// not, and hangs up. Resolves when both answered.
export function testAccountRoute(req: {
  accountId: number
  proxy: AccountProxy
  imapHost: string
  imapPort: number
  smtpHost: string
  smtpPort: number
}): Promise<void> {
  return App.TestAccountRoute(new desktop.RouteTestRequest(req))
}

// chooseArchiveExportFolder opens a directory picker for the export-on-archive
// location, returning '' when the user cancelled.
export function chooseArchiveExportFolder(): Promise<string> {
  return App.ChooseArchiveExportFolder()
}

// previewArchiveExportName renders a file name template against a fixed sample
// message, so the settings screen can show what a pattern produces.
export function previewArchiveExportName(template: string, subfolders: string): Promise<string> {
  return App.PreviewArchiveExportName(template, subfolders)
}

// checkAccountPassword tries a password against the account's imap server
// without storing it, so the prompt can say straight away whether it works.
export function checkAccountPassword(accountId: number, password: string): Promise<PasswordCheck> {
  return App.CheckAccountPassword(accountId, password) as unknown as Promise<PasswordCheck>
}

// setAccountPassword stores a login password for an account, replacing any
// existing one. Refused for accounts that sign in with OAuth.
export function setAccountPassword(accountId: number, password: string): Promise<void> {
  return App.SetAccountPassword(accountId, password)
}

// accountOAuthProvider returns the oauth provider an account signs in with
// ('google', 'microsoft'), or empty when it uses a password.
export function accountOAuthProvider(accountId: number): Promise<string> {
  return App.AccountOAuthProvider(accountId)
}

// reauthorizeOAuthAccount runs the provider sign-in again in the browser with
// the account's stored client and replaces its tokens.
export function reauthorizeOAuthAccount(accountId: number): Promise<void> {
  return App.ReauthorizeOAuthAccount(accountId)
}

// accountsNeedingPassword lists accounts with no stored password, which is the
// state an account imported from another mail client arrives in. Accounts whose
// prompt was dismissed are still listed, so the ui can mark them.
export function accountsNeedingPassword(): Promise<Account[]> {
  if (isDemoActive()) {
    return Promise.resolve([])
  }
  return App.AccountsNeedingPassword().then((list) => (list ?? []) as unknown as Account[])
}

// dismissPasswordPrompt stops the missing-password prompt from interrupting for
// one account. The mailbox is marked in the sidebar and in Settings instead.
export function dismissPasswordPrompt(accountId: number): Promise<void> {
  if (isDemoActive()) {
    return Promise.resolve()
  }
  return App.DismissPasswordPrompt(accountId)
}

// deleteAccount removes an account, its cached mail and its keyring secret.
export function deleteAccount(id: number): Promise<void> {
  if (isDemoActive()) {
    return Promise.resolve()
  }
  return App.DeleteAccount(id)
}

// listFolders returns one account's full mailbox tree with counts.
export function listFolders(accountId: number): Promise<Folder[]> {
  if (isDemoActive()) {
    return Promise.resolve(demoFolders(accountId))
  }
  return App.ListFolders(accountId)
}

// createFolder creates a mailbox on the server and returns the new folder.
// parentId 0 creates it at the root of the account. name is one level: nesting
// comes from parentId, not from embedding the server's delimiter in the name.
export function createFolder(accountId: number, parentId: number, name: string): Promise<Folder> {
  return App.CreateFolder(new desktop.CreateFolderRequest({ accountId, parentId, name }))
}

// renameFolder renames a mailbox in place, keeping its cached mail. Special
// mailboxes (INBOX, Sent, Drafts, Trash, Junk, Archive) are refused by the
// backend.
export function renameFolder(id: number, name: string): Promise<void> {
  if (isDemoActive()) {
    return Promise.resolve()
  }
  return App.RenameFolder(id, name)
}

// deleteFolder deletes a mailbox, its subfolders and their mail, on the server
// as well as locally. Destructive and not undoable: confirm before calling.
export function deleteFolder(id: number): Promise<void> {
  if (isDemoActive()) {
    return Promise.resolve()
  }
  return App.DeleteFolder(id)
}

// emptyTrash permanently deletes every message in a trash folder and resolves
// with how many it removed. The backend refuses any folder that is not the
// account's trash. Destructive and not undoable: confirm before calling.
export function emptyTrash(folderId: number): Promise<number> {
  if (isDemoActive()) {
    return Promise.resolve(0)
  }
  return App.EmptyTrash(folderId)
}

// listUnifiedViews returns the cross-account views with aggregate counts.
export function listUnifiedViews(): Promise<UnifiedView[]> {
  if (isDemoActive()) {
    return Promise.resolve(demoViews())
  }
  return App.ListUnifiedViews()
}

// listFolderMessages reads a page of a single folder. split narrows an inbox
// to one split inbox tab; '' reads all of it.
export function listFolderMessages(
  folderId: number,
  limit: number,
  offset: number,
  split = '',
): Promise<MessageList> {
  if (isDemoActive()) {
    return Promise.resolve(demoList())
  }
  return App.ListMessages(
    new desktop.ListMessagesRequest({ kind: 'folder', folderId, view: '', limit, offset, split }),
  )
}

// listViewMessages reads a page of a unified view, split as for a folder.
export function listViewMessages(
  view: ViewKey,
  limit: number,
  offset: number,
  split = '',
): Promise<MessageList> {
  if (isDemoActive()) {
    return Promise.resolve(demoList())
  }
  return App.ListMessages(
    new desktop.ListMessagesRequest({ kind: 'view', folderId: 0, view, limit, offset, split }),
  )
}

// listSavedViewMessages reads a page of a saved View (preset search).
export function listSavedViewMessages(
  viewId: number,
  limit: number,
  offset: number,
): Promise<MessageList> {
  if (isDemoActive()) {
    return Promise.resolve(demoList())
  }
  return App.ListMessages(
    new desktop.ListMessagesRequest({ kind: 'savedView', folderId: 0, view: '', viewId, limit, offset }),
  )
}

// listFolderThreads reads a page of one folder as conversations. Paging counts
// conversations, and a conversation is listed when any of its messages is in
// the folder.
export function listFolderThreads(folderId: number, limit: number, offset: number, split = ''): Promise<ThreadList> {
  if (isDemoActive()) {
    return Promise.resolve(demoThreads())
  }
  return App.ListThreads(
    new desktop.ListMessagesRequest({ kind: 'folder', folderId, view: '', limit, offset, split }),
  )
}

// listViewThreads reads a page of a unified view as conversations.
export function listViewThreads(view: ViewKey, limit: number, offset: number, split = ''): Promise<ThreadList> {
  if (isDemoActive()) {
    return Promise.resolve(demoThreads())
  }
  return App.ListThreads(
    new desktop.ListMessagesRequest({ kind: 'view', folderId: 0, view, limit, offset, split }),
  )
}

// listSavedViewThreads reads a page of a saved View as conversations.
export function listSavedViewThreads(viewId: number, limit: number, offset: number): Promise<ThreadList> {
  if (isDemoActive()) {
    return Promise.resolve(demoThreads())
  }
  return App.ListThreads(
    new desktop.ListMessagesRequest({ kind: 'savedView', folderId: 0, view: '', viewId, limit, offset }),
  )
}

// getThread returns one conversation's messages, oldest first, with the user's
// own replies included and marked. Bodies still come from getMessage.
export function getThread(threadId: number): Promise<Thread> {
  if (isDemoActive()) {
    return Promise.resolve(demoThread(threadId))
  }
  return App.GetThread(threadId)
}

// fetchOlderMessages pulls the next batch of older mail from the server for a
// selection, for when the local cache runs out before the mailbox does. It is a
// network round trip, unlike the list* functions above which only read the
// cache. In demo mode there is no server, so it reports nothing fetched.
export function fetchOlderMessages(sel: Selection): Promise<FetchOlderResult> {
  if (isDemoActive()) {
    return Promise.resolve({ fetched: 0, hasOlder: false })
  }
  return App.FetchOlderMessages(
    new desktop.ListMessagesRequest({
      kind: sel.kind,
      folderId: sel.kind === 'folder' ? sel.folderId : 0,
      view: sel.kind === 'view' ? sel.view : '',
      viewId: sel.kind === 'savedView' ? sel.viewId : 0,
      limit: 0,
      offset: 0,
    }),
  )
}

// listViews returns every saved View with its eager-run counts.
export function listViews(): Promise<View[]> {
  if (isDemoActive()) {
    return Promise.resolve([])
  }
  return App.ListViews() as unknown as Promise<View[]>
}

// saveView creates (id 0) or updates a saved View and returns it with fresh counts.
export function saveView(view: View): Promise<View> {
  return App.SaveView(new desktop.ViewDTO(view)) as unknown as Promise<View>
}

// deleteView removes a saved View.
export function deleteView(id: number): Promise<void> {
  return App.DeleteView(id)
}

// reorderViews persists a new sidebar order for the given view ids.
export function reorderViews(orderedIds: number[]): Promise<void> {
  return App.ReorderViews(orderedIds)
}

// reorderFolders persists a new order for one group of sibling folders. Every
// id must share an account and a parent; the backend rejects a mixed group.
export function reorderFolders(orderedIds: number[]): Promise<void> {
  return App.ReorderFolders(orderedIds)
}

// reorderAccounts persists a new order for the sidebar's account sections.
export function reorderAccounts(orderedIds: number[]): Promise<void> {
  return App.ReorderAccounts(orderedIds)
}

// reorderUnifiedViews persists a new order for the unified views block, by key.
export function reorderUnifiedViews(keys: string[]): Promise<void> {
  return App.ReorderUnifiedViews(keys)
}

// listPinnedFolders returns the pinned folders across every account in the
// order of the sidebar's Pinned group.
export function listPinnedFolders(): Promise<Folder[]> {
  if (isDemoActive()) {
    return Promise.resolve([])
  }
  return App.ListPinnedFolders()
}

// setFolderPinned mirrors a folder into the Pinned group, or removes it from
// there. The folder stays in its own account's tree either way.
export function setFolderPinned(folderId: number, pinned: boolean): Promise<void> {
  return App.SetFolderPinned(folderId, pinned)
}

// setFolderSyncExcluded turns syncing for one folder off or on. Excluding does
// not delete cached mail, it only stops the folder being updated (#173).
export function setFolderSyncExcluded(folderId: number, excluded: boolean): Promise<void> {
  return App.SetFolderSyncExcluded(folderId, excluded)
}

// startAccountSync runs a newly added account's first sync and parks it on
// idle. Adding an account no longer does this on its own, so the wizard can
// show the folder list before anything is fetched.
export function startAccountSync(accountId: number): Promise<void> {
  if (isDemoActive()) {
    return Promise.resolve()
  }
  return App.StartAccountSync(accountId)
}

// assignableFolderRoles are the roles a user may give a folder by hand. Inbox is
// absent on purpose: every account has exactly one, named by the protocol, and a
// second would break the unified inbox.
export const assignableFolderRoles = ['normal', 'sent', 'drafts', 'trash', 'junk', 'archive'] as const

// setFolderRole assigns a folder's role by hand, overriding what the server
// reported and what its name suggests. Pass an empty role to go back to
// automatic detection. Nothing is sent to the server: the role is local
// classification, so this never renames or moves the mailbox.
export function setFolderRole(folderId: number, role: string): Promise<void> {
  return App.SetFolderRole(folderId, role)
}

// reorderPinnedFolders persists a new order for the Pinned group.
export function reorderPinnedFolders(orderedIds: number[]): Promise<void> {
  return App.ReorderPinnedFolders(orderedIds)
}

// getMessage returns the full message with sanitized body and attachments.
export function getMessage(id: number): Promise<MessageDetail> {
  if (isDemoActive()) {
    return Promise.resolve(demoMessage(id))
  }
  return App.GetMessage(id) as unknown as Promise<MessageDetail>
}

// unsubscribeMessage carries out a message's advertised unsubscribe (the
// one-click POST or the mailto send); plain links are opened by the caller.
export function unsubscribeMessage(id: number): Promise<void> {
  if (isDemoActive()) {
    return Promise.resolve()
  }
  return App.Unsubscribe(id)
}

// getMessageSource fetches a message's raw RFC 822 source on demand over imap.
export function getMessageSource(id: number): Promise<string> {
  if (isDemoActive()) {
    return Promise.resolve(demoSource(id))
  }
  return App.GetMessageSource(id)
}

// getMessageHtml re-renders a body with the chosen remote-image policy.
// includeTrackers additionally loads the images detection flagged as tracking
// pixels, which the setting otherwise keeps blocked even here.
export function getMessageHtml(id: number, allowRemote: boolean, includeTrackers = false): Promise<string> {
  if (isDemoActive()) {
    return Promise.resolve(demoHtml())
  }
  return App.GetMessageHTML(id, allowRemote, includeTrackers)
}

// setSeen / setFlagged toggle a flag and queue the change for sync.
export function setSeen(id: number, seen: boolean): Promise<void> {
  if (isDemoActive()) {
    return Promise.resolve()
  }
  return App.SetSeen(id, seen)
}

export function setFlagged(id: number, flagged: boolean): Promise<void> {
  noteAction('flag')
  if (isDemoActive()) {
    return Promise.resolve()
  }
  return App.SetFlagged(id, flagged)
}

// deleteMessage marks a message for server-side deletion on the next sync.
export function deleteMessage(id: number): Promise<void> {
  noteAction('delete-message')
  if (isDemoActive()) {
    return Promise.resolve()
  }
  return App.DeleteMessage(id)
}

// undoDelete reverses a pending delete while the message is still cached.
export function undoDelete(id: number): Promise<void> {
  if (isDemoActive()) {
    return Promise.resolve()
  }
  return App.UndoDelete(id)
}

// ArchiveUndo is what undo needs after a move: the message's stable rfc
// Message-ID, the folder it came from and the folder it went to. messageId is
// empty when the message had no Message-ID (undo not possible then).
export interface ArchiveUndo {
  messageId: string
  originalFolderId: number
  destFolderId: number
  // the .eml copy the account's export-on-archive option wrote, '' when the
  // option is off. exportError explains why no copy was written when one was
  // expected: the archive itself still succeeded.
  exportPath: string
  exportError: string
}

// composeProtectionStatus reports what signing and encryption are possible for
// a message from this account to these recipients, so the compose controls
// never offer something that would fail at send time.
export function composeProtectionStatus(
  accountId: number,
  recipients: string[],
): Promise<desktop.ProtectionStatusDTO> {
  return App.ComposeProtectionStatus(accountId, recipients)
}

// signerFingerprint is the key an account signs with, so the compose window can
// point the passphrase prompt at the right one. '' when there is none.
export function signerFingerprint(accountId: number): Promise<string> {
  return App.SignerFingerprint(accountId)
}

// --- mcp write actions (#127) ---

// mcpPermissions returns every write tool an agent could use and whether it is
// allowed. All off until switched on.
export function mcpPermissions(): Promise<MCPPermission[]> {
  return App.MCPPermissions().then((p) => p ?? [])
}

// setMCPPermission turns one write tool on or off and restarts the server.
export function setMCPPermission(tool: string, allowed: boolean): Promise<void> {
  return App.SetMCPPermission(tool, allowed)
}

// agentActions returns the log of writes agents have made.
export function agentActions(): Promise<AgentAction[]> {
  return App.AgentActions().then((a) => a ?? [])
}

// clearAgentActions empties that log.
export function clearAgentActions(): Promise<void> {
  return App.ClearAgentActions()
}

// agentProposals returns messages an agent wants sent, awaiting approval.
export function agentProposals(): Promise<AgentProposal[]> {
  return App.AgentProposals().then((p) => p ?? [])
}

// approveAgentProposal sends a proposed message through the ordinary send path.
export function approveAgentProposal(id: number): Promise<void> {
  return App.ApproveAgentProposal(id)
}

// discardAgentProposal throws a proposed message away unsent.
export function discardAgentProposal(id: number): Promise<void> {
  return App.DiscardAgentProposal(id)
}

// --- s/mime revocation (#226) ---

// checkSMIMERevocation asks the signing certificate's authority whether it has
// been withdrawn. Returns a '' status when the setting is off or the message
// carries nothing to check, so the caller can call it unconditionally.
export function checkSMIMERevocation(messageId: number): Promise<SMIMERevocation> {
  return App.CheckSMIMERevocation(messageId)
}

// smimeRevocationEnabled reports whether revocation checking is on.
export function smimeRevocationEnabled(): Promise<boolean> {
  return App.SMIMERevocationEnabled()
}

// setSMIMERevocation turns revocation checking on or off. Turning it off also
// empties the cache of past answers.
export function setSMIMERevocation(on: boolean): Promise<void> {
  return App.SetSMIMERevocation(on)
}

// unsealDraft opens a draft of an encrypted message with a passphrase.
export function unsealDraft(id: number, passphrase: string): Promise<desktop.DraftDTO> {
  return App.UnsealDraft(id, passphrase)
}

// noUndo is what demo mode answers a move with: nothing to undo.
const noUndo: ArchiveUndo = { messageId: '', originalFolderId: 0, destFolderId: 0, exportPath: '', exportError: '' }

// archiveMessage moves a message to its account's Archive folder on the server,
// returning the info needed to undo it.
export function archiveMessage(id: number): Promise<ArchiveUndo> {
  noteAction('archive')
  if (isDemoActive()) {
    return Promise.resolve(noUndo)
  }
  return App.ArchiveMessage(id)
}

// restoreMessage undoes a move: it finds the message in fromFolderId by rfc
// Message-ID and moves it back to toFolderId.
export function restoreMessage(messageId: string, fromFolderId: number, toFolderId: number): Promise<void> {
  if (isDemoActive()) {
    return Promise.resolve()
  }
  return App.RestoreMessage(messageId, fromFolderId, toFolderId)
}

// moveMessage moves a message to any folder of its account, returning undo info.
export function moveMessage(id: number, destFolderId: number): Promise<ArchiveUndo> {
  if (isDemoActive()) {
    return Promise.resolve(noUndo)
  }
  return App.MoveMessage(id, destFolderId)
}

// markSpam moves a message to its account's spam folder, tagging it $Junk where
// the server keeps keywords, and returns undo info.
export function markSpam(id: number): Promise<ArchiveUndo> {
  if (isDemoActive()) {
    return Promise.resolve(noUndo)
  }
  return App.MarkSpam(id)
}

// markNotSpam moves a message out of the spam folder, back to where it was
// marked from or else the inbox, tagging it $NotJunk, and returns undo info.
export function markNotSpam(id: number): Promise<ArchiveUndo> {
  if (isDemoActive()) {
    return Promise.resolve(noUndo)
  }
  return App.MarkNotSpam(id)
}

/** What a bulk action did: the ids it acted on, and the first failure. */
export interface BulkResult {
  done: number[]
  failed: number
  firstError: string
}

/** A bulk archive: BulkResult plus the undo record of every moved message. */
export interface BulkArchive extends BulkResult {
  undo: ArchiveUndo[]
}

const noBulk = (): BulkResult => ({ done: [], failed: 0, firstError: '' })

/** Deletes many messages at once; undoDeletes brings them back until the next sync. */
export function deleteMessages(ids: number[]): Promise<BulkResult> {
  if (isDemoActive()) {
    return Promise.resolve({ ...noBulk(), done: ids })
  }
  return App.DeleteMessages(ids) as unknown as Promise<BulkResult>
}

/** Reverses deleteMessages for the messages still cached. */
export function undoDeletes(ids: number[]): Promise<BulkResult> {
  if (isDemoActive()) {
    return Promise.resolve({ ...noBulk(), done: ids })
  }
  return App.UndoDeletes(ids) as unknown as Promise<BulkResult>
}

/** Archives many messages with one server session per account. */
export function archiveMessages(ids: number[]): Promise<BulkArchive> {
  if (isDemoActive()) {
    return Promise.resolve({ ...noBulk(), undo: [] })
  }
  return App.ArchiveMessages(ids) as unknown as Promise<BulkArchive>
}

/** Picks the newsletter mail of some senders for a bulk action. */
export interface NewsletterSelection {
  emails: string[]
  /** Above 0 keeps only mail older than that many days. */
  olderThanDays: number
  /** Leaves each sender's newest message out. */
  keepLatest: boolean
  /** Leaves out mail already in an archive folder. */
  forArchive: boolean
}

/** Every sender whose mail offers List-Unsubscribe, aggregated in the store. */
export function listNewsletters(): Promise<NewsletterSender[]> {
  if (isDemoActive()) {
    return Promise.resolve([])
  }
  return App.ListNewsletters() as unknown as Promise<NewsletterSender[]>
}

/** The ids of the newsletter mail a selection picks. */
export function newsletterMessageIds(sel: NewsletterSelection): Promise<number[]> {
  if (isDemoActive()) {
    return Promise.resolve([])
  }
  return App.NewsletterMessageIDs(new desktop.NewsletterSelectionDTO(sel))
}

/** The storage section of the insights page, computed from the local store. */
export function insightsStorage(): Promise<InsightsStorage> {
  if (isDemoActive()) {
    return Promise.resolve({ accounts: [], senders: [], attachments: [] })
  }
  return App.InsightsStorage() as unknown as Promise<InsightsStorage>
}

/** The volume section over the last `days` days, 0 for all mail. */
export function insightsVolume(days: number): Promise<InsightsVolume> {
  if (isDemoActive()) {
    return Promise.resolve({ since: '', days: [], hours: [], hasSent: false })
  }
  return App.InsightsVolume(days) as unknown as Promise<InsightsVolume>
}

/** The people section over the last `days` days, 0 for all mail. */
export function insightsPeople(days: number): Promise<InsightsPeople> {
  if (isDemoActive()) {
    const none = { available: false, count: 0, medianSeconds: 0 }
    return Promise.resolve({ writesTo: [], writesYou: [], myReplies: none, theirReplies: none })
  }
  return App.InsightsPeople(days) as unknown as Promise<InsightsPeople>
}

// SearchRequest is a ranked search over cached mail: free text plus an optional
// date window. afterUnix/beforeUnix are unix seconds; 0 leaves that side open.
export interface SearchRequest {
  query: string
  afterUnix: number
  beforeUnix: number
  limit: number
  // offset skips that many ranked hits, so the list can page through a result
  // set instead of stopping at the first page.
  offset: number
  // field-scoped constraints from typed search chips (from:/to:/subject:).
  from: string
  to: string
  subject: string
  hasAttachment: boolean
  unreadOnly: boolean
  flaggedOnly?: boolean
  // accountId and folderId scope the search to one account or folder; 0 or
  // absent searches everywhere.
  accountId?: number
  folderId?: number
  // the order results come back in, already resolved from the "automatic"
  // setting: the backend is told an order, never asked to guess one.
  sort: SearchSort
  // note keeps messages whose private note contains this text, hasNote every
  // message that has one.
  note?: string
  hasNote?: boolean
}

// SearchResult is one page of ranked results. total counts index matches and is
// an upper bound on what is shown, since the attachment filter and any message
// deleted since it was indexed are applied per page after ranking.
export interface SearchResult {
  messages: MessageSummary[]
  total: number
}

// search runs a ranked, typo-tolerant search and returns a page of matching
// summaries in relevance order.
export function search(req: SearchRequest): Promise<SearchResult> {
  if (isDemoActive()) {
    return Promise.resolve(demoSearch())
  }
  return App.Search(new desktop.SearchRequestDTO(req))
}

// MessageIDs is every id a list matches. capped means the backend stopped at
// its limit, so the selection is the newest ids rather than all of them.
export interface MessageIDs {
  ids: number[]
  capped: boolean
  matching: number
}

// messageIds returns every message id in a list, ignoring paging, for select
// all. The list in the ui only holds the pages that were scrolled to, so
// selecting a whole mailbox has to come from the query.
export function messageIds(sel: Selection, split = ''): Promise<MessageIDs> {
  if (isDemoActive()) {
    return Promise.resolve({ ids: [], capped: false, matching: 0 })
  }
  if (sel.kind === 'view') {
    return App.MessageIDs(
      new desktop.ListMessagesRequest({ kind: 'view', folderId: 0, view: sel.view, limit: 0, offset: 0, split }),
    )
  }
  if (sel.kind === 'savedView') {
    return App.MessageIDs(
      new desktop.ListMessagesRequest({ kind: 'savedView', folderId: 0, view: '', viewId: sel.viewId, limit: 0, offset: 0 }),
    )
  }
  return App.MessageIDs(
    new desktop.ListMessagesRequest({ kind: 'folder', folderId: sel.folderId, view: '', limit: 0, offset: 0, split }),
  )
}

// searchMessageIds is messageIds for a result set: every match of the current
// search, ids only.
export function searchMessageIds(req: SearchRequest): Promise<MessageIDs> {
  if (isDemoActive()) {
    return Promise.resolve({ ids: [], capped: false, matching: 0 })
  }
  return App.SearchMessageIDs(new desktop.SearchRequestDTO(req))
}

// saveAttachment prompts for a path and writes the file, returning the path or
// an empty string if the user cancelled.
export function saveAttachment(messageId: number, attachmentId: number): Promise<string> {
  if (isDemoActive()) {
    return Promise.resolve('')
  }
  return App.SaveAttachment(messageId, attachmentId)
}

// sendMessage enqueues a message in the durable outbox. the plain request is
// wrapped back into the generated class the binding expects.
export function sendMessage(req: ComposeRequest): Promise<number> {
  if (isDemoActive()) {
    return Promise.resolve(0)
  }
  return App.SendMessage(new desktop.ComposeRequest(req))
}

// saveDraft stores a compose request as a local draft, returning its id.
export function saveDraft(id: number, req: ComposeRequest): Promise<number> {
  if (isDemoActive()) {
    return Promise.resolve(0)
  }
  return App.SaveDraft(id, new desktop.ComposeRequest(req))
}

export function deleteDraft(id: number): Promise<void> {
  if (isDemoActive()) {
    return Promise.resolve()
  }
  return App.DeleteDraft(id)
}

// uploadDraft puts a saved local draft into the account's server Drafts folder,
// replacing its previous copy there, and returns the Message-ID it used ('' for a
// local-only account). Encrypted drafts are refused: they stay on this device.
export function uploadDraft(id: number): Promise<string> {
  if (isDemoActive()) {
    return Promise.resolve('')
  }
  return App.UploadDraft(id)
}

// openDraft looks up the local draft behind a message in a Drafts folder. A
// draft with id 0 means Pelton on this device did not save that message.
export function openDraft(messageId: number): Promise<Draft> {
  if (isDemoActive()) {
    return Promise.resolve({ id: 0, savedAt: '', request: {} as ComposeRequest, locked: false, accountId: 0, protection: '' })
  }
  return App.OpenDraft(messageId)
}

// removeServerDraft deletes a draft's copy from the server Drafts folder. An
// empty messageId is a no-op.
export function removeServerDraft(accountId: number, messageId: string): Promise<void> {
  if (isDemoActive()) {
    return Promise.resolve()
  }
  return App.RemoveServerDraft(accountId, messageId)
}

// listOutbox returns the current outbox contents.
export function listOutbox(): Promise<OutboxRow[]> {
  if (isDemoActive()) {
    return Promise.resolve(demoOutbox())
  }
  return App.ListOutbox()
}

// triggerSync runs one sync pass on demand.
export function triggerSync(): Promise<void> {
  if (isDemoActive()) {
    return Promise.resolve()
  }
  return App.TriggerSync()
}

// accountSyncStates returns how each account's last sync went. Accounts that
// have never synced are absent rather than reported as failing.
export function accountSyncStates(): Promise<AccountSyncState[]> {
  if (isDemoActive()) {
    return Promise.resolve([])
  }
  return App.AccountSyncStates().then((list) => (list ?? []) as unknown as AccountSyncState[])
}

// syncAccountNow syncs one account, for the retry on a failed mailbox. It
// rejects with what went wrong this time.
export function syncAccountNow(accountId: number): Promise<void> {
  if (isDemoActive()) {
    return Promise.resolve()
  }
  return App.SyncAccountNow(accountId)
}

// appVersion returns the build version string (injected via ldflags), shown in
// the about section.
export function appVersion(): Promise<string> {
  return App.AppVersion()
}

// UpdateCheckResult mirrors the go DTO of the same name.
export interface UpdateCheckResult {
  checked: boolean
  available: boolean
  currentVersion: string
  latestVersion: string
  releaseUrl: string
  error: string
}

// checkForUpdates does an immediate GitHub-releases check (the "Check now"
// button in settings), regardless of the update_check_frequency setting.
export function checkForUpdates(): Promise<UpdateCheckResult> {
  return App.CheckForUpdates()
}

// cancelSend pulls a still-queued message back out of the outbox during its
// undo-send delay window, resolving true when it was cancelled in time.
export function cancelSend(id: number): Promise<boolean> {
  return App.CancelSend(id)
}

// listFollowUps returns every follow-up reminder, waiting and fired, the
// soonest due first.
export function listFollowUps(): Promise<FollowUp[]> {
  if (isDemoActive()) {
    return Promise.resolve([])
  }
  return App.ListFollowUps().then((list) => list ?? [])
}

// listSnippets returns every snippet in the user's order.
export function listSnippets(): Promise<Snippet[]> {
  if (isDemoActive()) {
    return Promise.resolve([])
  }
  return App.ListSnippets().then((list) => list ?? [])
}

// saveSnippet creates a snippet (id 0) or updates one, returning it as stored.
export function saveSnippet(snippet: Snippet): Promise<Snippet> {
  return App.SaveSnippet(snippet) as Promise<Snippet>
}

// deleteSnippet removes a snippet.
export function deleteSnippet(id: number): Promise<void> {
  return App.DeleteSnippet(id)
}

// reorderSnippets stores the order the snippets are listed in.
export function reorderSnippets(ids: number[]): Promise<void> {
  return App.ReorderSnippets(ids)
}

// cancelFollowUp drops a reminder, waiting or fired.
export function cancelFollowUp(id: number): Promise<void> {
  return App.CancelFollowUp(id)
}

// clearSentOutbox prunes rows already marked sent after the ui has shown the
// brief sent confirmation.
export function clearSentOutbox(): Promise<void> {
  return App.ClearSentOutbox()
}

// retrySend puts a failed message back in the send queue with a fresh attempt
// budget. Resolves false when it was no longer failed.
export function retrySend(id: number): Promise<boolean> {
  return App.RetrySend(id)
}

// discardFailedSend removes a failed message from the outbox. The message is
// gone afterwards, so the caller confirms first.
export function discardFailedSend(id: number): Promise<boolean> {
  return App.DiscardFailedSend(id)
}

// sendOutboxNow makes a scheduled message due at once. Resolves false when it
// was already on its way.
export function sendOutboxNow(id: number): Promise<boolean> {
  return App.SendOutboxNow(id)
}

// editOutboxMessage takes a scheduled or failed message back out of the outbox
// and returns the request it was sent with; taken is false when it could not
// be taken back.
export function editOutboxMessage(id: number): Promise<OutboxEdit> {
  return App.EditOutboxMessage(id) as unknown as Promise<OutboxEdit>
}

// cancelOutboxSend stops a message that is waiting or being transmitted and
// says where it went: back to Drafts, back as a request for a composer, kept
// as a failed send, or nowhere because it was too late.
export function cancelOutboxSend(id: number): Promise<OutboxCancel> {
  return App.CancelOutboxSend(id) as unknown as Promise<OutboxCancel>
}

// prioritiseOutboxSend moves a waiting message to the front of the queue.
// Resolves false when it was no longer waiting.
export function prioritiseOutboxSend(id: number): Promise<boolean> {
  return App.PrioritiseOutboxSend(id)
}

// readOutboxLog returns the send activity of this run, oldest first.
export function readOutboxLog(): Promise<OutboxLogEntry[]> {
  if (isDemoActive()) {
    return Promise.resolve([])
  }
  return App.ReadOutboxLog().then((list) => (list ?? []) as OutboxLogEntry[])
}

// openOutboxWindow shows the send activity in a window of its own, or brings
// the one already open forward.
export function openOutboxWindow(): Promise<void> {
  return App.OpenOutboxWindow()
}

/** Focus mode as it stands now; the backend ends an elapsed one first. */
export function getFocus(): Promise<FocusInfo> {
  return App.GetFocus()
}

/** Turns focus on until the given time, or until it is turned off with null. */
export function startFocus(until: Date | null): Promise<FocusInfo> {
  return App.StartFocus(until ? until.toISOString().replace(/\.\d{3}Z$/, 'Z') : '')
}

/** Turns focus off now; the summary arrives as a focus:ended event. */
export function endFocus(): Promise<void> {
  return App.EndFocus()
}

/** The days and hours focus starts on its own. */
export function getFocusSchedule(): Promise<FocusSchedule> {
  return App.GetFocusSchedule().then((s) => ({ ...s, days: s.days ?? [] }))
}

/** Stores the focus schedule; a window already open starts focus at once. */
export function setFocusSchedule(schedule: FocusSchedule): Promise<void> {
  return App.SetFocusSchedule(schedule)
}

// trustSenderImages permanently allows remote content from a message's sender.
export function trustSenderImages(messageId: number): Promise<void> {
  if (isDemoActive()) {
    return Promise.resolve()
  }
  return App.TrustSenderImages(messageId)
}

// allowDomainImages permanently allows remote content from a sender's domain.
export function allowDomainImages(messageId: number): Promise<void> {
  if (isDemoActive()) {
    return Promise.resolve()
  }
  return App.AllowDomainImages(messageId)
}

// allowRemoteForMessage permanently allows remote content for this one message,
// without trusting the whole sender or domain.
export function allowRemoteForMessage(messageId: number): Promise<void> {
  if (isDemoActive()) {
    return Promise.resolve()
  }
  return App.AllowRemoteForMessage(messageId)
}

// ImageAllowEntry is one trusted sender or domain in the remote-image
// allowlist, with an example cached message when one exists.
export type ImageAllowEntry = desktop.ImageAllowEntryDTO

// listImageAllowlist returns every trusted sender and domain the user has
// allowed remote content for.
export function listImageAllowlist(): Promise<ImageAllowEntry[]> {
  return App.ListImageAllowlist()
}

// removeImageAllow removes a trusted sender or domain from the allowlist.
export function removeImageAllow(kind: 'sender' | 'domain', value: string): Promise<void> {
  return App.RemoveImageAllow(kind, value)
}

/** How the reader shows one sender's mail; empty colors and 0 follow the global settings. */
export interface SenderReading {
  address: string
  colors: '' | 'adapt' | 'original'
  fontSize: number
}

/** listSenderReading returns every sender with a remembered reading choice. */
export async function listSenderReading(): Promise<SenderReading[]> {
  if (isDemoActive()) {
    return []
  }
  return (await App.ListSenderReading()).map((r) => ({ address: r.address, colors: r.colors as SenderReading['colors'], fontSize: r.fontSize }))
}

/** setSenderColors remembers how html mail from a sender is coloured; '' forgets it. */
export function setSenderColors(address: string, colors: SenderReading['colors']): Promise<void> {
  if (isDemoActive()) {
    return Promise.resolve()
  }
  return App.SetSenderColors(address, colors)
}

/** setSenderFontSize remembers the reader text size for a sender in px; 0 forgets it. */
export function setSenderFontSize(address: string, size: number): Promise<void> {
  if (isDemoActive()) {
    return Promise.resolve()
  }
  return App.SetSenderFontSize(address, size)
}

/** resetSenderReading forgets every remembered sender choice and says how many there were. */
export function resetSenderReading(): Promise<number> {
  if (isDemoActive()) {
    return Promise.resolve(0)
  }
  return App.ResetSenderReading()
}

// listVIPSenders returns the addresses the user has marked as VIP (#126).
export function listVIPSenders(): Promise<string[]> {
  return App.ListVIPSenders()
}

// addVIPSender marks an address as VIP. The backend normalizes it to the bare
// lowercased address.
export function addVIPSender(address: string): Promise<void> {
  return App.AddVIPSender(address)
}

// removeVIPSender drops an address from the VIP list.
export function removeVIPSender(address: string): Promise<void> {
  return App.RemoveVIPSender(address)
}

// markSenderVIP adds a message's sender to the VIP list.
export function markSenderVIP(messageId: number): Promise<void> {
  if (isDemoActive()) {
    return Promise.resolve()
  }
  return App.MarkSenderVIP(messageId)
}

// unmarkSenderVIP removes a message's sender from the VIP list.
export function unmarkSenderVIP(messageId: number): Promise<void> {
  if (isDemoActive()) {
    return Promise.resolve()
  }
  return App.UnmarkSenderVIP(messageId)
}

// senderPhotos resolves the ordered list of remote photo candidates for a sender
// under the configured fallback chain. empty means "no network source"; the ui
// then draws a generated placeholder.
export function senderPhotos(email: string): Promise<string[]> {
  if (isDemoActive()) {
    // demo mode stays offline: fall back to the generated avatars, no network.
    return Promise.resolve([])
  }
  return App.SenderPhotos(email)
}

// exportMessagePrintView opens a print-ready view of a message in the system
// browser, where it can be saved as a pdf or printed.
export function exportMessagePrintView(id: number): Promise<void> {
  if (isDemoActive()) {
    return Promise.resolve()
  }
  return App.ExportMessagePrintView(id)
}

// exportMessageEml asks where to save one message and writes it as an .eml
// file, returning the path written ('' when the user cancelled).
export function exportMessageEml(id: number): Promise<string> {
  if (isDemoActive()) {
    return Promise.resolve('')
  }
  return App.ExportMessageEML(id)
}

// exportMessagesEml asks for a folder and writes one .eml file per message into
// it. dir is '' when the user cancelled; failed counts messages that could not
// be exported and firstError says why the first one failed.
export function exportMessagesEml(ids: number[]): Promise<desktop.EmlExportDTO> {
  if (isDemoActive()) {
    return Promise.resolve(new desktop.EmlExportDTO({ dir: '', exported: 0, failed: 0, firstError: '' }))
  }
  return App.ExportMessagesEML(ids)
}

// LicenseEntry is one third-party dependency's license record.
export interface LicenseEntry {
  group: string
  name: string
  license: string
  text: string
}

// licenses returns the embedded third-party license manifest. it is fetched only
// when the about section's licenses view is opened, so the text stays out of the
// main bundle.
export async function licenses(): Promise<LicenseEntry[]> {
  const raw = await App.Licenses()
  try {
    return JSON.parse(raw) as LicenseEntry[]
  } catch {
    return []
  }
}

// programLicense returns Pelton's own license text (GPL-3.0).
export function programLicense(): Promise<string> {
  return App.ProgramLicense()
}

// --- add-mailbox wizard ---

// discoverConfig resolves likely imap/smtp settings for an email address.
export function discoverConfig(email: string): Promise<Discovered> {
  return App.DiscoverConfig(email)
}

// listOAuthProviders returns supported oauth provider keys mapped to labels.
export function listOAuthProviders(): Promise<Record<string, string>> {
  return App.ListOAuthProviders() as Promise<Record<string, string>>
}

// testConnection verifies imap credentials by logging in. It resolves with the
// server certificates that need reviewing when one did not verify, and with an
// empty list once the login worked.
export function testConnection(req: TestConnectionRequest): Promise<ConnectionTest> {
  return App.TestConnection(new desktop.TestConnectionRequest(req)) as unknown as Promise<ConnectionTest>
}

// probeAccountCertificates lists the certificates an account's servers present
// that it does not trust, so they can be shown before trusting them.
export function probeAccountCertificates(accountId: number): Promise<UntrustedCert[]> {
  return App.ProbeAccountCertificates(accountId) as unknown as Promise<UntrustedCert[]>
}

// trustAccountCertificate trusts a certificate the account's server presents.
export function trustAccountCertificate(accountId: number, fingerprint: string): Promise<void> {
  return App.TrustAccountCertificate(accountId, fingerprint)
}

// removeAccountTrustedCertificate stops trusting a pinned certificate.
export function removeAccountTrustedCertificate(accountId: number, fingerprint: string): Promise<void> {
  return App.RemoveAccountTrustedCertificate(accountId, fingerprint)
}

// chooseCAFile opens a picker for a CA certificate and returns it once it
// parses; pem is empty when the picker was cancelled.
export function chooseCAFile(): Promise<CAFile> {
  return App.ChooseCAFile() as unknown as Promise<CAFile>
}

// setAccountCA stores the CA an account trusts, empty to remove it.
export function setAccountCA(accountId: number, pem: string): Promise<void> {
  return App.SetAccountCA(accountId, pem)
}

// setAccountColor sets the colour marking an account's mail in mixed views;
// '' removes it.
export function setAccountColor(accountId: number, color: string): Promise<void> {
  return App.SetAccountColor(accountId, color)
}

// addPasswordAccount creates a password-authenticated account (stores the
// password in the keyring, discovers folders, syncs).
export function addPasswordAccount(req: AddAccountRequest): Promise<Account> {
  return App.AddPasswordAccount(new desktop.AddAccountRequest(req))
}

// addOAuthAccount runs the interactive PKCE flow then creates the account.
export function addOAuthAccount(req: AddAccountRequest): Promise<Account> {
  return App.AddOAuthAccount(new desktop.AddAccountRequest(req))
}

// --- signatures (header/footer blocks) ---

// listSignatures returns every signature block. the generated dto types kind as
// a plain string; we narrow back to the Signature union at this boundary.
export function listSignatures(): Promise<Signature[]> {
  return App.ListSignatures() as Promise<Signature[]>
}

// saveSignature creates the block when id is 0, otherwise updates it; resolves to
// the stored block (with its id).
export function saveSignature(s: Signature): Promise<Signature> {
  return App.SaveSignature(new desktop.SignatureDTO(s)) as Promise<Signature>
}

// deleteSignature removes a block; accounts defaulting to it have the slot cleared.
export function deleteSignature(id: number): Promise<void> {
  return App.DeleteSignature(id)
}

// getAccountSignatures returns an account's default header/footer ids (0 = none).
export function getAccountSignatures(accountId: number): Promise<AccountSignatures> {
  return App.GetAccountSignatures(accountId)
}

// setAccountSignatures sets an account's default header/footer (0 clears a slot).
export function setAccountSignatures(
  accountId: number,
  headerId: number,
  footerId: number,
): Promise<void> {
  return App.SetAccountSignatures(accountId, headerId, footerId)
}

// --- flag color, snooze, offline ---

// setFlagColor sets a message's color label (0 clears, 1..8 pick a color).
export function setFlagColor(id: number, color: number): Promise<void> {
  if (isDemoActive()) {
    return Promise.resolve()
  }
  return App.SetFlagColor(id, color)
}

/** Mutes or unmutes the conversation a message belongs to. */
export function setConversationMuted(id: number, muted: boolean): Promise<void> {
  if (isDemoActive()) {
    return Promise.resolve()
  }
  return App.SetConversationMuted(id, muted)
}

/**
 * Pins or unpins a message. With conversation set, unpinning clears every
 * message of its conversation.
 */
export function setPinned(id: number, pinned: boolean, conversation: boolean): Promise<void> {
  if (isDemoActive()) {
    return Promise.resolve()
  }
  return App.SetPinned(id, pinned, conversation)
}

/** Reads the private note on a message, or on its conversation. */
export function getNote(id: number, conversation: boolean): Promise<Note> {
  if (isDemoActive()) {
    return Promise.resolve({ body: '', updatedAt: '' })
  }
  return App.GetNote(id, conversation)
}

/** Writes the private note on a message, or on its conversation; a blank body deletes it. */
export function saveNote(id: number, conversation: boolean, body: string): Promise<Note> {
  if (isDemoActive()) {
    return Promise.resolve({ body, updatedAt: '' })
  }
  return App.SaveNote(id, conversation, body)
}

/** Deletes the private note on a message, or on its conversation. */
export function deleteNote(id: number, conversation: boolean): Promise<void> {
  if (isDemoActive()) {
    return Promise.resolve()
  }
  return App.DeleteNote(id, conversation)
}

/** Counts each split inbox tab of an inbox selection; empty for anything else. */
export function splitCounts(sel: Selection): Promise<SplitTab[]> {
  if (isDemoActive() || sel.kind === 'savedView') {
    return Promise.resolve([])
  }
  return App.SplitCounts(
    new desktop.ListMessagesRequest({
      kind: sel.kind,
      folderId: sel.kind === 'folder' ? sel.folderId : 0,
      view: sel.kind === 'view' ? sel.view : '',
      limit: 0,
      offset: 0,
    }),
  )
}

// downloadMessageOffline / removeOffline pin or unpin a single message.
export function downloadMessageOffline(id: number): Promise<void> {
  if (isDemoActive()) {
    return Promise.resolve()
  }
  return App.DownloadMessageOffline(id)
}

export function removeOffline(id: number): Promise<void> {
  if (isDemoActive()) {
    return Promise.resolve()
  }
  return App.RemoveOffline(id)
}

// snoozeMessage schedules a message to resurface at untilRFC3339; hideNow also
// hides it from the list until then.
export function snoozeMessage(id: number, untilRFC3339: string, hideNow: boolean): Promise<void> {
  if (isDemoActive()) {
    return Promise.resolve()
  }
  return App.SnoozeMessage(id, untilRFC3339, hideNow)
}

export function unsnoozeMessage(id: number): Promise<void> {
  if (isDemoActive()) {
    return Promise.resolve()
  }
  return App.UnsnoozeMessage(id)
}

// --- attachments (preview, save-all) ---

// readAttachment returns an attachment's bytes for the in-app previewer.
export function readAttachment(messageId: number, attachmentId: number): Promise<AttachmentContent> {
  if (isDemoActive()) {
    return Promise.resolve({ filename: '', contentType: '', sizeBytes: 0, data: '', tooLarge: false })
  }
  return App.ReadAttachment(messageId, attachmentId)
}

// saveAllAttachments prompts for a directory and writes every attachment there,
// returning the directory (empty if cancelled).
export function saveAllAttachments(messageId: number): Promise<string> {
  if (isDemoActive()) {
    return Promise.resolve('')
  }
  return App.SaveAllAttachments(messageId)
}

/** queryAttachments reads one page of the attachments browser, newest first. */
export async function queryAttachments(q: AttachmentQuery): Promise<AttachmentPage> {
  if (isDemoActive()) {
    return { entries: [], total: q.withTotal ? 0 : -1 }
  }
  const page = await App.QueryAttachments(q)
  return { entries: (page.entries ?? []) as AttachmentEntry[], total: page.total }
}

/**
 * fetchAttachment makes sure an attachment's bytes are stored here, fetching
 * its message from the server when they are not, and returns the entry as it
 * now stands.
 */
export async function fetchAttachment(attachmentId: number): Promise<AttachmentEntry> {
  return (await App.FetchAttachment(attachmentId)) as AttachmentEntry
}

/** openAttachmentFile opens an attachment in the application the system has for it. */
export function openAttachmentFile(messageId: number, attachmentId: number): Promise<void> {
  if (isDemoActive()) {
    return Promise.resolve()
  }
  return App.OpenAttachmentFile(messageId, attachmentId)
}

/**
 * saveAttachmentsTo asks for a folder and saves the given attachments there,
 * returning the folder, or '' when the dialog was cancelled.
 */
export function saveAttachmentsTo(refs: { messageId: number; attachmentId: number }[]): Promise<string> {
  if (isDemoActive()) {
    return Promise.resolve('')
  }
  return App.SaveAttachmentsTo(refs)
}

/** senderOverview returns what the local mail says about one address. */
export async function senderOverview(address: string): Promise<SenderOverview> {
  if (isDemoActive()) {
    return { address, count: 0, first: '', last: '', recent: [], files: [], filesTotal: 0, automated: false }
  }
  const o = await App.SenderOverview(address)
  return {
    ...o,
    recent: (o.recent ?? []) as MessageSummary[],
    files: (o.files ?? []) as AttachmentEntry[],
  }
}

// --- offline range download ---

// downloadRange downloads all mail since the start date that is not cached yet.
export function downloadRange(startDate: string, includeAttachments: boolean): Promise<void> {
  return App.DownloadRange(startDate, includeAttachments)
}

// cancelDownload stops a running bulk offline download and clears its resume
// marker so it does not restart on the next launch.
export function cancelDownload(): Promise<void> {
  return App.CancelDownload()
}

// --- import / export ---

// BackupInfo describes a picked backup file before importing it.
export type BackupInfo = desktop.BackupInfoDTO

// exportData writes the selected categories to a user-chosen json file,
// returning its path (empty if the save dialog was cancelled). credentialPassword,
// when non-empty, also encrypts and includes each exported mailbox's stored
// credential; pass an empty string to leave credentials out entirely.
export function exportData(categories: string[], credentialPassword: string): Promise<string> {
  return App.ExportData(categories, credentialPassword)
}

// inspectBackupFile opens a file picker and returns what the chosen backup
// holds. An empty path means the dialog was cancelled.
export function inspectBackupFile(): Promise<BackupInfo> {
  return App.InspectBackupFile()
}

// importData applies the selected categories from the backup file at path.
// credentialPassword, when non-empty, also decrypts and restores any mailbox
// credentials the file carries; a wrong password rejects with an error.
export function importData(path: string, categories: string[], credentialPassword: string): Promise<void> {
  return App.ImportData(path, categories, credentialPassword)
}

// --- import from another mail client ---

// findThunderbirdProfiles looks for Thunderbird profiles where it installs them
// on this platform. An empty list means none were found there.
export function findThunderbirdProfiles(): Promise<ThunderbirdProfile[]> {
  return App.FindThunderbirdProfiles()
}

// chooseThunderbirdProfile opens a folder picker for a profile kept somewhere
// discovery does not look. An empty list means the dialog was cancelled.
export function chooseThunderbirdProfile(): Promise<ThunderbirdProfile[]> {
  return App.ChooseThunderbirdProfile()
}

// importThunderbirdAccounts recreates the given addresses as mailboxes, reading
// their server settings from the profile. Passwords cannot come across, so each
// one needs its password entered once. Returns how many were created.
export function importThunderbirdAccounts(profilePath: string, emails: string[]): Promise<number> {
  return App.ImportThunderbirdAccounts(profilePath, emails)
}

// chooseMailFiles opens a picker for .eml and .mbox files. An empty list means
// the dialog was cancelled.
export function chooseMailFiles(): Promise<string[]> {
  return App.ChooseMailFiles()
}

// importMailFiles imports the given files into Local Folders. It returns as soon
// as the job starts; watch onImportProgress for progress and the final count.
export function importMailFiles(paths: string[]): Promise<void> {
  return App.ImportMailFiles(paths)
}

// importThunderbirdFolders imports mail folders found in a Thunderbird profile,
// keeping their names. Like importMailFiles it runs in the background.
export function importThunderbirdFolders(paths: string[]): Promise<void> {
  return App.ImportThunderbirdFolders(paths)
}

// systemColorScheme returns the OS dark/light preference ("dark" | "light"), or
// "" when it cannot be determined. Only meaningful on Linux, where WebKitGTK
// does not expose it to CSS prefers-color-scheme; elsewhere it returns "".
export function systemColorScheme(): Promise<string> {
  return App.SystemColorScheme()
}

// --- address book ---

export function searchAddresses(query: string, limit: number): Promise<AddressBookEntry[]> {
  if (isDemoActive()) {
    return Promise.resolve([])
  }
  return App.SearchAddresses(query, limit)
}

export function listAddresses(): Promise<AddressBookEntry[]> {
  if (isDemoActive()) {
    return Promise.resolve([])
  }
  return App.ListAddresses()
}

export function deleteAddress(email: string): Promise<void> {
  return App.DeleteAddress(email)
}

// --- window ---

// setWindowTitle updates the native window title to reflect context.
export function setWindowTitle(title: string): void {
  void App.SetWindowTitle(title)
}

// closeWindow does what the window's close button does, following the close
// action setting: hide and keep syncing, or quit.
export function closeWindow(): void {
  void App.CloseWindow()
}

// closeCurrentWindow closes a message window it is called from; from the main
// window it does what closeWindow does.
export function closeCurrentWindow(): void {
  void App.CloseCurrentWindow()
}

// openMessageWindow shows a message in a window of its own, or brings forward
// the one it is already open in. Nothing to open in demo mode, where the ids
// belong to samples.
export function openMessageWindow(id: number, title: string): Promise<void> {
  if (isDemoActive()) {
    return Promise.resolve()
  }
  return App.OpenMessageWindow(id, title)
}

// openComposeWindow moves a compose pane's state into a window of its own,
// which picks it up with takeComposeSession.
export function openComposeWindow(session: unknown): Promise<void> {
  return App.OpenComposeWindow(JSON.stringify(session))
}

// showMessageInMainWindow brings the main window forward with a message open,
// for a compose window to show the message it replies to.
export function showMessageInMainWindow(id: number): Promise<void> {
  return App.ShowMessageInMainWindow(id)
}

// takeComposeSession returns the compose pane a compose window was opened
// for, once, or null when there is none (the window was reloaded).
export async function takeComposeSession<T>(token: string): Promise<T | null> {
  const raw = await App.TakeComposeSession(token)
  return raw ? (JSON.parse(raw) as T) : null
}

// titleBarDoubleClick runs the system's title-bar double-click action (zoom,
// minimise or nothing). Needed because the app draws its own title bar on
// macOS, so the window server never sees the click.
export function titleBarDoubleClick(): void {
  void App.TitleBarDoubleClick()
}

// trafficLights is where the macOS window buttons sit, in points: `center` is
// their centre line below the window's top edge, `end` where the last one ends
// from the leading edge. Both are 0 when unknown or not on macOS. Without a
// token it measures the main window, with one that compose window.
export function trafficLights(composeToken = ''): Promise<{ center: number; end: number }> {
  return App.TrafficLights(composeToken)
}

// applyTrafficLights lines the page's title bar tokens up with where the
// window buttons really are. `fill` also makes the title bar strip as tall as
// twice their centre line, for a bar the buttons sit in the middle of.
export async function applyTrafficLights(composeToken = '', fill = false): Promise<void> {
  const at = await trafficLights(composeToken).catch(() => ({ center: 0, end: 0 }))
  if (at.center <= 0) {
    return
  }
  const root = document.documentElement.style
  const scaled = (points: number): string => `calc(${points}px / var(--ui-scale, 1))`
  root.setProperty('--titlebar-lights-center', scaled(at.center))
  if (fill) {
    root.setProperty('--titlebar-lights', scaled(at.center * 2))
    root.setProperty('--titlebar-inset', scaled(at.end + 14))
  }
}

// quitApp exits the app, as the Quit menu item does.
export function quitApp(): void {
  void App.QuitApp()
}

// hideApp hides the app the way the close button does when it keeps Pelton
// running: on macOS the Dock icon brings it back.
export function hideApp(): void {
  void App.HideApp()
}

// --- app lock ---

/** Whether the lock is on and the app locked, and the look the lock screen takes. */
export function getLockState(): Promise<LockState> {
  return App.GetLockState() as unknown as Promise<LockState>
}

/** Locks the app, when the lock is on. */
export function lockNow(): Promise<void> {
  return App.LockNow()
}

/** Tells the idle check the app is in use. */
export function noteActivity(): Promise<void> {
  return App.NoteActivity()
}

/** Shows the system prompt with reason and unlocks when it passes. */
export function unlockWithSystem(reason: string): Promise<LockAuthResult> {
  return App.UnlockWithSystem(reason) as unknown as Promise<LockAuthResult>
}

/** Unlocks with the lock passphrase. */
export function unlockWithPassphrase(passphrase: string): Promise<LockAuthResult> {
  return App.UnlockWithPassphrase(passphrase) as unknown as Promise<LockAuthResult>
}

/**
 * Turns the lock on: with a passphrase when one is given, otherwise with the
 * system prompt, which has to pass once first.
 */
export function enableAppLock(passphrase: string, reason: string): Promise<LockAuthResult> {
  return App.EnableAppLock(passphrase, reason) as unknown as Promise<LockAuthResult>
}

/** Turns the lock off once the system prompt or the passphrase has passed. */
export function disableAppLock(passphrase: string, reason: string): Promise<LockAuthResult> {
  return App.DisableAppLock(passphrase, reason) as unknown as Promise<LockAuthResult>
}

/** Replaces the lock passphrase once the current one has been checked. */
export function changeLockPassphrase(current: string, next: string): Promise<LockAuthResult> {
  return App.ChangeLockPassphrase(current, next) as unknown as Promise<LockAuthResult>
}

// --- quick compose ---

/** The quick compose setting and whether its shortcut is registered. */
export function getQuickComposeStatus(): Promise<QuickComposeStatus> {
  return App.GetQuickComposeStatus() as unknown as Promise<QuickComposeStatus>
}

/**
 * Turns quick compose on or off with a shortcut. The shortcut is registered
 * before anything is stored; a refused one leaves the old setting in place and
 * the status says why.
 */
export function setQuickCompose(enabled: boolean, shortcut: string): Promise<QuickComposeStatus> {
  return App.SetQuickCompose(enabled, shortcut) as unknown as Promise<QuickComposeStatus>
}

// isFullscreen reports whether the window is fullscreen.
export function isFullscreen(): Promise<boolean> {
  return App.IsFullscreen()
}

/**
 * Reports whether the main window draws its own title bar, in which case the
 * interface supplies the window buttons and the drag region.
 */
export function windowFrameIntegrated(): Promise<boolean> {
  return App.WindowFrameIntegrated()
}

/** Minimises the main window. */
export function minimiseWindow(): void {
  void App.MinimiseWindow()
}

/** Maximises the main window, or restores it. */
export function toggleMaximiseWindow(): Promise<void> {
  return App.ToggleMaximiseWindow()
}

/** Reports whether the main window is maximised. */
export function isWindowMaximised(): Promise<boolean> {
  return App.IsWindowMaximised()
}

// toggleFullscreen enters or leaves fullscreen.
export function toggleFullscreen(): Promise<void> {
  return App.ToggleFullscreen()
}

// setUnreadBadge puts the unread count on the dock icon on macOS, and the unread
// dot on the tray icon on Windows and Linux.
export function setUnreadBadge(unread: number): void {
  void App.SetUnreadBadge(unread)
}

// setWindowTheme matches the native window chrome (the Windows caption bar) to
// the resolved ui theme. No-op on macOS/Linux.
export function setWindowTheme(dark: boolean): void {
  void App.SetWindowTheme(dark)
}

// setMailActionsEnabled greys out or restores the native Mail menu's message
// actions; the app calls it as the open message changes so those items are only
// selectable while a message is open.
export function setMailActionsEnabled(enabled: boolean): void {
  void App.SetMailActionsEnabled(enabled)
}

// getUIPrefs returns all ui preferences with defaults applied server-side. The
// backend types viewsPlacement as a plain string; it only ever emits the
// ViewsPlacement values, so the cast is safe.
export function getUIPrefs(): Promise<UIPrefs> {
  return App.GetUIPrefs() as unknown as Promise<UIPrefs>
}

// setSetting writes a single preference by key.
export function setSetting(key: string, value: string): Promise<void> {
  return App.SetSetting(key, value)
}

// getSetting reads a single raw setting. found is false when the key was never
// written, so callers can apply their own default.
export function getSetting(key: string): Promise<{ value: string; found: boolean }> {
  return App.GetSetting(key)
}

// setting keys shared with the backend (bind_settings.go). centralized so the
// stores never sprinkle raw strings.
export const SettingKeys = {
  theme: 'theme',
  accent: 'accent',
  density: 'density',
  showMailboxBadge: 'show_mailbox_badge',
  showDateTime: 'show_datetime',
  showPgp: 'show_pgp',
  showAuth: 'show_auth',
  // lets search see inside encrypted mail, at the cost of writing the decrypted
  // text into the on-disk index. off by default; toggling rebuilds the index.
  indexDecrypted: 'search_index_decrypted',
  toastPosition: 'toast_position',
  paneLocked: 'pane_locked',
  sidebarWidth: 'sidebar_width',
  listWidth: 'list_width',
  sendDelay: 'send_delay_seconds',
  flagHighlight: 'flag_highlight',
  shortcutHints: 'show_shortcut_hints',
  coachHints: 'coach_hints',
  coachHold: 'coach_hold',
  // the shortcut coach's counters: hints shown and keys used, per action.
  coachCounters: 'shortcut_coach',
  showTips: 'show_tips',
  // the one-time tips already shown, as a json list of tip ids.
  tipsSeen: 'tips_seen',
  // the what's new entries last looked at, as a release line and content hash.
  lastSeenVersion: 'last_seen_version',
  harvestAddresses: 'harvest_addresses',
  accountEmail: 'show_account_email',
  onboarded: 'onboarding_complete',
  // where a setup run stopped, so the next launch resumes at that step.
  onboardingProgress: 'onboarding_progress',
  alwaysLoadImages: 'remote_images_always',
  avatarSource: 'avatar_source',
  avatarStyle: 'avatar_style',
  multiSelectEnabled: 'multi_select_enabled',
  selectAllScope: 'select_all_scope',
  selectAllUnified: 'select_all_unified',
  searchSortText: 'search_sort_text',
  searchSortDated: 'search_sort_dated',
  searchSortFiltered: 'search_sort_filtered',
  showSelectedCount: 'show_selected_count',
  sidebarIndentGuides: 'sidebar_indent_guides',
  rowTemplate: 'row_template',
  rowShowAvatar: 'row_show_avatar',
  rowShowSnippet: 'row_show_snippet',
  previewLines: 'preview_lines',
  uiScale: 'ui_scale',
  messageFontSize: 'message_font_size',
  showFlaggedCount: 'show_flagged_count',
  viewsPlacement: 'views_placement',
  flagColorSync: 'flag_color_sync',
  showOfflineIndicator: 'show_offline_indicator',
  showUnsyncedFolder: 'show_unsynced_folder',
  restoreTabs: 'restore_reading_tabs',
  paletteProfiles: 'palette_profiles',
  openTabs: 'open_reading_tabs',
  swipeEnabled: 'swipe_enabled',
  swipeLeftAction: 'swipe_left_action',
  swipeRightAction: 'swipe_right_action',
  swipeLeftShort: 'swipe_left_short',
  swipeLeftLong: 'swipe_left_long',
  swipeRightShort: 'swipe_right_short',
  swipeRightLong: 'swipe_right_long',
  rowActions: 'row_actions',
  rowActionList: 'row_action_list',
  htmlColors: 'html_colors',
  composeVimMode: 'compose_vim_mode',
  downloadIncludeAttachments: 'download_include_attachments',
  appVimMode: 'app_vim_mode',
  language: 'language',
  lowPowerMode: 'low_power_mode',
  autoSyncIntervalSeconds: 'auto_sync_interval_seconds',
  defaultEditorMode: 'default_editor_mode',
  composeAutocomplete: 'compose_autocomplete',
  composeChips: 'compose_chips',
  composeMarkdown: 'compose_markdown',
  smoothCaret: 'smooth_caret',
  updateCheckFrequency: 'update_check_frequency',
  emptyStateImage: 'empty_state_image',
  emptyStateFullscreen: 'empty_state_fullscreen',
  cornerStyle: 'corner_style',
  design: 'design',
  layoutMode: 'layout_mode',
  navStyle: 'nav_style',
  railSide: 'rail_side',
  railPins: 'rail_pins',
  spaceOrder: 'space_order',
  hiddenSpaces: 'hidden_spaces',
  paneStyle: 'pane_style',
  avatarPlacement: 'avatar_placement',
  contactsView: 'contacts_view',
  listHeight: 'list_height',
  nativeTitlebar: 'native_titlebar',
  quickFilterBar: 'quick_filter_bar',
  recentSearches: 'recent_searches',
  composeOpenIn: 'compose_open_in',
  threadedList: 'threaded_list',
  afterMove: 'after_move',
  afterReplySend: 'after_reply_send',
  sendRemindAfter: 'send_remind_after',
  warnMissingAttachment: 'warn_missing_attachment',
  warnEmptySubject: 'warn_empty_subject',
  splitInbox: 'split_inbox',
  splitTabs: 'split_inbox_tabs',
  splitActive: 'split_inbox_active',
  readerWidth: 'reader_width',
  senderPanel: 'sender_panel',
  mailDates: 'mail_dates',
  myDayPanel: 'my_day_panel',
  projectsView: 'projects_view',
  projectSuggestions: 'project_suggestions',
  feedsRefreshMinutes: 'feeds_refresh_minutes',
  feedsKeepDays: 'feeds_keep_days',
  feedsKeepCount: 'feeds_keep_count',
  feedsLoadImages: 'feeds_load_images',
  feedsMarkReadOnScroll: 'feeds_mark_read_on_scroll',
  themeId: 'theme_id',
  menuBarInApp: 'menu_bar_in_app',
  menuBarNativeMinimal: 'menu_bar_native_minimal',
  windowMaterial: 'window_material',
  menuBarIcons: 'menu_bar_icons',
  menuBarLayout: 'menu_bar_layout',
  paletteUsage: 'palette_usage',
  paletteQuickSelect: 'palette_quick_select',
  menuBarNewItems: 'menu_bar_new_items',
  timeFormat: 'time_format',
  reduceMotion: 'reduce_motion',
  handCursor: 'hand_cursor',
  unreadBadge: 'dock_badge',
  themeDarkStart: 'theme_dark_start',
  themeDarkEnd: 'theme_dark_end',
  bodyFont: 'body_font',
  senderFonts: 'sender_fonts',
  uiFont: 'ui_font',
  monoFont: 'mono_font',
  notifyNewMail: 'notify_new_mail',
  notifyActionArchive: 'notify_action_archive',
  notifyActionMarkRead: 'notify_action_mark_read',
  notifyActionReply: 'notify_action_reply',
  cleanLinks: 'clean_links',
  autoconfigLookup: 'autoconfig_lookup',
  verboseSync: 'verbose_sync',
  syncProgressBar: 'sync_progress_bar',
  statusClock: 'status_clock',
  statusUnread: 'status_unread',
  chatEnabled: 'chat_enabled',
  chatSendReceipts: 'chat_send_receipts',
  chatSendTyping: 'chat_send_typing',
  chatNotify: 'chat_notify',
  chatMarkdown: 'chat_markdown',
  markReadOnOpen: 'mark_read_on_open',
  openOnArrow: 'open_on_arrow',
  signatureAboveQuote: 'signature_above_quote',
  closeAction: 'close_button_action',
  startHidden: 'start_hidden',
  appLockIdleMinutes: 'app_lock_idle_minutes',
  appLockOnLaunch: 'app_lock_on_launch',
  appLockHideNotifications: 'app_lock_hide_notifications',
  syncMessageLimit: 'sync_message_limit',
  syncAutoBackfill: 'sync_auto_backfill',
  startupSelection: 'startup_selection',
  lastSelection: 'last_selection',
  liabilityAccepted: 'liability_accepted',
  logToFile: 'log_to_file',
  logLevel: 'log_level',
  logMessageMetadata: 'log_message_metadata',
  crashLogs: 'crash_logs',
  blockTrackingPixels: 'block_tracking_pixels',
  notesVault: 'notes_vault',
  notesDailyFolder: 'notes_daily_folder',
  notesDailyFormat: 'notes_daily_format',
  notesDailyTemplate: 'notes_daily_template',
  notesTemplatesFolder: 'notes_templates_folder',
  notesAttachmentsFolder: 'notes_attachments_folder',
  notesProjectsFolder: 'notes_projects_folder',
  notesEditorMode: 'notes_editor_mode',
  notesLineWidth: 'notes_line_width',
  notesStarred: 'notes_starred',
  notesRecent: 'notes_recent',
  notesSort: 'notes_sort',
} as const

// listSystemFonts returns the installed font family names for the body font
// dropdown (cached backend-side after the first scan).
export function listSystemFonts(): Promise<string[]> {
  return App.ListSystemFonts().then((f) => f ?? [])
}

// --- pgp keys (#192) ---

// listPGPKeys returns every imported key, private keys first.
export function listPGPKeys(): Promise<PGPKey[]> {
  return App.ListPGPKeys() as unknown as Promise<PGPKey[]>
}

// importPGPKey opens a file picker and imports the keys the file holds.
// Resolves with an empty list when the dialog was cancelled.
export function importPGPKey(): Promise<PGPKey[]> {
  return App.ImportPGPKey().then((keys) => (keys ?? []) as unknown as PGPKey[])
}

// deletePGPKey removes a key from both rings and forgets its passphrase.
export function deletePGPKey(fingerprint: string): Promise<void> {
  return App.DeletePGPKey(fingerprint)
}

// exportPGPKey writes a key to a file the user picks, returning the path or ''
// if cancelled. includePrivate writes the private half, which is the only
// backup path for it: keys are deliberately excluded from the backup archive.
export function exportPGPKey(fingerprint: string, includePrivate: boolean): Promise<string> {
  return App.ExportPGPKey(fingerprint, includePrivate)
}

// unlockPGPKey verifies a passphrase and holds it for the session. remember
// also stores it in the os keyring so it survives a restart.
export function unlockPGPKey(
  fingerprint: string,
  passphrase: string,
  remember: boolean,
): Promise<void> {
  return App.UnlockPGPKey(fingerprint, passphrase, remember)
}

// unlockMessage tries a passphrase against one protected message and, if it
// opens, holds the passphrase for the rest of the session. It does not offer to
// store it in the keyring: that needs a specific key to file it under, which
// the Encryption pane has and a message does not.
export function unlockMessage(messageId: number, passphrase: string): Promise<void> {
  return App.UnlockMessage(messageId, passphrase)
}

// forgetPGPPassphrase drops a passphrase from the session and the keyring.
export function forgetPGPPassphrase(fingerprint: string): Promise<void> {
  return App.ForgetPGPPassphrase(fingerprint)
}

// getAccountPGPKey returns the fingerprint an account signs with, or ''.
export function getAccountPGPKey(accountId: number): Promise<string> {
  return App.GetAccountPGPKey(accountId)
}

// setAccountPGPKey pins the signing key for an account; '' clears it and falls
// back to matching the account address against the keys' user ids.
export function setAccountPGPKey(accountId: number, fingerprint: string): Promise<void> {
  return App.SetAccountPGPKey(accountId, fingerprint)
}

// --- custom themes ---

// listThemes returns every installed custom theme for the settings gallery.
export function listThemes(): Promise<ThemeInfo[]> {
  return App.ListThemes() as Promise<ThemeInfo[]>
}

// getThemeApply loads an installed theme in apply form (base, validated token
// overrides, concatenated css with bundled assets inlined, icon svgs).
export function getThemeApply(id: string): Promise<ThemeApply> {
  return App.GetThemeApply(id) as Promise<ThemeApply>
}

// previewThemeImport opens a file picker for a .peltontheme container and
// returns the read-before-import view. canceled is true when dismissed;
// nothing is installed yet.
export function previewThemeImport(): Promise<ThemeImportPreview> {
  return App.PreviewThemeImport() as Promise<ThemeImportPreview>
}

// confirmThemeImport installs a previewed container. allowRemote keeps the
// css's network references; false strips them before anything hits disk.
// importTokens and importCSS are the parts choice: a deselected part is
// dropped from the container before it is written.
export function confirmThemeImport(
  path: string,
  allowRemote: boolean,
  importTokens: boolean,
  importCSS: boolean,
): Promise<ThemeInfo> {
  return App.ConfirmThemeImport(path, allowRemote, importTokens, importCSS) as Promise<ThemeInfo>
}

// deleteTheme removes an installed theme (and resets the selection if it was
// active).
export function deleteTheme(id: string): Promise<void> {
  return App.DeleteTheme(id)
}

// exportTheme zips an installed theme back into a shareable .peltontheme via
// a save dialog, returning the chosen path ('' if cancelled).
export function exportTheme(id: string): Promise<string> {
  return App.ExportTheme(id)
}

// saveCustomTheme validates and writes a palette-editor theme as a
// .peltontheme file in the themes folder, returning its gallery info.
export function saveCustomTheme(req: SaveThemeRequest): Promise<ThemeInfo> {
  return App.SaveCustomTheme(new desktop.SaveThemeRequest(req)) as Promise<ThemeInfo>
}

// getThemeDraft loads an installed theme back into editor form: metadata,
// tokens and the raw source of the editor's own stylesheet.
export function getThemeDraft(id: string): Promise<ThemeDraft> {
  return App.GetThemeDraft(id) as Promise<ThemeDraft>
}

// openThemesFolder shows the themes folder in the system file manager.
export function openThemesFolder(): Promise<void> {
  return App.OpenThemesFolder()
}

// --- custom languages ---

// listUserLocales returns every valid language file in the locales folder.
export function listUserLocales(): Promise<UserLocale[]> {
  return App.ListUserLocales() as Promise<UserLocale[]>
}

// getUserLocale loads one custom language in apply form (base + strings).
export function getUserLocale(id: string): Promise<UserLocaleApply> {
  return App.GetUserLocale(id) as Promise<UserLocaleApply>
}

// openLocalesFolder shows the locales folder in the system file manager.
export function openLocalesFolder(): Promise<void> {
  return App.OpenLocalesFolder()
}

// saveLocaleTemplate writes a translation template file via a save dialog,
// returning the chosen path ('' if cancelled).
export function saveLocaleTemplate(content: string): Promise<string> {
  return App.SaveLocaleTemplate(content)
}

// getProxyConfig returns the current outbound proxy preference (without the
// stored password; hasPassword reports whether one is set).
export function getProxyConfig(): Promise<ProxyConfig> {
  return App.GetProxyConfig() as Promise<ProxyConfig>
}

// setProxyConfig validates, persists and applies a proxy preference. Leave
// password empty with hasPassword true to keep the stored one.
export function setProxyConfig(cfg: ProxyConfig): Promise<void> {
  return App.SetProxyConfig(new desktop.ProxyConfigDTO(cfg))
}

// testProxy dials a well-known endpoint through the given settings so the ui can
// confirm the proxy works before saving. Resolves on success.
export function testProxy(cfg: ProxyConfig): Promise<void> {
  return App.TestProxy(new desktop.ProxyConfigDTO(cfg))
}

// getMCPConfig returns the read-only MCP server's state: enabled, loopback url,
// bearer token and whether it is listening.
export function getMCPConfig(): Promise<MCPConfig> {
  return App.GetMCPConfig() as Promise<MCPConfig>
}

// setMCPEnabled turns the MCP server on or off, generating a token on first
// enable so the endpoint is never unauthenticated.
export function setMCPEnabled(enabled: boolean): Promise<void> {
  return App.SetMCPEnabled(enabled)
}

// setMCPPort changes the loopback port (1024-65535) and restarts the server if
// it is running.
export function setMCPPort(port: number): Promise<void> {
  return App.SetMCPPort(port)
}

// regenerateMCPToken issues a fresh bearer token, invalidating the old one, and
// returns it for display.
export function regenerateMCPToken(): Promise<string> {
  return App.RegenerateMCPToken()
}

// getVirusTotalConfig returns the VirusTotal integration's settings: whether it
// is on, whether a key is stored, and the two auto-scan toggles.
export function getVirusTotalConfig(): Promise<VirusTotalConfig> {
  return App.GetVirusTotalConfig() as Promise<VirusTotalConfig>
}

// setVirusTotalEnabled turns the integration on or off. Turning it off also
// discards every cached verdict and resets both auto-scan toggles.
export function setVirusTotalEnabled(enabled: boolean): Promise<void> {
  return App.SetVirusTotalEnabled(enabled)
}

// setVirusTotalApiKey stores the api key in the os keyring, or clears it (and
// the cached verdicts it produced) when given an empty string.
export function setVirusTotalApiKey(apiKey: string): Promise<void> {
  return App.SetVirusTotalAPIKey(apiKey)
}

// setVirusTotalAutoScanLinks turns automatic link scanning on or off.
export function setVirusTotalAutoScanLinks(enabled: boolean): Promise<void> {
  return App.SetVirusTotalAutoScanLinks(enabled)
}

// setVirusTotalAutoScanAttachments turns automatic attachment scanning on or off.
export function setVirusTotalAutoScanAttachments(enabled: boolean): Promise<void> {
  return App.SetVirusTotalAutoScanAttachments(enabled)
}

// scanUrl looks one link up on VirusTotal, answering from the local cache when
// it can. This is the on-demand path behind the link context menu.
export function scanUrl(url: string): Promise<Verdict> {
  return App.ScanURL(url) as Promise<Verdict>
}

// scanAttachment looks one attachment up by the sha-256 of its bytes. The file
// itself is never uploaded, so an unrecognised file stays 'unknown'.
export function scanAttachment(messageId: number, attachmentId: number): Promise<Verdict> {
  return App.ScanAttachment(messageId, attachmentId) as Promise<Verdict>
}

// scanMessage scans a whole message's links, attachments, or both. Results come
// back per target, so one failed lookup does not lose the others.
export function scanMessage(messageId: number, links: boolean, attachments: boolean): Promise<MessageScan> {
  return App.ScanMessage(messageId, links, attachments) as Promise<MessageScan>
}

// --- logs and crash reports (#211) ---

// getLogStatus reports where the log folder is, whether anything is being
// written to it, and whether the last run left a crash report behind.
export function getLogStatus(): Promise<LogStatus> {
  return App.GetLogStatus() as Promise<LogStatus>
}

// openLogFolder shows the log folder in the system file manager, creating it
// first so the button works before anything has been logged.
export function openLogFolder(): Promise<void> {
  return App.OpenLogFolder()
}

// deleteLogs removes every log and crash file.
export function deleteLogs(): Promise<void> {
  return App.DeleteLogs()
}

// openCrashReport opens the pending crash file and marks it seen.
export function openCrashReport(): Promise<void> {
  return App.OpenCrashReport()
}

// dismissCrashReport marks the pending crash seen without opening it. The file
// stays on disk.
export function dismissCrashReport(): Promise<void> {
  return App.DismissCrashReport()
}

// getDiagnostics returns the build and platform summary the about section
// copies to the clipboard. It never contains mail or addresses.
export function getDiagnostics(): Promise<string> {
  return App.GetDiagnostics()
}

// devToolsEnabled reports whether the developer overlays are available (#188).
// They are, only when the app was started with PELTON_DEV or PELTON_DEVTOOLS;
// no setting can turn them on.
export function devToolsEnabled(): Promise<boolean> {
  if (isDemoActive()) {
    return Promise.resolve(false)
  }
  return App.DevToolsEnabled()
}

// devActivity returns the buffered log lines newer than seq, along with the
// sequence to ask for next. Pass 0 for everything still buffered.
export function devActivity(after: number): Promise<DevActivity> {
  return App.DevActivity(after).then((page) => ({
    lines: page.lines ?? [],
    next: page.next,
    level: page.level,
  }))
}

// clearDevActivity empties the activity buffer.
export function clearDevActivity(): Promise<void> {
  return App.ClearDevActivity()
}

// devProcessStats samples the Go runtime and the app's on-disk footprint.
export function devProcessStats(): Promise<DevProcessStats> {
  return App.DevProcessStats()
}

// listProfiles returns every profile on the install, main first (#270).
export function listProfiles(): Promise<Profile[]> {
  if (isDemoActive()) {
    return Promise.resolve([])
  }
  return App.ListProfiles().then((list) => (list ?? []) as unknown as Profile[])
}

// activeProfile returns the profile the app is in.
export function activeProfile(): Promise<Profile> {
  return App.ActiveProfile() as unknown as Promise<Profile>
}

// createProfile adds a profile. Areas set to 'copy' are duplicated from main
// now; areas set to 'share' stay linked to it.
export function createProfile(draft: ProfileDraft): Promise<Profile> {
  return App.CreateProfile(new desktop.ProfileRequest(draft)) as unknown as Promise<Profile>
}

// updateProfile saves a profile's name, icon, sharing and visible accounts.
export function updateProfile(draft: ProfileDraft): Promise<Profile> {
  return App.UpdateProfile(new desktop.ProfileRequest(draft)) as unknown as Promise<Profile>
}

// deleteProfile removes a profile and what it owned. Its accounts and their
// mail belong to the install and stay.
export function deleteProfile(id: number): Promise<void> {
  return App.DeleteProfile(id)
}

// switchProfile moves the app to another profile, in place.
export function switchProfile(id: number): Promise<void> {
  return App.SwitchProfile(id)
}

// allAccounts lists every account on the install, including ones the current
// profile does not show. Only the profile editor wants this.
export function allAccounts(): Promise<Account[]> {
  if (isDemoActive()) {
    return Promise.resolve(demoAccounts())
  }
  return App.AllAccounts().then((list) => (list ?? []) as unknown as Account[])
}

// listAddressBooks returns every configured CardDAV address book (#168).
export function listAddressBooks(): Promise<AddressBook[]> {
  if (isDemoActive()) {
    return Promise.resolve([])
  }
  return App.ListAddressBooks().then((list) => (list ?? []) as unknown as AddressBook[])
}

// discoverAddressBooks asks a server which address books a login can see. With
// an accountId and no url it starts from that mailbox's domain.
export function discoverAddressBooks(draft: AddressBookDraft): Promise<DiscoveredBook[]> {
  return App.DiscoverAddressBooks(new desktop.AddressBookRequest(draft)).then(
    (list) => (list ?? []) as unknown as DiscoveredBook[],
  )
}

// addAddressBook stores an address book and syncs it once.
export function addAddressBook(draft: AddressBookDraft): Promise<AddressBook> {
  return App.AddAddressBook(new desktop.AddressBookRequest(draft)) as unknown as Promise<AddressBook>
}

// updateAddressBook saves a book's name, location and login. An empty password
// leaves the stored one alone.
export function updateAddressBook(draft: AddressBookDraft): Promise<AddressBook> {
  return App.UpdateAddressBook(new desktop.AddressBookRequest(draft)) as unknown as Promise<AddressBook>
}

// removeAddressBook forgets a book here. Nothing is deleted on the server.
export function removeAddressBook(id: number): Promise<void> {
  return App.RemoveAddressBook(id)
}

// listContacts returns the contacts in a book, or in all of them for bookId 0.
export function listContacts(bookId: number): Promise<Contact[]> {
  if (isDemoActive()) {
    return Promise.resolve([])
  }
  return App.ListContacts(bookId).then((list) => (list ?? []) as unknown as Contact[])
}

// syncContacts refreshes every address book now.
export function syncContacts(): Promise<void> {
  return App.SyncContacts()
}

// saveContact writes a contact to its server. A refused write comes back with
// conflict true and both versions, having changed nothing.
export function saveContact(draft: ContactDraft): Promise<ContactConflict> {
  return App.SaveContact(new desktop.ContactRequest(draft)) as unknown as Promise<ContactConflict>
}

// deleteContact removes a contact here and on the server. force writes through
// a conflict the user has already been shown.
export function deleteContact(id: number, force: boolean): Promise<ContactConflict> {
  return App.DeleteContact(id, force) as unknown as Promise<ContactConflict>
}

/** prepareMeeting gathers what helps with a meeting from local data. */
export function prepareMeeting(eventId: number, recurrenceId: string): Promise<MeetingPrep> {
  return App.PrepareMeeting(eventId, recurrenceId) as unknown as Promise<MeetingPrep>
}

/** findDuplicateContacts lists the groups of contacts that look like one person. */
export function findDuplicateContacts(): Promise<Duplicates> {
  if (isDemoActive()) {
    return Promise.resolve({ groups: [], dismissed: 0 })
  }
  return App.FindDuplicateContacts() as unknown as Promise<Duplicates>
}

/** mergeContacts folds contacts into one, here and on their servers. */
export function mergeContacts(req: MergeRequest): Promise<MergeResult> {
  return App.MergeContacts(new desktop.MergeContactsRequest(req)) as unknown as Promise<MergeResult>
}

/** undoContactMerge puts the contacts of a merge back and returns them. */
export function undoContactMerge(token: string): Promise<Contact[]> {
  return App.UndoContactMerge(token).then((list) => (list ?? []) as unknown as Contact[])
}

/** dismissDuplicateContacts marks the contacts as not duplicates of each other. */
export function dismissDuplicateContacts(ids: number[]): Promise<void> {
  return App.DismissDuplicateContacts(ids)
}

/** resetDismissedDuplicates forgets the not-duplicates answers and returns how many there were. */
export function resetDismissedDuplicates(): Promise<number> {
  return App.ResetDismissedDuplicates()
}

/** listCalendarAccounts returns every CalDAV login. */
export function listCalendarAccounts(): Promise<CalendarAccount[]> {
  if (isDemoActive()) {
    return Promise.resolve([])
  }
  return App.ListCalendarAccounts().then((list) => (list ?? []) as unknown as CalendarAccount[])
}

/**
 * discoverCalendars asks a server which calendars a login can see. Without a
 * url it starts from the address book or the mailbox the draft names.
 */
export function discoverCalendars(draft: CalendarAccountDraft): Promise<CalendarDiscovery> {
  return App.DiscoverCalendars(new desktop.CalendarAccountRequest(draft)).then((d) => {
    const out = d as unknown as CalendarDiscovery
    return { ...out, calendars: out.calendars ?? [] }
  })
}

/** addCalendarAccount stores a CalDAV login; its first sync runs in the background. */
export function addCalendarAccount(draft: CalendarAccountDraft): Promise<CalendarAccount> {
  return App.AddCalendarAccount(new desktop.CalendarAccountRequest(draft)) as unknown as Promise<CalendarAccount>
}

/** updateCalendarAccount saves a login's name, url and username; an empty password keeps the stored one. */
export function updateCalendarAccount(draft: CalendarAccountDraft): Promise<CalendarAccount> {
  return App.UpdateCalendarAccount(new desktop.CalendarAccountRequest(draft)) as unknown as Promise<CalendarAccount>
}

/** removeCalendarAccount forgets a login and its calendars here. Nothing is deleted on the server. */
export function removeCalendarAccount(id: number): Promise<void> {
  return App.RemoveCalendarAccount(id)
}

/** listCalendars returns every calendar, local ones first. */
export function listCalendars(): Promise<Calendar[]> {
  if (isDemoActive()) {
    return Promise.resolve([])
  }
  return App.ListCalendars().then((list) => (list ?? []) as unknown as Calendar[])
}

/** setCalendarColor sets a calendar's #rrggbb color here; empty clears it. */
export function setCalendarColor(id: number, color: string): Promise<void> {
  return App.SetCalendarColor(id, color)
}

/** setCalendarVisible shows or hides a calendar's events and reminders. */
export function setCalendarVisible(id: number, visible: boolean): Promise<void> {
  return App.SetCalendarVisible(id, visible)
}

/** createLocalCalendar adds a calendar that lives only on this device. */
export function createLocalCalendar(name: string, color: string): Promise<Calendar> {
  return App.CreateLocalCalendar(name, color) as unknown as Promise<Calendar>
}

/** renameCalendar renames a local calendar. */
export function renameCalendar(id: number, name: string): Promise<void> {
  return App.RenameCalendar(id, name)
}

/** deleteLocalCalendar removes a local calendar and its events; the last one stays. */
export function deleteLocalCalendar(id: number): Promise<void> {
  return App.DeleteLocalCalendar(id)
}

/** getCalendarSettings returns the calendar preferences. */
export function getCalendarSettings(): Promise<CalendarSettings> {
  return App.GetCalendarSettings() as unknown as Promise<CalendarSettings>
}

/** setCalendarSettings saves the calendar preferences. */
export function setCalendarSettings(settings: CalendarSettings): Promise<void> {
  return App.SetCalendarSettings(new desktop.CalendarSettingsDTO(settings))
}

/**
 * listOccurrences returns the expanded occurrences overlapping [from, to), both
 * rfc3339. calendarIds limits them to those calendars; empty means every
 * visible one.
 */
export function listOccurrences(from: string, to: string, calendarIds: number[] = []): Promise<Occurrence[]> {
  if (isDemoActive()) {
    return Promise.resolve([])
  }
  return App.ListOccurrences(from, to, calendarIds).then((list) => (list ?? []) as unknown as Occurrence[])
}

/** getEvent returns an event, or with a recurrenceId that one occurrence of it. */
export function getEvent(id: number, recurrenceId = ''): Promise<CalendarEvent> {
  return App.GetEvent(id, recurrenceId) as unknown as Promise<CalendarEvent>
}

/** createEvent stores a new event and writes it to its calendar's server. */
export function createEvent(draft: EventDraft): Promise<EventSaveResult> {
  return App.CreateEvent(new desktop.EventRequest(draft)) as unknown as Promise<EventSaveResult>
}

/** updateEvent saves an edit; for an occurrence, draft.scope says what it applies to. */
export function updateEvent(draft: EventDraft): Promise<EventSaveResult> {
  return App.UpdateEvent(new desktop.EventRequest(draft)) as unknown as Promise<EventSaveResult>
}

/**
 * deleteEvent deletes an event, or for an occurrence of a series the scope of
 * it named. force deletes over a newer server copy.
 */
export function deleteEvent(id: number, scope: EventScope | '' = '', recurrenceId = '', force = false): Promise<EventSaveResult> {
  return App.DeleteEvent(id, scope, recurrenceId, force) as unknown as Promise<EventSaveResult>
}

/** resolveEventConflict keeps the local version (keepMine) or takes the server's. */
export function resolveEventConflict(id: number, keepMine: boolean): Promise<CalendarEvent> {
  return App.ResolveEventConflict(id, keepMine) as unknown as Promise<CalendarEvent>
}

/** A call that can be stopped; cancel stops the work on the go side too. */
export type Cancellable<T> = Promise<T> & { cancel(): void }

/**
 * lookupFreeBusy reads the user's busy time from the calendars here and asks
 * the CalDAV server of the event's calendar about the attendees. Attendees the
 * server cannot answer for come back unknown; an unreachable server rejects.
 */
export function lookupFreeBusy(query: FreeBusyQuery): Cancellable<FreeBusy> {
  if (isDemoActive()) {
    const none: FreeBusy = { mine: [], attendees: query.attendees.map((email) => ({ email, known: false, reason: 'local', status: '', busy: [] })) }
    return Object.assign(Promise.resolve(none), { cancel: () => {} })
  }
  const call = App.LookupFreeBusy(new desktop.FreeBusyRequest(query))
  const out = call.then((r) => r as unknown as FreeBusy)
  return Object.assign(out, { cancel: () => call.cancel() })
}

/** syncCalendars syncs every calendar account and resolves when done. */
export function syncCalendars(): Promise<void> {
  if (isDemoActive()) {
    return Promise.resolve()
  }
  return App.SyncCalendars()
}

/** parseInvite reads the invitation a message carries; found is false when there is none. */
export function parseInvite(messageId: number): Promise<Invite> {
  return App.ParseInvite(messageId) as unknown as Promise<Invite>
}

/** answerInvite accepts, tentatively accepts or declines an invitation, optionally replying. */
export function answerInvite(answer: InviteAnswer): Promise<InviteAnswerResult> {
  return App.AnswerInvite(new desktop.InviteAnswerRequest(answer)) as unknown as Promise<InviteAnswerResult>
}

/** applyInvite applies a CANCEL or an attendee's REPLY to the stored event. */
export function applyInvite(messageId: number): Promise<InviteAnswerResult> {
  return App.ApplyInvite(messageId) as unknown as Promise<InviteAnswerResult>
}

/** importICSData imports iCalendar or JSCalendar text into a calendar. */
export function importICSData(calendarId: number, data: string): Promise<ICSImportResult> {
  return App.ImportICSData(calendarId, data) as unknown as Promise<ICSImportResult>
}

/** openICSFile asks for a calendar file and reads it for import; an empty name means cancelled. */
export function openICSFile(): Promise<ICSFile> {
  return App.OpenICSFile() as unknown as Promise<ICSFile>
}

/** readICSFiles reads dropped or opened calendar files for import. */
export function readICSFiles(paths: string[]): Promise<ICSFile[]> {
  return App.ReadICSFiles(paths) as unknown as Promise<ICSFile[]>
}

/** consumePendingICS returns the .ics files the app was asked to open, once. */
export function consumePendingICS(): Promise<string[]> {
  return App.ConsumePendingICS()
}

/** exportCalendar saves a calendar as iCalendar or JSCalendar and returns the path, empty when cancelled. */
export function exportCalendar(calendarId: number, format: 'ics' | 'jscal'): Promise<string> {
  return App.ExportCalendar(calendarId, format)
}

/** decodeEventLink reads the events a pasted event link carries; nothing is fetched. */
export function decodeEventLink(link: string): Promise<EventBundle> {
  return App.DecodeEventLink(link) as unknown as Promise<EventBundle>
}

/** decodeJSCalendar reads pasted JSCalendar text into event drafts. */
export function decodeJSCalendar(text: string): Promise<EventBundle> {
  return App.DecodeJSCalendar(text) as unknown as Promise<EventBundle>
}

/** mailEventSource reads a mail for turning it into an event. */
export function mailEventSource(messageId: number): Promise<MailEventSource> {
  return App.MailEventSource(messageId)
}

/** findMessageByLink finds the stored message a mid: link names. */
export function findMessageByLink(link: string): Promise<MessageRef> {
  return App.FindMessageByLink(link)
}

/** defaultCalendarFileStatus reports whether Pelton opens .ics files. */
export function defaultCalendarFileStatus(): Promise<desktop.DefaultMailStatusDTO> {
  return App.DefaultCalendarFileStatus()
}

/** setDefaultCalendarFileHandler asks the OS to open .ics files with Pelton. */
export function setDefaultCalendarFileHandler(): Promise<void> {
  return App.SetDefaultCalendarFileHandler()
}

/** listTaskLists returns every list that takes tasks, local ones first. */
export function listTaskLists(): Promise<TaskList[]> {
  if (isDemoActive()) {
    return Promise.resolve([])
  }
  return App.ListTaskLists().then((list) => (list ?? []) as unknown as TaskList[])
}

/** createTaskList adds a task list that lives only on this device. */
export function createTaskList(name: string, color: string): Promise<TaskList> {
  return App.CreateTaskList(name, color) as unknown as Promise<TaskList>
}

/** deleteTaskList removes a local task list and its tasks; the last one stays. */
export function deleteTaskList(id: number): Promise<void> {
  return App.DeleteTaskList(id)
}

/** listTasks returns every task, completed ones only as long as the settings keep them. */
export function listTasks(): Promise<Task[]> {
  if (isDemoActive()) {
    return Promise.resolve([])
  }
  return App.ListTasks().then((list) => (list ?? []) as unknown as Task[])
}

/** getTask returns one task. */
export function getTask(id: number): Promise<Task> {
  return App.GetTask(id) as unknown as Promise<Task>
}

/** createTask stores a new task and writes it to its list's server. */
export function createTask(draft: TaskDraft): Promise<TaskSaveResult> {
  return App.CreateTask(new desktop.TaskRequest(draft)) as unknown as Promise<TaskSaveResult>
}

/** updateTask saves an edit; another listId moves the task with its subtasks. */
export function updateTask(draft: TaskDraft): Promise<TaskSaveResult> {
  return App.UpdateTask(new desktop.TaskRequest(draft)) as unknown as Promise<TaskSaveResult>
}

/** setTaskDone completes a task or opens it again; a repeating one moves on instead. */
export function setTaskDone(id: number, done: boolean): Promise<TaskSaveResult> {
  return App.SetTaskDone(id, done) as unknown as Promise<TaskSaveResult>
}

/** setTaskDue moves a task's due date; empty clears it. */
export function setTaskDue(id: number, due: string): Promise<TaskSaveResult> {
  return App.SetTaskDue(id, due) as unknown as Promise<TaskSaveResult>
}

/** reorderTasks gives tasks the manual order they are listed in. */
export function reorderTasks(ids: number[]): Promise<void> {
  return App.ReorderTasks(ids)
}

/** deleteTask deletes a task with its subtasks and returns what went, for undo. */
export function deleteTask(id: number, force = false): Promise<TaskDeleteResult> {
  return App.DeleteTask(id, force) as unknown as Promise<TaskDeleteResult>
}

/** restoreTasks brings deleted tasks back as they were. */
export function restoreTasks(items: RemovedTask[]): Promise<void> {
  return App.RestoreTasks(items.map((i) => new desktop.RemovedTaskDTO(i)))
}

/** resolveTaskConflict keeps the local version (keepMine) or takes the server's. */
export function resolveTaskConflict(id: number, keepMine: boolean): Promise<Task> {
  return App.ResolveTaskConflict(id, keepMine) as unknown as Promise<Task>
}

/** listTaskMailMarks returns the stored messages that tasks link to. */
export function listTaskMailMarks(): Promise<TaskMailMark[]> {
  if (isDemoActive()) {
    return Promise.resolve([])
  }
  return App.ListTaskMailMarks().then((list) => (list ?? []) as unknown as TaskMailMark[])
}

/** findEventByUID finds the event a task links to; found is false when it is gone. */
export function findEventByUID(uid: string): Promise<{ found: boolean; eventId: number; start: string; allDay: boolean; summary: string }> {
  return App.FindEventByUID(uid)
}

/** getTaskSettings returns the task preferences. */
export function getTaskSettings(): Promise<TaskSettings> {
  return App.GetTaskSettings() as unknown as Promise<TaskSettings>
}

/** setTaskSettings saves the task preferences. */
export function setTaskSettings(settings: TaskSettings): Promise<void> {
  return App.SetTaskSettings(new desktop.TaskSettingsDTO(settings))
}

/** listProjects returns every project in its manual order, with its tile counts. */
export function listProjects(): Promise<Project[]> {
  if (isDemoActive()) {
    return Promise.resolve([])
  }
  return App.ListProjects().then((list) => (list ?? []) as unknown as Project[])
}

/** getProject returns one project with its counts. */
export function getProject(id: number): Promise<Project> {
  return App.GetProject(id) as unknown as Promise<Project>
}

/** createProject makes a project with the columns the draft names. */
export function createProject(draft: ProjectDraft): Promise<Project> {
  return App.CreateProject(new desktop.ProjectRequest(draft)) as unknown as Promise<Project>
}

/** updateProject saves a project's name, look, description, dates and task list. */
export function updateProject(draft: ProjectDraft): Promise<Project> {
  return App.UpdateProject(new desktop.ProjectRequest(draft)) as unknown as Promise<Project>
}

/** setProjectStatus archives a project or makes it active again. */
export function setProjectStatus(id: number, status: 'active' | 'archived'): Promise<void> {
  return App.SetProjectStatus(id, status)
}

/** deleteProject removes a project with its board, links and notes; tasks and mail stay. */
export function deleteProject(id: number): Promise<void> {
  return App.DeleteProject(id)
}

/** reorderProjects gives the projects the order of ids. */
export function reorderProjects(ids: number[]): Promise<void> {
  return App.ReorderProjects(ids)
}

/** getProjectBoard returns a project's columns and cards. */
export function getProjectBoard(id: number): Promise<ProjectBoard> {
  return App.GetProjectBoard(id).then((b) => {
    const board = b as unknown as ProjectBoard
    return { ...board, columns: board.columns ?? [], cards: board.cards ?? [] }
  })
}

/** addProjectColumn adds a column at the end of a board. */
export function addProjectColumn(projectId: number, name: string): Promise<ProjectColumn> {
  return App.AddProjectColumn(projectId, name) as unknown as Promise<ProjectColumn>
}

/** updateProjectColumn renames a column and sets its WIP limit, 0 for none. */
export function updateProjectColumn(id: number, name: string, wipLimit: number): Promise<void> {
  return App.UpdateProjectColumn(id, name, wipLimit)
}

/** setProjectDoneColumn makes a column the done column, or none for 0. */
export function setProjectDoneColumn(projectId: number, columnId: number): Promise<void> {
  return App.SetProjectDoneColumn(projectId, columnId)
}

/** reorderProjectColumns gives a board's columns the order of ids. */
export function reorderProjectColumns(projectId: number, ids: number[]): Promise<void> {
  return App.ReorderProjectColumns(projectId, ids)
}

/** deleteProjectColumn removes a column; its cards go to moveTo, or off the board for 0. */
export function deleteProjectColumn(id: number, moveTo: number): Promise<void> {
  return App.DeleteProjectColumn(id, moveTo)
}

/** createProjectCard makes a task and puts it on a board. */
export function createProjectCard(draft: ProjectCardDraft): Promise<ProjectCard> {
  return App.CreateProjectCard(new desktop.ProjectCardRequest(draft)) as unknown as Promise<ProjectCard>
}

/** addTaskToProject puts an existing task on a board; columnId 0 picks one. */
export function addTaskToProject(projectId: number, taskUid: string, columnId = 0): Promise<ProjectCard> {
  return App.AddTaskToProject(projectId, taskUid, columnId) as unknown as Promise<ProjectCard>
}

/** moveProjectCard moves a card to index in a column; the done column completes its task. */
export function moveProjectCard(taskUid: string, columnId: number, index: number): Promise<ProjectMoveResult> {
  return App.MoveProjectCard(taskUid, columnId, index) as unknown as Promise<ProjectMoveResult>
}

/** removeProjectCard takes a card off its board; the task stays. */
export function removeProjectCard(taskUid: string): Promise<void> {
  return App.RemoveProjectCard(taskUid)
}

/** projectOfTask returns the project a task is a card of, 0 for none. */
export function projectOfTask(taskUid: string): Promise<number> {
  return App.ProjectOfTask(taskUid)
}

/** addMailToProject files the conversations of messages under a project or one of its cards. */
export function addMailToProject(projectId: number, messageIds: number[], taskUid = ''): Promise<number> {
  return App.AddMailToProject(projectId, messageIds, taskUid)
}

/** removeProjectThread takes a conversation out of a project; the mail stays. */
export function removeProjectThread(projectId: number, threadId: number): Promise<void> {
  return App.RemoveProjectThread(projectId, threadId)
}

/** listProjectThreads returns a project's conversations as list rows, newest first. */
export function listProjectThreads(projectId: number): Promise<ThreadSummary[]> {
  return App.ListProjectThreads(projectId).then((list) => (list ?? []) as unknown as ThreadSummary[])
}

/** listProjectMailMarks says which conversations belong to which projects. */
export function listProjectMailMarks(): Promise<ProjectMailMark[]> {
  if (isDemoActive()) {
    return Promise.resolve([])
  }
  return App.ListProjectMailMarks().then((list) => (list ?? []) as unknown as ProjectMailMark[])
}

/** dismissProjectSuggestion stops a conversation being suggested for a project again. */
export function dismissProjectSuggestion(threadId: number): Promise<void> {
  return App.DismissProjectSuggestion(threadId)
}

/** listProjectDismissals returns the conversations whose suggestion was dismissed. */
export function listProjectDismissals(): Promise<number[]> {
  if (isDemoActive()) {
    return Promise.resolve([])
  }
  return App.ListProjectDismissals().then((list) => list ?? [])
}

/** setEventProject files an event under a project by UID; 0 takes it out of all. */
export function setEventProject(uid: string, summary: string, projectId: number): Promise<void> {
  return App.SetEventProject(uid, summary, projectId)
}

/** linkEventToCard attaches an event to a card by UID. */
export function linkEventToCard(taskUid: string, uid: string, summary: string): Promise<void> {
  return App.LinkEventToCard(taskUid, uid, summary)
}

/** projectsOfEvent returns the projects an event is filed under. */
export function projectsOfEvent(uid: string): Promise<number[]> {
  return App.ProjectsOfEvent(uid).then((list) => list ?? [])
}

/** listProjectEvents returns a project's events, coming ones first. */
export function listProjectEvents(projectId: number): Promise<ProjectEvent[]> {
  return App.ListProjectEvents(projectId).then((list) => (list ?? []) as unknown as ProjectEvent[])
}

/** addProjectPerson adds someone to a project by address. */
export function addProjectPerson(projectId: number, email: string, name: string): Promise<void> {
  return App.AddProjectPerson(projectId, email, name)
}

/** removeProjectPerson takes someone off a project. */
export function removeProjectPerson(projectId: number, email: string): Promise<void> {
  return App.RemoveProjectPerson(projectId, email)
}

/** listCardLinks returns the mail, events and notes a card holds. */
export function listCardLinks(taskUid: string): Promise<ProjectLink[]> {
  return App.ListCardLinks(taskUid).then((list) => (list ?? []) as unknown as ProjectLink[])
}

/** removeProjectLink removes one link of a project or a card. */
export function removeProjectLink(id: number): Promise<void> {
  return App.RemoveProjectLink(id)
}

/** attachProjectNote attaches a project's note to one of its cards. */
export function attachProjectNote(noteId: number, taskUid: string): Promise<void> {
  return App.AttachProjectNote(noteId, taskUid)
}

/** listProjectNotes returns a project's notes, the last edited first. */
export function listProjectNotes(projectId: number): Promise<ProjectNoteRecord[]> {
  return App.ListProjectNotes(projectId).then((list) => (list ?? []) as unknown as ProjectNoteRecord[])
}

/** createProjectNote adds a note to a project. */
export function createProjectNote(projectId: number, title: string, body: string): Promise<ProjectNoteRecord> {
  return App.CreateProjectNote(projectId, title, body) as unknown as Promise<ProjectNoteRecord>
}

/** updateProjectNote saves a note's title and body. */
export function updateProjectNote(id: number, title: string, body: string): Promise<ProjectNoteRecord> {
  return App.UpdateProjectNote(id, title, body) as unknown as Promise<ProjectNoteRecord>
}

/** deleteProjectNote removes a note and the card links to it. */
export function deleteProjectNote(id: number): Promise<void> {
  return App.DeleteProjectNote(id)
}

/**
 * listProjectVaultNotes returns a project's notes in the notes folder and the
 * notes linking to it; the first call with a folder open moves the notes kept
 * in the database there.
 */
export function listProjectVaultNotes(projectId: number): Promise<ProjectVaultNotes> {
  return App.ListProjectVaultNotes(projectId) as unknown as Promise<ProjectVaultNotes>
}

/** createProjectVaultNote writes a new note of a project into its folder; body gets the project in its front matter. */
export function createProjectVaultNote(projectId: number, title: string, body: string): Promise<ProjectVaultNote> {
  return App.CreateProjectVaultNote(projectId, title, body) as unknown as Promise<ProjectVaultNote>
}

/** addNoteToProject puts a note of the notes folder in a project through its front matter. */
export function addNoteToProject(path: string, projectId: number): Promise<void> {
  return App.AddNoteToProject(path, projectId)
}

/** removeNoteFromProject takes a note out of a project and its cards; the file stays. */
export function removeNoteFromProject(path: string, projectId: number): Promise<void> {
  return App.RemoveNoteFromProject(path, projectId)
}

/** attachVaultNote attaches a note of the notes folder to a card. */
export function attachVaultNote(path: string, taskUid: string): Promise<void> {
  return App.AttachVaultNote(path, taskUid)
}

/** trashProjectNote moves a project's note to the trash folder of the notes folder; vaultUndo brings it back. */
export function trashProjectNote(path: string, projectId: number): Promise<VaultOp> {
  return App.TrashProjectNote(path, projectId) as unknown as Promise<VaultOp>
}

/** createEventDraftFromMessage prefills an event from a mail dropped on the calendar. */
export function createEventDraftFromMessage(messageId: number): Promise<EventDraft> {
  return App.CreateEventDraftFromMessage(messageId) as unknown as Promise<EventDraft>
}

// demoSieve answers the sieve calls in demo mode, which never reaches a server.
function demoSieve(accountId: number): SieveState {
  return {
    accountId,
    status: 'unsupported',
    detail: '',
    host: '',
    port: 0,
    tls: '',
    manual: false,
    remembered: false,
    caps: { implementation: '', extensions: [], vacation: false, dates: false, fileInto: false, flags: false, envelope: false, include: false },
    exists: false,
    active: false,
    editable: true,
    raw: '',
    script: { vacation: null, rules: [], includes: [] },
    otherActive: '',
    hasPrevious: false,
    addresses: [],
  }
}

// sieveState connects to an account's sieve service (out of office and server
// filters) and reports what is there.
export function sieveState(accountId: number): Promise<SieveState> {
  if (isDemoActive()) {
    return Promise.resolve(demoSieve(accountId))
  }
  return sieveCall(App.SieveState(accountId))
}

function sieveCall(call: Promise<unknown>): Promise<SieveState> {
  return call.then((s) => normalizeState(s as SieveState))
}

function sieveSaveCall(call: Promise<unknown>): Promise<SieveSave> {
  return call.then((r) => {
    const out = r as SieveSave
    return { ...out, state: normalizeState(out.state) }
  })
}

// recheckSieve looks for a sieve service again on a server remembered as having none.
export function recheckSieve(accountId: number): Promise<SieveState> {
  if (isDemoActive()) {
    return Promise.resolve(demoSieve(accountId))
  }
  return sieveCall(App.RecheckSieve(accountId))
}

// setSieveConnection sets the sieve host, port and security by hand; '' and 0
// go back to the defaults, tls is 'starttls' or 'ssl'.
export function setSieveConnection(accountId: number, host: string, port: number, tls: string): Promise<SieveState> {
  return sieveCall(App.SetSieveConnection(accountId, host, port, tls))
}

// saveSieveVacation writes the out of office to the server and activates it.
export function saveSieveVacation(accountId: number, vacation: SieveVacation): Promise<SieveSave> {
  return sieveSaveCall(App.SaveSieveVacation(accountId, vacation))
}

// saveSieveRules writes the server filters to the server and activates them.
export function saveSieveRules(accountId: number, rules: SieveRule[]): Promise<SieveSave> {
  return sieveSaveCall(App.SaveSieveRules(accountId, rules))
}

// checkSieveScript asks the server whether a script is valid, storing nothing.
export function checkSieveScript(accountId: number, text: string): Promise<SieveSave> {
  return sieveSaveCall(App.CheckSieveScript(accountId, text))
}

// saveSieveScript writes a script as text after the server checked it.
export function saveSieveScript(accountId: number, text: string): Promise<SieveSave> {
  return sieveSaveCall(App.SaveSieveScript(accountId, text))
}

// restoreSievePrevious undoes the last sieve save.
export function restoreSievePrevious(accountId: number): Promise<SieveState> {
  return sieveCall(App.RestoreSievePrevious(accountId))
}

// --- the notes space -----------------------------------------------------------
// the folder of Markdown notes. These are not the private notes on a message
// above (getNote and friends), which live in the database.

/** vaultOpen opens the notes folder in the settings, creating the default one on first use. */
export function vaultOpen(): Promise<VaultInfo> {
  return App.VaultOpen() as unknown as Promise<VaultInfo>
}

/** vaultChooseFolder asks for a notes folder and opens it; an empty folder means cancelled. */
export function vaultChooseFolder(): Promise<VaultInfo> {
  return App.VaultChooseFolder() as unknown as Promise<VaultInfo>
}

/** vaultUseDefaultFolder goes back to the Notes folder in the data directory. */
export function vaultUseDefaultFolder(): Promise<VaultInfo> {
  return App.VaultUseDefaultFolder() as unknown as Promise<VaultInfo>
}

/** vaultRescan checks the notes folder for changes made by other apps now. */
export function vaultRescan(): Promise<void> {
  return App.VaultRescan()
}

/** vaultSnapshot returns every note, folder and other file of the notes folder. */
export function vaultSnapshot(): Promise<VaultSnapshot> {
  return App.VaultSnapshot() as unknown as Promise<VaultSnapshot>
}

/** vaultRead returns a note's text and the hash a save passes back. */
export function vaultRead(path: string): Promise<VaultFile> {
  return App.VaultRead(path) as unknown as Promise<VaultFile>
}

/** vaultSave writes a note edited from the version with baseHash; force overwrites a newer one. */
export function vaultSave(path: string, content: string, baseHash: string, force = false): Promise<VaultSave> {
  return App.VaultSave(path, content, baseHash, force) as unknown as Promise<VaultSave>
}

/** vaultCreate writes a new note and fails when the name is taken. */
export function vaultCreate(path: string, content: string): Promise<string> {
  return App.VaultCreate(path, content)
}

/** vaultUniquePath returns path, or path with a number added when it is taken. */
export function vaultUniquePath(path: string): Promise<string> {
  return App.VaultUniquePath(path)
}

/** vaultCreateFolder makes a folder. */
export function vaultCreateFolder(path: string): Promise<string> {
  return App.VaultCreateFolder(path)
}

/** vaultMove renames or moves a note or folder and updates the links to it. */
export function vaultMove(from: string, to: string): Promise<VaultOp> {
  return App.VaultMove(from, to) as unknown as Promise<VaultOp>
}

/** vaultDelete moves a note or folder to the trash folder of the vault. */
export function vaultDelete(path: string): Promise<VaultOp> {
  return App.VaultDelete(path) as unknown as Promise<VaultOp>
}

/** vaultUndo takes back a rename, move or delete. */
export function vaultUndo(id: string): Promise<void> {
  return App.VaultUndo(id)
}

/** vaultBacklinks lists the lines in other notes that link to a note. */
export function vaultBacklinks(path: string): Promise<VaultMention[]> {
  return App.VaultBacklinks(path).then((m) => (m ?? []) as VaultMention[])
}

/** vaultUnlinkedMentions lists the lines that name a note without linking to it. */
export function vaultUnlinkedMentions(path: string): Promise<VaultMention[]> {
  return App.VaultUnlinkedMentions(path).then((m) => (m ?? []) as VaultMention[])
}

/** vaultLinkMention turns an unlinked mention into a link to target. */
export function vaultLinkMention(source: string, line: number, match: string, target: string): Promise<void> {
  return App.VaultLinkMention(source, line, match, target)
}

/** vaultSearch finds notes by text, name, aliases, headings and tags. */
export function vaultSearch(query: string): Promise<VaultHit[]> {
  return App.VaultSearch(query).then((h) => (h ?? []) as VaultHit[])
}

/** vaultSaveAttachment stores a pasted file (base64) in the attachments folder and returns its path. */
export function vaultSaveAttachment(name: string, base64: string): Promise<string> {
  return App.VaultSaveAttachment(name, base64)
}

/** vaultImportFile copies a file dropped from the system into the attachments folder and returns its path. */
export function vaultImportFile(path: string): Promise<string> {
  return App.VaultImportFile(path)
}

/** vaultFileData returns a file of the notes folder as a data: URL. */
export function vaultFileData(path: string): Promise<string> {
  return App.VaultFileData(path)
}

// --- feeds ------------------------------------------------------------------------

const emptyFeedSidebar: FeedSidebar = { folders: [], feeds: [], counts: { unread: 0, today: 0, starred: 0, readLater: 0 } }

/** feedScopeDTO turns a list scope into what the backend reads. */
function feedScopeDTO(scope: FeedScope, unreadOnly: boolean, search: string): desktop.FeedScopeDTO {
  const id = scope.kind === 'feed' || scope.kind === 'folder' ? scope.id : 0
  return new desktop.FeedScopeDTO({ kind: scope.kind, id, unreadOnly, search })
}

/** listFeedSidebar returns the folders, the feeds and the smart view counts. */
export function listFeedSidebar(): Promise<FeedSidebar> {
  if (isDemoActive()) {
    return Promise.resolve(emptyFeedSidebar)
  }
  return App.ListFeedSidebar().then((s) => ({
    folders: (s.folders ?? []) as FeedFolder[],
    feeds: (s.feeds ?? []) as Feed[],
    counts: s.counts,
  }))
}

/** listFeedEntries returns a page of the entries a scope selects, newest first. */
export function listFeedEntries(scope: FeedScope, unreadOnly: boolean, search: string, limit: number, offset: number): Promise<FeedEntry[]> {
  if (isDemoActive() || scope.kind === 'newsletters') {
    return Promise.resolve([])
  }
  return App.ListFeedEntries(feedScopeDTO(scope, unreadOnly, search), limit, offset).then((list) => (list ?? []) as unknown as FeedEntry[])
}

/** searchFeedEntries returns the newest entries of every feed matching text, read or not. */
export function searchFeedEntries(text: string, limit: number): Promise<FeedEntry[]> {
  if (isDemoActive() || !text.trim()) {
    return Promise.resolve([])
  }
  return App.ListFeedEntries(new desktop.FeedScopeDTO({ kind: 'all', id: 0, unreadOnly: false, search: text }), limit, 0).then((list) => (list ?? []) as unknown as FeedEntry[])
}

/** getFeedEntry returns an entry for the reader, its html sanitized. */
export function getFeedEntry(id: number): Promise<FeedEntryDetail> {
  return App.GetFeedEntry(id) as unknown as Promise<FeedEntryDetail>
}

/** getFeedEntryHtml renders an entry again with remote images allowed or not. */
export function getFeedEntryHtml(id: number, allowRemote: boolean, includeTrackers: boolean): Promise<string> {
  return App.GetFeedEntryHTML(id, allowRemote, includeTrackers)
}

/** setFeedEntriesRead marks entries read or unread; it returns the ids that changed. */
export function setFeedEntriesRead(ids: number[], read: boolean): Promise<number[]> {
  return App.SetFeedEntriesRead(ids, read).then((list) => list ?? [])
}

/** markFeedScopeRead marks every unread entry of a scope read and returns their ids. */
export function markFeedScopeRead(scope: FeedScope, search: string): Promise<number[]> {
  return App.MarkFeedScopeRead(feedScopeDTO(scope, true, search)).then((list) => list ?? [])
}

/** setFeedEntryStarred stars or unstars an entry. */
export function setFeedEntryStarred(id: number, starred: boolean): Promise<void> {
  return App.SetFeedEntryStarred(id, starred)
}

/** setFeedEntryReadLater puts an entry on the read-later list or takes it off. */
export function setFeedEntryReadLater(id: number, later: boolean): Promise<void> {
  return App.SetFeedEntryReadLater(id, later)
}

/** discoverFeeds finds the working feeds at an address, with a preview of each. */
export function discoverFeeds(address: string): Promise<FeedCandidate[]> {
  return App.DiscoverFeeds(address).then((list) => (list ?? []) as unknown as FeedCandidate[])
}

/** addFeed subscribes to a feed, fetches it and returns it. */
export function addFeed(address: string, folderId: number, title: string): Promise<Feed> {
  return App.AddFeed(address, folderId, title) as unknown as Promise<Feed>
}

/** updateFeed saves what the user set on a feed. */
export function updateFeed(settings: FeedSettings): Promise<void> {
  return App.UpdateFeed(new desktop.FeedSettingsDTO(settings))
}

/** deleteFeed unsubscribes from a feed and drops its entries. */
export function deleteFeed(id: number): Promise<void> {
  return App.DeleteFeed(id)
}

/** moveFeeds puts feeds into a folder (0 for none) in the given order. */
export function moveFeeds(folderId: number, ids: number[]): Promise<void> {
  return App.MoveFeeds(folderId, ids)
}

/** createFeedFolder adds a folder at the end. */
export function createFeedFolder(name: string): Promise<FeedFolder> {
  return App.CreateFeedFolder(name) as unknown as Promise<FeedFolder>
}

/** renameFeedFolder renames a folder. */
export function renameFeedFolder(id: number, name: string): Promise<void> {
  return App.RenameFeedFolder(id, name)
}

/** deleteFeedFolder removes a folder; its feeds move out of it. */
export function deleteFeedFolder(id: number): Promise<void> {
  return App.DeleteFeedFolder(id)
}

/** reorderFeedFolders puts the folders in the given order. */
export function reorderFeedFolders(ids: number[]): Promise<void> {
  return App.ReorderFeedFolders(ids)
}

/** refreshFeeds fetches every feed in the background; progress arrives as events. */
export function refreshFeeds(): Promise<void> {
  return App.RefreshFeeds()
}

/** refreshFeed fetches one feed in the background. */
export function refreshFeed(id: number): Promise<void> {
  return App.RefreshFeed(id)
}

/** importOpml asks for an OPML file and subscribes to its feeds. */
export function importOpml(): Promise<FeedImportResult> {
  return App.ImportOPML() as unknown as Promise<FeedImportResult>
}

/** exportOpml writes the subscriptions to a file the user picks; '' when cancelled. */
export function exportOpml(): Promise<string> {
  return App.ExportOPML()
}

/** listNewsletterMail returns the newest newsletter mail for the Feeds space. */
export function listNewsletterMail(limit: number, offset: number, unreadOnly: boolean): Promise<MessageSummary[]> {
  if (isDemoActive()) {
    return Promise.resolve([])
  }
  return App.ListNewsletterMail(limit, offset, unreadOnly).then((list) => (list ?? []) as unknown as MessageSummary[])
}

/** countNewsletterMail counts the unread newsletter mail. */
export function countNewsletterMail(): Promise<number> {
  if (isDemoActive()) {
    return Promise.resolve(0)
  }
  return App.CountNewsletterMail()
}

// chat. Every call reaches the user's own homeserver through the backend; in
// demo mode chat stays signed out and none of them leave the frontend.

const signedOutChat: ChatStatus = {
  state: 'signedOut',
  userId: '',
  deviceId: '',
  homeserver: '',
  displayName: '',
  avatar: '',
  error: '',
  encryption: {
    ready: false,
    deviceVerified: false,
    crossSigning: false,
    secretStorage: false,
    secretStoragePassphrase: false,
    backupVersion: '',
    backupKeys: 0,
    backupActive: false,
    fingerprint: '',
  },
}

/** The chat account's state; signed out while chat is off. */
export function chatStatus(): Promise<ChatStatus> {
  if (isDemoActive()) {
    return Promise.resolve(signedOutChat)
  }
  return App.ChatStatus() as unknown as Promise<ChatStatus>
}

/** Finds the homeserver for a server name, Matrix ID or URL, and its sign-in methods. */
export function chatDiscover(input: string): Promise<ChatServer> {
  return App.ChatDiscover(input) as unknown as Promise<ChatServer>
}

/** Signs in with a user name and password. */
export function chatLoginPassword(homeserver: string, user: string, password: string): Promise<void> {
  return App.ChatLoginPassword(homeserver, user, password)
}

/** Signs in through the server's single sign-on page in the system browser. */
export function chatLoginSSO(homeserver: string, providerId: string): Promise<void> {
  return App.ChatLoginSSO(homeserver, providerId)
}

/** Abandons a browser sign-in that is waiting. */
export function chatCancelSSO(): Promise<void> {
  return App.ChatCancelSSO()
}

/** Signs in with a login token. */
export function chatLoginToken(homeserver: string, token: string): Promise<void> {
  return App.ChatLoginToken(homeserver, token)
}

/** Signs this device out and removes its chat data and keys. */
export function chatSignOut(): Promise<void> {
  return App.ChatSignOut()
}

/** Every room, direct chat, space and invite, most recent first. */
export function chatRooms(): Promise<ChatRoom[]> {
  if (isDemoActive()) {
    return Promise.resolve([])
  }
  return App.ChatRooms().then((list) => (list ?? []) as unknown as ChatRoom[])
}

/** Up to limit messages older than before, or the newest when before is empty. */
export function chatTimeline(roomId: string, before: string, limit: number): Promise<ChatTimelinePage> {
  return App.ChatTimeline(roomId, before, limit) as unknown as Promise<ChatTimelinePage>
}

/** Sends a message, reply or edit and returns its event id. */
export function chatSend(req: ChatSendRequest): Promise<string> {
  return App.ChatSend(req)
}

/** Reacts to a message. */
export function chatReact(roomId: string, eventId: string, key: string): Promise<string> {
  return App.ChatReact(roomId, eventId, key)
}

/** Removes a message or takes back a reaction. */
export function chatRedact(roomId: string, eventId: string): Promise<void> {
  return App.ChatRedact(roomId, eventId)
}

/** Tells a room the user is typing; the backend drops it when typing notices are off. */
export function chatTyping(roomId: string, typing: boolean): Promise<void> {
  return App.ChatTyping(roomId, typing)
}

/** Moves the read marker of a room. */
export function chatMarkRead(roomId: string, eventId: string): Promise<void> {
  return App.ChatMarkRead(roomId, eventId)
}

/** Who is typing in a room. */
export function chatTypists(roomId: string): Promise<ChatMember[]> {
  return App.ChatTypists(roomId).then((list) => (list ?? []) as unknown as ChatMember[])
}

/** A room's members, by power level and name. */
export function chatMembers(roomId: string): Promise<ChatMember[]> {
  return App.ChatMembers(roomId).then((list) => (list ?? []) as unknown as ChatMember[])
}

/** Creates a room and returns its id. */
export function chatCreateRoom(req: ChatCreateRoomRequest): Promise<string> {
  return App.ChatCreateRoom(req)
}

/** The direct chat with a person, started when there is none. */
export function chatOpenDirect(userId: string): Promise<string> {
  return App.ChatOpenDirect(userId)
}

/** Joins a room by address or link, or accepts an invite. */
export function chatJoin(target: string): Promise<string> {
  return App.ChatJoin(target)
}

/** Leaves a room or declines an invite. */
export function chatLeave(roomId: string): Promise<void> {
  return App.ChatLeave(roomId)
}

/** Invites a person to a room. */
export function chatInvite(roomId: string, userId: string): Promise<void> {
  return App.ChatInvite(roomId, userId)
}

/** Adds a room to or removes it from the favourites. */
export function chatSetFavourite(roomId: string, favourite: boolean): Promise<void> {
  return App.ChatSetFavourite(roomId, favourite)
}

/** A person's public profile, to show who a Matrix ID is. */
export function chatLookupProfile(userId: string): Promise<ChatProfile> {
  return App.ChatLookupProfile(userId) as unknown as Promise<ChatProfile>
}

/** A message's file or thumbnail, in the shape the attachment previewer reads. */
export async function chatMedia(roomId: string, eventId: string, thumbnail: boolean): Promise<AttachmentContent> {
  const m = await App.ChatMedia(roomId, eventId, thumbnail)
  return { filename: m.name, contentType: m.mime, sizeBytes: m.size, data: m.data, tooLarge: m.tooLarge }
}

/** A small mxc picture as a data url. */
export function chatAvatar(mxc: string): Promise<string> {
  return App.ChatAvatar(mxc)
}

/** An mxc image from message html as a data url. */
export function chatInlineImage(mxc: string): Promise<string> {
  return App.ChatInlineImage(mxc)
}

/** Asks where to save a message's file and saves it; empty when cancelled. */
export function chatSaveMedia(roomId: string, eventId: string): Promise<string> {
  return App.ChatSaveMedia(roomId, eventId)
}

/** The largest file the homeserver accepts, or 0 when it does not say. */
export function chatUploadLimit(): Promise<number> {
  return App.ChatUploadLimit()
}

/** Sends a file the frontend holds, such as a pasted image. */
export function chatSendFileData(roomId: string, name: string, mime: string, base64: string, uploadId: string, replyTo: string): Promise<string> {
  return App.ChatSendFileData({ roomId, name, mime, replyTo }, base64, uploadId)
}

/** Sends files from disk, such as dropped ones. */
export function chatSendFilePaths(roomId: string, paths: string[], uploadIds: string[], replyTo: string): Promise<void> {
  return App.ChatSendFilePaths(roomId, paths, uploadIds, replyTo)
}

/** Asks for files to send. */
export function chatPickFiles(): Promise<string[]> {
  return App.ChatPickFiles().then((list) => list ?? [])
}

/** Asks again for the keys of undecryptable messages. */
export function chatRetryDecryption(roomId: string, eventIds: string[]): Promise<void> {
  return App.ChatRetryDecryption(roomId, eventIds)
}

/** The device verifications in flight. */
export function chatVerifications(): Promise<ChatVerification[]> {
  if (isDemoActive()) {
    return Promise.resolve([])
  }
  return App.ChatVerifications().then((list) => (list ?? []) as unknown as ChatVerification[])
}

/** Asks another device to verify; one of the user's own when userId is empty. */
export function chatStartVerification(userId: string): Promise<string> {
  return App.ChatStartVerification(userId)
}

/** Accepts a verification request. */
export function chatAcceptVerification(id: string): Promise<void> {
  return App.ChatAcceptVerification(id)
}

/** Starts comparing emoji in a ready verification. */
export function chatStartSAS(id: string): Promise<void> {
  return App.ChatStartSAS(id)
}

/** Says the emoji match. */
export function chatConfirmSAS(id: string): Promise<void> {
  return App.ChatConfirmSAS(id)
}

/** Stops a verification; mismatch says the emoji did not match. */
export function chatCancelVerification(id: string, mismatch: boolean): Promise<void> {
  return App.ChatCancelVerification(id, mismatch)
}

/** Forgets a finished verification. */
export function chatDismissVerification(id: string): Promise<void> {
  return App.ChatDismissVerification(id)
}

/** Reads the encryption state again. */
export function chatRefreshEncryption(): Promise<void> {
  return App.ChatRefreshEncryption()
}

/** Opens secure storage with the recovery key, or its passphrase. */
export function chatUnlock(secret: string, passphrase: boolean): Promise<void> {
  return App.ChatUnlock(secret, passphrase)
}

/** Sets up cross-signing, secure storage and key backup; returns the recovery key. */
export function chatSetupEncryption(passphrase: string, password: string): Promise<string> {
  return App.ChatSetupEncryption(passphrase, password).then((r) => r.recoveryKey)
}

/** The user's sessions, this one first. */
export function chatDevices(): Promise<ChatDevice[]> {
  return App.ChatDevices().then((list) => (list ?? []) as unknown as ChatDevice[])
}

/** Signs out another of the user's sessions. */
export function chatSignOutDevice(deviceId: string, password: string): Promise<void> {
  return App.ChatSignOutDevice(deviceId, password)
}

/** Whether the user verified a person. */
export function chatUserTrust(userId: string): Promise<boolean> {
  return App.ChatUserTrust(userId)
}

/** Tells the backend which room is on screen, so it stays quiet while focused. */
export function chatSetViewing(roomId: string): Promise<void> {
  if (isDemoActive()) {
    return Promise.resolve()
  }
  return App.ChatSetViewing(roomId)
}

// automations. The generated models type enums and maps their own way, so the
// values cross this boundary as the shapes lib/scripting.ts declares.

function asScript(s: desktop.ScriptDTO): Script {
  return s as unknown as Script
}

function asRun(r: desktop.ScriptRunDTO): ScriptRun {
  return r as unknown as ScriptRun
}

function toDTO(input: ScriptInput): desktop.ScriptInput {
  return input as unknown as desktop.ScriptInput
}

/** Everything scripts can use, as data. */
export function scriptRegistry(): Promise<ScriptCatalog> {
  return App.ScriptRegistry().then((c) => c as unknown as ScriptCatalog)
}

/** Every script, by name. */
export function listScripts(): Promise<Script[]> {
  if (isDemoActive()) {
    return Promise.resolve([])
  }
  return App.ListScripts().then((list) => (list ?? []).map(asScript))
}

/** Stores a new script, switched off. */
export function createScript(input: ScriptInput): Promise<Script> {
  return App.CreateScript(toDTO(input)).then(asScript)
}

/** Saves a script's content; a new permission switches it off until approved. */
export function updateScript(id: number, input: ScriptInput): Promise<Script> {
  return App.UpdateScript(id, toDTO(input)).then(asScript)
}

/** Removes a script with its log and storage. */
export function deleteScript(id: number): Promise<void> {
  return App.DeleteScript(id)
}

/** Switches a script on, approving the given permissions, or off. */
export function setScriptEnabled(id: number, enabled: boolean, approve: string[]): Promise<Script> {
  return App.SetScriptEnabled(id, enabled, approve).then(asScript)
}

/** Lets a script make a risky call, e.g. "mail.send", without asking, or takes that back. */
export function setScriptAllowUnasked(id: number, fn: string, allow: boolean): Promise<Script> {
  return App.SetScriptAllowUnasked(id, fn, allow).then(asScript)
}

/** Stores the values of a script's settings. */
export function setScriptSettings(id: number, values: Record<string, unknown>): Promise<Script> {
  return App.SetScriptSettings(id, values).then(asScript)
}

/** Sets what a mail script does with mail that arrived while Pelton was closed. */
export function setScriptCatchUp(id: number, mode: string, cap: number): Promise<Script> {
  return App.SetScriptCatchUp(id, mode, cap).then(asScript)
}

/** Compiles a block tree (JSON) to Lua for a trigger, with every problem found. */
export function compileScriptBlocks(blocks: string, trigger: string): Promise<CompileResult> {
  return App.CompileScriptBlocks(blocks, trigger).then((r) => r as unknown as CompileResult)
}

/** A syntax error in Lua, or '' when it parses. */
export function checkScriptLua(source: string): Promise<string> {
  return App.CheckScriptLua(source)
}

/** Turns a block script into a Lua script for good. */
export function convertScriptToLua(id: number): Promise<Script> {
  return App.ConvertScriptToLua(id).then(asScript)
}

/** Runs an enabled script now, on a message when it takes one (0 for none). */
export function runScript(id: number, messageId: number): Promise<ScriptRun> {
  return App.RunScript(id, messageId).then(asRun)
}

/** Dry runs a script as written, against a message (0 for the sample). Nothing changes. */
export function testScript(input: ScriptInput, messageId: number): Promise<ScriptRun> {
  return App.TestScript(toDTO(input), messageId).then(asRun)
}

/** The latest runs of a script, or of every script for 0, newest first. */
export function scriptRuns(scriptId: number, limit: number): Promise<ScriptRun[]> {
  if (isDemoActive()) {
    return Promise.resolve([])
  }
  return App.ScriptRuns(scriptId, limit).then((list) => (list ?? []).map(asRun))
}

/** Empties the log of a script, or every log for 0. */
export function clearScriptRuns(scriptId: number): Promise<void> {
  return App.ClearScriptRuns(scriptId)
}

/** How many days the run log keeps. */
export function scriptLogDays(): Promise<number> {
  return App.ScriptLogDays()
}

/** Sets how many days the run log keeps, 1 to 365. */
export function setScriptLogDays(days: number): Promise<void> {
  return App.SetScriptLogDays(days)
}

/** Reverses the mail changes of a run. */
export function undoScriptRun(runId: number): Promise<void> {
  return App.UndoScriptRun(runId)
}

/** Answers a confirmation a script asked for. */
export function answerScriptConfirm(id: string, allow: boolean, always: boolean): Promise<void> {
  return App.AnswerScriptConfirm(id, allow, always)
}

/** Answers whether a script runs over mail that arrived while Pelton was closed. */
export function resolveScriptCatchUp(scriptId: number, run: boolean): Promise<void> {
  return App.ResolveScriptCatchUp(scriptId, run)
}

/** The catch-up questions still open. */
export function pendingScriptCatchUps(): Promise<ScriptCatchUp[]> {
  if (isDemoActive()) {
    return Promise.resolve([])
  }
  return App.PendingScriptCatchUps().then((list) => (list ?? []) as ScriptCatchUp[])
}

/** Writes a script to a file the user picks; resolves to the path, '' when cancelled. */
export function exportScript(id: number): Promise<string> {
  return App.ExportScript(id)
}

/** Asks for a script file; resolves to its path, '' when cancelled. */
export function pickScriptFile(): Promise<string> {
  return App.PickScriptFile()
}

/** Reads a script file for the import dialog, storing and running nothing. */
export function readScriptFile(path: string): Promise<ScriptFile> {
  return App.ReadScriptFile(path).then((f) => ({ script: asScript(f.script), text: f.text }))
}

/** Stores the script in a file's text, switched off and marked as imported. */
export function importScript(text: string): Promise<Script> {
  return App.ImportScript(text).then(asScript)
}
