import { describe, expect, it } from 'vitest'
import { messageIdFromMid, midFromMessageId, parsePeltonUrl, peltonMarkdownLink, peltonUrl } from './peltonlinks'

describe('pelton links', () => {
  it('round-trips ids with characters a URL cannot hold', () => {
    const url = peltonUrl('mail', 'abc/def@host.example')
    expect(url).toBe('pelton://mail/abc%2Fdef%40host.example')
    expect(parsePeltonUrl(url)).toEqual({ kind: 'mail', id: 'abc/def@host.example' })
  })

  it('rejects other schemes and unknown kinds', () => {
    expect(parsePeltonUrl('https://x')).toBeNull()
    expect(parsePeltonUrl('pelton://folder/1')).toBeNull()
    expect(parsePeltonUrl('pelton://event/%E0%A4%A')).toBeNull()
  })

  it('writes a Markdown link with its label escaped', () => {
    expect(peltonMarkdownLink('task', 'u1', 'Pay [now]')).toBe('[Pay \\[now\\]](pelton://task/u1)')
    expect(peltonMarkdownLink('contact', 'a@b', '')).toBe('[a@b](pelton://contact/a%40b)')
  })

  it('converts between mid: URLs and Message-IDs', () => {
    expect(messageIdFromMid('mid:abc%40host')).toBe('abc@host')
    expect(messageIdFromMid('mid:abc%40host/part')).toBe('abc@host')
    expect(midFromMessageId('<abc@host>')).toBe('mid:abc%40host')
  })
})
