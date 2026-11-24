import { describe, expect, it } from 'vitest'
import { hasUnseen, releaseFor, releaseLine, releases, seenKey, type WhatsNewRelease } from './whatsnew'
import en from './locales/en'

const list: WhatsNewRelease[] = [
  { line: '2026.4', entries: [{ id: 'a', titleKey: 'x', bodyKey: 'y', target: { kind: 'history' } }] },
  {
    line: '2027.1',
    entries: [
      { id: 'b', titleKey: 'x', bodyKey: 'y', target: { kind: 'history' } },
      { id: 'c', titleKey: 'x', bodyKey: 'y', target: { kind: 'history' } },
    ],
  },
]

describe('release line', () => {
  it('reads plain releases with or without the v and patch', () => {
    expect(releaseLine('v2027.1')).toBe('2027.1')
    expect(releaseLine('2027.1.4')).toBe('2027.1')
    expect(releaseLine('v2026.04.8')).toBe('2026.4')
  })

  it('gives no line for nightlies, commit suffixes and dev builds', () => {
    expect(releaseLine('nightly-2026.11.15+e887200')).toBeNull()
    expect(releaseLine('nightly-2026.11.15-318-ge887200f')).toBeNull()
    expect(releaseLine('v2026.4.8-12-gabc1234')).toBeNull()
    expect(releaseLine('v2026.4.8-dirty')).toBeNull()
    expect(releaseLine('dev')).toBeNull()
    expect(releaseLine('')).toBeNull()
  })
})

describe('notes for a version', () => {
  it('picks the release line, or the newest one not newer', () => {
    expect(releaseFor('v2027.1.2', list)?.line).toBe('2027.1')
    expect(releaseFor('v2026.4.8', list)?.line).toBe('2026.4')
    expect(releaseFor('v2027.3', list)?.line).toBe('2027.1')
    expect(releaseFor('v2025.1', list)).toBeNull()
  })

  it('shows the newest notes on a nightly or dev build', () => {
    expect(releaseFor('nightly-2026.11.15', list)?.line).toBe('2027.1')
    expect(releaseFor('dev', list)?.line).toBe('2027.1')
  })
})

describe('unseen', () => {
  it('is unseen until the key of the same notes is stored', () => {
    const key = seenKey(list[1])
    expect(hasUnseen('', 'v2027.1', list)).toBe(true)
    expect(hasUnseen(key, 'v2027.1', list)).toBe(false)
  })

  it('does not come back for patch releases or new nightlies of the same notes', () => {
    const key = seenKey(list[1])
    expect(hasUnseen(key, 'v2027.1.3', list)).toBe(false)
    expect(hasUnseen(key, 'nightly-2026.11.15+aaa', list)).toBe(false)
    expect(hasUnseen(key, 'nightly-2026.11.16+bbb', list)).toBe(false)
  })

  it('comes back when the notes change, even within one line', () => {
    const key = seenKey(list[1])
    const grown = [list[0], { ...list[1], entries: [...list[1].entries, { id: 'd', titleKey: 'x', bodyKey: 'y', target: { kind: 'history' } as const }] }]
    expect(hasUnseen(key, 'nightly-2026.11.17', grown)).toBe(true)
  })

  it('stays quiet for a version without notes', () => {
    expect(hasUnseen('', 'v2025.1', list)).toBe(false)
  })
})

describe('shipped notes', () => {
  it('have unique ids and every text in English', () => {
    const strings = en as Record<string, string>
    for (const r of releases) {
      const ids = r.entries.map((e) => e.id)
      expect(new Set(ids).size).toBe(ids.length)
      for (const e of r.entries) {
        expect(strings[e.titleKey], e.titleKey).toBeTruthy()
        expect(strings[e.bodyKey], e.bodyKey).toBeTruthy()
      }
    }
  })
})
