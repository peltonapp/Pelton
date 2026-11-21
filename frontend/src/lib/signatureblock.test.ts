import { describe, it, expect } from 'vitest'
import { withSignatureBlock } from './signatureblock'
import type { Signature } from './types'

function sig(kind: 'header' | 'footer', content: string): Signature {
  return { id: 1, name: 's', kind, content, format: 'markdown' } as Signature
}

const quote = 'On Monday, Ann wrote:\n> hi\n'
const replyBody = `\n\n${quote}`

describe('withSignatureBlock', () => {
  it('appends a footer below everything by default', () => {
    expect(withSignatureBlock(replyBody, sig('footer', 'Bo'), 'plaintext')).toBe(`\n\n${quote.trimEnd()}\n\n-- \nBo`)
  })

  it('puts a footer above the quote of a reply', () => {
    expect(withSignatureBlock(replyBody, sig('footer', 'Bo'), 'plaintext', quote)).toBe(`\n\n-- \nBo\n\n${quote}`)
  })

  it('keeps what was typed above the quote ahead of the footer', () => {
    const typed = `Thanks!\n\n${quote}`
    expect(withSignatureBlock(typed, sig('footer', 'Bo'), 'plaintext', quote)).toBe(`Thanks!\n\n-- \nBo\n\n${quote}`)
  })

  it('falls back to the bottom once the quote is gone', () => {
    expect(withSignatureBlock('just text', sig('footer', 'Bo'), 'plaintext', quote)).toBe('just text\n\n-- \nBo')
  })

  it('places a footer above the quote in the rich editor', () => {
    const html = '<p></p><p>On Monday, Ann wrote:</p><blockquote><p>hi</p></blockquote>'
    const q = html.slice('<p></p>'.length)
    const s = { ...sig('footer', '<p>Bo</p>'), format: 'html' } as Signature
    expect(withSignatureBlock(html, s, 'wysiwyg', q)).toBe(`<p></p><p>-- </p><p>Bo</p>${q}`)
  })

  it('leaves headers at the top', () => {
    expect(withSignatureBlock(replyBody, sig('header', 'Hi'), 'plaintext', quote)).toBe(`Hi\n\n${replyBody}`)
  })
})
