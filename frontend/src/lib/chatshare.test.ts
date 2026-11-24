import { describe, expect, it } from 'vitest'
import { buildShareText, trimExcerpt, withNote, type ShareSource } from './chatshare'

function source(patch: Partial<ShareSource> = {}): ShareSource {
  return {
    subject: 'Quarterly plan',
    fromName: 'Ann Example',
    fromAddress: 'Ann Example <ann@example.com>',
    date: '2026-10-01T09:00:00Z',
    bodyPlain: 'Hello team\nSee you soon',
    bodyQuote: '',
    ...patch,
  }
}

describe('buildShareText', () => {
  it('starts with the bold subject, the sender and the date', () => {
    const lines = buildShareText(source()).split('\n')
    expect(lines[0]).toBe('**Quarterly plan**')
    expect(lines[1]).toBe('From: Ann Example <ann@example.com>')
    expect(lines[2]).toMatch(/^Date: .*2026/)
  })

  it('uses the bare address when the sender has no name', () => {
    const text = buildShareText(source({ fromName: '', fromAddress: 'ann@example.com' }))
    expect(text).toContain('From: ann@example.com\n')
  })

  it('quotes every line of the body', () => {
    const text = buildShareText(source({ bodyPlain: 'one\n\ntwo' }))
    expect(text.endsWith('\n\n> one\n>\n> two')).toBe(true)
  })

  it('prefers the quotable text and drops quoted replies, attributions and signatures', () => {
    const body = ['Thanks', '', 'On Mon, Ann wrote:', '> old text', '', '-- ', 'Ann'].join('\n')
    const text = buildShareText(source({ bodyPlain: 'ignored', bodyQuote: body }))
    expect(text).toContain('> Thanks')
    expect(text).not.toContain('old text')
    expect(text).not.toContain('wrote')
    expect(text).not.toContain('Ann\n')
  })

  it('collapses runs of blank lines', () => {
    const text = buildShareText(source({ bodyPlain: 'a\n\n\n\n\nb' }))
    expect(text).toContain('> a\n>\n> b')
  })

  it('leaves out the excerpt for an empty body', () => {
    const text = buildShareText(source({ bodyPlain: '', bodyQuote: '' }))
    expect(text).not.toMatch(/^>/m)
    expect(text.split('\n')).toHaveLength(3)
  })

  it('trims a long body at a word boundary with three dots', () => {
    const body = 'word '.repeat(300)
    const text = buildShareText(source({ bodyPlain: body }))
    const quoted = text.split('\n\n')[1]
    expect(quoted.endsWith('word...')).toBe(true)
    expect(quoted.length).toBeLessThanOrEqual(2 + 600 + 3)
  })

  it('never writes em or en dashes', () => {
    const em = String.fromCharCode(0x2014)
    const en = String.fromCharCode(0x2013)
    const text = buildShareText(source({ subject: `Plan ${em} draft`, bodyPlain: `a ${en} b ${em} c` }))
    expect(text).not.toContain(em)
    expect(text).not.toContain(en)
    expect(text).toContain('a - b - c')
  })
})

describe('trimExcerpt', () => {
  it('keeps short text as it is', () => {
    expect(trimExcerpt('short', 10)).toBe('short')
  })

  it('does not cut inside a word', () => {
    expect(trimExcerpt('alpha beta gamma', 8)).toBe('alpha...')
  })
})

describe('withNote', () => {
  it('puts the note above the text', () => {
    expect(withNote(' fyi ', 'body')).toBe('fyi\n\nbody')
  })

  it('returns the text alone without a note', () => {
    expect(withNote('  ', 'body')).toBe('body')
  })
})
