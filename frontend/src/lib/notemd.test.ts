import { describe, expect, it } from 'vitest'
import {
  classifyLines,
  codeTokens,
  inlineTokens,
  joinPath,
  nextListPrefix,
  noteHeadings,
  noteTags,
  noteTitle,
  parseFrontMatter,
  parseWikiBody,
  safeNoteName,
  tableAligns,
  tableCells,
} from './notemd'

describe('parseFrontMatter', () => {
  it('reads block and inline lists, quotes and the end offset', () => {
    const text = '---\ntags:\n  - work\n  - "q a"\naliases: [One, \'Two\']\ntitle: Hello: world\n---\nbody'
    const fm = parseFrontMatter(text)
    expect(fm?.lastLine).toBe(6)
    expect(text.slice(fm!.end)).toBe('body')
    expect(fm?.props).toEqual([
      { key: 'tags', value: '', list: ['work', 'q a'] },
      { key: 'aliases', value: '', list: ['One', 'Two'] },
      { key: 'title', value: 'Hello: world', list: null },
    ])
  })

  it('keeps carriage returns out of values and finds the end of a CRLF block', () => {
    const text = '---\r\ntag: x\r\n---\r\nrest'
    const fm = parseFrontMatter(text)
    expect(fm?.props[0]).toEqual({ key: 'tag', value: 'x', list: null })
    expect(text.slice(fm!.end)).toBe('rest')
  })

  it('needs the block at the very top and closed', () => {
    expect(parseFrontMatter('text\n---\na: b\n---')).toBeNull()
    expect(parseFrontMatter('---\na: b\nno end')).toBeNull()
  })
})

describe('classifyLines', () => {
  it('tracks fences, headings, lists, tasks, quotes, callouts, rules and tables', () => {
    const lines = [
      '---',
      'a: 1',
      '---',
      '# Title',
      '```js',
      '# not a heading',
      '```',
      '- [x] done',
      '  1. item',
      '> [!warning]- Careful',
      '> body',
      '> plain quote continues',
      '',
      '> plain',
      '***',
      '| a | b |',
      '|---|:-:|',
      '| 1 | 2 |',
      'text',
    ]
    const k = classifyLines(lines)
    expect(k.slice(0, 3).map((x) => x.type)).toEqual(['frontmatter', 'frontmatter', 'frontmatter'])
    expect(k[3]).toEqual({ type: 'heading', level: 1, markEnd: 2 })
    expect(k[4]).toMatchObject({ type: 'fence', open: true, lang: 'js', block: 4 })
    expect(k[5]).toMatchObject({ type: 'code', block: 4 })
    expect(k[6]).toMatchObject({ type: 'fence', open: false, block: 4 })
    expect(k[7]).toMatchObject({ type: 'list', ordered: false, task: { checked: true, from: 2 } })
    expect(k[8]).toMatchObject({ type: 'list', ordered: true, markFrom: 2, task: null })
    expect(k[9]).toMatchObject({ type: 'quote', callout: { kind: 'warning', title: 'Careful', fold: '-', head: true } })
    expect(k[10]).toMatchObject({ type: 'quote', callout: { kind: 'warning', head: false } })
    expect(k[13]).toMatchObject({ type: 'quote', callout: null })
    expect(k[14].type).toBe('hr')
    expect(k[15]).toMatchObject({ type: 'table', block: 15, delimiter: false })
    expect(k[16]).toMatchObject({ type: 'table', block: 15, delimiter: true })
    expect(k[17]).toMatchObject({ type: 'table', block: 15 })
    expect(k[18].type).toBe('text')
  })

  it('does not close a fence on a different marker', () => {
    const k = classifyLines(['~~~', '```', 'x', '~~~', 'after'])
    expect(k.map((x) => x.type)).toEqual(['fence', 'code', 'code', 'fence', 'text'])
  })
})

describe('tables', () => {
  it('splits cells keeping escaped pipes and reads alignment', () => {
    expect(tableCells('| a | b \\| c | d |')).toEqual(['a', 'b \\| c', 'd'])
    expect(tableAligns('|:--|:-:|--:|---|')).toEqual(['left', 'center', 'right', ''])
  })
})

describe('inlineTokens', () => {
  it('reads wiki links with headings and aliases, hiding only the syntax', () => {
    const line = 'see [[Note#Part|shown]] and ![[img.png]]'
    const [wiki, embed] = inlineTokens(line)
    expect(wiki).toMatchObject({ kind: 'wiki', target: 'Note', subpath: 'Part', label: 'shown' })
    expect(line.slice(wiki.from, wiki.to)).toBe('[[Note#Part|shown]]')
    const visible = wiki.marks.reduce((s, [a, b]) => s.slice(0, a - wiki.from) + '\0'.repeat(b - a) + s.slice(b - wiki.from), line.slice(wiki.from, wiki.to)).replace(/\0/g, '')
    expect(visible).toBe('shown')
    expect(embed).toMatchObject({ kind: 'embed', target: 'img.png' })
    expect(line.slice(embed.from, embed.to)).toBe('![[img.png]]')
  })

  it('reads Markdown links, images, pelton links, urls and tags', () => {
    const tokens = inlineTokens('[a b](My%20Note.md#h) ![x](<p q.png>) [m](pelton://mail/1) https://x.org/y. #tag/sub #123')
    expect(tokens.map((t) => t.kind)).toEqual(['link', 'image', 'link', 'url', 'tag'])
    expect(tokens[0]).toMatchObject({ target: 'My Note.md', subpath: 'h', label: 'a b' })
    expect(tokens[1]).toMatchObject({ target: 'p q.png' })
    expect(tokens[2]).toMatchObject({ target: 'pelton://mail/1' })
    expect(tokens[3].target).toBe('https://x.org/y')
    expect(tokens[4].target).toBe('tag/sub')
  })

  it('reads emphasis, nesting and code, and leaves code alone', () => {
    const kinds = (s: string) => inlineTokens(s).map((t) => t.kind)
    expect(kinds('**bold *it* x**')).toEqual(['strong', 'em'])
    expect(kinds('***both***')).toEqual(['strong', 'em'])
    expect(kinds('~~gone~~ ==mark== %%hidden%%')).toEqual(['strike', 'highlight', 'comment'])
    expect(kinds('`a **b** [[c]]`')).toEqual(['code'])
    expect(kinds('snake_case_word and 2 * 3 * 4')).toEqual([])
    expect(kinds('\\*not em\\*')).toEqual([])
  })
})

describe('parseWikiBody', () => {
  it('splits target, heading and alias', () => {
    expect(parseWikiBody('A/B#C|D')).toEqual({ target: 'A/B', subpath: 'C', alias: 'D' })
    expect(parseWikiBody('#Local')).toEqual({ target: '', subpath: 'Local', alias: '' })
    expect(parseWikiBody('T\\|x')).toEqual({ target: 'T', subpath: '', alias: 'x' })
  })
})

describe('nextListPrefix', () => {
  it('continues bullets, numbers, tasks and quotes', () => {
    expect(nextListPrefix('- item')?.prefix).toBe('- ')
    expect(nextListPrefix('\t* item')?.prefix).toBe('\t* ')
    expect(nextListPrefix('9. item')?.prefix).toBe('10. ')
    expect(nextListPrefix('3) item')?.prefix).toBe('4) ')
    expect(nextListPrefix('- [x] done')?.prefix).toBe('- [ ] ')
    expect(nextListPrefix('> quote')?.prefix).toBe('> ')
    expect(nextListPrefix('plain')).toBeNull()
  })

  it('reports an empty item so Enter ends the list', () => {
    expect(nextListPrefix('- ')?.empty).toBe(true)
    expect(nextListPrefix('- [ ] ')?.empty).toBe(true)
    expect(nextListPrefix('- a')?.empty).toBe(false)
  })
})

describe('codeTokens', () => {
  it('marks comments, strings, numbers and keywords', () => {
    expect(codeTokens('const x = "a" // note', 'js').map((t) => t.kind)).toEqual(['keyword', 'string', 'comment'])
    expect(codeTokens('x = 42 # note', 'python').map((t) => t.kind)).toEqual(['number', 'comment'])
  })
})

describe('note text helpers', () => {
  it('collects tags from text and front matter, outside code', () => {
    expect(noteTags('---\ntags: [a, b]\n---\n#c `#d`\n```\n#e\n```\n#A')).toEqual(['a', 'b', 'c'])
  })

  it('collects headings with their lines', () => {
    expect(noteHeadings('# One\ntext\n## Two ##\n```\n# no\n```')).toEqual([
      { level: 1, text: 'One', line: 0 },
      { level: 2, text: 'Two', line: 2 },
    ])
  })

  it('names and joins paths safely', () => {
    expect(noteTitle('a/b/My Note.md')).toBe('My Note')
    expect(safeNoteName('a/b: c?')).toBe('a b c')
    expect(safeNoteName('...')).toBe('Untitled')
    expect(joinPath('', 'x.md')).toBe('x.md')
    expect(joinPath('/a/', 'x.md')).toBe('a/x.md')
  })
})
