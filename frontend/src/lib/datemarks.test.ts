import { describe, expect, it } from 'vitest'
import * as en from 'chrono-node/en'
import { collectText, dateAttr, dateClass, markRanges, unmarkDates } from './datemarks'
import { dateMentions, type ChronoLike } from './eventphrase'
import { markDocument } from './highlight'

const english: ChronoLike[] = [en.casual]
// Friday 13 November 2026, 09:00: when the mail was sent.
const sent = new Date(2026, 10, 13, 9)

function docOf(html: string): Document {
  const doc = document.implementation.createHTMLDocument('mail')
  doc.body.innerHTML = html
  return doc
}

function markAll(doc: Document): number {
  const body = collectText(doc.body)
  const found = dateMentions(body.text, { ref: sent, chrono: english })
  return markRanges(
    body,
    found.map((m, id) => ({ id, index: m.index, length: m.length })),
    (id) => `date ${id}`,
  )
}

const mail = `
  <p>Hi Ada,</p>
  <p>Could we meet <b>Thursday</b> at 3? Otherwise <i>12 March, 14:00 to 15:30</i> works.</p>
  <table><tr><td>Deadline</td><td>Friday</td></tr></table>
  <p>Details: <a href="https://example.com/x">see the plan for tomorrow morning</a>.</p>
  <blockquote>On 2 November 2026, Bob wrote:<br>see you tomorrow morning</blockquote>
  <script>var x = "tomorrow at 9"</script>
`

describe('collectText', () => {
  it('joins text nodes, breaking at blocks and leaving out links and scripts', () => {
    const doc = docOf(mail)
    const { text, runs } = collectText(doc.body)
    expect(text).toContain('Could we meet Thursday at 3? Otherwise 12 March, 14:00 to 15:30 works.')
    expect(text).toContain('Deadline\nFriday')
    expect(text).not.toContain('see the plan')
    expect(text).not.toContain('var x')
    for (const run of runs) {
      expect(text.slice(run.from, run.from + run.node.data.length)).toBe(run.node.data)
    }
  })
})

describe('markRanges', () => {
  it('marks the dates without changing a character of the mail', () => {
    const doc = docOf(mail)
    const before = doc.body.textContent
    const count = markAll(doc)
    expect(count).toBeGreaterThanOrEqual(4)
    expect(doc.body.textContent).toBe(before)
    const marked = Array.from(doc.querySelectorAll(`span.${dateClass}`)).map((s) => s.textContent)
    expect(marked.join('|')).toContain('12 March, 14:00 to 15:30')
    expect(marked).toContain('Friday')
  })

  it('gives a date across two elements one number, with focus on its first piece', () => {
    const doc = docOf(mail)
    markAll(doc)
    const pieces = Array.from(doc.querySelectorAll<HTMLElement>(`span.${dateClass}`)).filter((s) => s.closest('p')?.textContent?.includes('Thursday'))
    const thursday = pieces.filter((s) => s.getAttribute(dateAttr) === pieces[0].getAttribute(dateAttr))
    expect(thursday.map((s) => s.textContent).join('')).toBe('Thursday at 3')
    expect(thursday[0].tabIndex).toBe(0)
    expect(thursday[0].getAttribute('role')).toBe('button')
    expect(thursday[0].getAttribute('aria-label')).toMatch(/^date \d$/)
    expect(thursday[1].hasAttribute('tabindex')).toBe(false)
  })

  it('leaves links, quotes headers and scripts alone where they are not text', () => {
    const doc = docOf(mail)
    markAll(doc)
    expect(doc.querySelector('a')?.querySelector(`.${dateClass}`)).toBeNull()
    expect(doc.querySelector('script')?.querySelector(`.${dateClass}`)).toBeNull()
    expect(doc.querySelector('a')?.getAttribute('href')).toBe('https://example.com/x')
  })

  it('builds the marks as elements, so markup in the text stays text', () => {
    const doc = docOf('<p>On Friday &lt;b&gt;at 5pm&lt;/b&gt; please</p>')
    const before = doc.body.innerHTML
    markAll(doc)
    expect(doc.querySelector('b')).toBeNull()
    expect(doc.body.textContent).toBe('On Friday <b>at 5pm</b> please')
    unmarkDates(doc)
    expect(doc.body.innerHTML).toBe(before)
  })

  it('comes off again, giving back the same document', () => {
    const doc = docOf(mail)
    const before = doc.body.innerHTML
    markAll(doc)
    unmarkDates(doc)
    expect(doc.querySelector(`.${dateClass}`)).toBeNull()
    expect(doc.body.innerHTML).toBe(before)
  })

  it('lives with the search marks over the same text', () => {
    const doc = docOf('<p>Lunch on Friday at noon with the team</p>')
    const before = doc.body.textContent
    markDocument(doc, ['day at'])
    markAll(doc)
    expect(doc.body.textContent).toBe(before)
    expect(doc.querySelector(`.${dateClass}`)).not.toBeNull()
    markDocument(doc, [])
    unmarkDates(doc)
    expect(doc.body.textContent).toBe(before)
  })

  it('skips a range that has no text left to wrap', () => {
    const doc = docOf('<p>Nothing here</p>')
    const body = collectText(doc.body)
    expect(markRanges(body, [{ id: 0, index: 500, length: 4 }], () => '')).toBe(0)
    expect(doc.body.innerHTML).toBe('<p>Nothing here</p>')
  })
})
