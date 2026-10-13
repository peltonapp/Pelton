import { describe, expect, it } from 'vitest'
import { parseAddressList, splitAddressList, renderBody } from './mailcompose'

describe('parseAddressList', () => {
  it('keeps a comma inside a quoted name', () => {
    expect(parseAddressList('"Doe, John" <j@x>, b@y')).toEqual([
      { name: 'Doe, John', email: 'j@x' },
      { name: '', email: 'b@y' },
    ])
  })

  it('splits on semicolons too', () => {
    expect(parseAddressList('a@x; b@y')).toHaveLength(2)
  })

  it('unescapes quotes in a name', () => {
    expect(parseAddressList('"Say \\"hi\\"" <h@x>')[0].name).toBe('Say "hi"')
  })

  it('does not split inside angle brackets', () => {
    expect(parseAddressList('Ann <a,b@x>')).toEqual([{ name: 'Ann', email: 'a,b@x' }])
  })

  it('rejoins an unquoted name with a comma as one address', () => {
    expect(parseAddressList('Doe, John <j@x>, b@y')).toEqual([
      { name: 'Doe, John', email: 'j@x' },
      { name: '', email: 'b@y' },
    ])
  })

  it('rejoins a name with several commas', () => {
    expect(parseAddressList('Doe, Jr., John <j@x>')).toEqual([{ name: 'Doe, Jr., John', email: 'j@x' }])
  })

  it('does not glue a stray word onto a bare address', () => {
    expect(splitAddressList('bob, a@x')).toEqual(['bob', 'a@x'])
  })

  it('drops empty tokens', () => {
    expect(parseAddressList(' , a@x,, ;')).toEqual([{ name: '', email: 'a@x' }])
  })
})

describe('splitAddressList', () => {
  it('returns trimmed raw tokens', () => {
    expect(splitAddressList('"Doe, John" <j@x> , b@y')).toEqual(['"Doe, John" <j@x>', 'b@y'])
  })
})

describe('rich message plain-text alternative', () => {
  it('preserves paragraphs, line breaks, and nested quotes in sent text', () => {
    expect(renderBody('wysiwyg', '<p>Hello &amp; welcome<br>Next line</p><blockquote><p>Original</p><blockquote><p>Older</p></blockquote></blockquote>').text)
      .toBe('Hello & welcome\nNext line\n\n> Original\n> \n> > Older')
  })

  it('keeps link destinations and lists readable without formatting markup', () => {
    expect(renderBody('wysiwyg', '<p><strong>See</strong> <a href="https://example.com">the details</a></p><ul><li>One</li><li>Two</li></ul>').text)
      .toBe('See the details (https://example.com)\n\n- One\n- Two')
  })
})

describe('markdown message html', () => {
  it('preserves quoted source lines in the HTML sent from Markdown', () => {
    const body = '> First line\n> Second line'
    const rendered = renderBody('markdown', body)
    const doc = new DOMParser().parseFromString(rendered.html, 'text/html')
    expect(doc.querySelectorAll('blockquote br')).toHaveLength(1)
    expect(rendered.text).toBe(body)
  })
})
