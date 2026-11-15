import { describe, expect, it } from 'vitest'
import { addressOf, fieldBeside, holds, introducerToBcc, moveRecipient, senderToCc, withLeadLine } from './recipientmoves'

describe('addressOf', () => {
  it('reads the address of a named or bare recipient', () => {
    expect(addressOf('Ada Lovelace <Ada@Example.com>')).toBe('ada@example.com')
    expect(addressOf(' bob@x ')).toBe('bob@x')
  })
})

describe('fieldBeside', () => {
  it('moves down To, Cc, Bcc and up the other way, stopping at the ends', () => {
    expect(fieldBeside('to', 'down')).toBe('cc')
    expect(fieldBeside('cc', 'down')).toBe('bcc')
    expect(fieldBeside('bcc', 'down')).toBeNull()
    expect(fieldBeside('bcc', 'up')).toBe('cc')
    expect(fieldBeside('to', 'up')).toBeNull()
  })
})

describe('moveRecipient', () => {
  const fields = { to: 'Ada <ada@x>, bob@x', cc: 'carol@x', bcc: '' }

  it('moves a recipient as written', () => {
    expect(moveRecipient(fields, 'Ada <ada@x>', 'to', 'bcc')).toEqual({ to: 'bob@x', cc: 'carol@x', bcc: 'Ada <ada@x>' })
  })

  it('matches by address and keeps no duplicate', () => {
    expect(moveRecipient({ to: 'ADA@x', cc: 'Ada <ada@x>', bcc: '' }, 'ADA@x', 'to', 'cc')).toEqual({ to: '', cc: 'Ada <ada@x>', bcc: '' })
  })

  it('leaves the fields alone for a move onto itself', () => {
    expect(moveRecipient(fields, 'bob@x', 'to', 'to')).toBe(fields)
  })

  it('keeps a quoted name with a comma as one recipient', () => {
    expect(moveRecipient({ to: '"Doe, Jo" <jo@x>, bob@x', cc: '', bcc: '' }, '"Doe, Jo" <jo@x>', 'to', 'cc')).toEqual({
      to: 'bob@x',
      cc: '"Doe, Jo" <jo@x>',
      bcc: '',
    })
  })
})

describe('introducerToBcc', () => {
  const intro = { name: 'Ada', email: 'ada@x' }

  it('moves the introducer to Bcc', () => {
    expect(introducerToBcc({ to: 'Ada <ada@x>, bob@x', cc: '', bcc: '' }, intro)).toEqual({ to: 'bob@x', cc: '', bcc: 'Ada <ada@x>' })
  })

  it('takes the introducer out of Cc too', () => {
    expect(introducerToBcc({ to: 'bob@x', cc: 'ada@x', bcc: 'eve@x' }, intro)).toEqual({ to: 'bob@x', cc: '', bcc: 'eve@x, ada@x' })
  })

  it('moves Cc up to To when the introducer was all of To', () => {
    expect(introducerToBcc({ to: 'Ada <ada@x>', cc: 'bob@x, carol@x', bcc: '' }, intro)).toEqual({
      to: 'bob@x, carol@x',
      cc: '',
      bcc: 'Ada <ada@x>',
    })
  })

  it('has nothing to do when the introducer is not a recipient', () => {
    expect(introducerToBcc({ to: 'bob@x', cc: '', bcc: 'ada@x' }, intro)).toBeNull()
    expect(introducerToBcc({ to: 'bob@x', cc: '', bcc: '' }, { name: '', email: '' })).toBeNull()
  })
})

describe('senderToCc', () => {
  const sender = { name: 'Ada Lovelace', email: 'ada@x' }

  it('adds the sender to Cc', () => {
    expect(senderToCc({ to: 'bob@x', cc: 'carol@x', bcc: '' }, sender)).toEqual({ to: 'bob@x', cc: 'carol@x, Ada Lovelace <ada@x>', bcc: '' })
  })

  it('has nothing to do when the sender already gets it', () => {
    for (const field of ['to', 'cc', 'bcc'] as const) {
      const fields = { to: '', cc: '', bcc: '', [field]: 'ADA@x' }
      expect(senderToCc(fields, sender)).toBeNull()
    }
  })
})

describe('holds', () => {
  it('finds an address whatever its case and name', () => {
    expect(holds('Ada <Ada@X>, bob@x', 'ada@x')).toBe(true)
    expect(holds('bob@x', 'ada@x')).toBe(false)
  })
})

describe('withLeadLine', () => {
  const line = 'Thanks for the intro, moving you to Bcc.'

  it('puts a paragraph above a rich body and escapes it', () => {
    expect(withLeadLine('<p></p><blockquote>q</blockquote>', 'a < b', 'wysiwyg')).toBe('<p>a &lt; b</p><p></p><blockquote>q</blockquote>')
  })

  it('puts a line above a plain body, keeping the reply room under it', () => {
    expect(withLeadLine('\n\nOn Monday, Ada wrote:', line, 'plaintext')).toBe(`${line}\n\nOn Monday, Ada wrote:`)
    expect(withLeadLine('Hello', line, 'plaintext')).toBe(`${line}\n\nHello`)
    expect(withLeadLine('', line, 'plaintext')).toBe(line)
  })
})
