import { describe, expect, it } from 'vitest'
import { convertEditorBody, textToHtml } from './richtext'

describe('textToHtml', () => {
  it('keeps line breaks and paragraphs', () => {
    expect(textToHtml('one\ntwo\n\nthree')).toBe('<p>one<br>two</p><p>three</p>')
  })

  it('escapes markup', () => {
    expect(textToHtml('a <b> & "c"')).toBe('<p>a &lt;b&gt; &amp; "c"</p>')
  })

  it('turns "> " quoting into nested blockquotes', () => {
    expect(textToHtml('top\n> one\n> > two\n> back')).toBe(
      '<p>top</p><blockquote><p>one</p><blockquote><p>two</p></blockquote><p>back</p></blockquote>',
    )
  })

  it('reads ">>" and a bare ">" as quote levels', () => {
    expect(textToHtml('>> deep\n>\n> next')).toBe(
      '<blockquote><blockquote><p>deep</p></blockquote><p>next</p></blockquote>',
    )
  })
})
describe('convertEditorBody', () => {
  it('preserves Markdown quote line breaks when entering Rich', () => {
    const html = convertEditorBody('> First line\n> Second line', 'markdown', 'wysiwyg')
    const doc = new DOMParser().parseFromString(html, 'text/html')
    expect(doc.querySelectorAll('blockquote br')).toHaveLength(1)
  })

  it('keeps the trailing space of the signature delimiter when leaving Rich', () => {
    const html = convertEditorBody('body\n\n-- \nJan', 'plaintext', 'wysiwyg')
    expect(convertEditorBody(html, 'wysiwyg', 'plaintext')).toBe('body\n\n-- \nJan')
  })

  it('keeps the delimiter directly above a signature that is its own paragraph', () => {
    expect(convertEditorBody('<p>body</p><p>-- </p><p>Jan</p>', 'wysiwyg', 'plaintext')).toBe('body\n\n-- \nJan')
  })

  it('carries the empty lines above a reply quote into Rich and back', () => {
    const html = convertEditorBody('\n\nOn x wrote:\n> hi', 'plaintext', 'wysiwyg')
    expect(html).toMatch(/^<p><\/p><p>On x wrote:<\/p>/)
    expect(convertEditorBody(html, 'wysiwyg', 'plaintext')).toMatch(/^\n\nOn x wrote:/)
  })
})
