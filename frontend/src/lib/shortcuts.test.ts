import { describe, expect, it } from 'vitest'
import { isMac, shortcutLabel } from './i18n'
import { comboHasModifier, comboMatches, comboTypes, eventToCombo, matchShortcut, parseCombo, shortcuts, spaceNumber } from './shortcuts'

// key builds a keydown event. `mod` is the platform's primary modifier, which
// is what a combo's "mod" token means, so the tests read the same on either
// platform and still exercise the real branch.
function key(k: string, mods: { mod?: boolean; alt?: boolean; shift?: boolean; other?: boolean } = {}): KeyboardEvent {
  return new KeyboardEvent('keydown', {
    key: k,
    metaKey: isMac ? !!mods.mod : !!mods.other,
    ctrlKey: isMac ? !!mods.other : !!mods.mod,
    altKey: !!mods.alt,
    shiftKey: !!mods.shift,
  })
}

describe('parseCombo', () => {
  it('reads a bare key', () => {
    expect(parseCombo('e')).toEqual({ mod: false, alt: false, shift: false, key: 'e' })
  })

  it('reads each modifier', () => {
    expect(parseCombo('mod+n')).toMatchObject({ mod: true, alt: false, shift: false, key: 'n' })
    expect(parseCombo('alt+n')).toMatchObject({ mod: false, alt: true, key: 'n' })
    expect(parseCombo('shift+n')).toMatchObject({ mod: false, shift: true, key: 'n' })
    expect(parseCombo('mod+alt+shift+n')).toEqual({ mod: true, alt: true, shift: true, key: 'n' })
  })

  it('is case insensitive', () => {
    expect(parseCombo('MOD+N')).toEqual(parseCombo('mod+n'))
  })

  it('keeps a key that is itself a plus sign usable', () => {
    expect(parseCombo('mod+,').key).toBe(',')
  })
})

describe('comboHasModifier', () => {
  it('is false for a bare letter', () => {
    expect(comboHasModifier('e')).toBe(false)
    expect(comboHasModifier('f11')).toBe(false)
  })

  it('is true for any modifier', () => {
    expect(comboHasModifier('mod+n')).toBe(true)
    expect(comboHasModifier('alt+n')).toBe(true)
    expect(comboHasModifier('shift+n')).toBe(true)
  })
})

describe('comboMatches', () => {
  it('matches a bare key', () => {
    expect(comboMatches(key('e'), 'e')).toBe(true)
  })

  it('matches regardless of the reported case', () => {
    expect(comboMatches(key('E'), 'e')).toBe(true)
  })

  it('matches the primary modifier', () => {
    expect(comboMatches(key('n', { mod: true }), 'mod+n')).toBe(true)
  })

  // the whole point of matching strictly: cmd+n must not also fire whatever is
  // bound to plain n.
  it('does not fire a bare combo while the modifier is held', () => {
    expect(comboMatches(key('n', { mod: true }), 'n')).toBe(false)
  })

  it('does not fire a modifier combo without the modifier', () => {
    expect(comboMatches(key('n'), 'mod+n')).toBe(false)
  })

  // ctrl on macOS, or cmd elsewhere, is a modifier Pelton never binds, so a
  // combo must not match while it is held.
  it('rejects the platform modifier that is not bound', () => {
    expect(comboMatches(key('n', { other: true }), 'n')).toBe(false)
    expect(comboMatches(key('n', { mod: true, other: true }), 'mod+n')).toBe(false)
  })

  it('is strict about alt and shift in both directions', () => {
    expect(comboMatches(key('n', { alt: true }), 'n')).toBe(false)
    expect(comboMatches(key('n'), 'alt+n')).toBe(false)
    expect(comboMatches(key('n', { shift: true }), 'n')).toBe(false)
    expect(comboMatches(key('n'), 'shift+n')).toBe(false)
  })

  it('matches the space key by name', () => {
    expect(comboMatches(key(' '), 'space')).toBe(true)
  })

  // the send shortcut (#480): Cmd+Enter on macOS, Ctrl+Enter elsewhere, and a
  // plain Enter is a new line, never a send.
  it('sends on mod+enter and on nothing less', () => {
    const send = shortcuts.find((s) => s.action === 'send')?.combo ?? ''
    expect(comboMatches(key('Enter', { mod: true }), send)).toBe(true)
    expect(comboMatches(key('Enter'), send)).toBe(false)
    expect(comboMatches(key('Enter', { shift: true }), send)).toBe(false)
    expect(comboMatches(key('Enter', { other: true }), send)).toBe(false)
  })
})

describe('eventToCombo', () => {
  it('builds a bare key', () => {
    expect(eventToCombo(key('e'))).toBe('e')
  })

  it('builds modifiers in a stable order', () => {
    expect(eventToCombo(key('n', { mod: true, alt: true, shift: true }))).toBe('mod+alt+shift+n')
  })

  it('records the primary modifier as mod', () => {
    expect(eventToCombo(key('n', { mod: true }))).toBe('mod+n')
  })

  it('names the space key', () => {
    expect(eventToCombo(key(' '))).toBe('space')
  })

  // the recorder has to wait for a real key rather than storing the modifier
  // the user is still holding down.
  it('returns nothing for a bare modifier press', () => {
    for (const k of ['Shift', 'Control', 'Alt', 'Meta']) {
      expect(eventToCombo(key(k))).toBeNull()
    }
  })

  it('round-trips through comboMatches', () => {
    const event = key('k', { mod: true })
    expect(comboMatches(event, eventToCombo(event)!)).toBe(true)
  })
})

describe('matchShortcut', () => {
  it('finds the action bound to the combo', () => {
    expect(matchShortcut(key('k', { mod: true }), { 'command-palette': 'mod+k' })).toBe('command-palette')
  })

  it('returns null when nothing matches', () => {
    expect(matchShortcut(key('j'), { 'command-palette': 'mod+k' })).toBeNull()
  })

  // an action with no key must never be reached by an event that happens to
  // carry an empty key.
  it('never matches an unbound action', () => {
    expect(matchShortcut(key(''), { quit: '' })).toBeNull()
  })

  it('honours the alternate key while the binding is untouched', () => {
    const withAlt = shortcuts.find((s) => s.alt)
    expect(withAlt, 'no shortcut defines an alt key any more').toBeDefined()
    const event = key(withAlt!.alt!)
    expect(matchShortcut(event, { [withAlt!.action]: withAlt!.combo })).toBe(withAlt!.action)
  })

  // rebinding is a statement about which key you want, so the alternate that
  // came with the default stops answering.
  it('drops the alternate key once the action is rebound', () => {
    const withAlt = shortcuts.find((s) => s.alt)!
    const event = key(withAlt.alt!)
    expect(matchShortcut(event, { [withAlt.action]: 'mod+shift+f9' })).toBeNull()
  })

  it('matches against the user binding, not the default', () => {
    expect(matchShortcut(key('j', { mod: true }), { 'command-palette': 'mod+j' })).toBe('command-palette')
    expect(matchShortcut(key('k', { mod: true }), { 'command-palette': 'mod+j' })).toBeNull()
  })
})

describe('the default registry', () => {
  it('binds every action at most once', () => {
    const seen = new Map<string, string>()
    for (const s of shortcuts) {
      if (!s.combo) {
        continue
      }
      expect(seen.has(s.combo), `${s.combo} is bound to both ${seen.get(s.combo)} and ${s.action}`).toBe(false)
      seen.set(s.combo, s.action)
    }
  })

  it('gives every entry a label key', () => {
    for (const s of shortcuts) {
      expect(s.labelKey, `${s.action} has no label key`).toBeTruthy()
    }
  })

  it('lists each action once', () => {
    const actions = shortcuts.map((s) => s.action)
    expect(new Set(actions).size).toBe(actions.length)
  })

  it('goes to the spaces by number on mod and a digit, which work while typing', () => {
    const map = Object.fromEntries(shortcuts.map((s) => [s.action, s.combo]))
    expect(matchShortcut(key('1', { mod: true }), map)).toBe('go-space-1')
    expect(matchShortcut(key('9', { mod: true }), map)).toBe('go-space-9')
    expect(matchShortcut(key('0', { mod: true }), map)).toBeNull()
    expect(comboTypes('mod+1')).toBe(false)
    expect(spaceNumber('go-space-4')).toBe(4)
    expect(spaceNumber('go-mail')).toBeNull()
  })

  it('opens the shortcut sheet on "?" as the key arrives, with Shift held', () => {
    const map = Object.fromEntries(shortcuts.map((s) => [s.action, s.combo]))
    expect(matchShortcut(key('?', { shift: true }), map)).toBe('shortcut-sheet')
    expect(shortcutLabel('shift+?')).toBe('?')
  })
})

describe('comboTypes', () => {
  it('holds back what would type in a text field', () => {
    expect(comboTypes('e')).toBe(true)
    expect(comboTypes('shift+?')).toBe(true)
    expect(comboTypes('backspace')).toBe(true)
  })

  it('lets modifier and function keys through', () => {
    expect(comboTypes('mod+k')).toBe(false)
    expect(comboTypes('mod+shift+h')).toBe(false)
    expect(comboTypes('alt+s')).toBe(false)
    expect(comboTypes('shift+f6')).toBe(false)
    expect(comboTypes('shift+arrowdown')).toBe(false)
  })
})
