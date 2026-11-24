import { describe, expect, it } from 'vitest'
import { recordShortcut, shortcutKeys, type KeyPress } from './quickshortcut'

function press(code: string, mods: Partial<KeyPress> = {}): KeyPress {
  return { code, ctrlKey: false, altKey: false, shiftKey: false, metaKey: false, ...mods }
}

describe('recordShortcut', () => {
  it('reads the physical key, not the character Option types', () => {
    expect(recordShortcut(press('KeyM', { ctrlKey: true, altKey: true }), true)).toEqual({ kind: 'shortcut', accelerator: 'Ctrl+Option+M' })
    expect(recordShortcut(press('KeyM', { ctrlKey: true, altKey: true }), false)).toEqual({ kind: 'shortcut', accelerator: 'Ctrl+Alt+M' })
  })

  it('names Command on macOS and Super elsewhere', () => {
    expect(recordShortcut(press('Digit1', { metaKey: true, shiftKey: true }), true)).toEqual({ kind: 'shortcut', accelerator: 'Shift+Cmd+1' })
    expect(recordShortcut(press('Digit1', { metaKey: true, shiftKey: true }), false)).toEqual({ kind: 'shortcut', accelerator: 'Shift+Super+1' })
  })

  it('takes function and named keys', () => {
    expect(recordShortcut(press('F5', { ctrlKey: true }), false)).toEqual({ kind: 'shortcut', accelerator: 'Ctrl+f5' })
    expect(recordShortcut(press('Space', { altKey: true }), false)).toEqual({ kind: 'shortcut', accelerator: 'Alt+space' })
    expect(recordShortcut(press('PageUp', { ctrlKey: true }), false)).toEqual({ kind: 'shortcut', accelerator: 'Ctrl+page up' })
  })

  it('waits while only modifiers are down', () => {
    expect(recordShortcut(press('ControlLeft', { ctrlKey: true }))).toEqual({ kind: 'pending' })
    expect(recordShortcut(press('AltRight', { altKey: true }))).toEqual({ kind: 'pending' })
    expect(recordShortcut(press('MetaLeft', { metaKey: true }))).toEqual({ kind: 'pending' })
  })

  it('refuses a key without a strong modifier, or a key no shortcut can use', () => {
    expect(recordShortcut(press('KeyM'))).toEqual({ kind: 'invalid' })
    expect(recordShortcut(press('KeyM', { shiftKey: true }))).toEqual({ kind: 'invalid' })
    expect(recordShortcut(press('Minus', { ctrlKey: true }))).toEqual({ kind: 'invalid' })
    expect(recordShortcut(press('F13', { ctrlKey: true }))).toEqual({ kind: 'invalid' })
  })
})

describe('shortcutKeys', () => {
  it('splits an accelerator into keys to show', () => {
    expect(shortcutKeys('Ctrl+Option+M')).toEqual(['Ctrl', 'Option', 'M'])
    expect(shortcutKeys('Ctrl+page up')).toEqual(['Ctrl', 'Page Up'])
    expect(shortcutKeys('Alt+f5')).toEqual(['Alt', 'F5'])
    expect(shortcutKeys('')).toEqual([])
  })
})
