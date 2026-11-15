// shortcuts.ts defines the app-wide keyboard shortcuts and the helpers to parse,
// match and record key combos. combos use "mod" for the platform primary
// modifier (cmd on macos, ctrl elsewhere), plus optional "alt" and "shift", then
// a key, e.g. "mod+n", "mod+shift+a", "alt+s". the user can rebind any of these
// in settings; the live bindings live in stores/shortcuts.ts, while this module
// stays a pure, store-free library so it can be unit-reasoned and imported
// anywhere.

import { isMac } from './i18n'

// ShortcutAction is the set of app-wide actions a shortcut can trigger. The
// second group are message-level actions that act on the open message, or on
// the selection when there is one. The ones reached daily ship on the single
// letters webmail uses; the rest ship unbound (empty default combo) for the
// user to assign.
export type ShortcutAction =
  | 'compose'
  // sends the message being written. Only a compose pane answers to it, and
  // only while focus is inside that pane (#480).
  | 'send'
  | 'preferences'
  | 'sync'
  | 'search'
  | 'command-palette'
  | 'add-mailbox'
  | 'export-pdf'
  | 'toggle-fullscreen'
  | 'close-window'
  | 'quit'
  | 'reply'
  | 'reply-all'
  | 'forward'
  | 'mark-read'
  | 'mark-unread'
  | 'flag'
  | 'snooze'
  | 'download-offline'
  | 'delete-message'
  | 'archive'
  // archives the open or selected conversations whole and moves the reader on
  // to the next one.
  | 'done'
  // marks the message as spam, or rescues it when it is already in the spam
  // folder: one key, since only one of the two ever applies.
  | 'toggle-spam'
  | 'unsubscribe'
  | 'new-view'
  | 'next-view'
  | 'prev-view'
  // actions the command palette (#134) made first-class. Several need a target
  // the user has to choose; those open the palette on their own picker step
  // rather than acting straight away, so they bind to a key like any other.
  | 'mark-vip'
  | 'move-to'
  | 'flag-color'
  | 'remove-offline'
  | 'empty-trash'
  | 'new-folder'
  | 'rename-folder'
  | 'delete-folder'
  | 'toggle-pin-folder'
  | 'apply-theme'
  | 'edit-view'
  // reading-pane tabs (#197). Unbound by default: the feature is invisible
  // until you use it, and a default key would not be.
  | 'open-in-tab'
  | 'close-tab'
  // profiles (#270). Switching opens the picker; next and previous move along
  // the list without one.
  | 'switch-profile'
  | 'next-profile'
  | 'prev-profile'
  // moving focus between the sidebar, the list and the reading pane (#495).
  | 'next-pane'
  | 'prev-pane'
  | 'focus-sidebar'
  | 'focus-list'
  | 'focus-reader'
  // opens the calendar's quick add, from the calendar or any other space.
  | 'calendar-quick-add'
  // switching between the spaces of the new design.
  | 'go-mail'
  | 'go-calendar'
  | 'go-contacts'
  // the attachments browser and the reader's sender panel, new design only.
  | 'open-attachments'
  | 'toggle-sender-panel'
  // shows or hides the My Day panel beside the mail.
  | 'toggle-my-day'
  // shows the Tasks space.
  | 'go-tasks'
  // makes a task from the open message, or from each selected one.
  | 'create-task'
  // the split inbox tabs, only while the message list has focus.
  | 'next-split'
  | 'prev-split'
  // pin, mute and a private note on the open message or its conversation.
  | 'toggle-pin'
  | 'toggle-mute'
  | 'edit-note'
  // the newsletter manager on the mail surface, and the insights page.
  | 'open-newsletters'
  | 'open-insights'
  // the shortcut sheet and focus mode, new design only.
  | 'shortcut-sheet'
  | 'focus-mode'
  // composer actions of the new design. They act on the composer that last
  // had focus, so they carry a modifier and work while typing.
  | ComposeAction

/** The shortcut actions that act on a composer rather than on mail. */
export type ComposeAction =
  | 'compose-follow-up'
  | 'compose-insert-snippet'
  | 'compose-intro-bcc'
  | 'compose-toggle-reply-all'
  | 'compose-cc-sender'
  // move the recipient chip that has focus to the next field down (To, Cc,
  // Bcc) or up.
  | 'compose-recipient-down'
  | 'compose-recipient-up'

/** Every composer action, in the order Settings and the palette list them. */
export const composeActions: readonly ComposeAction[] = [
  'compose-follow-up',
  'compose-insert-snippet',
  'compose-intro-bcc',
  'compose-toggle-reply-all',
  'compose-cc-sender',
  'compose-recipient-down',
  'compose-recipient-up',
]

/** The composer actions that act on a focused recipient chip. */
export const chipActions: readonly ComposeAction[] = ['compose-recipient-down', 'compose-recipient-up']

/** isComposeAction reports whether an action belongs to a composer. */
export function isComposeAction(action: string): action is ComposeAction {
  return (composeActions as readonly string[]).includes(action)
}

// Shortcut pairs an action with its default combo and the label key for display.
export interface Shortcut {
  action: ShortcutAction
  combo: string
  labelKey: string
  /** The action exists in the new design only; classic does not list it. */
  newDesignOnly?: boolean
  /**
   * A second key that fires the same action while the binding is untouched.
   * It exists for actions where two platforms disagree about which key is
   * obvious and neither key does anything else: delete is Backspace on macOS
   * and Delete elsewhere, and the key you reach for should work whichever
   * machine you learned it on. Rebinding the action replaces the pair, since
   * at that point you have said which key you want.
   */
  alt?: string
}

// the default registry, also used to seed the editable bindings and render the
// shortcuts list in settings.
export const shortcuts: Shortcut[] = [
  { action: 'compose', combo: 'mod+n', labelKey: 'shortcut.compose' },
  // the key every desktop mail client sends with, so it is bound by default.
  { action: 'send', combo: 'mod+enter', labelKey: 'action.send' },
  { action: 'preferences', combo: 'mod+,', labelKey: 'shortcut.preferences' },
  { action: 'sync', combo: 'mod+r', labelKey: 'shortcut.sync' },
  { action: 'add-mailbox', combo: 'mod+m', labelKey: 'shortcut.addMailbox' },
  { action: 'search', combo: 'mod+f', labelKey: 'shortcut.search' },
  { action: 'command-palette', combo: 'mod+k', labelKey: 'shortcut.commandPalette' },
  { action: 'export-pdf', combo: 'mod+p', labelKey: 'shortcut.exportPdf' },
  // on macOS the native menu owns fullscreen (Cmd+Ctrl+F) and quit (Cmd+Q);
  // elsewhere the in-app menu bar relies on these frontend bindings.
  { action: 'toggle-fullscreen', combo: isMac ? '' : 'f11', labelKey: 'shortcut.toggleFullscreen' },
  { action: 'quit', combo: isMac ? '' : 'mod+q', labelKey: 'shortcut.quit' },
  // bound on every platform, macOS included: the native File menu accelerator
  // consumes Cmd+W before the webview sees it, so this never double-fires, and
  // it keeps working in the reduced-native-menu mode where File is dropped.
  { action: 'close-window', combo: 'mod+w', labelKey: 'shortcut.closeWindow' },
  // message-level actions. The daily ones take the single letters webmail has
  // used for two decades, so the keys someone already has in their fingers do
  // what they expect here too. A modifier-less binding never fires while the
  // event comes from a text field, and vim mode reads its own keys first, so
  // these collide with neither typing nor h/j/k/l navigation.
  { action: 'reply', combo: 'r', labelKey: 'shortcut.reply' },
  { action: 'reply-all', combo: 'a', labelKey: 'shortcut.replyAll' },
  { action: 'forward', combo: 'f', labelKey: 'shortcut.forward' },
  // read stays unbound: marking something read by hand is rare next to the
  // other direction, and there is no letter for it anyone would guess.
  { action: 'mark-read', combo: '', labelKey: 'shortcut.markRead' },
  { action: 'mark-unread', combo: 'u', labelKey: 'shortcut.markUnread' },
  { action: 'flag', combo: 's', labelKey: 'shortcut.flag' },
  { action: 'snooze', combo: 'z', labelKey: 'shortcut.snooze' },
  { action: 'download-offline', combo: '', labelKey: 'shortcut.downloadOffline' },
  // bound by default, unlike the rest of the message actions: every mail client
  // deletes on this key, neither key types anything in a list, and delete means
  // move to trash with one undo behind it (#329).
  { action: 'delete-message', combo: 'backspace', alt: 'delete', labelKey: 'shortcut.deleteMessage' },
  { action: 'archive', combo: 'e', labelKey: 'shortcut.archive' },
  { action: 'done', combo: 'd', labelKey: 'shortcut.done' },
  { action: 'toggle-spam', combo: '', labelKey: 'shortcut.toggleSpam' },
  { action: 'unsubscribe', combo: '', labelKey: 'shortcut.unsubscribe' },
  // saved views (preset searches), unbound by default so the user opts in.
  { action: 'new-view', combo: '', labelKey: 'shortcut.newView' },
  { action: 'next-view', combo: '', labelKey: 'shortcut.nextView' },
  { action: 'prev-view', combo: '', labelKey: 'shortcut.prevView' },
  // palette actions, unbound by default. Move-to is the exception: it is a
  // message action reached as often as archive, so it takes its letter too.
  { action: 'edit-view', combo: '', labelKey: 'palette.action.editView' },
  { action: 'mark-vip', combo: '', labelKey: 'palette.action.markVip' },
  { action: 'move-to', combo: 'm', labelKey: 'messageList.menu.moveTo' },
  { action: 'flag-color', combo: '', labelKey: 'palette.action.flagColor' },
  { action: 'remove-offline', combo: '', labelKey: 'messageList.menu.removeOffline' },
  { action: 'new-folder', combo: '', labelKey: 'palette.action.newFolder' },
  { action: 'rename-folder', combo: '', labelKey: 'palette.action.renameFolder' },
  { action: 'delete-folder', combo: '', labelKey: 'palette.action.deleteFolder' },
  { action: 'toggle-pin-folder', combo: '', labelKey: 'palette.action.pinFolder' },
  { action: 'empty-trash', combo: '', labelKey: 'folders.emptyTrash' },
  { action: 'apply-theme', combo: '', labelKey: 'palette.action.applyTheme' },
  { action: 'open-in-tab', combo: '', labelKey: 'messageList.menu.openInTab' },
  { action: 'close-tab', combo: '', labelKey: 'tabs.close' },
  { action: 'switch-profile', combo: '', labelKey: 'profiles.switch' },
  { action: 'next-profile', combo: '', labelKey: 'profiles.next' },
  { action: 'prev-profile', combo: '', labelKey: 'profiles.previous' },
  // F6 is the key desktop apps cycle panes with; Tab stays with the controls.
  { action: 'next-pane', combo: 'f6', labelKey: 'shortcut.nextPane' },
  { action: 'prev-pane', combo: 'shift+f6', labelKey: 'shortcut.prevPane' },
  { action: 'focus-sidebar', combo: '', labelKey: 'shortcut.focusSidebar' },
  { action: 'focus-list', combo: '', labelKey: 'shortcut.focusList' },
  { action: 'focus-reader', combo: '', labelKey: 'shortcut.focusReader' },
  // q is the key other calendars open quick add with, and nothing else here
  // uses it; like every bare letter it never fires from inside a text field.
  { action: 'calendar-quick-add', combo: 'q', labelKey: 'shortcut.calendarQuickAdd' },
  // letters rather than digits: mod+1 to 9 pick reading-pane tabs, and a digit
  // with Shift or Alt types a symbol that would not match.
  { action: 'go-mail', combo: 'mod+shift+m', labelKey: 'shortcut.goMail' },
  { action: 'go-calendar', combo: 'mod+shift+c', labelKey: 'shortcut.goCalendar' },
  { action: 'go-contacts', combo: 'mod+shift+a', labelKey: 'shortcut.goContacts' },
  // F for files. The panel takes a plain letter like the other reader keys:
  // mod+shift+i opens the web inspector in development builds.
  { action: 'open-attachments', combo: 'mod+shift+f', labelKey: 'shortcut.openAttachments' },
  { action: 'toggle-sender-panel', combo: 'i', labelKey: 'shortcut.toggleSenderPanel' },
  // y for "your day"; the letters near it are taken by the spaces and tasks.
  { action: 'toggle-my-day', combo: 'mod+shift+y', labelKey: 'shortcut.toggleMyDay' },
  // the key Thunderbird shows its task list with.
  { action: 'go-tasks', combo: 'mod+shift+d', labelKey: 'shortcut.goTasks' },
  // t for task, free among the message letters.
  { action: 'create-task', combo: 't', labelKey: 'shortcut.createTask' },
  // Tab only moves between splits while the list has focus; everywhere else it
  // keeps moving focus between controls.
  { action: 'next-split', combo: 'tab', labelKey: 'shortcut.nextSplit' },
  { action: 'prev-split', combo: 'shift+tab', labelKey: 'shortcut.prevSplit' },
  { action: 'toggle-pin', combo: 'shift+p', labelKey: 'shortcut.togglePin' },
  { action: 'toggle-mute', combo: 'shift+m', labelKey: 'shortcut.toggleMute' },
  { action: 'edit-note', combo: 'shift+n', labelKey: 'shortcut.editNote' },
  { action: 'open-newsletters', combo: 'mod+shift+l', labelKey: 'shortcut.openNewsletters' },
  { action: 'open-insights', combo: '', labelKey: 'shortcut.openInsights' },
  // "?" is shifted on every common layout, so the key arrives with shift held.
  { action: 'shortcut-sheet', combo: 'shift+?', labelKey: 'shortcut.shortcutSheet', newDesignOnly: true },
  { action: 'focus-mode', combo: 'mod+shift+h', labelKey: 'shortcut.focusMode', newDesignOnly: true },
  { action: 'compose-follow-up', combo: 'mod+shift+u', labelKey: 'followUp.menu' },
  { action: 'compose-insert-snippet', combo: 'mod+shift+i', labelKey: 'snippets.insert' },
  { action: 'compose-intro-bcc', combo: 'mod+shift+b', labelKey: 'recipients.introBcc' },
  { action: 'compose-toggle-reply-all', combo: 'mod+shift+e', labelKey: 'recipients.toggleReplyAll' },
  { action: 'compose-cc-sender', combo: 'mod+shift+o', labelKey: 'recipients.ccSender' },
  // alt with an arrow, since a focused chip has no text to move through.
  { action: 'compose-recipient-down', combo: 'alt+arrowdown', labelKey: 'recipients.moveDown' },
  { action: 'compose-recipient-up', combo: 'alt+arrowup', labelKey: 'recipients.moveUp' },
]

// ParsedCombo is a combo broken into its modifier flags and final key.
export interface ParsedCombo {
  mod: boolean
  alt: boolean
  shift: boolean
  key: string
}

// parseCombo splits a combo string into modifier flags and the final key.
export function parseCombo(combo: string): ParsedCombo {
  const parts = combo.toLowerCase().split('+')
  const key = parts[parts.length - 1] ?? ''
  return {
    mod: parts.includes('mod'),
    alt: parts.includes('alt'),
    shift: parts.includes('shift'),
    key,
  }
}

// isModifierKey reports whether a key event is a bare modifier press (so the
// recorder waits for a real key).
function isModifierKey(key: string): boolean {
  return key === 'Shift' || key === 'Control' || key === 'Alt' || key === 'Meta'
}

// normalizeKey maps a few keys to stable combo tokens.
function normalizeKey(key: string): string {
  if (key === ' ') {
    return 'space'
  }
  return key.toLowerCase()
}

// eventToCombo builds a combo string from a keydown event, or null when only a
// modifier is held. the primary modifier is recorded as "mod" per platform.
export function eventToCombo(event: KeyboardEvent): string | null {
  if (isModifierKey(event.key)) {
    return null
  }
  const parts: string[] = []
  const mod = isMac ? event.metaKey : event.ctrlKey
  if (mod) {
    parts.push('mod')
  }
  if (event.altKey) {
    parts.push('alt')
  }
  if (event.shiftKey) {
    parts.push('shift')
  }
  parts.push(normalizeKey(event.key))
  return parts.join('+')
}

// isFunctionKeyCombo reports whether a combo's key is F1 to F24, which types
// nothing and so is safe to fire from inside a text field.
export function isFunctionKeyCombo(combo: string): boolean {
  return /^f([1-9]|1[0-9]|2[0-4])$/.test(parseCombo(combo).key)
}

// comboHasModifier reports whether a combo includes any modifier, so callers can
// avoid firing modifier-less shortcuts while the user is typing in a field.
export function comboHasModifier(combo: string): boolean {
  const p = parseCombo(combo)
  return p.mod || p.alt || p.shift
}

/**
 * Whether pressing the combo in a text field would type something, so it must
 * not fire there: a key without modifiers, or a character typed with only
 * Shift held. Function keys type nothing.
 */
export function comboTypes(combo: string): boolean {
  const p = parseCombo(combo)
  if (p.mod || p.alt || isFunctionKeyCombo(combo)) {
    return false
  }
  return !p.shift || p.key.length === 1
}

// comboMatches reports whether a keydown event exactly matches a combo. the match
// is strict on every modifier so, for example, cmd+n never also fires plain n,
// and ctrl on macos (a distinct modifier we do not bind) never matches.
export function comboMatches(event: KeyboardEvent, combo: string): boolean {
  const p = parseCombo(combo)
  const primary = isMac ? event.metaKey : event.ctrlKey
  if (primary !== p.mod) {
    return false
  }
  // reject the non-primary platform modifier so combos stay unambiguous.
  if (isMac ? event.ctrlKey : event.metaKey) {
    return false
  }
  if (event.altKey !== p.alt || event.shiftKey !== p.shift) {
    return false
  }
  return normalizeKey(event.key) === p.key
}

// matchShortcut returns the action whose bound combo matches the event, or null.
// bindings maps each action to its current combo (defaults overlaid with the
// user's overrides).
export function matchShortcut(
  event: KeyboardEvent,
  bindings: Record<string, string>,
): ShortcutAction | null {
  for (const action of Object.keys(bindings)) {
    const combo = bindings[action]
    // an empty combo is an unbound action; never match it.
    if (!combo) {
      continue
    }
    if (comboMatches(event, combo)) {
      return action as ShortcutAction
    }
    // the alternate key, only while the binding is still the default one: a
    // rebound action answers to what it was rebound to and nothing else.
    const def = shortcuts.find((s) => s.action === action)
    if (def?.alt && combo === def.combo && comboMatches(event, def.alt)) {
      return action as ShortcutAction
    }
  }
  return null
}
