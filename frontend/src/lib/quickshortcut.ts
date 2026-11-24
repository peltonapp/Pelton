// quickshortcut.ts turns a key press into the system-wide shortcut quick
// compose registers, written the way Wails reads accelerators ("Ctrl+Alt+M").
// It reads the physical key (event.code) rather than the character, since
// Option on macOS turns M into µ and the shortcut is about the key either way.

import { isMac } from './platform'

/** The keys a shortcut may end in, by event.code, other than letters and digits. */
const namedKeys: Record<string, string> = {
  Space: 'space',
  Tab: 'tab',
  Enter: 'return',
  Backspace: 'backspace',
  Delete: 'delete',
  Home: 'home',
  End: 'end',
  PageUp: 'page up',
  PageDown: 'page down',
  ArrowLeft: 'left',
  ArrowRight: 'right',
  ArrowUp: 'up',
  ArrowDown: 'down',
}

/** What a key press amounts to while recording a shortcut. */
export type RecordedShortcut =
  /** only modifiers are down so far; keep waiting. */
  | { kind: 'pending' }
  /** a key no shortcut can use, or one without a modifier strong enough. */
  | { kind: 'invalid' }
  | { kind: 'shortcut'; accelerator: string }

/** The modifiers and key of a press, as KeyboardEvent carries them. */
export type KeyPress = Pick<KeyboardEvent, 'code' | 'ctrlKey' | 'altKey' | 'shiftKey' | 'metaKey'>

/**
 * recordShortcut reads one key press. A shortcut needs Control, Alt (Option),
 * or Command (Super) with the key; Shift alone would take a typed capital.
 */
export function recordShortcut(press: KeyPress, mac = isMac): RecordedShortcut {
  if (/^(Control|Alt|Shift|Meta|OS)(Left|Right)?$/.test(press.code)) {
    return { kind: 'pending' }
  }
  const key = keyName(press.code)
  if (!key || !(press.ctrlKey || press.altKey || press.metaKey)) {
    return { kind: 'invalid' }
  }
  const parts: string[] = []
  if (press.ctrlKey) {
    parts.push('Ctrl')
  }
  if (press.altKey) {
    parts.push(mac ? 'Option' : 'Alt')
  }
  if (press.shiftKey) {
    parts.push('Shift')
  }
  if (press.metaKey) {
    parts.push(mac ? 'Cmd' : 'Super')
  }
  parts.push(key)
  return { kind: 'shortcut', accelerator: parts.join('+') }
}

function keyName(code: string): string {
  const letter = /^Key([A-Z])$/.exec(code)
  if (letter) {
    return letter[1]
  }
  const digit = /^Digit([0-9])$/.exec(code)
  if (digit) {
    return digit[1]
  }
  const fn = /^F([1-9]|1[0-2])$/.exec(code)
  if (fn) {
    return `f${fn[1]}`
  }
  return namedKeys[code] ?? ''
}

/** The parts of an accelerator to show as keys, with named keys capitalised. */
export function shortcutKeys(accelerator: string): string[] {
  return accelerator
    .split('+')
    .filter((part) => part !== '')
    .map((part) => (part.length > 1 ? part.replace(/\b\w/g, (c) => c.toUpperCase()) : part))
}
