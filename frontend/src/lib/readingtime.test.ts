import { describe, expect, it } from 'vitest'
import { countWords, readingMinutes, visibleText, wordsPerMinute } from './readingtime'

describe('countWords', () => {
  it('counts words in any script and skips punctuation', () => {
    expect(countWords('Hello, world - it’s here.')).toBe(4)
    expect(countWords('Grüße aus Köln')).toBe(3)
    expect(countWords('مرحبا بالعالم')).toBe(2)
    expect(countWords('  ')).toBe(0)
  })
})

describe('readingMinutes', () => {
  it('stays quiet under about a minute', () => {
    expect(readingMinutes(0)).toBe(0)
    expect(readingMinutes(150)).toBe(0)
  })

  it('rounds to whole minutes from there', () => {
    expect(readingMinutes(wordsPerMinute)).toBe(1)
    expect(readingMinutes(wordsPerMinute * 4.4)).toBe(4)
    expect(readingMinutes(wordsPerMinute * 4.6)).toBe(5)
  })

  it('takes another pace', () => {
    expect(readingMinutes(600, 200)).toBe(3)
  })
})

describe('visibleText', () => {
  it('reads the text of html without its markup', () => {
    const text = visibleText({ isHtml: true, bodyHtmlSafe: '<p>One <b>two</b></p><p>three</p>', bodyPlain: '' })
    expect(countWords(text)).toBe(3)
  })

  it('leaves out the quoted original of a plain reply', () => {
    const body = 'Sounds good.\n\nOn Mon, Ada wrote:\n> a very long quoted message\n> with many words in it'
    expect(countWords(visibleText({ isHtml: false, bodyHtmlSafe: '', bodyPlain: body }))).toBeLessThan(8)
  })
})
