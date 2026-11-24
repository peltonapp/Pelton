import { describe, expect, it } from 'vitest'
import { splitHtmlQuote, splitPlainQuote } from './quotes'

describe('splitHtmlQuote', () => {
  it('folds a Gmail quote', () => {
    const html = '<div>Sounds good.</div><div class="gmail_quote"><div>On Mon, Ada wrote:</div><blockquote>Lunch?</blockquote></div>'
    const split = splitHtmlQuote(html)
    expect(split.hasQuote).toBe(true)
    expect(split.head).toBe('<div>Sounds good.</div>')
  })

  it('folds a cited blockquote with the attribution line before it', () => {
    const html = '<p>Yes.</p><p>On 3 May, Bob wrote:</p><blockquote type="cite"><p>Coming?</p></blockquote>'
    const split = splitHtmlQuote(html)
    expect(split.hasQuote).toBe(true)
    expect(split.head).toBe('<p>Yes.</p>')
  })

  it('folds an Outlook original from its header block on', () => {
    const html = '<p>Done.</p><div id="divRplyFwdMsg"><b>From:</b> Ada</div><div>Can you finish it?</div>'
    expect(splitHtmlQuote(html)).toEqual({ head: '<p>Done.</p>', hasQuote: true })
  })

  it('keeps a reply written between quoted lines', () => {
    const answer = 'Here is a long answer to the first question. '.repeat(8)
    const html = `<p>See below.</p><blockquote>First question?</blockquote><p>${answer}</p><blockquote>Second?</blockquote>`
    expect(splitHtmlQuote(html).hasQuote).toBe(false)
  })

  it('keeps a message that is nothing but a quote', () => {
    expect(splitHtmlQuote('<blockquote>Only this</blockquote>').hasQuote).toBe(false)
  })

  it('leaves mail without a quote alone', () => {
    expect(splitHtmlQuote('<p>Hello</p>')).toEqual({ head: '<p>Hello</p>', hasQuote: false })
  })
})

describe('splitPlainQuote', () => {
  it('folds the quoted lines at the end with their attribution', () => {
    const text = 'Fine by me.\n\nOn Monday, Ada wrote:\n> Lunch at noon?\n>\n> Ada\n'
    expect(splitPlainQuote(text)).toEqual({ head: 'Fine by me.', hasQuote: true })
  })

  it('folds from an original message marker', () => {
    const text = 'Thanks!\n\n-----Original Message-----\nFrom: Bob\nSubject: Report'
    expect(splitPlainQuote(text)).toEqual({ head: 'Thanks!', hasQuote: true })
  })

  it('keeps quotes that are answered in between', () => {
    const text = '> First?\nYes.\n> Second?\nNo.'
    expect(splitPlainQuote(text).hasQuote).toBe(false)
  })

  it('keeps a message that is all quote', () => {
    expect(splitPlainQuote('> just this').hasQuote).toBe(false)
  })
})
