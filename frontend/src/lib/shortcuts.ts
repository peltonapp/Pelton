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

// Shortcut pairs an action with its default combo and the label key for display.
export interface Shortcut {
  action: ShortcutAction
  combo: string
  labelKey: string
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

// comboHasModifier reports whether a combo includes any modifier, so callers can
// avoid firing modifier-less shortcuts while the user is typing in a field.
export function comboHasModifier(combo: string): boolean {
  const p = parseCombo(combo)
  return p.mod || p.alt || p.shift
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
