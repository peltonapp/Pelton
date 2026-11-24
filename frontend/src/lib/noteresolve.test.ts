import { describe, expect, it } from 'vitest'
import { cleanPath, createResolver, linkText, resolveLink, resolveMarkdownLink } from './noteresolve'

// the same cases as internal/notes/resolve_test.go, so both sides agree.
describe('resolveLink', () => {
  const r = createResolver([
    'Inbox.md',
    'Projects/Alpha.md',
    'Archive/Alpha.md',
    'Archive/Old/Alpha.md',
    'People/Ann Lee.md',
    'img/pic.png',
    'Deep/a/Shared.md',
    'Deep/b/Shared.md',
  ])
  const cases: [string, string, string][] = [
    ['Inbox', 'x.md', 'Inbox.md'],
    ['inbox.md', 'x.md', 'Inbox.md'],
    ['ann lee', 'x.md', 'People/Ann Lee.md'],
    ['Alpha', 'Archive/Note.md', 'Archive/Alpha.md'],
    ['Alpha', 'Projects/Note.md', 'Projects/Alpha.md'],
    ['Alpha', 'Other/Note.md', 'Archive/Alpha.md'],
    ['Archive/Old/Alpha', 'x.md', 'Archive/Old/Alpha.md'],
    ['Old/Alpha', 'x.md', 'Archive/Old/Alpha.md'],
    ['pic.png', 'x.md', 'img/pic.png'],
    ['pic', 'x.md', ''],
    ['Missing', 'x.md', ''],
    ['', 'self.md', 'self.md'],
    ['a/Shared', 'x.md', 'Deep/a/Shared.md'],
  ]
  for (const [target, from, want] of cases) {
    it(`resolves ${JSON.stringify(target)} from ${from}`, () => {
      expect(resolveLink(r, target, from)).toBe(want)
    })
  }
})

describe('resolveMarkdownLink', () => {
  const r = createResolver(['a/b/Note.md', 'a/Other.md', 'Top.md'])
  it('reads paths relative to the linking note first', () => {
    expect(resolveMarkdownLink(r, '../Other.md', 'a/b/Note.md')).toBe('a/Other.md')
    expect(resolveMarkdownLink(r, 'Note.md', 'a/b/x.md')).toBe('a/b/Note.md')
    expect(resolveMarkdownLink(r, 'Top.md', 'a/b/x.md')).toBe('Top.md')
    expect(resolveMarkdownLink(r, '/a/Other.md', 'a/b/x.md')).toBe('a/Other.md')
  })
})

describe('linkText', () => {
  it('uses the bare name when it is unique', () => {
    const r = createResolver(['One.md', 'a/Two.md', 'b/Two.md', 'img/x.png'])
    expect(linkText(r, 'One.md')).toBe('One')
    expect(linkText(r, 'a/Two.md')).toBe('a/Two')
    expect(linkText(r, 'img/x.png')).toBe('x.png')
  })
})

describe('paths', () => {
  it('cleans paths', () => {
    expect(cleanPath('a/./b/../c')).toBe('a/c')
    expect(cleanPath('../x')).toBe('../x')
  })
})
