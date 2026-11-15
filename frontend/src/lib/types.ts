// types.ts defines the dto shapes the ui works with as plain interfaces. they
// mirror the wails-generated go dtos (frontend/wailsjs/go/models.ts) field for
// field, but are decoupled from the generated classes so the ui can spread and
// construct them freely. the generated class instances returned by the bindings
// are structurally assignable to these interfaces; api.ts wraps the few request
// types back into their generated classes before calling the bindings.

export interface Account {
  id: number
  email: string
  // the name recipients see in the From header of mail sent from this account.
  displayName: string
  // what this app calls the mailbox instead, when useLocalLabel is set. it is
  // never sent anywhere, and stays stored when the toggle goes off. use
  // accountLabel() rather than reading these two directly.
  localLabel: string
  useLocalLabel: boolean
  // the login name when it differs from the email; empty logs in with email.
  username: string
  imapHost: string
  imapPort: number
  smtpHost: string
  smtpPort: number
  // the Local Folders account, which holds imported mail and has no server:
  // it never syncs, and the ui hides the server-side folder actions on it.
  local: boolean
  // pinned connection security, a TLSMode value: 'ssl', 'starttls', or '' to
  // derive it from the port, which is what accounts created before it was
  // storable use. typed as string to match the generated dto, which cannot
  // carry the union.
  imapTls: string
  smtpTls: string
  // write a local .eml copy of every message archived from this account.
  // exportDir is where the files go, exportSubfolders is 'none' | 'year' |
  // 'month', and exportNameTemplate is the file name pattern ('' for the
  // default). typed as string to match the generated dto.
  exportOnArchive: boolean
  exportDir: string
  exportSubfolders: string
  exportNameTemplate: string
  // how this account starts a new message: '' unprotected, 'sign', or 'auto'
  // to sign and encrypt whenever every recipient has a key.
  pgpDefault: string
  // marks this account's mail in lists that mix accounts (#486); '' is none.
  color?: string
  // the user told the missing-password prompt to stop asking about this
  // account. It still cannot sync; the ui marks it instead of interrupting.
  passwordPromptDismissed: boolean
  // fingerprints of server certificates the user accepted for this mailbox,
  // formatted for reading, and the subjects of the CA it trusts (#446).
  trustedCerts: string[]
  caSubjects: string[]
  // the route this mailbox's connections take (#457), password left out.
  proxy: AccountProxy
}

// AccountProxyMode is where a mailbox's connections go: 'global' follows the
// app-wide network setting, 'off' connects directly, 'system' follows the
// proxy environment variables and 'manual' uses the mailbox's own proxy.
export type AccountProxyMode = 'global' | 'off' | 'system' | 'manual'

// AccountProxy is a mailbox's route. password is write-only, as in
// ProxyConfig: empty with hasPassword keeps the stored one. The two useGlobal
// switches send contacts sync and sign-in refresh the app-wide way instead.
export interface AccountProxy {
  mode: string
  scheme: string
  host: string
  port: number
  username: string
  password: string
  hasPassword: boolean
  contactsUseGlobal: boolean
  oauthUseGlobal: boolean
}

// ThunderbirdAccount is one account read out of a Thunderbird profile. There is
// no password: Thunderbird keeps its own, so an imported account is
// re-authenticated once.
export interface ThunderbirdAccount {
  email: string
  displayName: string
  username: string
  imapHost: string
  imapPort: number
  smtpHost: string
  smtpPort: number
  // 'imap' or 'pop3'. Pelton speaks imap, so a pop3 account is listed but
  // cannot be imported.
  kind: string
  // true when this address is already configured here.
  exists: boolean
}

// ThunderbirdFolder is one mail folder found on disk in a profile.
export interface ThunderbirdFolder {
  name: string
  path: string
  sizeBytes: number
}

// ThunderbirdProfile is one discovered profile and everything it holds.
export interface ThunderbirdProfile {
  name: string
  path: string
  accounts: ThunderbirdAccount[]
  localFolders: ThunderbirdFolder[]
}

// TLSMode is the connection security for one leg of an account.
export type TLSMode = '' | 'ssl' | 'starttls'

export interface Folder {
  id: number
  accountId: number
  name: string
  imapPath: string
  delimiter: string
  // null/undefined at the tree root; a parent folder id otherwise. optional to
  // match the generated dto where the go nil pointer becomes undefined.
  parentId?: number | null
  role: string
  unreadCount: number
  totalCount: number
  attributes: string[]
  // mirrored into the sidebar's Pinned group. the folder stays in its own
  // account's tree either way, so this is display state, not a move.
  pinned: boolean
  // the role the user assigned by hand, empty when role above was detected.
  // kept apart from role so the picker can show whether the current state is a
  // choice or automatic detection.
  roleOverride: string
  // syncExcluded means the user unchecked this folder, so sync skips it. What
  // was already fetched stays readable; it just stops being updated (#173).
  syncExcluded: boolean
}

export interface UnifiedView {
  key: string
  label: string
  unreadCount: number
  totalCount: number
}

export interface MessageSummary {
  id: number
  accountId: number
  folderId: number
  accountEmail: string
  folderName: string
  subject: string
  fromName: string
  fromAddress: string
  snippet: string
  date: string
  seen: boolean
  flagged: boolean
  hasAttachments: boolean
  pgp: string
  auth: string
  // flagColor is 0 (none) or 1..8. offline marks a user-pinned message.
  // snoozeUntil is a stored timestamp (empty when not snoozed).
  flagColor: number
  offline: boolean
  snoozeUntil: string
  // senderVip is true when the from-address is on the VIP list, so the row can
  // show a star.
  senderVip: boolean
  // smime is the signature verdict recorded when the message synced.
  smime: SMIMESignature
  // threadId is the conversation the message belongs to within its account,
  // 0 until the background threading pass reaches mail cached before
  // threading existed.
  threadId: number
  // answered and forwarded mirror \Answered and $Forwarded, from the server
  // or from a reply or forward sent through Pelton.
  answered: boolean
  forwarded: boolean
}

/** ThreadSummary is one conversation row in the threaded list. head is the
 * newest message of the conversation in the listed folders, which the list is
 * ordered by. count, unreadCount, participants, hasAttachments and flagged
 * cover every member, the user's replies in Sent included; Trash and Junk only
 * count when they are what is listed. answered and forwarded are the head's.
 * messageIds are the members, oldest first, each message once. threadId is 0
 * for a message not threaded yet, which is then the only member. */
export interface ThreadSummary {
  threadId: number
  head: MessageSummary
  count: number
  unreadCount: number
  // distinct sender names, newest first, at most four.
  participants: string[]
  hasAttachments: boolean
  flagged: boolean
  answered: boolean
  forwarded: boolean
  messageIds: number[]
}

/** ThreadList is a page of conversations; total and paging count
 * conversations, not messages. */
export interface ThreadList {
  threads: ThreadSummary[]
  total: number
  hasOlder: boolean
}

/** ThreadMessage is one message of an open conversation. folderRole is the
 * role of its folder ('inbox', 'sent', 'archive', 'normal' and so on) and
 * fromMe is true when the user sent it. */
export interface ThreadMessage extends MessageSummary {
  folderRole: string
  fromMe: boolean
}

/** Thread is an open conversation, oldest message first. Bodies come from
 * getMessage per message. */
export interface Thread {
  threadId: number
  messages: ThreadMessage[]
}

// SMIMESignature is a message's s/mime signature verdict. An empty status means
// the message is not signed, which is most mail. detail explains anything that
// is not valid and is written to be shown as-is.
export interface SMIMESignature {
  // one of SMIMEStatus. typed as string to match the generated dto, the same
  // way pgp is, and narrowed at the point of use.
  status: string
  signer: string
  email: string
  issuer: string
  detail: string
}

export type SMIMEStatus = '' | 'valid' | 'untrusted' | 'invalid'

// MCPPermission is one write tool an agent may be allowed to use. Every tool is
// switched separately; group only says where the settings ui puts it.
export interface MCPPermission {
  tool: string
  group: string
  allowed: boolean
}

// AgentAction is one write an agent made, for the log. summary is written by
// the backend, never by the agent, so nothing an agent says lands here as fact.
export interface AgentAction {
  id: number
  tool: string
  summary: string
  error: string
  when: string
}

// AgentProposal is a message an agent wants sent, waiting on you. Nothing has
// been sent and nothing has reached a server.
export interface AgentProposal {
  id: number
  accountId: number
  to: string
  cc: string
  bcc: string
  subject: string
  body: string
  when: string
}


// SMIMERevocation is what the issuing authority says about a signing
// certificate now, which the signature itself cannot tell you: a message signed
// with a stolen key still verifies. status is '' when no check was made, either
// because the setting is off or the message carries no signature.
export interface SMIMERevocation {
  status: string
  detail: string
  revokedAt: string
  checked: string
}

// good means still in force, revoked means the authority has withdrawn it, and
// unknown means the question could not be answered. unknown is not evidence
// either way and must not read as one.
export type SMIMERevocationStatus = '' | 'good' | 'revoked' | 'unknown'


// PhishingSignal is one finding. kind is a stable identifier the ui turns into
// a sentence; detail is the domain, address or url it is about, and target the
// link a link signal came from.
export interface PhishingSignal {
  kind: string
  detail?: string
  target?: string
}

// PhishingReport is the verdict for one message. 'caution' means something is
// odd but has ordinary explanations; 'warning' means the message is claiming to
// be from someone it was not sent by. There is no 'safe': nothing local can
// establish that a message is genuine.
export interface PhishingReport {
  level: 'none' | 'caution' | 'warning'
  signals?: PhishingSignal[]
  links?: string[]
}

/** MessageDetail includes rendered content and bare RFC identifiers for replies. */
export interface MessageDetail extends MessageSummary {
  messageId: string
  inReplyTo: string[]
  references: string[]
  toAddresses: string
  ccAddresses: string
  bodyPlain: string
  // bodyQuote is the message as plain text for a reply or forward to quote:
  // bodyPlain when the message has a text part, and the html rendered down to
  // text when it does not. Replies to html-only mail quoted nothing before
  // this existed (#239).
  bodyQuote: string
  bodyHtmlSafe: string
  isHtml: boolean
  hasRemoteContent: boolean
  // remoteAllowed is true when remote content was rendered because the sender or
  // domain is trusted (or the global setting is on), so no banner is shown.
  remoteAllowed: boolean
  // remoteHosts lists the hosts blocked remote content would load from.
  remoteHosts: string[]
  // trackingPixels are the blocked remote images that look like they exist to
  // report the open rather than to be seen. Empty when detection is off.
  // Detection is a guess and will sometimes be wrong, so the ui says "look
  // like" and offers to load them anyway.
  trackingPixels: TrackingPixel[]
  attachments: Attachment[]
  // phishing is what the local checks made of the message. level 'none' means
  // nothing was found and the ui shows nothing at all.
  phishing: PhishingReport
  // charsetGuess names what the text was read as when the message declared no
  // encoding or one nothing knows, and is 'detected' when one was picked
  // without being reported back. Empty for mail that was right about itself.
  charsetGuess: string
  // unsubscribe describes the List-Unsubscribe mechanism the message
  // advertises: oneclick (RFC 8058 background POST), mailto (sent via the
  // account's smtp) or link (opened in the browser). null when none is on
  // record; the ui may still fall back to an unsubscribe link in the body.
  unsubscribe: UnsubscribeInfo | null
  // pgpState is '' for ordinary mail. for protected mail it says what happened
  // when it was opened, so the pane can offer the right next step rather than
  // one generic error.
  pgpState: PGPState
  // replyIds and forwardIds are the cached copies of what the user sent from
  // Pelton in reply to or as a forward of this message, oldest first. A sent
  // message appears once its Sent folder has synced.
  replyIds: number[]
  forwardIds: number[]
}

export type PGPState = '' | 'open' | 'locked' | 'nokey' | 'failed'

// TrackingPixel is one remote image that looks like it exists to report the
// open. reasons carries the signals behind that (tiny, hidden, known-host,
// recipient, opaque-id, lone-image) so the ui can show its working instead of
// asking to be believed.
export interface TrackingPixel {
  host: string
  url: string
  reasons: string[]
}

export interface UnsubscribeInfo {
  kind: 'oneclick' | 'mailto' | 'link'
  target: string
  // done is true when this sender was already unsubscribed from.
  done: boolean
}

export interface MessageList {
  messages: MessageSummary[]
  total: number
  // hasOlder means the server still holds mail older than anything cached for
  // this selection, so reaching total is not the end of the mailbox.
  hasOlder: boolean
}

// FetchOlderResult reports what one 'load older mail' round trip fetched, and
// whether anything is still left on the server after it.
export interface FetchOlderResult {
  fetched: number
  hasOlder: boolean
}

export interface Attachment {
  id: number
  filename: string
  contentType: string
  sizeBytes: number
  inline: boolean
}

export interface Address {
  name: string
  email: string
}

export interface ComposeAttachment {
  filename: string
  contentType: string
  contentBase64: string
  inline: boolean
  contentId: string
}

export interface ComposeRequest {
  accountId: number
  to: Address[]
  cc: Address[]
  bcc: Address[]
  subject: string
  text: string
  html: string
  inReplyTo: string
  references: string[]
  attachments: ComposeAttachment[]
  // optional RFC3339 timestamp for a scheduled ("send later") send. empty
  // means send immediately, subject to the undo-send delay.
  sendAt: string
  // pgp treatment for this message: 'none', 'sign', 'encrypt' or
  // 'signencrypt'. The send refuses rather than falling back to plaintext when
  // it cannot do what was asked.
  protection: string
  // draftMessageId is the Message-ID of this draft's copy in the server Drafts
  // folder, '' until it has been uploaded.
  draftMessageId: string
  // forwardOf is the id of the message this one forwards; once sent, that
  // message is marked forwarded. Leave it out (or 0) for anything else.
  forwardOf?: number
}

// ProtectionStatus is what the compose window needs to offer the pgp controls
// honestly for one account and one set of recipients.
export interface ProtectionStatus {
  canSign: boolean
  // signerLocked means signing will ask for a passphrase before it can send.
  signerLocked: boolean
  canEncrypt: boolean
  recipients: { email: string; hasKey: boolean }[]
  // the account's configured starting point, and what this message should
  // start as given the keys available.
  default: string
  suggested: string
}

export interface Draft {
  id: number
  savedAt: string
  request: ComposeRequest
  // locked is true for a draft of an encrypted message that could not be
  // opened with a passphrase Pelton currently holds. Its request is empty
  // until unsealDraft opens it.
  locked: boolean
  accountId: number
  protection: string
}

export interface OutboxRow {
  id: number
  accountId: number
  recipients: string[]
  state: string
  attempts: number
  lastError: string
  nextAttemptAt: string
  createdAt: string
  // editable is set when the message can be taken back into a composer.
  editable: boolean
  // priority orders the queue: a higher one goes before older messages.
  priority: number
}

/** A message taken back out of the outbox, or taken false when it could not be. */
export interface OutboxEdit {
  taken: boolean
  request: ComposeRequest
}

/**
 * How cancelling a send ended: back in Drafts, back as a request to open in a
 * composer, kept as a failed send, too late, or no longer in the outbox.
 */
export interface OutboxCancel {
  outcome: 'draft' | 'compose' | 'held' | 'tooLate' | 'gone'
  request: ComposeRequest
}

/** One line of the send activity log. */
export interface OutboxLogEntry {
  seq: number
  at: string
  messageId: number
  step: string
  recipients: string[]
  detail: string
  retryAt: string
}

export interface UIPrefs {
  theme: string
  accent: string
  density: string
  showMailboxBadge: boolean
  showDateTime: boolean
  showPgp: boolean
  showAuth: boolean
  toastPosition: string
  paneLocked: boolean
  sidebarWidth: number
  listWidth: number
  // sendDelaySeconds holds outgoing mail for this many seconds so the user can
  // undo. 0 disables the delay.
  sendDelaySeconds: number
  // flagHighlight controls how flagged rows stand out: flag, left, both, off.
  flagHighlight: string
  // showShortcutHints toggles the keyboard shortcut shown beside a context-menu
  // entry that has one. On by default.
  showShortcutHints: boolean
  // harvestAddresses keeps learning addresses from mail for compose
  // autocomplete. off leaves only the contacts from a synced address book.
  harvestAddresses: boolean
  // showAccountEmail shows the account email instead of its name in the sidebar.
  showAccountEmail: boolean
  // alwaysLoadImages disables remote-image blocking globally (off by default).
  alwaysLoadImages: boolean
  // blockTrackingPixels keeps images detected as tracking pixels blocked even
  // once the rest of a message's remote content is loaded. Off by default; the
  // private preset in onboarding turns it on.
  blockTrackingPixels: boolean
  // avatarSource selects the sender-photo fallback chain: bimi_gravatar,
  // gravatar_bimi, bimi, or pfp (generated only). avatarStyle picks the generated
  // placeholder look: initials, mono, pixel, or geometric.
  avatarSource: string
  avatarStyle: string
  // multiSelectEnabled allows selecting several rows at once for bulk actions.
  multiSelectEnabled: boolean
  // showSelectedCount toggles the "N selected" count text in the selection bar.
  showSelectedCount: boolean
  // selectAllScope is how far select-all reaches: 'offer' selects the loaded
  // messages and offers the rest, 'all' takes the whole list at once, 'loaded'
  // stops at what is loaded.
  selectAllScope: SelectAllScope
  // selectAllUnified offers select-all in the unified views too. Off by
  // default, since those span every account.
  selectAllUnified: boolean
  // the remembered sort order for each kind of search (see SearchKind). 'auto',
  // the default, reads the order off the query instead of fixing one.
  searchSortText: SearchSortPref
  searchSortDated: SearchSortPref
  searchSortFiltered: SearchSortPref
  // sidebarIndentGuides draws vertical guide lines for nested folders.
  sidebarIndentGuides: boolean
  // rowTemplate selects the list row layout: relaxed, comfortable, compact, single.
  rowTemplate: string
  // rowShowAvatar / rowShowSnippet are per-field overrides on the row template.
  rowShowAvatar: boolean
  rowShowSnippet: boolean
  // previewLines clamps the snippet to this many lines (where the template allows).
  previewLines: number
  // uiScale zooms the whole interface (string multiplier, '1' = 100%).
  uiScale: string
  // messageFontSize is the base font size (px) for rendered email content.
  messageFontSize: number
  // showFlaggedCount shows the count and bold styling on the sidebar Flagged view.
  showFlaggedCount: boolean
  // viewsPlacement controls how saved Views (preset searches) are surfaced:
  // hidden (off), sidebar (group in the mailbox sidebar), or tab (separate rail).
  viewsPlacement: ViewsPlacement
  // flagColorSync pushes color labels to the server as imap keywords.
  flagColorSync: boolean
  // showOfflineIndicator shows the little downloaded badge on pinned messages.
  showOfflineIndicator: boolean
  // showUnsyncedFolder marks folders excluded from sync in the sidebar, so a
  // folder that stopped receiving new mail says why.
  showUnsyncedFolder: boolean
  // restoreTabs brings back the reading-pane tabs that were open at quit. Off
  // by default: a tab is a temporary place to park a message.
  restoreTabs: boolean
  // list every profile directly in the command palette, so switching is one
  // keystroke rather than a picker step.
  paletteProfiles: boolean
  // swipe gestures on message rows (trackpad only).
  swipeEnabled: boolean
  swipeLeftAction: string
  swipeRightAction: string
  // composeVimMode enables vim keybindings in the compose editor.
  composeVimMode: boolean
  // downloadIncludeAttachments is the remembered default for the range download.
  downloadIncludeAttachments: boolean
  // appVimMode enables global vim-style navigation (h/j/k/l) for moving around
  // the app window itself, outside of compose.
  appVimMode: boolean
  // language is the ui locale code (en, de, fr, nl, es, etc).
  language: string
  // lowPowerMode pauses periodic auto-sync, bulk downloads and address-book
  // rescans. autoSyncIntervalSeconds is how often a full sync pass runs on top
  // of the always-on imap idle push (0 disables it).
  lowPowerMode: boolean
  autoSyncIntervalSeconds: number
  // defaultEditorMode is the editor a new compose session starts in
  // (plaintext, markdown, or wysiwyg).
  defaultEditorMode: string
  // composeAutocomplete offers address-book suggestions while typing a
  // recipient. composeChips renders recipients as removable chips; when off,
  // the recipient fields fall back to a plain comma-separated text input.
  composeAutocomplete: boolean
  composeChips: boolean
  // composeMarkdown formats Markdown as it is typed in the composer.
  composeMarkdown: boolean
  // smoothCaret glides the compose editor's caret between positions.
  smoothCaret: boolean
  // updateCheckFrequency controls the automatic GitHub-releases update check:
  // 'off' (default), 'startup', 'weekly', or 'monthly'.
  updateCheckFrequency: string
  // emptyStateImage is a data-uri image shown in the reading pane when no
  // message is open; empty means the bundled Pelton logo.
  emptyStateImage: string
  // emptyStateFullscreen shows the empty-state image as a full-bleed cover
  // background instead of a small centered mark.
  emptyStateFullscreen: boolean
  // cornerStyle picks the corner radius look: default, square, or round.
  cornerStyle: string
  // design picks the interface design; classic is the shell and settings
  // screen from before the redesign.
  design: DesignPref
  // layoutMode arranges the mail panes in the new design.
  layoutMode: LayoutMode
  // navStyle places the Mail/Contacts/Calendar switcher in the new design.
  navStyle: NavStyle
  // railSide is the window edge the rail sits on.
  railSide: RailSide
  // railPins lists the views pinned to the middle of the rail, comma
  // separated: "u:inbox" for a unified view, "v:12" for a saved one.
  railPins: string
  // paneStyle draws the panes in the new design.
  paneStyle: PaneStyle
  // avatarPlacement puts the sender picture in its own column, inline in the
  // address line, or hides it.
  avatarPlacement: AvatarPlacement
  // contactsView is how the contacts space shows its contacts.
  contactsView: ContactsView
  // listHeight is the message list height in the stacked layout, in px.
  listHeight: number
  // nativeTitlebar uses the operating system's title bar instead of the
  // integrated one.
  nativeTitlebar: boolean
  // quickFilterBar shows the quick filter bar above the message list in the
  // new design.
  quickFilterBar: boolean
  // composeOpenIn is where a new message, reply or forward opens in the new
  // design.
  composeOpenIn: ComposeOpenIn
  // threadedList groups the message list into conversations in the new
  // design.
  threadedList: boolean
  // readerWidth caps the reading column in the new design.
  readerWidth: ReaderWidth
  // senderPanel shows the panel about the open message's sender beside the
  // reader in the new design.
  senderPanel: boolean
  // themeId selects an installed custom theme; empty means the built-in
  // default themes driven by the theme (light/dark/system) setting.
  themeId: string
  // menuBarInApp shows the in-app menu bar on macOS (Windows/Linux always show
  // it); menuBarNativeMinimal then reduces the native macOS menu to the app
  // menu. menuBarIcons shows icons next to the in-app bar's dropdown items.
  menuBarInApp: boolean
  menuBarNativeMinimal: boolean
  menuBarIcons: boolean
  // windowMaterial puts the window over a native material (Liquid Glass on
  // macOS, Mica on Windows) from the next launch on.
  windowMaterial: boolean
  // timeFormat picks the clock for rendered times: auto (locale), 12, or 24.
  timeFormat: string
  // reduceMotion disables ui transitions and animations.
  reduceMotion: boolean
  // handCursor shows the browser hand over clickable chrome instead of the
  // native arrow. Hyperlinks keep the hand regardless.
  handCursor: boolean
  // unreadBadge shows the unread count on the dock icon on macOS, or the unread
  // dot on the tray icon on Windows and Linux.
  unreadBadge: boolean
  // themeDarkStart/themeDarkEnd bound the dark window ("HH:MM") for the
  // schedule theme mode.
  themeDarkStart: string
  themeDarkEnd: string
  // bodyFont is the reader fallback font for mail bodies (a key from the
  // curated list in lib/fonts.ts).
  bodyFont: string
  // uiFont / monoFont override the interface and monospace font tokens (a
  // curated key or 'sys:<family>'; 'default' keeps the built-in fonts).
  uiFont: string
  monoFont: string
  // senderFonts lets a message use the font families it asks for. On by
  // default; off renders every message in bodyFont.
  senderFonts: boolean
  // notifyNewMail raises a native OS notification for new inbox mail. VIP
  // senders notify regardless of this (see stores/vip.ts).
  notifyNewMail: boolean
  // verboseSync shows which mailbox is currently syncing in the status line,
  // with the account and the server it is talking to.
  verboseSync: boolean
  // syncProgressBar puts a progress bar beside that line, counting message
  // bodies rather than mailboxes. On by default.
  syncProgressBar: boolean
  // statusClock shows the date and time in the status bar, statusUnread the
  // unread count of the unified inbox (#488).
  statusClock: boolean
  statusUnread: boolean
  // markReadOnOpen marks a message read when it is opened (#517).
  markReadOnOpen: boolean
  // openOnArrow opens the row the arrow keys move to, instead of waiting for Enter.
  openOnArrow: boolean
  // signatureAboveQuote puts a reply's footer above the quoted original (#512).
  signatureAboveQuote: boolean
  // closeAction is what the window's close button does: 'background' keeps
  // Pelton running and syncing with the window hidden, 'quit' exits.
  closeAction: CloseAction
  // startHidden keeps the window hidden at launch (#483).
  startHidden: boolean
  // syncMessageLimit caps how many of a folder's newest messages the first sync
  // fetches; older mail stays on the server until asked for. 0 means no limit.
  syncMessageLimit: number
  // syncAutoBackfill fetches the next batch of older mail automatically on
  // reaching the end of the list. Off puts it behind a button instead.
  syncAutoBackfill: boolean
  // what the sidebar selects on launch: 'view:<key>' for a unified view,
  // 'folder:<id>' for one account folder, or 'last' to restore the previous
  // session. A target that no longer exists falls back to the unified inbox.
  startupSelection: string
  // logToFile writes Pelton's own log to a rotating file in the data folder,
  // at logLevel. logMessageMetadata additionally lets subjects and senders into
  // it for debugging sync. crashLogs leaves a stack behind when the app
  // crashes. All off by default; nothing is ever uploaded.
  logToFile: boolean
  logLevel: LogLevel
  logMessageMetadata: boolean
  crashLogs: boolean
}

// how much detail the log records.
export type LogLevel = 'debug' | 'info' | 'warn' | 'error'

// LogStatus is what the settings ui shows about logging: where the files are,
// whether anything is being written, and whether the last run crashed.
export interface LogStatus {
  dir: string
  // writing is the live state, which is not the setting: --debug and
  // PELTON_DEBUG force logging on regardless of it, and forced says so.
  writing: boolean
  forced: boolean
  sizeBytes: number
  // crashName is empty when there is no crash the user has not seen yet.
  crashName: string
  crashTime: string
}

// what the window's close button does.
export type CloseAction = 'background' | 'quit'

// an installed custom theme, as shown in the settings gallery.
export interface ThemeInfo {
  id: string
  name: string
  author: string
  version: string
  description: string
  base: string
  hasCss: boolean
  // network references still present in the installed css (only non-empty
  // when the user chose Allow at import).
  remoteRefs: string[]
  // preview screenshot as a data uri, or ''.
  preview: string
  // set when the running app version is outside the range the theme declares
  // itself made for. informational only.
  compatWarning: string
  // a few of the theme's token colors for the gallery card, for themes
  // without a preview screenshot.
  swatches: string[]
}

// a palette-editor save: name, light/dark base and token overrides. id is
// set when editing an existing installed theme, empty when creating one.
export interface SaveThemeRequest {
  id: string
  name: string
  author: string
  version: string
  base: string
  tokens: Record<string, string>
  css: string
}

// an installed theme loaded back into theme-editor form. css is the raw
// source of the editor's own stylesheet, not the applied one.
export interface ThemeDraft {
  id: string
  name: string
  author: string
  version: string
  base: string
  tokens: Record<string, string>
  css: string
}

// everything needed to apply a custom theme to the document.
export interface ThemeApply {
  id: string
  base: string
  tokens: Record<string, string>
  css: string
  icons: Record<string, string>
}

// one stylesheet of a theme container, for the read-before-import viewer.
export interface ThemeCSSFile {
  path: string
  content: string
  remoteRefs: string[]
}

// the read-before-import view of a chosen .peltontheme file.
export interface ThemeImportPreview {
  canceled: boolean
  path: string
  info: ThemeInfo
  cssFiles: ThemeCSSFile[]
  tokenCount: number
  updatesExisting: boolean
  installedVersion: string
}

// a custom language file in the locales folder, as shown in the picker.
export interface UserLocale {
  id: string
  name: string
  author: string
  base: string
  // the number of strings the file provides; a low count marks a partial
  // override on top of its base language.
  count: number
}

// a custom language in apply form.
export interface UserLocaleApply {
  id: string
  name: string
  base: string
  strings: Record<string, string>
}

// a harvested contact for compose autocomplete and the settings manager.
export interface AddressBookEntry {
  email: string
  name: string
  useCount: number
  lastUsed: string
  createdAt: string
}

// an attachment's bytes for the in-app previewer. data is base64; tooLarge is
// set (with empty data) when the file exceeds the preview cap.
export interface AttachmentContent {
  filename: string
  contentType: string
  sizeBytes: number
  data: string
  tooLarge: boolean
}

/** The groups the attachments browser filters by. */
export type AttachmentKind = 'document' | 'image' | 'pdf' | 'spreadsheet' | 'archive' | 'media' | 'other'

/** One file in the attachments browser, with the message it came with. */
export interface AttachmentEntry {
  id: number
  messageId: number
  accountId: number
  folderId: number
  threadId: number
  filename: string
  contentType: string
  sizeBytes: number
  kind: AttachmentKind
  subject: string
  fromName: string
  fromAddress: string
  date: string
  /** False when the bytes are not stored here and come from the server on request. */
  downloaded: boolean
}

/** The attachments browser filters. Dates are YYYY-MM-DD; empty leaves a filter off. */
export interface AttachmentFilter {
  kinds: AttachmentKind[]
  accountId: number
  sender: string
  since: string
  until: string
  name: string
}

/** A request for one page of attachments. */
export interface AttachmentQuery extends AttachmentFilter {
  senderAddress: string
  limit: number
  offset: number
  withTotal: boolean
}

/** A page of attachments; total is -1 unless it was asked for. */
export interface AttachmentPage {
  entries: AttachmentEntry[]
  total: number
}

/** What the sender panel knows about one address, all from local mail. */
export interface SenderOverview {
  address: string
  count: number
  first: string
  last: string
  /** One message per conversation, newest first. */
  recent: MessageSummary[]
  files: AttachmentEntry[]
  filesTotal: number
  /** The address reads as a machine sender (noreply and the like). */
  automated: boolean
}

// the eight flag colors. index 0 means "no color"; 1..8 map to the palette in
// theme/flagcolors.ts and to imap $Label1..$Label8 when syncing is on.
export type FlagColor = 0 | 1 | 2 | 3 | 4 | 5 | 6 | 7 | 8

// swipe gesture actions for message rows.
export type SwipeAction = 'none' | 'delete' | 'read' | 'unread' | 'flag' | 'archive' | 'snooze'

// list row layouts, from most spacious to a single dense line.
export type RowTemplate = 'relaxed' | 'comfortable' | 'compact' | 'single'

// the six corners/edges a toast stack can anchor to.
export type ToastPosition =
  | 'top-left'
  | 'top-center'
  | 'top-right'
  | 'bottom-left'
  | 'bottom-center'
  | 'bottom-right'

export interface SettingResult {
  value: string
  found: boolean
}

// a reusable header/footer block. kind places it (top/bottom of a new message);
// format picks how the content is inserted into the compose body.
export interface Signature {
  id: number
  name: string
  kind: 'header' | 'footer'
  format: 'markdown' | 'html'
  content: string
}

// an account's default header/footer assignment. 0 means no default.
export interface AccountSignatures {
  headerId: number
  footerId: number
}

// the outbound proxy preference shown in settings. password is write-only: the
// backend never sends the stored secret back, only hasPassword so the field can
// show a placeholder.
export interface MCPConfig {
  enabled: boolean
  port: number
  token: string
  url: string
  running: boolean
}

// the VirusTotal integration's settings. the api key is write-only: the backend
// keeps it in the os keyring and only reports hasApiKey, so the settings field
// can show a filled state without the secret coming back to the ui.
// one imported OpenPGP key, as the encryption settings list shows it. It never
// carries key material: fingerprints and user ids are all the ui needs.
export interface PGPKey {
  // uppercase hex, unseparated.
  fingerprint: string
  name: string
  email: string
  emails: string[]
  // rfc 3339; expires is empty when the key never expires.
  created: string
  expires: string
  expired: boolean
  hasPrivate: boolean
  // locked means the private half is passphrase protected.
  locked: boolean
  // unlocked means this session already holds the passphrase.
  unlocked: boolean
  // remembered means the passphrase is in the os keyring.
  remembered: boolean
  algorithm: string
  bits: number
}

export interface VirusTotalConfig {
  enabled: boolean
  hasApiKey: boolean
  autoScanLinks: boolean
  autoScanAttachments: boolean
}

// one scan result. status is 'clean', 'flagged' or 'unknown'; error is set
// instead when that one lookup failed, and carries either a code the ui
// localizes ('rate_limited', 'unauthorized') or a message to show verbatim.
export interface Verdict {
  status: 'clean' | 'flagged' | 'unknown'
  malicious: number
  suspicious: number
  total: number
  permalink: string
  error: string
}

// a verdict paired with the link it belongs to.
export interface LinkVerdict {
  url: string
  verdict: Verdict
}

// a verdict paired with the attachment it belongs to.
export interface AttachmentVerdict {
  attachmentId: number
  filename: string
  verdict: Verdict
}

// the result of scanning a whole message. either list is empty when that target
// type was not requested.
export interface MessageScan {
  links: LinkVerdict[]
  attachments: AttachmentVerdict[]
}

export interface ProxyConfig {
  mode: string
  scheme: string
  host: string
  port: number
  username: string
  password: string
  hasPassword: boolean
}

// autodiscovery result for the add-mailbox wizard.
export interface Discovered {
  imapHost: string
  imapPort: number
  smtpHost: string
  smtpPort: number
  // the security the source stated, empty when it said nothing usable.
  imapTls: string
  smtpTls: string
  oauth: boolean
  // the oauth provider key to sign in with when the servers belong to one
  // pelton supports ('google'), empty otherwise.
  oauthProvider: string
  source: string
}

// the metadata the wizard collects to create an account. password is set for
// password auth; provider + clientId are set for oauth (per-user PKCE).
export interface AddAccountRequest {
  email: string
  // the From name recipients see, and the local-only label for the sidebar.
  displayName: string
  localLabel: string
  useLocalLabel: boolean
  // the login name when it differs from the email; empty logs in with email.
  username: string
  imapHost: string
  imapPort: number
  smtpHost: string
  smtpPort: number
  imapTls: TLSMode
  smtpTls: TLSMode
  password: string
  provider: string
  clientId: string
  // optional oauth client secret for confidential-client app registrations
  // (some Microsoft Entra setups). empty keeps the default PKCE public flow.
  clientSecret: string
  // what the mailbox trusts beyond the system roots: fingerprints accepted in
  // the connection test, and a CA file's PEM text.
  trustedCerts: string[]
  caPem: string
  proxy: AccountProxy
}

export interface TestConnectionRequest {
  email: string
  // the login name when it differs from the email; empty logs in with email.
  username: string
  imapHost: string
  imapPort: number
  // tested with the same security the account will use, not a guess from the port.
  imapTls: TLSMode
  password: string
  // the smtp server is only checked for the certificate it presents.
  smtpHost: string
  smtpPort: number
  smtpTls: TLSMode
  trustedCerts: string[]
  caPem: string
  // the route the new mailbox will take, so one only reachable through its
  // own proxy can pass the test.
  proxy: AccountProxy
}

// SyncFailureReason is the coarse class of a failed sync, which the ui turns
// into a sentence. Anything unrecognized reads as 'other'.
export type SyncFailureReason = 'auth' | 'network' | 'credentials' | 'certificate' | 'other'

// UntrustedCert is a server certificate that failed verification, laid out for
// the user to review before trusting it. fingerprint is what trusting it sends
// back; display is the same for reading. Dates are rfc3339.
export interface UntrustedCert {
  server: 'imap' | 'smtp'
  host: string
  port: number
  fingerprint: string
  display: string
  subject: string
  issuer: string
  notBefore: string
  notAfter: string
  names: string[]
  selfSigned: boolean
  reason: string
}

// ConnectionTest is a connection test that reached the servers: untrusted is
// empty when it logged in.
export interface ConnectionTest {
  untrusted: UntrustedCert[]
}

// CAFile is a picked CA certificate file. pem is empty when the picker was
// cancelled.
export interface CAFile {
  pem: string
  subjects: string[]
}

// AccountSyncState is how one account's last sync went. lastOk survives a later
// failure, so "broken since" is answerable. Both times are rfc3339, empty for
// never. A non-empty failedAt means the account is currently failing.
export interface AccountSyncState {
  accountId: number
  email: string
  lastOk: string
  failedAt: string
  reason: SyncFailureReason
  detail: string
}

// PasswordCheck is what the server said when a password was tried against an
// existing account. rejected means the login was refused; an empty error with
// ok false means the server could not be reached, which says nothing about the
// password.
export interface PasswordCheck {
  ok: boolean
  rejected: boolean
  error: string
}

// folder roles mirror the backend's folderRole classification.
export type FolderRole =
  | 'inbox'
  | 'sent'
  | 'drafts'
  | 'trash'
  | 'junk'
  | 'archive'
  | 'normal'

// unified view keys mirror the backend view constants.
export type ViewKey = 'inbox' | 'unread' | 'flagged' | 'sent' | 'drafts' | 'archive' | 'junk' | 'trash'

// pgp status mirrors mailview.PGPStatus.
export type PGPStatus = 'none' | 'signed' | 'encrypted'

// auth status placeholder. only "unavailable" exists until the backend parses
// Authentication-Results headers (documented follow-up).
export type AuthStatus = 'unavailable'

// editor modes for the compose pane.
export type EditorMode = 'plaintext' | 'markdown' | 'wysiwyg'

// theme and density preference values.
export type ThemePref = 'system' | 'light' | 'dark' | 'schedule'
export type DensityPref = 'compact' | 'medium' | 'luxe'
/** The interface design: the redesign or the shell from before it. */
export type DesignPref = 'new' | 'classic'
/** How the mail panes are arranged in the new design. */
export type LayoutMode = 'columns' | 'stacked'
/** Where the Mail/Contacts/Calendar switcher sits in the new design. */
export type NavStyle = 'rail' | 'footer' | 'tabs' | 'hidden'
/** The window edge the rail sits on. */
export type RailSide = 'left' | 'right'
/** How the panes are drawn in the new design. */
export type PaneStyle = 'hybrid' | 'flat' | 'soft'
/** Where the sender picture goes in a list row and the reading header. */
export type AvatarPlacement = 'column' | 'inline' | 'hidden'
/** How the contacts space shows its contacts: a list with a detail pane, or a card grid. */
export type ContactsView = 'panes' | 'grid'
/** Where compose opens in the new design: its own window, docked, or a tab. */
export type ComposeOpenIn = 'window' | 'docked' | 'tab'
/** How wide the reading column runs in the new design. */
export type ReaderWidth = 'narrow' | 'medium' | 'full'
/** One of the top-level areas of the app. */
export type Space = 'mail' | 'contacts' | 'calendar'
export type SelectAllScope = 'offer' | 'all' | 'loaded'

// SearchSort is an order search results can actually come back in. The backend
// takes one of these; 'auto' is never sent, it is resolved first.
export type SearchSort = 'relevance' | 'newest' | 'oldest' | 'subjectAsc' | 'subjectDesc'

// SearchSortPref is what the user picked for a kind of search, which may be
// 'auto': let the query decide.
export type SearchSortPref = SearchSort | 'auto'

// SearchKind is the shape a query has, and the unit the sort choice is
// remembered by (#404). A sort picked while looking through a date range is
// remembered for date ranges; it does not follow the user onto a text search,
// where a different order is the right one.
//
//   text     words were typed, with or without chips
//   dated    no words, but a before:/after: window is set
//   filtered no words and no dates, only from:/to:/subject:/has: chips
export type SearchKind = 'text' | 'dated' | 'filtered'

// Selection identifies what the message list is currently showing: a unified
// cross-account view, a single account folder, or a user-defined saved View
// (preset search).
export type Selection =
  | { kind: 'view'; view: ViewKey; label: string }
  | { kind: 'folder'; folderId: number; accountId: number; label: string }
  | { kind: 'savedView'; viewId: number; label: string }

// View is a user-defined saved search ("preset search"), mirroring the backend
// ViewDTO. accountId 0 means all accounts; withinDays 0 means no date bound. The
// count fields are the eager-run results shown as a sidebar badge.
export interface View {
  id: number
  name: string
  icon: string
  color: string
  queryText: string
  queryFrom: string[]
  queryTo: string[]
  querySubject: string
  useRegex: boolean
  withinDays: number
  unreadOnly: boolean
  flaggedOnly: boolean
  hasAttachment: boolean
  accountId: number
  position: number
  unreadCount: number
  totalCount: number
}

// how the Views group is surfaced in the ui. 'hidden' keeps the feature off,
// 'sidebar' shows it as a group in the mailbox sidebar, 'tab' shows it in a
// separate Views tab/rail.
export type ViewsPlacement = 'hidden' | 'sidebar' | 'tab'

// DevLogLine is one line in the developer activity overlay. seq counts every
// line the session produced, so a gap in it means lines were dropped.
export interface DevLogLine {
  seq: number
  text: string
}

// DevActivity is a page of the activity overlay: the lines newer than what was
// asked for, the sequence to ask for next, and the current log threshold.
export interface DevActivity {
  lines: DevLogLine[]
  next: number
  level: string
}

// DevProcessStats is what the process overlay shows: the Go runtime's own
// numbers and the app's footprint on disk, all in bytes.
export interface DevProcessStats {
  goroutines: number
  heapBytes: number
  heapSysBytes: number
  gcRuns: number
  databaseBytes: number
  attachmentsBytes: number
  dataDirBytes: number
  uptimeSeconds: number
}

// Profile is one workspace within the install (#270): which accounts it shows,
// and whether its settings, signatures and saved views are its own or the main
// profile's. Mail belongs to the install, so an account visible in two profiles
// is cached and synced once.
export interface Profile {
  id: number
  name: string
  // an emoji or short glyph shown next to the name, so the current profile is
  // recognisable at a glance.
  icon: string
  // the profile the install started as: renamable, never deletable.
  main: boolean
  // the profile the app is currently in.
  active: boolean
  // live links to the main profile. A copied area reads false: it was a
  // one-time duplicate, not a link.
  shareSettings: boolean
  shareSignatures: boolean
  shareViews: boolean
  // the sidebar layout: which folders are pinned, and the order of folders and
  // account sections.
  shareLayout: boolean
  accountIds: number[]
}

// ProfileStart is where one area of a new profile comes from: a live link to
// the main profile, a one-time copy of it, or nothing at all.
export type ProfileStart = 'share' | 'copy' | 'fresh'

// ProfileDraft is the create/edit form's shape.
export interface ProfileDraft {
  id: number
  name: string
  icon: string
  accountIds: number[]
  startSettings: ProfileStart
  startSignatures: ProfileStart
  startViews: ProfileStart
  startLayout: ProfileStart
}

// AddressBook is one configured CardDAV address book (#168).
export interface AddressBook {
  id: number
  // the mail account it was discovered from, 0 for one added by hand.
  accountId: number
  name: string
  url: string
  collectionPath: string
  username: string
  // the server refuses writes, so the editor is read-only for its contacts.
  readOnly: boolean
  // rfc3339, empty when it has never synced.
  lastSync: string
  // the last sync failure in the server's words, empty once one succeeds.
  lastError: string
  contactCount: number
  // false when the keyring has no password for it, so it cannot sync.
  hasPassword: boolean
}

// DiscoveredBook is one address book a server offered, before it is added.
export interface DiscoveredBook {
  name: string
  url: string
  collectionPath: string
  // already configured here, so the ui offers it greyed rather than twice.
  exists: boolean
}

// ContactValue is one address or phone number with its vCard label.
export interface ContactValue {
  value: string
  label: string
}

// Contact is one address book entry.
export interface Contact {
  id: number
  bookId: number
  bookName: string
  uid: string
  fullName: string
  organization: string
  title: string
  note: string
  emails: ContactValue[]
  phones: ContactValue[]
  readOnly: boolean
  updated: string
  // vCard properties Pelton has no field for. They are kept on every save;
  // the editor lists them so it is clear the card holds more than the form.
  extra: string[]
}

// ContactConflict is the answer to a save or delete the server refused because
// the contact changed elsewhere. Both versions come back and the user picks.
export interface ContactConflict {
  conflict: boolean
  server: Contact
  mine: Contact
  // the stored contact when there was no conflict.
  saved: Contact
}

// ContactDraft is the editor's shape. id 0 creates, and then bookId says which
// address book it goes in.
export interface ContactDraft {
  id: number
  bookId: number
  fullName: string
  organization: string
  title: string
  note: string
  emails: ContactValue[]
  phones: ContactValue[]
  // set only by "keep mine" on the conflict dialog: write over whatever the
  // server now holds.
  force: boolean
}

// AddressBookDraft is the add/edit address book form.
export interface AddressBookDraft {
  id: number
  accountId: number
  name: string
  url: string
  collectionPath: string
  username: string
  password: string
}

/** CalendarAccount is one CalDAV login. */
export interface CalendarAccount {
  id: number
  // the mail account it was discovered from, 0 for none.
  accountId: number
  // the address book whose login it shares, 0 for none.
  addressBookId: number
  name: string
  url: string
  username: string
  // rfc3339, empty when it has never synced.
  lastSync: string
  // the last sync failure in the server's words, empty once one succeeds.
  lastError: string
  hasPassword: boolean
  calendarCount: number
}

/**
 * CalendarAccountDraft is the add/edit calendar account form, and what
 * discovery starts from. With addressBookId set, an empty url, username or
 * password is taken from that address book; with accountId and no url,
 * .well-known/caldav on the mailbox's domain is tried.
 */
export interface CalendarAccountDraft {
  id: number
  accountId: number
  addressBookId: number
  name: string
  url: string
  username: string
  // empty on update leaves the stored password alone.
  password: string
}

/** DiscoveredCalendar is one calendar a login can see. */
export interface DiscoveredCalendar {
  name: string
  path: string
  color: string
  readOnly: boolean
}

/** CalendarDiscovery is the endpoint discovery resolved and its calendars. */
export interface CalendarDiscovery {
  // the url to add the account with.
  url: string
  username: string
  calendars: DiscoveredCalendar[]
  // an account for this url and login already exists.
  exists: boolean
}

/** Calendar is one calendar, synced or local. */
export interface Calendar {
  id: number
  // 0 for a local calendar.
  accountId: number
  accountName: string
  // empty for the local calendar the app starts with; the ui labels it.
  name: string
  description: string
  // #rrggbb, or empty for the ui to pick.
  color: string
  local: boolean
  readOnly: boolean
  visible: boolean
  lastSync: string
  lastError: string
  // where new events go unless another calendar is chosen.
  default: boolean
}

/** CalendarSettings are the calendar preferences. */
export interface CalendarSettings {
  // background sync interval, 0 turns it off.
  syncIntervalMinutes: number
  remindersEnabled: boolean
  // the reminder a new event gets, -1 for none.
  defaultReminderMinutes: number
  // where new events go, 0 for the first writable calendar.
  defaultCalendarId: number
  // the view the calendar opens in.
  defaultView: CalendarView
  // first day of the week, 0 for Sunday through 6, -1 to follow the locale.
  weekStart: number
  // working hours as minutes after midnight.
  workDayStart: number
  workDayEnd: number
  // off, the zone's name over the hours, or a second column of hours.
  timeZoneDisplay: TimeZoneDisplay
  // IANA zone of the second column, empty for none.
  secondaryTimeZone: string
}

/** The views of the calendar screen. */
export type CalendarView = 'month' | 'week' | 'day' | 'agenda'

/** How the time grid names time zones. */
export type TimeZoneDisplay = 'off' | 'label' | 'dual'

/** The scope an edit or delete of one occurrence of a series applies to. */
export type EventScope = 'this' | 'future' | 'all'

/**
 * Occurrence is one already expanded occurrence in a window. start and end are
 * rfc3339, or yyyy-mm-dd dates for an all-day occurrence with end exclusive.
 */
export interface Occurrence {
  eventId: number
  calendarId: number
  calendarColor: string
  uid: string
  summary: string
  location: string
  start: string
  end: string
  allDay: boolean
  recurring: boolean
  // names the occurrence for getEvent, updateEvent and deleteEvent: the
  // rfc3339 utc instant it originally started at. Empty when not recurring.
  recurrenceId: string
  // this occurrence was edited on its own.
  override: boolean
  status: string
  transparency: string
  readOnly: boolean
  hasReminders: boolean
  // a change made here has not reached the server yet.
  pending: boolean
  conflict: boolean
}

/**
 * Reminder is one VALARM. minutesBefore counts back from the start (from the
 * end with relatedEnd); negative means after. absolute, an rfc3339 time,
 * replaces both when set.
 */
export interface Reminder {
  minutesBefore: number
  relatedEnd: boolean
  absolute: string
  // DISPLAY, AUDIO or EMAIL; empty means DISPLAY.
  action: string
}

/** Attendee is an attendee or the organizer of an event. */
export interface Attendee {
  email: string
  name: string
  // REQ-PARTICIPANT, OPT-PARTICIPANT, NON-PARTICIPANT or CHAIR.
  role: string
  // NEEDS-ACTION, ACCEPTED, TENTATIVE, DECLINED or DELEGATED.
  partStat: string
  rsvp: boolean
}

/** CalendarEvent is one event, or one occurrence of a series, for the editor. */
export interface CalendarEvent {
  id: number
  calendarId: number
  calendarName: string
  calendarColor: string
  uid: string
  summary: string
  description: string
  location: string
  url: string
  status: string
  transparency: string
  categories: string[]
  start: string
  end: string
  allDay: boolean
  // IANA zone the event was written in, empty for utc or floating.
  timeZone: string
  // the series' RRULE value, empty for none.
  rrule: string
  recurring: boolean
  // rfc3339 starts of occurrences removed from the series.
  exDates: string[]
  // set when this is one occurrence of a series.
  recurrenceId: string
  reminders: Reminder[]
  // email is empty when the event has no organizer.
  organizer: Attendee
  attendees: Attendee[]
  readOnly: boolean
  pending: boolean
  conflict: boolean
  syncError: string
  // iCalendar properties kept on every save that the editor has no field for.
  extra: string[]
}

/**
 * EventDraft is the editor's event. id 0 creates in calendarId. For an
 * occurrence of a series, recurrenceId names it and scope says what changes.
 * The draft is the full state: an empty rrule removes the recurrence.
 */
export interface EventDraft {
  id: number
  calendarId: number
  summary: string
  description: string
  location: string
  url: string
  status: string
  transparency: string
  categories: string[]
  start: string
  end: string
  allDay: boolean
  timeZone: string
  rrule: string
  reminders: Reminder[]
  organizer: Attendee
  attendees: Attendee[]
  scope: EventScope | ''
  recurrenceId: string
  // set only by "keep mine" on a conflict: write over the server's copy.
  force: boolean
}

/** EventSaveResult is the outcome of a save or delete. */
export interface EventSaveResult {
  event: CalendarEvent
  // the server was unreachable; the change goes out with the next sync.
  queued: boolean
  // the server's copy moved on; server holds it and resolveEventConflict
  // settles which one stays.
  conflict: boolean
  server: CalendarEvent
}

/** Invite is the invitation a message carries. */
export interface Invite {
  // false for a message without calendar data; nothing else is set then.
  found: boolean
  // REQUEST, CANCEL, REPLY or PUBLISH, or empty.
  method: string
  uid: string
  sequence: number
  summary: string
  description: string
  location: string
  start: string
  end: string
  allDay: boolean
  timeZone: string
  recurring: boolean
  rrule: string
  recurrenceId: string
  organizer: Attendee
  attendees: Attendee[]
  // the mailbox address the invitation is read for and its answer so far.
  myEmail: string
  myPartStat: string
  // the copy an earlier answer stored, 0 for none.
  existingEventId: number
  existingCalendarId: number
  // the stored copy is newer than this invitation.
  outdated: boolean
}

/** InviteResponse is an answer to an invitation. */
export type InviteResponse = 'accepted' | 'tentative' | 'declined'

/** InviteAnswer is the request to answer an invitation. */
export interface InviteAnswer {
  messageId: number
  // where an accepted event goes when no copy is stored, 0 for the default.
  calendarId: number
  response: InviteResponse
  sendReply: boolean
}

/** InviteAnswerResult says what answering or applying an invitation did. */
export interface InviteAnswerResult {
  // 0 when the answer removed the event.
  eventId: number
  // 0 when no reply was queued.
  outboxId: number
}

/** ICSImportResult counts what an .ics import did. */
export interface ICSImportResult {
  imported: number
  updated: number
  skipped: number
}

/** ICSFile is a calendar file read for import, iCalendar or JSCalendar. */
export interface ICSFile {
  name: string
  data: string
  /** How many events an import would take, and the titles of the first few. */
  events: number
  summaries: string[]
  /** Set instead of the rest when the file could not be read. */
  error: string
}

/** EventBundle is the events a pasted link or JSCalendar text carries, as drafts with no calendar. */
export interface EventBundle {
  events: EventDraft[]
  skipped: number
}

/** MailEventSource is what the calendar reads from a mail dropped on it. */
export interface MailEventSource {
  subject: string
  text: string
  /** When the mail was sent, rfc3339; empty when unknown. */
  date: string
  fromName: string
  fromAddress: string
  /** A mid: link back to the mail, empty when it has no Message-ID. */
  link: string
}

/** MessageRef says where a stored message lives. */
export interface MessageRef {
  found: boolean
  id: number
  accountId: number
  folderId: number
}
