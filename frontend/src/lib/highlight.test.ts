import { describe, expect, it } from 'vitest'
import { highlightParts, markClass, markDocument, termPattern } from './highlight'

describe('highlightParts', () => {
  it('marks every match, ignoring case', () => {
    expect(highlightParts('Invoice for the invoice', ['invoice'])).toEqual([
      { text: 'Invoice', hit: true },
      { text: ' for the ', hit: false },
      { text: 'invoice', hit: true },
    ])
  })

  it('prefers the longer term where two overlap', () => {
    expect(highlightParts('reporting', ['rep', 'report'])).toEqual([
      { text: 'report', hit: true },
      { text: 'ing', hit: false },
    ])
  })

  it('treats regex characters as text', () => {
    expect(highlightParts('cost (net) 3.5', ['(net)', '3.5'])).toEqual([
      { text: 'cost ', hit: false },
      { text: '(net)', hit: true },
      { text: ' ', hit: false },
      { text: '3.5', hit: true },
    ])
  })

  it('returns the text whole when there is nothing to mark', () => {
    expect(highlightParts('hello', [])).toEqual([{ text: 'hello', hit: false }])
    expect(highlightParts('hello', ['a'])).toEqual([{ text: 'hello', hit: false }])
    expect(termPattern([' ', 'x'])).toBeNull()
  })
})

describe('markDocument', () => {
  it('wraps matches in text nodes and leaves markup alone', () => {
    const doc = document.implementation.createHTMLDocument('')
    doc.body.innerHTML = '<p>Quarterly <b>report</b> attached</p><a href="https://report.example">link</a>'
    expect(markDocument(doc, ['report'])).toBe(1)
    const marks = doc.querySelectorAll(`mark.${markClass}`)
    expect(marks).toHaveLength(1)
    expect(marks[0].parentElement?.tagName).toBe('B')
    expect(doc.querySelector('a')?.getAttribute('href')).toBe('https://report.example')
  })

  it('removes the previous marks before marking again', () => {
    const doc = document.implementation.createHTMLDocument('')
    doc.body.innerHTML = '<p>alpha beta</p>'
    markDocument(doc, ['alpha'])
    markDocument(doc, ['beta'])
    const marks = Array.from(doc.querySelectorAll('mark')).map((m) => m.textContent)
    expect(marks).toEqual(['beta'])
    markDocument(doc, [])
    expect(doc.querySelectorAll('mark')).toHaveLength(0)
    expect(doc.body.textContent).toBe('alpha beta')
  })
})
