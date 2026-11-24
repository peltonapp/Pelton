import { describe, expect, it } from 'vitest'
import { looksLikeMarkdown } from './markdown'

describe('looksLikeMarkdown', () => {
  it.each([
    '# Notes',
    'Some **bold** words',
    '- one\n- two',
    '1. first\n2. second',
    '> quoted',
    'call `make run`',
    'see [the docs](https://example.com)',
    '```\ncode\n```',
  ])('reads %j as Markdown', (text) => {
    expect(looksLikeMarkdown(text)).toBe(true)
  })

  it.each(['Hello Ada,\n\nsee you on Monday.', 'Price: 5 * 3 = 15', 'C# is a language', 'a - b'])('leaves %j as prose', (text) => {
    expect(looksLikeMarkdown(text)).toBe(false)
  })
})
