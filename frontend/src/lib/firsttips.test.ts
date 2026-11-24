import { describe, expect, it } from 'vitest'
import { decideTip, parseSeen, serializeSeen, tipDef, tipDefs, tipParts, tipsPerSession, type TipGate } from './firsttips'
import { shortcuts } from './shortcuts'
import en from './locales/en'

const bound = Object.fromEntries(shortcuts.map((s) => [s.action, s.combo]))

function gate(over: Partial<TipGate> = {}): TipGate {
  return {
    enabled: true,
    suspended: false,
    busy: false,
    seen: new Set(),
    shownThisSession: 0,
    bindings: { ...bound, 'compose-send-archive': 'mod+shift+enter', 'compose-send-remind': 'mod+alt+enter' },
    learned: () => false,
    ...over,
  }
}

describe('tip rules', () => {
  const reader = tipDef('reader-view')

  it('shows a fresh tip', () => {
    expect(decideTip(reader, gate())).toBe('show')
  })

  it('stays away when switched off', () => {
    expect(decideTip(reader, gate({ enabled: false }))).toBe('off')
  })

  it('never shows during onboarding or another full-window screen', () => {
    expect(decideTip(reader, gate({ suspended: true }))).toBe('suspended')
  })

  it('shows one tip at a time', () => {
    expect(decideTip(reader, gate({ busy: true }))).toBe('busy')
  })

  it('shows at most one new tip per session', () => {
    expect(tipsPerSession).toBe(1)
    expect(decideTip(reader, gate({ shownThisSession: 1 }))).toBe('session')
  })

  it('never shows a tip again once seen', () => {
    expect(decideTip(reader, gate({ seen: new Set(['reader-view']) }))).toBe('seen')
  })

  it('waits while a key it names is unbound', () => {
    expect(decideTip(reader, gate({ bindings: { ...bound, 'reading-view': '' } }))).toBe('unbound')
  })

  it('retires a tip whose keys are already in use', () => {
    expect(decideTip(reader, gate({ learned: (a) => a === 'reading-view' }))).toBe('retire')
    const space = tipDef('space-keys')
    expect(decideTip(space, gate({ learned: (a) => a === 'nav-back' }))).toBe('show')
  })

  it('shows a tip without keys regardless of the coach', () => {
    expect(decideTip(tipDef('mail-drag'), gate({ learned: () => true }))).toBe('show')
  })
})

describe('tip list', () => {
  it('stays at about ten tips, each with English text naming its keys', () => {
    expect(tipDefs.length).toBeLessThanOrEqual(10)
    const strings = en as Record<string, string>
    for (const d of tipDefs) {
      const text = strings[d.textKey]
      expect(text, d.textKey).toBeTruthy()
      for (const name of Object.keys(d.keys)) {
        expect(text).toContain(`{${name}}`)
      }
      for (const action of Object.values(d.keys)) {
        expect(shortcuts.some((s) => s.action === action) || action.startsWith('compose-'), action).toBe(true)
      }
    }
  })
})

describe('seen storage', () => {
  it('round-trips and survives junk', () => {
    expect(parseSeen(serializeSeen(new Set(['b', 'a'])))).toEqual(new Set(['a', 'b']))
    expect(serializeSeen(new Set(['b', 'a']))).toBe('["a","b"]')
    expect(parseSeen('nope')).toEqual(new Set())
    expect(parseSeen('{"a":1}')).toEqual(new Set())
    expect(parseSeen('["a",3]')).toEqual(new Set(['a']))
  })
})

describe('tip text', () => {
  it('fills placeholders with the live combos', () => {
    const def = tipDef('space-keys')
    const parts = tipParts('{first} jumps. {back} and {forward} go.', def, { 'go-space-1': 'mod+1', 'nav-back': 'mod+[', 'nav-forward': 'mod+]' })
    expect(parts).toEqual([{ combo: 'mod+1' }, { text: ' jumps. ' }, { combo: 'mod+[' }, { text: ' and ' }, { combo: 'mod+]' }, { text: ' go.' }])
  })

  it('leaves unknown placeholders as text', () => {
    expect(tipParts('Type [[ and {x}.', tipDef('notes-links'), {})).toEqual([{ text: 'Type [[ and {x}.' }])
  })
})
